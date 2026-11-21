// Package store persists percepts, decisions and memory in PostgreSQL.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/search"
)

// Store is one agent's memory in the platform's database: every row it reads
// or writes is that agent's (sources excepted, which every agent shares).
type Store struct {
	pool  *pgxpool.Pool
	agent string
}

// New is unbound: it touches no agent's rows until bound with For.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) For(agent string) *Store { return &Store{pool: s.pool, agent: agent} }

var errUnbound = errors.New("store: not bound to an agent")

// exec, query, queryRow and begin are the only ways to the database. The first
// three pass the agent as $1, so a query's own arguments start at $2; in a
// transaction from begin, pass s.agent as $1 the same way. Each says first
// which agent it's for (app.agent_id), which the tables' row-level security
// reads: a query that forgets its agent_id filter still sees only this agent's
// rows. exec, query and queryRow send that and the query together, in one round
// trip: a batch is one implicit transaction, so the setting holds for the query
// and ends with it, and each query stands alone, as a failed one in a shared
// transaction would spoil the rest.
func (s *Store) exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	br, err := s.send(ctx, sql, args)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	tag, err := br.Exec()
	return tag, errors.Join(err, br.Close())
}

func (s *Store) query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	br, err := s.send(ctx, sql, args)
	if err != nil {
		return nil, err
	}
	rows, err := br.Query()
	if err != nil {
		br.Close()
		return nil, err
	}
	return batchRows{Rows: rows, br: br}, nil
}

func (s *Store) queryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return batchRow{s: s, ctx: ctx, sql: sql, args: args}
}

const setAgent = `SELECT set_config('app.agent_id', $1, true)`

// send queues the agent's setting and the query, and reads the setting's
// result: what's left is the query's.
func (s *Store) send(ctx context.Context, sql string, args []any) (pgx.BatchResults, error) {
	if s.agent == "" {
		return nil, errUnbound
	}
	b := &pgx.Batch{}
	b.Queue(setAgent, s.agent)
	b.Queue(sql, append([]any{s.agent}, args...)...)
	br := s.pool.SendBatch(ctx, b)
	if _, err := br.Exec(); err != nil {
		br.Close()
		return nil, err
	}
	return br, nil
}

func (s *Store) begin(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	if s.agent == "" {
		return nil, errUnbound
	}
	tx, err := s.pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, setAgent, s.agent); err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

// batchRows ends its batch when the rows are closed.
type batchRows struct {
	pgx.Rows
	br pgx.BatchResults
}

func (r batchRows) Close() {
	r.Rows.Close()
	r.br.Close()
}

// batchRow sends its query when it's scanned.
type batchRow struct {
	s    *Store
	ctx  context.Context
	sql  string
	args []any
}

func (r batchRow) Scan(dest ...any) error {
	br, err := r.s.send(r.ctx, r.sql, r.args)
	if err != nil {
		return err
	}
	return errors.Join(br.QueryRow().Scan(dest...), br.Close())
}

