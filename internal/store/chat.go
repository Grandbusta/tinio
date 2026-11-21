package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/memory"
)

// The channels the chat notifies, with "<agent id>:<row id>": the owner's rows
// go to whichever worker runs the agent, the agent's (and a row settled) to the
// app. Drafting is the reply being written, "<agent id>:<all of it so far>".
const (
	ToAgent   = "chat_to_agent"
	FromAgent = "chat_from_agent"
	Drafting  = "chat_draft"
)

// draftAtMost is the longest draft told, in bytes: a notification holds less
// than 8000. A longer reply shows once it's sent.
const draftAtMost = 7000

// WaitingOwner is the wake_at of an agent whose owner is waiting on them (as
// SQL): the earliest there is, so workers claim them before anyone who is only
// due, and even when full (internal/host), so a crowd of those, like a
// morning's check-ins, never holds up a reply.
const WaitingOwner = `'epoch'::timestamptz`

// SaveChat keeps a chat row and, in the same statement, tells whoever listens
// for that side. The owner's also wakes the agent, first in line, if they're
// resting.
func (s *Store) SaveChat(ctx context.Context, m memory.ChatMessage) error {
	channel := FromAgent
	if m.Author == memory.ChatOwner {
		channel = ToAgent
	}
	if m.Kind == "" {
		m.Kind = memory.ChatMessageKind
	}
	_, err := s.exec(ctx, `
		WITH m AS (INSERT INTO chat_messages (agent_id, id, author, kind, text, payload, at, topic_id, reply_to)
			VALUES ($1, $2, $3, $4, $5, $6, $7, nullif($9, ''), nullif($10, '')) RETURNING agent_id, id),
		woken AS (UPDATE agent_runs SET wake_at = `+WaitingOwner+` WHERE agent_id = $1 AND $3 = 'owner')
		SELECT pg_notify($8, agent_id || ':' || id) FROM m`, m.ID, m.Author, m.Kind, m.Text, nullJSON(m.Payload), m.At, channel,
		m.Topic, m.ReplyTo)
	if err != nil {
		return fmt.Errorf("save chat message: %w", err)
	}
	return nil
}

