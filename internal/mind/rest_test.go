package mind

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
)

func restingMind(s1 *fakeSystem1) (*Mind, *fakeStore, chan time.Time, chan perception.Percept) {
	m, s := newMind(&fakeReplier{}, s1, &fakeSystem2{})
	ticks, percepts := make(chan time.Time), make(chan perception.Percept)
	m.Ticks, m.Percepts, m.Born, m.RestAfter = ticks, percepts, true, 5*time.Minute
	m.CheckIns = CheckIns{Times: []time.Duration{12*time.Hour + 30*time.Minute}, Quiet: 4 * time.Hour}
	return m, s, ticks, percepts
}

func runUntilRest(t *testing.T, m *Mind, ticks chan time.Time, at time.Time) *Resting {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- m.Run(ctx) }()
	ticks <- at
	var rest *Resting
	if err := <-stopped; !errors.As(err, &rest) {
		t.Fatalf("didn't rest: %v", err)
	}
	return rest
}

// An idle agent rests until the soonest thing they have to do: whatever is
// due in their memory or on their clock, and never longer than longestRest.
func TestAnIdleAgentRestsUntilTheyreNextDue(t *testing.T) {
	ten := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		add  func(*Mind, *fakeStore)
		want time.Time
	}{
		{"their next check-in", func(*Mind, *fakeStore) {}, ten.Add(2*time.Hour + 30*time.Minute)},
		{"a reminder before it", func(_ *Mind, s *fakeStore) {
			s.intentions = append(s.intentions, memory.Intention{ID: ulid.Make(), Kind: memory.IntentionRemind,
				Status: memory.IntentionPending, Subject: sam, DueAt: ten.Add(time.Hour)})
		}, ten.Add(time.Hour)},
		{"nothing at all, so the ceiling", func(m *Mind, _ *fakeStore) { m.CheckIns.Times = nil }, ten.Add(longestRest)},
	}
	for _, c := range cases {
		m, s, ticks, _ := restingMind(&fakeSystem1{})
		c.add(m, s)
		rest := runUntilRest(t, m, ticks, ten)
		if !rest.Until.Equal(c.want) || !rest.Kept.LastTick.Equal(ten) {
			t.Errorf("%s: resting until %s, kept %+v; want until %s", c.name, rest.Until, rest.Kept, c.want)
		}
	}
}

// heardIt waits until the talk loop has finished the message in hand: the
// reply goes out before the rest is saved, and the clock mustn't move on
// before then (resting waits for a message being handled).
func heardIt(m *Mind) {
	for !m.stepMu.TryLock() {
		time.Sleep(time.Millisecond)
	}
	m.stepMu.Unlock()
}

// An agent stays awake a while after a message, so a conversation doesn't
// wake and rest them at every turn.
func TestAnAgentStaysAwakeAfterAMessage(t *testing.T) {
	m, s, ticks, percepts := restingMind(&fakeSystem1{choice: "share", confidence: 0.93})
	store := replyStore{fakeStore: s, replied: make(chan struct{})}
	m.Store = store
	heard := time.Date(2026, 10, 29, 9, 58, 0, 0, time.UTC)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- m.Run(ctx) }()
	p := percept("1", "morning!")
	p.At = heard
	percepts <- p
	<-store.replied
	heardIt(m)
	ticks <- heard.Add(2 * time.Minute)
	select {
	case ticks <- heard.Add(6 * time.Minute): // still awake to take it
	case err := <-stopped:
		t.Fatalf("rested two minutes after a message: %v", err)
	}
	var rest *Resting
	if err := <-stopped; !errors.As(err, &rest) {
		t.Fatalf("didn't rest five minutes after: %v", err)
	}
}

// A check-in whose time came while they rested is made when they wake.
func TestACheckInWhileRestingIsMadeOnWaking(t *testing.T) {
	noon := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	m := New(Config{Kept: Kept{LastTick: noon}, CheckIns: CheckIns{Times: []time.Duration{12*time.Hour + 30*time.Minute}}})
	if !m.checkInTime(noon.Add(4 * time.Hour)) {
		t.Error("the 12:30 check-in was forgotten over the rest")
	}
}

// worked counts the ticks that did their work, by the one look each takes at
// what's due next.
type worked struct {
	replyStore
	ticks atomic.Int32
}

func (s *worked) NextDue(ctx context.Context, now time.Time, promiseLead, askExpires time.Duration, canAsk bool, sumAfter time.Duration) (time.Time, bool, error) {
	s.ticks.Add(1)
	return s.replyStore.NextDue(ctx, now, promiseLead, askExpires, canAsk, sumAfter)
}

// Awake, a tick does its work only when something is due, they've heard
// something, or it's been awakeAtMost: a quiet minute costs no queries.
func TestAQuietMinuteCostsNothing(t *testing.T) {
	m, s, ticks, percepts := restingMind(&fakeSystem1{choice: "share", confidence: 0.93})
	m.RestAfter = 0 // awake throughout
	store := &worked{replyStore: replyStore{fakeStore: s, replied: make(chan struct{})}}
	m.Store = store
	ten := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	s.intentions = append(s.intentions, memory.Intention{ID: ulid.Make(), Kind: memory.IntentionRemind,
		Status: memory.IntentionPending, Subject: sam, DueAt: ten.Add(5 * time.Minute)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	at := func(minute int) { ticks <- ten.Add(time.Duration(minute) * time.Minute) }
	worked := func(want int32, when string) {
		t.Helper()
		if got := store.ticks.Load(); got != want {
			t.Errorf("%s: %d ticks did their work, want %d", when, got, want)
		}
	}

	at(0) // waking, it does
	at(1)
	at(2)
	worked(1, "quiet minutes")
	percepts <- percept("1", "Morning!")
	<-store.replied
	heardIt(m)
	at(3) // after a message, it does
	at(4)
	worked(2, "after a message")
	at(5) // the reminder is due
	at(6)
	worked(3, "a reminder due")
	for minute := 7; minute <= 20; minute++ { // nothing due until the check-in at 12:30, but at most a quarter of an hour
		at(minute)
	}
	at(21)
	worked(4, "a quarter of an hour on")
}

// Asked to reload, they rest at their next tick, though they've just heard
// from their owner, and wake at once, keeping what they had in hand.
func TestReloadRestsThemAndWakesThemAtOnce(t *testing.T) {
	m, _, ticks, _ := restingMind(&fakeSystem1{})
	ten := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	m.lastHeard = ten
	m.planned, m.round = "2026-10-29", 2
	m.Reload()
	rest := runUntilRest(t, m, ticks, ten)
	if !rest.Until.Equal(ten) || rest.Kept.Planned != "2026-10-29" || rest.Kept.Round != 2 {
		t.Errorf("rest = %+v", rest)
	}
}
