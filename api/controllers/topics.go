package controllers

import (
	"encoding/json"
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

// The topics an owner and their agent have going, as the agent keeps them:
// the owner can rename, close or merge one, and move a message to another.
// Only the owner's own topics count; the agent's with anyone else are never
// shown or used.

const (
	newTopic        = "new"
	maxTitleRunes   = 60
	topicsShown     = 100 // open and quiet, of any age, newest first
	thingsPerTopics = 200 // files and asks loaded for the list, newest first
	thingsShown     = 6   // of each topic's
)

type topicView struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"` // "" until the agent names it
	Summary   string      `json:"summary"`
	Status    string      `json:"status"`
	Settled   []string    `json:"settled"`
	Quiet     bool        `json:"quiet"`
	StartedAt time.Time   `json:"started_at"`
	LastAt    time.Time   `json:"last_at"`
	Waiting   int         `json:"waiting"` // asks and calls in it not answered yet
	Things    []topicItem `json:"things"`  // its newest files and asks
}

type topicItem struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Text    string          `json:"text"`
	Payload json.RawMessage `json:"payload,omitempty"`
	At      time.Time       `json:"at"`
	Settled bool            `json:"settled"`
}

func ownerKey(c *gin.Context) string { return "account:" + middlewares.Owned(c).AccountID }

// topicFor is the topic a message is sent in: the owner's topic, a new one
// for "new", or "" for none.
func (ch *Chat) topicFor(c *gin.Context, st *store.Store, topic string) (string, error) {
	switch topic {
	case "":
		return "", nil
	case newTopic:
		t := memory.Topic{ID: ulid.Make().String(), Person: ownerKey(c), StartedAt: ch.now()}
		return t.ID, st.StartTopic(c, t)
	}
	t, ok, err := st.Topic(c, topic)
	if err != nil {
		return "", err
	}
	if !ok || t.Person != ownerKey(c) {
		return "", utils.ErrNoSuchTopic
	}
	return t.ID, nil
}

// Topics is the owner's topics, open and quiet, newest first.
func (ch *Chat) Topics(c *gin.Context) error {
	st := ch.store(c)
	ts, err := st.Topics(c, ownerKey(c), time.Time{}, topicsShown)
	if err != nil {
		return err
	}
	ids := make([]string, len(ts))
	for i, t := range ts {
		ids[i] = t.ID
	}
	things, err := st.TopicThings(c, ids, thingsPerTopics)
	if err != nil {
		return err
	}
	views := make([]topicView, len(ts))
	at := map[string]int{}
	for i, t := range ts {
		at[t.ID] = i
		views[i] = topicView{ID: t.ID, Title: t.Title, Summary: t.Summary, Status: t.Status, Settled: t.Settled, Quiet: t.Quiet,
			StartedAt: t.StartedAt.UTC(), LastAt: t.LastAt.UTC(), Things: []topicItem{}}
		if views[i].Settled == nil {
			views[i].Settled = []string{}
		}
	}
	for _, m := range things {
		v := &views[at[m.Topic]]
		open := m.Kind != memory.ChatFile && m.SettledAt.IsZero()
		if open {
			v.Waiting++
		}
		if len(v.Things) < thingsShown {
			v.Things = append(v.Things, topicItem{ID: m.ID, Kind: m.Kind, Text: m.Text, Payload: m.Payload, At: m.At.UTC(), Settled: !open})
		}
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"topics": views})
	return nil
}

// ownTopic is the topic named in the path, if it's the owner's.
func (ch *Chat) ownTopic(c *gin.Context, st *store.Store) (memory.Topic, error) {
	t, ok, err := st.Topic(c, c.Param("topic"))
	if err != nil {
		return t, err
	}
	if !ok || t.Person != ownerKey(c) {
		return t, utils.ErrNoSuchTopic
	}
	return t, nil
}

// RenameTopic gives a topic the owner's title, which the agent keeps.
func (ch *Chat) RenameTopic(c *gin.Context) error {
	var in struct {
		Title string `json:"title"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	title := strings.TrimSpace(in.Title)
	if title == "" || utf8.RuneCountInString(title) > maxTitleRunes {
		return utils.Invalid("title", "Give it a name, up to 60 characters.")
	}
	st := ch.store(c)
	t, err := ch.ownTopic(c, st)
	if err != nil {
		return err
	}
	if _, err := st.RenameTopic(c, t.ID, title); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{})
	return nil
}

// CloseTopic makes a topic quiet. Nothing is deleted, and it's open again
// once it's brought up.
func (ch *Chat) CloseTopic(c *gin.Context) error {
	st := ch.store(c)
	t, err := ch.ownTopic(c, st)
	if err != nil {
		return err
	}
	if _, err := st.QuietTopic(c, t.ID); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{})
	return nil
}

// MergeTopic moves everything in a topic into another of the owner's.
func (ch *Chat) MergeTopic(c *gin.Context) error {
	var in struct {
		Into string `json:"into"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	st := ch.store(c)
	from, err := ch.ownTopic(c, st)
	if err != nil {
		return err
	}
	into, ok, err := st.Topic(c, in.Into)
	if err != nil {
		return err
	}
	if !ok || into.Person != from.Person || into.ID == from.ID {
		return utils.Invalid("into", "Pick another of your topics.")
	}
	if _, err := st.MergeTopic(c, from.ID, into.ID); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{})
	return nil
}

// MoveMessage puts a row in another of the owner's topics, a new one ("new"),
// or none (""). The owner's own message takes the agent's answers to it along.
func (ch *Chat) MoveMessage(c *gin.Context) error {
	var in struct {
		Topic *string `json:"topic"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Topic == nil {
		return utils.ErrBadRequest
	}
	st := ch.store(c)
	m, ok, err := st.ChatMessage(c, c.Param("message"))
	if err != nil {
		return err
	}
	if !ok || m.Kind == memory.ChatHello {
		return utils.ErrNoSuchMessage
	}
	topic, err := ch.topicFor(c, st, *in.Topic)
	if err != nil {
		return err
	}
	if _, err := st.MoveChat(c, m.ID, topic, ch.now()); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"topic": topic})
	return nil
}
