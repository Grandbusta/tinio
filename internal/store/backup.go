package store

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// A backup is one agent's: a zip of backup.json, then each of the agent's
// tables as CSV (only their rows), in an order that loads without breaking a
// foreign key. Secrets are in it as stored, sealed. Sources, shared by every
// agent, are not in it.
const backupFormat = 1

type Backup struct {
	Format  int           `json:"format"`
	Whose   string        `json:"whose"`
	Version int64         `json:"version"` // the newest migration it was made at
	At      time.Time     `json:"at"`
	Tables  []BackupTable `json:"tables"`
}

type BackupTable struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Rows    int64    `json:"rows"`
}

func (b Backup) Rows() int64 {
	var n int64
	for _, t := range b.Tables {
		n += t.Rows
	}
	return n
}

// Backup writes everything the agent has as it is at one moment, so they can
// keep running while it's made.
func (s *Store) Backup(ctx context.Context, w io.Writer, at time.Time) (Backup, error) {
	tx, err := s.begin(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Backup{}, fmt.Errorf("backup: %w", err)
	}
	defer tx.Rollback(ctx)
	b := Backup{Format: backupFormat, At: at}
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_id), 0) FROM db_migrations WHERE is_applied`).Scan(&b.Version); err != nil {
		return Backup{}, fmt.Errorf("backup: %w", err)
	}
	switch err := tx.QueryRow(ctx, `SELECT name FROM agents WHERE id = $1`, s.agent).Scan(&b.Whose); {
	case errors.Is(err, pgx.ErrNoRows):
		return Backup{}, fmt.Errorf("backup: there's no agent %s", s.agent)
	case err != nil:
		return Backup{}, fmt.Errorf("backup: %w", err)
	}
	if b.Tables, err = tables(ctx, tx); err != nil {
		return Backup{}, err
	}
	z := zip.NewWriter(w)
	for i, t := range b.Tables {
		f, err := z.Create(t.Name + ".csv")
		if err != nil {
			return Backup{}, err
		}
		cols := make([]string, len(t.Columns))
		for j, c := range t.Columns {
			cols[j] = pgx.Identifier{c}.Sanitize()
		}
		// COPY takes no parameters, so the agent goes in as a quoted literal.
		tag, err := tx.Conn().PgConn().CopyTo(ctx, f, fmt.Sprintf("COPY (SELECT %s FROM %s WHERE agent_id = '%s') TO STDOUT WITH (FORMAT csv)",
			strings.Join(cols, ", "), pgx.Identifier{t.Name}.Sanitize(), strings.ReplaceAll(s.agent, "'", "''")))
		if err != nil {
			return Backup{}, fmt.Errorf("backup %s: %w", t.Name, err)
		}
		b.Tables[i].Rows = tag.RowsAffected()
	}
	f, err := z.Create("backup.json")
	if err != nil {
		return Backup{}, err
	}
	if err := json.NewEncoder(f).Encode(b); err != nil {
		return Backup{}, err
	}
	return b, z.Close()
}

// tables are those an agent's rows are in (the ones with agent_id), with their
// columns, each after those of them it refers to.
func tables(ctx context.Context, tx pgx.Tx) ([]BackupTable, error) {
	rows, err := tx.Query(ctx, `SELECT c.relname::text, array_agg(a.attname::text ORDER BY a.attnum),
		COALESCE((SELECT array_agg(DISTINCT p.relname::text) FROM pg_constraint f JOIN pg_class p ON p.oid = f.confrelid
			WHERE f.conrelid = c.oid AND f.contype = 'f' AND f.confrelid <> c.oid), '{}')
		FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid
		WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r'
			AND a.attnum > 0 AND NOT a.attisdropped AND a.attgenerated = ''
		GROUP BY c.oid, c.relname HAVING bool_or(a.attname = 'agent_id') ORDER BY c.relname`)
	if err != nil {
		return nil, fmt.Errorf("tables: %w", err)
	}
	defer rows.Close()
	var all []BackupTable
	refers := map[string][]string{}
	for rows.Next() {
		var t BackupTable
		var parents []string
		if err := rows.Scan(&t.Name, &t.Columns, &parents); err != nil {
			return nil, err
		}
		all = append(all, t)
		refers[t.Name] = parents
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []BackupTable
	placed := map[string]bool{}
	for len(out) < len(all) {
		before := len(out)
		for _, t := range all {
			if !placed[t.Name] && !slices.ContainsFunc(refers[t.Name], func(p string) bool {
				_, ours := refers[p]
				return ours && !placed[p]
			}) {
				out, placed[t.Name] = append(out, t), true
			}
		}
		if len(out) == before {
			return nil, errors.New("tables: their foreign keys go round in a circle")
		}
	}
	return out, nil
}
