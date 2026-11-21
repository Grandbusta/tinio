package controllers

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/agent"
	"github.com/Grandbusta/tinio/internal/config"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// Today is an agent's own day as their owner sees it, in the agent's time:
// what they planned and how each thing went, what they made, a line from
// their journal, and how many of their requests wait for an answer. Behind
// middlewares.OwnAgent.
type Today struct {
	DB  *pgxpool.Pool
	Now func() time.Time // nil: the clock
}

// Plan states: done; now (due, being done); later; waiting (for their owner's
// approval); missed (not done in time).
type planned struct {
	At     string `json:"at"` // HH:MM
	What   string `json:"what"`
	Detail string `json:"detail,omitempty"` // what came of it
	State  string `json:"state"`
}

type made struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
	At    string `json:"at"` // HH:MM
}

// journalLine is at most this long, from the start of their newest entry.
const journalLine = 240

// theirTime is now and the start of today, in the agent's time zone.
func theirTime(ctx context.Context, st *store.Store, now time.Time) (agent.Agent, time.Time, time.Time, error) {
	ag, err := agent.Load(ctx, st)
	if err != nil {
		return ag, now, now, err
	}
	zone := time.UTC
	if ag.Life.Timezone != "" {
		if zone, err = time.LoadLocation(ag.Life.Timezone); err != nil { // checked when they were raised
			return ag, now, now, err
		}
	}
	now = now.In(zone)
	return ag, now, time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone), nil
}

// ownDay is the agent's own activities due today, for themselves or for their
// owner (account:<id>), in their time (cancelled ones left out), and now in their
// time zone.
func ownDay(ctx context.Context, st *store.Store, owner string, now time.Time) (agent.Agent, time.Time, []memory.Intention, error) {
	ag, now, midnight, err := theirTime(ctx, st, now)
	if err != nil {
		return ag, now, nil, err
	}
	is, err := st.IntentionsDue(ctx, midnight, midnight.AddDate(0, 0, 1))
	if err != nil {
		return ag, now, nil, err
	}
	return ag, now, slices.DeleteFunc(is, func(i memory.Intention) bool {
		return (i.Subject != memory.SubjectSelf && i.Subject != owner) || i.Kind != memory.IntentionActivity || i.Status == memory.IntentionCancelled
	}), nil
}

// nowAndNext is what they're doing now (an activity due and not yet done) and
// the next thing they've planned, with its time; "" for none.
func nowAndNext(is []memory.Intention, now time.Time) (string, string) {
	var doing, next string
	for _, i := range is {
		switch {
		case i.Status != memory.IntentionPending:
		case !i.DueAt.After(now):
			doing = i.Text
		case next == "":
			next = i.DueAt.In(now.Location()).Format("15:04") + " · " + i.Text
		}
	}
	return doing, next
}

func (t *Today) Get(c *gin.Context) error {
	st := store.New(t.DB).For(middlewares.Owned(c).ID)
	now := time.Now()
	if t.Now != nil {
		now = t.Now()
	}
	ag, now, is, err := ownDay(c, st, "account:"+middlewares.Owned(c).AccountID, now)
	if err != nil {
		return err
	}
	zone := now.Location()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	plan := []planned{}
	var plannedAt time.Time
	for _, i := range is {
		if plannedAt.IsZero() || i.CreatedAt.Before(plannedAt) {
			plannedAt = i.CreatedAt
		}
		p := planned{At: i.DueAt.In(zone).Format("15:04"), What: i.Text, Detail: i.Result, State: "later"}
		switch {
		case i.Status == memory.IntentionDone:
			p.State = "done"
		case i.Status == memory.IntentionMissed:
			p.State = "missed"
		case i.Status == memory.IntentionAwaiting:
			p.State = "waiting"
		case !i.DueAt.After(now):
			p.State = "now"
		}
		plan = append(plan, p)
	}

	ws, err := st.WorksBetween(c, midnight, now.Add(time.Second))
	if err != nil {
		return err
	}
	works := []made{}
	for _, w := range ws {
		if !slices.Contains([]string{"journal", "review"}, w.Kind) {
			works = append(works, made{ID: w.ID.String(), Title: w.Title, Kind: strings.ReplaceAll(w.Kind, "_", " "), At: w.CreatedAt.In(zone).Format("15:04")})
		}
	}
	var line string
	if j, ok, err := st.LatestWork(c, "journal"); err != nil {
		return err
	} else if ok {
		line = firstLine(j.Text)
	}
	waiting, err := st.Waiting(c)
	if err != nil {
		return err
	}
	plansAt := ag.Life.Day.PlanAt
	if plansAt == "" {
		plansAt = config.Load().DailyLife.PlanAt
	}
	out := gin.H{"date": now.Format("Monday 2 January"), "plans_at": plansAt, "plan": plan, "made": works,
		"journal": line, "waiting": waiting}
	if !plannedAt.IsZero() {
		out["planned_at"] = plannedAt.In(zone).Format("15:04")
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"today": out})
	return nil
}

// firstLine is the start of text: its first sentence, or as much as fits.
func firstLine(text string) string {
	text, _, _ = strings.Cut(strings.TrimSpace(text), "\n")
	for i, r := range text {
		if (r == '.' || r == '!' || r == '?') && (i+1 == len(text) || text[i+1] == ' ') {
			text = text[:i+1]
			break
		}
	}
	if utf8.RuneCountInString(text) > journalLine {
		text = string([]rune(text)[:journalLine-1]) + "…"
	}
	return text
}
