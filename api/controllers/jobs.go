package controllers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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
	Brief     string    `json:"brief,omitempty"`
	Base      string    `json:"base"`
	Rounds    int       `json:"rounds"`
	Commit    string    `json:"commit,omitempty"`
	PR        string    `json:"pr,omitempty"`
	Turns     int       `json:"turns"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
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
		out = append(out, viewOf(x))
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"jobs": out})
	return nil
}

// Answer is the owner's answer to a job's question: "once", "job" (for the rest
// of the job) or "no", with what they'd like instead.
func (j *Jobs) Answer(c *gin.Context) error {
	var in struct {
		Answer  string            `json:"answer"`
		Note    string            `json:"note"`
		Choices map[string]string `json:"choices"` // "choose": Claude Code's own question, answered
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	if in.Answer == "choose" {
		if len(in.Choices) == 0 {
			return utils.Invalid("choices", "Choose an answer.")
		}
		if err := j.Jobs.Choose(c, c.Param("id"), c.Param("job"), c.Param("ask"), in.Choices); err != nil {
			if errors.Is(err, jobs.ErrNoJob) {
				return utils.Invalid("ask", "That question isn't waiting any more.")
			}
			return err
		}
		utils.SuccessWithData(c, http.StatusOK, gin.H{"answered": true})
		return nil
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

func viewOf(x jobs.Job) codingJobView {
	return codingJobView{ID: x.ID, Title: x.Title, Folder: folderName(x.Repo), Topic: x.Topic, Branch: x.Branch, CLI: x.CLI,
		Model: x.Model, Status: x.Status, Card: x.Card, CostUSD: x.CostUSD, StartedAt: x.StartedAt, Brief: x.Brief, Base: x.Base,
		Rounds: x.Rounds, Commit: x.CommitSHA, PR: x.PRURL, Turns: x.Turns, EndedAt: x.EndedAt}
}

// One is a job's page: its story, its changes (all of them, or ?round=n's),
// and whether it can be pushed.
func (j *Jobs) One(c *gin.Context) error {
	x, err := j.Jobs.Get(c, c.Param("id"), c.Param("job"))
	if err != nil {
		if errors.Is(err, jobs.ErrNoJob) {
			return utils.ErrNoSuchJob
		}
		return err
	}
	round, _ := strconv.Atoi(c.Query("round"))
	steps, err := j.Jobs.Steps(c, x.ID)
	if err != nil {
		return err
	}
	changes, err := j.Jobs.Changes(c, x, round)
	if err != nil {
		return err
	}
	notes, err := j.Jobs.Notes(c, x.Agent, x.Folder)
	if err != nil {
		return err
	}
	if notes == nil {
		notes = []string{}
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"job": viewOf(x), "steps": steps, "changes": changes, "github": j.Jobs.Finder.GH(c),
		"origin": j.Jobs.HasOrigin(c, x), "notes": notes})
	return nil
}

// Back sends the job back with the owner's notes: on the whole, and on lines. Plain is
// a message as it was written, to a job working (passed on) or done (carried on).
func (j *Jobs) Back(c *gin.Context) error {
	var in struct {
		Note  string `json:"note"`
		Plain bool   `json:"plain"`
		Lines []struct {
			File string `json:"file"`
			Line int    `json:"line"`
			Text string `json:"text"`
		} `json:"lines"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	var b strings.Builder
	if n := strings.TrimSpace(in.Note); n != "" {
		b.WriteString(n + "\n")
	}
	for _, l := range in.Lines {
		if t := strings.TrimSpace(l.Text); t != "" {
			fmt.Fprintf(&b, "- %s, line %d: %s\n", l.File, l.Line, t)
		}
	}
	if b.Len() == 0 {
		return utils.Invalid("note", "Say what to change.")
	}
	text := "The owner looked at your changes and sends them back:\n" + b.String()
	if in.Plain {
		text = strings.TrimSpace(b.String())
	}
	if err := j.Jobs.Reply(c, c.Param("id"), c.Param("job"), text); err != nil {
		if errors.Is(err, jobs.ErrNoJob) {
			return utils.Invalid("job", "That job has ended, and can't be carried on.")
		}
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"sent": true})
	return nil
}

// GoBack puts the job's copy back as it was before its last round.
func (j *Jobs) GoBack(c *gin.Context) error {
	x, err := j.Jobs.GoBack(c, c.Param("id"), c.Param("job"))
	return j.after(c, x, err)
}

// Notes sets what the agent keeps about the job's folder: the owner taking one out.
func (j *Jobs) Notes(c *gin.Context) error {
	var b struct {
		Notes []string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || b.Notes == nil {
		return utils.ErrBadRequest
	}
	x, err := j.Jobs.Get(c, c.Param("id"), c.Param("job"))
	if err != nil {
		if errors.Is(err, jobs.ErrNoJob) {
			return utils.ErrNoSuchJob
		}
		return err
	}
	if err := j.Jobs.SaveNotes(c, x.Agent, x.Folder, b.Notes); err != nil {
		return err
	}
	notes, err := j.Jobs.Notes(c, x.Agent, x.Folder)
	if err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"notes": notes})
	return nil
}

// Commit commits the job on its branch, on this computer: the owner's yes.
func (j *Jobs) Commit(c *gin.Context) error {
	var in struct {
		Message string `json:"message"`
	}
	c.ShouldBindJSON(&in)
	x, err := j.Jobs.Commit(c, c.Param("id"), c.Param("job"), in.Message)
	return j.after(c, x, err)
}

// Push pushes the job's branch and opens a pull request: the owner's second yes.
func (j *Jobs) Push(c *gin.Context) error {
	var in struct {
		Body string `json:"body"`
	}
	c.ShouldBindJSON(&in)
	x, err := j.Jobs.Push(c, c.Param("id"), c.Param("job"), in.Body)
	return j.after(c, x, err)
}

func (j *Jobs) after(c *gin.Context, x jobs.Job, err error) error {
	switch {
	case errors.Is(err, jobs.ErrNoJob):
		return utils.ErrNoSuchJob
	case err != nil:
		return utils.Invalid("job", capitalFirst(err.Error())+".")
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"job": viewOf(x)})
	return nil
}

func capitalFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func folderName(repo string) string {
	for i := len(repo) - 1; i >= 0; i-- {
		if repo[i] == '/' || repo[i] == '\\' {
			return repo[i+1:]
		}
	}
	return repo
}
