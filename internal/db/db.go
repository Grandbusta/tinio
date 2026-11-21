// Package db is Tinio's database: one SQLite file, opened with its migrations
// run, and a small API over database/sql that the store and the API share.
//
// Values are converted both ways, so callers pass and scan Go values as they
// are: a time is UTC text of one fixed width (so times sort and compare as
// text), a []float32 a little-endian BLOB, a []string, map or struct JSON
// text, a bool 0 or 1. Placeholders are written $1, $2, ... as in Postgres.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sync"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNoRows is what Row.Scan returns when there's no row.
var ErrNoRows = sql.ErrNoRows

// DB is the open database, and what's said about it in this process
// (Notify, Listen).
type DB struct {
	sql *sql.DB
	*hub
}

// Open opens the file at path, made if it isn't there, and brings it up to
// date. WAL lets reads go on while one write happens; a write waits up to ten
// seconds for another to finish; foreign keys are checked.
func Open(ctx context.Context, path string) (*DB, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)" +
		"&_pragma=synchronous(NORMAL)&_txlock=immediate"
	s, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := s.PingContext(ctx); err != nil {
		s.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := migrate(ctx, s); err != nil {
		s.Close()
		return nil, err
	}
	return &DB{sql: s, hub: &hub{}}, nil
}

func migrate(ctx context.Context, s *sql.DB) error {
	dir, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, s, dir, goose.WithTableName("db_migrations"))
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

func (d *DB) Close() error { return d.sql.Close() }

// Querier is the database or a transaction.
type Querier interface {
	Exec(ctx context.Context, query string, args ...any) (Result, error)
	Query(ctx context.Context, query string, args ...any) (*Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) *Row
}

func (d *DB) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	return exec(ctx, d.sql, query, args)
}

func (d *DB) Query(ctx context.Context, query string, args ...any) (*Rows, error) {
	return queryRows(ctx, d.sql, query, args)
}

func (d *DB) QueryRow(ctx context.Context, query string, args ...any) *Row {
	return queryRow(ctx, d.sql, query, args)
}

// Begin starts a transaction, holding the database's one write lock from the
// start (_txlock=immediate), so two never deadlock upgrading to a write.
func (d *DB) Begin(ctx context.Context) (*Tx, error) {
	tx, err := d.sql.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx, db: d}, nil
}

// Tx is a transaction. What's notified in it is told only once it commits.
type Tx struct {
	tx      *sql.Tx
	db      *DB
	pending [][2]string
}

func (t *Tx) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	return exec(ctx, t.tx, query, args)
}

func (t *Tx) Query(ctx context.Context, query string, args ...any) (*Rows, error) {
	return queryRows(ctx, t.tx, query, args)
}

func (t *Tx) QueryRow(ctx context.Context, query string, args ...any) *Row {
	return queryRow(ctx, t.tx, query, args)
}

// Notify tells channel's listeners payload once the transaction commits.
func (t *Tx) Notify(channel, payload string) {
	t.pending = append(t.pending, [2]string{channel, payload})
}

func (t *Tx) Commit(context.Context) error {
	if err := t.tx.Commit(); err != nil {
		return err
	}
	for _, n := range t.pending {
		t.db.Notify(n[0], n[1])
	}
	return nil
}

// Rollback after Commit does nothing, so it can be deferred.
func (t *Tx) Rollback(context.Context) error {
	if err := t.tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return err
	}
	return nil
}

// Result is what a statement changed.
type Result struct{ n int64 }

func (r Result) RowsAffected() int64 { return r.n }

type runner interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Statements run without their caller's cancellation (context.WithoutCancel):
// database/sql would otherwise watch the context from a goroutine of its own,
// and a request's context (gin's) is reused once the request ends. A
// statement on a local file is quick, and a transaction ends when its caller
// commits or rolls it back.
func exec(ctx context.Context, r runner, query string, args []any) (Result, error) {
	a, err := bind(args)
	if err != nil {
		return Result{}, err
	}
	res, err := r.ExecContext(context.WithoutCancel(ctx), numbered(query), a...)
	if err != nil {
		return Result{}, err
	}
	n, err := res.RowsAffected()
	return Result{n}, err
}

func queryRows(ctx context.Context, r runner, query string, args []any) (*Rows, error) {
	a, err := bind(args)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(context.WithoutCancel(ctx), numbered(query), a...)
	if err != nil {
		return nil, err
	}
	return &Rows{rows: rows}, nil
}

func queryRow(ctx context.Context, r runner, query string, args []any) *Row {
	a, err := bind(args)
	if err != nil {
		return &Row{err: err}
	}
	return &Row{row: r.QueryRowContext(context.WithoutCancel(ctx), numbered(query), a...)}
}

// Rows are a query's rows; Close them, or read them to the end.
type Rows struct{ rows *sql.Rows }

func (r *Rows) Next() bool { return r.rows.Next() }
func (r *Rows) Err() error { return r.rows.Err() }
func (r *Rows) Close()     { r.rows.Close() }

func (r *Rows) Scan(dest ...any) error {
	return scanInto(r.rows.Scan, dest)
}

// Row is one row; Scan says ErrNoRows if there's none.
type Row struct {
	row *sql.Row
	err error
}

func (r *Row) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return scanInto(r.row.Scan, dest)
}

// Scanner is a row being read.
type Scanner interface{ Scan(dest ...any) error }

// CollectRows reads every row with each, and closes rows.
func CollectRows[T any](rows *Rows, each func(Scanner) (T, error)) ([]T, error) {
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := each(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

var placeholder = regexp.MustCompile(`\$(\d+)`)

// numbered is query with $n as ?n, SQLite's numbered placeholder, worked out
// once per query.
func numbered(query string) string {
	if q, ok := rewritten.Load(query); ok {
		return q.(string)
	}
	q := placeholder.ReplaceAllString(query, "?$1")
	rewritten.Store(query, q)
	return q
}

var rewritten sync.Map

// WithTx runs do in a transaction, committed if do returns nil.
func (d *DB) WithTx(ctx context.Context, do func(*Tx) error) error {
	tx, err := d.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := do(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
