package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
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
	again   string        // what the agent sent back, for another round once this one is over
	// While the agent checks a round the CLI isn't listening: what the owner
	// says then goes into the next round.
	reviewing bool
	heard     []string
	// The owner said no to something it asked: the round stops there, unchecked,
	// and carries on once with instead, if they said what to do.
	refused bool
	instead string
	saying  string     // what it's writing now, so far; "" between its words
	quiet   bool       // the owner acted while it was checked (committed): the check ends without a word
	showing sync.Mutex // one card update at a time, each of the latest state
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
		"For anything with more than two steps, keep your plan in your TodoWrite list and tick items off as you go: %[1]s follows it there. "+
		"When you finish, say in two or three sentences what you changed and how you checked it.", r.owner, where, r.owner) +
		r.knownNote()
}

// knownNote is what the agent keeps about the job's folder, for the CLI.
func (r *run) knownNote() string {
	notes, err := r.s.Notes(context.Background(), r.job.Agent, r.job.Folder)
	if err != nil || len(notes) == 0 {
		return ""
	}
	return "\n\n" + notesNote("What earlier jobs in this repository taught (follow these unless this job says otherwise):", notes)
}

// drive runs a round: in kept, the job's CLI left open after the last round, or
// a new one.
func (r *run) drive(ad coding.Adapter, set Settings, cj coding.Job, kept *parked) {
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
		r.mu.Lock()
		again := r.again
		if len(r.heard) > 0 {
			if again != "" {
				again += "\n\n"
			}
			again += r.owner + " also said: " + strings.Join(r.heard, "\n")
		}
		r.mu.Unlock()
		if again != "" && !r.cancelled() {
			// The agent sent it back: another round, in the same session.
			if err := r.s.carryOn(r.s.ctx, r.snapshot(), again, "Doing another round"); err != nil {
				r.s.Log.Warn("jobs: sending the job back failed", "job", r.job.ID, "err", err)
				r.finish(Stopped, "It couldn't go back for another round: "+err.Error())
			}
		}
	}()

	// Where the round starts in its story, with what started it.
	r.s.step(context.Background(), r.job.ID, WhoTinio, RoundStarted, cj.Brief)
	// The CLI lives past the round (parked for the next), so it has its own context;
	// the round's ending early (its time up, or stopped) ends it.
	var sess coding.Session
	var stopCLI context.CancelFunc
	started := false
	if kept != nil {
		if err := kept.sess.Send(cj.Brief); err == nil {
			sess, stopCLI, started = kept.sess, kept.cancel, true
		} else {
			kept.close()
		}
	}
	if sess == nil {
		cliCtx, cancelCLI := context.WithCancel(r.s.ctx)
		s, err := ad.Start(cliCtx, cj)
		if err != nil {
			cancelCLI()
			r.end(Failed, "Claude Code didn't start: "+err.Error(), coding.Event{})
			return
		}
		sess, stopCLI = s, cancelCLI
	}
	roundDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done(): // out of time, or stopped
			stopCLI()
		case <-roundDone:
		}
	}()
	r.mu.Lock()
	r.session = sess
	r.mu.Unlock()
	ended := false
