package controllers

import (
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/store"
	"github.com/Grandbusta/tinio/internal/usage"
)

// agentUse is an agent's part of the week, in percent of its allowance.
type agentUse struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Chat float64 `json:"chat"`
	Work float64 `json:"work"` // its work on its own: mornings, drafts, what it watches
}

type usageView struct {
	CapUSD       float64    `json:"cap_usd"`   // what a week may cost, as the owner set it; 0: no limit
	SpentUSD     float64    `json:"spent_usd"` // this week, on their own keys
	State        string     `json:"state"`     // normal, light, resting
	Used         float64    `json:"used"`      // percent of the cap, past 100 once it's over; 0 with no cap
	ResetsAt     time.Time  `json:"resets_at"`
	MessagesLeft int        `json:"messages_left"` // before the cap, at this account's own rate
	Agents       []agentUse `json:"agents"`        // oldest first
}

// Usage is the owner's week: what it may cost and has, and by which agent,
// its state, and when it resets.
func (a *Agents) Usage(c *gin.Context) error {
	account := middlewares.Account(c).ID
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	st, err := store.New(a.DB).AccountStanding(c, account)
	if err != nil {
		return err
	}
	st = st.At(now)
	ags, err := models.AgentsOf(c, a.DB, account)
	if err != nil {
		return err
	}
	budget := st.Budget()
	pct := func(usd float64) float64 {
		if budget <= 0 {
			return 0
		}
		return math.Round(usd/budget*1000) / 10
	}
	out := usageView{CapUSD: st.CapUSD, SpentUSD: math.Round(st.WeekUSD*100) / 100, State: string(st.State(now)), Used: pct(st.WeekUSD),
		ResetsAt: st.ResetsAt(now), Agents: make([]agentUse, 0, len(ags))}
	var chat float64
	var replies int
	for _, ag := range ags {
		ch, day, n, err := store.New(a.DB).For(ag.ID).Spent(c, st.WeekStart)
		if err != nil {
			return err
		}
		chat, replies = chat+ch, replies+n
		out.Agents = append(out.Agents, agentUse{ID: ag.ID, Name: ag.Name, Chat: pct(ch), Work: pct(day)})
	}
	reply := usage.ReplyUSD
	if replies >= 10 { // enough to go on its own rate
		reply = chat / float64(replies)
	}
	if left := budget - st.WeekUSD; left > 0 {
		out.MessagesLeft = int(left / reply)
	}
	utils.SuccessWithData(c, http.StatusOK, out)
	return nil
}

// SetCap is the owner setting what a week may cost on their keys: dollars,
// or 0 for no limit.
func (a *Agents) SetCap(c *gin.Context) error {
	var in struct {
		CapUSD *float64 `json:"cap_usd"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.CapUSD == nil {
		return utils.ErrBadRequest
	}
	if *in.CapUSD < 0 || *in.CapUSD > 10000 {
		return utils.Invalid("cap_usd", "A week's cap is between $0 (no limit) and $10,000.")
	}
	if _, err := a.DB.Exec(c, `UPDATE accounts SET week_usd = $2 WHERE id = $1`, middlewares.Account(c).ID, *in.CapUSD); err != nil {
		return err
	}
	return a.Usage(c)
}
