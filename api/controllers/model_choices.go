package controllers

import (
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/db"
	"github.com/Grandbusta/tinio/internal/pricing"
)

// Models are the models the agents use for each of their jobs: replies,
// decisions, their own day, light mode and works. The owner picks one of the
// models Tinio has prices for; a job set in the environment (OPENAI_MODEL, ...)
// is shown and can't be changed here.
type Models struct {
	DB *db.DB
}

type modelOption struct {
	Model  string  `json:"model"`
	Input  float64 `json:"input"`  // USD per 1M tokens
	Output float64 `json:"output"` // USD per 1M tokens
}

type jobView struct {
	Job     string        `json:"job"`
	Model   string        `json:"model"`   // "" for work: the replies' model
	From    string        `json:"from"`    // "app", "environment" or "default"
	Default string        `json:"default"` // "" for work: the replies' model
	Models  []modelOption `json:"models"`  // dearest first
}

func (m *Models) Get(c *gin.Context) error {
	return m.answer(c)
}

// Choose keeps the model for a job ("" for Tinio's default), and the agents
// start again on it.
func (m *Models) Choose(c *gin.Context) error {
	var in struct {
		Job   string  `json:"job"`
		Model *string `json:"model"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Model == nil {
		return utils.ErrBadRequest
	}
	job, ok := models.JobNamed(in.Job)
	if !ok {
		return utils.Invalid("job", "That isn't one of their jobs.")
	}
	chosen, err := models.ChosenModels(c, m.DB)
	if err != nil {
		return err
	}
	if chosen[job.Name].From == "environment" {
		return utils.Invalid("model", "It's set in the environment ("+job.Env+") where Tinio runs: change it there.")
	}
	if *in.Model != "" && !slices.Contains(job.Models, *in.Model) {
		return utils.Invalid("model", "Pick one of the models listed.")
	}
	if err := models.ChooseModel(c, m.DB, job.Name, *in.Model, time.Now().UTC()); err != nil {
		return err
	}
	m.DB.Notify(models.KeysChanged, "models")
	return m.answer(c)
}

func (m *Models) answer(c *gin.Context) error {
	chosen, err := models.ChosenModels(c, m.DB)
	if err != nil {
		return err
	}
	out := make([]jobView, 0, len(models.Jobs))
	for _, j := range models.Jobs {
		v := jobView{Job: j.Name, Model: chosen[j.Name].Model, From: chosen[j.Name].From, Default: j.Default}
		for _, name := range j.Models {
			p := pricing.OpenAI[name]
			v.Models = append(v.Models, modelOption{Model: name, Input: p.Input, Output: p.Output})
		}
		out = append(out, v)
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"jobs": out})
	return nil
}
