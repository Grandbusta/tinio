package models

import (
	"context"
	"errors"
	"fmt"
	"time"

	tdb "github.com/Grandbusta/tinio/internal/db"
	"github.com/Grandbusta/tinio/internal/secret"
)

// KeyOpenAI is the owner's OpenAI key, by its name in keys.
const KeyOpenAI = "openai"

// KeysChanged is the channel told, with the key's name, when the owner sets
// or removes one.
const KeysChanged = "keys_changed"

// SaveKey keeps the owner's key name, sealed with key; value "" removes it.
func SaveKey(ctx context.Context, db DB, key secret.Key, name, value string, at time.Time) error {
	var err error
	if value == "" {
		_, err = db.Exec(ctx, `DELETE FROM keys WHERE name = $1`, name)
	} else {
		_, err = db.Exec(ctx, `INSERT INTO keys (name, sealed, set_at) VALUES ($1, $2, $3)
			ON CONFLICT (name) DO UPDATE SET sealed = excluded.sealed, set_at = excluded.set_at`, name, key.Seal("key:"+name, value), at)
	}
	if err != nil {
		return fmt.Errorf("save key: %w", err)
	}
	return nil
}

// OpenKey is the owner's key name; "" if it isn't set.
func OpenKey(ctx context.Context, db DB, key secret.Key, name string) (string, error) {
	var sealed []byte
	err := db.QueryRow(ctx, `SELECT sealed FROM keys WHERE name = $1`, name).Scan(&sealed)
	if errors.Is(err, tdb.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load key: %w", err)
	}
	return key.Open("key:"+name, sealed)
}