again:
	for e := range sess.Events() {
		switch e.Kind {
		case coding.Started:
			started = true
			r.mu.Lock()
			r.job.Session = e.SessionID
			j := r.job
			r.mu.Unlock()
			r.s.save(context.Background(), j)
		case coding.Saying:
			r.mu.Lock()
			r.saying = r.inCopy(e.Text)
			r.mu.Unlock()
			r.now(firstLine(e.Text), false)
		case coding.Said:
			r.mu.Lock()
			r.saying = ""
			r.mu.Unlock()
			r.now(firstLine(e.Text), false)
			r.s.step(context.Background(), r.job.ID, WhoCLI, Said, r.inCopy(e.Text))
		case coding.Step:
			r.mu.Lock()
			r.card.Steps++
			r.mu.Unlock()
			r.now(r.inCopy(doing(e.Tool, e.Input)), false)
			if e.Tool == "TodoWrite" {
				// Its plan, as it stands now: the page shows the latest of the round.
				if b, err := json.Marshal(e.Input["todos"]); err == nil {
					r.s.step(context.Background(), r.job.ID, WhoCLI, Planned, r.inCopy(string(b)))
				}
			} else {
				r.s.step(context.Background(), r.job.ID, WhoCLI, r.inCopy(doing(e.Tool, e.Input)), r.inCopy(detail(e.Tool, e.Input)))
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
			if e.Status == coding.Failed && !started && cj.Resume != "" {
				// Claude Code no longer has the session to resume: it starts afresh in the
				// copy, told where the job stands.
				go sess.Close()
				stopCLI()
				cj.Resume = ""
				cj.Brief = r.s.freshBrief(context.Background(), r.snapshot(), cj.Brief)
				cliCtx, cancelCLI := context.WithCancel(r.s.ctx)
				stopCLI = cancelCLI
				if fresh, err := ad.Start(cliCtx, cj); err == nil {
					r.s.Log.Info("jobs: the session was gone; started afresh", "job", r.job.ID)
					sess = fresh
					r.mu.Lock()
					r.session = fresh
					r.mu.Unlock()
					goto again
				}
			}
			ended = true
			close(roundDone)
			r.mu.Lock()
			r.saying = ""
			r.mu.Unlock()
			status := map[string]string{coding.Completed: Ready, coding.Stopped: Stopped, coding.Failed: Failed}[e.Status]
			r.mu.Lock()
			refused, instead := r.refused, r.instead
			r.refused, r.instead = false, ""
			r.mu.Unlock()
			// Finished (or stopped where the owner said no), it stays open for the next
			// round; out of steps, or failed, it's done with.
			if (e.Status == coding.Completed || refused) && !r.cancelled() && sess.Alive() {
				r.s.park(r.job.ID, sess, cj.Model, stopCLI)
			} else {
				s := sess
				go func() {
					s.Close()
					stopCLI()
				}()
			}
			text := e.Text
			if refused { // it stopped where the owner said no: nothing finished to check
				status = Stopped
				text = "Stopped where you said no."
				if t := strings.TrimSpace(e.Text); t != "" {
					text += "\n\n" + t
				}
			}
			r.roundOver(status, text, e)
			if rv := r.s.reviewerNow(); !refused && status == Ready && set.Review && rv != nil && !r.cancelled() {
				r.review(ctx, rv, e.Text)
			} else {
				r.finish(status, text)
			}
			if refused && instead != "" && !r.cancelled() {
				j := r.snapshot()
				go r.s.Reply(context.Background(), j.Agent, j.ID, instead) // one round, with what to do instead
			}
			break again // the CLI stays open, parked: the round is over
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

// end is the round over, and the job with it, as status.
func (r *run) end(status, text string, e coding.Event) {
	r.roundOver(status, text, e)
	r.finish(status, text)
}

// roundOver is what's kept when the CLI finishes a round: a snapshot of the
// copy, what it said, what it cost, and the questions it can no longer ask.
func (r *run) roundOver(status, text string, e coding.Event) {
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
	r.job.CostUSD += e.CostUSD
	r.job.Turns += e.Turns
	r.card.Asking = 0
	// What's still asked can't be answered now.
	asks := r.asks
	r.asks = map[string]pending{}
	j := r.job
	r.mu.Unlock()
	st := store.New(r.s.DB).For(j.Agent)
	for _, p := range asks {
		st.Settle(context.Background(), p.row, r.inCopy(askText(p.event))+"\n\nThe job ended before you answered.", time.Now().UTC())
	}
}

// finish ends the job as status, for now: the owner can carry it on.
func (r *run) finish(status, text string) {
	r.mu.Lock()
	if r.job.Status == Cancelled {
		status = Cancelled
	}
	r.job.Status, r.card.Status = status, status
	r.job.EndedAt = time.Now().UTC()
	r.card.CostUSD, r.card.Rounds = r.job.CostUSD, r.job.Rounds
	r.card.Summary = strings.TrimSpace(text)
	r.card.Now = map[string]string{Ready: "Done: for you to look at", Stopped: "Stopped: reply here to carry it on",
		Failed: "It didn't work out", Cancelled: "You stopped it"}[status]
	if status != Ready {
		r.card.Verdict = ""
	}
	j := r.job
	r.mu.Unlock()
	r.s.save(context.Background(), j)
	r.show()
}

func (r *run) ask(e coding.Event) {
	r.mu.Lock()
	ok, decided, why := r.gate.Decide(e)
	sess := r.session
	r.mu.Unlock()
	if decided && e.Tool != asksYou { // its own question is always the owner's
		sess.Answer(e.AskID, ok, why, false)
		return
	}
	// The owner's: a card with three answers.
	b, _ := json.Marshal(map[string]any{"job": r.job.ID, "ask": e.AskID, "tool": e.Tool, "detail": r.inCopy(detail(e.Tool, e.Input)),
		"why": whyAsked(r.gate.Rules, e), "questions": e.Input["questions"]})
	row := ulid.Make().String()
	r.mu.Lock()
	j := r.job
	r.mu.Unlock()
	if err := store.New(r.s.DB).For(j.Agent).SaveChat(context.Background(), memory.ChatMessage{ID: row, Author: memory.ChatAgent,
		Kind: memory.ChatJobAsk, Text: r.inCopy(askText(e)), Payload: b, At: time.Now().UTC(), Topic: j.Topic}); err != nil {
		r.s.Log.Warn("jobs: asking the owner failed; it's a no", "job", j.ID, "err", err)
		sess.Answer(e.AskID, false, "Tinio couldn't ask the owner.", false)
		return
	}
	r.mu.Lock()
	r.asks[e.AskID] = pending{event: e, row: row}
	r.job.Status, r.card.Status = Waiting, Waiting
	r.card.Asking = len(r.asks)
	r.mu.Unlock()
	r.s.step(context.Background(), j.ID, WhoCLI, "Asked to "+strings.ToLower(askVerb(e.Tool)), r.inCopy(detail(e.Tool, e.Input)))
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
		said = "You said no, so it stopped there."
		if n := strings.TrimSpace(note); n != "" {
			said = "You said no: " + n + " It carries on with that."
		}
		r.refused, r.instead = true, strings.TrimSpace(note)
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
	store.New(r.s.DB).For(j.Agent).Settle(ctx, p.row, r.inCopy(askText(p.event))+"\n\n"+said, time.Now().UTC())
	r.now("", true)
	if !allow {
		// It should stop there; if it hasn't before long, it's stopped.
		go func() {
			select {
			case <-r.over:
			case <-time.After(15 * time.Second):
				r.mu.Lock()
				still := r.refused && r.session == sess
				r.mu.Unlock()
				if still {
					sess.Interrupt()
				}
			}
		}()
	}
	return sess.Answer(ask, allow, why, !allow)
}

// choose answers Claude Code's own question with the owner's choices, by
// question.
func (r *run) choose(ctx context.Context, ask string, answers map[string]string) error {
	r.mu.Lock()
	p, ok := r.asks[ask]
	if !ok || p.event.Tool != asksYou {
		r.mu.Unlock()
		return ErrNoJob
	}
	delete(r.asks, ask)
	if len(r.asks) == 0 && r.job.Status == Waiting {
		r.job.Status, r.card.Status = Working, Working
	}
	r.card.Asking = len(r.asks)
	sess, j := r.session, r.job
	r.mu.Unlock()
	var said []string
	for _, q := range questionsOf(p.event.Input) {
		if a := strings.TrimSpace(answers[q.Question]); a != "" {
			said = append(said, a)
		}
	}
	r.s.save(ctx, j)
	r.s.step(ctx, j.ID, WhoOwner, "Chose "+strings.Join(said, "; "), "")
	store.New(r.s.DB).For(j.Agent).Settle(ctx, p.row, r.inCopy(askText(p.event))+"\n\nYou chose: "+strings.Join(said, "; ")+".", time.Now().UTC())
	r.now("", true)
	return sess.Choose(ask, answers)
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

// inCopy is s with the job's own copy left out of it: a command that starts by
// going into it, and paths in it, read as they would from inside.
func (r *run) inCopy(s string) string {
	dir := r.job.Dir
	if dir == "" {
		return s
	}
	for _, cd := range []string{`cd "` + dir + `" && `, `cd '` + dir + `' && `, "cd " + dir + " && ", `cd "` + dir + `"; `, "cd " + dir + "; "} {
		s = strings.ReplaceAll(s, cd, "")
	}
	return strings.ReplaceAll(s, dir+string(filepath.Separator), "")
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// cutRunes is s, at most n runes of it, with … when cut.
func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
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
	case "Grep":
		if p := str("pattern"); p != "" {
			return "Searching for " + strconv.Quote(cutRunes(p, 60))
		}
		return "Searching the code"
	case "Glob":
		if p := str("pattern"); p != "" {
			return "Finding files like " + cutRunes(p, 60)
		}
		return "Finding files"
	case "LS":
		return "Looking in " + filepath.Base(str("path"))
	case "WebFetch":
		if u, err := url.Parse(str("url")); err == nil && u.Host != "" {
			return "Reading a page on " + u.Host
		}
		return "Reading a web page"
	case "WebSearch":
		return "Searching the web for " + strconv.Quote(cutRunes(str("query"), 60))
	case "Task":
		if d := str("description"); d != "" {
			return "Handing part to a helper: " + cutRunes(d, 60)
		}
		return "Handing part to a helper"
	case "NotebookEdit":
		return "Changing " + filepath.Base(str("notebook_path"))
	case "TodoWrite":
		return "Planning"
	}
	return "Using " + tool
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
	if e.Tool == asksYou {
		var qs []string
		for _, q := range questionsOf(e.Input) {
			qs = append(qs, q.Question)
		}
		return "Claude Code asks you: " + strings.Join(qs, " ")
	}
	return "Claude Code wants to " + strings.ToLower(askVerb(e.Tool)) + ": " + detail(e.Tool, e.Input)
}

// asksYou is Claude Code's own question to the owner, with choices: never a
// rule's to allow, always the owner's to answer.
const asksYou = "AskUserQuestion"

type question struct {
	Question string `json:"question"`
	Options  []struct {
		Label string `json:"label"`
	} `json:"options"`
}

func questionsOf(input map[string]any) []question {
	var qs []question
	b, _ := json.Marshal(input["questions"])
	json.Unmarshal(b, &qs)
	return qs
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
