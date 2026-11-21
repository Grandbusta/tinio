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

const topicColumns = `t.id, t.person, t.title, t.summary, t.status, t.settled, t.state = 'quiet', t.started_at, t.last_at, t.folded_at, t.summed_at`

// Topics is every open topic with person and the quiet ones last talked in
// at or after quietSince, newest first, each with the newest message in it.
func (s *Store) Topics(ctx context.Context, person string, quietSince time.Time, limit int) ([]memory.Topic, error) {
	return s.queryTopics(ctx, `SELECT `+topicColumns+`, coalesce(said.text, '') FROM topics t
		LEFT JOIN LATERAL (
			SELECT p.text FROM percepts p WHERE p.agent_id = t.agent_id AND p.topic_id = t.id AND p.text <> ''
			ORDER BY p.at DESC LIMIT 1
		) said ON true
		WHERE t.agent_id = $1 AND t.person = $2 AND (t.state = 'open' OR t.last_at >= $3)
		ORDER BY t.last_at DESC, t.id DESC LIMIT $4`, person, quietSince, limit)
}

// OldTopics is person's quiet topics last talked in before before, newest
// first, with their embeddings: what a message may bring up again, however
// long ago.
func (s *Store) OldTopics(ctx context.Context, person string, before time.Time, limit int) ([]memory.Topic, error) {
	rows, err := s.query(ctx, `SELECT `+topicColumns+`, '', coalesce(t.embedding, '{}'), t.embedded_with FROM topics t
		WHERE t.agent_id = $1 AND t.person = $2 AND t.state = 'quiet' AND t.last_at < $3
		ORDER BY t.last_at DESC, t.id DESC LIMIT $4`, person, before, limit)
	if err != nil {
		return nil, fmt.Errorf("load old topics: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (memory.Topic, error) {
		var t memory.Topic
		err := scanTopic(r, &t, &t.Embedding, &t.EmbeddedWith)
		return t, err
	})
}

func (s *Store) SaveTopicEmbedding(ctx context.Context, id string, v []float32, model string) error {
	if _, err := s.exec(ctx, `UPDATE topics SET embedding = $3, embedded_with = $4 WHERE agent_id = $1 AND id = $2`, id, v, model); err != nil {
		return fmt.Errorf("save topic embedding: %w", err)
	}
	return nil
}

// Topic is one topic; false if there's none.
func (s *Store) Topic(ctx context.Context, id string) (memory.Topic, bool, error) {
	ts, err := s.queryTopics(ctx, `SELECT `+topicColumns+`, '' FROM topics t WHERE t.agent_id = $1 AND t.id = $2`, id)
	if err != nil || len(ts) == 0 {
		return memory.Topic{}, false, err
	}
	return ts[0], true, nil
}

func (s *Store) StartTopic(ctx context.Context, t memory.Topic) error {
	if _, err := s.exec(ctx, `INSERT INTO topics (agent_id, id, person, title, started_at, last_at) VALUES ($1, $2, $3, $4, $5, $5)`,
		t.ID, t.Person, t.Title, t.StartedAt); err != nil {
		return fmt.Errorf("start topic: %w", err)
	}
	return nil
}

// SetTopic puts a percept in topic ("": none), and its chat row with it,
// which the app is told of. The topic was last talked in at at, and is open.
func (s *Store) SetTopic(ctx context.Context, percept ulid.ULID, topic string, at time.Time) error {
	_, err := s.exec(ctx, `
		WITH p AS (UPDATE percepts SET topic_id = nullif($3, '') WHERE agent_id = $1 AND id = $2 RETURNING source, external_id),
		t AS (UPDATE topics SET last_at = greatest(last_at, $4), state = 'open' WHERE agent_id = $1 AND id = $3),
		msg AS (UPDATE chat_messages c SET topic_id = nullif($3, '') FROM p
			WHERE c.agent_id = $1 AND p.source = '`+memory.ChatSource+`' AND c.id = p.external_id RETURNING c.agent_id, c.id)
		SELECT pg_notify('`+FromAgent+`', agent_id || ':' || id) FROM msg`, percept.String(), topic, at)
	if err != nil {
		return fmt.Errorf("set topic: %w", err)
	}
	return nil
}

