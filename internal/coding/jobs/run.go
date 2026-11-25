package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/coding"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// A run is a job with its CLI running: from its start (or the owner's reply
// carrying it on) to its end.
type run struct {
	s     *Service
	owner string // the owner's name, for what the CLI is told

	mu      sync.Mutex
	job     Job
	card    Card
	gate    *coding.Gate
	session coding.Session
	asks    map[string]pending // the questions waiting on the owner, by the CLI's id
	shown   time.Time          // when the card was last shown
	stop    context.CancelFunc
	over    chan struct{} // closed when the CLI has ended
	showing sync.Mutex    // one card update at a time, each of the latest state
}

// show shows the job's card as it is now. Updates go one at a time, each
// reading the state as it writes, so a later one is never of an earlier state.
func (r *run) show() {
	r.showing.Lock()
	defer r.showing.Unlock()
	r.mu.Lock()
	j, c := r.job, r.card
	r.mu.Unlock()
	r.s.showCard(context.Background(), j, c)
}

// wait waits until the CLI has ended, for a few seconds at most.
func (r *run) wait() {
	select {
	case <-r.over:
	case <-time.After(5 * time.Second):
	}
}

// pending is a question on the owner's card.
type pending struct {
	event coding.Event
	row   string // its chat row
}

func (r *run) system(set Settings) string {
	where := "in the repository itself"
	if r.job.Branch != "" {
		where = "on a copy of the repository, on its own branch " + r.job.Branch
	}
	return fmt.Sprintf("You're doing a coding job for %s through Tinio, %s. Follow the repository's own rules. "+
		"Don't commit, push or open pull requests: Tinio does that, with %s's yes. "+
		"If something you need isn't allowed, say what you'd do instead. "+
		"When you finish, say in two or three sentences what you changed and how you checked it.", r.owner, where, r.owner)
}

func (r *run) drive(ad coding.Adapter, set Settings, cj coding.Job) {
	limit := time.Duration(set.Minutes) * time.Minute
	if limit <= 0 {
		limit = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(r.s.ctx, limit)
	defer cancel()
	r.mu.Lock()
	r.stop = cancel
	r.mu.Unlock()
	defer func() {
		r.s.mu.Lock()
		if r.s.runs[r.job.ID] == r {
			delete(r.s.runs, r.job.ID)
		}
		r.s.mu.Unlock()
		close(r.over)
	}()

	sess, err := ad.Start(ctx, cj)
	if err != nil {
		r.end(Failed, "Claude Code didn't start: "+err.Error(), coding.Event{})
		return
	}
	r.mu.Lock()
	r.session = sess
	r.mu.Unlock()
	ended := false
	for e := range sess.Events() {
		switch e.Kind {
		case coding.Started:
			r.mu.Lock()
			r.job.Session = e.SessionID
			j := r.job
			r.mu.Unlock()
			r.s.save(context.Background(), j)
		case coding.Said:
			r.now(firstLine(e.Text), false)
		case coding.Step:
			r.mu.Lock()
			r.card.Steps++
			r.mu.Unlock()
			r.now(doing(e.Tool, e.Input), false)
			if e.Tool != "TodoWrite" {
				r.s.step(context.Background(), r.job.ID, WhoCLI, doing(e.Tool, e.Input), detail(e.Tool, e.Input))
			}
		case coding.Changed:
			r.mu.Lock()
			rel := e.Path
			if p, err := filepath.Rel(r.job.Dir, e.Path); err == nil && !strings.HasPrefix(p, "..") {
				rel = p
			}
			if !contains(r.card.Files, rel) {
				r.card.Files = append(r.card.Files, rel)
			}
			r.mu.Unlock()
		case coding.Ask:
			r.ask(e)
		case coding.Done:
			ended = true
			status := map[string]string{coding.Completed: Ready, coding.Stopped: Stopped, coding.Failed: Failed}[e.Status]
			r.end(status, e.Text, e)
			go sess.Close()
		}
	}
	if !ended {
		why := "It stopped."
		if ctx.Err() == context.DeadlineExceeded {
			why = fmt.Sprintf("It ran out of time (%d minutes). Reply here to carry it on.", int(limit.Minutes()))
		} else if r.cancelled() {
			why = "You stopped it."
		}
		status := Stopped
		if r.cancelled() {
			status = Cancelled
		}
		r.end(status, why, coding.Event{})
	}
}

func (r *run) cancelled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.job.Status == Cancelled
}

