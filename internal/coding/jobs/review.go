package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Grandbusta/tinio/internal/clis"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

// The agent checks each round before the owner sees it: Tinio runs the
// repository's tests in the copy, and the agent reads the changes against the
// brief and the repository's rules. Not right: the agent sends it back to the
// CLI, saying what to fix, up to MaxReviews times for each thing the owner
// asked. Right, or out of tries: it's ready for the owner, with the agent's
// verdict on its card.

// MaxReviews is how many times the agent sends one request back before it
// hands the work to the owner as it is.
const MaxReviews = 3

// What the agent's steps are called, as the job's page tells them.
const (
	sentBack     = "Checked it and sent it back"
	checkedReady = "Checked it: ready for you"
)

// SetReviewer is the model agents check work with: their own, for works. Nil:
// jobs go straight to the owner.
func (s *Service) SetReviewer(r system2.Reasoner) {
	s.mu.Lock()
	s.reviewer = r
	s.mu.Unlock()
}

func (s *Service) reviewerNow() system2.Reasoner {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reviewer
}

// A check is one of the repository's own test commands, run in the copy.
type check struct {
	Command string
	Passed  bool
	Output  string // its last lines
}

// testCommands are the repository's own tests, as Tinio runs them itself.
func testCommands(dir string) [][]string {
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	var out [][]string
	if b, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		json.Unmarshal(b, &pkg)
		if _, ok := pkg.Scripts["test"]; ok && has("node_modules") {
			run := "npm"
			switch {
			case has("pnpm-lock.yaml"):
				run = "pnpm"
			case has("yarn.lock"):
				run = "yarn"
			case has("bun.lockb"), has("bun.lock"):
				run = "bun"
			}
			out = append(out, []string{run, "test"})
		}
	}
	if has("go.mod") {
		out = append(out, []string{"go", "test", "./..."})
	}
	if has("Cargo.toml") {
		out = append(out, []string{"cargo", "test"})
	}
	if len(out) == 0 && (has("pyproject.toml") || has("pytest.ini")) {
		out = append(out, []string{"python", "-m", "pytest", "-q"})
	}
	return out
}

// checks runs the repository's tests in dir, each for 5 minutes at most.
func checks(ctx context.Context, dir string) []check {
	var out []check
	for _, cmd := range testCommands(dir) {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		c := exec.CommandContext(cctx, cmd[0], cmd[1:]...)
		c.Dir = dir
		c.Env = clis.Env(os.Environ(), "")
		b, err := c.CombinedOutput()
		cancel()
		ch := check{Command: strings.Join(cmd, " "), Passed: err == nil, Output: lastLines(string(b), 40)}
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			ch.Output += "\n(stopped after 5 minutes)"
		}
		out = append(out, ch)
	}
	return out
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// rulesOf is what the repository tells whoever works on it, from its own files.
func rulesOf(dir string) string {
	var out []string
	for _, f := range []string{"AGENTS.md", "CLAUDE.md", "CONTRIBUTING.md"} {
		if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil {
			t := string(b)
			if r := []rune(t); len(r) > 6000 {
				t = string(r[:6000]) + "\n…"
			}
			out = append(out, "## "+f+"\n"+t)
		}
	}
	return strings.Join(out, "\n\n")
}

// A Verdict is the agent's word on a round.
type Verdict struct {
	Ready   bool   `json:"ready"`
	ToOwner string `json:"to_owner"` // what the owner reads: does it do what was asked, and anything to look at
	ToCLI   string `json:"to_cli"`   // what to fix, when it isn't ready
}

var verdictSchema = map[string]any{
	"type": "object", "additionalProperties": false, "required": []string{"ready", "to_owner", "to_cli"},
	"properties": map[string]any{
		"ready":    map[string]any{"type": "boolean"},
		"to_owner": map[string]any{"type": "string"},
		"to_cli":   map[string]any{"type": "string"},
	},
}

// timesSentBack is how many times the agent has sent the job back since the
// owner last asked for something.
func (s *Service) timesSentBack(ctx context.Context, job string) int {
	steps, err := s.Steps(ctx, job)
	if err != nil {
		return MaxReviews
	}
	n := 0
	for _, st := range steps {
		switch {
		case st.Who == WhoOwner && (st.What == "Started the job" || st.What == "Sent more" || st.What == "Sent it back"):
			n = 0
		case st.Who == WhoAgent && st.What == sentBack:
			n++
		}
	}
	return n
}

