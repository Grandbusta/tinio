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
	"github.com/Grandbusta/tinio/internal/mind"
	"github.com/Grandbusta/tinio/internal/providers/system2"
	"github.com/Grandbusta/tinio/internal/store"
)

// Settings are how jobs run, as the owner set them.
type Settings struct {
	Model     string // Claude Code's model id, e.g. claude-opus-5-5
	ModelName string // as shown: "Opus 5.5"
	Copy      bool   // on a copy of the repository, on its own branch
	Minutes   int    // the longest a job runs before it's stopped
	MaxSteps  int
	Review    bool // the agent checks each round before the owner sees it
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

	ctx      context.Context
	mu       sync.Mutex
	runs     map[string]*run         // by job id: those with a CLI running
	reviewer system2.Reasoner        // what agents check work with; nil: they don't
	gates    map[string]*coding.Gate // by job id: what the owner allowed for the job, kept across its rounds
	parked   map[string]*parked      // by job id: its CLI, left open after a round for the next
}

// A parked CLI is a job's, left open after a round finished, for a while: the
// next round (the owner's reply, or the agent sending it back) goes straight in,
// with nothing started or loaded again.
type parked struct {
	sess   coding.Session
	model  string
	cancel context.CancelFunc
	timer  *time.Timer
}

// parkFor is how long a CLI waits for the next round before it's closed; after
// that, the next round resumes its session in a new one.
const parkFor = 2 * time.Minute

// park keeps job's CLI open for the next round, for parkFor.
func (s *Service) park(job string, sess coding.Session, model string, cancel context.CancelFunc) {
	p := &parked{sess: sess, model: model, cancel: cancel}
	s.mu.Lock()
	if s.parked == nil {
		s.parked = map[string]*parked{}
	}
	old := s.parked[job]
	s.parked[job] = p
	p.timer = time.AfterFunc(parkFor, func() {
		if s.unpark(job, "") == p {
			p.close()
		}
	})
	s.mu.Unlock()
	if old != nil {
		old.close()
	}
}

// unpark takes job's parked CLI, if it has one still running with model ("":
// any); nil if not.
func (s *Service) unpark(job, model string) *parked {
	s.mu.Lock()
	p := s.parked[job]
	delete(s.parked, job)
	s.mu.Unlock()
	if p == nil {
		return nil
	}
	p.timer.Stop()
	if !p.sess.Alive() || (model != "" && p.model != model) {
		p.close()
		return nil
	}
	return p
}

func (p *parked) close() {
	go func() {
		p.sess.Close()
		p.cancel()
	}()
}

// gate is job's gate, the same each round while Tinio runs.
func (s *Service) gate(j Job) *coding.Gate {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gates == nil {
		s.gates = map[string]*coding.Gate{}
	}
	g, ok := s.gates[j.ID]
	if !ok {
		g = &coding.Gate{Rules: coding.Rules{Dir: j.Dir, Commands: coding.Commands(j.Dir)}}
		s.gates[j.ID] = g
	}
	return g
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
		s.editCard(ctx, j, func(c *Card) { c.Now = "Stopped when Tinio closed. Reply here to carry it on." })
	}
	// Its questions still open can't be answered now: the CLI that asked is gone.
	if _, err := s.DB.Exec(ctx, `UPDATE chat_messages SET settled_at = $1, text = text || $2
		WHERE kind = 'job_ask' AND settled_at IS NULL`, time.Now().UTC(), "\n\nThe job ended before you answered."); err != nil {
		return err
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
	BaseSHA                                string // the commit it started from
	Rounds                                 int    // snapshots of its copy, one after each round
	CommitSHA, PRURL                       string // on the owner's yes, and their second
}

const jobColumns = `id, agent_id, folder_id, topic_id, title, brief, cli, model, repo, dir, branch, base, session, status, card, cost_usd, turns, started_at, ended_at, base_sha, rounds, commit_sha, pr_url`

