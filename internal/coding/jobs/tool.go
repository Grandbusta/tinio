package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/Grandbusta/tinio/internal/action"
	"github.com/Grandbusta/tinio/internal/mind"
)

// Tool is the agent's code.start: a coding job, proposed with its brief, which
// starts on the owner's yes. Nil when none of folders is one they may make
// files in: a job changes files.
func (s *Service) Tool(agent string, folders []Folder, model string) mind.Doer {
	if len(folders) == 0 {
		return nil
	}
	return start{s: s, agent: agent, folders: folders, model: model}
}

type start struct {
	s       *Service
	agent   string
	folders []Folder
	model   string // as shown
}

type startArgs struct {
	Folder string `json:"folder"`
	Title  string `json:"title"`
	Brief  string `json:"brief"`
}

func (start) Name() string { return "code.start" }

func (t start) Describe() string {
	names := make([]string, len(t.folders))
	var known []string
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for i, f := range t.folders {
		names[i] = f.Name
		if notes, err := t.s.Notes(ctx, t.agent, f.ID); err == nil && len(notes) > 0 {
			known = append(known, notesNote("What you keep about "+f.Name+" from earlier jobs (put what matters in the brief):", notes))
		}
	}
	extra := ""
	if len(known) > 0 {
		extra = "\n" + strings.Join(known, "\n")
	}
	return `Start a coding job: Claude Code writes the code in one of the owner's folders that's a git repository (` + strings.Join(names, ", ") +
		`), on a copy of it on its own branch, and you see it through. Use it for any change to files there, code or not (a README row, a doc, a config value), even one line: it's made on its own branch and the owner reviews it. ` +
		`Arguments: {"folder": "its name", "title": "the job in a few words, e.g. Fix the basket rounding", ` +
		`"brief": "what to do and why, what done looks like (tests to add or pass), the repository's rules that matter, and what not to touch"}. ` +
		`Read the folder first (its README, AGENTS.md or CLAUDE.md, the code in question), so the brief is specific. The owner sees the brief and says yes first. ` +
		`If one of your jobs is already about this, carry it on (code.carry_on) instead of starting another.` + extra
}

func (start) Class() action.Class { return action.Code }
func (start) Destructive() bool   { return false }
func (start) Quick() bool         { return true } // it starts the job and returns
func (start) Reports() bool       { return true } // the job's card says how it goes

func (t start) parse(raw string) (startArgs, Folder, error) {
	var a startArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return a, Folder{}, errors.New(`arguments must be {"folder", "title", "brief"}`)
	}
	a.Title, a.Brief = strings.TrimSpace(a.Title), strings.TrimSpace(a.Brief)
	if a.Title == "" || a.Brief == "" {
		return a, Folder{}, errors.New("a job needs a title and a brief")
	}
	for _, f := range t.folders {
		if strings.EqualFold(f.Name, strings.TrimSpace(a.Folder)) {
			return a, f, nil
		}
	}
	return a, Folder{}, errors.New("that isn't one of the folders you may make files in")
}

func (t start) Check(raw string) error {
	_, f, err := t.parse(raw)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = Repo(ctx, f.Path)
	return err
}

func (t start) Do(ctx context.Context, raw string, _ time.Time) (string, error) {
	a, f, err := t.parse(raw)
	if err != nil {
		return "", err
	}
	j, err := t.s.Begin(ctx, t.agent, f, a.Title, a.Brief)
	if err != nil {
		return "", err
	}
	return "Started the coding job " + j.Title + " in " + f.Name + ": its card shows it as it goes.", nil
}

// Explain is the owner's card: the brief, and where and with what it runs.
func (t start) Explain(raw string) (title, where, quote string) {
	a, f, err := t.parse(raw)
	if err != nil {
		return "Start a coding job?", "", raw
	}
	where = "A copy of " + f.Name + ", on its own branch · Claude Code"
	if t.model != "" {
		where += " · " + t.model
	}
	return "Start a coding job in " + f.Name + ": " + a.Title + "?", where, a.Brief
}

