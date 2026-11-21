package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Grandbusta/tinio/internal/db"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/Grandbusta/tinio"
	"github.com/Grandbusta/tinio/internal/character"
	"github.com/Grandbusta/tinio/internal/host"
)

type dayOptions struct {
	only string
	pace int
}

type dayCost struct {
	Action   string  `json:"action"`
	Provider string  `json:"provider"`
	Calls    int     `json:"calls"`
	CostUSD  float64 `json:"cost_usd"`
}

type dayResult struct {
	Character string         `json:"character"`
	Costs     []dayCost      `json:"costs"`
	CostUSD   float64        `json:"cost_usd"`
	Made      map[string]int `json:"made"` // works, by kind
	Took      time.Duration  `json:"took_ns"`
	Err       string         `json:"error,omitempty"`
}

// evaluateDays is `api eval -day`: a new agent raised from each ready
// character lives one whole day of their own, today, on a clock ticking every
// minute, in a throwaway database of its own, after their owner
// says good morning. It reports what the day cost, by what it was spent on.
func evaluateDays(ctx context.Context, models host.Models, o dayOptions, log *slog.Logger) error {
	d, done, err := evalDB(ctx)
	if err != nil {
		return err
	}
	defer done()
	templates, err := character.Templates(tinio.Files, "characters")
	if err != nil {
		return err
	}
	h, err := host.New(d, tinio.Files, models, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		return err
	}
	h.Days, h.RestAfter = true, 0
	var chosen []character.Template
	for _, t := range templates {
		if o.only == "" || slices.Contains(strings.Split(o.only, ","), t.Key) {
			chosen = append(chosen, t)
		}
	}
	log.Info("living a day", "characters", len(chosen), "pace", o.pace)
	results := make([]dayResult, len(chosen))
	var wg sync.WaitGroup
	for i, t := range chosen {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = liveADay(ctx, d, h, t, o.pace)
			log.Info("lived", "character", t.Key, "cost", fmt.Sprintf("$%.4f", results[i].CostUSD), "took", results[i].Took.Round(time.Second))
		}()
	}
	wg.Wait()
	writeDays(os.Stdout, o, results)
	return saveEval("day", fmt.Sprintf("pace%d", o.pace), results)
}

func liveADay(ctx context.Context, d *db.DB, h *host.Host, tpl character.Template, pace int) dayResult {
	r := dayResult{Character: tpl.Key}
	fail := func(err error) dayResult { r.Err = err.Error(); return r }
	start := time.Now()
	agent, account, err := raiseForEval(ctx, d, tpl, tpl.Relations[0])
	if err != nil {
		return fail(err)
	}
	t, err := h.Talk(ctx, agent, account, evalOwner, pace)
	if err != nil {
		return fail(err)
	}
	// Today, so their budget (counted from midnight on the decisions' own
	// clock) holds as it would.
	now := time.Now().In(evalStart.Location())
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	morning := midnight.Add(6 * time.Hour)
	said, err := t.Say(ctx, "Morning! What's on for you today?", morning)
	if err != nil {
		t.End()
		return fail(err)
	}
	for len(t.Said(said)) == 0 { // the reply's cost isn't the day's
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
	from := time.Now()
	for at := morning; at.Before(midnight.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		if err := t.Tick(ctx, at); err != nil {
			t.End()
			return fail(err)
		}
	}
	// The last tick, 23:59, has nothing of the day left to do; End stops it.
	if err := t.End(); err != nil && ctx.Err() == nil {
		return fail(err)
	}
	r.Took = time.Since(start)
	err = d.WithTx(ctx, func(tx *db.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action, provider, count(*), coalesce(sum(cost_usd), 0) FROM decisions
			WHERE agent_id = $1 AND at >= $2 GROUP BY action, provider ORDER BY 4 DESC`, agent, from)
		if err != nil {
			return err
		}
		for rows.Next() {
			var c dayCost
			if err := rows.Scan(&c.Action, &c.Provider, &c.Calls, &c.CostUSD); err != nil {
				return err
			}
			r.Costs, r.CostUSD = append(r.Costs, c), r.CostUSD+c.CostUSD
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT kind, count(*) FROM works WHERE agent_id = $1 GROUP BY kind`, agent)
		if err != nil {
			return err
		}
		r.Made = map[string]int{}
		for rows.Next() {
			var kind string
			var n int
			if err := rows.Scan(&kind, &n); err != nil {
				return err
			}
			r.Made[kind] = n
		}
		return rows.Err()
	})
	if err != nil {
		return fail(err)
	}
	return r
}

func writeDays(w io.Writer, o dayOptions, rs []dayResult) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "\nCHARACTER\tSPENT ON\tBY\tCALLS\tCOST")
	var costs []float64
	for _, r := range rs {
		if r.Err != "" {
			fmt.Fprintf(tw, "%s\t%s\t\t\t\n", r.Character, r.Err)
			continue
		}
		costs = append(costs, r.CostUSD)
		for _, c := range r.Costs {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t$%.4f\n", r.Character, c.Action, c.Provider, c.Calls, c.CostUSD)
		}
		var made []string
		for kind, n := range r.Made {
			made = append(made, fmt.Sprintf("%d %s", n, kind))
		}
		slices.Sort(made)
		fmt.Fprintf(tw, "%s\tthe day, making %s\t\t\t$%.4f\n", r.Character, orDefault(strings.Join(made, ", "), "nothing"), r.CostUSD)
	}
	tw.Flush()
	if len(costs) == 0 {
		return
	}
	slices.Sort(costs)
	sum := 0.0
	for _, c := range costs {
		sum += c
	}
	avg := sum / float64(len(costs))
	fmt.Fprintf(w, "\nA day at pace %d: $%.4f on average ($%.4f to $%.4f), about $%.2f a month.\n",
		o.pace, avg, costs[0], costs[len(costs)-1], avg*30)
}

// evalDB is a throwaway database of its own, gone when the eval ends (done).
func evalDB(ctx context.Context) (d *db.DB, done func(), err error) {
	dir, err := os.MkdirTemp("", "tinio-eval-")
	if err != nil {
		return nil, nil, err
	}
	d, err = db.Open(ctx, filepath.Join(dir, "tinio.db"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, nil, err
	}
	return d, func() { d.Close(); os.RemoveAll(dir) }, nil
}

// saveEval keeps a run's results in evals/results, as <time>-<kind>-<label>.json.
func saveEval(kind, label string, results any) error {
	dir := filepath.Join(evalDir, "results")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s-%s.json", time.Now().UTC().Format("20060102-150405"), kind, label))
	fmt.Printf("Results in %s.\n", path)
	return os.WriteFile(path, b, 0o644)
}
