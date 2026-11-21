package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/Grandbusta/tinio/internal/db"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/memory"
)

const topicColumns = `t.id, t.person, t.title, t.summary, t.status, t.settled, t.state = 'quiet', t.started_at, t.last_at, t.folded_at, t.summed_at`

// Topics is every open topic with person and the quiet ones last talked in
// at or after quietSince, newest first, each with the newest message in it.
func (s *Store) Topics(ctx context.Context, person string, quietSince time.Time, limit int) ([]memory.Topic, error) {
	return s.queryTopics(ctx, `SELECT `+topicColumns+`, coalesce((SELECT p.text FROM percepts p
			WHERE p.agent_id = t.agent_id AND p.topic_id = t.id AND p.text <> '' ORDER BY p.at DESC LIMIT 1), '') FROM topics t
		WHERE t.agent_id = $1 AND t.person = $2 AND (t.state = 'open' OR t.last_at >= $3)
		ORDER BY t.last_at DESC, t.id DESC LIMIT $4`, person, quietSince, limit)
}

// OldTopics is person's quiet topics last talked in before before, newest
// first, with their embeddings: what a message may bring up again, however
// long ago.
func (s *Store) OldTopics(ctx context.Context, person string, before time.Time, limit int) ([]memory.Topic, error) {
	rows, err := s.query(ctx, `SELECT `+topicColumns+`, '', t.embedding, t.embedded_with FROM topics t
		WHERE t.agent_id = $1 AND t.person = $2 AND t.state = 'quiet' AND t.last_at < $3
		ORDER BY t.last_at DESC, t.id DESC LIMIT $4`, person, before, limit)
	if err != nil {
		return nil, fmt.Errorf("load old topics: %w", err)
	}
	return db.CollectRows(rows, func(r db.Scanner) (memory.Topic, error) {
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
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("set topic: %w", err)
	}
	defer tx.Rollback(ctx)
	var source, external string
	err = tx.QueryRow(ctx, `UPDATE percepts SET topic_id = nullif($3, '') WHERE agent_id = $1 AND id = $2 RETURNING source, external_id`,
		s.agent, percept.String(), topic).Scan(&source, &external)
	if errors.Is(err, db.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("set topic: %w", err)
	}
	if err := touchTopic(ctx, tx, s.agent, topic, at); err != nil {
		return fmt.Errorf("set topic: %w", err)
	}
	if source == memory.ChatSource {
		tag, err := tx.Exec(ctx, `UPDATE chat_messages SET topic_id = nullif($3, '') WHERE agent_id = $1 AND id = $2`, s.agent, external, topic)
		if err != nil {
			return fmt.Errorf("set topic: %w", err)
		}
		if tag.RowsAffected() == 1 {
			tx.Notify(FromAgent, s.agent+":"+external)
		}
	}
	return tx.Commit(ctx)
}

// touchTopic has topic ("": none) last talked in at at, and open.
func touchTopic(ctx context.Context, tx *db.Tx, agent, topic string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE topics SET last_at = greatest(last_at, $3), state = 'open' WHERE agent_id = $1 AND id = $2`, agent, topic, at)
	return err
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
		WHERE t.agent_id = $1 AND t.last_at > coalesce(t.summed_at, '') AND t.last_at <= $2
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
	if errors.Is(err, db.ErrNoRows) {
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
	tx, err := s.begin(ctx)
	if err != nil {
		return false, fmt.Errorf("move chat: %w", err)
	}
	defer tx.Rollback(ctx)
	var author string
	var was *string
	err = tx.QueryRow(ctx, `SELECT author, topic_id FROM chat_messages WHERE agent_id = $1 AND id = $2`, s.agent, row).Scan(&author, &was)
	if errors.Is(err, db.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("move chat: %w", err)
	}
	rows, err := tx.Query(ctx, `UPDATE chat_messages AS m SET topic_id = nullif($3, '')
		WHERE m.agent_id = $1 AND (m.id = $2 OR ($4 = 'owner' AND m.author = 'agent' AND m.id > $2 AND m.topic_id IS $5
			AND m.id < coalesce((SELECT min(c.id) FROM chat_messages c WHERE c.agent_id = $1 AND c.author = 'owner'
				AND c.kind = '`+memory.ChatMessageKind+`' AND c.id > $2), '~')))
		RETURNING id`, s.agent, row, topic, author, was)
	if err != nil {
		return false, fmt.Errorf("move chat: %w", err)
	}
	moved, err := db.CollectRows(rows, func(r db.Scanner) (string, error) {
		var id string
		return id, r.Scan(&id)
	})
	if err != nil {
		return false, fmt.Errorf("move chat: %w", err)
	}
	if author == memory.ChatOwner {
		if _, err := tx.Exec(ctx, `UPDATE percepts SET topic_id = nullif($3, '') WHERE agent_id = $1 AND source = $4 AND external_id = $2`,
			s.agent, row, topic, memory.ChatSource); err != nil {
			return false, fmt.Errorf("move chat: %w", err)
		}
	}
	if err := touchTopic(ctx, tx, s.agent, topic, at); err != nil {
		return false, fmt.Errorf("move chat: %w", err)
	}
	for _, id := range moved {
		tx.Notify(FromAgent, s.agent+":"+id)
	}
	return true, tx.Commit(ctx)
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
	tx, err := s.begin(ctx)
	if err != nil {
		return false, fmt.Errorf("merge topic: %w", err)
	}
	defer tx.Rollback(ctx)
	var (
		person, gone   string
		settled, extra []string
		started, last  time.Time
	)
	err = tx.QueryRow(ctx, `DELETE FROM topics WHERE agent_id = $1 AND id = $2 RETURNING person, settled, started_at, last_at`,
		s.agent, from).Scan(&gone, &extra, &started, &last)
	if errors.Is(err, db.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("merge topic: %w", err)
	}
	err = tx.QueryRow(ctx, `SELECT person, settled FROM topics WHERE agent_id = $1 AND id = $2`, s.agent, into).Scan(&person, &settled)
	if errors.Is(err, db.ErrNoRows) || err == nil && person != gone {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("merge topic: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE topics SET settled = $3, started_at = least(started_at, $4), last_at = greatest(last_at, $5),
		state = 'open', folded_at = NULL, summed_at = NULL, embedding = NULL WHERE agent_id = $1 AND id = $2`,
		s.agent, into, append(settled, extra...), started, last); err != nil {
		return false, fmt.Errorf("merge topic: %w", err)
	}
	for _, table := range []string{"percepts", "approvals"} {
		if _, err := tx.Exec(ctx, `UPDATE `+table+` SET topic_id = $3 WHERE agent_id = $1 AND topic_id = $2`, s.agent, from, into); err != nil {
			return false, fmt.Errorf("merge topic: %w", err)
		}
	}
	rows, err := tx.Query(ctx, `UPDATE chat_messages SET topic_id = $3 WHERE agent_id = $1 AND topic_id = $2 RETURNING id`, s.agent, from, into)
	if err != nil {
		return false, fmt.Errorf("merge topic: %w", err)
	}
	moved, err := db.CollectRows(rows, func(r db.Scanner) (string, error) {
		var id string
		return id, r.Scan(&id)
	})
	if err != nil {
		return false, fmt.Errorf("merge topic: %w", err)
	}
	for _, id := range moved {
		tx.Notify(FromAgent, s.agent+":"+id)
	}
	return true, tx.Commit(ctx)
}

func (s *Store) queryTopics(ctx context.Context, sql string, args ...any) ([]memory.Topic, error) {
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("load topics: %w", err)
	}
	return db.CollectRows(rows, func(r db.Scanner) (memory.Topic, error) {
		var t memory.Topic
		err := scanTopic(r, &t)
		return t, err
	})
}

// scanTopic scans topicColumns and the newest message into t, then more.
func scanTopic(r db.Scanner, t *memory.Topic, more ...any) error {
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