// CarryOnTool is the agent's code.carry_on: more for one of their coding jobs,
// in its copy and on its branch, where Claude Code left it, on the owner's yes.
// Nil when they have no folders for jobs.
func (s *Service) CarryOnTool(agent string, folders []Folder) mind.Doer {
	if len(folders) == 0 {
		return nil
	}
	return carry{s: s, agent: agent}
}

type carry struct {
	s     *Service
	agent string
}

type carryArgs struct {
	Job  string `json:"job"`
	What string `json:"what"`
}

func (carry) Name() string { return "code.carry_on" }

// jobWord is where a job stands, in a word or two, for the agent.
func jobWord(j Job) string {
	switch {
	case j.PRURL != "":
		return "committed, pull request open"
	case j.CommitSHA != "":
		return "committed"
	}
	return map[string]string{Working: "working", Waiting: "waiting on the owner's answer", Ready: "ready for the owner to look at",
		Stopped: "stopped", Failed: "didn't work out", Cancelled: "stopped by the owner"}[j.Status]
}

func (t carry) Describe() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	js, _ := t.s.Jobs(ctx, t.agent, 8)
	var b strings.Builder
	b.WriteString(`Carry on one of your coding jobs: Claude Code takes it up where it left off, in the same copy and on the same branch, with what you say. ` +
		`Use it for more work on what a job is already about (another file, a change to what it did, the owner's next ask about it), rather than starting another job. ` +
		`Arguments: {"job": "its id", "what": "what to do now: specific, as you'd brief it"}. The owner sees it and says yes first.`)
	if len(js) == 0 {
		b.WriteString(" You have no coding jobs yet.")
		return b.String()
	}
	b.WriteString(" Your jobs, newest first:")
	for _, j := range js {
		b.WriteString("\n- " + j.ID + ": " + j.Title + " (" + filepathBase(j.Repo) + ", " + jobWord(j) + ")")
	}
	return b.String()
}

func filepathBase(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func (carry) Class() action.Class { return action.Code }
func (carry) Destructive() bool   { return false }
func (carry) Quick() bool         { return true } // it sends it on and returns
func (carry) Reports() bool       { return true } // the job's card says how it goes

func (t carry) parse(raw string) (carryArgs, Job, error) {
	var a carryArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return a, Job{}, errors.New(`arguments must be {"job", "what"}`)
	}
	a.Job, a.What = strings.TrimSpace(a.Job), strings.TrimSpace(a.What)
	if a.What == "" {
		return a, Job{}, errors.New("say what the job should do now")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	j, err := t.s.Get(ctx, t.agent, a.Job)
	if err != nil {
		// By its title, too: the newest of that name.
		js, _ := t.s.Jobs(ctx, t.agent, 50)
		for _, x := range js {
			if strings.EqualFold(x.Title, a.Job) {
				return a, x, nil
			}
		}
		return a, Job{}, errors.New("that isn't one of your coding jobs: give its id")
	}
	return a, j, nil
}

func (t carry) Check(raw string) error {
	_, j, err := t.parse(raw)
	if err != nil {
		return err
	}
	if _, err := os.Stat(j.Dir); j.Dir == "" || err != nil {
		return errors.New("that job's copy is gone: start a new job instead")
	}
	return nil
}

func (t carry) Do(ctx context.Context, raw string, _ time.Time) (string, error) {
	a, j, err := t.parse(raw)
	if err != nil {
		return "", err
	}
	if err := t.s.sendMore(ctx, t.agent, j.ID, a.What, WhoAgent); err != nil {
		return "", err
	}
	return "Carried on the coding job " + j.Title + ": its card shows it as it goes.", nil
}

// Explain is the owner's card: the job, and what it's to do now.
func (t carry) Explain(raw string) (title, where, quote string) {
	a, j, err := t.parse(raw)
	if err != nil {
		return "Carry on a coding job?", "", raw
	}
	where = filepathBase(j.Repo) + ", on " + j.Branch + " · Claude Code"
	return "Carry on the coding job " + j.Title + "?", where, a.What
}
