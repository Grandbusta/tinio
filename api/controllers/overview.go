package controllers

import (
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// shownInChat are the rows the app shows; the owner's taps and hello aren't.
var shownInChat = []string{memory.ChatMessageKind, memory.ChatAsk, memory.ChatChoice, memory.ChatFile}

const (
	lastRows    = 20             // rows read for each agent's last shown one
	latelyFor   = 48 * time.Hour // how far back the feed goes
	latelyItems = 8
)

type lastSaid struct {
	Author string    `json:"author"`
	Kind   string    `json:"kind"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
}

type agentSummary struct {
	models.Agent
	Last    *lastSaid `json:"last,omitempty"` // nil: nothing said yet
	Waiting int       `json:"waiting"`        // their requests and calls not yet answered
	Unread  int       `json:"unread"`         // what they've said since their owner last looked
	Now     string    `json:"now,omitempty"`  // what they're doing, from today's plan
	Next    string    `json:"next,omitempty"` // "HH:MM · what", their time
}

// lately is one thing an agent did: made a work, or asked their owner
// something still waiting for an answer.
type lately struct {
	Agent string    `json:"agent"`
	Kind  string    `json:"kind"` // made, asked
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
}

// Overview is the signed-in account's agents, oldest first, each with the
// last thing said in their chat and how many of their requests wait, and what
// they've made and asked lately across all of them, newest first.
func (a *Agents) Overview(c *gin.Context) error {
	ags, err := models.AgentsOf(c, a.DB, middlewares.Account(c).ID)
	if err != nil {
		return err
	}
	now := time.Now()
	agents := make([]agentSummary, 0, len(ags))
	feed := []lately{}
	for _, ag := range ags {
		st := store.New(a.DB).For(ag.ID)
		sum := agentSummary{Agent: ag}
		rows, err := st.Chat(c, "", lastRows)
		if err != nil {
			return err
		}
		for _, m := range rows {
			if !slices.Contains(shownInChat, m.Kind) {
				continue
			}
			sum.Last = &lastSaid{Author: m.Author, Kind: m.Kind, Text: m.Text, At: m.At.UTC()}
			if m.Author == memory.ChatAgent && (m.Kind == memory.ChatAsk || m.Kind == memory.ChatChoice) && m.SettledAt.IsZero() &&
				now.Sub(m.At) < latelyFor {
				feed = append(feed, lately{Agent: ag.ID, Kind: "asked", Text: m.Text, At: m.At.UTC()})
			}
		}
		if sum.Waiting, err = st.Waiting(c); err != nil {
			return err
		}
		if sum.Unread, err = st.Unread(c, ag.ReadUntil); err != nil {
			return err
		}
		_, local, is, err := ownDay(c, st, "account:"+ag.AccountID, now)
		if err != nil {
			return err
		}
		sum.Now, sum.Next = nowAndNext(is, local)
		ws, err := st.WorksBetween(c, now.Add(-latelyFor), now.Add(time.Second))
		if err != nil {
			return err
		}
		for _, w := range ws {
			if w.Kind != "journal" && w.Kind != "review" {
				feed = append(feed, lately{Agent: ag.ID, Kind: "made", Text: w.Title, At: w.CreatedAt.UTC()})
			}
		}
		agents = append(agents, sum)
	}
	slices.SortFunc(feed, func(x, y lately) int { return y.At.Compare(x.At) })
	utils.SuccessWithData(c, http.StatusOK, gin.H{"agents": agents, "lately": feed[:min(len(feed), latelyItems)]})
	return nil
}