// Hello asks the agent to introduce themselves, once: their owner has opened
// their chat for the first time. Like the owner's other rows, it wakes them.
// False if it's been asked before.
func (s *Store) Hello(ctx context.Context, at time.Time) (bool, error) {
	tag, err := s.exec(ctx, `
		WITH m AS (INSERT INTO chat_messages (agent_id, id, author, kind, text, at) VALUES ($1, $2, 'owner', $3, '', $4)
			ON CONFLICT DO NOTHING RETURNING agent_id, id),
		woken AS (UPDATE agent_runs SET wake_at = `+WaitingOwner+` WHERE agent_id = $1 AND EXISTS (SELECT 1 FROM m))
		SELECT pg_notify('`+ToAgent+`', agent_id || ':' || id) FROM m`, memory.HelloID, memory.ChatHello, at)
	if err != nil {
		return false, fmt.Errorf("hello: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// Settle ends an ask or a choice, its text now saying how, and tells the app.
func (s *Store) Settle(ctx context.Context, id, text string, at time.Time) error {
	_, err := s.exec(ctx, `
		WITH m AS (UPDATE chat_messages SET text = $3, settled_at = $4 WHERE agent_id = $1 AND id = $2 RETURNING agent_id, id)
		SELECT pg_notify('`+FromAgent+`', agent_id || ':' || id) FROM m`, id, text, at)
	if err != nil {
		return fmt.Errorf("settle chat message: %w", err)
	}
	return nil
}

// Draft tells the app what the agent is writing, all of it so far. Nothing is
// kept: the reply's row replaces it.
func (s *Store) Draft(ctx context.Context, text string) error {
	if len(text) > draftAtMost {
		return nil
	}
	if _, err := s.exec(ctx, `SELECT pg_notify('`+Drafting+`', $1 || ':' || $2)`, text); err != nil {
		return fmt.Errorf("draft: %w", err)
	}
	return nil
}

// unheard is the owner's rows the agent hasn't perceived yet: one is heard
// once the mind has saved it as a percept. $1 is the agent.
const unheard = `FROM chat_messages c WHERE c.agent_id = $1 AND c.author = 'owner' AND NOT EXISTS (
	SELECT 1 FROM percepts p WHERE p.agent_id = $1 AND p.source = '` + memory.ChatSource + `' AND p.external_id = c.id)`

const chatColumns = `id, author, kind, text, payload, at, settled_at, coalesce(topic_id, '') AS topic_id, coalesce(reply_to, '') AS reply_to`

// Unheard is the owner's rows the agent hasn't perceived yet, oldest first.
func (s *Store) Unheard(ctx context.Context, limit int) ([]memory.ChatMessage, error) {
	return s.queryChat(ctx, `SELECT `+chatColumns+` `+unheard+` ORDER BY id LIMIT $2`, limit)
}

// Chat is the newest limit rows before before (all of them if ""), both sides,
// oldest first.
func (s *Store) Chat(ctx context.Context, before string, limit int) ([]memory.ChatMessage, error) {
	return s.queryChat(ctx, `
		SELECT `+chatColumns+` FROM (
			SELECT `+chatColumns+` FROM chat_messages WHERE agent_id = $1 AND ($2 = '' OR id < $2) ORDER BY id DESC LIMIT $3
		) newest ORDER BY id`, before, limit)
}

// ChatInTopic is like Chat, for topic's rows only.
func (s *Store) ChatInTopic(ctx context.Context, topic, before string, limit int) ([]memory.ChatMessage, error) {
	return s.queryChat(ctx, `
		SELECT `+chatColumns+` FROM (
			SELECT `+chatColumns+` FROM chat_messages WHERE agent_id = $1 AND topic_id = $2 AND ($3 = '' OR id < $3) ORDER BY id DESC LIMIT $4
		) newest ORDER BY id`, topic, before, limit)
}

// TopicThings is the files and asks in the topics, newest first, at most limit.
func (s *Store) TopicThings(ctx context.Context, topics []string, limit int) ([]memory.ChatMessage, error) {
	return s.queryChat(ctx, `SELECT `+chatColumns+` FROM chat_messages
		WHERE agent_id = $1 AND topic_id = ANY($2) AND kind IN ($3, $4, $5) ORDER BY id DESC LIMIT $6`,
		nonNil(topics), memory.ChatFile, memory.ChatAsk, memory.ChatChoice, limit)
}

// ChatAfter is the rows after after, oldest first: what a stream missed.
func (s *Store) ChatAfter(ctx context.Context, after string, limit int) ([]memory.ChatMessage, error) {
	return s.queryChat(ctx, `SELECT `+chatColumns+` FROM chat_messages WHERE agent_id = $1 AND id > $2 ORDER BY id LIMIT $3`, after, limit)
}

// ChatMessage is one row; false if there's none.
func (s *Store) ChatMessage(ctx context.Context, id string) (memory.ChatMessage, bool, error) {
	ms, err := s.queryChat(ctx, `SELECT `+chatColumns+` FROM chat_messages WHERE agent_id = $1 AND id = $2`, id)
	if err != nil || len(ms) == 0 {
		return memory.ChatMessage{}, false, err
	}
	return ms[0], true, nil
}

func (s *Store) queryChat(ctx context.Context, sql string, args ...any) ([]memory.ChatMessage, error) {
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("load chat: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (memory.ChatMessage, error) {
		var m memory.ChatMessage
		var payload []byte
		var settled *time.Time
		err := r.Scan(&m.ID, &m.Author, &m.Kind, &m.Text, &payload, &m.At, &settled, &m.Topic, &m.ReplyTo)
		m.Payload = payload
		if settled != nil {
			m.SettledAt = *settled
		}
		return m, err
	})
}

// SaveFile keeps a file the agent sends in their chat.
func (s *Store) SaveFile(ctx context.Context, f memory.SentFile) error {
	if _, err := s.exec(ctx, `INSERT INTO chat_files (agent_id, id, name, content_type, data, at) VALUES ($1, $2, $3, $4, $5, $6)`,
		f.ID, f.Name, f.ContentType, f.Data, f.At); err != nil {
		return fmt.Errorf("save file: %w", err)
	}
	return nil
}

// File is a file the agent sent; false if there's none.
func (s *Store) File(ctx context.Context, id string) (memory.SentFile, bool, error) {
	f := memory.SentFile{ID: id}
	err := s.queryRow(ctx, `SELECT name, content_type, data, at FROM chat_files WHERE agent_id = $1 AND id = $2`, id).
		Scan(&f.Name, &f.ContentType, &f.Data, &f.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, false, nil
	}
	if err != nil {
		return f, false, fmt.Errorf("load file: %w", err)
	}
	return f, true, nil
}

// Rest lets the agent go until until, keeping kept for when they wake, if
// worker still holds them (false if not); at once, first in line, instead if
// their owner wrote something they haven't heard. Their row is locked first, so the check for
// unheard messages, in a statement of its own, sees one being saved meanwhile;
// one saved just after finds them resting and wakes them (SaveChat).
func (s *Store) Rest(ctx context.Context, worker string, until time.Time, kept []byte) (bool, error) {
	tx, err := s.begin(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("rest: %w", err)
	}
	defer tx.Rollback(ctx)
	var held bool
	err = tx.QueryRow(ctx, `SELECT true FROM agent_runs WHERE agent_id = $1 AND worker = $2 FOR UPDATE`, s.agent, worker).Scan(&held)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("rest: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE agent_runs SET worker = NULL, lease_until = NULL, kept = $3,
		wake_at = CASE WHEN EXISTS (SELECT 1 `+unheard+`) THEN `+WaitingOwner+` ELSE $2 END WHERE agent_id = $1`, s.agent, until, kept); err != nil {
		return false, fmt.Errorf("rest: %w", err)
	}
	return true, tx.Commit(ctx)
}

// NextDue is the soonest something in the agent's memory needs them, and
// false if nothing does: a pending intention (a promise lead early, to be
// ready), a request or a call to their owner expiring, a request waiting to be
// sent (now, if canAsk), an approved action (now), or a topic to sum up,
// sumAfter after it was last talked in (never, if 0).
func (s *Store) NextDue(ctx context.Context, now time.Time, promiseLead, askExpires time.Duration, canAsk bool,
	sumAfter time.Duration) (time.Time, bool, error) {
	var due *time.Time
	err := s.queryRow(ctx, `SELECT least(
		(SELECT min(CASE WHEN kind = $5 THEN due_at - make_interval(secs => $3) ELSE due_at END)
			FROM intentions WHERE agent_id = $1 AND status = $6),
		(SELECT min(CASE WHEN sent_at IS NULL AND $8 THEN $2 ELSE asked_at + make_interval(secs => $4) END)
			FROM approvals WHERE agent_id = $1 AND status = $7),
		(SELECT min(asked_at + make_interval(secs => $4)) FROM judgment_calls WHERE agent_id = $1 AND status = $9),
		(SELECT $2::timestamptz FROM actions WHERE agent_id = $1 AND status = $10 LIMIT 1),
		(SELECT min(last_at + make_interval(secs => $11)) FROM topics
			WHERE agent_id = $1 AND $11 > 0 AND last_at > coalesce(summed_at, '-infinity')))`,
		now, promiseLead.Seconds(), askExpires.Seconds(), memory.IntentionTask, memory.IntentionPending, memory.ApprovalPending,
		canAsk, memory.CallAsked, memory.ActionApproved, sumAfter.Seconds()).Scan(&due)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("next due: %w", err)
	}
	if due == nil {
		return time.Time{}, false, nil
	}
	return *due, true, nil
}

// Waiting is how many of the agent's requests and calls in the chat their
// owner hasn't answered.
func (s *Store) Waiting(ctx context.Context) (int, error) {
	var n int
	err := s.queryRow(ctx, `SELECT count(*) FROM chat_messages WHERE agent_id = $1 AND kind IN ($2, $3) AND settled_at IS NULL`,
		memory.ChatAsk, memory.ChatChoice).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count waiting: %w", err)
	}
	return n, nil
}

// Unread is how many rows the agent has added to the chat, of the kinds
// shown, after the row after.
func (s *Store) Unread(ctx context.Context, after string) (int, error) {
	var n int
	err := s.queryRow(ctx, `SELECT count(*) FROM chat_messages WHERE agent_id = $1 AND author = $2 AND kind IN ($3, $4, $5, $6) AND id > $7`,
		memory.ChatAgent, memory.ChatMessageKind, memory.ChatAsk, memory.ChatChoice, memory.ChatFile, after).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}
	return n, nil
}

// OwnerMessagesSince is how many messages the owner has written since since.
// Row ids are ULIDs, so the count reads only the newest end of the key.
func (s *Store) OwnerMessagesSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	from := ulid.ULID{}
	if err := from.SetTime(ulid.Timestamp(since)); err != nil {
		return 0, fmt.Errorf("count messages: %w", err)
	}
	err := s.queryRow(ctx, `SELECT count(*) FROM chat_messages WHERE agent_id = $1 AND id >= $2 AND author = $3 AND kind = $4`,
		from.String(), memory.ChatOwner, memory.ChatMessageKind).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count messages: %w", err)
	}
	return n, nil
}
