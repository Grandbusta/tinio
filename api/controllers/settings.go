package controllers

import (
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
	"github.com/Grandbusta/tinio/internal/action"
	"github.com/Grandbusta/tinio/internal/config"
	"github.com/Grandbusta/tinio/internal/host"
	"github.com/Grandbusta/tinio/internal/store"
)

// Settings is an agent's own settings, for their owner to change in the app:
// what they may do on their own, how fast they work, their quiet hours,
// pausing them and deleting them. A change reaches them within a minute
// (host.Changed). Behind middlewares.OwnAgent.
type Settings struct {
	DB *db.DB
}

// settable are the classes the owner sets here, in the order the app shows
// them: the ones the owner may also change by chat (action.ByChat). fixed are
// shown but set only by Tinio's config: messages outside the chat and money.
var (
	settable = []action.Class{action.ReadWeb, action.Commit, action.Publish, action.ReachOut, action.Write}
	fixed    = []action.Class{action.ExternalMessage, action.Spend}
)

type classView struct {
	Class   string   `json:"class"`
	Verdict string   `json:"verdict"`
	Choices []string `json:"choices"` // empty: it can't be changed here
}

type quietView struct {
	From  string `json:"from"` // HH:MM in their time zone; both "": none
	Until string `json:"until"`
}

type settingsView struct {
	Classes []classView `json:"classes"`
	Quiet   quietView   `json:"quiet"`
	Morning morningView `json:"morning"`
	Paused  bool        `json:"paused"`
}

type settingsInput struct {
	Classes map[string]string `json:"classes"` // class → auto, ask or forbid; only the ones changed
	Quiet   *quietView        `json:"quiet"`   // nil: unchanged
	Morning *morningView      `json:"morning"` // nil: unchanged
}

// policy is the config's autonomy with the owner's changes over it, as the
// agent applies them when they start.
func (s *Settings) policy(c *gin.Context, st *store.Store) (action.Policy, error) {
	base, err := config.Load().Policy()
	if err != nil {
		return action.Policy{}, err
	}
	rules, err := st.AutonomyRules(c)
	if err != nil {
		return action.Policy{}, err
	}
	return base.WithChatRules(rules), nil
}

func choicesOf(c action.Class) []string {
	if c == action.ReadWeb || c == action.UseTools {
		return []string{string(action.Auto), string(action.Forbid)} // they can't wait for an answer
	}
	return []string{string(action.Auto), string(action.Ask), string(action.Forbid)}
}

func verdictOf(p action.Policy, c action.Class) string {
	if v, ok := p.Classes[c]; ok {
		return string(v)
	}
	return string(action.Ask)
}

func (s *Settings) view(c *gin.Context, ag models.Agent) (settingsView, error) {
	st := store.New(s.DB).For(ag.ID)
	p, err := s.policy(c, st)
	if err != nil {
		return settingsView{}, err
	}
	v := settingsView{Quiet: quietView{From: ag.QuietFrom, Until: ag.QuietUntil},
		Morning: morningOf(ag), Paused: ag.Paused}
	for _, c := range settable {
		v.Classes = append(v.Classes, classView{Class: string(c), Verdict: verdictOf(p, c), Choices: choicesOf(c)})
	}
	for _, c := range fixed {
		v.Classes = append(v.Classes, classView{Class: string(c), Verdict: verdictOf(p, c), Choices: []string{}})
	}
	return v, nil
}

func (s *Settings) Get(c *gin.Context) error {
	return s.answer(c, middlewares.Owned(c))
}

func (s *Settings) answer(c *gin.Context, ag models.Agent) error {
	v, err := s.view(c, ag)
	if err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, v)
	return nil
}

