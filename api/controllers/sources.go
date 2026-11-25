package controllers

import (
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/clis"
	"github.com/Grandbusta/tinio/internal/db"
	"github.com/Grandbusta/tinio/internal/secret"
)

// Sources are where the agents can think: the coding CLIs on this computer
// (only Claude Code so far) and the owner's OpenAI key. The owner picks one,
// and every job takes its default model there.
type Sources struct {
	DB     *db.DB
	Key    secret.Key
	Finder *clis.Finder
}

type sourcesView struct {
	Source string     `json:"source"` // "openai" or "claude"
	Key    bool       `json:"key"`    // an OpenAI key is set, here or in the environment
	CLIs   []clis.CLI `json:"clis"`
}

func (s *Sources) Get(c *gin.Context) error {
	if c.Query("again") != "" {
		s.Finder.Forget() // after signing in, say
	}
	return s.answer(c)
}

// Use makes the source in "source" where they think, every job on its
// default, and the agents start again on it.
func (s *Sources) Use(c *gin.Context) error {
	var in struct {
		Source string `json:"source"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	switch in.Source {
	case models.SourceClaude:
		s.Finder.Forget()
		if cl, ok := s.Finder.Get(c, clis.Claude); !ok || !cl.SignedIn {
			return utils.Invalid("source", "Claude Code isn't signed in on this computer. Run claude in a terminal, sign in, then try again.")
		}
	case models.SourceOpenAI:
		if ok, err := s.keySet(c); err != nil {
			return err
		} else if !ok {
			return utils.Invalid("source", "Set your OpenAI key first.")
		}
	default:
		return utils.Invalid("source", "That isn't somewhere they can think.")
	}
	if err := models.UseSource(c, s.DB, in.Source, time.Now().UTC()); err != nil {
		return err
	}
	s.DB.Notify(models.KeysChanged, "models")
	return s.answer(c)
}

func (s *Sources) keySet(c *gin.Context) (bool, error) {
	if os.Getenv("OPENAI_API_KEY") != "" {
		return true, nil
	}
	value, err := models.OpenKey(c, s.DB, s.Key, models.KeyOpenAI)
	return value != "", err
}

func (s *Sources) answer(c *gin.Context) error {
	source, err := models.Source(c, s.DB)
	if err != nil {
		return err
	}
	key, err := s.keySet(c)
	if err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, sourcesView{Source: source, Key: key, CLIs: s.Finder.All(c)})
	return nil
}