func (r *run) cancel() {
	r.mu.Lock()
	r.job.Status = Cancelled
	stop := r.stop
	r.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// now says what the job is doing on its card: shown at most twice a second
// unless force.
func (r *run) now(what string, force bool) {
	r.mu.Lock()
	if what != "" {
		r.card.Now = what
	}
	due := force || time.Since(r.shown) > 500*time.Millisecond
	if due {
		r.shown = time.Now()
	}
	r.mu.Unlock()
	if due {
		r.show()
	}
}

func (r *run) end(status, text string, e coding.Event) {
	// A snapshot of the copy as the round left it, for its changes and to go back to.
	if r.job.Dir != "" {
		if n, err := r.s.checkpoint(context.Background(), r.snapshot()); err != nil {
			r.s.Log.Warn("jobs: no snapshot of the round", "job", r.job.ID, "err", err)
		} else {
			r.mu.Lock()
			r.job.Rounds = n
			r.mu.Unlock()
		}
	}
	what := map[string]string{Ready: "Finished", Stopped: "Stopped", Failed: "Didn't work out", Cancelled: "Stopped by you"}[status]
	r.s.step(context.Background(), r.job.ID, WhoCLI, what, strings.TrimSpace(text))
	r.mu.Lock()
	if r.job.Status == Cancelled {
		status = Cancelled
	}
	r.job.Status, r.card.Status = status, status
	r.job.EndedAt = time.Now().UTC()
	r.job.CostUSD += e.CostUSD
	r.job.Turns += e.Turns
	r.card.CostUSD, r.card.Asking, r.card.Rounds = r.job.CostUSD, 0, r.job.Rounds
	r.card.Summary = strings.TrimSpace(text)
	r.card.Now = map[string]string{Ready: "Done: for you to look at", Stopped: "Stopped: reply here to carry it on",
		Failed: "It didn't work out", Cancelled: "You stopped it"}[status]
	// What's still asked can't be answered now.
	asks := r.asks
	r.asks = map[string]pending{}
	j := r.job
	r.mu.Unlock()
	st := store.New(r.s.DB).For(j.Agent)
	for _, p := range asks {
		st.Settle(context.Background(), p.row, askText(p.event)+"\n\nThe job ended before you answered.", time.Now().UTC())
	}
	r.s.save(context.Background(), j)
	r.show()
}

func (r *run) ask(e coding.Event) {
	r.mu.Lock()
	ok, decided, why := r.gate.Decide(e)
	sess := r.session
	r.mu.Unlock()
	if decided {
		sess.Answer(e.AskID, ok, why)
		return
	}
	// The owner's: a card with three answers.
	b, _ := json.Marshal(map[string]any{"job": r.job.ID, "ask": e.AskID, "tool": e.Tool, "detail": detail(e.Tool, e.Input),
		"why": whyAsked(r.gate.Rules, e)})
	row := ulid.Make().String()
	r.mu.Lock()
	j := r.job
	r.mu.Unlock()
	if err := store.New(r.s.DB).For(j.Agent).SaveChat(context.Background(), memory.ChatMessage{ID: row, Author: memory.ChatAgent,
		Kind: memory.ChatJobAsk, Text: askText(e), Payload: b, At: time.Now().UTC(), Topic: j.Topic}); err != nil {
		r.s.Log.Warn("jobs: asking the owner failed; it's a no", "job", j.ID, "err", err)
		sess.Answer(e.AskID, false, "Tinio couldn't ask the owner.")
		return
	}
	r.mu.Lock()
	r.asks[e.AskID] = pending{event: e, row: row}
	r.job.Status, r.card.Status = Waiting, Waiting
	r.card.Asking = len(r.asks)
	r.mu.Unlock()
	r.s.step(context.Background(), j.ID, WhoCLI, "Asked to "+strings.ToLower(askVerb(e.Tool)), detail(e.Tool, e.Input))
	r.now("Waiting for you: "+strings.ToLower(askVerb(e.Tool)), true)
	r.s.save(context.Background(), r.snapshot())
}

func (r *run) snapshot() Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.job
}

