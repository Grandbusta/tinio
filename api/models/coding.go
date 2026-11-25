package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Coding is how coding jobs run: who writes the code, on what, checked how,
// and for how long.
type Coding struct {
	Coder    string `json:"coder"`     // a Claude Code model, ClaudePrefix and its id
	Copy     bool   `json:"copy"`      // work on a copy of the repository, on its own branch, not in it
	Review   bool   `json:"review"`    // the agent runs the tests and reads the changes before the owner sees them
	Minutes  int    `json:"minutes"`   // the longest a job runs
	MaxSteps int    `json:"max_steps"` // the most steps it may take
}

// DefaultCoding is how jobs run until the owner says otherwise: Opus 5.5
// writes the code (decided 25 November 2026), on a copy, checked.
var DefaultCoding = Coding{Coder: ClaudePrefix + "claude-opus-5-5", Copy: true, Review: true, Minutes: 30, MaxSteps: 80}

// Valid is what's wrong with c, if anything: the field and why.
func (c Coding) Valid() (field, why string) {
	switch {
	case !strings.HasPrefix(c.Coder, ClaudePrefix) || !(Job{}).Allows(c.Coder):
		return "coder", "Pick one of Claude Code's models."
	case c.Minutes < 5 || c.Minutes > 240:
		return "minutes", "A job runs for 5 to 240 minutes."
	case c.MaxSteps < 10 || c.MaxSteps > 500:
		return "max_steps", "A job takes 10 to 500 steps."
	}
	return "", ""
}

// CodingSettings is how coding jobs run now: the owner's, or the defaults.
func CodingSettings(ctx context.Context, db DB) (Coding, error) {
	var value string
	err := db.QueryRow(ctx, `SELECT value FROM app_settings WHERE name = 'coding'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultCoding, nil
	}
	if err != nil {
		return Coding{}, fmt.Errorf("coding settings: %w", err)
	}
	c := DefaultCoding
	if err := json.Unmarshal([]byte(value), &c); err != nil {
		return DefaultCoding, nil // unreadable: as if never set
	}
	return c, nil
}

// SaveCoding keeps how coding jobs run.
func SaveCoding(ctx context.Context, db DB, c Coding, at time.Time) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `INSERT INTO app_settings (name, value, set_at) VALUES ('coding', $1, $2)
		ON CONFLICT (name) DO UPDATE SET value = excluded.value, set_at = excluded.set_at`, string(b), at); err != nil {
		return fmt.Errorf("save coding settings: %w", err)
	}
	return nil
}
