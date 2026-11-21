package store

import (
	"context"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/memory"
)

type LogEntry struct {
	decision.Decision
	Source, Kind, Text, Speaker string // the percept's; "" for a decision of the agent's own
}

// DecisionLog returns decisions after the given time: oldest first when
// following the log live, newest first for a page of history.
func (s *Store) DecisionLog(ctx context.Context, after time.Time, limit int, newestFirst bool) ([]LogEntry, error) {
	order := "ASC"
	if newestFirst {
		order = "DESC"
	}
	return s.queryLog(ctx, `SELECT `+decisionColumns+`, coalesce(p.source, ''), coalesce(p.kind, ''), coalesce(p.text, ''),
		coalesce(p.speaker, '') FROM decisions d LEFT JOIN percepts p ON p.agent_id = d.agent_id AND p.id = d.percept_id
		WHERE d.agent_id = $1 AND d.at > $2 ORDER BY d.at `+order+`, d.id `+order+` LIMIT $3`, after, limit)
}

// DecisionChain returns the decision with id and every decision about the
// same percept (or, for one of the agent's own, its children), oldest first.
func (s *Store) DecisionChain(ctx context.Context, id ulid.ULID) ([]LogEntry, error) {
	return s.queryLog(ctx, `SELECT `+decisionColumns+`, coalesce(p.source, ''), coalesce(p.kind, ''), coalesce(p.text, ''),
		coalesce(p.speaker, '') FROM decisions d LEFT JOIN percepts p ON p.agent_id = d.agent_id AND p.id = d.percept_id
		WHERE d.agent_id = $1 AND (d.id = $2 OR d.parent_id = $2
		   OR d.percept_id = (SELECT percept_id FROM decisions WHERE agent_id = $1 AND id = $2))
		ORDER BY d.at, d.id`, id.String())
}