func (s *Service) agentName(ctx context.Context, agent string) string {
	var name string
	if err := s.DB.QueryRow(ctx, `SELECT name FROM agents WHERE id = $1`, agent).Scan(&name); err != nil || name == "" {
		return "The agent"
	}
	return name
}

// verdict asks the agent's model about the round the CLI just finished.
func (s *Service) verdict(ctx context.Context, rv system2.Reasoner, j Job, name, owner, said string, cs []check, tries int) (Verdict, float64, error) {
	changes, err := s.Changes(ctx, j, 0)
	if err != nil {
		return Verdict{}, 0, err
	}
	var diff strings.Builder
	for _, c := range changes {
		fmt.Fprintf(&diff, "### %s (+%d −%d)\n%s\n", c.Path, c.Added, c.Removed, c.Patch)
	}
	d := diff.String()
	if r := []rune(d); len(r) > 60000 {
		d = string(r[:60000]) + "\n… (the rest is cut)"
	}
	if d == "" {
		d = "(no changes)"
	}
	var tests strings.Builder
	if len(cs) == 0 {
		tests.WriteString("Tinio found no tests it could run in this repository.")
	}
	for _, c := range cs {
		state := "passed"
		if !c.Passed {
			state = "FAILED"
		}
		fmt.Fprintf(&tests, "$ %s: %s\n%s\n\n", c.Command, state, c.Output)
	}
	// What the owner asked for after the brief changes it.
	var asked []string
	if steps, err := s.Steps(ctx, j.ID); err == nil {
		for _, st := range steps {
			if st.Who == WhoOwner && st.What == "Sent more" && st.Detail != "" {
				asked = append(asked, "- "+strings.ReplaceAll(st.Detail, "\n", "\n  "))
			}
		}
	}
	later := ""
	if len(asked) > 0 {
		later = fmt.Sprintf("\n\n# What %s asked for since, in order\nThese change the brief: where they differ from it, %s's words win.\n%s", owner, owner, strings.Join(asked, "\n"))
	}
	if notes, err := s.Notes(ctx, j.Agent, j.Folder); err == nil && len(notes) > 0 {
		later += "\n\n" + notesNote("# What you keep about this repository from earlier jobs", notes)
	}
	rules := rulesOf(j.Dir)
	if rules == "" {
		rules = "(the repository has no AGENTS.md, CLAUDE.md or CONTRIBUTING.md)"
	}
	instructions := fmt.Sprintf(`You're %[1]s, a software engineer. You wrote the brief below for a coding job for %[2]s, and Claude Code did the work. Check its work before %[2]s sees it, as a careful reviewer would.

Ready means: it does what the brief asks, as changed by anything %[2]s asked for since, nothing it wasn't asked to, the tests pass (or a failure is clearly not from this change), and it follows the repository's rules. Small matters of taste are not a reason to send it back.

If it isn't ready, say in to_cli exactly what to fix, plainly and briefly, as you'd say it to the engineer: Claude Code reads it and does another round. This is try %[3]d of %[4]d; on the last, only send it back for something that matters.

In to_owner, write to %[2]s in at most three short sentences, under 60 words: whether it does what was asked, and only what they need to look at or decide. Don't list what the tests or the diff show unless it matters. No em dashes. British spelling.`, name, owner, tries+1, MaxReviews)
	msg := fmt.Sprintf("# The brief\n%s%s\n\n# What Claude Code said when it finished\n%s\n\n# The tests, run by Tinio in the copy\n%s\n# The repository's rules\n%s\n\n# The changes, against where the job started\n%s",
		j.Brief, later, said, tests.String(), rules, d)
	res, err := rv.Reason(ctx, system2.Request{Instructions: instructions, Message: msg, Format: &system2.Format{Name: "verdict", Schema: verdictSchema}})
	if err != nil {
		return Verdict{}, res.CostUSD, err
	}
	var v Verdict
	if err := json.Unmarshal([]byte(res.Text), &v); err != nil {
		return Verdict{}, res.CostUSD, fmt.Errorf("jobs: the verdict wasn't JSON: %w", err)
	}
	v.ToOwner, v.ToCLI = strings.TrimSpace(v.ToOwner), strings.TrimSpace(v.ToCLI)
	if !v.Ready && v.ToCLI == "" {
		v.Ready = true // nothing to send back
	}
	return v, res.CostUSD, nil
}

