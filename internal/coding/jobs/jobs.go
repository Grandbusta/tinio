// Package jobs runs agents' coding jobs: a job is started (on the owner's yes
// to its brief) in one of the agent's folders that's a git repository, on a
// copy of it on its own branch, with Claude Code. One chat card shows it as it
// goes; what the job's rules don't allow is asked of the owner as a card with
// three answers; the owner's replies to the card go to the job.
//
// The Service lives as long as Tinio: agents rest and start again, and their
// hosts are made anew when the models change, but a job carries on.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/clis"
	"github.com/Grandbusta/tinio/internal/coding"
	"github.com/Grandbusta/tinio/internal/db"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// Settings are how jobs run, as the owner set them.
type Settings struct {
	Model     string // Claude Code's model id, e.g. claude-opus-5-5
	ModelName string // as shown: "Opus 5.5"
	Copy      bool   // on a copy of the repository, on its own branch
	Minutes   int    // the longest a job runs before it's stopped
	MaxSteps  int
}

// Statuses of a job.
const (
	Working   = "working"
	Waiting   = "waiting" // on the owner's answer
	Ready     = "ready"   // it finished: for the owner to look at
	Stopped   = "stopped" // out of time or steps, or Tinio closed: it can carry on
	Failed    = "failed"
	Cancelled = "cancelled"
)

// ErrNoJob is a job that isn't the agent's, or a question that isn't open.
var ErrNoJob = errors.New("jobs: no such job, or it has ended")

type Service struct {
	DB       *db.DB
	Finder   *clis.Finder
	Data     string // Tinio's data folder: copies go in Data/worktrees
	Settings func(ctx context.Context) (Settings, error)
	Log      *slog.Logger
	// Adapter starts jobs; nil: Claude Code, as the Finder finds it.
	Adapter coding.Adapter

	ctx  context.Context
	mu   sync.Mutex
	runs map[string]*run // by job id: those with a CLI running
}

// Start is the service, from now until ctx is done. Jobs that were running
// when Tinio last closed are stopped: their owner can carry them on.
func (s *Service) Start(ctx context.Context) error {
	s.ctx = ctx
	s.runs = map[string]*run{}
	rows, err := s.query(ctx, `SELECT `+jobColumns+` FROM coding_jobs WHERE status IN ('working', 'waiting')`)
	if err != nil {
		return err
	}
	for _, j := range rows {
		j.Status, j.EndedAt = Stopped, time.Now().UTC()
		if err := s.save(ctx, j); err != nil {
			return err
		}
		c := cardOf(j)
		c.Now = "Stopped when Tinio closed. Reply here to carry it on."
		s.showCard(ctx, j, c)
	}
	return nil
}

func (s *Service) adapter() (coding.Adapter, error) {
	if s.Adapter != nil {
		return s.Adapter, nil
	}
	c, ok := s.Finder.Get(s.ctx, clis.Claude)
	if !ok || !c.SignedIn {
		return nil, errors.New("Claude Code isn't signed in on this computer")
	}
	return coding.Claude{Program: c.Path}, nil
}

// A Job is one coding job, as kept.
type Job struct {
	ID, Agent, Folder, Topic, Title, Brief string
	CLI, Model, Repo, Dir, Branch, Base    string
	Session, Status, Card                  string
	CostUSD                                float64
	Turns                                  int
	StartedAt, EndedAt                     time.Time
}

const jobColumns = `id, agent_id, folder_id, topic_id, title, brief, cli, model, repo, dir, branch, base, session, status, card, cost_usd, turns, started_at, ended_at`

func (s *Service) query(ctx context.Context, sql string, args ...any) ([]Job, error) {
	rows, err := s.DB.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("jobs: %w", err)
	}
	return db.CollectRows(rows, func(r db.Scanner) (Job, error) {
		var j Job
		var ended string
		err := r.Scan(&j.ID, &j.Agent, &j.Folder, &j.Topic, &j.Title, &j.Brief, &j.CLI, &j.Model, &j.Repo, &j.Dir, &j.Branch, &j.Base,
			&j.Session, &j.Status, &j.Card, &j.CostUSD, &j.Turns, &j.StartedAt, &ended)
		if ended != "" {
			j.EndedAt, _ = time.Parse(time.RFC3339Nano, ended)
		}
		return j, err
	})
}

