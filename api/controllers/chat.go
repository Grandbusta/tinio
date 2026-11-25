package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/api/config"
	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/coding/jobs"
	"github.com/Grandbusta/tinio/internal/db"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/store"
)

// Chat is an owner's web chat with their agent: what they write and tap goes
// into the agent's chat in the database, and what the agent says comes back
// the same way. Every route is behind middlewares.OwnAgent.
type Chat struct {
	DB  *db.DB
	Hub *ChatHub
	// Jobs takes the owner's replies to a coding job's card; nil: there are none.
	Jobs *jobs.Service
	// PerAccount limits how fast an account writes.
	PerAccount *utils.Limiter
	Now        func() time.Time // nil: the clock
}

const (
	maxMessageRunes = 4000
	pageSize        = 50
	maxPage         = 200
	missedAtMost    = 500 // rows a reconnecting stream is sent
	maxNoteRunes    = 500
	maxSearchRunes  = 100
	searchShown     = 50
)

// chatRow is a row as the app reads it. The app keeps rows by id: a settled ask
// or choice comes again, with its new text and settled_at.
type chatRow struct {
	ID        string          `json:"id"`
	Author    string          `json:"author"`
	Kind      string          `json:"kind"`
	Text      string          `json:"text"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	At        time.Time       `json:"at"`
	SettledAt *time.Time      `json:"settled_at,omitempty"`
	Topic     string          `json:"topic,omitempty"`
	ReplyTo   string          `json:"reply_to,omitempty"`
}

func rowOf(m memory.ChatMessage) chatRow {
	r := chatRow{ID: m.ID, Author: m.Author, Kind: m.Kind, Text: m.Text, Payload: m.Payload, At: m.At.UTC(), Topic: m.Topic, ReplyTo: m.ReplyTo}
	if !m.SettledAt.IsZero() {
		settled := m.SettledAt.UTC()
		r.SettledAt = &settled
	}
	return r
}

func (ch *Chat) now() time.Time {
	if ch.Now != nil {
		return ch.Now().UTC()
	}
	return time.Now().UTC()
}

func (ch *Chat) store(c *gin.Context) *store.Store {
	return store.New(ch.DB).For(middlewares.Owned(c).ID)
}

// write saves an owner's row, which wakes the agent, and answers with it.
func (ch *Chat) write(c *gin.Context, m memory.ChatMessage, payload any) error {
	if !ch.PerAccount.Allow(middlewares.Account(c).ID) {
		return utils.ErrSlowDown
	}
	m.ID, m.Author, m.At = ulid.Make().String(), memory.ChatOwner, ch.now()
	if payload != nil {
		var err error
		if m.Payload, err = json.Marshal(payload); err != nil {
			return err
		}
	}
	if err := ch.store(c).SaveChat(c, m); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusCreated, gin.H{"message": rowOf(m)})
	return nil
}

// Send is the owner writing to their agent: in a topic of theirs, or "new"
// to start one (named by the agent's reply), and replying to a row, if they
// chose to. A reply with no topic is in the topic of what it replies to.
// Otherwise the agent sorts it.
func (ch *Chat) Send(c *gin.Context) error {
	var in struct {
		Text    string `json:"text"`
		Topic   string `json:"topic"`
		ReplyTo string `json:"reply_to"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	text := strings.TrimSpace(in.Text)
	if text == "" || utf8.RuneCountInString(text) > maxMessageRunes {
		return utils.Invalid("text", fmt.Sprintf("Write something, up to %d characters.", maxMessageRunes))
	}
	sent, err := ch.store(c).OwnerMessagesSince(c, ch.now().Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if sent >= config.MessagesPerDay {
		return utils.ErrDailyLimit
	}
	m := memory.ChatMessage{Kind: memory.ChatMessageKind, Text: text, Topic: in.Topic, ReplyTo: in.ReplyTo}
	st := ch.store(c)
	if m.ReplyTo != "" {
		to, ok, err := st.ChatMessage(c, m.ReplyTo)
		if err != nil {
			return err
		}
		if !ok || to.Kind == memory.ChatHello || to.Kind == memory.ChatApprove || to.Kind == memory.ChatPick {
			return utils.ErrNoSuchMessage
		}
		if m.Topic == "" {
			m.Topic = to.Topic
		}
		if job := jobOf(to); job != "" && ch.Jobs != nil {
			// A reply to a coding job goes to the job, and the agent doesn't hear it.
			if err := ch.Jobs.Reply(c, c.Param("id"), job, text); err != nil {
				if errors.Is(err, jobs.ErrNoJob) {
					return utils.Invalid("reply_to", "That job has ended, and can't be carried on.")
				}
				return err
			}
			m.Kind = memory.ChatJobReply
		}
	}
	if m.Topic, err = ch.topicFor(c, st, m.Topic); err != nil {
		return err
	}
	return ch.write(c, m, nil)
}

// jobOf is the coding job a chat row is about: its card, or one of its questions.
func jobOf(m memory.ChatMessage) string {
	if m.Kind != memory.ChatJob && m.Kind != memory.ChatJobAsk {
		return ""
	}
	var p struct {
		Job string `json:"job"`
	}
	json.Unmarshal(m.Payload, &p)
	return p.Job
}

// Hello is the owner opening their agent's chat for the first time: the agent
// introduces themselves, once however often it's asked (hello is false after).
func (ch *Chat) Hello(c *gin.Context) error {
	said, err := ch.store(c).Hello(c, ch.now())
	if err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"hello": said})
	return nil
}

