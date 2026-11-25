package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Grandbusta/tinio/internal/db"
	"github.com/Grandbusta/tinio/internal/memory"
)

// Morning is the morning for day (YYYY-MM-DD, the owner's time zone); false
// if none was prepared.
func (s *Store) Morning(ctx context.Context, day string) (memory.Morning, bool, error) {
	var m memory.Morning
	var sent *time.Time
	var items string
	err := s.queryRow(ctx, `SELECT day, prepared_at, sent_at, opening, question, items
		FROM mornings WHERE agent_id = $1 AND day = $2`, day).Scan(&m.Day, &m.PreparedAt, &sent, &m.Opening, &m.Question, &items)
	if errors.Is(err, db.ErrNoRows) {
		return memory.Morning{}, false, nil
	}
	if err != nil {
		return memory.Morning{}, false, fmt.Errorf("morning: %w", err)
	}
	if sent != nil {
		m.SentAt = *sent
	}
	if err := json.Unmarshal([]byte(items), &m.Items); err != nil {
		return memory.Morning{}, false, fmt.Errorf("morning items: %w", err)
	}
	return m, true, nil
}

// SaveMorning keeps a morning as it was prepared, or its items as they are
// now (a draft made): when it was sent stays as it was.
func (s *Store) SaveMorning(ctx context.Context, m memory.Morning) error {
	items, err := morningItems(m.Items)
	if err != nil {
		return err
	}
	_, err = s.exec(ctx, `INSERT INTO mornings (agent_id, day, prepared_at, opening, question, items) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (agent_id, day) DO UPDATE SET opening = $4, question = $5, items = $6`,
		m.Day, m.PreparedAt, m.Opening, m.Question, items)
	if err != nil {
		return fmt.Errorf("save morning: %w", err)
	}
	return nil
}

func (s *Store) MorningSent(ctx context.Context, day string, at time.Time) error {
	if _, err := s.exec(ctx, `UPDATE mornings SET sent_at = $3 WHERE agent_id = $1 AND day = $2`, day, at); err != nil {
		return fmt.Errorf("morning sent: %w", err)
	}
	return nil
}

// TickMorningItem marks one of a morning's items done or not; false if there's
// no such item.
func (s *Store) TickMorningItem(ctx context.Context, day, item string, done bool) (bool, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return false, fmt.Errorf("tick morning item: %w", err)
	}
	defer tx.Rollback(ctx)
	var raw string
	err = tx.QueryRow(ctx, `SELECT items FROM mornings WHERE agent_id = $1 AND day = $2`, s.agent, day).Scan(&raw)
	if errors.Is(err, db.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("tick morning item: %w", err)
	}
	var list []memory.MorningItem
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return false, fmt.Errorf("morning items: %w", err)
	}
	found := false
	for i := range list {
		if list[i].ID == item {
			list[i].Done, found = done, true
		}
	}
	if !found {
		return false, nil
	}
	items, err := morningItems(list)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE mornings SET items = $3 WHERE agent_id = $1 AND day = $2`, s.agent, day, items); err != nil {
		return false, fmt.Errorf("tick morning item: %w", err)
	}
	return true, tx.Commit(ctx)
}

// morningItems is items as kept: a JSON array, never null.
func morningItems(items []memory.MorningItem) (string, error) {
	if len(items) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(items)
	return string(b), err
}
