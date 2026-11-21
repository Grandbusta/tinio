package models

import (
	"context"
	"errors"
	"fmt"
	tdb "github.com/Grandbusta/tinio/internal/db"
	"time"

	"github.com/oklog/ulid/v2"
)

// Agent is one agent and who owns it.
type Agent struct {
	ID               string    `json:"id"`
	AccountID        string    `json:"-"`
	CharacterKey     string    `json:"character"` // the character it was raised from
	CharacterVersion int       `json:"-"`         // which version of it
	Name             string    `json:"name"`
	OwnerName        string    `json:"owner_name"` // what it calls its owner
	Relation         string    `json:"relation"`   // who its owner is to it
	Pace             int       `json:"pace"`
	CreatedAt        time.Time `json:"created_at"`
	Paused           bool      `json:"paused"`
	// QuietFrom and QuietUntil are HH:MM in their time zone; both "": no quiet hours.
	QuietFrom, QuietUntil string `json:"-"`
	ReadUntil             string `json:"-"` // the last chat row their owner has seen; read only by AgentsOf
}

func (a *Agent) Save(ctx context.Context, db DB) error {
	a.ID = ulid.Make().String()
	_, err := db.Exec(ctx, `
		INSERT INTO agents (id, account_id, character_key, character_version, name, owner_name, relation, pace, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		a.ID, a.AccountID, a.CharacterKey, a.CharacterVersion, a.Name, a.OwnerName, a.Relation, a.Pace, a.CreatedAt)
	if err == nil {
		_, err = db.Exec(ctx, `INSERT INTO agent_runs (agent_id) VALUES ($1)`, a.ID) // free for a worker to claim
	}
	if err != nil {
		return fmt.Errorf("save agent: %w", err)
	}
	return nil
}

const agentColumns = `id, account_id, character_key, name, owner_name, relation, pace, created_at, paused_at IS NOT NULL, quiet_from, quiet_until`

func (a *Agent) fields() []any {
	return []any{&a.ID, &a.AccountID, &a.CharacterKey, &a.Name, &a.OwnerName, &a.Relation, &a.Pace, &a.CreatedAt, &a.Paused, &a.QuietFrom, &a.QuietUntil}
}

// AgentsOf is an account's agents, oldest first.
func AgentsOf(ctx context.Context, db DB, account string) ([]Agent, error) {
	rows, err := db.Query(ctx, `
		SELECT `+agentColumns+`, owner_read_until FROM agents WHERE account_id = $1 ORDER BY created_at, id`, account)
	if err != nil {
		return nil, fmt.Errorf("load agents: %w", err)
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		var a Agent
		if err := rows.Scan(append(a.fields(), &a.ReadUntil)...); err != nil {
			return nil, fmt.Errorf("load agents: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AgentOf is the agent id if account owns them; false if not, or there's none.
func AgentOf(ctx context.Context, db DB, account, id string) (Agent, bool, error) {
	var a Agent
	err := db.QueryRow(ctx, `SELECT `+agentColumns+` FROM agents WHERE id = $1 AND account_id = $2`, id, account).Scan(a.fields()...)
	if errors.Is(err, tdb.ErrNoRows) {
		return a, false, nil
	}
	if err != nil {
		return a, false, fmt.Errorf("load agent: %w", err)
	}
	return a, true, nil
}

// MarkRead is their owner having seen agent's chat up to the row until. It
// never goes back.
func MarkRead(ctx context.Context, db DB, agent, until string) error {
	if _, err := db.Exec(ctx, `UPDATE agents SET owner_read_until = $2 WHERE id = $1 AND owner_read_until < $2`, agent, until); err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	return nil
}

// AgentVersion is one version of one of an agent's files, which the mind reads
// them from.
type AgentVersion struct {
	ID        string
	AgentID   string
	Section   string // character | work | judgment
	Doc       string
	Source    string
	CreatedAt time.Time
}

func (v *AgentVersion) Save(ctx context.Context, db DB) error {
	v.ID = ulid.Make().String()
	_, err := db.Exec(ctx, `
		INSERT INTO agent_versions (agent_id, id, section, doc, source, at) VALUES ($1, $2, $3, $4, $5, $6)`,
		v.AgentID, v.ID, v.Section, v.Doc, v.Source, v.CreatedAt)
	if err != nil {
		return fmt.Errorf("save %s: %w", v.Section, err)
	}
	return nil
}