// Answer is the owner's tap on their agent's request (approval, approve; with
// text, their version of it, approved instead) or call (call, option; -1 for
// something else). Only one still waiting counts.
func (ch *Chat) Answer(c *gin.Context) error {
	var in struct {
		Approval string `json:"approval"`
		Approve  *bool  `json:"approve"`
		Text     string `json:"text"`
		Call     string `json:"call"`
		Option   *int   `json:"option"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	st := ch.store(c)
	switch {
	case in.Approval != "" && in.Approve != nil && in.Call == "":
		id, err := ulid.Parse(in.Approval)
		if err != nil {
			return utils.ErrNotWaiting
		}
		a, ok, err := st.Approval(c, id)
		if err != nil {
			return err
		}
		if !ok || a.Status != memory.ApprovalPending || a.SentAt.IsZero() {
			return utils.ErrNotWaiting
		}
		text := strings.TrimSpace(in.Text)
		switch {
		case text != "" && !*in.Approve:
			return utils.Invalid("text", "Your version goes with a yes.")
		case utf8.RuneCountInString(text) > maxMessageRunes:
			return utils.Invalid("text", fmt.Sprintf("Your version can be up to %d characters.", maxMessageRunes))
		}
		return ch.write(c, memory.ChatMessage{Kind: memory.ChatApprove}, perception.Answer{Approval: in.Approval, Approve: *in.Approve, Text: text})
	case in.Call != "" && in.Option != nil && in.Approval == "":
		id, err := ulid.Parse(in.Call)
		if err != nil {
			return utils.ErrNotWaiting
		}
		call, ok, err := st.Call(c, id)
		if err != nil {
			return err
		}
		if !ok || call.Status != memory.CallAsked {
			return utils.ErrNotWaiting
		}
		if *in.Option < -1 || *in.Option >= len(call.Options) {
			return utils.Invalid("option", "Pick one of the options, or something else.")
		}
		return ch.write(c, memory.ChatMessage{Kind: memory.ChatPick}, perception.Pick{Call: in.Call, Option: *in.Option})
	}
	return utils.ErrBadRequest
}

// React is the owner's thumb on something their agent made (work), with what
// was off, if they say: a row their agent hears, as feedback on the skills it
// shows. Never shown in the chat.
func (ch *Chat) React(c *gin.Context) error {
	var in struct {
		Work string `json:"work"`
		Good *bool  `json:"good"`
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Good == nil {
		return utils.ErrBadRequest
	}
	id, err := ulid.Parse(in.Work)
	if err != nil {
		return utils.ErrNoSuchWork
	}
	note := strings.TrimSpace(in.Note)
	if *in.Good && note != "" || utf8.RuneCountInString(note) > maxNoteRunes {
		return utils.Invalid("note", fmt.Sprintf("What was off goes with a thumb down, up to %d characters.", maxNoteRunes))
	}
	if _, ok, err := ch.store(c).WorkFor(c, id, "account:"+middlewares.Owned(c).AccountID); err != nil {
		return err
	} else if !ok {
		return utils.ErrNoSuchWork
	}
	return ch.write(c, memory.ChatMessage{Kind: memory.ChatReact}, perception.Reaction{Work: in.Work, Good: *in.Good, Note: note})
}

// Search is the rows of the chat with q in their words, newest first.
func (ch *Chat) Search(c *gin.Context) error {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" || utf8.RuneCountInString(q) > maxSearchRunes {
		return utils.Invalid("q", fmt.Sprintf("Search for something, up to %d characters.", maxSearchRunes))
	}
	ms, err := ch.store(c).SearchChat(c, q, searchShown)
	if err != nil {
		return err
	}
	rows := make([]chatRow, len(ms))
	for i, m := range ms {
		rows[i] = rowOf(m)
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"messages": rows})
	return nil
}

// Read is the owner having seen their agent's chat up to the row until.
func (ch *Chat) Read(c *gin.Context) error {
	var in struct {
		Until string `json:"until"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	if _, err := ulid.Parse(in.Until); err != nil {
		return utils.Invalid("until", "until is the id of a row in the chat.")
	}
	if err := models.MarkRead(c, ch.DB, middlewares.Owned(c).ID, in.Until); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{})
	return nil
}

