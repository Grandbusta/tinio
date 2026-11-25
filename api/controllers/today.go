package controllers

import (
	"context"
	"github.com/Grandbusta/tinio/internal/db"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/agent"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// Today is the owner's day as their agents see it: each agent's morning,
// once it's sent, with the things it chose, what it drafted for them and
// which they've ticked done, and how many of its requests wait for an answer.
type Today struct {
	DB  *db.DB
	Now func() time.Time // nil: the clock
}

type made struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
	At    string `json:"at"` // HH:MM
}

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

type morningItemView struct {
	ID     string `json:"id"`
	What   string `json:"what"`
	How    string `json:"how"`
	Source string `json:"source"`
	Work   *made  `json:"work,omitempty"` // what it drafted for it
	Done   bool   `json:"done"`
}

// ownerDay is one agent's part of their owner's day. Sent is "" until the
// morning is sent, and then its items show; Next is when it comes ("" if
// mornings are off).
type ownerDay struct {
	Agent    string            `json:"agent"`
	Name     string            `json:"name"`
	Date     string            `json:"date"`
	Sent     string            `json:"sent"` // HH:MM
	Next     string            `json:"next"`
	Opening  string            `json:"opening"`
	Question string            `json:"question"`
	Items    []morningItemView `json:"items"`
	Waiting  int               `json:"waiting"`
}

func (t *Today) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// dayOf is ag's part of today, in their (their owner's) time zone.
func dayOf(c *gin.Context, d *db.DB, ag models.Agent, now time.Time) (ownerDay, error) {
	st := store.New(d).For(ag.ID)
	_, now, midnight, err := theirTime(c, st, now)
	if err != nil {
		return ownerDay{}, err
	}
	v := ownerDay{Agent: ag.ID, Name: ag.Name, Date: now.Format("Monday 2 January"), Items: []morningItemView{}}
	if ag.MorningOn {
		v.Next = ag.MorningAt
	}
	if v.Waiting, err = st.Waiting(c); err != nil {
		return v, err
	}
	mo, ok, err := st.Morning(c, midnight.Format(time.DateOnly))
	if err != nil || !ok || mo.SentAt.IsZero() {
		return v, err
	}
	v.Sent, v.Opening, v.Question = mo.SentAt.In(now.Location()).Format("15:04"), mo.Opening, mo.Question
	for _, it := range mo.Items {
		iv := morningItemView{ID: it.ID, What: it.What, How: it.How, Source: it.Source, Done: it.Done}
		if id, err := ulid.Parse(it.Work); err == nil {
			if w, ok, err := st.Work(c, id); err != nil {
				return v, err
			} else if ok {
				iv.Work = &made{ID: w.ID.String(), Title: w.Title, Kind: strings.ReplaceAll(w.Kind, "_", " "), At: w.CreatedAt.In(now.Location()).Format("15:04")}
			}
		}
		v.Items = append(v.Items, iv)
	}
	return v, nil
}

// Get is one agent's part of their owner's day. Behind middlewares.OwnAgent.
func (t *Today) Get(c *gin.Context) error {
	v, err := dayOf(c, t.DB, middlewares.Owned(c), t.now())
	if err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"today": v})
	return nil
}

// Yours is the signed-in owner's day: every agent's part, in the order they
// were raised.
func (t *Today) Yours(c *gin.Context) error {
	ags, err := models.AgentsOf(c, t.DB, middlewares.Account(c).ID)
	if err != nil {
		return err
	}
	days := []ownerDay{}
	for _, ag := range ags {
		v, err := dayOf(c, t.DB, ag, t.now())
		if err != nil {
			return err
		}
		days = append(days, v)
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"days": days})
	return nil
}

// Tick marks one of today's things done, or not.
func (t *Today) Tick(c *gin.Context) error {
	var in struct {
		Done bool `json:"done"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	ag := middlewares.Owned(c)
	st := store.New(t.DB).For(ag.ID)
	_, _, midnight, err := theirTime(c, st, t.now())
	if err != nil {
		return err
	}
	ok, err := st.TickMorningItem(c, midnight.Format(time.DateOnly), c.Param("item"), in.Done)
	if err != nil {
		return err
	}
	if !ok {
		return utils.ErrNoSuchItem
	}
	return t.Get(c)
}
