package controllers

import (
	"github.com/Grandbusta/tinio/internal/db"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// Judgment is how their owner decides, as the agent has learnt it: the
// principles it may use (only those its owner kept), the calls its owner
// answered, and how often it guessed them. Behind middlewares.OwnAgent.
type Judgment struct {
	DB  *db.DB
	Now func() time.Time // nil: the clock
}

const (
	guessedOf      = 10 // the last this many answered calls, for how often it guessed
	callsShown     = 30
	principleRunes = 300
)

type principleRow struct {
	ID     string    `json:"id"`
	Text   string    `json:"text"`
	Kind   string    `json:"kind"`   // principle, priority (in rank order) or boundary (never, whatever the gain)
	Status string    `json:"status"` // proposed (waiting for their owner) or approved (kept)
	Source string    `json:"source"` // seed (raised with it), review (its weekly review drew it from their calls) or owner
	Calls  int       `json:"calls"`  // the decisions it was drawn from
	At     time.Time `json:"at"`     // when it was proposed or last changed
}

type callRow struct {
	ID        string    `json:"id"`
	Situation string    `json:"situation"`
	Chose     string    `json:"chose"`         // their owner's answer
	Guessed   string    `json:"guessed"`       // the agent's own pick
	Why       string    `json:"why,omitempty"` // their owner's reason, if they gave one
	Request   bool      `json:"request"`       // a yes or no to one of its requests, rather than a choice
	At        time.Time `json:"at"`
}

func (j *Judgment) now() time.Time {
	if j.Now != nil {
		return j.Now()
	}
	return time.Now()
}

func (j *Judgment) store(c *gin.Context) *store.Store {
	return store.New(j.DB).For(middlewares.Owned(c).ID)
}

func pick(options []string, i int, own string) string {
	if i >= 0 && i < len(options) {
		return options[i]
	}
	return own
}

// Get is their principles, proposed and kept (removed ones left out), the
// calls their owner answered, newest first, and how many of the last
// guessedOf they guessed and how many calls were asked this week and the week
// before.
func (j *Judgment) Get(c *gin.Context) error {
	st, now := j.store(c), j.now()
	ps, err := st.Principles(c, "")
	if err != nil {
		return err
	}
	principles := []principleRow{}
	for _, p := range ps {
		if p.Status != memory.JudgmentRejected {
			principles = append(principles, principleRow{ID: p.ID.String(), Text: p.Text, Kind: p.Kind, Status: p.Status, Source: p.Source,
				Calls: len(p.Cases), At: p.UpdatedAt.UTC()})
		}
	}
	answered, err := st.RecentCalls(c, memory.CallAnswered, callsShown)
	if err != nil {
		return err
	}
	calls := make([]callRow, len(answered))
	right, of := 0, 0
	for i, cl := range answered {
		calls[i] = callRow{ID: cl.ID.String(), Situation: cl.Situation, Chose: pick(cl.Options, cl.OwnerPick, cl.OwnerChoice),
			Guessed: pick(cl.Options, cl.Pick, ""), Why: cl.OwnerReason, Request: cl.AnsweredBy == memory.AnsweredByApproval, At: cl.AnsweredAt.UTC()}
		if cl.Agreed != nil && of < guessedOf {
			of++
			if *cl.Agreed {
				right++
			}
		}
	}
	thisWeek, err := st.CallsAskedBetween(c, now.Add(-growthWeek), now)
	if err != nil {
		return err
	}
	lastWeek, err := st.CallsAskedBetween(c, now.Add(-2*growthWeek), now.Add(-growthWeek))
	if err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"principles": principles, "calls": calls,
		"guessed": gin.H{"right": right, "of": of}, "asked": gin.H{"this_week": thisWeek, "last_week": lastWeek}})
	return nil
}

// Add is a principle their owner writes, kept at once: it applies to every
// kind of call.
func (j *Judgment) Add(c *gin.Context) error {
	var in struct {
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	text := strings.Join(strings.Fields(in.Text), " ")
	if text == "" || utf8.RuneCountInString(text) > principleRunes {
		return utils.Invalid("text", "A principle is a sentence, up to 300 characters.")
	}
	now := j.now().UTC()
	p := memory.Principle{ID: ulid.Make(), Text: text, Kind: memory.PrincipleRule, Status: memory.JudgmentApproved,
		Source: memory.JudgmentFromOwner, CreatedAt: now, UpdatedAt: now}
	if err := j.store(c).SavePrinciple(c, p); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusCreated, gin.H{"principle": principleRow{ID: p.ID.String(), Text: p.Text, Kind: p.Kind,
		Status: p.Status, Source: p.Source, At: now}})
	return nil
}

// Review keeps a principle, so the agent may use it, or removes it, so it
// never does: any of theirs, proposed or kept.
func (j *Judgment) Review(c *gin.Context) error {
	var in struct {
		Keep *bool `json:"keep"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Keep == nil {
		return utils.ErrBadRequest
	}
	id, err := ulid.Parse(c.Param("principle"))
	if err != nil {
		return utils.ErrNoSuchPrinciple
	}
	status := memory.JudgmentRejected
	if *in.Keep {
		status = memory.JudgmentApproved
	}
	found, err := j.store(c).ReviewPrinciple(c, id, status, "", j.now().UTC())
	if err != nil {
		return err
	}
	if !found {
		return utils.ErrNoSuchPrinciple
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"status": status})
	return nil
}
