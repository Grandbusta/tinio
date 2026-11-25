package controllers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/clis"
	"github.com/Grandbusta/tinio/internal/db"
)

// Coding is how coding jobs run, as Settings shows it: who writes the code, on
// a copy or not, checked or not, how long, and the CLI and gh they'd use.
type Coding struct {
	DB     *db.DB
	Finder *clis.Finder
}

type codingView struct {
	Settings models.Coding `json:"settings"`
	Claude   clis.CLI      `json:"claude"` // the CLI that writes the code
	GitHub   clis.GitHub   `json:"github"` // pushes and pull requests, as the owner
}

func (cd *Coding) Get(c *gin.Context) error {
	return cd.answer(c)
}

// Change changes what's sent, and keeps the rest.
func (cd *Coding) Change(c *gin.Context) error {
	var in struct {
		Coder    *string `json:"coder"`
		Copy     *bool   `json:"copy"`
		Review   *bool   `json:"review"`
		Minutes  *int    `json:"minutes"`
		MaxSteps *int    `json:"max_steps"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	s, err := models.CodingSettings(c, cd.DB)
	if err != nil {
		return err
	}
	if in.Coder != nil {
		s.Coder = *in.Coder
	}
	if in.Copy != nil {
		s.Copy = *in.Copy
	}
	if in.Review != nil {
		s.Review = *in.Review
	}
	if in.Minutes != nil {
		s.Minutes = *in.Minutes
	}
	if in.MaxSteps != nil {
		s.MaxSteps = *in.MaxSteps
	}
	if field, why := s.Valid(); field != "" {
		return utils.Invalid(field, why)
	}
	if err := models.SaveCoding(c, cd.DB, s, time.Now().UTC()); err != nil {
		return err
	}
	return cd.answer(c)
}

func (cd *Coding) answer(c *gin.Context) error {
	s, err := models.CodingSettings(c, cd.DB)
	if err != nil {
		return err
	}
	claude, _ := cd.Finder.Get(c, clis.Claude)
	utils.SuccessWithData(c, http.StatusOK, codingView{Settings: s, Claude: claude, GitHub: cd.Finder.GH(c)})
	return nil
}
