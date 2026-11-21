package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"gopkg.in/yaml.v3"

	"github.com/Grandbusta/tinio"
	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/internal/character"
	"github.com/Grandbusta/tinio/internal/config"
	"github.com/Grandbusta/tinio/internal/host"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/providers/system2"
	"github.com/Grandbusta/tinio/internal/store"
)

// replyCase is one conversation with an agent raised from a ready character
// (its file's name), and what it should leave behind.
type replyCase struct {
	Name string `yaml:"name"`
	// Relation is who the owner is to them; "": the character's first.
	Relation string `yaml:"relation"`
	Turns    []struct {
		Say   string   `yaml:"say"`
		Says  []string `yaml:"says"`  // patterns the reply must match
		Never []string `yaml:"never"` // patterns it mustn't
	} `yaml:"turns"`
	// After the conversation, nil: not checked.
	Reminders *int `yaml:"reminders"` // open reminders for the owner
	// ReminderAt is when one of them is due, in the owner's time ("2006-01-02 15:04"), to 5 minutes.
	ReminderAt string   `yaml:"reminder_at"`
	Promises   *int     `yaml:"promises"`  // open promises to the owner
	Remembers  []string `yaml:"remembers"` // each in a fact they saved, any case
	Judge      string   `yaml:"judge"`     // what a good reply does here, for the judge
}

const (
	repliesDir = "evals/replies"
	judgeFile  = "evals/judge.md"
	evalOwner  = "Ada"
	evalZone   = "Europe/London"
)

// evalStart is when every conversation starts, in the owner's time: a Tuesday
// morning, so "tomorrow at 9" is always the same moment. Turns are a minute apart.
var evalStart = time.Date(2026, 11, 17, 10, 0, 0, 0, mustZone(evalZone))

func mustZone(name string) *time.Location {
	z, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return z
}

type replyOptions struct {
	model, judge, only string
	limit, parallel    int
	maxCost            float64
	cases              bool
}

type turnResult struct {
	Said    string        `json:"said"`
	Replied string        `json:"replied"`
	Took    time.Duration `json:"took_ns"`
	// FirstWords is how long until the reply began to show; 0 if it never did.
	FirstWords time.Duration `json:"first_words_ns,omitempty"`
}

type replyResult struct {
	Character string       `json:"character"`
	Case      string       `json:"case"`
	Turns     []turnResult `json:"turns"`
	Failed    []string     `json:"failed,omitempty"` // checks that failed
	Scores    *judgement   `json:"scores,omitempty"`
	CostUSD   float64      `json:"cost_usd"` // the agent's; the judge's is apart
	JudgeUSD  float64      `json:"judge_usd"`
	Err       string       `json:"error,omitempty"`
}

type judgement struct {
	Character int    `json:"character"`
	Natural   int    `json:"natural"`
	Useful    int    `json:"useful"`
	Notes     string `json:"notes"`
}

// evaluateReplies is `api eval -replies`: each conversation in evals/replies
// had with a new agent, on the real mind with their day held still, in a
// database of its own (EVAL_DATABASE_URL, never a real one). What they leave
// behind is checked, and a judge scores their voice.
func evaluateReplies(ctx context.Context, models host.Models, o replyOptions, log *slog.Logger) error {
	cfg := config.Load()
	if o.model == "" {
		o.model = cfg.Reasoning.Model
	}
	models.System2 = system2.WithoutEmDashes(models.Reasoner(o.model, cfg.Reasoning.Effort))
	judge := models.Reasoner(o.judge, "medium")
	rubric, err := os.ReadFile(judgeFile)
	if err != nil {
		return err
	}
	byCharacter, err := loadReplyCases(o.only, o.limit)
	if err != nil {
		return err
	}
	templates, err := character.Templates(tinio.Files, "characters")
	if err != nil {
		return err
	}
	db, err := evalDB(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil)) // the mind's logs would drown the report
	h, err := host.New(db, tinio.Files, models, quiet)
	if err != nil {
		return err
	}

	type job struct {
		tpl character.Template
		c   replyCase
	}
	var jobs []job
	for _, t := range templates {
		for _, c := range byCharacter[t.Key] {
			jobs = append(jobs, job{t, c})
		}
	}
	if len(jobs) == 0 {
		return fmt.Errorf("eval: no conversations in %s", repliesDir)
	}
	log.Info("having conversations", "cases", len(jobs), "model", o.model, "judge", o.judge)
	results := make([]replyResult, len(jobs))
	var (
		mu    sync.Mutex
		spent float64
		wg    sync.WaitGroup
		slots = make(chan struct{}, max(o.parallel, 1))
	)
	for i, j := range jobs {
		mu.Lock()
		over := spent >= o.maxCost
		mu.Unlock()
		if over {
			results[i] = replyResult{Character: j.tpl.Key, Case: j.c.Name, Err: "not had: the cost cap"}
			continue
		}
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			r := converse(ctx, db, h, j.tpl, j.c)
			if r.Err == "" {
				var err error
				if r.Scores, r.JudgeUSD, err = judgeReply(ctx, judge, rubric, j.tpl, j.c, r); err != nil {
					r.Err = "judge: " + err.Error()
				}
			}
			mu.Lock()
			spent += r.CostUSD + r.JudgeUSD
			mu.Unlock()
			results[i] = r
			log.Info("had", "character", j.tpl.Key, "case", j.c.Name, "failed", len(r.Failed), "cost", fmt.Sprintf("$%.4f", r.CostUSD+r.JudgeUSD))
		}()
	}
	wg.Wait()
	writeReplies(os.Stdout, o, results)
	return saveReplies(o, results)
}