// NameTopic gives a topic its title, unless it has one: the owner's word wins.
func (s *Store) NameTopic(ctx context.Context, id, title string) error {
	if _, err := s.exec(ctx, `UPDATE topics SET title = $3 WHERE agent_id = $1 AND id = $2 AND title = ''`, id, title); err != nil {
		return fmt.Errorf("name topic: %w", err)
	}
	return nil
}

// TopicsToSum is the topics talked in since they were last summed up, and
// not since before (the talk has paused), longest waiting first.
func (s *Store) TopicsToSum(ctx context.Context, before time.Time, limit int) ([]memory.Topic, error) {
	return s.queryTopics(ctx, `SELECT `+topicColumns+`, '' FROM topics t
		WHERE t.agent_id = $1 AND t.last_at > coalesce(t.summed_at, '-infinity') AND t.last_at <= $2
		ORDER BY t.last_at, t.id LIMIT $3`, before, limit)
}

// SumTopic keeps a topic's summary, status and settled, folded up to FoldedAt
// and written from what was said up to SummedAt; its title too, if it has
// none, and quiet if it's finished. Its embedding is of the old summary, so
// it goes.
func (s *Store) SumTopic(ctx context.Context, t memory.Topic) error {
	if _, err := s.exec(ctx, `UPDATE topics SET title = CASE WHEN title = '' THEN $3 ELSE title END,
		summary = $4, status = $5, settled = $6, folded_at = $7, summed_at = $8, embedding = NULL,
		state = CASE WHEN $9 AND last_at <= $8 THEN 'quiet' ELSE state END
		WHERE agent_id = $1 AND id = $2`,
		t.ID, t.Title, t.Summary, t.Status, nonNil(t.Settled), nullTime(t.FoldedAt), t.SummedAt, t.Quiet); err != nil {
		return fmt.Errorf("sum topic: %w", err)
	}
	return nil
}

// QuietTopics makes the open topics last talked in before before quiet.
func (s *Store) QuietTopics(ctx context.Context, before time.Time) error {
	if _, err := s.exec(ctx, `UPDATE topics SET state = 'quiet' WHERE agent_id = $1 AND state = 'open' AND last_at < $2`, before); err != nil {
		return fmt.Errorf("quiet topics: %w", err)
	}
	return nil
}

// PerceptTopic is the topic a percept is in; "" for none, or if there's no
// such percept.
func (s *Store) PerceptTopic(ctx context.Context, id ulid.ULID) (string, error) {
	var topic string
	err := s.queryRow(ctx, `SELECT coalesce(topic_id, '') FROM percepts WHERE agent_id = $1 AND id = $2`, id.String()).Scan(&topic)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("percept topic: %w", err)
	}
	return topic, nil
}

