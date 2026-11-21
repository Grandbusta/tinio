package models

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Grandbusta/tinio/internal/character"
)

// Character is one version of a character to raise.
type Character struct {
	Key        string
	Version    int
	AccountID  string            // "" for Tinio's own
	Visibility string            // ready, private or retired
	Docs       map[string][]byte // by file name (character.TemplateFiles)
	CreatedAt  time.Time
}

const characterColumns = `key, version, coalesce(account_id, ''), visibility, template, character, judgment, created_at`

// SyncReady makes Tinio's own characters what ts are: a new version of each
// whose files changed, and those no longer among them retired. Many processes
// may run it at once: a version one of them saved first is left as it is.
func SyncReady(ctx context.Context, db DB, ts []character.Template, now time.Time) error {
	keys := make([]string, len(ts))
	for i, t := range ts {
		keys[i] = t.Key
		var last Character
		err := scanCharacter(db.QueryRow(ctx, `SELECT `+characterColumns+` FROM characters
			WHERE key = $1 AND account_id IS NULL ORDER BY version DESC LIMIT 1`, t.Key), &last)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("sync characters: %w", err)
		}
		if last.Visibility == "ready" && sameDocs(last.Docs, t.Docs) {
			continue
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO characters (key, version, visibility, template, character, judgment, created_at)
			VALUES ($1, $2, 'ready', $3, $4, $5, $6) ON CONFLICT DO NOTHING`,
			t.Key, last.Version+1, string(t.Docs["template.yaml"]), string(t.Docs["character.yaml"]), string(t.Docs["judgment.yaml"]), now); err != nil {
			return fmt.Errorf("sync characters: %w", err)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE characters SET visibility = 'retired'
		WHERE account_id IS NULL AND visibility = 'ready' AND NOT (key = ANY($1))`, keys); err != nil {
		return fmt.Errorf("sync characters: %w", err)
	}
	return nil
}

func sameDocs(a, b map[string][]byte) bool {
	for _, f := range character.TemplateFiles {
		if !bytes.Equal(a[f], b[f]) {
			return false
		}
	}
	return true
}

// CharactersFor is the newest version of each character account may raise:
// Tinio's ready ones, then their own, each by key.
func CharactersFor(ctx context.Context, db DB, account string) ([]Character, error) {
	return queryCharacters(ctx, db, account, "")
}

// CharacterFor is the newest version of the character key, if account may raise it.
func CharacterFor(ctx context.Context, db DB, account, key string) (Character, bool, error) {
	cs, err := queryCharacters(ctx, db, account, key)
	if err != nil || len(cs) == 0 {
		return Character{}, false, err
	}
	return cs[0], true, nil
}

// queryCharacters is every one ("" key) or one. A retired version is never
// offered, though agents raised from it keep their copy.
func queryCharacters(ctx context.Context, db DB, account, key string) ([]Character, error) {
	rows, err := db.Query(ctx, `
		SELECT * FROM (
			SELECT DISTINCT ON (key) `+characterColumns+` FROM characters
			WHERE (account_id IS NULL OR account_id = $1) AND ($2 = '' OR key = $2)
			ORDER BY key, version DESC
		) newest WHERE visibility <> 'retired' ORDER BY visibility = 'private', key`, account, key)
	if err != nil {
		return nil, fmt.Errorf("load characters: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Character, error) {
		var c Character
		return c, scanCharacter(r, &c)
	})
}

func scanCharacter(row pgx.Row, c *Character) error {
	var tmpl, char, judgment string
	if err := row.Scan(&c.Key, &c.Version, &c.AccountID, &c.Visibility, &tmpl, &char, &judgment, &c.CreatedAt); err != nil {
		return err
	}
	c.Docs = map[string][]byte{"template.yaml": []byte(tmpl), "character.yaml": []byte(char), "judgment.yaml": []byte(judgment)}
	return nil
}
