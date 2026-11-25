package controllers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/coding/jobs"
)

// Jobs are an agent's coding jobs: the list (for the strip at the top of their
// chat), the owner's answers to a job's questions, and stopping one.
type Jobs struct {
	Jobs *jobs.Service
}

type codingJobView struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Folder    string    `json:"folder"`
	Topic     string    `json:"topic"`
	Branch    string    `json:"branch"`
	CLI       string    `json:"cli"`
	Model     string    `json:"model"`
	Status    string    `json:"status"`
	Card      string    `json:"card"`
	CostUSD   float64   `json:"cost_usd"`
	StartedAt time.Time `json:"started_at"`
}

func (j *Jobs) List(c *gin.Context) error {
	list, err := j.Jobs.Jobs(c, c.Param("id"), 50)
	if err != nil {
		return err
	}
	out := make([]codingJobView, 0, len(list))
	for _, x := range list {
		out = append(out, codingJobView{ID: x.ID, Title: x.Title, Folder: folderName(x.Repo), Topic: x.Topic, Branch: x.Branch, CLI: x.CLI,
			Model: x.Model, Status: x.Status, Card: x.Card, CostUSD: x.CostUSD, StartedAt: x.StartedAt})
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"jobs": out})
	return nil
}

// Answer is the owner's answer to a job's question: "once", "job" (for the rest
// of the job) or "no", with what they'd like instead.
func (j *Jobs) Answer(c *gin.Context) error {
	var in struct {
		Answer string `json:"answer"`
		Note   string `json:"note"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	if in.Answer != jobs.Once && in.Answer != jobs.ForJob && in.Answer != jobs.No {
		return utils.Invalid("answer", "Answer once, job or no.")
	}
	if err := j.Jobs.Answer(c, c.Param("id"), c.Param("job"), c.Param("ask"), in.Answer, in.Note); err != nil {
		if errors.Is(err, jobs.ErrNoJob) {
			return utils.Invalid("ask", "That question isn't waiting any more.")
		}
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"answered": true})
	return nil
}

func (j *Jobs) Cancel(c *gin.Context) error {
	if err := j.Jobs.Cancel(c, c.Param("id"), c.Param("job")); err != nil {
		if errors.Is(err, jobs.ErrNoJob) {
			return utils.Invalid("job", "That job isn't running.")
		}
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"stopped": true})
	return nil
}

func folderName(repo string) string {
	for i := len(repo) - 1; i >= 0; i-- {
		if repo[i] == '/' || repo[i] == '\\' {
			return repo[i+1:]
		}
	}
	return repo
}
