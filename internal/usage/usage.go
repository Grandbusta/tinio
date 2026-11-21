// Package usage is how much of the week the owner's agents have used, against
// the cap they set. The week is metered in dollars, the real cost of every
// decision, on their own keys.
package usage

import "time"

type State string

const (
	Normal  State = "normal"
	Light   State = "light"   // the week is used: replies on a small model, their own day paused
	Resting State = "resting" // well past it, or an hour that used too much: only what's never cut
)

const (
	Week = 7 * 24 * time.Hour
	// restAt is how far past the week they rest; hourShare is the most of a week
	// one hour may use before they rest until it ends, so a loop or abuse can't
	// burn a week in an hour.
	restAt    = 1.5
	hourShare = 0.25
	// ReplyUSD is what a reply costs on average (measured 20 November 2026),
	// for "messages left" until an account has its own.
	ReplyUSD = 0.0028
)

// Standing is an account's use of its week. The zero value is no account:
// nothing is metered.
type Standing struct {
	CapUSD  float64   // what a week may cost; 0: no limit
	Started time.Time // when the account started: its weeks count from it
	// Used this week and this hour: what the counter holds, if it's for the
	// week or hour that contains now (see At).
	WeekStart, HourStart time.Time
	WeekUSD, HourUSD     float64
}

// WeekOf is when the week containing now started, for an account started then.
func WeekOf(started, now time.Time) time.Time {
	if now.Before(started) {
		return started
	}
	return started.Add(now.Sub(started) / Week * Week)
}

// At is s as of now: a counter from an earlier week or hour counts nothing.
func (s Standing) At(now time.Time) Standing {
	week := WeekOf(s.Started, now)
	if s.WeekStart.Before(week) {
		s.WeekStart, s.WeekUSD = week, 0
	}
	if hour := now.Truncate(time.Hour); s.HourStart.Before(hour) {
		s.HourStart, s.HourUSD = hour, 0
	}
	return s
}

// Budget is what a week may cost; 0: no limit.
func (s Standing) Budget() float64 { return s.CapUSD }

// Used is the share of this week used, 1 at the budget; 0 with no limit.
func (s Standing) Used(now time.Time) float64 {
	s = s.At(now)
	b := s.Budget()
	if b <= 0 {
		return 0
	}
	return s.WeekUSD / b
}

func (s Standing) hourOver(now time.Time) bool {
	s = s.At(now)
	b := s.Budget()
	return b > 0 && s.HourUSD >= b*hourShare
}

func (s Standing) State(now time.Time) State {
	switch used := s.Used(now); {
	case used >= restAt || s.hourOver(now):
		return Resting
	case used >= 1:
		return Light
	}
	return Normal
}

// ResetsAt is when the state next changes back: the end of the hour if only
// the hour is over, else the start of the next week.
func (s Standing) ResetsAt(now time.Time) time.Time {
	if s.Used(now) < restAt && s.hourOver(now) {
		return now.Truncate(time.Hour).Add(time.Hour)
	}
	return WeekOf(s.Started, now).Add(Week)
}

// OwnDay is whether their own day goes on: planning, activities, check-ins,
// what they watch and the weekly review. Not past the week.
func (s Standing) OwnDay(now time.Time) bool { return s.State(now) == Normal }