func (s *Service) query(ctx context.Context, sql string, args ...any) ([]Job, error) {
	rows, err := s.DB.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("jobs: %w", err)
	}
	return db.CollectRows(rows, func(r db.Scanner) (Job, error) {
		var j Job
		var ended string
		err := r.Scan(&j.ID, &j.Agent, &j.Folder, &j.Topic, &j.Title, &j.Brief, &j.CLI, &j.Model, &j.Repo, &j.Dir, &j.Branch, &j.Base,
			&j.Session, &j.Status, &j.Card, &j.CostUSD, &j.Turns, &j.StartedAt, &ended, &j.BaseSHA, &j.Rounds, &j.CommitSHA, &j.PRURL)
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
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)
		ON CONFLICT (id) DO UPDATE SET topic_id = excluded.topic_id, dir = excluded.dir, branch = excluded.branch, base = excluded.base,
			session = excluded.session, status = excluded.status, card = excluded.card, cost_usd = excluded.cost_usd,
			turns = excluded.turns, ended_at = excluded.ended_at, base_sha = excluded.base_sha, rounds = excluded.rounds,
			commit_sha = excluded.commit_sha, pr_url = excluded.pr_url`,
		j.ID, j.Agent, j.Folder, j.Topic, j.Title, j.Brief, j.CLI, j.Model, j.Repo, j.Dir, j.Branch, j.Base,
		j.Session, j.Status, j.Card, j.CostUSD, j.Turns, j.StartedAt.UTC(), ended, j.BaseSHA, j.Rounds, j.CommitSHA, j.PRURL)
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
	Rounds  int      `json:"rounds"`
	Commit  string   `json:"commit"`  // its commit's short sha, on the owner's yes
	PR      string   `json:"pr"`      // its pull request, on their second
	Verdict string   `json:"verdict"` // the agent's word on it, once they've checked it
}

func cardOf(j Job) Card {
	return Card{Job: j.ID, Title: j.Title, Folder: filepath.Base(j.Repo), Branch: j.Branch, Base: j.Base, CLI: j.CLI,
		Model: j.Model, Status: j.Status, Files: []string{}, CostUSD: j.CostUSD}
}

// CardOf is j's card as it is now.
func (s *Service) CardOf(ctx context.Context, j Job) Card { return s.card(ctx, j) }

// card is j's card as it is now.
func (s *Service) card(ctx context.Context, j Job) Card {
	c := cardOf(j)
	if m, ok, err := store.New(s.DB).For(j.Agent).ChatMessage(ctx, j.Card); err == nil && ok {
		json.Unmarshal(m.Payload, &c)
	}
	return c
}

// editCard changes j's card as it is now, keeping what edit doesn't change.
func (s *Service) editCard(ctx context.Context, j Job, edit func(*Card)) {
	c := s.card(ctx, j)
	c.Status, c.Rounds, c.CostUSD = j.Status, j.Rounds, j.CostUSD
	if j.CommitSHA != "" {
		c.Commit = j.CommitSHA[:min(7, len(j.CommitSHA))]
	}
	c.PR = j.PRURL
	edit(&c)
	s.showCard(ctx, j, c)
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
	if j.BaseSHA, err = git(ctx, repo, "rev-parse", "HEAD"); err != nil {
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
	// The job belongs where it was asked for; a project's own topic otherwise.
	if j.Topic = mind.TopicOf(ctx); j.Topic == "" {
		if j.Topic, err = s.projectTopic(ctx, st, agent, f.Name); err != nil {
			s.Log.Warn("jobs: no topic for the project; the job is in none", "job", id, "err", err)
		}
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
	s.step(ctx, j.ID, WhoAgent, "Wrote the brief", j.Brief)
	s.step(ctx, j.ID, WhoOwner, "Started the job", "")
	r := &run{s: s, job: j, card: c, gate: s.gate(j), asks: map[string]pending{}, over: make(chan struct{})}
	r.owner = s.ownerName(ctx, agent)
	s.mu.Lock()
	s.runs[id] = r
	s.mu.Unlock()
	go r.drive(ad, set, coding.Job{Dir: j.Dir, Brief: j.Brief, System: r.system(set), Model: set.Model, MaxTurns: set.MaxSteps}, nil)
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

// Choose answers Claude Code's own question to the owner: each question's text,
// with what they chose or wrote.
func (s *Service) Choose(ctx context.Context, agent, job, ask string, answers map[string]string) error {
	r, ok := s.running(agent, job)
	if !ok {
		return ErrNoJob
	}
	return r.choose(ctx, ask, answers)
}

// Reply sends the owner's words to the job: to it as it works, or, when it
// has finished or stopped, carrying it on with them.
func (s *Service) Reply(ctx context.Context, agent, job, text string) error {
	if r, ok := s.running(agent, job); ok {
		if r.hear(text) {
			s.step(ctx, job, WhoOwner, "Sent more", text)
			return nil
		}
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
	if _, err := os.Stat(j.Dir); j.Dir == "" || err != nil {
		return ErrNoJob // its copy is gone: there's nothing to carry on in
	}
	// Ended any way (finished, stopped, stopped by you, failed): it carries on. With no
	// session to resume, Claude Code starts afresh in the copy, told where it stands.
	s.step(ctx, j.ID, WhoOwner, "Sent more", text)
	if j.Session == "" {
		text = s.freshBrief(ctx, j, text)
	}
	return s.carryOn(ctx, j, text, "Carrying on with what you said")
}

// Checking is whether the agent is checking the job's last round now.
func (s *Service) Checking(job string) bool {
	s.mu.Lock()
	r := s.runs[job]
	s.mu.Unlock()
	return r != nil && r.isChecking()
}

// Saying is what the job's CLI is writing now, so far: "" when nothing.
func (s *Service) Saying(job string) string {
	s.mu.Lock()
	r := s.runs[job]
	s.mu.Unlock()
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saying
}

// freshBrief is what a new Claude Code session is told to carry j on with text:
// the job's brief and where it got to, the earlier rounds' work already in its copy.
func (s *Service) freshBrief(ctx context.Context, j Job, text string) string {
	var b strings.Builder
	b.WriteString("You're carrying on a coding job in this copy of the repository. What earlier rounds did is already here, uncommitted or on this branch: look at git status and git diff before you change anything.\n\nThe job's brief was:\n")
	b.WriteString(j.Brief)
	if steps, err := s.Steps(ctx, j.ID); err == nil {
		for i := len(steps) - 1; i >= 0; i-- {
			if st := steps[i]; st.Who == WhoCLI && st.Detail != "" && (st.What == "Finished" || st.What == "Stopped" || st.What == "Didn't work out" || st.What == "Stopped by you") {
				b.WriteString("\n\nWhere it got to, as last said:\n" + st.Detail)
				break
			}
		}
	}
	b.WriteString("\n\nWhat to do now:\n" + text)
	return b.String()
}

// carryOn starts another round of j, in its CLI session, with text.
func (s *Service) carryOn(ctx context.Context, j Job, text, now string) error {
	ad, err := s.adapter()
	if err != nil {
		return err
	}
	set, err := s.Settings(ctx)
	if err != nil {
		return err
	}
	j.Status, j.EndedAt = Working, time.Time{}
	r := &run{s: s, job: j, card: s.card(ctx, j), gate: s.gate(j), asks: map[string]pending{}, over: make(chan struct{})}
	r.owner = s.ownerName(ctx, j.Agent)
	r.card.Now, r.card.Status, r.card.Summary, r.card.Verdict = now, Working, "", ""
	s.mu.Lock()
	s.runs[j.ID] = r
	s.mu.Unlock()
	if err := s.save(ctx, j); err != nil {
		return err
	}
	go r.drive(ad, set, coding.Job{Dir: j.Dir, Brief: text, System: r.system(set), Model: set.Model, MaxTurns: set.MaxSteps, Resume: j.Session}, s.unpark(j.ID, set.Model))
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

// Who did a step.
const (
	WhoAgent = "agent"
	WhoCLI   = "cli"
	WhoOwner = "owner"
	WhoTinio = "tinio"
)

// The steps that tell a round as it goes, for the job's page: where each round
// starts (Tinio, with what started it), what Claude Code said, and its plan
// (its to-do list, as JSON, each time it changes). Its actions are steps of
// their own, told as doing() says.
const (
	RoundStarted = "Round started"
	Said         = "Said"
	Planned      = "Planned"
)

// A Step is one thing in a job's story, told by who did it.
type Step struct {
	At     time.Time `json:"at"`
	Who    string    `json:"who"`
	What   string    `json:"what"`
	Detail string    `json:"detail"`
}

// step keeps one thing a job did; a failure to is only logged.
func (s *Service) step(ctx context.Context, job, who, what, detail string) {
	if r := []rune(detail); len(r) > 4000 {
		detail = string(r[:4000]) + "…"
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO coding_steps (job_id, seq, at, who, what, detail)
		VALUES ($1, (SELECT coalesce(max(seq), 0) + 1 FROM coding_steps WHERE job_id = $1), $2, $3, $4, $5)`,
		job, time.Now().UTC(), who, what, detail); err != nil {
		s.Log.Warn("jobs: keeping a step failed", "job", job, "err", err)
	}
}

// Steps is a job's story, oldest first.
func (s *Service) Steps(ctx context.Context, job string) ([]Step, error) {
	rows, err := s.DB.Query(ctx, `SELECT at, who, what, detail FROM coding_steps WHERE job_id = $1 ORDER BY seq`, job)
	if err != nil {
		return nil, fmt.Errorf("jobs: steps: %w", err)
	}
	return db.CollectRows(rows, func(r db.Scanner) (Step, error) {
		var st Step
		err := r.Scan(&st.At, &st.Who, &st.What, &st.Detail)
		return st, err
	})
}