func (r *run) answer(ctx context.Context, ask, answer, note string) error {
	r.mu.Lock()
	p, ok := r.asks[ask]
	if !ok {
		r.mu.Unlock()
		return ErrNoJob
	}
	delete(r.asks, ask)
	allow, why, said := true, "", ""
	switch answer {
	case Once:
		said = "✅ Allowed once."
	case ForJob:
		r.gate.AllowForJob(p.event)
		said = "✅ Allowed for this job."
	case No:
		allow = false
		why = r.owner + " said no."
		if n := strings.TrimSpace(note); n != "" {
			why += " " + n
		}
		said = "You said no."
		if n := strings.TrimSpace(note); n != "" {
			said += " " + n
		}
	default:
		r.asks[ask] = p
		r.mu.Unlock()
		return fmt.Errorf("jobs: %q isn't an answer", answer)
	}
	if len(r.asks) == 0 && r.job.Status == Waiting {
		r.job.Status, r.card.Status = Working, Working
	}
	r.card.Asking = len(r.asks)
	sess, j := r.session, r.job
	r.mu.Unlock()
	// Saved and shown before the CLI hears it: once it has, the job may end at
	// once, and what's saved after that would undo its end.
	r.s.save(ctx, j)
	r.s.step(ctx, j.ID, WhoOwner, strings.TrimPrefix(strings.TrimSuffix(said, "."), "✅ "), strings.TrimSpace(note))
	store.New(r.s.DB).For(j.Agent).Settle(ctx, p.row, askText(p.event)+"\n\n"+said, time.Now().UTC())
	r.now("", true)
	return sess.Answer(ask, allow, why)
}

func (r *run) send(text string) error {
	r.mu.Lock()
	sess := r.session
	r.mu.Unlock()
	if sess == nil {
		return ErrNoJob
	}
	if err := sess.Send(text); err != nil {
		return err
	}
	r.s.step(context.Background(), r.job.ID, WhoOwner, "Sent more", text)
	r.now("Taking in what you said", true)
	return nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 140 {
		s = string(r[:139]) + "…"
	}
	return s
}

// doing is a step as the card says it.
func doing(tool string, input map[string]any) string {
	str := func(k string) string { v, _ := input[k].(string); return v }
	switch tool {
	case "Read":
		return "Reading " + filepath.Base(str("file_path"))
	case "Edit", "MultiEdit", "Write":
		return "Changing " + filepath.Base(str("file_path"))
	case "Bash":
		return "Running " + firstLine(str("command"))
	case "Grep", "Glob":
		return "Searching the code"
	case "TodoWrite":
		return "Planning"
	}
	return "Working"
}

func askVerb(tool string) string {
	switch tool {
	case "Bash":
		return "Run a command"
	case "WebFetch":
		return "Read a web page"
	case "WebSearch":
		return "Search the web"
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		return "Change a file outside its copy"
	case "Read", "Glob", "Grep", "LS":
		return "Read outside its copy"
	}
	return "Use " + tool
}

func askText(e coding.Event) string {
	return "Claude Code wants to " + strings.ToLower(askVerb(e.Tool)) + ": " + detail(e.Tool, e.Input)
}

// detail is exactly what it would do: the command, the page, the file.
func detail(tool string, input map[string]any) string {
	for _, k := range []string{"command", "url", "query", "file_path", "path", "notebook_path", "pattern"} {
		if v, ok := input[k].(string); ok && v != "" {
			return v
		}
	}
	b, _ := json.Marshal(input)
	return string(b)
}

func whyAsked(rules coding.Rules, e coding.Event) string {
	_, why := rules.Allows(e.Tool, e.Input)
	return why
}
