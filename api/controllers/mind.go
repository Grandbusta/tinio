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
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/console"
	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/decision/questions"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// Mind is how an agent thinks, as their owner sees it: today's decisions
// counted, the newest of them, and any one's whole chain with the wording of
// what System 1 asked itself. Their owner can say whether a read was right,
// as in the console. Behind middlewares.OwnAgent.
type Mind struct {
	DB   *db.DB
	Sets map[string]questions.Set // by id, for their questions' wording
	Now  func() time.Time         // nil: the clock
}

const (
	decisionsShown = 100
	labelRunes     = 100
)

type questionView struct {
	Set          string       `json:"set"`
	Type         string       `json:"type"`
	Instructions string       `json:"instructions"`
	Choices      []choiceView `json:"choices,omitempty"`
}

type choiceView struct {
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

func (m *Mind) store(c *gin.Context) *store.Store {
	return store.New(m.DB).For(middlewares.Owned(c).ID)
}

// Get is today, in their time, counted, and their newest decisions, newest
// first.
func (m *Mind) Get(c *gin.Context) error {
	st := m.store(c)
	now := time.Now()
	if m.Now != nil {
		now = m.Now()
	}
	_, _, midnight, err := theirTime(c, st, now)
	if err != nil {
		return err
	}
	v, err := st.Vitals(c, midnight)
	if err != nil {
		return err
	}
	entries, err := st.DecisionLog(c, time.Time{}, decisionsShown, true)
	if err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{
		"today": gin.H{"decisions": v.Decisions, "by_tier": v.ByTier, "escalations": v.Escalations, "cost_usd": v.CostUSD,
			"avg_latency_ms": v.AvgLatency.Milliseconds()},
		"decisions": console.EntryViews(entries),
	})
	return nil
}

// Chain is a decision and every other about the same thing, oldest first,
// each with its labels, and the questions System 1 answered in them, by name.
func (m *Mind) Chain(c *gin.Context) error {
	st := m.store(c)
	id, err := ulid.Parse(c.Param("decision"))
	if err != nil {
		return utils.ErrNoSuchDecision
	}
	chain, err := st.DecisionChain(c, id)
	if err != nil {
		return err
	}
	if len(chain) == 0 {
		return utils.ErrNoSuchDecision
	}
	ids := make([]ulid.ULID, len(chain))
	for i, e := range chain {
		ids[i] = e.ID
	}
	labels, err := st.Labels(c, ids)
	if err != nil {
		return err
	}
	ag := middlewares.Owned(c)
	n := strings.NewReplacer("{agent}", ag.Name, "{speaker}", ag.OwnerName)
	qs := map[string]questionView{}
	for _, e := range chain {
		if e.Tier != decision.TierSystem1 {
			continue
		}
		for _, ref := range strings.Split(e.SetID, ",") {
			set, ok := m.Sets[strings.SplitN(ref, "@", 2)[0]]
			if !ok {
				continue
			}
			for _, q := range set.Questions {
				v := questionView{Set: set.Ref(), Type: string(q.Type), Instructions: n.Replace(q.Instructions)}
				for _, ch := range q.Choices {
					v.Choices = append(v.Choices, choiceView{Value: ch.Value, Description: n.Replace(ch.Description)})
				}
				qs[q.Name] = v
			}
		}
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"chain": console.Labelled(console.EntryViews(chain), labels), "questions": qs})
	return nil
}

// Label is their owner saying what the right answer to one of a decision's
// questions was: a choice, or "true" or "false". Labels are what replays and
// evals score System 1 against; they change nothing the agent does.
func (m *Mind) Label(c *gin.Context) error {
	var in struct {
		Decision string `json:"decision"`
		Question string `json:"question"`
		Value    string `json:"value"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	id, err := ulid.Parse(in.Decision)
	if err != nil {
		return utils.ErrNoSuchDecision
	}
	in.Question, in.Value = strings.TrimSpace(in.Question), strings.TrimSpace(in.Value)
	if in.Question == "" || in.Value == "" || len([]rune(in.Question)) > labelRunes || len([]rune(in.Value)) > labelRunes {
		return utils.Invalid("value", "A label is a question's name and its right answer.")
	}
	st := m.store(c)
	chain, err := st.DecisionChain(c, id)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(chain, func(e store.LogEntry) bool { return e.ID == id }) {
		return utils.ErrNoSuchDecision
	}
	if err := st.SaveLabel(c, memory.Label{DecisionID: id, Question: in.Question, Value: in.Value, Source: memory.LabelFromApp,
		At: time.Now().UTC()}); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusCreated, gin.H{})
	return nil
}
