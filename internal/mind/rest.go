package mind

import (
	"context"
	"fmt"
	"time"

	"github.com/Grandbusta/tinio/internal/perception"
)

const (
	// restMargin: something due this soon keeps them awake.
	restMargin = 2 * time.Minute
	// longestRest: they're woken this often whatever is due, so something
	// nextWake doesn't count is late, never lost.
	longestRest = 6 * time.Hour
	// awakeAtMost: awake, a tick does its work at least this often, for the
	// same reason, however little is due.
	awakeAtMost = 15 * time.Minute
)

// Kept is the little the life loop keeps in memory, kept while they rest and
// given back when they wake (Config.Kept).
type Kept struct {
	LastTick time.Time `json:"last_tick"`
	Planned  string    `json:"planned"` // the local date their day was last planned for
	Round    int       `json:"round"`
	Reviewed time.Time `json:"reviewed"`
}

// Resting is what Run returns when they've gone to rest: when to wake them, and
// what to give back to them then.
type Resting struct {
	Until time.Time
	Kept  Kept
}

func (r *Resting) Error() string {
	return fmt.Sprintf("resting until %s", r.Until.Format(time.RFC3339))
}

// hear handles p unless they've gone to rest; then it's left unheard, to be
// given to them when they wake, and false. stepMu is held for the whole
// message, so they never go to rest in the middle of one.
func (m *Mind) hear(ctx context.Context, p perception.Percept) (awake bool) {
	m.stepMu.Lock()
	defer m.stepMu.Unlock()
	if m.resting {
		return false
	}
	awake, m.lastHeard = true, p.At // so a message that panics leaves them listening
	defer m.survive("percept "+p.ID.String(), nil)
	defer m.changed.Store(true) // what they heard may have made something due
	m.handle(ctx, p)
	return true
}

// Reload has them rest at their next tick, never in the middle of a message,
// and wake at once: started again, they read their character afresh. Safe
// from any goroutine.
func (m *Mind) Reload() { m.reload.Store(true) }

// restIfIdle goes to rest, after a tick, if they've heard nothing for RestAfter,
// have nothing in hand and nothing due within restMargin, or were asked to
// Reload. nil if they stay awake.
func (m *Mind) restIfIdle(ctx context.Context, now time.Time) *Resting {
	reload := m.reload.Load()
	if (m.RestAfter <= 0 && !reload) || m.toPlan.Load() || len(m.Percepts) > 0 {
		return nil
	}
	if !m.stepMu.TryLock() { // a message being handled
		return nil
	}
	defer m.stepMu.Unlock()
	until := now
	if !reload {
		if now.Sub(m.lastHeard) < m.RestAfter || m.changed.Load() {
			return nil
		}
		if until = m.nextDue; until.Sub(now) < restMargin {
			return nil
		}
	}
	m.resting = true
	return &Resting{Until: until, Kept: Kept{LastTick: m.lastTick, Planned: m.planned, Round: m.round, Reviewed: m.reviewed}}
}

// nextWake is the soonest they have something to do after now: what's due in
// their memory, a time on their clock, or a tool to look at; at most
// longestRest away.
func (m *Mind) nextWake(ctx context.Context, now time.Time) (time.Time, error) {
	next := now.Add(longestRest)
	if t := m.nextOnClock(now); !t.IsZero() && t.Before(next) {
		next = t
	}
	for _, w := range m.Watchers {
		if t := m.looked[w.Name()].Add(w.Every()); t.Before(next) {
			next = t
		}
	}
	if h := m.held.Load(); h != 0 && time.Unix(0, h).Before(next) { // messages waiting for the reset
		next = time.Unix(0, h)
	}
	for _, t := range []time.Time{m.planTries.next, m.reviewTries.next} { // a plan or review tried again
		if t.After(now) && t.Before(next) {
			next = t
		}
	}
	p := m.policy()
	quiet := m.Quiet.at(now.In(m.Zone))
	if quiet && m.Quiet.end(now.In(m.Zone)).Before(next) { // what waited for the quiet hours goes then
		next = m.Quiet.end(now.In(m.Zone))
	}
	var summing time.Duration
	if m.TopicSort.ID != "" && m.TopicPrompt.Text != "" {
		summing = sumAfter
	}
	due, ok, err := m.Store.NextDue(ctx, now, promiseLead, p.AskExpires, m.Asker != nil && p.Owner != "" && !quiet, summing)
	if err != nil {
		return time.Time{}, err
	}
	if ok && due.Before(next) {
		next = due
	}
	return next, nil
}

// due is whether a tick at now has anything to do: something came due, they
// heard something since the last one that did, or it's been awakeAtMost.
// After one that does, it works out when the next is due (one query), so a
// tick with nothing to do costs nothing.
func (m *Mind) due(now time.Time) bool {
	return m.changed.Load() || m.dataLeft || !now.Before(m.nextDue) || !now.Before(m.lastTick.Add(awakeAtMost))
}

func (m *Mind) planNext(ctx context.Context, now time.Time) {
	next, err := m.nextWake(ctx, now)
	if err != nil {
		m.Log.Warn("working out what's due next failed; every tick does its work", "err", err)
		next = now
	}
	m.nextDue = next
}

// nextOnClock is the next time on their clock after now, within a day: a
// check-in, and with a day of their own its plan rounds, journal and weekly
// review. Zero if there's none.
func (m *Mind) nextOnClock(now time.Time) time.Time {
	var next time.Time
	consider := func(t time.Time) {
		if t.After(now) && (next.IsZero() || t.Before(next)) {
			next = t
		}
	}
	for d := range 2 {
		midnight := m.startOfDay(now).AddDate(0, 0, d)
		for _, at := range m.CheckIns.Times {
			consider(midnight.Add(at))
		}
		if m.dayOn() {
			consider(midnight.Add(m.Day.PlanAt))
			for r := range max(m.Day.Pace, 1) {
				consider(m.roundEnd(r, midnight)) // the next round's start, and at the last, the journal
			}
		}
	}
	if m.Day.ReviewPrompt.Text != "" {
		at := m.reviewTime(now)
		if !at.After(now) {
			at = at.AddDate(0, 0, 7)
		}
		consider(at)
	}
	return next
}
