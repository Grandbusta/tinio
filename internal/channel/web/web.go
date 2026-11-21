// Package web is the platform's web chat, as a channel for the mind: what an
// agent says, asks and sends is a row in their chat (chat_messages), which the
// app reads, and their owner's taps on a request or a choice come back as rows
// the worker hands the mind as the percepts Telegram's buttons make.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"path/filepath"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/store"
)

// Channel is one agent's chat; Store is bound to them. The percepts it's
// given say nothing it needs: an agent has one chat, with their owner.
type Channel struct {
	Store *store.Store
}

// Asked is an ask's payload: the request it's for, and its parts for the
// card (memory.Request); the row's text is all of it in words.
type Asked struct {
	Approval string     `json:"approval"`
	Title    string     `json:"title,omitempty"`
	Quote    string     `json:"quote,omitempty"`
	Where    string     `json:"where,omitempty"`
	When     string     `json:"when,omitempty"`
	Undo     string     `json:"undo,omitempty"`
	Final    bool       `json:"final,omitempty"`
	Why      string     `json:"why,omitempty"`
	Expires  *time.Time `json:"expires,omitempty"`
	Editable bool       `json:"editable,omitempty"`
}

// Offered is a choice's payload: the call, how many options its text lists, and
// the agent's own pick (an index).
type Offered struct {
	Call    string `json:"call"`
	Options int    `json:"options"`
	Pick    int    `json:"pick"`
}

// Sent is a file row's payload.
type Sent struct {
	File string `json:"file"`
	Name string `json:"name"`
	Size int    `json:"size"`
}

// where is what Ask and Choose return, for Settle: the row.
type where struct {
	Message string `json:"message"`
}

func (c Channel) Reply(ctx context.Context, p perception.Percept, text string) error {
	_, err := c.say(ctx, p, memory.ChatMessageKind, text, nil)
	return err
}

func (c Channel) Draft(ctx context.Context, _ perception.Percept, text string) error {
	return c.Store.Draft(ctx, text)
}

func (c Channel) Ask(ctx context.Context, p perception.Percept, r memory.Request, id string) (json.RawMessage, error) {
	a := Asked{Approval: id, Title: r.Title, Quote: r.Quote, Where: r.Where, When: r.When, Undo: r.Undo, Final: r.Final,
		Why: r.Why, Editable: r.Editable}
	if !r.Expires.IsZero() {
		at := r.Expires.UTC()
		a.Expires = &at
	}
	return c.say(ctx, p, memory.ChatAsk, r.Text, a)
}

func (c Channel) Choose(ctx context.Context, p perception.Percept, text, id string, options, pick int) (json.RawMessage, error) {
	return c.say(ctx, p, memory.ChatChoice, text, Offered{Call: id, Options: options, Pick: pick})
}

func (c Channel) Settle(ctx context.Context, at json.RawMessage, text string) error {
	var w where
	if err := json.Unmarshal(at, &w); err != nil || w.Message == "" {
		return fmt.Errorf("web settle: bad message %s", at)
	}
	return c.Store.Settle(ctx, w.Message, text, time.Now().UTC())
}

func (c Channel) SendFile(ctx context.Context, p perception.Percept, name string, data []byte, caption string) error {
	kind := mime.TypeByExtension(filepath.Ext(name))
	if kind == "" {
		kind = "application/octet-stream"
	}
	f := memory.SentFile{ID: ulid.Make().String(), Name: name, ContentType: kind, Data: data, At: time.Now().UTC()}
	if err := c.Store.SaveFile(ctx, f); err != nil {
		return err
	}
	_, err := c.say(ctx, p, memory.ChatFile, caption, Sent{File: f.ID, Name: name, Size: len(data)})
	return err
}

// say saves a row from the agent, in p's topic, and returns where it is, for Settle.
func (c Channel) say(ctx context.Context, p perception.Percept, kind, text string, payload any) (json.RawMessage, error) {
	m := memory.ChatMessage{ID: ulid.Make().String(), Author: memory.ChatAgent, Kind: kind, Text: text, At: time.Now().UTC(), Topic: p.Topic}
	if payload != nil {
		var err error
		if m.Payload, err = json.Marshal(payload); err != nil {
			return nil, err
		}
	}
	if err := c.Store.SaveChat(ctx, m); err != nil {
		return nil, err
	}
	return json.Marshal(where{Message: m.ID})
}
