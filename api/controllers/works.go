package controllers

import (
	"errors"
	"github.com/Grandbusta/tinio/internal/db"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/channel/web"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/render"
	"github.com/Grandbusta/tinio/internal/render/markdown"
	"github.com/Grandbusta/tinio/internal/store"
)

// Works is what an agent has made that their owner may see: for themselves or
// for their owner, never the journal or reviews (store.WorksFor). Behind
// middlewares.OwnAgent.
type Works struct {
	DB      *db.DB
	Formats *render.Set
}

const (
	worksShown = 100 // the newest, on the page
	peekRunes  = 180
)

type workRow struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"` // as people say it: "beat idea", not "beat_idea"
	Title   string    `json:"title"`
	Peek    string    `json:"peek,omitempty"`
	Text    string    `json:"text,omitempty"` // only when one is read
	At      time.Time `json:"at"`
	ForYou  bool      `json:"for_you"` // made for their owner, not for themselves
	Formats []string  `json:"formats"` // the files it can be made into now, e.g. "pdf"
}

func (wk *Works) row(w memory.Work, owner string, full bool) workRow {
	r := workRow{ID: w.ID.String(), Kind: strings.ReplaceAll(w.Kind, "_", " "), Title: w.Title, At: w.CreatedAt.UTC(),
		ForYou: w.MadeFor == owner, Formats: []string{}}
	if full {
		r.Text = w.Text
	} else {
		r.Peek = peek(w.Title, w.Text)
	}
	for _, f := range wk.Formats.For(w) {
		r.Formats = append(r.Formats, f.Name())
	}
	return r
}

// peek is the start of a work's words on one line, as plain text: its Markdown
// read as words, and a first line that repeats its title left out.
func peek(title, body string) string {
	var b strings.Builder
	for _, bl := range markdown.Blocks(title, body) {
		if b.Len() > 4*peekRunes {
			break
		}
		b.WriteString(bl.Text + " ")
	}
	text := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(text) > peekRunes {
		text = string([]rune(text)[:peekRunes-1]) + "…"
	}
	return text
}

func (wk *Works) store(c *gin.Context) (*store.Store, string) {
	ag := middlewares.Owned(c)
	return store.New(wk.DB).For(ag.ID), "account:" + ag.AccountID
}

// List is their works, newest first.
func (wk *Works) List(c *gin.Context) error {
	st, owner := wk.store(c)
	ws, err := st.WorksFor(c, owner, worksShown)
	if err != nil {
		return err
	}
	rows := make([]workRow, len(ws))
	for i, w := range ws {
		rows[i] = wk.row(w, owner, false)
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"works": rows})
	return nil
}

// One is a work, all of it.
func (wk *Works) One(c *gin.Context) error {
	w, owner, err := wk.work(c)
	if err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"work": wk.row(w, owner, true)})
	return nil
}

// File is a work made into a file in one of its formats, downloaded.
func (wk *Works) File(c *gin.Context) error {
	w, _, err := wk.work(c)
	if err != nil {
		return err
	}
	f, err := wk.Formats.File(c.Param("format"), w, middlewares.Owned(c).Name)
	if errors.Is(err, render.ErrNoData) {
		return utils.ErrNoSuchFile
	}
	if err != nil {
		if _, ok := wk.Formats.Get(c.Param("format")); !ok {
			return utils.ErrNoSuchFile
		}
		return err
	}
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, f.Type, f.Data)
	return nil
}

// Send has them send a work into their owner's chat: as a file in its first
// format (what it's best made into), or, with none, as a message. No model is
// asked: it goes as they made it, captioned with its title.
func (wk *Works) Send(c *gin.Context) error {
	w, _, err := wk.work(c)
	if err != nil {
		return err
	}
	ag := middlewares.Owned(c)
	chat := web.Channel{Store: store.New(wk.DB).For(ag.ID)}
	formats := wk.Formats.For(w)
	if len(formats) == 0 {
		if err := chat.Reply(c, perception.Percept{}, w.Title+"\n\n"+w.Text); err != nil {
			return err
		}
		utils.SuccessWithData(c, http.StatusCreated, gin.H{"sent": ""})
		return nil
	}
	f, err := wk.Formats.File(formats[0].Name(), w, ag.Name)
	if err != nil {
		return err
	}
	if err := chat.SendFile(c, perception.Percept{}, f.Name, f.Data, w.Title); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusCreated, gin.H{"sent": f.Name})
	return nil
}

func (wk *Works) work(c *gin.Context) (memory.Work, string, error) {
	id, err := ulid.Parse(c.Param("work"))
	if err != nil {
		return memory.Work{}, "", utils.ErrNoSuchWork
	}
	st, owner := wk.store(c)
	w, ok, err := st.WorkFor(c, id, owner)
	if err != nil {
		return w, owner, err
	}
	if !ok {
		return w, owner, utils.ErrNoSuchWork
	}
	return w, owner, nil
}
