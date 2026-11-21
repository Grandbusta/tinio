package usage

import (
	"testing"
	"time"
)

// The week and the hour each count from their own start, and the state comes
// back when they end.
func TestStateAndReset(t *testing.T) {
	started := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC) // a Tuesday
	now := started.Add(9*24*time.Hour + 30*time.Minute)     // the second week, Thursday 09:30
	week, hour := started.Add(Week), now.Truncate(time.Hour)
	personal := Plans["personal"]
	for _, c := range []struct {
		name   string
		s      Standing
		state  State
		resets time.Time
	}{
		{"within the week", Standing{Plan: personal, Started: started, WeekStart: week, WeekUSD: 1}, Normal, week.Add(Week)},
		{"the week used", Standing{Plan: personal, Started: started, WeekStart: week, WeekUSD: 1.25}, Light, week.Add(Week)},
		{"well past it", Standing{Plan: personal, Started: started, WeekStart: week, WeekUSD: 2}, Resting, week.Add(Week)},
		{"last week's count", Standing{Plan: personal, Started: started, WeekStart: started, WeekUSD: 5}, Normal, week.Add(Week)},
		{"a quarter of the week in an hour", Standing{Plan: personal, Started: started, WeekStart: week, WeekUSD: 0.4,
			HourStart: hour, HourUSD: 0.32}, Resting, hour.Add(time.Hour)},
		{"no account", Standing{}, Normal, time.Time{}},
	} {
		if got := c.s.State(now); got != c.state {
			t.Errorf("%s: %s, want %s", c.name, got, c.state)
		}
		if got := c.s.ResetsAt(now); c.s.Plan.WeekUSD > 0 && !got.Equal(c.resets) {
			t.Errorf("%s: resets %s, want %s", c.name, got, c.resets)
		}
	}
}