func loadReplyCases(only string, limit int) (map[string][]replyCase, error) {
	files, err := filepath.Glob(filepath.Join(repliesDir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	out := map[string][]replyCase{}
	for _, f := range files {
		key := strings.TrimSuffix(filepath.Base(f), ".yaml")
		if only != "" && !slices.Contains(strings.Split(only, ","), key) {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var cs []replyCase
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		if err := dec.Decode(&cs); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for _, c := range cs {
			if c.Name == "" || len(c.Turns) == 0 {
				return nil, fmt.Errorf("%s: every case needs a name and turns", f)
			}
			for _, t := range c.Turns {
				for _, p := range slices.Concat(t.Says, t.Never) {
					if _, err := regexp.Compile(p); err != nil {
						return nil, fmt.Errorf("%s: %s: %w", f, c.Name, err)
					}
				}
			}
		}
		if limit > 0 && len(cs) > limit {
			cs = cs[:limit]
		}
		out[key] = cs
	}
	return out, nil
}

// converse raises a new agent from tpl, has c with them, and checks what's left.
func converse(ctx context.Context, db *pgxpool.Pool, h *host.Host, tpl character.Template, c replyCase) replyResult {
	r := replyResult{Character: tpl.Key, Case: c.Name}
	fail := func(err error) replyResult { r.Err = err.Error(); return r }
	relation := c.Relation
	if relation == "" {
		relation = tpl.Relations[0]
	}
	agent, account, err := raiseForEval(ctx, db, tpl, relation)
	if err != nil {
		return fail(err)
	}
	t, err := h.Talk(ctx, agent, account, evalOwner, 1)
	if err != nil {
		return fail(err)
	}
	ids := make([]ulid.ULID, len(c.Turns))
	taken := make([]time.Time, len(c.Turns)+1)
	for i, turn := range c.Turns {
		if ids[i], err = t.Say(ctx, turn.Say, evalStart.Add(time.Duration(i)*time.Minute)); err != nil {
			t.End()
			return fail(err)
		}
		taken[i] = time.Now()
	}
	if err := t.End(); err != nil && ctx.Err() == nil {
		return fail(err)
	}
	taken[len(c.Turns)] = time.Now()
	for i, turn := range c.Turns {
		tr := turnResult{Said: turn.Say, Replied: strings.Join(t.Said(ids[i]), "\n"), Took: taken[i+1].Sub(taken[i])}
		if first := t.FirstWords(ids[i]); !first.IsZero() {
			tr.FirstWords = first.Sub(taken[i])
		}
		r.Turns = append(r.Turns, tr)
	}
	st := store.New(db).For(agent)
	r.Failed = check(ctx, st, c, r.Turns, "account:"+account)
	days, err := st.CostByDay(ctx, time.Time{}, time.UTC)
	if err != nil {
		return fail(err)
	}
	for _, d := range days {
		r.CostUSD += d.CostUSD
	}
	return r
}

// check is every way the conversation fell short of c.
func check(ctx context.Context, st *store.Store, c replyCase, turns []turnResult, owner string) []string {
	var failed []string
	if len(turns) != len(c.Turns) {
		return []string{fmt.Sprintf("%d turns in the chat, %d said", len(turns), len(c.Turns))}
	}
	for i, t := range turns {
		n := fmt.Sprintf("turn %d", i+1)
		switch {
		case t.Replied == "":
			failed = append(failed, n+": no reply")
			continue
		case strings.Contains(t.Replied, "couldn't think that through"):
			failed = append(failed, n+": System 2 failed")
			continue
		case strings.Contains(t.Replied, "—"):
			failed = append(failed, n+": an em dash")
		}
		for _, p := range c.Turns[i].Says {
			if !regexp.MustCompile(p).MatchString(t.Replied) {
				failed = append(failed, fmt.Sprintf("%s: doesn't say /%s/", n, p))
			}
		}
		for _, p := range c.Turns[i].Never {
			if regexp.MustCompile(p).MatchString(t.Replied) {
				failed = append(failed, fmt.Sprintf("%s: says /%s/", n, p))
			}
		}
	}
	if c.Reminders != nil || c.ReminderAt != "" {
		rs, err := st.OpenIntentions(ctx, owner, memory.IntentionRemind)
		switch {
		case err != nil:
			failed = append(failed, "reminders: "+err.Error())
		case c.Reminders != nil && len(rs) != *c.Reminders:
			failed = append(failed, fmt.Sprintf("%d reminders, want %d", len(rs), *c.Reminders))
		case c.ReminderAt != "":
			want, err := time.ParseInLocation("2006-01-02 15:04", c.ReminderAt, evalStart.Location())
			if err != nil {
				failed = append(failed, "reminder_at: "+err.Error())
				break
			}
			if !slices.ContainsFunc(rs, func(r memory.Intention) bool { return r.DueAt.Sub(want).Abs() <= 5*time.Minute }) {
				var due []string
				for _, r := range rs {
					due = append(due, r.DueAt.In(want.Location()).Format("2006-01-02 15:04"))
				}
				failed = append(failed, fmt.Sprintf("no reminder at %s (have %v)", c.ReminderAt, due))
			}
		}
	}
	if c.Promises != nil {
		ps, err := st.OpenIntentions(ctx, owner, memory.IntentionTask)
		if err != nil {
			failed = append(failed, "promises: "+err.Error())
		} else if len(ps) != *c.Promises {
			failed = append(failed, fmt.Sprintf("%d promises, want %d", len(ps), *c.Promises))
		}
	}
	if len(c.Remembers) > 0 {
		facts, err := st.RecentFacts(ctx, 500)
		if err != nil {
			return append(failed, "facts: "+err.Error())
		}
		for _, want := range c.Remembers {
			if !slices.ContainsFunc(facts, func(f memory.Fact) bool {
				return strings.Contains(strings.ToLower(f.Text), strings.ToLower(want))
			}) {
				failed = append(failed, fmt.Sprintf("remembers nothing of %q", want))
			}
		}
	}
	return failed
}

// raiseForEval is what the API does for a new account raising tpl, as Ada.
func raiseForEval(ctx context.Context, db *pgxpool.Pool, tpl character.Template, relation string) (agent, account string, err error) {
	now := time.Now().UTC()
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		acc := models.Account{Name: evalOwner, CreatedAt: now}
		acc.Email = "eval-" + strings.ToLower(ulid.Make().String()) + "@example.com"
		if err := acc.Save(ctx, tx); err != nil {
			return err
		}
		files, err := tpl.Render(character.Owner{Name: evalOwner, Relation: relation, Account: "account:" + acc.ID, Timezone: evalZone})
		if err != nil {
			return err
		}
		ag := models.Agent{AccountID: acc.ID, CharacterKey: tpl.Key, Name: tpl.Name, OwnerName: evalOwner, Relation: relation, Pace: 1, CreatedAt: now}
		if err := ag.Save(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.agent_id', $1, true)`, ag.ID); err != nil {
			return err
		}
		for _, f := range character.Files {
			v := models.AgentVersion{AgentID: ag.ID, Section: strings.TrimSuffix(f, ".yaml"), Doc: string(files[f]), Source: "eval", CreatedAt: now}
			if err := v.Save(ctx, tx); err != nil {
				return err
			}
		}
		agent, account = ag.ID, acc.ID
		return nil
	})
	return agent, account, err
}

// judgeReply has judge score the conversation against the rubric (evals/judge.md).
func judgeReply(ctx context.Context, judge system2.Reasoner, rubric []byte, tpl character.Template, c replyCase, r replyResult) (*judgement, float64, error) {
	files, err := tpl.Render(character.Owner{Name: evalOwner, Relation: orFirst(c.Relation, tpl.Relations), Account: "account:eval", Timezone: evalZone})
	if err != nil {
		return nil, 0, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Who the agent is (their character file):\n\n%s\n\nTheir owner is %s, their %s. The conversation, which starts on %s in %s's time:\n",
		files["character.yaml"], evalOwner, orFirst(c.Relation, tpl.Relations), evalStart.Format("Monday 2 January 2006 at 15:04"), evalOwner)
	for _, t := range r.Turns {
		fmt.Fprintf(&b, "\n%s: %s\n%s: %s\n", evalOwner, t.Said, tpl.Name, t.Replied)
	}
	fmt.Fprintf(&b, "\nWhat a good reply does here: %s\n", orDefault(c.Judge, "answers well, as themselves."))
	score := map[string]any{"type": "integer", "enum": []int{1, 2, 3, 4, 5}}
	resp, err := judge.Reason(ctx, system2.Request{
		Instructions: string(rubric),
		Message:      b.String(),
		Format: &system2.Format{Name: "judgement", Schema: map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"character", "natural", "useful", "notes"},
			"properties": map[string]any{"character": score, "natural": score, "useful": score, "notes": map[string]any{"type": "string"}},
		}},
	})
	if err != nil {
		return nil, 0, err
	}
	var j judgement
	if err := json.Unmarshal([]byte(resp.Text), &j); err != nil {
		return nil, resp.CostUSD, err
	}
	return &j, resp.CostUSD, nil
}