// History is a page of the chat, oldest first: the newest rows, or those
// before before; only one topic's, with topic.
func (ch *Chat) History(c *gin.Context) error {
	limit := pageSize
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return utils.Invalid("limit", "limit is a number of rows.")
		}
		limit = min(n, maxPage)
	}
	var (
		ms  []memory.ChatMessage
		err error
	)
	if topic := c.Query("topic"); topic != "" {
		ms, err = ch.store(c).ChatInTopic(c, topic, c.Query("before"), limit)
	} else {
		ms, err = ch.store(c).Chat(c, c.Query("before"), limit)
	}
	if err != nil {
		return err
	}
	rows := make([]chatRow, len(ms))
	for i, m := range ms {
		rows[i] = rowOf(m)
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"messages": rows})
	return nil
}

// File is a file the agent sent, downloaded under its own name.
func (ch *Chat) File(c *gin.Context) error {
	f, ok, err := ch.store(c).File(c, c.Param("file"))
	if err != nil {
		return err
	}
	if !ok {
		return utils.ErrNoSuchFile
	}
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, f.ContentType, f.Data)
	return nil
}

// Stream sends the chat's new and settled rows as they happen, as server-sent
// events, and the reply being written as drafts (all of it so far, never
// kept: the reply's row replaces it). A new row carries its id; a settled one, older, doesn't, so the
// browser's place never goes back. A browser that reconnects says the last id
// it saw (Last-Event-ID, or ?after=) and is first sent what came after it;
// what was settled meanwhile it reads from the history.
func (ch *Chat) Stream(c *gin.Context) error {
	st := ch.store(c)
	changed, stop := ch.Hub.Watch(middlewares.Owned(c).ID)
	defer stop()
	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	// no-transform: a proxy that compresses (Next's rewrite, for a browser) would
	// otherwise hold the events back until the stream ends.
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no") // nginx passes each event on at once
	w.WriteHeader(http.StatusOK)
	ctx := c.Request.Context()
	after := c.GetHeader("Last-Event-ID")
	if after == "" {
		after = c.Query("after")
	}
	newest := after
	send := func(m memory.ChatMessage) bool {
		b, err := json.Marshal(rowOf(m))
		if err == nil {
			id := ""
			if m.ID > newest {
				id, newest = "id: "+m.ID+"\n", m.ID
			}
			_, err = fmt.Fprintf(w, "%sevent: chat\ndata: %s\n\n", id, b)
		}
		w.Flush()
		return err == nil
	}
	if after != "" {
		missed, err := st.ChatAfter(ctx, after, missedAtMost)
		if err != nil {
			return nil // the stream has begun; the browser reconnects
		}
		for _, m := range missed {
			if !send(m) {
				return nil
			}
		}
	} else {
		fmt.Fprint(w, ": open\n\n")
		w.Flush()
	}
	ping := time.NewTicker(25 * time.Second) // so idle connections aren't closed on the way
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			w.Flush()
		case ch, ok := <-changed:
			if !ok {
				return nil // it fell behind; the browser reconnects and catches up
			}
			if ch.row == "" {
				b, _ := json.Marshal(gin.H{"text": ch.draft})
				if _, err := fmt.Fprintf(w, "event: draft\ndata: %s\n\n", b); err != nil {
					return nil
				}
				w.Flush()
				continue
			}
			m, found, err := st.ChatMessage(ctx, ch.row)
			if err != nil {
				return nil
			}
			if found && !send(m) {
				return nil
			}
		}
	}
}

