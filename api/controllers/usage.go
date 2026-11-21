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

type planView struct {
	Name     string  `json:"name"`
	PriceUSD float64 `json:"price_usd"`
	Agents   int     `json:"agents"`
	MaxPace  int     `json:"max_pace"`
	OwnDay   bool    `json:"own_day"` // false: only for a trial, then chat only
	// Week is its allowance next to Personal's (1); what it costs in dollars is ours to know.
	Week float64 `json:"week"`
}

// agentUse is an agent's part of the week, in percent of its allowance.
type agentUse struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Chat float64 `json:"chat"`
	Day  float64 `json:"day"`
}

type usageView struct {
	Plan         planView   `json:"plan"`
	State        string     `json:"state"` // normal, light, resting
	Used         float64    `json:"used"`  // percent of this week, past 100 once it's over
	ResetsAt     time.Time  `json:"resets_at"`
	MessagesLeft int        `json:"messages_left"`           // before the week is used, at this account's own rate
	OwnDayUntil  *time.Time `json:"own_day_until,omitempty"` // a trial's end, while it lasts
	Agents       []agentUse `json:"agents"`                  // oldest first
	Plans        []planView `json:"plans"`                   // every plan, smallest first
}

var planOrder = []string{"free", "personal", "pro", "team"}

func viewOf(p usage.Plan) planView {
	return planView{Name: p.Name, PriceUSD: p.PriceUSD, Agents: p.Agents, MaxPace: p.MaxPace, OwnDay: p.Trial == 0,
		Week: math.Round(p.WeekUSD/usage.Plans["personal"].WeekUSD*10) / 10}
}

// Usage is the signed-in account's week: its plan, how much is used and by
// which agent, its state, and when it resets.
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
	budget := st.Budget(st.WeekStart)
	pct := func(usd float64) float64 {
		if budget <= 0 {
			return 0
		}
		return math.Round(usd/budget*1000) / 10
	}
	out := usageView{Plan: viewOf(st.Plan), State: string(st.State(now)), Used: pct(st.WeekUSD), ResetsAt: st.ResetsAt(now),
		Agents: make([]agentUse, 0, len(ags))}
	var chat float64
	var replies int
	for _, ag := range ags {
		ch, day, n, err := store.New(a.DB).For(ag.ID).Spent(c, st.WeekStart)
		if err != nil {
			return err
		}
		chat, replies = chat+ch, replies+n
		out.Agents = append(out.Agents, agentUse{ID: ag.ID, Name: ag.Name, Chat: pct(ch), Day: pct(day)})
	}
	reply := usage.ReplyUSD
	if replies >= 10 { // enough to go on its own rate
		reply = chat / float64(replies)
	}
	if left := budget - st.WeekUSD; left > 0 {
		out.MessagesLeft = int(left / reply)
	}
	if p := st.Plan; p.Trial > 0 && now.Before(st.Started.Add(p.Trial)) {
		end := st.Started.Add(p.Trial)
		out.OwnDayUntil = &end
	}
	for _, name := range planOrder {
		out.Plans = append(out.Plans, viewOf(usage.Plans[name]))
	}
	utils.SuccessWithData(c, http.StatusOK, out)
	return nil
}