// review is the agent checking the round the CLI just finished; it's ready
// for the owner, or it goes back to the CLI.
func (r *run) review(ctx context.Context, rv system2.Reasoner, said string) {
	j := r.snapshot()
	name := r.s.agentName(ctx, j.Agent)
	// Ready for the owner at once: the check goes on beside it, and they needn't wait.
	r.mu.Lock()
	r.job.Status, r.card.Status, r.card.Now = Ready, Ready, name+" is checking the work"
	r.reviewing = true
	ready := r.job
	r.mu.Unlock()
	r.s.save(context.Background(), ready)
	defer func() {
		r.mu.Lock()
		r.reviewing = false
		r.mu.Unlock()
	}()
	r.now("", true)

	cs := checks(ctx, j.Dir)
	for _, c := range cs {
		what := "Ran " + c.Command + ": passed"
		if !c.Passed {
			what = "Ran " + c.Command + ": failed"
		}
		r.s.step(ctx, j.ID, WhoAgent, what, c.Output)
	}
	if ctx.Err() != nil {
		if !r.isQuiet() {
			r.finish(r.stoppedStatus(), "It was stopped while "+name+" checked it.")
		}
		return
	}
	r.now(name+" is reading the changes", true)
	tries := r.s.timesSentBack(ctx, j.ID)
	v, cost, err := r.s.verdict(ctx, rv, j, name, r.owner, said, cs, tries)
	r.mu.Lock()
	r.job.CostUSD += cost
	r.mu.Unlock()
	switch {
	case ctx.Err() != nil:
		if !r.isQuiet() {
			r.finish(r.stoppedStatus(), "It was stopped while "+name+" checked it.")
		}
		return
	case err != nil:
		r.s.Log.Warn("jobs: the agent couldn't check the work", "job", j.ID, "err", err)
		r.s.step(ctx, j.ID, WhoAgent, "Couldn't check it", "")
		r.finishReady(said, name+" couldn't check it this time, so look it over yourself.")
		return
	case v.Ready:
		r.s.step(ctx, j.ID, WhoAgent, checkedReady, v.ToOwner)
		r.finishReady(said, v.ToOwner)
		return
	case tries+1 >= MaxReviews:
		r.s.step(ctx, j.ID, WhoAgent, "Checked it: still not right", v.ToCLI)
		r.finishReady(said, fmt.Sprintf("I sent it back %d times and it still isn't right: %s", MaxReviews, v.ToOwner))
		return
	}
	r.s.step(ctx, j.ID, WhoAgent, sentBack, v.ToCLI)
	r.mu.Lock()
	r.job.Status, r.card.Status = Working, Working
	r.card.Summary, r.card.Verdict = "", ""
	j = r.job
	r.mu.Unlock()
	r.s.save(context.Background(), j)
	r.now(name+" sent it back: "+firstLine(v.ToCLI), true)
	r.again = v.ToCLI
}

// isQuiet is whether the owner acted while the work was checked, so the check
// ends without a word.
func (r *run) isQuiet() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.quiet
}

// isChecking is whether the agent is checking the round's work.
func (r *run) isChecking() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reviewing
}

// endCheck ends the agent's check without a word, and waits for the run to end:
// the owner is acting on the work (committing it).
func (r *run) endCheck() {
	r.mu.Lock()
	r.quiet = true
	stop := r.stop
	r.mu.Unlock()
	if stop != nil {
		stop()
	}
	r.wait()
}

func (r *run) stoppedStatus() string {
	if r.cancelled() {
		return Cancelled
	}
	return Stopped
}

// finishReady ends the round ready for the owner, with the agent's verdict;
// unless they said something while it was checked: then it carries on.
func (r *run) finishReady(said, verdict string) {
	r.mu.Lock()
	heard := len(r.heard) > 0
	if !heard {
		r.card.Verdict = verdict
	}
	r.mu.Unlock()
	if heard {
		r.now("Taking in what you said", true)
		return
	}
	r.finish(Ready, said)
}

// hear keeps what the owner said while the round is checked; false: it isn't.
func (r *run) hear(text string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.reviewing {
		return false
	}
	r.heard = append(r.heard, text)
	return true
}
