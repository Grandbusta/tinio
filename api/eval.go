package api

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Grandbusta/tinio"
	"github.com/Grandbusta/tinio/internal/config"
	"github.com/Grandbusta/tinio/internal/decision/questions"
	"github.com/Grandbusta/tinio/internal/host"
	"github.com/Grandbusta/tinio/internal/replay"
)

// evalDir holds the test cases, a file a question set (evals/<set>.jsonl), and
// results/, each run's results (kept out of git).
const evalDir = "evals"

// evaluate is `tinio eval`: it asks System 1 the test cases with the question
// sets as they are now, and reports how often it's right, how much it decides
// alone, what it costs and how long it takes. It needs OPENAI_API_KEY, and
// never touches the owner's database.
func evaluate(ctx context.Context, args []string, log *slog.Logger) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	only := fs.String("set", "", "question sets, comma-separated (default: every set with cases)")
	maxCost := fs.Float64("max-cost", 1, "stop past this many dollars, across every set or conversation")
	parallel := fs.Int("parallel", 8, "cases asked at once")
	limit := fs.Int("n", 0, "at most this many cases a set, or conversations a character (0: all)")
	cases := fs.Bool("cases", false, "list every case, not only what each set adds up to")
	model := fs.String("model", "", "the model under test: it answers every set, through the reasoning API, not the model each set names; with -replies, it writes the replies (default OPENAI_MODEL)")
	replies := fs.Bool("replies", false, "have the conversations in evals/replies with new agents, and check and judge their replies")
	judge := fs.String("judge", "gpt-6-astra", "with -replies, the model that judges them")
	characters := fs.String("character", "", "with -replies or -day, only these characters, comma-separated")
	day := fs.Bool("day", false, "have a new agent from each ready character live a whole day of their own, and report what it cost")
	pace := fs.Int("pace", 1, "with -day, their pace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	models, err := host.OpenAI(log, "")
	if err != nil {
		return err
	}
	if err := host.Priced(*model, *judge); err != nil {
		return err
	}
	if *day {
		return evaluateDays(ctx, models, dayOptions{only: *characters, pace: max(*pace, 1)}, log)
	}
	if *replies {
		return evaluateReplies(ctx, models, replyOptions{model: *model, judge: *judge, only: *characters, limit: *limit,
			parallel: *parallel, maxCost: *maxCost, cases: *cases}, log)
	}
	sets, err := questions.LoadDir(tinio.Files, config.Load().Decision.QuestionSets)
	if err != nil {
		return err
	}
	names, err := evalSets(*only)
	if err != nil {
		return err
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	spent := 0.0
	for _, name := range names {
		set, ok := sets[name]
		if !ok {
			return fmt.Errorf("eval: no question set %q in %s", name, config.Load().Decision.QuestionSets)
		}
		f, err := os.Open(filepath.Join(evalDir, name+".jsonl"))
		if err != nil {
			return err
		}
		cs, err := replay.Load(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("eval: %s: %w", name, err)
		}
		if *limit > 0 && len(cs) > *limit {
			cs = cs[:*limit]
		}
		if *model != "" {
			set = set.On(*model)
		}
		left := *maxCost - spent
		if left <= 0 {
			fmt.Printf("\n%s: not asked; $%.2f spent, the cap.\n", name, spent)
			continue
		}
		r, err := replay.Run(ctx, models.Routed(set), set, "", cs, replay.Options{Parallel: *parallel, MaxCostUSD: left})
		if err != nil {
			return err
		}
		spent += r.CostUSD
		fmt.Println()
		r.Write(os.Stdout, *cases)
		if err := saveResult(stamp, r); err != nil {
			return err
		}
	}
	fmt.Printf("\n$%.4f in all. Results in %s.\n", spent, filepath.Join(evalDir, "results"))
	return nil
}

// evalSets is the sets named, or every set with a file of cases.
func evalSets(only string) ([]string, error) {
	if only != "" {
		return strings.Split(only, ","), nil
	}
	files, err := filepath.Glob(filepath.Join(evalDir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, f := range files {
		names = append(names, strings.TrimSuffix(filepath.Base(f), ".jsonl"))
	}
	slices.Sort(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("eval: no cases in %s (run it from the repository)", evalDir)
	}
	return names, nil
}

// saveResult keeps a run's report, to compare runs and models over time.
func saveResult(stamp string, r replay.Report) error {
	type row struct {
		Case   string  `json:"case"`
		Label  string  `json:"label"`
		Now    string  `json:"now"`
		Conf   float64 `json:"confidence"`
		Ms     int64   `json:"ms"`
		Failed string  `json:"failed,omitempty"`
	}
	out := struct {
		Set      string         `json:"set"`
		Question string         `json:"question"`
		Model    string         `json:"model"`
		Act      float64        `json:"act"`
		Suggest  float64        `json:"suggest"`
		CostUSD  float64        `json:"cost_usd"`
		Summary  replay.Summary `json:"summary"`
		Rows     []row          `json:"rows"`
	}{Set: r.Set, Question: r.Question, Model: r.Provider, Act: r.Act, Suggest: r.Suggest, CostUSD: r.CostUSD, Summary: r.Summary()}
	for _, x := range r.Rows {
		state, _ := json.Marshal(x.State)
		rw := row{Case: string(state), Label: x.Label, Now: x.Now, Conf: x.NowConf, Ms: x.Latency.Milliseconds()}
		if x.Failed != nil {
			rw.Failed = x.Failed.Error()
		}
		out.Rows = append(out.Rows, rw)
	}
	dir := filepath.Join(evalDir, "results")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s-%s.json", stamp, strings.ReplaceAll(r.Set, "@", "-v"), strings.ReplaceAll(r.Provider, "/", "_"))
	return os.WriteFile(filepath.Join(dir, name), b, 0o644)
}
