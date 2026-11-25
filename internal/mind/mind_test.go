package mind

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/character"
	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/decision/questions"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/system1"
	"github.com/Grandbusta/tinio/internal/providers/system2"
	"github.com/Grandbusta/tinio/internal/render"
	"github.com/Grandbusta/tinio/internal/render/midi"
	"github.com/Grandbusta/tinio/internal/render/pdf"
	"github.com/Grandbusta/tinio/internal/search"
	"github.com/Grandbusta/tinio/internal/usage"
)

type fakeStore struct {
	standing   usage.Standing
	seen       map[string]bool
	decisions  []decision.Decision
	exchanges  []memory.Exchange
	facts      []memory.Fact // newest first
	people     map[string]memory.Person
	intentions []memory.Intention
	actions    []memory.Action
	projects   map[ulid.ULID]memory.Project
	works      []memory.Work
	spent      float64 // what CostSince returns
	convs      []memory.Conversation
	percepts   []perception.Percept
	asked      map[ulid.ULID]time.Time
	approvals  []memory.Approval
	rules      map[string]string // autonomy rules: class → verdict
	labels     []memory.Label
	lastRead   ulid.ULID // what LastRead returns
	skills     []memory.Skill
	evidence   []memory.Evidence // oldest first
	lessons    []memory.Lesson
	principles []memory.Principle
	cases      []memory.JudgmentCase
	calls      []memory.Call
	public     map[ulid.ULID]string // works set public: "true by owner", ...
	topics     []memory.Topic
	mornings   map[string]memory.Morning
}

// keep returns the xs that match, at most limit of them (all, if limit < 0).
func keep[T any](xs []T, limit int, match func(T) bool) []T {
	var out []T
	for _, x := range xs {
		if match(x) && (limit < 0 || len(out) < limit) {
			out = append(out, x)
		}
	}
	return out
}

func find[T any](xs []T, match func(T) bool) (T, bool) {
	if i := slices.IndexFunc(xs, match); i >= 0 {
		return xs[i], true
	}
	var zero T
	return zero, false
}

func reversed[T any](xs []T) []T {
	out := slices.Clone(xs)
	slices.Reverse(out)
	return out
}

// update calls f on each x that matches.
func update[T any](xs []T, match func(T) bool, f func(*T)) {
	for i := range xs {
		if match(xs[i]) {
			f(&xs[i])
		}
	}
}

// upsert replaces the x that matches, or adds it.
func upsert[T any](xs []T, x T, match func(T) bool) []T {
	if i := slices.IndexFunc(xs, match); i >= 0 {
		xs[i] = x
		return xs
	}
	return append(xs, x)
}

func (s *fakeStore) SaveAutonomyRule(_ context.Context, class, verdict, _ string, _ ulid.ULID, _ time.Time) error {
	if s.rules == nil {
		s.rules = map[string]string{}
	}
	s.rules[class] = verdict
	return nil
}

func (s *fakeStore) AutonomyRules(context.Context) (map[string]string, error) { return s.rules, nil }