// Save keeps what changed: each class as the owner's newest rule for it, the
// quiet hours and the morning on the agent.
func (s *Settings) Save(c *gin.Context) error {
	var in settingsInput
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	ag := middlewares.Owned(c)
	st := store.New(s.DB).For(ag.ID)
	p, err := s.policy(c, st)
	if err != nil {
		return err
	}
	for class, verdict := range in.Classes {
		if !slices.Contains(settable, action.Class(class)) || !slices.Contains(choicesOf(action.Class(class)), verdict) {
			return utils.Invalid("classes", "That can't be set to that here.")
		}
	}
	if q := in.Quiet; q != nil && (q.From != "" || q.Until != "") {
		from, err1 := time.Parse("15:04", q.From)
		until, err2 := time.Parse("15:04", q.Until)
		if err1 != nil || err2 != nil || from.Equal(until) {
			return utils.Invalid("quiet", "Quiet hours are two different times, like 22:00 and 07:00.")
		}
	}
	var days string
	if in.Morning != nil {
		if days, err = in.Morning.check(); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	changed := false
	for class, verdict := range in.Classes {
		if verdictOf(p, action.Class(class)) == verdict {
			continue
		}
		if err := st.SaveAutonomyRule(c, class, verdict, "app:account:"+ag.AccountID, ulid.ULID{}, now); err != nil {
			return err
		}
		changed = true
	}
	if q := in.Quiet; q != nil && (q.From != ag.QuietFrom || q.Until != ag.QuietUntil) {
		if _, err := s.DB.Exec(c, `UPDATE agents SET quiet_from = $1, quiet_until = $2 WHERE id = $3`, q.From, q.Until, ag.ID); err != nil {
			return err
		}
		ag.QuietFrom, ag.QuietUntil = q.From, q.Until
		changed = true
	}
	if mo := in.Morning; mo != nil && (mo.On != ag.MorningOn || mo.At != ag.MorningAt || days != ag.MorningDays) {
		if _, err := s.DB.Exec(c, `UPDATE agents SET morning_on = $1, morning_at = $2, morning_days = $3 WHERE id = $4`,
			mo.On, mo.At, days, ag.ID); err != nil {
			return err
		}
		ag.MorningOn, ag.MorningAt, ag.MorningDays = mo.On, mo.At, days
		changed = true
	}
	if changed {
		s.DB.Notify(host.Changed, ag.ID)
	}
	return s.answer(c, ag)
}

// Pause stops the agent working and hearing anything until their owner resumes
// them; what they're sent meanwhile is read then. Paused, no worker claims them,
// and one running them lets them go at their next tick.
func (s *Settings) Pause(c *gin.Context) error {
	var in struct {
		Paused bool `json:"paused"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	ag := middlewares.Owned(c)
	if in.Paused != ag.Paused {
		tx, err := s.DB.Begin(c)
		if err != nil {
			return err
		}
		defer tx.Rollback(c)
		if _, err := tx.Exec(c, `UPDATE agents SET paused_at = CASE WHEN $1 THEN now() END WHERE id = $2`, in.Paused, ag.ID); err != nil {
			return err
		}
		notify := host.Changed // a worker running them lets them go
		if !in.Paused {
			// Due at once, so they're claimed now and hear what waited.
			if _, err := tx.Exec(c, `UPDATE agent_runs SET wake_at = now() WHERE agent_id = $1 AND worker IS NULL`, ag.ID); err != nil {
				return err
			}
			notify = host.Raised
		}
		tx.Notify(notify, ag.ID)
		if err := tx.Commit(c); err != nil {
			return err
		}
		ag.Paused = in.Paused
	}
	return s.answer(c, ag)
}

// Delete removes the agent and everything of theirs, for good, once their
// owner has typed their name.
func (s *Settings) Delete(c *gin.Context) error {
	var in struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	ag := middlewares.Owned(c)
	if !strings.EqualFold(strings.TrimSpace(in.Name), ag.Name) {
		return utils.Invalid("name", "Type "+ag.Name+"'s name to delete them.")
	}
	if _, err := s.DB.Exec(c, `DELETE FROM agents WHERE id = $1 AND account_id = $2`, ag.ID, ag.AccountID); err != nil {
		return err
	}
	// A worker running them stops at their next tick.
	s.DB.Notify(host.Changed, ag.ID)
	utils.SuccessWithData(c, http.StatusOK, gin.H{"deleted": ag.ID})
	return nil
}