func (s *Service) save(ctx context.Context, j Job) error {
	ended := ""
	if !j.EndedAt.IsZero() {
		ended = j.EndedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO coding_jobs (`+jobColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		ON CONFLICT (id) DO UPDATE SET topic_id = excluded.topic_id, dir = excluded.dir, branch = excluded.branch, base = excluded.base,
			session = excluded.session, status = excluded.status, card = excluded.card, cost_usd = excluded.cost_usd,
			turns = excluded.turns, ended_at = excluded.ended_at`,
		j.ID, j.Agent, j.Folder, j.Topic, j.Title, j.Brief, j.CLI, j.Model, j.Repo, j.Dir, j.Branch, j.Base,
		j.Session, j.Status, j.Card, j.CostUSD, j.Turns, j.StartedAt.UTC(), ended)
	if err != nil {
		return fmt.Errorf("jobs: save: %w", err)
	}
	return nil
}

// Jobs is an agent's jobs, newest first.
func (s *Service) Jobs(ctx context.Context, agent string, limit int) ([]Job, error) {
	return s.query(ctx, `SELECT `+jobColumns+` FROM coding_jobs WHERE agent_id = $1 ORDER BY started_at DESC LIMIT $2`, agent, limit)
}

// Get is one of an agent's jobs.
func (s *Service) Get(ctx context.Context, agent, id string) (Job, error) {
	js, err := s.query(ctx, `SELECT `+jobColumns+` FROM coding_jobs WHERE agent_id = $1 AND id = $2`, agent, id)
	if err != nil {
		return Job{}, err
	}
	if len(js) == 0 {
		return Job{}, ErrNoJob
	}
	return js[0], nil
}

// A Card is a job's chat card, its row's payload.
type Card struct {
	Job     string   `json:"job"`
	Title   string   `json:"title"`
	Folder  string   `json:"folder"`
	Branch  string   `json:"branch"`
	Base    string   `json:"base"`
	CLI     string   `json:"cli"`   // "claude"
	Model   string   `json:"model"` // as shown
	Status  string   `json:"status"`
	Now     string   `json:"now"` // what it's doing, or how it ended
	Files   []string `json:"files"`
	Steps   int      `json:"steps"`
	Asking  int      `json:"asking"` // questions waiting on the owner
	CostUSD float64  `json:"cost_usd"`
	Summary string   `json:"summary"` // its last words, when it's done
}

func cardOf(j Job) Card {
	return Card{Job: j.ID, Title: j.Title, Folder: filepath.Base(j.Repo), Branch: j.Branch, Base: j.Base, CLI: j.CLI,
		Model: j.Model, Status: j.Status, Files: []string{}, CostUSD: j.CostUSD}
}

func (s *Service) showCard(ctx context.Context, j Job, c Card) {
	if j.Card == "" {
		return
	}
	b, _ := json.Marshal(c)
	if err := store.New(s.DB).For(j.Agent).Update(ctx, j.Card, "Coding job: "+j.Title+" · "+c.Status, b); err != nil {
		s.Log.Warn("jobs: showing a job's card failed", "job", j.ID, "err", err)
	}
}

// git runs git in dir and is what it printed.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Repo is the git repository dir is the top of, or an error saying it isn't one.
func Repo(ctx context.Context, dir string) (string, error) {
	top, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s isn't a git repository", filepath.Base(dir))
	}
	return top, nil
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(title string) string {
	s := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > 32 {
		s = strings.TrimRight(s[:32], "-")
	}
	if s == "" {
		s = "job"
	}
	return s
}

// A Folder is where a job works: one of the agent's folders.
type Folder struct {
	ID, Name, Path string
}

// Begin starts a job for agent in folder: a copy of the repository on a new
// branch (unless the owner works in place), the project's topic, its card in
// the chat, and Claude Code with the brief.
func (s *Service) Begin(ctx context.Context, agent string, f Folder, title, brief string) (Job, error) {
	if s.ctx == nil {
		return Job{}, errors.New("jobs: not started")
	}
	ad, err := s.adapter()
	if err != nil {
		return Job{}, err
	}
	set, err := s.Settings(ctx)
	if err != nil {
		return Job{}, err
	}
	repo, err := Repo(ctx, f.Path)
	if err != nil {
		return Job{}, err
	}
	id := ulid.Make().String()
	j := Job{ID: id, Agent: agent, Folder: f.ID, Title: strings.TrimSpace(title), Brief: strings.TrimSpace(brief), CLI: clis.Claude,
		Model: set.ModelName, Repo: repo, Dir: repo, Status: Working, StartedAt: time.Now().UTC()}
	if j.Base, err = git(ctx, repo, "rev-parse", "--abbrev-ref", "HEAD"); err != nil {
		return Job{}, err
	}
	if set.Copy {
		j.Branch = "tinio/" + slug(j.Title) + "-" + strings.ToLower(id[len(id)-4:])
		j.Dir = filepath.Join(s.Data, "worktrees", id)
		if err := os.MkdirAll(filepath.Dir(j.Dir), 0o700); err != nil {
			return Job{}, err
		}
		if _, err := git(ctx, repo, "worktree", "add", "-b", j.Branch, j.Dir, "HEAD"); err != nil {
			return Job{}, fmt.Errorf("making the job's copy: %w", err)
		}
	}
	st := store.New(s.DB).For(agent)
	if j.Topic, err = s.projectTopic(ctx, st, agent, f.Name); err != nil {
		s.Log.Warn("jobs: no topic for the project; the job is in none", "job", id, "err", err)
	}
	c := cardOf(j)
	c.Now = "Starting Claude Code"
	b, _ := json.Marshal(c)
	j.Card = ulid.Make().String()
	if err := st.SaveChat(ctx, memory.ChatMessage{ID: j.Card, Author: memory.ChatAgent, Kind: memory.ChatJob,
		Text: "Coding job: " + j.Title, Payload: b, At: time.Now().UTC(), Topic: j.Topic}); err != nil {
		return Job{}, err
	}
	if err := s.save(ctx, j); err != nil {
		return Job{}, err
	}
	r := &run{s: s, job: j, card: c, gate: &coding.Gate{Rules: coding.Rules{Dir: j.Dir, Commands: coding.Commands(j.Dir)}}, asks: map[string]pending{}, over: make(chan struct{})}
	r.owner = s.ownerName(ctx, agent)
	s.mu.Lock()
	s.runs[id] = r
	s.mu.Unlock()
	go r.drive(ad, set, coding.Job{Dir: j.Dir, Brief: j.Brief, System: r.system(set), Model: set.Model, MaxTurns: set.MaxSteps})
	return j, nil
}

// projectTopic is the agent's open topic named after the project, made if
// there's none: a project is a topic.
func (s *Service) projectTopic(ctx context.Context, st *store.Store, agent, name string) (string, error) {
	var account string
	if err := s.DB.QueryRow(ctx, `SELECT account_id FROM agents WHERE id = $1`, agent).Scan(&account); err != nil {
		return "", err
	}
	person := "account:" + account
	ts, err := st.Topics(ctx, person, time.Now().Add(-30*24*time.Hour), 200)
	if err != nil {
		return "", err
	}
	for _, t := range ts {
		if strings.EqualFold(t.Title, name) {
			return t.ID, nil
		}
	}
	t := memory.Topic{ID: ulid.Make().String(), Person: person, Title: name, StartedAt: time.Now().UTC()}
	return t.ID, st.StartTopic(ctx, t)
}

func (s *Service) ownerName(ctx context.Context, agent string) string {
	var name string
	if err := s.DB.QueryRow(ctx, `SELECT owner_name FROM agents WHERE id = $1`, agent).Scan(&name); err != nil || name == "" {
		return "The owner"
	}
	return name
}

func (s *Service) running(agent, id string) (*run, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	return r, ok && r.job.Agent == agent
}

// The owner's answers to a job's question.
const (
	Once   = "once"
	ForJob = "job"
	No     = "no"
)

// Answer gives the owner's answer to a job's question: once, for the rest of
// the job, or no, with what they'd like instead (note, may be "").
func (s *Service) Answer(ctx context.Context, agent, job, ask, answer, note string) error {
	r, ok := s.running(agent, job)
	if !ok {
		return ErrNoJob
	}
	return r.answer(ctx, ask, answer, note)
}

// Reply sends the owner's words to the job: to it as it works, or, when it
// has finished or stopped, carrying it on with them.
func (s *Service) Reply(ctx context.Context, agent, job, text string) error {
	if r, ok := s.running(agent, job); ok {
		if st := r.snapshot().Status; st == Working || st == Waiting {
			if err := r.send(text); err == nil {
				return nil
			}
		}
		r.wait() // it's ending: carry it on once it has
	}
	j, err := s.Get(ctx, agent, job)
	if err != nil {
		return err
	}
	if j.Session == "" || j.Status == Cancelled {
		return ErrNoJob
	}
	ad, err := s.adapter()
	if err != nil {
		return err
	}
	set, err := s.Settings(ctx)
	if err != nil {
		return err
	}
	j.Status, j.EndedAt = Working, time.Time{}
	r := &run{s: s, job: j, card: cardOf(j), gate: &coding.Gate{Rules: coding.Rules{Dir: j.Dir, Commands: coding.Commands(j.Dir)}}, asks: map[string]pending{}, over: make(chan struct{})}
	r.owner = s.ownerName(ctx, agent)
	r.card.Now = "Carrying on with what you said"
	s.mu.Lock()
	s.runs[j.ID] = r
	s.mu.Unlock()
	if err := s.save(ctx, j); err != nil {
		return err
	}
	go r.drive(ad, set, coding.Job{Dir: j.Dir, Brief: text, System: r.system(set), Model: set.Model, MaxTurns: set.MaxSteps, Resume: j.Session})
	return nil
}

// Cancel stops a running job; what it did is kept.
func (s *Service) Cancel(ctx context.Context, agent, job string) error {
	r, ok := s.running(agent, job)
	if !ok {
		return ErrNoJob
	}
	r.cancel()
	return nil
}
