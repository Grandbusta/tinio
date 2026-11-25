package models

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

// Where the agents think: on the owner's OpenAI key, or with their own Claude
// Code. The source picks every job's default; each job can still pick a model
// from either.
const (
	SourceOpenAI = "openai"
	SourceClaude = "claude"
)

// ClaudePrefix marks a choice that thinks with Claude Code (host.ClaudePrefix).
const ClaudePrefix = "claude:"

// sourceRow is the model_choices row that names the source: not a job.
const sourceRow = "source"

// ClaudeModels are Claude Code's models, dearest first, with their names.
var ClaudeModels = []struct{ ID, Name string }{
	{"claude-opus-5-5", "Opus 5.5"},
	{"claude-sonnet-5-5", "Sonnet 5.5"},
	{"claude-haiku-5-5", "Haiku 5.5"},
}

// A Job is one of the agents' jobs a model does, and the models it may have:
// OpenAI's that Tinio has prices for (internal/pricing), so a cap can hold,
// and Claude Code's, which report what they cost.
type Job struct {
	Name    string   // as kept in model_choices
	Env     string   // set in the environment, it's used instead (on the key), and can't be changed in the app
	Default string   // on the key; "" for work: the replies' model
	Claude  string   // the default with Claude Code (decided 25 November 2026)
	Models  []string // OpenAI's, dearest first
}

// The models Tinio can use, dearest first.
var (
	reasoningModels = []string{"gpt-6-astra", "gpt-6.2-sol", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6-luna"}
	decisionModels  = []string{"gpt-6-luna"} // the only one the Decisions API has
)

// Jobs are the agents' jobs, in the order they're shown.
var Jobs = []Job{
	{Name: "reply", Env: "OPENAI_MODEL", Default: "gpt-6.1-terra", Claude: "claude-sonnet-5-5", Models: reasoningModels},
	{Name: "decision", Env: "OPENAI_DECISION_MODEL", Default: "gpt-6-luna", Claude: "claude-haiku-5-5", Models: decisionModels},
	{Name: "day", Env: "OPENAI_DAY_MODEL", Default: "gpt-6-luna", Claude: "claude-haiku-5-5", Models: reasoningModels},
	{Name: "light", Env: "OPENAI_LIGHT_MODEL", Default: "gpt-6-luna", Claude: "claude-haiku-5-5", Models: reasoningModels},
	{Name: "work", Env: "OPENAI_WORK_MODEL", Default: "", Claude: "claude-opus-5-5", Models: reasoningModels},
}

// JobNamed is the job by name.
func JobNamed(name string) (Job, bool) {
	i := slices.IndexFunc(Jobs, func(j Job) bool { return j.Name == name })
	if i < 0 {
		return Job{}, false
	}
	return Jobs[i], true
}

// Allows is whether model may do the job: one of its OpenAI models, or a
// Claude Code model with ClaudePrefix.
func (j Job) Allows(model string) bool {
	if id, ok := strings.CutPrefix(model, ClaudePrefix); ok {
		return slices.ContainsFunc(ClaudeModels, func(m struct{ ID, Name string }) bool { return m.ID == id })
	}
	return slices.Contains(j.Models, model)
}

// A Choice is the model a job has now, and where it's from: "environment",
// "app" (the owner chose it), or "default".
type Choice struct {
	Model string
	From  string
}

// Source is where the agents think by default: SourceOpenAI until the owner
// picks Claude Code.
func Source(ctx context.Context, db DB) (string, error) {
	chosen, err := choices(ctx, db)
	if err != nil {
		return "", err
	}
	if chosen[sourceRow] == SourceClaude {
		return SourceClaude, nil
	}
	return SourceOpenAI, nil
}

// UseSource makes source where every job thinks, each with its default: the
// models chosen before are forgotten.
func UseSource(ctx context.Context, db DB, source string, at time.Time) error {
	if _, err := db.Exec(ctx, `DELETE FROM model_choices`); err != nil {
		return fmt.Errorf("use source: %w", err)
	}
	return ChooseModel(ctx, db, sourceRow, source, at)
}

func choices(ctx context.Context, db DB) (map[string]string, error) {
	rows, err := db.Query(ctx, `SELECT job, model FROM model_choices`)
	if err != nil {
		return nil, fmt.Errorf("model choices: %w", err)
	}
	chosen := map[string]string{}
	defer rows.Close()
	for rows.Next() {
		var job, model string
		if err := rows.Scan(&job, &model); err != nil {
			return nil, err
		}
		chosen[job] = model
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return chosen, nil
}

// ChosenModels is the model each job has now, by job's name. With Claude Code
// as the source, the environment's OpenAI models don't apply.
func ChosenModels(ctx context.Context, db DB) (map[string]Choice, error) {
	chosen, err := choices(ctx, db)
	if err != nil {
		return nil, err
	}
	claude := chosen[sourceRow] == SourceClaude
	out := map[string]Choice{}
	for _, j := range Jobs {
		switch {
		case !claude && os.Getenv(j.Env) != "":
			out[j.Name] = Choice{os.Getenv(j.Env), "environment"}
		case chosen[j.Name] != "" && j.Allows(chosen[j.Name]):
			out[j.Name] = Choice{chosen[j.Name], "app"}
		case claude:
			out[j.Name] = Choice{ClaudePrefix + j.Claude, "default"}
		default:
			out[j.Name] = Choice{j.Default, "default"}
		}
	}
	return out, nil
}

// ChooseModel keeps model for job; "" goes back to Tinio's default.
func ChooseModel(ctx context.Context, db DB, job, model string, at time.Time) error {
	var err error
	if model == "" {
		_, err = db.Exec(ctx, `DELETE FROM model_choices WHERE job = $1`, job)
	} else {
		_, err = db.Exec(ctx, `INSERT INTO model_choices (job, model, chosen_at) VALUES ($1, $2, $3)
			ON CONFLICT (job) DO UPDATE SET model = excluded.model, chosen_at = excluded.chosen_at`, job, model, at)
	}
	if err != nil {
		return fmt.Errorf("choose model: %w", err)
	}
	return nil
}