// SavePercept reports whether p was new: one already seen from the same source
// (same ExternalID) is not stored again. A new one in a topic is its newest word.
func (s *Store) SavePercept(ctx context.Context, p perception.Percept) (bool, error) {
	tag, err := s.exec(ctx, `
		WITH p AS (INSERT INTO percepts (agent_id, id, source, external_id, thread, speaker, kind, text, payload, at, topic_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, nullif($11, ''))
			ON CONFLICT (agent_id, source, external_id) DO NOTHING RETURNING topic_id, at),
		t AS (UPDATE topics SET last_at = greatest(last_at, p.at), state = 'open' FROM p WHERE topics.agent_id = $1 AND topics.id = p.topic_id)
		SELECT 1 FROM p`,
		p.ID.String(), p.Source, p.ExternalID, p.Thread, p.Speaker, p.Kind, p.Text, nullJSON(p.Payload), p.At, p.Topic)
	if err != nil {
		return false, fmt.Errorf("save percept: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) SaveDecision(ctx context.Context, d decision.Decision) error {
	answers, err := json.Marshal(d.Answers)
	if err != nil {
		return err
	}
	if d.Answers == nil {
		answers = []byte("{}")
	}
	var outcome []byte
	if d.Outcome != nil {
		if outcome, err = json.Marshal(d.Outcome); err != nil {
			return err
		}
	}
	var parent *string
	if d.ParentID != nil {
		id := d.ParentID.String()
		parent = &id
	}
	// What it cost is counted against its account's week and hour in the same
	// statement, so the meter never misses one (a decision from an earlier week
	// or hour than the counter's is left out of it).
	_, err = s.exec(ctx, `
		WITH d AS (
			INSERT INTO decisions (agent_id, id, parent_id, at, percept_id, set_id, tier, provider, answers,
			                       action, reply, gate, latency_ms, cost_usd, outcome, affect)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
			RETURNING agent_id, at, cost_usd)
		INSERT INTO account_usage AS u (account_id, week_start, week_usd, hour_start, hour_usd)
		SELECT g.account_id, a.created_at + floor(extract(epoch FROM d.at - a.created_at) / 604800) * interval '7 days',
		       d.cost_usd, date_trunc('hour', d.at), d.cost_usd
		FROM d JOIN agents g ON g.id = d.agent_id JOIN accounts a ON a.id = g.account_id
		WHERE d.cost_usd > 0
		ON CONFLICT (account_id) DO UPDATE SET
			week_usd = CASE WHEN u.week_start = excluded.week_start THEN u.week_usd + excluded.week_usd
			                WHEN u.week_start < excluded.week_start THEN excluded.week_usd ELSE u.week_usd END,
			week_start = greatest(u.week_start, excluded.week_start),
			hour_usd = CASE WHEN u.hour_start = excluded.hour_start THEN u.hour_usd + excluded.hour_usd
			                WHEN u.hour_start < excluded.hour_start THEN excluded.hour_usd ELSE u.hour_usd END,
			hour_start = greatest(u.hour_start, excluded.hour_start)`,
		d.ID.String(), parent, d.At, nullULID(d.PerceptID), d.SetID, string(d.Tier), d.Provider,
		answers, d.Action, nullString(d.Reply), string(d.Gate), float64(d.Latency)/float64(time.Millisecond),
		d.CostUSD, nullJSON(outcome), nullJSON(d.Affect))
	if err != nil {
		return fmt.Errorf("save decision: %w", err)
	}
	return nil
}

func (s *Store) RecentDecisions(ctx context.Context, n int) ([]decision.Decision, error) {
	rows, err := s.query(ctx, `SELECT `+decisionColumns+` FROM decisions d WHERE d.agent_id = $1 ORDER BY d.at DESC LIMIT $2`, n)
	if err != nil {
		return nil, fmt.Errorf("query decisions: %w", err)
	}
	defer rows.Close()
	var out []decision.Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

const decisionColumns = `d.id, d.parent_id, d.at, d.percept_id, d.set_id, d.tier, d.provider, d.answers,
	d.action, coalesce(d.reply, ''), d.gate, d.latency_ms, d.cost_usd::float8, d.outcome, d.affect`

// scanDecision reads decisionColumns, then into extra any columns after them.
func scanDecision(row pgx.Row, extra ...any) (decision.Decision, error) {
	var (
		d                        decision.Decision
		id                       string
		parentID, perceptID      *string
		tier, gate               string
		answers, outcome, affect []byte
		latencyMS                float64
	)
	dest := append([]any{&id, &parentID, &d.At, &perceptID, &d.SetID, &tier, &d.Provider,
		&answers, &d.Action, &d.Reply, &gate, &latencyMS, &d.CostUSD, &outcome, &affect}, extra...)
	if err := row.Scan(dest...); err != nil {
		return d, err
	}
	var err error
	if d.ID, err = ulid.Parse(id); err != nil {
		return d, err
	}
	if d.PerceptID, err = parseNullULID(perceptID); err != nil { // zero: a decision of the agent's own
		return d, err
	}
	if parentID != nil {
		pid, err := ulid.Parse(*parentID)
		if err != nil {
			return d, err
		}
		d.ParentID = &pid
	}
	d.Tier, d.Gate = decision.Tier(tier), decision.Gate(gate)
	d.Latency = time.Duration(latencyMS * float64(time.Millisecond))
	if err := json.Unmarshal(answers, &d.Answers); err != nil {
		return d, err
	}
	if outcome != nil {
		d.Outcome = &decision.Outcome{}
		if err := json.Unmarshal(outcome, d.Outcome); err != nil {
			return d, err
		}
	}
	d.Affect = affect
	return d, nil
}

// RecentExchanges is oldest first, with what the agent said first (reminders,
// check-ins). The percept being handled is left out.
func (s *Store) RecentExchanges(ctx context.Context, source, thread string, since time.Time,
	exclude ulid.ULID, limit int) ([]memory.Exchange, error) {
	return s.exchanges(ctx, `p.source = $2 AND p.thread = $3 AND p.at >= $4`, exclude, limit, source, thread, since)
}

// TopicExchanges is the newest limit exchanges in topic after after (all if
// zero), from any channel, but exclude; oldest first.
func (s *Store) TopicExchanges(ctx context.Context, topic string, after time.Time, exclude ulid.ULID, limit int) ([]memory.Exchange, error) {
	return s.exchanges(ctx, `p.topic_id = $2 AND p.at > $3`, exclude, limit, topic, after)
}

// exchanges is the newest limit percepts that where picks ($2 on), but
// exclude, each with what was said to it; oldest first.
func (s *Store) exchanges(ctx context.Context, where string, exclude ulid.ULID, limit int, args ...any) ([]memory.Exchange, error) {
	n := len(args) + 2
	rows, err := s.query(ctx, `
		SELECT p.at, p.text, coalesce(r.reply, ''), coalesce(p.topic_id, '')
		FROM percepts p
		LEFT JOIN LATERAL (
			SELECT d.reply FROM decisions d
			WHERE d.agent_id = p.agent_id AND d.percept_id = p.id AND d.reply IS NOT NULL
			ORDER BY d.at DESC LIMIT 1
		) r ON true
		WHERE p.agent_id = $1 AND `+where+fmt.Sprintf(` AND p.id <> $%d
		  AND (p.text <> '' OR p.kind IN ($%d, $%d, $%d, $%d))
		ORDER BY p.at DESC, p.id DESC
		LIMIT $%d`, n, n+2, n+3, n+4, n+5, n+1),
		append(args, exclude.String(), limit, perception.KindReminder, perception.KindCheckIn,
			perception.KindDelivery, perception.KindReviewRequest)...)
	if err != nil {
		return nil, fmt.Errorf("query exchanges: %w", err)
	}
	defer rows.Close()

	var out []memory.Exchange
	for rows.Next() {
		var e memory.Exchange
		if err := rows.Scan(&e.At, &e.Message, &e.Reply, &e.Topic); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.Reverse(out)
	return out, nil
}

func (s *Store) SaveFact(ctx context.Context, f memory.Fact) error {
	_, err := s.exec(ctx, `INSERT INTO facts (agent_id, id, percept_id, subject, text, at, areas) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		f.ID.String(), f.PerceptID.String(), f.Subject, f.Text, f.At, nonNil(f.Areas))
	if err != nil {
		return fmt.Errorf("save fact: %w", err)
	}
	return nil
}

const factColumns = `id, percept_id, subject, text, at, areas`

// RecentFacts returns up to limit facts, newest first.
func (s *Store) RecentFacts(ctx context.Context, limit int) ([]memory.Fact, error) {
	return s.queryFacts(ctx, `SELECT `+factColumns+` FROM facts WHERE agent_id = $1
		ORDER BY at DESC, id DESC LIMIT $2`, limit)
}

// RecallCandidates returns facts sharing an area with areas first, then the
// rest, newest first within each group.
func (s *Store) RecallCandidates(ctx context.Context, subjects, areas []string, limit int) ([]memory.Fact, error) {
	return s.queryFacts(ctx, `SELECT `+factColumns+` FROM facts
		WHERE agent_id = $1 AND subject = ANY($2)
		ORDER BY (areas && $3) DESC, at DESC, id DESC LIMIT $4`, nonNil(subjects), nonNil(areas), limit)
}

// UnlabelledFacts returns facts with no areas yet, oldest first.
func (s *Store) UnlabelledFacts(ctx context.Context, limit int) ([]memory.Fact, error) {
	return s.queryFacts(ctx, `SELECT `+factColumns+` FROM facts
		WHERE agent_id = $1 AND areas = '{}' ORDER BY at, id LIMIT $2`, limit)
}

func (s *Store) SetFactAreas(ctx context.Context, id ulid.ULID, areas []string) error {
	if _, err := s.exec(ctx, `UPDATE facts SET areas = $3 WHERE agent_id = $1 AND id = $2`, id.String(), nonNil(areas)); err != nil {
		return fmt.Errorf("set fact areas: %w", err)
	}
	return nil
}

// UndecidedPublicFacts are about the agent itself, oldest first.
func (s *Store) UndecidedPublicFacts(ctx context.Context, limit int) ([]memory.Fact, error) {
	return s.queryFacts(ctx, `SELECT `+factColumns+` FROM facts
		WHERE agent_id = $1 AND subject = $2 AND public IS NULL ORDER BY at, id LIMIT $3`, memory.SubjectSelf, limit)
}

func (s *Store) SetFactPublic(ctx context.Context, id ulid.ULID, public bool) error {
	if _, err := s.exec(ctx, `UPDATE facts SET public = $3 WHERE agent_id = $1 AND id = $2`, id.String(), public); err != nil {
		return fmt.Errorf("set fact public: %w", err)
	}
	return nil
}

// PublicFacts returns the newest public facts, oldest first. Facts about
// people are never among them.
func (s *Store) PublicFacts(ctx context.Context, limit int) ([]memory.Fact, error) {
	facts, err := s.queryFacts(ctx, `SELECT `+factColumns+` FROM facts
		WHERE agent_id = $1 AND subject = $2 AND public ORDER BY at DESC, id DESC LIMIT $3`, memory.SubjectSelf, limit)
	slices.Reverse(facts)
	return facts, err
}

// Profile returns the page as JSON and a hash of what it was written from; ok
// is false if it never has been.
func (s *Store) Profile(ctx context.Context) (page []byte, inputs string, at time.Time, ok bool, err error) {
	err = s.queryRow(ctx, `SELECT page, inputs, written_at FROM profile WHERE agent_id = $1`).Scan(&page, &inputs, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", time.Time{}, false, nil
	}
	if err != nil {
		return nil, "", time.Time{}, false, fmt.Errorf("load profile: %w", err)
	}
	return page, inputs, at, true, nil
}

func (s *Store) SaveProfile(ctx context.Context, page []byte, inputs string, at time.Time) error {
	_, err := s.exec(ctx, `
		INSERT INTO profile (agent_id, page, inputs, written_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (agent_id) DO UPDATE SET page = EXCLUDED.page, inputs = EXCLUDED.inputs, written_at = EXCLUDED.written_at`,
		string(page), inputs, at)
	if err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	return nil
}

func (s *Store) queryFacts(ctx context.Context, sql string, args ...any) ([]memory.Fact, error) {
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query facts: %w", err)
	}
	defer rows.Close()

	var out []memory.Fact
	for rows.Next() {
		var (
			f             memory.Fact
			id, perceptID string
		)
		if err := rows.Scan(&id, &perceptID, &f.Subject, &f.Text, &f.At, &f.Areas); err != nil {
			return nil, err
		}
		if f.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		if f.PerceptID, err = ulid.Parse(perceptID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

const intentionColumns = `id, subject, kind, percept_id, project_id, text, ability, args, due_at, status,
	coalesce(result, ''), created_at, done_at, attempts`

func (s *Store) SaveIntention(ctx context.Context, i memory.Intention) error {
	args, err := json.Marshal(orEmpty(i.Args))
	if err != nil {
		return fmt.Errorf("save intention: %w", err)
	}
	_, err = s.exec(ctx, `
		INSERT INTO intentions (agent_id, id, subject, kind, percept_id, project_id, text, ability, args, due_at, status,
		                        result, created_at, done_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		i.ID.String(), i.Subject, i.Kind, nullULID(i.PerceptID), nullULID(i.ProjectID), i.Text, i.Ability, args,
		i.DueAt, i.Status, nullString(i.Result), i.CreatedAt, nullTime(i.DoneAt))
	if err != nil {
		return fmt.Errorf("save intention: %w", err)
	}
	return nil
}

// DueIntentions returns pending intentions due by now, earliest first.
func (s *Store) DueIntentions(ctx context.Context, now time.Time, limit int) ([]memory.Intention, error) {
	return s.queryIntentions(ctx, `SELECT `+intentionColumns+` FROM intentions
		WHERE agent_id = $1 AND status = $2 AND due_at <= $3 ORDER BY due_at, id LIMIT $4`, memory.IntentionPending, now, limit)
}

// IntentionsDue returns those due in [from, to), whatever their status, earliest first.
func (s *Store) IntentionsDue(ctx context.Context, from, to time.Time) ([]memory.Intention, error) {
	return s.queryIntentions(ctx, `SELECT `+intentionColumns+` FROM intentions
		WHERE agent_id = $1 AND due_at >= $2 AND due_at < $3 ORDER BY due_at, id`, from, to)
}

func (s *Store) Intention(ctx context.Context, id ulid.ULID) (memory.Intention, bool, error) {
	is, err := s.queryIntentions(ctx, `SELECT `+intentionColumns+` FROM intentions WHERE agent_id = $1 AND id = $2`, id.String())
	if err != nil || len(is) == 0 {
		return memory.Intention{}, false, err
	}
	return is[0], true, nil
}

// OpenIntentions returns the pending ones, earliest due first.
func (s *Store) OpenIntentions(ctx context.Context, subject, kind string) ([]memory.Intention, error) {
	return s.queryIntentions(ctx, `SELECT `+intentionColumns+` FROM intentions
		WHERE agent_id = $1 AND subject = $2 AND kind = $3 AND status = $4 ORDER BY due_at, id`, subject, kind, memory.IntentionPending)
}

func (s *Store) FinishIntention(ctx context.Context, id ulid.ULID, status, result string, at time.Time) error {
	_, err := s.exec(ctx, `UPDATE intentions SET status = $3, result = $4, done_at = $5 WHERE agent_id = $1 AND id = $2`,
		id.String(), status, nullString(result), at)
	if err != nil {
		return fmt.Errorf("finish intention: %w", err)
	}
	return nil
}

func orEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func (s *Store) AwaitIntention(ctx context.Context, id ulid.ULID) error {
	if _, err := s.exec(ctx, `UPDATE intentions SET status = $3 WHERE agent_id = $1 AND id = $2 AND status = $4`,
		id.String(), memory.IntentionAwaiting, memory.IntentionPending); err != nil {
		return fmt.Errorf("await intention: %w", err)
	}
	return nil
}

// ResumeIntention makes an intention that was waiting for approval pending
// again, due when it was or at notBefore, whichever is later.
func (s *Store) ResumeIntention(ctx context.Context, id ulid.ULID, notBefore time.Time) error {
	if _, err := s.exec(ctx, `UPDATE intentions SET status = $3, due_at = GREATEST(due_at, $4) WHERE agent_id = $1 AND id = $2 AND status = $5`,
		id.String(), memory.IntentionPending, notBefore, memory.IntentionAwaiting); err != nil {
		return fmt.Errorf("resume intention: %w", err)
	}
	return nil
}

// RetryIntention puts a failed intention off until due, counting the attempt.
func (s *Store) RetryIntention(ctx context.Context, id ulid.ULID, due time.Time) error {
	if _, err := s.exec(ctx, `UPDATE intentions SET due_at = $3, attempts = attempts + 1 WHERE agent_id = $1 AND id = $2`,
		id.String(), due); err != nil {
		return fmt.Errorf("retry intention: %w", err)
	}
	return nil
}

func (s *Store) PostponeIntention(ctx context.Context, id ulid.ULID, due time.Time) error {
	if _, err := s.exec(ctx, `UPDATE intentions SET due_at = $3 WHERE agent_id = $1 AND id = $2`, id.String(), due); err != nil {
		return fmt.Errorf("postpone intention: %w", err)
	}
	return nil
}

func (s *Store) queryIntentions(ctx context.Context, sql string, args ...any) ([]memory.Intention, error) {
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query intentions: %w", err)
	}
	defer rows.Close()
	var out []memory.Intention
	for rows.Next() {
		var (
			i                    memory.Intention
			id                   string
			perceptID, projectID *string
			doneAt               *time.Time
			args                 []byte
		)
		if err := rows.Scan(&id, &i.Subject, &i.Kind, &perceptID, &projectID, &i.Text, &i.Ability, &args,
			&i.DueAt, &i.Status, &i.Result, &i.CreatedAt, &doneAt, &i.Attempts); err != nil {
			return nil, err
		}
		if i.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		if i.PerceptID, err = parseNullULID(perceptID); err != nil {
			return nil, err
		}
		if i.ProjectID, err = parseNullULID(projectID); err != nil {
			return nil, err
		}
		if doneAt != nil {
			i.DoneAt = *doneAt
		}
		if err := json.Unmarshal(args, &i.Args); err != nil {
			return nil, fmt.Errorf("intention %s args: %w", id, err)
		}
		if len(i.Args) == 0 {
			i.Args = nil
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

const projectSelect = `SELECT p.id, p.title, p.kind, p.status, p.notes, p.source_id, p.position, p.created_at, p.updated_at, p.goal_id,
	s.kind, s.title, s.author, s.chars FROM projects p LEFT JOIN sources s ON s.id = p.source_id
	WHERE p.agent_id = $1`

func (s *Store) SaveProject(ctx context.Context, p memory.Project) error {
	_, err := s.exec(ctx, `
		INSERT INTO projects (agent_id, id, title, kind, status, notes, source_id, position, created_at, updated_at, goal_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (agent_id, id) DO UPDATE SET title = EXCLUDED.title, kind = EXCLUDED.kind, status = EXCLUDED.status,
			notes = EXCLUDED.notes, source_id = EXCLUDED.source_id, position = EXCLUDED.position, updated_at = EXCLUDED.updated_at,
			goal_id = EXCLUDED.goal_id`,
		p.ID.String(), p.Title, p.Kind, p.Status, p.Notes, nullULID(p.SourceID), p.Position, p.CreatedAt, p.UpdatedAt, nullULID(p.GoalID))
	if err != nil {
		return fmt.Errorf("save project: %w", err)
	}
	return nil
}

func (s *Store) Project(ctx context.Context, id ulid.ULID) (memory.Project, bool, error) {
	ps, err := s.queryProjects(ctx, projectSelect+` AND p.id = $2`, id.String())
	if err != nil || len(ps) == 0 {
		return memory.Project{}, false, err
	}
	return ps[0], true, nil
}

// ActiveProjects returns up to limit active projects, most recently worked on first.
func (s *Store) ActiveProjects(ctx context.Context, limit int) ([]memory.Project, error) {
	return s.queryProjects(ctx, projectSelect+` AND p.status = $2 ORDER BY p.updated_at DESC, p.id LIMIT $3`,
		memory.ProjectActive, limit)
}

func (s *Store) queryProjects(ctx context.Context, sql string, args ...any) ([]memory.Project, error) {
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()
	var out []memory.Project
	for rows.Next() {
		var (
			p                      memory.Project
			id                     string
			sourceID, goalID       *string
			sKind, sTitle, sAuthor *string
			sChars                 *int
		)
		if err := rows.Scan(&id, &p.Title, &p.Kind, &p.Status, &p.Notes, &sourceID, &p.Position, &p.CreatedAt, &p.UpdatedAt, &goalID,
			&sKind, &sTitle, &sAuthor, &sChars); err != nil {
			return nil, err
		}
		if p.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		if p.SourceID, err = parseNullULID(sourceID); err != nil {
			return nil, err
		}
		if p.GoalID, err = parseNullULID(goalID); err != nil {
			return nil, err
		}
		if sTitle != nil {
			p.Source = &memory.Reading{Kind: *sKind, Title: *sTitle, Author: *sAuthor, Chars: *sChars}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SaveSource returns the ID already stored if the same URL was fetched before.
// Sources are shared by every agent, so they're not scoped.
func (s *Store) SaveSource(ctx context.Context, src memory.Source) (ulid.ULID, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO sources (id, kind, title, author, url, text, chars, fetched_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (url) DO UPDATE SET url = EXCLUDED.url RETURNING id`,
		src.ID.String(), src.Kind, src.Title, src.Author, src.URL, src.Text, len(src.Text), src.FetchedAt).Scan(&id)
	if err != nil {
		return ulid.ULID{}, fmt.Errorf("save source: %w", err)
	}
	return ulid.Parse(id)
}

func (s *Store) SourceText(ctx context.Context, id ulid.ULID) (string, error) {
	var text string
	if err := s.pool.QueryRow(ctx, `SELECT text FROM sources WHERE id = $1`, id.String()).Scan(&text); err != nil {
		return "", fmt.Errorf("load source: %w", err)
	}
	return text, nil
}

const workColumns = `id, intention_id, project_id, kind, title, text, created_at, public, coalesce(public_by, ''),
	coalesce(data::text, ''), data_kind`

// shareableWork is the condition for a work (aliased w) that may ever be
// public: not its journal, and made for itself, not for someone (a promise).
const shareableWork = `w.kind NOT IN ('journal', 'review') AND (w.intention_id IS NULL OR EXISTS (
	SELECT 1 FROM intentions i WHERE i.agent_id = w.agent_id AND i.id = w.intention_id AND i.subject = '` + memory.SubjectSelf + `'))`

func (s *Store) SaveWork(ctx context.Context, w memory.Work) error {
	_, err := s.exec(ctx, `
		INSERT INTO works (agent_id, id, intention_id, project_id, kind, title, text, created_at, data, data_kind)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		w.ID.String(), nullULID(w.IntentionID), nullULID(w.ProjectID), w.Kind, w.Title, w.Text, w.CreatedAt, nullData(w.Data), w.DataKind)
	if err != nil {
		return fmt.Errorf("save work: %w", err)
	}
	return nil
}

// WorksBetween returns what the agent made in [from, to), oldest first.
func (s *Store) WorksBetween(ctx context.Context, from, to time.Time) ([]memory.Work, error) {
	return s.queryWorks(ctx, `SELECT `+workColumns+` FROM works
		WHERE agent_id = $1 AND created_at >= $2 AND created_at < $3 ORDER BY created_at, id`, from, to)
}

// ProjectWorks returns the newest works in a project, oldest first.
func (s *Store) ProjectWorks(ctx context.Context, project ulid.ULID, limit int) ([]memory.Work, error) {
	ws, err := s.queryWorks(ctx, `SELECT `+workColumns+` FROM works
		WHERE agent_id = $1 AND project_id = $2 ORDER BY created_at DESC, id DESC LIMIT $3`, project.String(), limit)
	slices.Reverse(ws)
	return ws, err
}

// UndecidedPublicWorks returns works that may be public but haven't been
// decided on yet, oldest first.
func (s *Store) UndecidedPublicWorks(ctx context.Context, limit int) ([]memory.Work, error) {
	return s.queryWorks(ctx, `SELECT `+workColumns+` FROM works w
		WHERE w.agent_id = $1 AND w.public IS NULL AND w.public_by IS NULL AND `+shareableWork+` ORDER BY w.created_at, w.id LIMIT $2`, limit)
}

// AwaitWorkPublic marks an undecided work as judged public but waiting for
// the owner's approval, so the daily check doesn't ask about it again.
func (s *Store) AwaitWorkPublic(ctx context.Context, id ulid.ULID) error {
	if _, err := s.exec(ctx, `UPDATE works SET public_by = $3 WHERE agent_id = $1 AND id = $2 AND public IS NULL`,
		id.String(), memory.PublicAwaiting); err != nil {
		return fmt.Errorf("await work public: %w", err)
	}
	return nil
}

// SetWorkPublic reports whether the work was changed. The daily check
// (PublicBySystem1) never overrides its owner, and a work that may never be
// public can't be made public.
func (s *Store) SetWorkPublic(ctx context.Context, id ulid.ULID, public bool, by string) (bool, error) {
	tag, err := s.exec(ctx, `UPDATE works w SET public = $3, public_by = $4
		WHERE w.agent_id = $1 AND w.id = $2 AND (NOT $3 OR `+shareableWork+`)
		  AND ($4 = '`+memory.PublicByOwner+`' OR w.public_by IS DISTINCT FROM '`+memory.PublicByOwner+`')`,
		id.String(), public, by)
	if err != nil {
		return false, fmt.Errorf("set work public: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) PublicWorks(ctx context.Context, limit int) ([]memory.Work, error) {
	return s.queryWorks(ctx, `SELECT `+workColumns+` FROM works w
		WHERE w.agent_id = $1 AND w.public AND `+shareableWork+` ORDER BY w.created_at DESC, w.id DESC LIMIT $2`, limit)
}

func (s *Store) PublicWork(ctx context.Context, id ulid.ULID) (memory.Work, bool, error) {
	ws, err := s.queryWorks(ctx, `SELECT `+workColumns+` FROM works w
		WHERE w.agent_id = $1 AND w.id = $2 AND w.public AND `+shareableWork, id.String())
	if err != nil || len(ws) == 0 {
		return memory.Work{}, false, err
	}
	return ws[0], true, nil
}

func (s *Store) IntentionWork(ctx context.Context, intention ulid.ULID) (memory.Work, bool, error) {
	ws, err := s.queryWorks(ctx, `SELECT `+workColumns+` FROM works
		WHERE agent_id = $1 AND intention_id = $2 ORDER BY created_at DESC, id DESC LIMIT 1`, intention.String())
	if err != nil || len(ws) == 0 {
		return memory.Work{}, false, err
	}
	return ws[0], true, nil
}

func (s *Store) LatestWork(ctx context.Context, kind string) (memory.Work, bool, error) {
	ws, err := s.queryWorks(ctx, `SELECT `+workColumns+` FROM works
		WHERE agent_id = $1 AND kind = $2 ORDER BY created_at DESC, id DESC LIMIT 1`, kind)
	if err != nil || len(ws) == 0 {
		return memory.Work{}, false, err
	}
	return ws[0], true, nil
}

// worksFor selects the works it may bring up with person ($3): those made for
// itself or for them, not its journal or reviews.
const worksFor = `SELECT ` + workColumns + `, embedding, embedded_with,
	coalesce((SELECT i.subject FROM intentions i WHERE i.agent_id = w.agent_id AND i.id = w.intention_id), '') FROM works w
	WHERE w.agent_id = $1 AND w.kind NOT IN ('journal', 'review') AND (w.intention_id IS NULL OR EXISTS (
		SELECT 1 FROM intentions i WHERE i.agent_id = w.agent_id AND i.id = w.intention_id AND i.subject IN ($2, $3)))`

// WorksFor returns, newest first, the works it may bring up with person.
func (s *Store) WorksFor(ctx context.Context, person string, limit int) ([]memory.Work, error) {
	return s.queryWorksWith(ctx, true, worksFor+` ORDER BY w.created_at DESC, w.id DESC LIMIT $4`, memory.SubjectSelf, person, limit)
}

// WorkFor is work id if it's one it may bring up with person; false if not.
func (s *Store) WorkFor(ctx context.Context, id ulid.ULID, person string) (memory.Work, bool, error) {
	ws, err := s.queryWorksWith(ctx, true, worksFor+` AND w.id = $4`, memory.SubjectSelf, person, id.String())
	if err != nil || len(ws) == 0 {
		return memory.Work{}, false, err
	}
	return ws[0], true, nil
}

func (s *Store) SetWorkData(ctx context.Context, id ulid.ULID, kind string, data json.RawMessage) error {
	if _, err := s.exec(ctx, `UPDATE works SET data = $3, data_kind = $4 WHERE agent_id = $1 AND id = $2`, id.String(), nullData(data), kind); err != nil {
		return fmt.Errorf("set work data: %w", err)
	}
	return nil
}

// WorksMissingData returns up to limit works of kinds that carry no data,
// newest first.
func (s *Store) WorksMissingData(ctx context.Context, kinds []string, limit int) ([]memory.Work, error) {
	return s.queryWorks(ctx, `SELECT `+workColumns+` FROM works WHERE agent_id = $1 AND data IS NULL AND kind = ANY($2)
		ORDER BY created_at DESC, id DESC LIMIT $3`, kinds, limit)
}

func nullData(data json.RawMessage) any {
	if len(data) == 0 {
		return nil
	}
	return string(data)
}

func (s *Store) SaveWorkEmbedding(ctx context.Context, id ulid.ULID, v []float32, model string) error {
	if _, err := s.exec(ctx, `UPDATE works SET embedding = $3, embedded_with = $4 WHERE agent_id = $1 AND id = $2`, id.String(), v, model); err != nil {
		return fmt.Errorf("save work embedding: %w", err)
	}
	return nil
}

func (s *Store) queryWorks(ctx context.Context, sql string, args ...any) ([]memory.Work, error) {
	return s.queryWorksWith(ctx, false, sql, args...)
}

// queryWorksWith scans workColumns, then embedding, embedded_with and who
// it was made for if embedded.
func (s *Store) queryWorksWith(ctx context.Context, embedded bool, sql string, args ...any) ([]memory.Work, error) {
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query works: %w", err)
	}
	defer rows.Close()
	var out []memory.Work
	for rows.Next() {
		var (
			w                      memory.Work
			id                     string
			intentionID, projectID *string
		)
		var data string
		dest := []any{&id, &intentionID, &projectID, &w.Kind, &w.Title, &w.Text, &w.CreatedAt, &w.Public, &w.PublicBy, &data, &w.DataKind}
		if embedded {
			dest = append(dest, &w.Embedding, &w.EmbeddedWith, &w.MadeFor)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if data != "" {
			w.Data = json.RawMessage(data)
		}
		if w.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		if w.IntentionID, err = parseNullULID(intentionID); err != nil {
			return nil, err
		}
		if w.ProjectID, err = parseNullULID(projectID); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) CostSince(ctx context.Context, since time.Time, actions []string) (float64, error) {
	var cost float64
	err := s.queryRow(ctx, `SELECT coalesce(sum(cost_usd), 0)::float8 FROM decisions
		WHERE agent_id = $1 AND at >= $2 AND action = ANY($3)`, since, actions).Scan(&cost)
	if err != nil {
		return 0, fmt.Errorf("query cost: %w", err)
	}
	return cost, nil
}

// Conversations returns, for each person who has messaged the agent, the
// thread of their latest message and when anything was last said in it.
func (s *Store) Conversations(ctx context.Context) ([]memory.Conversation, error) {
	rows, err := s.query(ctx, `
		SELECT DISTINCT ON (p.speaker) p.speaker, p.source, p.thread, p.payload,
		       (SELECT max(q.at) FROM percepts q WHERE q.agent_id = p.agent_id AND q.source = p.source AND q.thread = p.thread)
		FROM percepts p
		WHERE p.agent_id = $1 AND p.kind = 'message' AND p.speaker <> '' AND p.thread <> ''
		ORDER BY p.speaker, p.at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query conversations: %w", err)
	}
	defer rows.Close()
	var out []memory.Conversation
	for rows.Next() {
		var c memory.Conversation
		if err := rows.Scan(&c.Person, &c.Source, &c.Thread, &c.Payload, &c.LastActivity); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LastPercept is when the newest percept of kind with person was, and whether
// there was one: a message they wrote, or a reminder or check-in the agent
// spoke first with.
func (s *Store) LastPercept(ctx context.Context, person, kind string) (time.Time, bool, error) {
	var at *time.Time
	err := s.queryRow(ctx, `SELECT max(at) FROM percepts WHERE agent_id = $1 AND speaker = $2 AND kind = $3`, person, kind).Scan(&at)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query last %s: %w", kind, err)
	}
	if at == nil {
		return time.Time{}, false, nil
	}
	return *at, true, nil
}

// UnaskedFacts returns facts the agent has not checked in about yet, newest first.
func (s *Store) UnaskedFacts(ctx context.Context, person string, since time.Time, limit int) ([]memory.Fact, error) {
	return s.queryFacts(ctx, `SELECT `+factColumns+` FROM facts
		WHERE agent_id = $1 AND subject = $2 AND at >= $3 AND asked_at IS NULL ORDER BY at DESC, id DESC LIMIT $4`, person, since, limit)
}

func (s *Store) MarkFactsAsked(ctx context.Context, ids []ulid.ULID, at time.Time) error {
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = id.String()
	}
	if _, err := s.exec(ctx, `UPDATE facts SET asked_at = $3 WHERE agent_id = $1 AND id = ANY($2)`, keys, at); err != nil {
		return fmt.Errorf("mark facts asked: %w", err)
	}
	return nil
}

const personColumns = `key, name, relation, circle, source, first_seen, updated_at`

func (s *Store) Person(ctx context.Context, key string) (memory.Person, bool, error) {
	people, err := s.queryPeople(ctx, `SELECT `+personColumns+` FROM people WHERE agent_id = $1 AND key = $2`, key)
	if err != nil || len(people) == 0 {
		return memory.Person{}, false, err
	}
	return people[0], true, nil
}

// KnownPeople returns everyone whose relation is known, oldest acquaintance first.
func (s *Store) KnownPeople(ctx context.Context, limit int) ([]memory.Person, error) {
	return s.queryPeople(ctx, `SELECT `+personColumns+` FROM people WHERE agent_id = $1 AND relation <> ''
		ORDER BY first_seen, key LIMIT $2`, limit)
}

func (s *Store) SavePerson(ctx context.Context, p memory.Person) error {
	_, err := s.exec(ctx, `
		INSERT INTO people (agent_id, key, name, relation, circle, source, first_seen, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (agent_id, key) DO UPDATE SET name = EXCLUDED.name, relation = EXCLUDED.relation,
			circle = EXCLUDED.circle, source = EXCLUDED.source, updated_at = EXCLUDED.updated_at`,
		p.Key, p.Name, p.Relation, p.Circle, p.Source, p.FirstSeen, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save person: %w", err)
	}
	return nil
}

// AddPersonIfNew is for what the agent was born knowing: memory, once formed,
// wins over the file it came from.
func (s *Store) AddPersonIfNew(ctx context.Context, p memory.Person) (bool, error) {
	tag, err := s.exec(ctx, `
		INSERT INTO people (agent_id, key, name, relation, circle, source, first_seen, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (agent_id, key) DO NOTHING`,
		p.Key, p.Name, p.Relation, p.Circle, p.Source, p.FirstSeen, p.UpdatedAt)
	if err != nil {
		return false, fmt.Errorf("add person: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) queryPeople(ctx context.Context, sql string, args ...any) ([]memory.Person, error) {
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query people: %w", err)
	}
	defer rows.Close()
	var out []memory.Person
	for rows.Next() {
		var p memory.Person
		if err := rows.Scan(&p.Key, &p.Name, &p.Relation, &p.Circle, &p.Source, &p.FirstSeen, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// nonNil keeps a nil slice from becoming SQL NULL in a NOT NULL array column.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nullULID(id ulid.ULID) any {
	if id == (ulid.ULID{}) {
		return nil
	}
	return id.String()
}

func parseNullULID(s *string) (ulid.ULID, error) {
	if s == nil {
		return ulid.ULID{}, nil
	}
	return ulid.Parse(*s)
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

const approvalColumns = `id, class, person, what, text, kind, ref, status, asked_at, sent_at, decided_at, message, why, coalesce(topic_id, '')`

func (s *Store) SaveApproval(ctx context.Context, a memory.Approval) error {
	_, err := s.exec(ctx, `INSERT INTO approvals (agent_id, id, class, person, what, text, kind, ref, status, asked_at, why, topic_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, nullif($12, ''))`,
		a.ID.String(), a.Class, a.Person, a.What, a.Text, a.Kind, ulidOrEmpty(a.Ref), a.Status, a.AskedAt, a.Why, a.Topic)
	if err != nil {
		return fmt.Errorf("save approval: %w", err)
	}
	return nil
}

func (s *Store) Approval(ctx context.Context, id ulid.ULID) (memory.Approval, bool, error) {
	as, err := s.queryApprovals(ctx, `SELECT `+approvalColumns+` FROM approvals WHERE agent_id = $1 AND id = $2`, id.String())
	if err != nil || len(as) == 0 {
		return memory.Approval{}, false, err
	}
	return as[0], true, nil
}

// UnsentApprovals returns pending approvals the owner hasn't been asked yet, oldest first.
func (s *Store) UnsentApprovals(ctx context.Context, limit int) ([]memory.Approval, error) {
	return s.queryApprovals(ctx, `SELECT `+approvalColumns+` FROM approvals
		WHERE agent_id = $1 AND status = $2 AND sent_at IS NULL ORDER BY asked_at, id LIMIT $3`, memory.ApprovalPending, limit)
}

func (s *Store) MarkApprovalSent(ctx context.Context, id ulid.ULID, message []byte, at time.Time) error {
	if _, err := s.exec(ctx, `UPDATE approvals SET sent_at = $3, message = $4 WHERE agent_id = $1 AND id = $2`,
		id.String(), at, message); err != nil {
		return fmt.Errorf("mark approval sent: %w", err)
	}
	return nil
}

// DecideApproval reports whether it was still pending: an approval is decided
// once. text, if not "", is the owner's version of its text, kept with it.
func (s *Store) DecideApproval(ctx context.Context, id ulid.ULID, status, text string, at time.Time) (bool, error) {
	tag, err := s.exec(ctx, `UPDATE approvals SET status = $3, decided_at = $4, text = coalesce(nullif($6, ''), text)
		WHERE agent_id = $1 AND id = $2 AND status = $5`, id.String(), status, at, memory.ApprovalPending, text)
	if err != nil {
		return false, fmt.Errorf("decide approval: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// StaleApprovals returns pending approvals asked before before, oldest first.
func (s *Store) StaleApprovals(ctx context.Context, before time.Time, limit int) ([]memory.Approval, error) {
	return s.queryApprovals(ctx, `SELECT `+approvalColumns+` FROM approvals
		WHERE agent_id = $1 AND status = $2 AND asked_at < $3 ORDER BY asked_at, id LIMIT $4`, memory.ApprovalPending, before, limit)
}

// RecentApprovals returns the pending approvals, oldest first, then the newest
// decided ones.
func (s *Store) RecentApprovals(ctx context.Context, limit int) ([]memory.Approval, error) {
	return s.queryApprovals(ctx, `(SELECT `+approvalColumns+` FROM approvals WHERE agent_id = $1 AND status = $2 ORDER BY asked_at, id)
		UNION ALL (SELECT `+approvalColumns+` FROM approvals WHERE agent_id = $1 AND status <> $2 ORDER BY decided_at DESC, id DESC LIMIT $3)`,
		memory.ApprovalPending, limit)
}

func (s *Store) queryApprovals(ctx context.Context, sql string, args ...any) ([]memory.Approval, error) {
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query approvals: %w", err)
	}
	defer rows.Close()
	var out []memory.Approval
	for rows.Next() {
		var (
			a                 memory.Approval
			id, ref           string
			sentAt, decidedAt *time.Time
		)
		if err := rows.Scan(&id, &a.Class, &a.Person, &a.What, &a.Text, &a.Kind, &ref, &a.Status, &a.AskedAt,
			&sentAt, &decidedAt, &a.Message, &a.Why, &a.Topic); err != nil {
			return nil, err
		}
		if a.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		if ref != "" {
			if a.Ref, err = ulid.Parse(ref); err != nil {
				return nil, err
			}
		}
		if sentAt != nil {
			a.SentAt = *sentAt
		}
		if decidedAt != nil {
			a.DecidedAt = *decidedAt
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func ulidOrEmpty(id ulid.ULID) string {
	if id.IsZero() {
		return ""
	}
	return id.String()
}

func (s *Store) SaveAutonomyRule(ctx context.Context, class, verdict, by string, percept ulid.ULID, at time.Time) error {
	if _, err := s.exec(ctx, `INSERT INTO autonomy_rules (agent_id, id, class, verdict, set_by, percept_id, at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		ulid.Make().String(), class, verdict, by, nullULID(percept), at); err != nil {
		return fmt.Errorf("save autonomy rule: %w", err)
	}
	return nil
}

// AutonomyRules returns the owner's newest change per class.
func (s *Store) AutonomyRules(ctx context.Context) (map[string]string, error) {
	rows, err := s.query(ctx, `SELECT DISTINCT ON (class) class, verdict FROM autonomy_rules WHERE agent_id = $1 ORDER BY class, at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query autonomy rules: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var class, verdict string
		if err := rows.Scan(&class, &verdict); err != nil {
			return nil, err
		}
		out[class] = verdict
	}
	return out, rows.Err()
}

func (s *Store) DeleteFact(ctx context.Context, id ulid.ULID) (bool, error) {
	tag, err := s.exec(ctx, `DELETE FROM facts WHERE agent_id = $1 AND id = $2`, id.String())
	if err != nil {
		return false, fmt.Errorf("delete fact: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// CorrectFact puts a fact in new words, as of at, keeping what it's about and
// how it's filed.
func (s *Store) CorrectFact(ctx context.Context, id ulid.ULID, text string, at time.Time) (bool, error) {
	tag, err := s.exec(ctx, `UPDATE facts SET text = $3, at = $4 WHERE agent_id = $1 AND id = $2`, id.String(), text, at)
	if err != nil {
		return false, fmt.Errorf("correct fact: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// SaveLabel replaces an earlier label for the same decision and question.
func (s *Store) SaveLabel(ctx context.Context, l memory.Label) error {
	_, err := s.exec(ctx, `INSERT INTO labels (agent_id, decision_id, question, value, source, at) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (agent_id, decision_id, question) DO UPDATE SET value = EXCLUDED.value, source = EXCLUDED.source, at = EXCLUDED.at`,
		l.DecisionID.String(), l.Question, l.Value, l.Source, l.At)
	if err != nil {
		return fmt.Errorf("save label: %w", err)
	}
	return nil
}

func (s *Store) Labels(ctx context.Context, decisions []ulid.ULID) ([]memory.Label, error) {
	ids := make([]string, len(decisions))
	for i, id := range decisions {
		ids[i] = id.String()
	}
	rows, err := s.query(ctx, `SELECT decision_id, question, value, source, at FROM labels WHERE agent_id = $1 AND decision_id = ANY($2)`, ids)
	if err != nil {
		return nil, fmt.Errorf("query labels: %w", err)
	}
	defer rows.Close()
	var out []memory.Label
	for rows.Next() {
		var (
			l  memory.Label
			id string
		)
		if err := rows.Scan(&id, &l.Question, &l.Value, &l.Source, &l.At); err != nil {
			return nil, err
		}
		if l.DecisionID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// LastRead returns the newest decision of setID (any version) on a message in
// source's thread before before: the agent's read of the message before the one
// it is answering.
func (s *Store) LastRead(ctx context.Context, setID, source, thread string, before time.Time) (ulid.ULID, bool, error) {
	var id string
	err := s.queryRow(ctx, `SELECT d.id FROM decisions d JOIN percepts p ON p.agent_id = d.agent_id AND p.id = d.percept_id
		WHERE d.agent_id = $1 AND d.set_id LIKE $2 AND p.source = $3 AND p.thread = $4 AND p.at < $5 AND p.kind = 'message'
		ORDER BY p.at DESC, d.at LIMIT 1`, setID+"@%", source, thread, before).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ulid.ULID{}, false, nil
	}
	if err != nil {
		return ulid.ULID{}, false, fmt.Errorf("last read: %w", err)
	}
	d, err := ulid.Parse(id)
	return d, err == nil, err
}

type Example struct {
	DecisionID ulid.ULID
	SetID      string // the set and version that decided, e.g. "incoming_message@1"
	Text       string
	Answer     decision.Answer
	Label      string
}

// LabelledExamples returns the newest decisions of setID (any version) with a
// label on question.
func (s *Store) LabelledExamples(ctx context.Context, setID, question string, limit int) ([]Example, error) {
	rows, err := s.query(ctx, `SELECT d.id, split_part(d.set_id, ',', 1), p.text, d.answers -> $3, l.value
		FROM labels l JOIN decisions d ON d.agent_id = l.agent_id AND d.id = l.decision_id
		JOIN percepts p ON p.agent_id = d.agent_id AND p.id = d.percept_id
		WHERE l.agent_id = $1 AND l.question = $3 AND d.set_id LIKE $2 AND p.text <> ''
		ORDER BY d.at DESC LIMIT $4`, setID+"@%", question, limit)
	if err != nil {
		return nil, fmt.Errorf("query labelled examples: %w", err)
	}
	defer rows.Close()
	var out []Example
	for rows.Next() {
		var (
			e      Example
			id     string
			answer []byte
		)
		if err := rows.Scan(&id, &e.SetID, &e.Text, &answer, &e.Label); err != nil {
			return nil, err
		}
		if e.DecisionID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		if len(answer) > 0 {
			if err := json.Unmarshal(answer, &e.Answer); err != nil {
				return nil, err
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SaveDream(ctx context.Context, d memory.Dream) error {
	_, err := s.exec(ctx, `INSERT INTO dreams (agent_id, id, text, status, source, since, ended_at) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (agent_id, id) DO UPDATE SET text = EXCLUDED.text, status = EXCLUDED.status, ended_at = EXCLUDED.ended_at`,
		d.ID.String(), d.Text, d.Status, d.Source, d.Since, nullTime(d.EndedAt))
	if err != nil {
		return fmt.Errorf("save dream: %w", err)
	}
	return nil
}

// Dreams returns its dreams, oldest first.
func (s *Store) Dreams(ctx context.Context, activeOnly bool) ([]memory.Dream, error) {
	rows, err := s.query(ctx, `SELECT id, text, status, source, since, ended_at FROM dreams
		WHERE agent_id = $1 AND (NOT $2 OR status = $3) ORDER BY since, id`, activeOnly, memory.DreamActive)
	if err != nil {
		return nil, fmt.Errorf("query dreams: %w", err)
	}
	defer rows.Close()
	var out []memory.Dream
	for rows.Next() {
		var (
			d     memory.Dream
			id    string
			ended *time.Time
		)
		if err := rows.Scan(&id, &d.Text, &d.Status, &d.Source, &d.Since, &ended); err != nil {
			return nil, err
		}
		if d.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		if ended != nil {
			d.EndedAt = *ended
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) SaveGoal(ctx context.Context, g memory.Goal) error {
	_, err := s.exec(ctx, `INSERT INTO goals (agent_id, id, dream_id, text, done_when, due, status, progress, created_at, updated_at, skill)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (agent_id, id) DO UPDATE SET text = EXCLUDED.text, done_when = EXCLUDED.done_when, due = EXCLUDED.due,
			status = EXCLUDED.status, progress = EXCLUDED.progress, updated_at = EXCLUDED.updated_at, skill = EXCLUDED.skill`,
		g.ID.String(), g.DreamID.String(), g.Text, g.DoneWhen, g.Due, g.Status, g.Progress, g.CreatedAt, g.UpdatedAt, g.Skill)
	if err != nil {
		return fmt.Errorf("save goal: %w", err)
	}
	return nil
}

// Goals returns its goals, oldest first.
func (s *Store) Goals(ctx context.Context, activeOnly bool) ([]memory.Goal, error) {
	rows, err := s.query(ctx, `SELECT id, dream_id, text, done_when, due, status, progress, created_at, updated_at, skill FROM goals
		WHERE agent_id = $1 AND (NOT $2 OR status = $3) ORDER BY created_at, id`, activeOnly, memory.GoalActive)
	if err != nil {
		return nil, fmt.Errorf("query goals: %w", err)
	}
	defer rows.Close()
	var out []memory.Goal
	for rows.Next() {
		var (
			g           memory.Goal
			id, dreamID string
		)
		if err := rows.Scan(&id, &dreamID, &g.Text, &g.DoneWhen, &g.Due, &g.Status, &g.Progress, &g.CreatedAt, &g.UpdatedAt, &g.Skill); err != nil {
			return nil, err
		}
		if g.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		if g.DreamID, err = ulid.Parse(dreamID); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// AddSkill does nothing if it already has the skill.
func (s *Store) AddSkill(ctx context.Context, sk memory.Skill) error {
	if _, err := s.exec(ctx, `INSERT INTO skills (agent_id, name, level, shown_by, updated_at) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (agent_id, name) DO NOTHING`, sk.Name, sk.Level, nonNil(sk.ShownBy), sk.UpdatedAt); err != nil {
		return fmt.Errorf("add skill: %w", err)
	}
	return nil
}

// Skills returns its skills, by name.
func (s *Store) Skills(ctx context.Context) ([]memory.Skill, error) {
	rows, err := s.query(ctx, `SELECT name, level, shown_by, updated_at FROM skills WHERE agent_id = $1 ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query skills: %w", err)
	}
	defer rows.Close()
	var out []memory.Skill
	for rows.Next() {
		var sk memory.Skill
		if err := rows.Scan(&sk.Name, &sk.Level, &sk.ShownBy, &sk.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

func (s *Store) SetSkillLevel(ctx context.Context, name string, level float64, at time.Time) error {
	if _, err := s.exec(ctx, `UPDATE skills SET level = $3, updated_at = $4 WHERE agent_id = $1 AND name = $2`, name, level, at); err != nil {
		return fmt.Errorf("set skill level: %w", err)
	}
	return nil
}

func (s *Store) SaveEvidence(ctx context.Context, e memory.Evidence) error {
	if _, err := s.exec(ctx, `INSERT INTO skill_evidence (agent_id, id, skill, work_id, score, source, note, at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.ID.String(), e.Skill, nullULID(e.WorkID), e.Score, e.Source, e.Note, e.At); err != nil {
		return fmt.Errorf("save evidence: %w", err)
	}
	return nil
}

// RecentEvidence returns a skill's newest scores, newest first.
func (s *Store) RecentEvidence(ctx context.Context, skill string, limit int) ([]memory.Evidence, error) {
	rows, err := s.query(ctx, `SELECT id, skill, work_id, score, source, note, at FROM skill_evidence
		WHERE agent_id = $1 AND skill = $2 ORDER BY at DESC, id DESC LIMIT $3`, skill, limit)
	if err != nil {
		return nil, fmt.Errorf("query evidence: %w", err)
	}
	defer rows.Close()
	var out []memory.Evidence
	for rows.Next() {
		var (
			e      memory.Evidence
			id     string
			workID *string
		)
		if err := rows.Scan(&id, &e.Skill, &workID, &e.Score, &e.Source, &e.Note, &e.At); err != nil {
			return nil, err
		}
		if e.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		if e.WorkID, err = parseNullULID(workID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SaveLesson(ctx context.Context, l memory.Lesson) error {
	if _, err := s.exec(ctx, `INSERT INTO lessons (agent_id, id, skill, text, source, work_id, at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		l.ID.String(), l.Skill, l.Text, l.Source, nullULID(l.WorkID), l.At); err != nil {
		return fmt.Errorf("save lesson: %w", err)
	}
	return nil
}

// Lessons returns active lessons, newest first; all skills' if skill is "".
func (s *Store) Lessons(ctx context.Context, skill string, limit int) ([]memory.Lesson, error) {
	rows, err := s.query(ctx, `SELECT id, skill, text, source, work_id, at FROM lessons
		WHERE agent_id = $1 AND retired_at IS NULL AND ($2 = '' OR skill = $2) ORDER BY at DESC, id DESC LIMIT $3`, skill, limit)
	if err != nil {
		return nil, fmt.Errorf("query lessons: %w", err)
	}
	defer rows.Close()
	var out []memory.Lesson
	for rows.Next() {
		var (
			l      memory.Lesson
			id     string
			workID *string
		)
		if err := rows.Scan(&id, &l.Skill, &l.Text, &l.Source, &workID, &l.At); err != nil {
			return nil, err
		}
		if l.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		if l.WorkID, err = parseNullULID(workID); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) RetireLesson(ctx context.Context, id ulid.ULID, at time.Time) error {
	if _, err := s.exec(ctx, `UPDATE lessons SET retired_at = $3 WHERE agent_id = $1 AND id = $2 AND retired_at IS NULL`, id.String(), at); err != nil {
		return fmt.Errorf("retire lesson: %w", err)
	}
	return nil
}

func (s *Store) MarkReviewAsked(ctx context.Context, work ulid.ULID, at time.Time) error {
	if _, err := s.exec(ctx, `UPDATE works SET review_asked_at = $3 WHERE agent_id = $1 AND id = $2`, work.String(), at); err != nil {
		return fmt.Errorf("mark review asked: %w", err)
	}
	return nil
}

func (s *Store) AskedForReview(ctx context.Context, since time.Time) (memory.Work, bool, error) {
	ws, err := s.queryWorks(ctx, `SELECT `+workColumns+` FROM works WHERE agent_id = $1 AND review_asked_at >= $2
		ORDER BY review_asked_at DESC, id DESC LIMIT 1`, since)
	if err != nil || len(ws) == 0 {
		return memory.Work{}, false, err
	}
	return ws[0], true, nil
}

func (s *Store) SavePlan(ctx context.Context, p memory.LearningPlan) error {
	tx, err := s.begin(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("save plan: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO learning_plans (agent_id, id, goal_id, skill, stage, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, s.agent, p.ID.String(), p.GoalID.String(), p.Skill, p.Stage, p.Status, p.CreatedAt, p.UpdatedAt); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}
	for _, st := range p.Stages {
		if _, err := tx.Exec(ctx, `INSERT INTO plan_stages (agent_id, plan_id, n, learn, practice, done_when) VALUES ($1, $2, $3, $4, $5, $6)`, s.agent,
			p.ID.String(), st.N, st.Learn, st.Practice, st.DoneWhen); err != nil {
			return fmt.Errorf("save plan stage: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// Plans returns its learning plans with their stages, oldest first.
func (s *Store) Plans(ctx context.Context, activeOnly bool) ([]memory.LearningPlan, error) {
	rows, err := s.query(ctx, `SELECT id, goal_id, skill, stage, status, created_at, updated_at FROM learning_plans
		WHERE agent_id = $1 AND (NOT $2 OR status = $3) ORDER BY created_at, id`, activeOnly, memory.PlanActive)
	if err != nil {
		return nil, fmt.Errorf("query plans: %w", err)
	}
	var out []memory.LearningPlan
	for rows.Next() {
		var (
			p          memory.LearningPlan
			id, goalID string
		)
		if err := rows.Scan(&id, &goalID, &p.Skill, &p.Stage, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if p.ID, err = ulid.Parse(id); err != nil {
			rows.Close()
			return nil, err
		}
		if p.GoalID, err = ulid.Parse(goalID); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		srows, err := s.query(ctx, `SELECT n, learn, practice, done_when, done_at FROM plan_stages WHERE agent_id = $1 AND plan_id = $2 ORDER BY n`,
			out[i].ID.String())
		if err != nil {
			return nil, fmt.Errorf("query plan stages: %w", err)
		}
		for srows.Next() {
			var (
				st   memory.Stage
				done *time.Time
			)
			if err := srows.Scan(&st.N, &st.Learn, &st.Practice, &st.DoneWhen, &done); err != nil {
				srows.Close()
				return nil, err
			}
			if done != nil {
				st.DoneAt = *done
			}
			out[i].Stages = append(out[i].Stages, st)
		}
		srows.Close()
	}
	return out, nil
}

// AdvancePlan finishes a plan's current stage and moves to the next, or
// marks the plan done after its last.
func (s *Store) AdvancePlan(ctx context.Context, plan ulid.ULID, at time.Time) error {
	if _, err := s.exec(ctx, `UPDATE plan_stages SET done_at = $3
		WHERE agent_id = $1 AND plan_id = $2 AND n = (SELECT stage FROM learning_plans WHERE agent_id = $1 AND id = $2)`, plan.String(), at); err != nil {
		return fmt.Errorf("advance plan: %w", err)
	}
	if _, err := s.exec(ctx, `UPDATE learning_plans p SET stage = p.stage + 1, updated_at = $3,
			status = CASE WHEN p.stage + 1 >= (SELECT count(*) FROM plan_stages WHERE agent_id = p.agent_id AND plan_id = p.id) THEN $4 ELSE p.status END
		WHERE p.agent_id = $1 AND p.id = $2`, plan.String(), at, memory.PlanDone); err != nil {
		return fmt.Errorf("advance plan: %w", err)
	}
	return nil
}

func (s *Store) EndPlan(ctx context.Context, plan ulid.ULID, status string, at time.Time) error {
	if _, err := s.exec(ctx, `UPDATE learning_plans SET status = $3, updated_at = $4 WHERE agent_id = $1 AND id = $2`, plan.String(), status, at); err != nil {
		return fmt.Errorf("end plan: %w", err)
	}
	return nil
}

func (s *Store) SaveResource(ctx context.Context, r memory.Resource) error {
	if _, err := s.exec(ctx, `INSERT INTO resources (agent_id, id, plan_id, stage, ability, query, why, status, added_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, r.ID.String(), r.PlanID.String(), r.Stage, r.Ability, r.Query, r.Why, r.Status, r.AddedAt); err != nil {
		return fmt.Errorf("save resource: %w", err)
	}
	return nil
}

// Resources returns a plan's resources, by stage and when they were added.
func (s *Store) Resources(ctx context.Context, plan ulid.ULID) ([]memory.Resource, error) {
	rows, err := s.query(ctx, `SELECT id, plan_id, stage, ability, query, why, status, title, work_id, added_at, done_at FROM resources
		WHERE agent_id = $1 AND plan_id = $2 ORDER BY stage, added_at, id`, plan.String())
	if err != nil {
		return nil, fmt.Errorf("query resources: %w", err)
	}
	defer rows.Close()
	var out []memory.Resource
	for rows.Next() {
		var (
			r          memory.Resource
			id, planID string
			workID     *string
			done       *time.Time
		)
		if err := rows.Scan(&id, &planID, &r.Stage, &r.Ability, &r.Query, &r.Why, &r.Status, &r.Title, &workID, &r.AddedAt, &done); err != nil {
			return nil, err
		}
		if r.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		if r.PlanID, err = ulid.Parse(planID); err != nil {
			return nil, err
		}
		if r.WorkID, err = parseNullULID(workID); err != nil {
			return nil, err
		}
		if done != nil {
			r.DoneAt = *done
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) FinishResource(ctx context.Context, id, work ulid.ULID, title string, at time.Time) error {
	if _, err := s.exec(ctx, `UPDATE resources SET status = $3, title = $4, work_id = $5, done_at = $6 WHERE agent_id = $1 AND id = $2`,
		id.String(), memory.ResourceDone, title, nullULID(work), at); err != nil {
		return fmt.Errorf("finish resource: %w", err)
	}
	return nil
}

// ReadingProject returns the active project that follows a source.
func (s *Store) ReadingProject(ctx context.Context, source ulid.ULID) (memory.Project, bool, error) {
	ps, err := s.queryProjects(ctx, projectSelect+` AND p.source_id = $2 AND p.status = $3 ORDER BY p.updated_at DESC LIMIT 1`,
		source.String(), memory.ProjectActive)
	if err != nil || len(ps) == 0 {
		return memory.Project{}, false, err
	}
	return ps[0], true, nil
}

func (s *Store) SaveFound(ctx context.Context, query string, rs []search.Result, at time.Time) error {
	for _, r := range rs {
		if _, err := s.exec(ctx, `INSERT INTO web_found (agent_id, url, title, snippet, query, found_at) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (agent_id, url) DO NOTHING`, r.URL, r.Title, r.Snippet, query, at); err != nil {
			return fmt.Errorf("save found page: %w", err)
		}
	}
	return nil
}

func (s *Store) Found(ctx context.Context, url string) (search.Result, bool, error) {
	var r search.Result
	err := s.queryRow(ctx, `SELECT url, title, snippet FROM web_found WHERE agent_id = $1 AND url = $2`, strings.TrimSpace(url)).
		Scan(&r.URL, &r.Title, &r.Snippet)
	if errors.Is(err, pgx.ErrNoRows) {
		return search.Result{}, false, nil
	}
	if err != nil {
		return search.Result{}, false, fmt.Errorf("found page: %w", err)
	}
	return r, true, nil
}

// RecentFound returns the newest pages its searches found, newest first.
func (s *Store) RecentFound(ctx context.Context, limit int) ([]search.Result, error) {
	rows, err := s.query(ctx, `SELECT url, title, snippet FROM web_found WHERE agent_id = $1 ORDER BY found_at DESC, url LIMIT $2`, limit)
	if err != nil {
		return nil, fmt.Errorf("query found pages: %w", err)
	}
	defer rows.Close()
	var out []search.Result
	for rows.Next() {
		var r search.Result
		if err := rows.Scan(&r.URL, &r.Title, &r.Snippet); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) LogWeb(ctx context.Context, kind, what string, costUSD float64, at time.Time) error {
	if _, err := s.exec(ctx, `INSERT INTO web_log (agent_id, kind, what, cost_usd, at) VALUES ($1, $2, $3, $4, $5)`, kind, what, costUSD, at); err != nil {
		return fmt.Errorf("log web: %w", err)
	}
	return nil
}

// LastFrom is the text of the newest percept from source; false if none.
func (s *Store) LastFrom(ctx context.Context, source string) (string, bool, error) {
	var text string
	err := s.queryRow(ctx, `SELECT text FROM percepts WHERE agent_id = $1 AND source = $2 ORDER BY at DESC, id DESC LIMIT 1`, source).Scan(&text)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("last from: %w", err)
	}
	return text, true, nil
}

// SeenFrom is whether any percept from source has been kept.
func (s *Store) SeenFrom(ctx context.Context, source string) (bool, error) {
	var seen bool
	if err := s.queryRow(ctx, `SELECT EXISTS (SELECT 1 FROM percepts WHERE agent_id = $1 AND source = $2)`, source).Scan(&seen); err != nil {
		return false, fmt.Errorf("seen from: %w", err)
	}
	return seen, nil
}

func (s *Store) SaveAction(ctx context.Context, a memory.Action) error {
	_, err := s.exec(ctx, `INSERT INTO actions (agent_id, id, tool, class, args, why, status, result, percept_id, proposed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, a.ID.String(), a.Tool, a.Class, a.Args, a.Why, a.Status, a.Result,
		nullULID(a.PerceptID), a.ProposedAt)
	if err != nil {
		return fmt.Errorf("save action: %w", err)
	}
	return nil
}

// SetAction moves an action on: only from one of from, so a second tap or a
// late expiry can't move it again. It reports whether it moved.
func (s *Store) SetAction(ctx context.Context, id ulid.ULID, from []string, status, result string, at time.Time) (bool, error) {
	tag, err := s.exec(ctx, `UPDATE actions SET status = $4, result = $5,
		done_at = CASE WHEN $4 IN ('done', 'failed', 'denied') THEN $6::timestamptz ELSE done_at END
		WHERE agent_id = $1 AND id = $2 AND status = ANY($3)`, id.String(), from, status, result, at)
	if err != nil {
		return false, fmt.Errorf("set action: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// Actions returns, oldest first, up to limit actions with one of statuses.
func (s *Store) Actions(ctx context.Context, statuses []string, limit int) ([]memory.Action, error) {
	return s.queryActions(ctx, `SELECT `+actionColumns+` FROM actions WHERE agent_id = $1 AND status = ANY($2) ORDER BY proposed_at, id LIMIT $3`,
		statuses, limit)
}

func (s *Store) RecentActions(ctx context.Context, limit int) ([]memory.Action, error) {
	return s.queryActions(ctx, `SELECT `+actionColumns+` FROM actions WHERE agent_id = $1 ORDER BY proposed_at DESC, id DESC LIMIT $2`, limit)
}

func (s *Store) ActionsSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	if err := s.queryRow(ctx, `SELECT count(*) FROM actions WHERE agent_id = $1 AND proposed_at >= $2`, since).Scan(&n); err != nil {
		return 0, fmt.Errorf("count actions: %w", err)
	}
	return n, nil
}

const actionColumns = `id, tool, class, args, why, status, result, percept_id, proposed_at, done_at`

func (s *Store) queryActions(ctx context.Context, sql string, args ...any) ([]memory.Action, error) {
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query actions: %w", err)
	}
	defer rows.Close()
	var out []memory.Action
	for rows.Next() {
		var (
			a       memory.Action
			id      string
			percept *string
			doneAt  *time.Time
		)
		if err := rows.Scan(&id, &a.Tool, &a.Class, &a.Args, &a.Why, &a.Status, &a.Result, &percept, &a.ProposedAt, &doneAt); err != nil {
			return nil, err
		}
		if a.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		if a.PerceptID, err = parseNullULID(percept); err != nil {
			return nil, err
		}
		if doneAt != nil {
			a.DoneAt = *doneAt
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type WebEntry struct {
	What string    `json:"what"`
	At   time.Time `json:"at"`
}

// WebLog returns the last limit entries of kind, newest first.
func (s *Store) WebLog(ctx context.Context, kind string, limit int) ([]WebEntry, error) {
	rows, err := s.query(ctx, `SELECT what, at FROM web_log WHERE agent_id = $1 AND kind = $2 ORDER BY at DESC LIMIT $3`, kind, limit)
	if err != nil {
		return nil, fmt.Errorf("query web log: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[WebEntry])
}

func (s *Store) WebSince(ctx context.Context, kind string, since time.Time) (int, error) {
	var n int
	if err := s.queryRow(ctx, `SELECT count(*) FROM web_log WHERE agent_id = $1 AND kind = $2 AND at >= $3`, kind, since).Scan(&n); err != nil {
		return 0, fmt.Errorf("count web: %w", err)
	}
	return n, nil
}

// SeedJudgment adds its owner's judgment file as proposed: each case not seen
// before (by key), and each principle not seeded before (by its seed text, so an
// edited or rejected one isn't seeded again). It returns how many of each it added.
func (s *Store) SeedJudgment(ctx context.Context, cases []memory.JudgmentCase, principles []memory.Principle) (int, int, error) {
	tx, err := s.begin(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("seed judgment: %w", err)
	}
	defer tx.Rollback(ctx)
	nc, np := 0, 0
	for _, c := range cases {
		tag, err := tx.Exec(ctx, `INSERT INTO judgment_cases (agent_id, id, key, type, situation, options, choice, reason, status, source, at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11) ON CONFLICT (agent_id, key) DO NOTHING`, s.agent,
			c.ID.String(), c.Key, c.Type, c.Situation, nonNil(c.Options), c.Choice, c.Reason, c.Status, c.Source, c.At)
		if err != nil {
			return 0, 0, fmt.Errorf("seed case %s: %w", c.Key, err)
		}
		nc += int(tag.RowsAffected())
	}
	for _, p := range principles {
		tag, err := tx.Exec(ctx, `INSERT INTO principles (agent_id, id, text, kind, rank, types, status, source, seed, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10) ON CONFLICT (agent_id, seed) DO NOTHING`, s.agent,
			p.ID.String(), p.Text, kindOf(p), p.Rank, nonNil(p.Types), p.Status, p.Source, p.Seed, p.CreatedAt)
		if err != nil {
			return 0, 0, fmt.Errorf("seed principle: %w", err)
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		np++
		if _, err := tx.Exec(ctx, `INSERT INTO principle_cases (agent_id, principle_id, case_id)
			SELECT $1, $2, id FROM judgment_cases WHERE agent_id = $1 AND key = ANY($3) ON CONFLICT DO NOTHING`, s.agent, p.ID.String(), nonNil(p.Cases)); err != nil {
			return 0, 0, fmt.Errorf("link principle's cases: %w", err)
		}
	}
	return nc, np, tx.Commit(ctx)
}

// Principles returns priorities by rank, then the rest oldest first, with the
// keys of the cases behind them; status "" is all.
func (s *Store) Principles(ctx context.Context, status string) ([]memory.Principle, error) {
	rows, err := s.query(ctx, `SELECT p.id, p.text, p.kind, p.rank, p.types, p.status, p.source, COALESCE(p.seed, ''), p.created_at, p.updated_at,
			COALESCE(array_agg(c.key ORDER BY c.at, c.key) FILTER (WHERE c.key IS NOT NULL), '{}')
		FROM principles p
		LEFT JOIN principle_cases pc ON pc.agent_id = p.agent_id AND pc.principle_id = p.id
		LEFT JOIN judgment_cases c ON c.agent_id = pc.agent_id AND c.id = pc.case_id
		WHERE p.agent_id = $1 AND ($2 = '' OR p.status = $2) GROUP BY p.agent_id, p.id ORDER BY p.kind = 'priority' DESC, p.rank, p.created_at, p.id`, status)
	if err != nil {
		return nil, fmt.Errorf("query principles: %w", err)
	}
	defer rows.Close()
	var out []memory.Principle
	for rows.Next() {
		var (
			p  memory.Principle
			id string
		)
		if err := rows.Scan(&id, &p.Text, &p.Kind, &p.Rank, &p.Types, &p.Status, &p.Source, &p.Seed, &p.CreatedAt, &p.UpdatedAt,
			&p.Cases); err != nil {
			return nil, err
		}
		if p.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// JudgmentCases returns oldest first; status "" is all, and no types is all types.
func (s *Store) JudgmentCases(ctx context.Context, status string, types []string) ([]memory.JudgmentCase, error) {
	rows, err := s.query(ctx, `SELECT id, key, type, situation, options, choice, reason, status, source, outcome, at, updated_at,
			embedding, embedded_with
		FROM judgment_cases WHERE agent_id = $1 AND ($2 = '' OR status = $2) AND (cardinality($3::text[]) = 0 OR type = ANY($3))
		ORDER BY at, key`, status, nonNil(types))
	if err != nil {
		return nil, fmt.Errorf("query judgment cases: %w", err)
	}
	defer rows.Close()
	var out []memory.JudgmentCase
	for rows.Next() {
		var (
			c  memory.JudgmentCase
			id string
		)
		if err := rows.Scan(&id, &c.Key, &c.Type, &c.Situation, &c.Options, &c.Choice, &c.Reason, &c.Status, &c.Source, &c.Outcome, &c.At,
			&c.UpdatedAt, &c.Embedding, &c.EmbeddedWith); err != nil {
			return nil, err
		}
		if c.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReviewPrinciple keeps the text when text is empty. It returns whether there
// was such a principle.
func (s *Store) ReviewPrinciple(ctx context.Context, id ulid.ULID, status, text string, at time.Time) (bool, error) {
	tag, err := s.exec(ctx, `UPDATE principles SET status = $3, text = COALESCE(NULLIF($4, ''), text), updated_at = $5
		WHERE agent_id = $1 AND id = $2`, id.String(), status, text, at)
	if err != nil {
		return false, fmt.Errorf("review principle: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ReviewCase keeps the reason when reason is empty. It returns whether there
// was such a case.
func (s *Store) ReviewCase(ctx context.Context, id ulid.ULID, status, reason string, at time.Time) (bool, error) {
	tag, err := s.exec(ctx, `UPDATE judgment_cases SET status = $3, reason = COALESCE(NULLIF($4, ''), reason), updated_at = $5,
			embedding = CASE WHEN $4 = '' OR $4 = reason THEN embedding END
		WHERE agent_id = $1 AND id = $2`, id.String(), status, reason, at)
	if err != nil {
		return false, fmt.Errorf("review case: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) SavePrinciple(ctx context.Context, p memory.Principle) error {
	tx, err := s.begin(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("save principle: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO principles (agent_id, id, text, kind, rank, types, status, source, seed, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)`, s.agent, p.ID.String(), p.Text, kindOf(p), p.Rank, nonNil(p.Types), p.Status, p.Source,
		nullString(p.Seed), p.CreatedAt); err != nil {
		return fmt.Errorf("save principle: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO principle_cases (agent_id, principle_id, case_id)
		SELECT $1, $2, id FROM judgment_cases WHERE agent_id = $1 AND key = ANY($3) ON CONFLICT DO NOTHING`, s.agent, p.ID.String(), nonNil(p.Cases)); err != nil {
		return fmt.Errorf("link principle's cases: %w", err)
	}
	return tx.Commit(ctx)
}

const callColumns = `id, type, mode, situation, options, pick, why, confidence, ref, status, waiting, waiting_since, owner_pick,
	owner_choice, owner_reason, agreed, answered_by, message, case_id, created_at, asked_at, answered_at, outcome, verdict, outcome_note,
	outcome_by, outcome_at, stakes, reversible`

func (s *Store) SaveCall(ctx context.Context, c memory.Call) error {
	_, err := s.exec(ctx, `INSERT INTO judgment_calls (agent_id, id, type, mode, situation, options, pick, why, confidence, ref, status,
		created_at, stakes, reversible) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`, c.ID.String(), c.Type, c.Mode,
		c.Situation, nonNil(c.Options), c.Pick, c.Why, c.Confidence, nullULID(c.Ref), c.Status, c.CreatedAt, c.Stakes, c.Reversible)
	if err != nil {
		return fmt.Errorf("save call: %w", err)
	}
	return nil
}

func scanCalls(rows pgx.Rows) ([]memory.Call, error) {
	defer rows.Close()
	var out []memory.Call
	for rows.Next() {
		var (
			c                     memory.Call
			id                    string
			ref, caseID           *string
			waitingAt, asked, ans *time.Time
			outcomeAt             *time.Time
			ownerPick             *int
		)
		if err := rows.Scan(&id, &c.Type, &c.Mode, &c.Situation, &c.Options, &c.Pick, &c.Why, &c.Confidence, &ref, &c.Status, &c.Waiting,
			&waitingAt, &ownerPick, &c.OwnerChoice, &c.OwnerReason, &c.Agreed, &c.AnsweredBy, &c.Message, &caseID, &c.CreatedAt, &asked,
			&ans, &c.Outcome, &c.Verdict, &c.OutcomeNote, &c.OutcomeBy, &outcomeAt, &c.Stakes, &c.Reversible); err != nil {
			return nil, err
		}
		var err error
		if c.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		if c.Ref, err = parseNullULID(ref); err != nil {
			return nil, err
		}
		if c.CaseID, err = parseNullULID(caseID); err != nil {
			return nil, err
		}
		for _, t := range []struct {
			from *time.Time
			to   *time.Time
		}{{waitingAt, &c.WaitingAt}, {asked, &c.AskedAt}, {ans, &c.AnsweredAt}, {outcomeAt, &c.OutcomeAt}} {
			if t.from != nil {
				*t.to = *t.from
			}
		}
		if ownerPick != nil {
			c.OwnerPick = *ownerPick
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Call(ctx context.Context, id ulid.ULID) (memory.Call, bool, error) {
	rows, err := s.query(ctx, `SELECT `+callColumns+` FROM judgment_calls WHERE agent_id = $1 AND id = $2`, id.String())
	if err != nil {
		return memory.Call{}, false, fmt.Errorf("query call: %w", err)
	}
	cs, err := scanCalls(rows)
	if err != nil || len(cs) == 0 {
		return memory.Call{}, false, err
	}
	return cs[0], true, nil
}

// RecentCalls returns its latest calls, newest first; status "" is all.
func (s *Store) RecentCalls(ctx context.Context, status string, limit int) ([]memory.Call, error) {
	rows, err := s.query(ctx, `SELECT `+callColumns+` FROM judgment_calls WHERE agent_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC, id DESC LIMIT $3`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("query calls: %w", err)
	}
	return scanCalls(rows)
}

func (s *Store) CallsAskedSince(ctx context.Context, mode string, since time.Time) (int, error) {
	var n int
	err := s.queryRow(ctx, `SELECT count(*) FROM judgment_calls WHERE agent_id = $1 AND mode = $2 AND asked_at >= $3`, mode, since).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count asked calls: %w", err)
	}
	return n, nil
}

func (s *Store) MarkCallAsked(ctx context.Context, id ulid.ULID, where json.RawMessage, at time.Time) error {
	if _, err := s.exec(ctx, `UPDATE judgment_calls SET status = $3, message = $4, asked_at = $5 WHERE agent_id = $1 AND id = $2 AND status = $6`,
		id.String(), memory.CallAsked, where, at, memory.CallLogged); err != nil {
		return fmt.Errorf("mark call asked: %w", err)
	}
	return nil
}

func (s *Store) StaleCalls(ctx context.Context, before time.Time, limit int) ([]memory.Call, error) {
	rows, err := s.query(ctx, `SELECT `+callColumns+` FROM judgment_calls WHERE agent_id = $1 AND status = $2 AND asked_at < $3
		ORDER BY asked_at LIMIT $4`, memory.CallAsked, before, limit)
	if err != nil {
		return nil, fmt.Errorf("query stale calls: %w", err)
	}
	return scanCalls(rows)
}

// ExpireCall returns whether it was still asked.
func (s *Store) ExpireCall(ctx context.Context, id ulid.ULID) (bool, error) {
	tag, err := s.exec(ctx, `UPDATE judgment_calls SET status = $3, waiting = '' WHERE agent_id = $1 AND id = $2 AND status = $4`,
		id.String(), memory.CallExpired, memory.CallAsked)
	if err != nil {
		return false, fmt.Errorf("expire call: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) WaitForOwner(ctx context.Context, id ulid.ULID, waiting string, at time.Time) error {
	if _, err := s.exec(ctx, `UPDATE judgment_calls SET waiting = $3, waiting_since = $4 WHERE agent_id = $1 AND id = $2`,
		id.String(), waiting, at); err != nil {
		return fmt.Errorf("wait for owner: %w", err)
	}
	return nil
}

func (s *Store) WaitingCall(ctx context.Context, since time.Time) (memory.Call, bool, error) {
	rows, err := s.query(ctx, `SELECT `+callColumns+` FROM judgment_calls WHERE agent_id = $1 AND waiting <> '' AND waiting_since >= $2
		ORDER BY waiting_since DESC LIMIT 1`, since)
	if err != nil {
		return memory.Call{}, false, fmt.Errorf("query waiting call: %w", err)
	}
	cs, err := scanCalls(rows)
	if err != nil || len(cs) == 0 {
		return memory.Call{}, false, err
	}
	return cs[0], true, nil
}

// AnswerCall records c's OwnerPick, OwnerChoice, OwnerReason, AnsweredBy,
// AnsweredAt and Waiting. With asCase, the answer is also kept as one of their
// decisions (approved: it's theirs). It returns false if already answered.
func (s *Store) AnswerCall(ctx context.Context, c memory.Call, asCase bool) (bool, error) {
	tx, err := s.begin(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("answer call: %w", err)
	}
	defer tx.Rollback(ctx)
	agreed := c.OwnerPick == c.Pick
	var caseID any
	if asCase {
		choice := c.OwnerChoice
		if c.OwnerPick >= 0 && c.OwnerPick < len(c.Options) {
			choice = c.Options[c.OwnerPick]
		}
		id := ulid.Make()
		if _, err := tx.Exec(ctx, `INSERT INTO judgment_cases (agent_id, id, key, type, situation, options, choice, reason, status, source, at, updated_at)
			VALUES ($1, $2, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)`, s.agent, id.String(), memory.CallCaseType(c.Type), c.Situation, nonNil(c.Options),
			choice, c.OwnerReason, memory.JudgmentApproved, c.AnsweredBy, c.AnsweredAt); err != nil {
			return false, fmt.Errorf("answer call: save case: %w", err)
		}
		caseID = id.String()
	}
	tag, err := tx.Exec(ctx, `UPDATE judgment_calls SET status = $3, owner_pick = $4, owner_choice = $5, owner_reason = $6, agreed = $7,
		answered_by = $8, answered_at = $9, waiting = $10, waiting_since = $9, case_id = $11 WHERE agent_id = $1 AND id = $2 AND status <> $3`, s.agent,
		c.ID.String(), memory.CallAnswered, c.OwnerPick, c.OwnerChoice, c.OwnerReason, agreed, c.AnsweredBy, c.AnsweredAt, c.Waiting, caseID)
	if err != nil {
		return false, fmt.Errorf("answer call: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	return true, tx.Commit(ctx)
}

// GiveCallReason also sets the reason on the case the call became, and stops
// waiting for one.
func (s *Store) GiveCallReason(ctx context.Context, id ulid.ULID, reason string, at time.Time) error {
	tx, err := s.begin(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("call reason: %w", err)
	}
	defer tx.Rollback(ctx)
	var caseID *string
	if err := tx.QueryRow(ctx, `UPDATE judgment_calls SET owner_reason = $3, waiting = '' WHERE agent_id = $1 AND id = $2 RETURNING case_id`, s.agent,
		id.String(), reason).Scan(&caseID); err != nil {
		return fmt.Errorf("call reason: %w", err)
	}
	if caseID != nil {
		if _, err := tx.Exec(ctx, `UPDATE judgment_cases SET reason = $3, updated_at = $4, embedding = NULL WHERE agent_id = $1 AND id = $2`, s.agent, *caseID, reason,
			at); err != nil {
			return fmt.Errorf("call reason: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// Agreement is over the latest n answered calls of each type.
func (s *Store) Agreement(ctx context.Context, n int) ([]memory.Agreement, error) {
	rows, err := s.query(ctx, `SELECT type, count(*), count(*) FILTER (WHERE agreed) FROM (
			SELECT type, agreed, row_number() OVER (PARTITION BY type ORDER BY answered_at DESC, id DESC) AS n
			FROM judgment_calls WHERE agent_id = $1 AND agreed IS NOT NULL) latest
		WHERE n <= $2 GROUP BY type ORDER BY type`, n)
	if err != nil {
		return nil, fmt.Errorf("query agreement: %w", err)
	}
	defer rows.Close()
	var out []memory.Agreement
	for rows.Next() {
		var a memory.Agreement
		if err := rows.Scan(&a.Type, &a.Answered, &a.Agreed); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// kindOf is a principle's kind; a rule if it has none.
func kindOf(p memory.Principle) string {
	if p.Kind == "" {
		return memory.PrincipleRule
	}
	return p.Kind
}

// RankPriority shifts the others to make room, and returns whether there was
// such a priority. Rank 1 is first.
func (s *Store) RankPriority(ctx context.Context, id ulid.ULID, rank int, at time.Time) (bool, error) {
	tx, err := s.begin(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("rank priority: %w", err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id FROM principles WHERE agent_id = $1 AND kind = $2 AND status <> $3 AND id <> $4 ORDER BY rank, created_at, id`, s.agent,
		memory.PrincipleRank, memory.JudgmentRejected, id.String())
	if err != nil {
		return false, fmt.Errorf("rank priority: %w", err)
	}
	var others []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			rows.Close()
			return false, err
		}
		others = append(others, o)
	}
	rows.Close()
	rank = max(1, min(rank, len(others)+1))
	tag, err := tx.Exec(ctx, `UPDATE principles SET rank = $3, updated_at = $4 WHERE agent_id = $1 AND id = $2 AND kind = $5`, s.agent, id.String(), rank, at,
		memory.PrincipleRank)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	for i, o := range others {
		r := i + 1
		if r >= rank {
			r++
		}
		if _, err := tx.Exec(ctx, `UPDATE principles SET rank = $3 WHERE agent_id = $1 AND id = $2`, s.agent, o, r); err != nil {
			return false, fmt.Errorf("rank priority: %w", err)
		}
	}
	return true, tx.Commit(ctx)
}

// CallsToLookBack returns calls not looked back on yet, oldest first: what its
// weekly review judges.
func (s *Store) CallsToLookBack(ctx context.Context, types []string, from, to time.Time, limit int) ([]memory.Call, error) {
	rows, err := s.query(ctx, `SELECT `+callColumns+` FROM judgment_calls
		WHERE agent_id = $1 AND outcome = '' AND type = ANY($2) AND created_at >= $3 AND created_at < $4 AND status <> $5
		ORDER BY created_at, id LIMIT $6`, nonNil(types), from, to, memory.CallAsked, limit)
	if err != nil {
		return nil, fmt.Errorf("query calls to look back on: %w", err)
	}
	return scanCalls(rows)
}

// SetOutcome also records it on the owner's decision the call became. The
// owner's verdict stands: its review never overrides it. It returns whether it
// was recorded.
func (s *Store) SetOutcome(ctx context.Context, id ulid.ULID, outcome, verdict, note, by string, at time.Time) (bool, error) {
	tx, err := s.begin(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("set outcome: %w", err)
	}
	defer tx.Rollback(ctx)
	var (
		caseID *string
		kept   string
	)
	err = tx.QueryRow(ctx, `UPDATE judgment_calls SET outcome = $3, verdict = $4, outcome_note = COALESCE(NULLIF($5, ''), outcome_note),
			outcome_by = $6, outcome_at = $7
		WHERE agent_id = $1 AND id = $2 AND (outcome_by <> $8 OR $6 = $8) RETURNING case_id, outcome_note`, s.agent,
		id.String(), outcome, verdict, note, by, at, memory.OutcomeByOwner).Scan(&caseID, &kept)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("set outcome: %w", err)
	}
	if caseID != nil {
		text := strings.ReplaceAll(outcome, "_", " ") + ", " + strings.ReplaceAll(verdict, "_", " ")
		if kept != "" {
			text += ": " + kept
		}
		if _, err := tx.Exec(ctx, `UPDATE judgment_cases SET outcome = $3, updated_at = $4 WHERE agent_id = $1 AND id = $2`, s.agent, *caseID, text, at); err != nil {
			return false, fmt.Errorf("set outcome: %w", err)
		}
	}
	return true, tx.Commit(ctx)
}

// AgreementByStakes is by what the calls put at stake ("low", "medium",
// "high"; "low, can't undo", ...), over the latest n answered at each level.
func (s *Store) AgreementByStakes(ctx context.Context, n int) ([]memory.Agreement, error) {
	rows, err := s.query(ctx, `SELECT level, count(*), count(*) FILTER (WHERE agreed) FROM (
			SELECT CASE WHEN stakes = '' THEN 'unknown' ELSE stakes END || CASE WHEN reversible THEN '' ELSE ', can''t undo' END AS level,
				agreed, row_number() OVER (PARTITION BY stakes, reversible ORDER BY answered_at DESC, id DESC) AS n
			FROM judgment_calls WHERE agent_id = $1 AND agreed IS NOT NULL) latest
		WHERE n <= $2 GROUP BY level ORDER BY level`, n)
	if err != nil {
		return nil, fmt.Errorf("query agreement by stakes: %w", err)
	}
	defer rows.Close()
	var out []memory.Agreement
	for rows.Next() {
		var a memory.Agreement
		if err := rows.Scan(&a.Type, &a.Answered, &a.Agreed); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) SaveCaseEmbedding(ctx context.Context, id ulid.ULID, v []float32, model string) error {
	if _, err := s.exec(ctx, `UPDATE judgment_cases SET embedding = $3, embedded_with = $4 WHERE agent_id = $1 AND id = $2`, id.String(), v, model); err != nil {
		return fmt.Errorf("save case embedding: %w", err)
	}
	return nil
}

func (s *Store) SaveTaste(ctx context.Context, t memory.Taste) error {
	if _, err := s.exec(ctx, `INSERT INTO tastes (agent_id, id, kind, likes, text, work_id, source, at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		t.ID.String(), t.Kind, t.Likes, t.Text, nullULID(t.WorkID), t.Source, t.At); err != nil {
		return fmt.Errorf("save taste: %w", err)
	}
	return nil
}

// Tastes returns those still true, newest first.
func (s *Store) Tastes(ctx context.Context, limit int) ([]memory.Taste, error) {
	rows, err := s.query(ctx, `SELECT id, kind, likes, text, work_id, source, at FROM tastes WHERE agent_id = $1 AND retired_at IS NULL
		ORDER BY at DESC, id DESC LIMIT $2`, limit)
	if err != nil {
		return nil, fmt.Errorf("query tastes: %w", err)
	}
	defer rows.Close()
	var out []memory.Taste
	for rows.Next() {
		var (
			t      memory.Taste
			id     string
			workID *string
		)
		if err := rows.Scan(&id, &t.Kind, &t.Likes, &t.Text, &workID, &t.Source, &t.At); err != nil {
			return nil, err
		}
		if t.ID, err = ulid.Parse(id); err != nil {
			return nil, err
		}
		if t.WorkID, err = parseNullULID(workID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) RetireTaste(ctx context.Context, id ulid.ULID, at time.Time) (bool, error) {
	tag, err := s.exec(ctx, `UPDATE tastes SET retired_at = $3 WHERE agent_id = $1 AND id = $2 AND retired_at IS NULL`, id.String(), at)
	if err != nil {
		return false, fmt.Errorf("retire taste: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// Whose is the agent's name and when it was made; false if there's no such agent.
func (s *Store) Whose(ctx context.Context) (string, time.Time, bool, error) {
	var (
		name string
		at   time.Time
	)
	err := s.queryRow(ctx, `SELECT name, created_at FROM agents WHERE id = $1`).Scan(&name, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, fmt.Errorf("whose: %w", err)
	}
	return name, at, true, nil
}

// Week is what an agent did since a time, counted.
type Week struct {
	Made     int `json:"made"`     // works, not counting the journal or reviews
	Decided  int `json:"decided"`  // requests their owner answered
	Approved int `json:"approved"` // of those, the ones they approved
	Answered int `json:"answered"` // calls on their judgment their owner answered
	Guessed  int `json:"guessed"`  // of those, the ones where they'd picked as their owner did
}

// WeekSince counts what the agent did since since.
func (s *Store) WeekSince(ctx context.Context, since time.Time) (Week, error) {
	var w Week
	err := s.queryRow(ctx, `SELECT
		(SELECT count(*) FROM works WHERE agent_id = $1 AND created_at >= $2 AND kind NOT IN ('journal', 'review')),
		(SELECT count(*) FROM approvals WHERE agent_id = $1 AND decided_at >= $2 AND status IN ($3, $4)),
		(SELECT count(*) FROM approvals WHERE agent_id = $1 AND decided_at >= $2 AND status = $3),
		(SELECT count(*) FROM judgment_calls WHERE agent_id = $1 AND answered_at >= $2 AND agreed IS NOT NULL),
		(SELECT count(*) FROM judgment_calls WHERE agent_id = $1 AND answered_at >= $2 AND agreed)`,
		since, memory.ApprovalApproved, memory.ApprovalDenied).Scan(&w.Made, &w.Decided, &w.Approved, &w.Answered, &w.Guessed)
	if err != nil {
		return w, fmt.Errorf("week since: %w", err)
	}
	return w, nil
}

// CallsAskedBetween is how many calls on the agent's judgment were asked of
// its owner in [from, to).
func (s *Store) CallsAskedBetween(ctx context.Context, from, to time.Time) (int, error) {
	var n int
	if err := s.queryRow(ctx, `SELECT count(*) FROM judgment_calls WHERE agent_id = $1 AND asked_at >= $2 AND asked_at < $3`,
		from, to).Scan(&n); err != nil {
		return 0, fmt.Errorf("calls asked: %w", err)
	}
	return n, nil
}