// ChatHub tells the streams watching each agent's chat which rows changed:
// one connection listening to Postgres per API process, however many watch.
type ChatHub struct {
	DB  *db.DB
	Log *slog.Logger

	mu       sync.Mutex
	watching map[string]map[chan change]bool // by agent
}

// change is a row added or settled, or, with no row, the reply being written.
type change struct {
	row   string
	draft string
}

// Watch is what changes in agent's chat, until stop. If the watcher falls
// behind on rows, the channel is closed: it should catch up from the last row
// it saw. A draft it has no room for is skipped; the next says it all.
func (h *ChatHub) Watch(agent string) (changed <-chan change, stop func()) {
	ch := make(chan change, 64)
	h.mu.Lock()
	if h.watching == nil {
		h.watching = map[string]map[chan change]bool{}
	}
	if h.watching[agent] == nil {
		h.watching[agent] = map[chan change]bool{}
	}
	h.watching[agent][ch] = true
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.watching[agent][ch] {
			delete(h.watching[agent], ch)
			close(ch)
		}
	}
}

// Run listens until ctx is done. If it falls behind, every stream ends: their
// browsers reconnect and catch up on what they missed.
func (h *ChatHub) Run(ctx context.Context) {
	for ctx.Err() == nil {
		h.listen(ctx)
		h.mu.Lock()
		for _, chans := range h.watching {
			for ch := range chans {
				close(ch)
			}
		}
		h.watching = nil
		h.mu.Unlock()
	}
}

// listen hands on what's said in every agent's chat until it falls behind or
// ctx is done.
func (h *ChatHub) listen(ctx context.Context) {
	toAgent, stopTo := h.DB.Listen(store.ToAgent, 1024)
	defer stopTo()
	fromAgent, stopFrom := h.DB.Listen(store.FromAgent, 1024)
	defer stopFrom()
	drafting, stopDraft := h.DB.Listen(store.Drafting, 1024)
	defer stopDraft()
	for {
		var (
			payload string
			ok      bool
			draft   bool
		)
		select {
		case <-ctx.Done():
			return
		case payload, ok = <-toAgent:
		case payload, ok = <-fromAgent:
		case payload, ok = <-drafting:
			draft = true
		}
		if !ok {
			return
		}
		agent, rest, _ := strings.Cut(payload, ":")
		c := change{row: rest}
		if draft {
			c = change{draft: rest}
		}
		h.mu.Lock()
		for ch := range h.watching[agent] {
			select {
			case ch <- c:
			default:
				if c.row != "" { // fallen behind: it catches up when it reconnects
					delete(h.watching[agent], ch)
					close(ch)
				}
			}
		}
		h.mu.Unlock()
	}
}

// ChatEvents tells emit what's said in every agent's chat until ctx is done,
// for the desktop app's window, which can't hold a stream open: "chat" with a
// row added or settled ({agent, row}), and "draft" with the reply being
// written ({agent, text}). If it falls behind, it says "behind", and the app
// reads the chats again.
func ChatEvents(ctx context.Context, d *db.DB, emit func(event string, data any)) {
	for ctx.Err() == nil {
		chatEvents(ctx, d, emit)
		if ctx.Err() == nil {
			emit("behind", nil)
		}
	}
}

func chatEvents(ctx context.Context, d *db.DB, emit func(event string, data any)) {
	toAgent, stopTo := d.Listen(store.ToAgent, 1024)
	defer stopTo()
	fromAgent, stopFrom := d.Listen(store.FromAgent, 1024)
	defer stopFrom()
	drafting, stopDraft := d.Listen(store.Drafting, 1024)
	defer stopDraft()
	for {
		var (
			payload string
			ok      bool
			draft   bool
		)
		select {
		case <-ctx.Done():
			return
		case payload, ok = <-toAgent:
		case payload, ok = <-fromAgent:
		case payload, ok = <-drafting:
			draft = true
		}
		if !ok {
			return
		}
		agent, rest, _ := strings.Cut(payload, ":")
		if draft {
			emit("draft", gin.H{"agent": agent, "text": rest})
			continue
		}
		m, found, err := store.New(d).For(agent).ChatMessage(ctx, rest)
		if err == nil && found {
			emit("chat", gin.H{"agent": agent, "row": rowOf(m)})
		}
	}
}
