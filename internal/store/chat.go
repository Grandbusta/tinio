package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Grandbusta/tinio/internal/db"
	"strings"
	"time"

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

// WaitingOwner is the wake_at of an agent whose owner is waiting on them (as
// SQL): the earliest there is, so workers claim them before anyone who is only
// due, and even when full (internal/host), so a crowd of those, like a
// morning's check-ins, never holds up a reply.
const WaitingOwner = `'0001-01-01T00:00:00.000000000Z'`

// SaveChat keeps a chat row and, once kept, tells whoever listens for that
// side. The owner's also wakes the agent, first in line, if they're resting.
func (s *Store) SaveChat(ctx context.Context, m memory.ChatMessage) error {
	channel := FromAgent
	if m.Author == memory.ChatOwner {
		channel = ToAgent
	}
	if m.Kind == "" {
		m.Kind = memory.ChatMessageKind
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("save chat message: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO chat_messages (agent_id, id, author, kind, text, payload, at, topic_id, reply_to)
		VALUES ($1, $2, $3, $4, $5, $6, $7, nullif($8, ''), nullif($9, ''))`,
		s.agent, m.ID, m.Author, m.Kind, m.Text, nullJSON(m.Payload), m.At, m.Topic, m.ReplyTo); err != nil {
		return fmt.Errorf("save chat message: %w", err)
	}
	if m.Author == memory.ChatOwner {
		if _, err := tx.Exec(ctx, `UPDATE agent_runs SET wake_at = `+WaitingOwner+` WHERE agent_id = $1`, s.agent); err != nil {
			return fmt.Errorf("save chat message: %w", err)
		}
	}
	tx.Notify(channel, s.agent+":"+m.ID)
	return tx.Commit(ctx)
}

// Hello asks the agent to introduce themselves, once: their owner has opened
// their chat for the first time. Like the owner's other rows, it wakes them.
// False if it's been asked before.
func (s *Store) Hello(ctx context.Context, at time.Time) (bool, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return false, fmt.Errorf("hello: %w", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `INSERT INTO chat_messages (agent_id, id, author, kind, text, at) VALUES ($1, $2, 'owner', $3, '', $4)
		ON CONFLICT DO NOTHING`, s.agent, memory.HelloID, memory.ChatHello, at)
	if err != nil {
		return false, fmt.Errorf("hello: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE agent_runs SET wake_at = `+WaitingOwner+` WHERE agent_id = $1`, s.agent); err != nil {
		return false, fmt.Errorf("hello: %w", err)
	}
	tx.Notify(ToAgent, s.agent+":"+memory.HelloID)
	return true, tx.Commit(ctx)
}

// Settle ends an ask or a choice, its text now saying how, and tells the app.
// Update changes a row's text and payload in place: a coding job's card as it
// goes. The chat is told, and shows it anew.
func (s *Store) Update(ctx context.Context, id, text string, payload json.RawMessage) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("update chat message: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE chat_messages SET text = $3, payload = $4 WHERE agent_id = $1 AND id = $2`,
		s.agent, id, text, nullJSON(payload)); err != nil {
		return fmt.Errorf("update chat message: %w", err)
	}
	tx.Notify(FromAgent, s.agent+":"+id)
	return tx.Commit(ctx)
}

func (s *Store) Settle(ctx context.Context, id, text string, at time.Time) error {
	tag, err := s.exec(ctx, `UPDATE chat_messages SET text = $3, settled_at = $4 WHERE agent_id = $1 AND id = $2`, id, text, at)
	if err != nil {
		return fmt.Errorf("settle chat message: %w", err)
	}
	if tag.RowsAffected() == 1 {
		s.db.Notify(FromAgent, s.agent+":"+id)
	}
	return nil
}

// Draft tells the app what the agent is writing, all of it so far. Nothing is
// kept: the reply's row replaces it.
func (s *Store) Draft(_ context.Context, text string) error {
	if s.agent == "" {
		return errUnbound
	}
	s.db.Notify(Drafting, s.agent+":"+text)
	return nil
}

// unheard is the owner's rows the agent hasn't perceived yet: one is heard
// once the mind has saved it as a percept. $1 is the agent.
// A reply to a coding job goes to the job, so the agent never hears it.
const unheard = `FROM chat_messages c WHERE c.agent_id = $1 AND c.author = 'owner' AND c.kind <> '` + memory.ChatJobReply + `' AND NOT EXISTS (
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
		WHERE agent_id = $1 AND topic_id IN (SELECT value FROM json_each($2)) AND kind IN ($3, $4, $5) ORDER BY id DESC LIMIT $6`,
		nonNil(topics), memory.ChatFile, memory.ChatAsk, memory.ChatChoice, limit)
}

// SearchChat is the rows shown in the chat whose words (or file's name)
// have q in them, any case, newest first, at most limit.
func (s *Store) SearchChat(ctx context.Context, q string, limit int) ([]memory.ChatMessage, error) {
	like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
	return s.queryChat(ctx, `SELECT `+chatColumns+` FROM chat_messages
		WHERE agent_id = $1 AND kind IN ($2, $3, $4, $5)
		  AND (lower(text) LIKE lower($6) ESCAPE '\' OR lower(json_extract(payload, '$.name')) LIKE lower($6) ESCAPE '\')
		ORDER BY id DESC LIMIT $7`, memory.ChatMessageKind, memory.ChatAsk, memory.ChatChoice, memory.ChatFile, like, limit)
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
	return db.CollectRows(rows, func(r db.Scanner) (memory.ChatMessage, error) {
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
	if errors.Is(err, db.ErrNoRows) {
		return f, false, nil
	}
	if err != nil {
		return f, false, fmt.Errorf("load file: %w", err)
	}
	return f, true, nil
}

// Rest lets the agent go until until, keeping kept for when they wake, if
// worker still holds them (false if not); at once, first in line, instead if
// their owner wrote something they haven't heard. It holds the database's
// write lock throughout (an immediate transaction), so a message saved just
// after finds them resting and wakes them (SaveChat).
func (s *Store) Rest(ctx context.Context, worker string, until time.Time, kept []byte) (bool, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return false, fmt.Errorf("rest: %w", err)
	}
	defer tx.Rollback(ctx)
	var held bool
	err = tx.QueryRow(ctx, `SELECT true FROM agent_runs WHERE agent_id = $1 AND worker = $2`, s.agent, worker).Scan(&held)
	if errors.Is(err, db.ErrNoRows) {
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
		(SELECT min(CASE WHEN kind = $5 THEN at_plus(due_at, -$3) ELSE due_at END)
			FROM intentions WHERE agent_id = $1 AND status = $6),
		(SELECT min(CASE WHEN sent_at IS NULL AND $8 THEN $2 ELSE at_plus(asked_at, $4) END)
			FROM approvals WHERE agent_id = $1 AND status = $7),
		(SELECT min(at_plus(asked_at, $4)) FROM judgment_calls WHERE agent_id = $1 AND status = $9),
		(SELECT $2 FROM actions WHERE agent_id = $1 AND status = $10 LIMIT 1),
		(SELECT min(at_plus(last_at, $11)) FROM topics
			WHERE agent_id = $1 AND $11 > 0 AND last_at > coalesce(summed_at, '')))`,
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
