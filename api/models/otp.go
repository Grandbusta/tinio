package models

import (
	"context"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// OTP is a one-time code, by Type: a sign-in code now; others later. It's
// kept only as its hash. One is live per type and target, and a new one
// replaces it.
type OTP struct {
	ID        string
	Type      string
	Target    string // where it went: an email address
	CodeHash  []byte
	Attempts  int // wrong tries
	ExpiresAt time.Time
	CreatedAt time.Time
}

// Save replaces the live code of its type and target, if there is one.
func (o *OTP) Save(ctx context.Context, db DB) error {
	o.ID = ulid.Make().String()
	_, err := db.Exec(ctx, `
		INSERT INTO otps (id, type, target, code_hash, attempts, expires_at, created_at)
		VALUES ($1, $2, $3, $4, 0, $5, $6)
		ON CONFLICT (type, target) WHERE used_at IS NULL
		DO UPDATE SET id = EXCLUDED.id, code_hash = EXCLUDED.code_hash, attempts = 0,
			expires_at = EXCLUDED.expires_at, created_at = EXCLUDED.created_at`,
		o.ID, o.Type, o.Target, o.CodeHash, o.ExpiresAt, o.CreatedAt)
	if err != nil {
		return fmt.Errorf("save code: %w", err)
	}
	return nil
}

// LiveOTP is the live code of typ for target, locked until the transaction
// db is in ends.
func LiveOTP(ctx context.Context, db DB, typ, target string) (OTP, bool, error) {
	var o OTP
	err := db.QueryRow(ctx, `
		SELECT id, type, target, code_hash, attempts, expires_at, created_at
		FROM otps WHERE type = $1 AND target = $2 AND used_at IS NULL FOR UPDATE`, typ, target).
		Scan(&o.ID, &o.Type, &o.Target, &o.CodeHash, &o.Attempts, &o.ExpiresAt, &o.CreatedAt)
	return o, found(err), notFoundIsNil(err, "load code")
}

func (o *OTP) WrongTry(ctx context.Context, db DB) error {
	o.Attempts++
	_, err := db.Exec(ctx, `UPDATE otps SET attempts = attempts + 1 WHERE id = $1`, o.ID)
	return err
}

func (o *OTP) Use(ctx context.Context, db DB, at time.Time) error {
	_, err := db.Exec(ctx, `UPDATE otps SET used_at = $2 WHERE id = $1`, o.ID, at)
	return err
}