func orFirst(s string, list []string) string {
	if s == "" && len(list) > 0 {
		return list[0]
	}
	return s
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func writeReplies(w io.Writer, o replyOptions, rs []replyResult) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "\nCHARACTER\tCASE\tCHECKS\tVOICE\tNATURAL\tUSEFUL\tCOST")
	var (
		had, passed, scored    int
		voice, natural, useful float64
		agentCost, judgeCost   float64
		took, first            []time.Duration
	)
	for _, r := range rs {
		agentCost, judgeCost = agentCost+r.CostUSD, judgeCost+r.JudgeUSD
		if r.Err != "" {
			fmt.Fprintf(tw, "%s\t%s\t%s\t\t\t\t\n", r.Character, r.Case, r.Err)
			continue
		}
		had++
		checks := "✓"
		if len(r.Failed) > 0 {
			checks = "✗ " + strings.Join(r.Failed, "; ")
		} else {
			passed++
		}
		v, n, u := "", "", ""
		if s := r.Scores; s != nil {
			scored++
			voice, natural, useful = voice+float64(s.Character), natural+float64(s.Natural), useful+float64(s.Useful)
			v, n, u = fmt.Sprint(s.Character), fmt.Sprint(s.Natural), fmt.Sprint(s.Useful)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t$%.4f\n", r.Character, r.Case, checks, v, n, u, r.CostUSD)
		for _, t := range r.Turns {
			took = append(took, t.Took)
			if t.FirstWords > 0 {
				first = append(first, t.FirstWords)
			}
		}
	}
	tw.Flush()
	if o.cases {
		for _, r := range rs {
			fmt.Fprintf(w, "\n%s / %s\n", r.Character, r.Case)
			for _, t := range r.Turns {
				fmt.Fprintf(w, "  %s: %s\n  agent: %s\n", evalOwner, t.Said, strings.ReplaceAll(t.Replied, "\n", "\n         "))
			}
			if r.Scores != nil {
				fmt.Fprintf(w, "  judge: %s\n", r.Scores.Notes)
			}
		}
	}
	fmt.Fprintf(w, "\nReplies by %s, judged by %s: checks passed in %d of %d conversations.\n", o.model, o.judge, passed, had)
	if scored > 0 {
		k := float64(scored)
		fmt.Fprintf(w, "Judged out of 5: voice %.2f, natural %.2f, useful %.2f.\n", voice/k, natural/k, useful/k)
	}
	if len(took) > 0 {
		slices.Sort(took)
		fmt.Fprintf(w, "A message answered in %s typically, %s at the 95th percentile (all of it: reading, reply, what it saves).\n",
			took[len(took)/2].Round(100*time.Millisecond), took[min(len(took)-1, len(took)*95/100)].Round(100*time.Millisecond))
	}
	if len(first) > 0 {
		slices.Sort(first)
		fmt.Fprintf(w, "Its first words showed in %s typically, %s at the 95th percentile (%d of %d replies showed while written).\n",
			first[len(first)/2].Round(100*time.Millisecond), first[min(len(first)-1, len(first)*95/100)].Round(100*time.Millisecond),
			len(first), len(took))
	}
	fmt.Fprintf(w, "$%.4f in all: $%.4f the agents, $%.4f the judge.\n", agentCost+judgeCost, agentCost, judgeCost)
}

func saveReplies(o replyOptions, rs []replyResult) error {
	return saveEval("replies", o.model, struct {
		Model   string        `json:"model"`
		Judge   string        `json:"judge"`
		Results []replyResult `json:"results"`
	}{o.model, o.judge, rs})
}
