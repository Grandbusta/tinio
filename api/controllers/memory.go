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

// Memory is what an agent remembers, about their owner and about their own
// life, for their owner to put right or have forgotten. Behind
// middlewares.OwnAgent.
type Memory struct {
	DB  *db.DB
	Now func() time.Time // nil: the clock
}

const (
	factsShown = 500
	factRunes  = 300
)

type factRow struct {
	ID    string    `json:"id"`
	About string    `json:"about"` // you (their owner) or self (their own life)
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
	Areas []string  `json:"areas"`
	From  string    `json:"from"` // the kind of thing it was learnt from: message, activity, observation, ...
}

func (m *Memory) store(c *gin.Context) *store.Store {
	return store.New(m.DB).For(middlewares.Owned(c).ID)
}

func (m *Memory) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Get is their facts about their owner and about themselves, newest first.
// Facts about anyone else (there are none on the platform, where only their
// owner talks to them) are left out.
func (m *Memory) Get(c *gin.Context) error {
	owner := "account:" + middlewares.Owned(c).AccountID
	fs, err := m.store(c).AllFacts(c, factsShown)
	if err != nil {
		return err
	}
	facts := []factRow{}
	for _, f := range fs {
		about := "self"
		switch f.Subject {
		case memory.SubjectSelf:
		case owner:
			about = "you"
		default:
			continue
		}
		areas := f.Areas
		if areas == nil {
			areas = []string{}
		}
		facts = append(facts, factRow{ID: f.ID.String(), About: about, Text: f.Text, At: f.At.UTC(), Areas: areas, From: f.From})
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"facts": facts})
	return nil
}

func (m *Memory) fact(c *gin.Context) (ulid.ULID, error) {
	id, err := ulid.Parse(c.Param("fact"))
	if err != nil {
		return id, utils.ErrNoSuchFact
	}
	return id, nil
}

// Correct puts one of their facts the way it is, in their owner's words; it's
// used from their next reply on.
func (m *Memory) Correct(c *gin.Context) error {
	id, err := m.fact(c)
	if err != nil {
		return err
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	text := strings.Join(strings.Fields(in.Text), " ")
	if text == "" || utf8.RuneCountInString(text) > factRunes {
		return utils.Invalid("text", "A fact is a sentence, up to 300 characters.")
	}
	found, err := m.store(c).CorrectFact(c, id, text, m.now().UTC())
	if err != nil {
		return err
	}
	if !found {
		return utils.ErrNoSuchFact
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"text": text})
	return nil
}

// Forget deletes one of their facts for good: they never use it again.
func (m *Memory) Forget(c *gin.Context) error {
	id, err := m.fact(c)
	if err != nil {
		return err
	}
	found, err := m.store(c).DeleteFact(c, id)
	if err != nil {
		return err
	}
	if !found {
		return utils.ErrNoSuchFact
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{})
	return nil
}
