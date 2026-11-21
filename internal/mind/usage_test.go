package mind

import (
	"context"
	"testing"
	"time"

	"github.com/Grandbusta/tinio/internal/usage"
)

// Past its week an agent replies on the light model; well past it, a message
// waits unheard, and at the reset they rest and wake at once to hear it.
func TestTheWeekSetsHowTheyReply(t *testing.T) {
	ctx := context.Background()
	started := now.Add(2*time.Hour - usage.Week) // the week resets in two hours
	used := func(usd float64) usage.Standing {
		return usage.Standing{Plan: usage.Plans["personal"], Started: started, WeekStart: started, WeekUSD: usd}
	}
	r, full, light := &fakeReplier{}, &fakeSystem2{}, &fakeSystem2{}
	m, s := newMind(r, &fakeSystem1{choice: "question", confidence: 0.9}, full)
	m.LightSystem2 = light

	s.standing = used(1.3)
	m.handle(ctx, percept("1", "What are you up to?"))
	if len(light.requests) != 1 || len(full.requests) != 0 || len(r.sent) != 1 {
		t.Fatalf("in light mode: light %d, full %d, sent %d", len(light.requests), len(full.requests), len(r.sent))
	}

	s.standing = used(2)
	m.handle(ctx, percept("2", "Still there?"))
	if len(r.sent) != 1 || s.seen["2"] {
		t.Errorf("resting, the message was heard: sent %v", r.sent)
	}
	reset := started.Add(usage.Week)
	if held := time.Unix(0, m.held.Load()); !held.Equal(reset) {
		t.Errorf("held until %s, want the reset %s", held, reset)
	}
	if next, _ := m.nextWake(ctx, now); !next.Equal(reset) {
		t.Errorf("they wake at %s, want the reset %s", next, reset)
	}
	m.tick(ctx, reset)
	if m.held.Load() != 0 || !m.reload.Load() {
		t.Error("at the reset they don't rest and wake to hear what waited")
	}
}

// Their own day goes on only while the week allows it: not once it's used,
// nor after a free trial.
func TestTheirDayNeedsTheWeek(t *testing.T) {
	plan := at(29, 9, 0)
	for _, c := range []struct {
		name  string
		st    usage.Standing
		plans bool
	}{
		{"within the week", usage.Standing{Plan: usage.Plans["personal"], Started: plan.AddDate(0, 0, -3), WeekStart: plan.AddDate(0, 0, -3), WeekUSD: 0.5}, true},
		{"the week used", usage.Standing{Plan: usage.Plans["personal"], Started: plan.AddDate(0, 0, -3), WeekStart: plan.AddDate(0, 0, -3), WeekUSD: 1.3}, false},
		{"a free trial", usage.Standing{Plan: usage.Plans["free"], Started: plan.AddDate(0, 0, -3), WeekStart: plan.AddDate(0, 0, -3), WeekUSD: 0.1}, true},
		{"after the trial", usage.Standing{Plan: usage.Plans["free"], Started: plan.AddDate(0, 0, -20)}, false},
	} {
		s2 := &fakeSystem2{formats: map[string]string{"day_plan": `{"activities":[]}`}}
		m, s := lifeMind(s2)
		s.standing = c.st
		m.tick(context.Background(), plan)
		if got := len(s2.requests) > 0; got != c.plans {
			t.Errorf("%s: planned %v, want %v", c.name, got, c.plans)
		}
	}
}