// MoveChat puts a chat row in topic ("": none) and tells the app. The
// owner's message moves with the percept it was heard as, and with the
// agent's rows after it that answered it (until the owner's next message,
// in the topic it was in). False if there's no such row.
func (s *Store) MoveChat(ctx context.Context, row, topic string, at time.Time) (bool, error) {
	tag, err := s.exec(ctx, `
		WITH was AS (SELECT id, author, topic_id FROM chat_messages WHERE agent_id = $1 AND id = $2),
		later AS (SELECT min(c.id) AS id FROM chat_messages c, was
			WHERE c.agent_id = $1 AND c.author = 'owner' AND c.kind = '`+memory.ChatMessageKind+`' AND c.id > was.id),
		moved AS (UPDATE chat_messages m SET topic_id = nullif($3, '') FROM was, later
			WHERE m.agent_id = $1 AND (m.id = was.id OR (was.author = 'owner' AND m.author = 'agent' AND m.id > was.id
				AND (later.id IS NULL OR m.id < later.id) AND m.topic_id IS NOT DISTINCT FROM was.topic_id))
			RETURNING m.agent_id, m.id),
		p AS (UPDATE percepts SET topic_id = nullif($3, '') FROM was
			WHERE percepts.agent_id = $1 AND was.author = 'owner' AND source = '`+memory.ChatSource+`' AND external_id = was.id),
		t AS (UPDATE topics SET last_at = greatest(last_at, $4), state = 'open' WHERE agent_id = $1 AND id = $3)
		SELECT pg_notify('`+FromAgent+`', agent_id || ':' || id) FROM moved`, row, topic, at)
	if err != nil {
		return false, fmt.Errorf("move chat: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// RenameTopic gives a topic the owner's title. False if there's none.
func (s *Store) RenameTopic(ctx context.Context, id, title string) (bool, error) {
	tag, err := s.exec(ctx, `UPDATE topics SET title = $3 WHERE agent_id = $1 AND id = $2`, id, title)
	if err != nil {
		return false, fmt.Errorf("rename topic: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// QuietTopic is the owner closing a topic. False if there's none.
func (s *Store) QuietTopic(ctx context.Context, id string) (bool, error) {
	tag, err := s.exec(ctx, `UPDATE topics SET state = 'quiet' WHERE agent_id = $1 AND id = $2`, id)
	if err != nil {
		return false, fmt.Errorf("close topic: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// MergeTopic moves everything in from into into, and drops from. Into keeps
// its title; what from settled is added to it, and it's summed up again from
// the start. False if either isn't there, or they're with different people.
func (s *Store) MergeTopic(ctx context.Context, from, into string) (bool, error) {
	if from == into {
		return false, nil
	}
	tx, err := s.begin(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("merge topic: %w", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `WITH f AS (DELETE FROM topics WHERE agent_id = $1 AND id = $2 RETURNING person, settled, started_at, last_at)
		UPDATE topics t SET settled = t.settled || f.settled, started_at = least(t.started_at, f.started_at),
			last_at = greatest(t.last_at, f.last_at), state = 'open', folded_at = NULL, summed_at = NULL, embedding = NULL
		FROM f WHERE t.agent_id = $1 AND t.id = $3 AND t.person = f.person`, s.agent, from, into)
	if err != nil {
		return false, fmt.Errorf("merge topic: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	for _, table := range []string{"percepts", "approvals"} {
		if _, err := tx.Exec(ctx, `UPDATE `+table+` SET topic_id = $3 WHERE agent_id = $1 AND topic_id = $2`, s.agent, from, into); err != nil {
			return false, fmt.Errorf("merge topic: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `WITH c AS (UPDATE chat_messages SET topic_id = $3 WHERE agent_id = $1 AND topic_id = $2 RETURNING agent_id, id)
		SELECT pg_notify('`+FromAgent+`', agent_id || ':' || id) FROM c`, s.agent, from, into); err != nil {
		return false, fmt.Errorf("merge topic: %w", err)
	}
	return true, tx.Commit(ctx)
}

func (s *Store) queryTopics(ctx context.Context, sql string, args ...any) ([]memory.Topic, error) {
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("load topics: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (memory.Topic, error) {
		var t memory.Topic
		err := scanTopic(r, &t)
		return t, err
	})
}

// scanTopic scans topicColumns and the newest message into t, then more.
func scanTopic(r pgx.CollectableRow, t *memory.Topic, more ...any) error {
	var folded, summed *time.Time
	err := r.Scan(append([]any{&t.ID, &t.Person, &t.Title, &t.Summary, &t.Status, &t.Settled, &t.Quiet, &t.StartedAt, &t.LastAt,
		&folded, &summed, &t.LastSaid}, more...)...)
	if folded != nil {
		t.FoldedAt = *folded
	}
	if summed != nil {
		t.SummedAt = *summed
	}
	return err
}
