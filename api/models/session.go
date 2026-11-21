package models

import (
	"context"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// Session is a signed-in browser. Its token is kept only as a SHA-256 hash.
type Session struct {
	ID        string
	AccountID string
	TokenHash []byte
	CreatedAt time.Time
	ExpiresAt time.Time
}

func (s *Session) Save(ctx context.Context, db DB) error {
	s.ID = ulid.Make().String()
	_, err := db.Exec(ctx, `
		INSERT INTO sessions (id, account_id, token_hash, created_at, expires_at, last_seen_at) VALUES ($1, $2, $3, $4, $5, $4)`,
		s.ID, s.AccountID, s.TokenHash, s.CreatedAt, s.ExpiresAt)
	if err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

// SessionAccount is the account of an unexpired session. Its last-seen time is
// written at most once an hour, so reading a session rarely writes.
func SessionAccount(ctx context.Context, db DB, tokenHash []byte, now time.Time) (Account, bool, error) {
	var a Account
	err := db.QueryRow(ctx, `
		SELECT a.id, a.email, a.name, a.created_at FROM sessions s JOIN accounts a ON a.id = s.account_id
		WHERE s.token_hash = $1 AND s.expires_at > $2`, tokenHash, now).
		Scan(&a.ID, &a.Email, &a.Name, &a.CreatedAt)
	if err == nil {
		_, err = db.Exec(ctx, `UPDATE sessions SET last_seen_at = $2 WHERE token_hash = $1 AND last_seen_at < $3`,
			tokenHash, now, now.Add(-time.Hour))
	}
	return a, found(err), notFoundIsNil(err, "load session")
}

func DeleteSession(ctx context.Context, db DB, tokenHash []byte) error {
	_, err := db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}
