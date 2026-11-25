package mind

import (
	"context"
	"testing"
	"time"

	"github.com/Grandbusta/tinio/internal/character"
	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

// slowPlan holds the day plan until release is closed, closing started when
// it begins.
type slowPlan struct {
	*fakeSystem2
	started, release chan struct{}
}

func (s slowPlan) Reason(ctx context.Context, req system2.Request) (system2.Response, error) {
	if req.Format != nil && req.Format.Name == "morning" {
		close(s.started)
		<-s.release
	}
	return s.fakeSystem2.Reason(ctx, req)
}

// replyStore closes replied when a System 2 reply is saved, the last thing
// handle does, so the fakes are never used by both loops at once.
type replyStore struct {
	*fakeStore
	replied chan struct{}
}

func (s replyStore) SaveDecision(ctx context.Context, d decision.Decision) error {
	err := s.fakeStore.SaveDecision(ctx, d)
	if d.Tier == decision.TierSystem2 && d.Action == "reply" {
		close(s.replied)
	}
	return err
}

func TestRepliesWhileItsDayIsBusy(t *testing.T) {
	inner := &fakeSystem2{formats: map[string]string{"morning": `{"opening":"Morning.","items":[],"question":""}`}}
	m, s, _, _ := morningMind()
	m.System2 = inner
	plan := slowPlan{fakeSystem2: inner, started: make(chan struct{}), release: make(chan struct{})}
	store := replyStore{fakeStore: s, replied: make(chan struct{})}
	ticks, percepts := make(chan time.Time), make(chan perception.Percept)
	m.System2, m.Store, m.Ticks, m.Percepts = plan, store, ticks, percepts

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stopped := make(chan error)
	go func() { stopped <- m.Run(ctx) }()

	ticks <- at(29, 6, 45)
	select {
	case <-plan.started:
	case <-ctx.Done():
		t.Fatal("the tick never started the morning")
	}
	select {
	case percepts <- percept("1", "how's your day going?"):
	case <-time.After(3 * time.Second):
		t.Fatal("the message waited for the morning")
	}
	select {
	case <-store.replied:
	case <-time.After(3 * time.Second):
		t.Fatal("no reply while the morning was still being written")
	}
	close(plan.release)
	cancel()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if r := m.Replier.(*fakeReplier); len(r.sent) != 1 {
		t.Errorf("sent %q", r.sent)
	}
}

// panicsOnce panics on its first reply, as a bug that only one message hits
// would. Only the talk loop calls it.
type panicsOnce struct {
	*fakeSystem2
	panicked bool
}

func (p *panicsOnce) Reason(ctx context.Context, req system2.Request) (system2.Response, error) {
	if !p.panicked && req.Format == nil {
		p.panicked = true
		panic("a bug one message hits")
	}
	return p.fakeSystem2.Reason(ctx, req)
}

// Many agents share a process, and an unrecovered panic ends it for all of them.
func TestAPanicDropsOnlyThatMessage(t *testing.T) {
	m, s := newMind(&fakeReplier{}, &fakeSystem1{choice: "share", confidence: 0.93}, &panicsOnce{fakeSystem2: &fakeSystem2{}})
	store := replyStore{fakeStore: s, replied: make(chan struct{})}
	percepts := make(chan perception.Percept)
	m.Store, m.Percepts = store, percepts

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stopped := make(chan error)
	go func() { stopped <- m.Run(ctx) }()

	percepts <- percept("1", "this one hits the bug")
	select {
	case percepts <- percept("2", "and this one doesn't"):
	case <-ctx.Done():
		t.Fatal("the mind stopped listening after the panic")
	}
	select {
	case <-store.replied:
	case <-ctx.Done():
		t.Fatal("no reply to the message after the panic")
	}
	cancel()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if r := m.Replier.(*fakeReplier); len(r.sent) != 1 {
		t.Errorf("sent %q", r.sent)
	}
}

// Seeding is dozens of writes, so an agent seeded at an earlier start isn't
// seeded on every start after.
func TestABornAgentIsntSeededAgain(t *testing.T) {
	for _, born := range []bool{false, true} {
		m, s := newMind(&fakeReplier{}, &fakeSystem1{}, &fakeSystem2{})
		m.Life.Family = []character.Relative{{Name: "Ada", Relation: "owner", Account: owner}}
		m.Born = born
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Run seeds, then sees it's done
		if err := m.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if seeded := len(s.people) == 1; seeded == born {
			t.Errorf("born %v: %d people", born, len(s.people))
		}
	}
}
