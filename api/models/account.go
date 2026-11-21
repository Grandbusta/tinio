package models

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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
	if err == nil || errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}
