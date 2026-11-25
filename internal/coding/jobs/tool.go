package jobs

import (
	"context"
	"encoding/json"
	"errors"
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
	for i, f := range t.folders {
		names[i] = f.Name
	}
	return `Start a coding job: Claude Code writes the code in one of the owner's folders that's a git repository (` + strings.Join(names, ", ") +
		`), on a copy of it on its own branch, and you see it through. Use it when they want code written, fixed or changed there. ` +
		`Arguments: {"folder": "its name", "title": "the job in a few words, e.g. Fix the basket rounding", ` +
		`"brief": "what to do and why, what done looks like (tests to add or pass), the repository's rules that matter, and what not to touch"}. ` +
		`Read the folder first (its README, AGENTS.md or CLAUDE.md, the code in question), so the brief is specific. The owner sees the brief and says yes first.`
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