func (s *Store) queryLog(ctx context.Context, sql string, args ...any) ([]LogEntry, error) {
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query decision log: %w", err)
	}
	defer rows.Close()
	var out []LogEntry
	for rows.Next() {
		var e LogEntry
		if e.Decision, err = scanDecision(rows, &e.Source, &e.Kind, &e.Text, &e.Speaker); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type Vitals struct {
	Decisions   int
	ByTier      map[string]int
	Escalations int // System 1 was unsure or only fairly sure
	CostUSD     float64
	AvgLatency  time.Duration // of decisions that took any time
}

func (s *Store) Vitals(ctx context.Context, since time.Time) (Vitals, error) {
	v := Vitals{ByTier: map[string]int{}}
	rows, err := s.query(ctx, `SELECT tier, count(*), count(*) FILTER (WHERE action IN ('ask', 'escalate')),
		coalesce(sum(cost_usd), 0), coalesce(avg(latency_ms) FILTER (WHERE latency_ms > 0), 0)
		FROM decisions WHERE agent_id = $1 AND at >= $2 GROUP BY tier`, since)
	if err != nil {
		return v, fmt.Errorf("query vitals: %w", err)
	}
	defer rows.Close()
	var weighted float64
	for rows.Next() {
		var (
			tier       string
			n, esc     int
			cost, avgL float64
		)
		if err := rows.Scan(&tier, &n, &esc, &cost, &avgL); err != nil {
			return v, err
		}
		v.ByTier[tier], v.Decisions, v.Escalations, v.CostUSD = n, v.Decisions+n, v.Escalations+esc, v.CostUSD+cost
		weighted += avgL * float64(n)
	}
	if v.Decisions > 0 {
		v.AvgLatency = time.Duration(weighted / float64(v.Decisions) * float64(time.Millisecond))
	}
	return v, rows.Err()
}

type DayCost struct {
	Day     string // YYYY-MM-DD
	CostUSD float64
	Count   int
}

// CostByDay returns each day since the given time, in zone, oldest first.
func (s *Store) CostByDay(ctx context.Context, since time.Time, zone *time.Location) ([]DayCost, error) {
	rows, err := s.query(ctx, `SELECT to_char(at AT TIME ZONE $3, 'YYYY-MM-DD') AS day,
		coalesce(sum(cost_usd), 0), count(*) FROM decisions WHERE agent_id = $1 AND at >= $2 GROUP BY day ORDER BY day`,
		since, zone.String())
	if err != nil {
		return nil, fmt.Errorf("query cost by day: %w", err)
	}
	defer rows.Close()
	var out []DayCost
	for rows.Next() {
		var d DayCost
		if err := rows.Scan(&d.Day, &d.CostUSD, &d.Count); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

type FactView struct {
	memory.Fact
	Public  *bool // whether it may go on the profile; nil: not decided (or not its own)
	AskedAt *time.Time
	From    string // the kind of percept it was learnt from (a message, an activity, ...)
}

// AllFacts returns up to limit facts, newest first.
func (s *Store) AllFacts(ctx context.Context, limit int) ([]FactView, error) {
	rows, err := s.query(ctx, `SELECT f.id, f.percept_id, f.subject, f.text, f.at, f.areas, f.public, f.asked_at, coalesce(p.kind, '')
		FROM facts f LEFT JOIN percepts p ON p.agent_id = f.agent_id AND p.id = f.percept_id
		WHERE f.agent_id = $1 ORDER BY f.at DESC, f.id DESC LIMIT $2`, limit)
	if err != nil {
		return nil, fmt.Errorf("query facts: %w", err)
	}
	defer rows.Close()
	var out []FactView
	for rows.Next() {
		var (
			f             FactView
			id, perceptID string
		)
		if err := rows.Scan(&id, &perceptID, &f.Subject, &f.Text, &f.At, &f.Areas, &f.Public, &f.AskedAt, &f.From); err != nil {
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

// AllPeople returns everyone the agent knows, oldest acquaintance first.
func (s *Store) AllPeople(ctx context.Context, limit int) ([]memory.Person, error) {
	return s.queryPeople(ctx, `SELECT `+personColumns+` FROM people WHERE agent_id = $1 ORDER BY first_seen, key LIMIT $2`, limit)
}

// AllProjects returns up to limit projects, most recently worked on first.
func (s *Store) AllProjects(ctx context.Context, limit int) ([]memory.Project, error) {
	return s.queryProjects(ctx, projectSelect+` ORDER BY p.updated_at DESC, p.id LIMIT $2`, limit)
}

// RecentWorks returns up to limit works, newest first.
func (s *Store) RecentWorks(ctx context.Context, limit int) ([]memory.Work, error) {
	return s.queryWorks(ctx, `SELECT `+workColumns+` FROM works WHERE agent_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, limit)
}

type Counts struct {
	Facts, SelfFacts, People, Intentions, Projects, Works, Percepts, Decisions int
}

func (s *Store) Counts(ctx context.Context) (Counts, error) {
	var c Counts
	err := s.queryRow(ctx, `SELECT
		(SELECT count(*) FROM facts WHERE agent_id = $1), (SELECT count(*) FROM facts WHERE agent_id = $1 AND subject = $2),
		(SELECT count(*) FROM people WHERE agent_id = $1),
		(SELECT count(*) FROM intentions WHERE agent_id = $1 AND status = 'pending'),
		(SELECT count(*) FROM projects WHERE agent_id = $1 AND status = 'active'),
		(SELECT count(*) FROM works WHERE agent_id = $1), (SELECT count(*) FROM percepts WHERE agent_id = $1),
		(SELECT count(*) FROM decisions WHERE agent_id = $1)`, memory.SubjectSelf).
		Scan(&c.Facts, &c.SelfFacts, &c.People, &c.Intentions, &c.Projects, &c.Works, &c.Percepts, &c.Decisions)
	if err != nil {
		return c, fmt.Errorf("query counts: %w", err)
	}
	return c, nil
}

func (s *Store) Work(ctx context.Context, id ulid.ULID) (memory.Work, bool, error) {
	ws, err := s.queryWorks(ctx, `SELECT `+workColumns+` FROM works WHERE agent_id = $1 AND id = $2`, id.String())
	if err != nil || len(ws) == 0 {
		return memory.Work{}, false, err
	}
	return ws[0], true, nil
}
