package models

import (
	"context"
	"errors"
	"fmt"
	tdb "github.com/Grandbusta/tinio/internal/db"
	"time"

	"github.com/oklog/ulid/v2"
)

// Account is who has an account. Making one accepts the Terms and says they
// are 18 or older, as of its CreatedAt.
type Account struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func (a *Account) Save(ctx context.Context, db DB) error {
	a.ID = ulid.Make().String()
	_, err := db.Exec(ctx, `
		INSERT INTO accounts (id, email, name, terms_accepted_at, adult_at, created_at)
		VALUES ($1, $2, $3, $4, $4, $4)`,
		a.ID, a.Email, a.Name, a.CreatedAt)
	if err != nil {
		return fmt.Errorf("save account: %w", err)
	}
	return nil
}

// Owner is this machine's owner: the one account, made the first time.
func Owner(ctx context.Context, db DB, now time.Time) (Account, error) {
	var a Account
	err := db.QueryRow(ctx, `SELECT id, email, name, created_at FROM accounts ORDER BY created_at, id LIMIT 1`).
		Scan(&a.ID, &a.Email, &a.Name, &a.CreatedAt)
	if errors.Is(err, tdb.ErrNoRows) {
		a = Account{Email: OwnerEmail, CreatedAt: now}
		return a, a.Save(ctx, db)
	}
	if err != nil {
		return a, fmt.Errorf("load the owner: %w", err)
	}
	return a, nil
}

// OwnerEmail is the owner's account's email: there's one account, and no
// email is sent.
const OwnerEmail = "owner@localhost"

// FindAccountByEmail is the account that writes to email, whatever it signs in with.
func FindAccountByEmail(ctx context.Context, db DB, email string) (Account, bool, error) {
	var a Account
	err := db.QueryRow(ctx, `SELECT id, email, name, created_at FROM accounts WHERE email = $1`, email).
		Scan(&a.ID, &a.Email, &a.Name, &a.CreatedAt)
	return a, found(err), notFoundIsNil(err, "find account")
}

// found and notFoundIsNil read a single-row query: no row isn't an error.
func found(err error) bool { return err == nil }

func notFoundIsNil(err error, what string) error {
	if err == nil || errors.Is(err, tdb.ErrNoRows) {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}
