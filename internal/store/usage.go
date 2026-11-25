package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/Grandbusta/tinio/internal/db"
	"time"

	"github.com/Grandbusta/tinio/internal/usage"
)

const standingColumns = `a.week_usd, a.created_at, coalesce(u.week_start, a.created_at), coalesce(u.week_usd, 0),
	coalesce(u.hour_start, a.created_at), coalesce(u.hour_usd, 0)`

func scanStanding(row db.Scanner) (usage.Standing, error) {
	var st usage.Standing
	err := row.Scan(&st.CapUSD, &st.Started, &st.WeekStart, &st.WeekUSD, &st.HourStart, &st.HourUSD)
	if errors.Is(err, db.ErrNoRows) {
		return usage.Standing{}, nil
	}
	if err != nil {
		return usage.Standing{}, fmt.Errorf("load usage: %w", err)
	}
	return st, nil
}

// Standing is the usage of the account the agent belongs to; the zero value
// (nothing metered) if it belongs to none.
func (s *Store) Standing(ctx context.Context) (usage.Standing, error) {
	return scanStanding(s.queryRow(ctx, `
		SELECT `+standingColumns+` FROM agents g JOIN accounts a ON a.id = g.account_id
		LEFT JOIN account_usage u ON u.account_id = a.id WHERE g.id = $1`))
}

// AccountStanding is account's usage; the zero value if there's no such account.
func (s *Store) AccountStanding(ctx context.Context, account string) (usage.Standing, error) {
	return scanStanding(s.db.QueryRow(ctx, `
		SELECT `+standingColumns+` FROM accounts a LEFT JOIN account_usage u ON u.account_id = a.id
		WHERE a.id = $1`, account))
}

// Spent is what the agent's decisions since since cost: chat is what someone
// said to it and what it did for them (replies, taps, reminders and promises
// kept), day is the rest (its work on its own: mornings, drafts, what it watches). replies is how many messages it answered.
func (s *Store) Spent(ctx context.Context, since time.Time) (chat, day float64, replies int, err error) {
	err = s.queryRow(ctx, `
		SELECT coalesce(sum(d.cost_usd) FILTER (WHERE p.kind IN `+chatKinds+`), 0),
		       coalesce(sum(d.cost_usd) FILTER (WHERE p.kind IS NULL OR p.kind NOT IN `+chatKinds+`), 0),
		       count(*) FILTER (WHERE d.parent_id IS NULL AND p.kind = 'message')
		FROM decisions d LEFT JOIN percepts p ON p.agent_id = d.agent_id AND p.id = d.percept_id
		WHERE d.agent_id = $1 AND d.at >= $2`, since).Scan(&chat, &day, &replies)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("load what was spent: %w", err)
	}
	return chat, day, replies, nil
}

const chatKinds = `('message', 'hello', 'approval', 'call', 'reminder', 'delivery')`