func (s *fakeStore) DeleteFact(_ context.Context, id ulid.ULID) (bool, error) {
	for n, f := range s.facts {
		if f.ID == id {
			s.facts = append(s.facts[:n], s.facts[n+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeStore) SaveLabel(_ context.Context, l memory.Label) error {
	s.labels = append(s.labels, l)
	return nil
}

func (s *fakeStore) LastRead(context.Context, string, string, string, time.Time) (ulid.ULID, bool, error) {
	return s.lastRead, !s.lastRead.IsZero(), nil
}

func (s *fakeStore) AddSkill(_ context.Context, sk memory.Skill) error {
	if !slices.ContainsFunc(s.skills, func(x memory.Skill) bool { return x.Name == sk.Name }) {
		s.skills = append(s.skills, sk)
	}
	return nil
}

func (s *fakeStore) Skills(context.Context) ([]memory.Skill, error) { return s.skills, nil }

func (s *fakeStore) SetSkillLevel(_ context.Context, name string, level float64, at time.Time) error {
	update(s.skills, func(x memory.Skill) bool { return x.Name == name }, func(x *memory.Skill) { x.Level, x.UpdatedAt = level, at })
	return nil
}

func (s *fakeStore) SaveEvidence(_ context.Context, e memory.Evidence) error {
	s.evidence = append(s.evidence, e)
	return nil
}

func (s *fakeStore) RecentEvidence(_ context.Context, skill string, limit int) ([]memory.Evidence, error) {
	return keep(reversed(s.evidence), limit, func(e memory.Evidence) bool { return e.Skill == skill }), nil
}

func (s *fakeStore) SaveLesson(_ context.Context, l memory.Lesson) error {
	s.lessons = append(s.lessons, l)
	return nil
}

func (s *fakeStore) Lessons(_ context.Context, skill string, limit int) ([]memory.Lesson, error) {
	return keep(reversed(s.lessons), limit, func(l memory.Lesson) bool { return l.RetiredAt.IsZero() && (skill == "" || l.Skill == skill) }), nil
}

func (s *fakeStore) RetireLesson(_ context.Context, id ulid.ULID, at time.Time) error {
	update(s.lessons, func(x memory.Lesson) bool { return x.ID == id }, func(x *memory.Lesson) { x.RetiredAt = at })
	return nil
}

func (s *fakeStore) RecentFound(context.Context, int) ([]search.Result, error) { return nil, nil }

func (s *fakeStore) AwaitIntention(_ context.Context, id ulid.ULID) error {
	update(s.intentions, func(x memory.Intention) bool { return x.ID == id && x.Status == memory.IntentionPending },
		func(x *memory.Intention) { x.Status = memory.IntentionAwaiting })
	return nil
}

func (s *fakeStore) ResumeIntention(_ context.Context, id ulid.ULID, notBefore time.Time) error {
	for n := range s.intentions {
		if i := &s.intentions[n]; i.ID == id && i.Status == memory.IntentionAwaiting {
			i.Status = memory.IntentionPending
			if i.DueAt.Before(notBefore) {
				i.DueAt = notBefore
			}
		}
	}
	return nil
}

func (s *fakeStore) SaveApproval(_ context.Context, a memory.Approval) error {
	s.approvals = append(s.approvals, a)
	return nil
}

func (s *fakeStore) Approval(_ context.Context, id ulid.ULID) (memory.Approval, bool, error) {
	a, ok := find(s.approvals, func(a memory.Approval) bool { return a.ID == id })
	return a, ok, nil
}

func (s *fakeStore) UnsentApprovals(_ context.Context, limit int) ([]memory.Approval, error) {
	return keep(s.approvals, limit, func(a memory.Approval) bool { return a.Status == memory.ApprovalPending && a.SentAt.IsZero() }), nil
}

func (s *fakeStore) MarkApprovalSent(_ context.Context, id ulid.ULID, message []byte, at time.Time) error {
	update(s.approvals, func(x memory.Approval) bool { return x.ID == id },
		func(x *memory.Approval) { x.SentAt, x.Message = at, message })
	return nil
}

func (s *fakeStore) DecideApproval(_ context.Context, id ulid.ULID, status, text string, at time.Time) (bool, error) {
	for n := range s.approvals {
		if a := &s.approvals[n]; a.ID == id && a.Status == memory.ApprovalPending {
			a.Status, a.DecidedAt = status, at
			if text != "" {
				a.Text = text
			}
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeStore) StaleApprovals(_ context.Context, before time.Time, limit int) ([]memory.Approval, error) {
	return keep(s.approvals, limit, func(a memory.Approval) bool { return a.Status == memory.ApprovalPending && a.AskedAt.Before(before) }), nil
}

func (s *fakeStore) SaveIntention(_ context.Context, i memory.Intention) error {
	s.intentions = append(s.intentions, i)
	return nil
}

func (s *fakeStore) DueIntentions(_ context.Context, now time.Time, limit int) ([]memory.Intention, error) {
	return keep(s.intentions, limit, func(i memory.Intention) bool { return i.Status == memory.IntentionPending && !i.DueAt.After(now) }), nil
}

func (s *fakeStore) IntentionsDue(_ context.Context, from, to time.Time) ([]memory.Intention, error) {
	out := keep(s.intentions, -1, func(i memory.Intention) bool { return !i.DueAt.Before(from) && i.DueAt.Before(to) })
	slices.SortStableFunc(out, func(a, b memory.Intention) int { return a.DueAt.Compare(b.DueAt) })
	return out, nil
}

func (s *fakeStore) FinishIntention(_ context.Context, id ulid.ULID, status, result string, at time.Time) error {
	update(s.intentions, func(x memory.Intention) bool { return x.ID == id },
		func(x *memory.Intention) { x.Status, x.Result, x.DoneAt = status, result, at })
	return nil
}

func (s *fakeStore) RetryIntention(_ context.Context, id ulid.ULID, due time.Time) error {
	update(s.intentions, func(x memory.Intention) bool { return x.ID == id }, func(x *memory.Intention) { x.DueAt, x.Attempts = due, x.Attempts+1 })
	return nil
}

func (s *fakeStore) PostponeIntention(_ context.Context, id ulid.ULID, due time.Time) error {
	update(s.intentions, func(x memory.Intention) bool { return x.ID == id }, func(x *memory.Intention) { x.DueAt = due })
	return nil
}

func (s *fakeStore) SetIntentionArg(_ context.Context, id ulid.ULID, key, value string) error {
	update(s.intentions, func(x memory.Intention) bool { return x.ID == id }, func(x *memory.Intention) {
		x.Args = maps.Clone(x.Args)
		if x.Args == nil {
			x.Args = map[string]string{}
		}
		x.Args[key] = value
	})
	return nil
}

func (s *fakeStore) intention(id ulid.ULID) memory.Intention {
	i, _, _ := s.Intention(context.Background(), id)
	return i
}

func (s *fakeStore) SaveProject(_ context.Context, p memory.Project) error {
	s.projects[p.ID] = p
	return nil
}

func (s *fakeStore) Project(_ context.Context, id ulid.ULID) (memory.Project, bool, error) {
	p, ok := s.projects[id]
	return p, ok, nil
}

func (s *fakeStore) ActiveProjects(_ context.Context, limit int) ([]memory.Project, error) {
	out := keep(slices.Collect(maps.Values(s.projects)), limit, func(p memory.Project) bool { return p.Status == memory.ProjectActive })
	slices.SortFunc(out, func(a, b memory.Project) int { return strings.Compare(a.Title, b.Title) })
	return out, nil
}

func (s *fakeStore) SaveWork(_ context.Context, w memory.Work) error {
	s.works = append(s.works, w)
	return nil
}

func (s *fakeStore) WorksBetween(_ context.Context, from, to time.Time) ([]memory.Work, error) {
	return keep(s.works, -1, func(w memory.Work) bool { return !w.CreatedAt.Before(from) && w.CreatedAt.Before(to) }), nil
}

func (s *fakeStore) ProjectWorks(_ context.Context, project ulid.ULID, limit int) ([]memory.Work, error) {
	out := keep(s.works, -1, func(w memory.Work) bool { return w.ProjectID == project })
	return out[max(0, len(out)-limit):], nil
}

func (s *fakeStore) OpenIntentions(_ context.Context, subject, kind string) ([]memory.Intention, error) {
	return keep(s.intentions, -1, func(i memory.Intention) bool {
		return i.Subject == subject && i.Kind == kind && i.Status == memory.IntentionPending
	}), nil
}

func (s *fakeStore) LastFrom(_ context.Context, source string) (string, bool, error) {
	for n := len(s.percepts) - 1; n >= 0; n-- {
		if s.percepts[n].Source == source {
			return s.percepts[n].Text, true, nil
		}
	}
	return "", false, nil
}

func (s *fakeStore) SeenFrom(_ context.Context, source string) (bool, error) {
	for _, p := range s.percepts {
		if p.Source == source {
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeStore) SaveAction(_ context.Context, a memory.Action) error {
	s.actions = append(s.actions, a)
	return nil
}

func (s *fakeStore) SetAction(_ context.Context, id ulid.ULID, from []string, status, result string, at time.Time) (bool, error) {
	for n := range s.actions {
		if a := &s.actions[n]; a.ID == id && slices.Contains(from, a.Status) {
			a.Status, a.Result, a.DoneAt = status, result, at
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeStore) Actions(_ context.Context, statuses []string, limit int) ([]memory.Action, error) {
	var out []memory.Action
	for _, a := range s.actions {
		if slices.Contains(statuses, a.Status) && len(out) < limit {
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *fakeStore) ActionsSince(_ context.Context, since time.Time) (int, error) {
	n := 0
	for _, a := range s.actions {
		if !a.ProposedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) Work(_ context.Context, id ulid.ULID) (memory.Work, bool, error) {
	w, ok := find(s.works, func(w memory.Work) bool { return w.ID == id })
	return w, ok, nil
}

func (s *fakeStore) SetWorkData(_ context.Context, id ulid.ULID, kind string, data json.RawMessage) error {
	for n := range s.works {
		if s.works[n].ID == id {
			s.works[n].Data, s.works[n].DataKind = data, kind
		}
	}
	return nil
}

func (s *fakeStore) WorksMissingData(_ context.Context, kinds []string, limit int) ([]memory.Work, error) {
	var out []memory.Work
	for n := len(s.works) - 1; n >= 0 && len(out) < limit; n-- {
		if w := s.works[n]; len(w.Data) == 0 && slices.Contains(kinds, w.Kind) {
			out = append(out, w)
		}
	}
	return out, nil
}

func (s *fakeStore) WorksFor(_ context.Context, person string, limit int) ([]memory.Work, error) {
	var out []memory.Work
	for n := len(s.works) - 1; n >= 0 && len(out) < limit; n-- {
		w := s.works[n]
		if i, ok, _ := s.Intention(context.Background(), w.IntentionID); ok {
			if i.Subject != memory.SubjectSelf && i.Subject != person {
				continue
			}
			w.MadeFor = i.Subject
		}
		out = append(out, w)
	}
	slices.SortStableFunc(out, func(a, b memory.Work) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, nil
}

func (s *fakeStore) SaveWorkEmbedding(_ context.Context, id ulid.ULID, v []float32, model string) error {
	update(s.works, func(x memory.Work) bool { return x.ID == id }, func(x *memory.Work) { x.Embedding, x.EmbeddedWith = v, model })
	return nil
}

func (s *fakeStore) Intention(_ context.Context, id ulid.ULID) (memory.Intention, bool, error) {
	i, ok := find(s.intentions, func(i memory.Intention) bool { return i.ID == id })
	return i, ok, nil
}

func (s *fakeStore) IntentionWork(_ context.Context, id ulid.ULID) (memory.Work, bool, error) {
	w, ok := find(reversed(s.works), func(w memory.Work) bool { return w.IntentionID == id })
	return w, ok, nil
}

func (s *fakeStore) LatestWork(_ context.Context, kind string) (memory.Work, bool, error) {
	w, ok := find(reversed(s.works), func(w memory.Work) bool { return w.Kind == kind })
	return w, ok, nil
}

func (s *fakeStore) CostSince(context.Context, time.Time, []string) (float64, error) {
	return s.spent, nil
}

func (s *fakeStore) Conversations(context.Context) ([]memory.Conversation, error) {
	return s.convs, nil
}

func (s *fakeStore) Standing(context.Context) (usage.Standing, error) { return s.standing, nil }

func (s *fakeStore) LastPercept(_ context.Context, person, kind string) (time.Time, bool, error) {
	var last time.Time
	for _, p := range s.percepts {
		if p.Speaker == person && p.Kind == kind && p.At.After(last) {
			last = p.At
		}
	}
	return last, !last.IsZero(), nil
}

func (s *fakeStore) UnaskedFacts(_ context.Context, person string, since time.Time, limit int) ([]memory.Fact, error) {
	return keep(s.facts, limit, func(f memory.Fact) bool {
		_, asked := s.asked[f.ID]
		return !asked && f.Subject == person && !f.At.Before(since)
	}), nil
}

func (s *fakeStore) MarkFactsAsked(_ context.Context, ids []ulid.ULID, at time.Time) error {
	for _, id := range ids {
		s.asked[id] = at
	}
	return nil
}

func (s *fakeStore) SaveFact(_ context.Context, f memory.Fact) error {
	s.facts = append([]memory.Fact{f}, s.facts...)
	return nil
}

// RecallCandidates mimics the store: only facts about subjects; those sharing
// an area first, then the rest, newest first within each group (s.facts is
// kept newest first).
func (s *fakeStore) RecallCandidates(_ context.Context, subjects, areas []string, limit int) ([]memory.Fact, error) {
	var same, rest []memory.Fact
	for _, f := range s.facts {
		if !slices.Contains(subjects, f.Subject) {
			continue
		}
		if slices.ContainsFunc(f.Areas, func(a string) bool { return slices.Contains(areas, a) }) {
			same = append(same, f)
		} else {
			rest = append(rest, f)
		}
	}
	all := append(same, rest...)
	return all[:min(limit, len(all))], nil
}

func (s *fakeStore) Person(_ context.Context, key string) (memory.Person, bool, error) {
	p, ok := s.people[key]
	return p, ok, nil
}

func (s *fakeStore) KnownPeople(_ context.Context, limit int) ([]memory.Person, error) {
	out := keep(slices.Collect(maps.Values(s.people)), limit, func(p memory.Person) bool { return p.Relation != "" })
	slices.SortFunc(out, func(a, b memory.Person) int { return strings.Compare(a.Key, b.Key) })
	return out, nil
}

func (s *fakeStore) SavePerson(_ context.Context, p memory.Person) error {
	s.people[p.Key] = p
	return nil
}

func (s *fakeStore) AddPersonIfNew(_ context.Context, p memory.Person) (bool, error) {
	if _, ok := s.people[p.Key]; ok {
		return false, nil
	}
	s.people[p.Key] = p
	return true, nil
}

func (s *fakeStore) UnlabelledFacts(context.Context, int) ([]memory.Fact, error) { return nil, nil }

func (s *fakeStore) SetFactAreas(context.Context, ulid.ULID, []string) error { return nil }

func (s *fakeStore) RecentExchanges(context.Context, string, string, time.Time, ulid.ULID, int) ([]memory.Exchange, error) {
	return s.exchanges, nil
}

func (s *fakeStore) SavePercept(_ context.Context, p perception.Percept) (bool, error) {
	if s.seen[p.ExternalID] {
		return false, nil
	}
	s.seen[p.ExternalID] = true
	s.percepts = append(s.percepts, p)
	return true, nil
}

func (s *fakeStore) SaveDecision(_ context.Context, d decision.Decision) error {
	s.decisions = append(s.decisions, d)
	return nil
}

type fakeReplier struct {
	sent   []string
	topics []string // the topic each reply was said in
	err    error
}

func (r *fakeReplier) Reply(_ context.Context, p perception.Percept, text string) error {
	r.sent = append(r.sent, text)
	r.topics = append(r.topics, p.Topic)
	return r.err
}

// sayingReplier is a fakeReplier whose chat shows what goes with a reply.
type sayingReplier struct {
	fakeReplier
	said []Said
}

func (r *sayingReplier) ReplyWith(ctx context.Context, p perception.Percept, text string, s Said) error {
	r.said = append(r.said, s)
	return r.Reply(ctx, p, text)
}

func (r *fakeReplier) Draft(_ context.Context, _ perception.Percept, text string) error {
	r.sent = append(r.sent, "draft: "+text)
	return nil
}

// fakeSystem1 may be called from two goroutines at once.
type fakeSystem1 struct {
	choice      string
	confidence  float64
	refused     bool
	err         error
	relevant    map[string]bool // fact text → relevant
	recallErr   error
	mu          sync.Mutex
	recallCalls []system1.Request
	scores      map[string]float64 // work title → level index (0-9), for skill scoring
	scoreCalls  []system1.Request
	stageMet    map[string]float64 // work title → how sure it meets the stage
	stageCalls  []system1.Request
	filed       []string           // what life_area was asked about
	inTopic     map[string]float64 // topic id → how sure a message carries on with it
	starts      float64            // how sure a message starts a topic
	topicCalls  []system1.Request
}

func (f *fakeSystem1) Decide(_ context.Context, req system1.Request) (system1.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if state, ok := req.State.(map[string]any); ok && state["skill"] != nil {
		f.scoreCalls = append(f.scoreCalls, req)
		title := state["work"].(map[string]string)["title"]
		return system1.Response{Provider: "fake1/1", CostUSD: 0.00002, Answers: map[string]decision.Answer{
			"level": {Type: decision.QuestionScore, Score: f.scores[title]},
		}}, nil
	}
	if state, ok := req.State.(map[string]any); ok && state["stage"] != nil {
		f.stageCalls = append(f.stageCalls, req)
		title := state["work"].(map[string]string)["title"]
		return system1.Response{Provider: "fake1/1", CostUSD: 0.00002, Answers: map[string]decision.Answer{
			"met": {Type: decision.QuestionPredicate, Probability: f.stageMet[title]},
		}}, nil
	}
	if state, ok := req.State.(map[string]any); ok && state["relation"] != nil {
		relation := state["relation"].(string)
		circle := "other"
		if strings.Contains(relation, "father") || strings.Contains(relation, "cousin") {
			circle = "family"
		}
		return system1.Response{Provider: "fake1/1", CostUSD: 0.00003, Answers: map[string]decision.Answer{
			"circle": {Type: decision.QuestionChoice, Choice: circle, Confidence: 0.95},
		}}, nil
	}
	if state, ok := req.State.(map[string]any); ok && state["text"] != nil {
		f.filed = append(f.filed, state["text"].(string))
		return system1.Response{Provider: "fake1/1", CostUSD: 0.00004, Answers: map[string]decision.Answer{
			"area":     {Type: decision.QuestionChoice, Probabilities: map[string]float64{"other": 1}},
			"personal": {Type: decision.QuestionPredicate, Probability: 1},
			"broad":    {Type: decision.QuestionPredicate},
		}}, nil
	}
	if state, ok := req.State.(map[string]any); ok && state["facts"] != nil {
		f.recallCalls = append(f.recallCalls, req)
		if f.recallErr != nil {
			return system1.Response{}, f.recallErr
		}
		texts := state["facts"].([]string)
		answers := map[string]decision.Answer{}
		for _, q := range req.Questions {
			var at int
			fmt.Sscanf(q.Instructions[strings.Index(q.Instructions, "`facts[")+len("`facts["):], "%d", &at)
			p := 0.0
			if f.relevant[texts[at]] {
				p = 0.9
			}
			answers[q.Name] = decision.Answer{Type: decision.QuestionPredicate, Probability: p}
		}
		return system1.Response{Provider: "fake1/1", CostUSD: 0.00003, Answers: answers}, nil
	}
	if state, ok := req.State.(map[string]any); ok && state["topics"] != nil {
		f.topicCalls = append(f.topicCalls, req)
		answers := map[string]decision.Answer{"starts": {Type: decision.QuestionPredicate, Probability: f.starts}}
		for _, q := range req.Questions {
			if id, ok := strings.CutPrefix(q.Name, "in:"); ok {
				answers[q.Name] = decision.Answer{Type: decision.QuestionPredicate, Probability: f.inTopic[id]}
			}
		}
		return system1.Response{Provider: "fake1/1", CostUSD: 0.00002, Answers: answers}, nil
	}
	if req.Questions[0].Name == "answers_previous" {
		return system1.Response{Provider: "fake1/1", CostUSD: 0.00002, Answers: map[string]decision.Answer{
			"answers_previous": {Type: decision.QuestionPredicate},
		}}, nil
	}
	if f.err != nil {
		return system1.Response{}, f.err
	}
	return system1.Response{
		Provider: "fake1/1",
		CostUSD:  0.00001,
		Answers: map[string]decision.Answer{
			"intent": {Type: decision.QuestionChoice, Choice: f.choice, Confidence: f.confidence, Refused: f.refused},
		},
	}, nil
}

type fakeSystem2 struct {
	err        error
	remember   []system2.Memory
	whoTheyAre string
	reminders  []system2.Reminder
	promises   []system2.Promise
	lessons    []system2.LessonLearned
	feedback   []system2.Feedback
	send       system2.Send
	moves      []system2.Move
	acts       []system2.Act
	looks      [][]system2.Look  // each reply call's looks, in turn, before it replies
	formats    map[string]string // reply text by Format.Name, for structured requests
	topic      string            // the name it gives a topic
	answers    []string          // answers to tap that it offers
	drafts     []string          // told to a request's Draft, in turn
	requests   []system2.Request
}

func (f *fakeSystem2) Reason(_ context.Context, req system2.Request) (system2.Response, error) {
	f.requests = append(f.requests, req)
	for _, d := range f.drafts {
		if req.Draft != nil {
			req.Draft(d)
		}
	}
	if f.err != nil {
		return system2.Response{Provider: "fake2/1", CostUSD: 0.002}, f.err // what failing cost
	}
	if req.Format != nil {
		return system2.Response{Provider: "fake2/1", Text: f.formats[req.Format.Name], CostUSD: 0.002}, nil
	}
	if len(f.looks) > 0 && req.CollectFacts {
		looks := f.looks[0]
		f.looks = f.looks[1:]
		return system2.Response{Provider: "fake2/1", Looks: looks, CostUSD: 0.001}, nil
	}
	return system2.Response{Provider: "fake2/1", Text: "a thoughtful reply", Remember: f.remember, WhoTheyAre: f.whoTheyAre,
		Reminders: f.reminders, Promises: f.promises, Lessons: f.lessons, Feedback: f.feedback, Send: f.send, Moves: f.moves, Acts: f.acts, Topic: f.topic, Answers: f.answers, CostUSD: 0.001}, nil
}

var testSet = questions.Set{
	ID: "incoming_message", Version: 1, Primary: "intent",
	Questions: []system1.Question{{Name: "intent", Type: decision.QuestionChoice, Instructions: "?",
		Choices: []system1.Choice{{Value: "remember"}, {Value: "question"}, {Value: "other"}}}},
	Thresholds: questions.Thresholds{Act: 0.85, Ask: 0.5},
}

var testFactRelevance = questions.Set{
	ID: "fact_relevance", Version: 1, Primary: "relevant",
	Questions:  []system1.Question{{Name: "relevant", Type: decision.QuestionPredicate, Instructions: "Would {fact} help with `message`?"}},
	Thresholds: questions.Thresholds{Act: 0.5, Ask: 0.5},
}

var testLifeArea = questions.Set{
	ID: "life_area", Version: 1,
	Questions: []system1.Question{
		{Name: "area", Type: decision.QuestionChoice, Instructions: "?",
			Choices: []system1.Choice{{Value: "identity"}, {Value: "home"}, {Value: "family"}, {Value: "religion"}, {Value: "other"}}},
		{Name: "personal", Type: decision.QuestionPredicate, Instructions: "?"},
		{Name: "broad", Type: decision.QuestionPredicate, Instructions: "?"},
	},
	Thresholds: questions.Thresholds{Act: 0.5, Ask: 0.5, Label: 0.15},
}

var testPersonCircle = questions.Set{
	ID: "person_circle", Version: 1, Primary: "circle",
	Questions: []system1.Question{{Name: "circle", Type: decision.QuestionChoice,
		Instructions: "Which circle of {agent}'s life?",
		Choices:      []system1.Choice{{Value: "family", Description: "{agent}'s relatives"}, {Value: "other"}}}},
	Thresholds: questions.Thresholds{Act: 0.5, Ask: 0.5},
}

var testOpenThreads = questions.Set{
	ID: "open_threads", Version: 1, Primary: "open",
	Questions:  []system1.Question{{Name: "open", Type: decision.QuestionPredicate, Instructions: "Is {fact} going on for {speaker}?"}},
	Thresholds: questions.Thresholds{Act: 0.5, Ask: 0.5},
}

var testFollowUp = questions.Set{
	ID: "follow_up", Version: 1, Primary: "answers_previous",
	Questions:  []system1.Question{{Name: "answers_previous", Type: decision.QuestionPredicate, Instructions: "?"}},
	Thresholds: questions.Thresholds{Act: 0.5, Ask: 0.5},
}

var testLife = character.Character{Name: "Tobi", Story: []string{"Tobi is 29 years old.", "Tobi is a Catholic."}}

func newMind(r Replier, s1 system1.DecisionModel, s2 system2.Reasoner) (*Mind, *fakeStore) {
	s := &fakeStore{seen: map[string]bool{}, people: map[string]memory.Person{}, asked: map[ulid.ULID]time.Time{},
		projects: map[ulid.ULID]memory.Project{}}
	return New(Config{
		Life:    testLife,
		Formats: render.NewSet(pdf.Format{}, midi.Format{}),
		Store:   s, Replier: r, System1: s1, System2: s2, Incoming: testSet, FollowUp: testFollowUp, LifeArea: testLifeArea,
		FactRelevance: testFactRelevance, RecallCandidates: 30, PersonCircle: testPersonCircle, OpenThreads: testOpenThreads,
		ReplyPrompt: Prompt{Ref: "reply@test", Text: "You are {name}, talking with {speaker}. {who_they_are}\n{background}"},
		Working:     WorkingMemory{Turns: 8, Window: 2 * time.Hour},
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}), s
}

var now = time.Date(2026, 10, 27, 13, 16, 0, 0, time.UTC)

const sam = "test:sam"

func percept(externalID, text string) perception.Percept {
	return perception.Percept{ID: ulid.Make(), Source: "test", ExternalID: externalID, Thread: "chat-1",
		Speaker: sam, SpeakerName: "Sam", Kind: "message", Text: text, At: now}
}

func TestRouting(t *testing.T) {
	cases := []struct {
		name        string
		s1          *fakeSystem1
		s2          *fakeSystem2
		wantActions []string // root first, then the System 2 child if any
		wantReply   string
	}{
		{"confident share is answered in his own words", &fakeSystem1{choice: "share", confidence: 0.93}, &fakeSystem2{},
			[]string{"share", "reply"}, "a thoughtful reply"},
		{"middling goes to System 2", &fakeSystem1{choice: "remember", confidence: 0.6}, &fakeSystem2{},
			[]string{"ask", "reply"}, "a thoughtful reply"},
		{"unsure escalates to System 2", &fakeSystem1{choice: "remember", confidence: 0.2}, &fakeSystem2{},
			[]string{"escalate", "reply"}, "a thoughtful reply"},
		{"refusal escalates to System 2", &fakeSystem1{refused: true}, &fakeSystem2{},
			[]string{"escalate", "reply"}, "a thoughtful reply"},
		{"System 1 error is recorded", &fakeSystem1{err: errors.New("boom")}, &fakeSystem2{},
			[]string{"error"}, "couldn't think"},
		{"System 2 error is recorded", &fakeSystem1{choice: "question", confidence: 0.9}, &fakeSystem2{err: errors.New("boom")},
			[]string{"question", "error"}, "couldn't think"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &fakeReplier{}
			m, s := newMind(r, tc.s1, tc.s2)
			p := percept("1", "what is the capital of France?")

			m.handle(context.Background(), p)

			if len(r.sent) != 1 || !strings.Contains(r.sent[0], tc.wantReply) {
				t.Fatalf("replies = %q, want one containing %q", r.sent, tc.wantReply)
			}
			last := s.decisions[len(s.decisions)-1]
			if last.Reply != r.sent[0] {
				t.Errorf("acting decision's reply = %q, want what was sent, %q", last.Reply, r.sent[0])
			}
			if len(s.decisions) != len(tc.wantActions) {
				t.Fatalf("decisions = %d, want %d", len(s.decisions), len(tc.wantActions))
			}
			for i, want := range tc.wantActions {
				if s.decisions[i].Action != want {
					t.Errorf("decision %d action = %q, want %q", i, s.decisions[i].Action, want)
				}
			}
			root := s.decisions[0]
			if root.Tier != decision.TierSystem1 || root.SetID != "incoming_message@1,life_area@1" || root.PerceptID != p.ID || root.ParentID != nil {
				t.Errorf("root = %+v", root)
			}
			if len(s.decisions) == 2 {
				child := s.decisions[1]
				if child.ParentID == nil || *child.ParentID != root.ID || child.Tier != decision.TierSystem2 ||
					child.SetID != "reply@test" || child.PerceptID != p.ID {
					t.Errorf("child = %+v", child)
				}
			}
		})
	}
}

func TestPeople(t *testing.T) {
	t.Run("known by account, not by name", func(t *testing.T) {
		s2 := &fakeSystem2{}
		m, s := newMind(&fakeReplier{}, &fakeSystem1{choice: "question", confidence: 0.9}, s2)
		m.Life.Family = []character.Relative{{Name: "Sam", Relation: "father", Account: sam}}
		m.bornKnowing(context.Background())

		m.handle(context.Background(), percept("1", "Who am I to you?"))
		impostor := percept("2", "Who am I to you?")
		impostor.Speaker = "test:another-sam" // same name, different account
		m.handle(context.Background(), impostor)

		if got := s2.requests[0].Instructions; !strings.Contains(got, "This is certain: Sam is Tobi's father.") {
			t.Errorf("father not known: %q", got)
		}
		if got := s2.requests[1].Instructions; strings.Contains(got, "This is certain") {
			t.Errorf("a shared name was taken for the father: %q", got)
		}
		if met := s.people["test:another-sam"]; met.Source != memory.KnownByMeeting || met.Circle != memory.CircleStranger {
			t.Errorf("new person not remembered as a stranger met: %+v", met)
		}
	})

	t.Run("a claim never overwrites what it was born knowing", func(t *testing.T) {
		s2 := &fakeSystem2{whoTheyAre: "Sam is Tobi's cousin."}
		m, s := newMind(&fakeReplier{}, &fakeSystem1{choice: "chat", confidence: 0.9}, s2)
		m.Life.Family = []character.Relative{{Name: "Sam", Relation: "father", Account: sam}}
		m.bornKnowing(context.Background())

		m.handle(context.Background(), percept("1", "Actually I'm your cousin"))

		if s.people[sam].Relation != "Sam is Tobi's father." {
			t.Errorf("born knowledge overwritten: %+v", s.people[sam])
		}
	})
}

// System 2 files what it learns as it writes it: System 1 files a fact only
// when System 2 named no area it knows.
func TestSystem2FilesWhatItLearns(t *testing.T) {
	s1 := &fakeSystem1{choice: "remember", confidence: 0.22}
	m, s := newMind(&fakeReplier{}, s1, &fakeSystem2{remember: []system2.Memory{
		{About: system2.AboutSpeaker, Text: "Sam goes to Grace Chapel", Areas: []string{"religion", "home", "religion"}},
		{About: system2.AboutSpeaker, Text: "Sam's cat is called Biscuit", Areas: []string{"pets"}},
	}})

	m.handle(context.Background(), percept("1", "I go to Grace Chapel, and my cat is called Biscuit."))

	areas := map[string][]string{}
	for _, f := range s.facts {
		areas[f.Text] = f.Areas
	}
	if !slices.Equal(areas["Sam goes to Grace Chapel"], []string{"religion", "home"}) || !slices.Equal(areas["Sam's cat is called Biscuit"], []string{"other"}) {
		t.Errorf("areas = %v", areas)
	}
	if slices.Contains(s1.filed, "Sam goes to Grace Chapel") || !slices.Contains(s1.filed, "Sam's cat is called Biscuit") {
		t.Errorf("System 1 filed %q; want only the fact System 2 couldn't", s1.filed)
	}
}

func TestSystem2SavesFactsAboutBothSides(t *testing.T) {
	s2 := &fakeSystem2{remember: []system2.Memory{
		{About: system2.AboutSpeaker, Text: "Sam's passport expires in 2027"},
		{About: system2.AboutSelf, Text: "Tobi's favourite colour is forest green"},
	}}
	m, s := newMind(&fakeReplier{}, &fakeSystem1{choice: "remember", confidence: 0.22}, s2)
	p := percept("1", "It is 2027. And what's your favourite colour?")

	m.handle(context.Background(), p)

	if !s2.requests[0].CollectFacts {
		t.Error("System 2 was not asked for facts")
	}
	subjects := map[string]string{}
	for _, f := range s.facts {
		subjects[f.Text] = f.Subject
		if f.PerceptID != p.ID || !f.At.Equal(p.At) || f.ID.IsZero() {
			t.Errorf("fact = %+v", f)
		}
	}
	if subjects["Sam's passport expires in 2027"] != sam || subjects["Tobi's favourite colour is forest green"] != memory.SubjectSelf {
		t.Errorf("subjects = %v", subjects)
	}
}

// onReply runs as a reply is sent.
type onReply func()

func (f onReply) Reply(context.Context, perception.Percept, string) error {
	f()
	return nil
}

// The reply goes out before what it learned is kept, so it never waits on that.
func TestTheReplyIsSentBeforeWhatItLearnedIsKept(t *testing.T) {
	var s *fakeStore
	keptFirst := -1
	m, s := newMind(onReply(func() { keptFirst = len(s.facts) }), &fakeSystem1{choice: "remember", confidence: 0.22},
		&fakeSystem2{remember: []system2.Memory{{About: system2.AboutSpeaker, Text: "Sam's passport expires in 2027"}}})

	m.handle(context.Background(), percept("1", "My passport expires in 2027."))

	if keptFirst != 0 || len(s.facts) != 1 {
		t.Errorf("facts kept when the reply went: %d; after: %d", keptFirst, len(s.facts))
	}
}

// A reply shows while it's written, then is sent; a draft that comes too soon
// after the last waits for the reply.
func TestAReplyShowsWhileItsWritten(t *testing.T) {
	r := &fakeReplier{}
	m, _ := newMind(r, &fakeSystem1{choice: "remember", confidence: 0.22}, &fakeSystem2{drafts: []string{"a", "a thoughtful"}})
	m.Drafter = r

	m.handle(context.Background(), percept("1", "What's your favourite colour?"))

	if want := []string{"draft: a", "a thoughtful reply"}; !slices.Equal(r.sent, want) {
		t.Errorf("sent %q, want %q", r.sent, want)
	}
}

func TestRecallPassesRelevantFactsToSystem2(t *testing.T) {
	s1 := &fakeSystem1{choice: "question", confidence: 0.93, relevant: map[string]bool{"Sam's passport expires in March 2027": true}}
	s2 := &fakeSystem2{}
	m, s := newMind(&fakeReplier{}, s1, s2)
	day := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	s.facts = []memory.Fact{ // newest first
		{ID: ulid.Make(), Subject: sam, Text: "My car is a blue Golf", At: day.Add(48 * time.Hour)},
		{ID: ulid.Make(), Subject: sam, Text: "My passport expires in March 2027", At: day},
	}

	m.handle(context.Background(), percept("1", "When does my passport expire?"))

	// Both ways recall may go are checked in one call while the message is
	// read; nothing is asked again after it.
	if len(s1.recallCalls) != 1 {
		t.Fatalf("recall checks = %d, want 1", len(s1.recallCalls))
	}
	note := s2.requests[0].Note
	if !strings.Contains(note, "Sam's passport expires in March 2027") || strings.Contains(note, "Golf") {
		t.Errorf("note = %q", note)
	}
	if len(s.decisions) != 3 || s.decisions[1].Action != "recall" || *s.decisions[1].ParentID != s.decisions[0].ID ||
		s.decisions[1].CostUSD != 0.00003 {
		t.Errorf("decisions = %+v", s.decisions)
	}
}

func TestOwnFactsAndTheirsAreKeptApart(t *testing.T) {
	mine := memory.Fact{ID: ulid.Make(), Subject: memory.SubjectSelf, Text: "Tobi's favourite food is jollof rice", At: now.Add(-time.Hour)}
	theirs := memory.Fact{ID: ulid.Make(), Subject: sam, Text: "I go to Grace Chapel", At: now.Add(-2 * time.Hour)}
	stranger := memory.Fact{ID: ulid.Make(), Subject: "test:someone-else", Text: "I go to St. Mary's", At: now}
	s1 := &fakeSystem1{choice: "question", confidence: 0.93, relevant: map[string]bool{
		"Tobi's favourite food is jollof rice": true, "Sam go to Grace Chapel": true, "Sam go to St. Mary's": true,
	}}
	s2 := &fakeSystem2{}
	m, s := newMind(&fakeReplier{}, s1, s2)
	s.facts = []memory.Fact{stranger, mine, theirs}

	m.handle(context.Background(), percept("1", "Tell me something"))

	note := s2.requests[0].Note
	wantMine := "About yourself:\n- [f1] 27 Oct 2026: Tobi's favourite food is jollof rice"
	wantTheirs := "About Sam:\n- [f2] 27 Oct 2026: Sam go to Grace Chapel"
	if !strings.Contains(note, wantMine) || !strings.Contains(note, wantTheirs) || strings.Contains(note, "St. Mary") {
		t.Errorf("note = %q", note)
	}
}

func TestFailedRecallStillReplies(t *testing.T) {
	s1 := &fakeSystem1{choice: "question", confidence: 0.93, recallErr: errors.New("boom")}
	r := &fakeReplier{}
	m, s := newMind(r, s1, &fakeSystem2{})
	s.facts = []memory.Fact{{ID: ulid.Make(), Subject: sam, Text: "x", At: now}}

	m.handle(context.Background(), percept("1", "hi?"))

	if len(r.sent) != 1 || r.sent[0] != "a thoughtful reply" {
		t.Fatalf("sent %q", r.sent)
	}
	if d := s.decisions[1]; d.Action != "error" || d.Outcome.Status != decision.OutcomeFailed {
		t.Errorf("recall decision = %+v", d)
	}
}

func TestDuplicatePerceptIsSkipped(t *testing.T) {
	r := &fakeReplier{}
	m, s := newMind(r, &fakeSystem1{choice: "small_talk", confidence: 0.9}, &fakeSystem2{})

	m.handle(context.Background(), percept("1", "hello"))
	afterFirst := len(s.decisions)
	m.handle(context.Background(), percept("1", "hello"))

	if len(r.sent) != 1 || len(s.decisions) != afterFirst {
		t.Fatalf("the second copy sent %d replies and added %d decisions, want nothing", len(r.sent)-1, len(s.decisions)-afterFirst)
	}
}

func (s *fakeStore) SeedJudgment(_ context.Context, cases []memory.JudgmentCase, ps []memory.Principle) (int, int, error) {
	nc, np := 0, 0
	for _, c := range cases {
		if !slices.ContainsFunc(s.cases, func(x memory.JudgmentCase) bool { return x.Key == c.Key }) {
			s.cases, nc = append(s.cases, c), nc+1
		}
	}
	for _, p := range ps {
		if !slices.ContainsFunc(s.principles, func(x memory.Principle) bool { return x.Seed == p.Seed }) {
			s.principles, np = append(s.principles, p), np+1
		}
	}
	return nc, np, nil
}

func (s *fakeStore) Principles(_ context.Context, status string) ([]memory.Principle, error) {
	return keep(s.principles, -1, func(p memory.Principle) bool { return status == "" || p.Status == status }), nil
}

func (s *fakeStore) JudgmentCases(_ context.Context, status string, types []string) ([]memory.JudgmentCase, error) {
	return keep(s.cases, -1, func(c memory.JudgmentCase) bool {
		return (status == "" || c.Status == status) && (len(types) == 0 || slices.Contains(types, c.Type))
	}), nil
}

func (s *fakeStore) SavePrinciple(_ context.Context, p memory.Principle) error {
	s.principles = append(s.principles, p)
	return nil
}

func (s *fakeStore) SaveCall(_ context.Context, c memory.Call) error {
	s.calls = append(s.calls, c)
	return nil
}

func (s *fakeStore) callAt(id ulid.ULID) *memory.Call {
	for i := range s.calls {
		if s.calls[i].ID == id {
			return &s.calls[i]
		}
	}
	return nil
}

func (s *fakeStore) Call(_ context.Context, id ulid.ULID) (memory.Call, bool, error) {
	if c := s.callAt(id); c != nil {
		return *c, true, nil
	}
	return memory.Call{}, false, nil
}

func (s *fakeStore) RecentCalls(_ context.Context, status string, limit int) ([]memory.Call, error) {
	return keep(reversed(s.calls), limit, func(c memory.Call) bool { return status == "" || c.Status == status }), nil
}

func (s *fakeStore) CallsAskedSince(_ context.Context, mode string, since time.Time) (int, error) {
	return len(keep(s.calls, -1, func(c memory.Call) bool { return c.Mode == mode && !c.AskedAt.IsZero() && !c.AskedAt.Before(since) })), nil
}

func (s *fakeStore) MarkCallAsked(_ context.Context, id ulid.ULID, where json.RawMessage, at time.Time) error {
	if c := s.callAt(id); c != nil && c.Status == memory.CallLogged {
		c.Status, c.Message, c.AskedAt = memory.CallAsked, where, at
	}
	return nil
}

func (s *fakeStore) StaleCalls(_ context.Context, before time.Time, limit int) ([]memory.Call, error) {
	return keep(s.calls, limit, func(c memory.Call) bool { return c.Status == memory.CallAsked && c.AskedAt.Before(before) }), nil
}

func (s *fakeStore) ExpireCall(_ context.Context, id ulid.ULID) (bool, error) {
	if c := s.callAt(id); c != nil && c.Status == memory.CallAsked {
		c.Status, c.Waiting = memory.CallExpired, ""
		return true, nil
	}
	return false, nil
}

func (s *fakeStore) WaitForOwner(_ context.Context, id ulid.ULID, waiting string, at time.Time) error {
	if c := s.callAt(id); c != nil {
		c.Waiting, c.WaitingAt = waiting, at
	}
	return nil
}

func (s *fakeStore) WaitingCall(_ context.Context, since time.Time) (memory.Call, bool, error) {
	c, ok := find(reversed(s.calls), func(c memory.Call) bool { return c.Waiting != "" && !c.WaitingAt.Before(since) })
	return c, ok, nil
}

func (s *fakeStore) AnswerCall(_ context.Context, c memory.Call, asCase bool) (bool, error) {
	old := s.callAt(c.ID)
	if old == nil || old.Status == memory.CallAnswered {
		return false, nil
	}
	agreed := c.OwnerPick == c.Pick
	c.Status, c.Agreed, c.WaitingAt = memory.CallAnswered, &agreed, c.AnsweredAt
	if asCase {
		choice := c.OwnerChoice
		if c.OwnerPick >= 0 {
			choice = c.Options[c.OwnerPick]
		}
		c.CaseID = ulid.Make()
		s.cases = append(s.cases, memory.JudgmentCase{ID: c.CaseID, Key: c.CaseID.String(), Type: memory.CallCaseType(c.Type),
			Situation: c.Situation, Options: c.Options, Choice: choice, Reason: c.OwnerReason, Status: memory.JudgmentApproved, Source: c.AnsweredBy})
	}
	*old = c
	return true, nil
}

func (s *fakeStore) GiveCallReason(_ context.Context, id ulid.ULID, reason string, _ time.Time) error {
	c := s.callAt(id)
	c.OwnerReason, c.Waiting = reason, ""
	for i := range s.cases {
		if s.cases[i].ID == c.CaseID {
			s.cases[i].Reason = reason
		}
	}
	return nil
}

func (s *fakeStore) CallsToLookBack(context.Context, []string, time.Time, time.Time, int) ([]memory.Call, error) {
	return nil, nil
}

func (s *fakeStore) SetOutcome(context.Context, ulid.ULID, string, string, string, string, time.Time) (bool, error) {
	return false, nil
}

func (s *fakeStore) SaveCaseEmbedding(context.Context, ulid.ULID, []float32, string) error {
	return nil
}

func (s *fakeStore) SaveTaste(context.Context, memory.Taste) error { return nil }

func (s *fakeStore) Tastes(context.Context, int) ([]memory.Taste, error) { return nil, nil }

func (s *fakeStore) NextDue(_ context.Context, now time.Time, promiseLead, askExpires time.Duration, canAsk bool, _ time.Duration) (time.Time, bool, error) {
	var next time.Time
	consider := func(t time.Time) {
		if next.IsZero() || t.Before(next) {
			next = t
		}
	}
	for _, i := range s.intentions {
		if i.Status == memory.IntentionPending {
			if i.Kind == memory.IntentionTask {
				consider(i.DueAt.Add(-promiseLead))
			} else {
				consider(i.DueAt)
			}
		}
	}
	for _, a := range s.approvals {
		if a.Status == memory.ApprovalPending {
			if a.SentAt.IsZero() && canAsk {
				consider(now)
			} else {
				consider(a.AskedAt.Add(askExpires))
			}
		}
	}
	for _, a := range s.actions {
		if a.Status == memory.ActionApproved {
			consider(now)
		}
	}
	return next, !next.IsZero(), nil
}
