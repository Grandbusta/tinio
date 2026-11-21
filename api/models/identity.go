package models

import (
	"context"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// Identity is one way to sign in to an account: email (Subject is the
// address) or google (Subject is Google's stable id). Unique by provider and
// subject.
type Identity struct {
	ID         string
	AccountID  string
	Provider   string
	Subject    string
	Email      string // what the provider said, for google
	CreatedAt  time.Time
	LastUsedAt time.Time
}

func (i *Identity) Save(ctx context.Context, db DB) error {
	i.ID = ulid.Make().String()
	_, err := db.Exec(ctx, `
		INSERT INTO identities (id, account_id, provider, subject, email, created_at, last_used_at) VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		i.ID, i.AccountID, i.Provider, i.Subject, i.Email, i.CreatedAt)
	if err != nil {
		return fmt.Errorf("save identity: %w", err)
	}
	return nil
}

// SignInWith is the account an identity signs in to, its last use noted.
func SignInWith(ctx context.Context, db DB, provider, subject string, at time.Time) (Account, bool, error) {
	var a Account
	err := db.QueryRow(ctx, `
		SELECT a.id, a.email, a.name, a.created_at FROM identities i JOIN accounts a ON a.id = i.account_id
		WHERE i.provider = $1 AND i.subject = $2`, provider, subject).
		Scan(&a.ID, &a.Email, &a.Name, &a.CreatedAt)
	if err == nil {
		_, err = db.Exec(ctx, `UPDATE identities SET last_used_at = $3 WHERE provider = $1 AND subject = $2`, provider, subject, at)
	}
	return a, found(err), notFoundIsNil(err, "find identity")
}
