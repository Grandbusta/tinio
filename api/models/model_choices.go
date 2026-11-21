package models

import (
	"context"
	"fmt"
	"os"
	"slices"
	"time"
)

// A Job is one of the agents' jobs a model does, and the models it may have:
// those Tinio has prices for (internal/pricing), so a cap can hold.
type Job struct {
	Name    string   // as kept in model_choices
	Env     string   // set in the environment, it's used instead, and can't be changed in the app
	Default string   // "" for work: the replies' model
	Models  []string // dearest first
}

// The models Tinio can use, dearest first.
var (
	reasoningModels = []string{"gpt-6-astra", "gpt-6.2-sol", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6-luna"}
	decisionModels  = []string{"gpt-6-luna"} // the only one the Decisions API has
)

// Jobs are the agents' jobs, in the order they're shown.
var Jobs = []Job{
	{Name: "reply", Env: "OPENAI_MODEL", Default: "gpt-6.1-terra", Models: reasoningModels},
	{Name: "decision", Env: "OPENAI_DECISION_MODEL", Default: "gpt-6-luna", Models: decisionModels},
	{Name: "day", Env: "OPENAI_DAY_MODEL", Default: "gpt-6-luna", Models: reasoningModels},
	{Name: "light", Env: "OPENAI_LIGHT_MODEL", Default: "gpt-6-luna", Models: reasoningModels},
	{Name: "work", Env: "OPENAI_WORK_MODEL", Default: "", Models: reasoningModels},
}

// JobNamed is the job by name.
func JobNamed(name string) (Job, bool) {
	i := slices.IndexFunc(Jobs, func(j Job) bool { return j.Name == name })
	if i < 0 {
		return Job{}, false
	}
	return Jobs[i], true
}

// A Choice is the model a job has now, and where it's from: "environment",
// "app" (the owner chose it), or "default".
type Choice struct {
	Model string
	From  string
}

// ChosenModels is the model each job has now, by job's name.
func ChosenModels(ctx context.Context, db DB) (map[string]Choice, error) {
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
	out := map[string]Choice{}
	for _, j := range Jobs {
		switch {
		case os.Getenv(j.Env) != "":
			out[j.Name] = Choice{os.Getenv(j.Env), "environment"}
		case chosen[j.Name] != "" && slices.Contains(j.Models, chosen[j.Name]):
			out[j.Name] = Choice{chosen[j.Name], "app"}
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
