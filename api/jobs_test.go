package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Grandbusta/tinio/internal/coding"
	"github.com/Grandbusta/tinio/internal/coding/jobs"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/providers/system2"
	"github.com/Grandbusta/tinio/internal/store"
)

// fakeCoder is a coding CLI that plays a script: each Start is the next job's.
type fakeCoder struct {
	mu      sync.Mutex
	started []coding.Job
	answers []string // "allow" or "deny: why", in order
}

func (f *fakeCoder) Start(_ context.Context, j coding.Job) (coding.Session, error) {
	f.mu.Lock()
	f.started = append(f.started, j)
	n := len(f.started)
	f.mu.Unlock()
	s := &fakeSession{f: f, events: make(chan coding.Event), answered: make(chan bool)}
	go s.play(n, j)
	return s, nil
}

type fakeSession struct {
	f        *fakeCoder
	events   chan coding.Event
	answered chan bool
	once     sync.Once
}

func (s *fakeSession) play(n int, j coding.Job) {
	defer close(s.events)
	ask := func(id, tool string, input map[string]any) {
		s.events <- coding.Event{Kind: coding.Ask, AskID: id, Tool: tool, Input: input}
		<-s.answered
	}
	if n > 1 { // carried on: it adds a note, says it did it, and ends
		os.WriteFile(filepath.Join(j.Dir, "notes.txt"), []byte("15% off £13.30 is £11.31\n"), 0o644)
		s.events <- coding.Event{Kind: coding.Started, SessionID: "s-1"}
		s.events <- coding.Event{Kind: coding.Done, Status: coding.Completed, Text: "Added the test.", CostUSD: 0.05, Turns: 2}
		return
	}
	s.events <- coding.Event{Kind: coding.Started, SessionID: "s-1"}
	s.events <- coding.Event{Kind: coding.Step, Tool: "Read", Input: map[string]any{"file_path": filepath.Join(j.Dir, "basket.go")}}
	s.events <- coding.Event{Kind: coding.Step, Tool: "Edit", Input: map[string]any{"file_path": filepath.Join(j.Dir, "basket.go")}}
	s.events <- coding.Event{Kind: coding.Changed, Tool: "Edit", Path: filepath.Join(j.Dir, "basket.go")}
	ask("r1", "Edit", map[string]any{"file_path": filepath.Join(j.Dir, "basket.go")}) // the rules allow it
	ask("r2", "Bash", map[string]any{"command": "npm install decimal.js"})            // the owner: for this job
	ask("r3", "Bash", map[string]any{"command": "npm install decimal.js"})            // allowed again, unasked
	ask("r4", "WebFetch", map[string]any{"url": "https://example.com/docs"})          // the owner: no
	os.WriteFile(filepath.Join(j.Dir, "basket.go"), []byte("package shop\n\n// Total rounds once.\nfunc Total() int { return 1131 }\n"), 0o644)
	s.events <- coding.Event{Kind: coding.Done, Status: coding.Completed, Text: "Fixed it.", CostUSD: 0.15, Turns: 5}
}

func (s *fakeSession) Events() <-chan coding.Event { return s.events }
func (s *fakeSession) Send(string) error           { return nil }
func (s *fakeSession) Interrupt() error            { return nil }
func (s *fakeSession) Close() error                { return nil }
func (s *fakeSession) Answer(_ string, allow bool, why string) error {
	s.f.mu.Lock()
	if allow {
		s.f.answers = append(s.f.answers, "allow")
	} else {
		s.f.answers = append(s.f.answers, "deny: "+why)
	}
	s.f.mu.Unlock()
	s.answered <- allow
	return nil
}

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "--allow-empty", "-m", "start"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"test":"vitest"}}`), 0o644)
	return dir
}

// waitFor polls until ok, for a few seconds.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("waited for %s", what)
}

// A coding job end to end: started on a copy of the repository on its own
// branch, in the project's topic; its card shows it; what the rules don't
// allow comes to the owner as a question, answered for the job (and not asked
// again) or no, with what they'd like instead; it finishes; and the owner's
// reply to its card carries it on, unheard by the agent.
func TestACodingJob(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	fake := &fakeCoder{}
	ta.jobs.Adapter = fake
	repo := gitRepo(t)
	ctx := context.Background()
	st := store.New(ta.db).For(zara)

	j, err := ta.jobs.Begin(ctx, zara, jobs.Folder{ID: "f1", Name: "shop-site", Path: repo}, "Fix the basket rounding", "Round once, on the total.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(j.Branch, "tinio/fix-the-basket-rounding-") || j.Base != "main" {
		t.Errorf("branch %q from %q", j.Branch, j.Base)
	}
	if out, _ := exec.Command("git", "-C", repo, "branch", "--list", j.Branch).Output(); !strings.Contains(string(out), j.Branch) {
		t.Errorf("no branch %s in the repository", j.Branch)
	}
	if _, err := os.Stat(filepath.Join(j.Dir, ".git")); err != nil || j.Dir == repo {
		t.Errorf("no copy at %s: %v", j.Dir, err)
	}
	topic, ok, err := st.Topic(ctx, j.Topic)
	if err != nil || !ok || topic.Title != "shop-site" {
		t.Errorf("project topic %+v %v", topic, err)
	}

	asks := func() []memory.ChatMessage {
		rows, _ := st.Chat(ctx, "", 100)
		var out []memory.ChatMessage
		for _, r := range rows {
			if r.Kind == memory.ChatJobAsk {
				out = append(out, r)
			}
		}
		return out
	}
	answer := func(row memory.ChatMessage, body map[string]any) {
		t.Helper()
		var p struct{ Job, Ask string }
		json.Unmarshal(row.Payload, &p)
		if rec, out := ta.do(t, "POST", "/v1/agents/"+zara+"/jobs/"+p.Job+"/asks/"+p.Ask, body, ada, site); rec.Code != http.StatusOK {
			t.Fatalf("answering: %d %v", rec.Code, out)
		}
	}
	waitFor(t, "the first question", func() bool { return len(asks()) == 1 })
	if a := asks()[0]; !strings.Contains(a.Text, "npm install decimal.js") || a.Topic != j.Topic {
		t.Errorf("question %q in topic %q", a.Text, a.Topic)
	}
	answer(asks()[0], map[string]any{"answer": "job"})
	waitFor(t, "the second question", func() bool { return len(asks()) == 2 })
	answer(asks()[1], map[string]any{"answer": "no", "note": "Use the docs in the repository."})

	waitFor(t, "the job to finish", func() bool { g, _ := ta.jobs.Get(ctx, zara, j.ID); return g.Status == jobs.Ready })
	fake.mu.Lock()
	got := strings.Join(fake.answers, " | ")
	fake.mu.Unlock()
	if got != "allow | allow | allow | deny: Ada said no. Use the docs in the repository." {
		t.Errorf("answers: %s", got)
	}
	a := asks()
	if a[0].SettledAt.IsZero() || !strings.Contains(a[0].Text, "Allowed for this job") || !strings.Contains(a[1].Text, "You said no. Use the docs") {
		t.Errorf("questions settled as %q and %q", a[0].Text, a[1].Text)
	}
	card, ok, _ := st.ChatMessage(ctx, j.Card)
	var c jobs.Card
	json.Unmarshal(card.Payload, &c)
	if !ok || card.Kind != memory.ChatJob || card.Topic != j.Topic || c.Status != jobs.Ready || c.Summary != "Fixed it." ||
		len(c.Files) != 1 || c.Files[0] != "basket.go" || c.CostUSD != 0.15 || c.Asking != 0 {
		t.Errorf("card %v %+v", ok, c)
	}
	_, out := ta.do(t, "GET", "/v1/agents/"+zara+"/jobs", nil, ada, "")
	if l := data(out)["jobs"].([]any); len(l) != 1 || l[0].(map[string]any)["status"] != "ready" || l[0].(map[string]any)["folder"] != filepath.Base(repo) {
		t.Errorf("jobs: %v", l)
	}

	rec, out := ta.do(t, "POST", "/v1/agents/"+zara+"/messages", map[string]any{"text": "Also add a test for 15% off.", "reply_to": j.Card}, ada, site)
	if rec.Code != http.StatusCreated {
		t.Fatalf("replying to the card: %d %v", rec.Code, out)
	}
	waitFor(t, "the job to carry on", func() bool { fake.mu.Lock(); defer fake.mu.Unlock(); return len(fake.started) == 2 })
	fake.mu.Lock()
	again := fake.started[1]
	fake.mu.Unlock()
	if again.Resume != "s-1" || again.Brief != "Also add a test for 15% off." || again.Dir != j.Dir {
		t.Errorf("carried on with %+v", again)
	}
	unheard, _ := st.Unheard(ctx, 10)
	for _, m := range unheard {
		if m.Text == "Also add a test for 15% off." {
			t.Error("the agent hears a reply meant for the job")
		}
	}
	waitFor(t, "it to finish again", func() bool { g, _ := ta.jobs.Get(ctx, zara, j.ID); return g.Status == jobs.Ready && g.CostUSD > 0.19 })
}

// After a job: its page shows what it did and its changes, by round; going
// back a round undoes the last; commit puts it on its branch; push sends the
// branch to origin and opens a pull request through the owner's gh.
func TestAJobsChangesCommitAndPullRequest(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	fake := &fakeCoder{}
	ta.jobs.Adapter = fake
	repo := gitRepo(t)
	ctx := context.Background()
	st := store.New(ta.db).For(zara)
	j, err := ta.jobs.Begin(ctx, zara, jobs.Folder{ID: "f1", Name: "shop-site", Path: repo}, "Fix the basket rounding", "Round once.")
	if err != nil {
		t.Fatal(err)
	}
	// Answer its questions as they come: all yes.
	go func() {
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			rows, _ := st.Chat(ctx, "", 100)
			for _, r := range rows {
				if r.Kind == memory.ChatJobAsk && r.SettledAt.IsZero() {
					var p struct{ Job, Ask string }
					json.Unmarshal(r.Payload, &p)
					ta.jobs.Answer(ctx, zara, p.Job, p.Ask, jobs.Once, "")
				}
			}
		}
	}()
	waitFor(t, "the first round", func() bool { g, _ := ta.jobs.Get(ctx, zara, j.ID); return g.Status == jobs.Ready && g.Rounds == 1 })
	page := func(q string) map[string]any {
		t.Helper()
		rec, out := ta.do(t, "GET", "/v1/agents/"+zara+"/jobs/"+j.ID+q, nil, ada, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("job page: %d %v", rec.Code, out)
		}
		return data(out)
	}
	p := page("")
	ch := p["changes"].([]any)
	if len(ch) != 1 || ch[0].(map[string]any)["path"] != "basket.go" || !strings.Contains(ch[0].(map[string]any)["patch"].(string), "+func Total() int { return 1131 }") {
		t.Fatalf("changes %v", ch)
	}
	steps := p["steps"].([]any)
	if first := steps[0].(map[string]any); first["who"] != "agent" || first["what"] != "Wrote the brief" {
		t.Errorf("first step %v", first)
	}
	if out, _ := exec.Command("git", "-C", repo, "log", "--oneline").Output(); strings.Count(string(out), "\n") != 1 {
		t.Errorf("the owner's log has more than their own commit:\n%s", out)
	}

	// A second round, its own changes, and back again.
	if err := ta.jobs.Reply(ctx, zara, j.ID, "Write down the example."); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the second round", func() bool { g, _ := ta.jobs.Get(ctx, zara, j.ID); return g.Status == jobs.Ready && g.Rounds == 2 })
	if ch := page("?round=2")["changes"].([]any); len(ch) != 1 || ch[0].(map[string]any)["path"] != "notes.txt" {
		t.Fatalf("round 2's changes %v", ch)
	}
	if rec, out := ta.do(t, "POST", "/v1/agents/"+zara+"/jobs/"+j.ID+"/goback", nil, ada, site); rec.Code != http.StatusOK {
		t.Fatalf("going back: %d %v", rec.Code, out)
	}
	if _, err := os.Stat(filepath.Join(j.Dir, "notes.txt")); !os.IsNotExist(err) {
		t.Error("going back left the round's new file")
	}
	if ch := page("")["changes"].([]any); len(ch) != 1 || ch[0].(map[string]any)["path"] != "basket.go" {
		t.Errorf("after going back %v", ch)
	}

	// Push before commit: no.
	if rec, _ := ta.do(t, "POST", "/v1/agents/"+zara+"/jobs/"+j.ID+"/push", nil, ada, site); rec.Code != http.StatusBadRequest {
		t.Errorf("pushing before committing: %d", rec.Code)
	}
	exec.Command("git", "-C", repo, "config", "user.name", "Ada").Run()
	exec.Command("git", "-C", repo, "config", "user.email", "ada@example.invalid").Run()
	rec, out := ta.do(t, "POST", "/v1/agents/"+zara+"/jobs/"+j.ID+"/commit", map[string]any{"message": "Round the basket total once"}, ada, site)
	if rec.Code != http.StatusOK || data(out)["job"].(map[string]any)["commit"] == "" {
		t.Fatalf("committing: %d %v", rec.Code, out)
	}
	if log, _ := exec.Command("git", "-C", j.Dir, "log", "-1", "--format=%s %an").Output(); strings.TrimSpace(string(log)) != "Round the basket total once Ada" {
		t.Errorf("the commit: %q", log)
	}

	// Push: to a bare origin, and a pull request through a fake gh.
	origin := t.TempDir()
	exec.Command("git", "init", "-q", "--bare", origin).Run()
	exec.Command("git", "-C", repo, "remote", "add", "origin", origin).Run()
	gh := t.TempDir()
	os.WriteFile(filepath.Join(gh, "gh"), []byte("#!/bin/sh\ncase \"$1 $2\" in\n\"auth status\") echo 'Logged in to github.com account ada';;\n\"pr create\") echo 'https://github.com/ada/shop/pull/7';;\nesac\n"), 0o755)
	t.Setenv("PATH", gh+string(os.PathListSeparator)+os.Getenv("PATH"))
	rec, out = ta.do(t, "POST", "/v1/agents/"+zara+"/jobs/"+j.ID+"/push", map[string]any{"body": "Rounds once."}, ada, site)
	if rec.Code != http.StatusOK || data(out)["job"].(map[string]any)["pr"] != "https://github.com/ada/shop/pull/7" {
		t.Fatalf("pushing: %d %v", rec.Code, out)
	}
	if b, _ := exec.Command("git", "-C", origin, "branch", "--list", j.Branch).Output(); !strings.Contains(string(b), j.Branch) {
		t.Errorf("origin has no %s", j.Branch)
	}
	card, _, _ := st.ChatMessage(ctx, j.Card)
	var c jobs.Card
	json.Unmarshal(card.Payload, &c)
	if c.PR != "https://github.com/ada/shop/pull/7" || c.Commit == "" || len(c.Files) == 0 {
		t.Errorf("card %+v", c)
	}
}

// fakeReviewer is the agent's model checking work: it sends the first round
// back and passes the second.
type fakeReviewer struct {
	mu     sync.Mutex
	seen   []string // what it was asked, each time
	learnt []string // what it was asked to learn from
}

func (f *fakeReviewer) Reason(_ context.Context, req system2.Request) (system2.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.Format != nil && req.Format.Name == "notes" { // learning from a committed job
		f.learnt = append(f.learnt, req.Message)
		return system2.Response{Text: `{"notes": ["Money is in whole pence: use money.ts, no new packages.", "go test ./... runs everything; it takes a few seconds."]}`}, nil
	}
	f.seen = append(f.seen, req.Message)
	if len(f.seen) == 1 {
		return system2.Response{Text: `{"ready": false, "to_owner": "", "to_cli": "Add a note with the 15% case."}`, CostUSD: 0.02}, nil
	}
	return system2.Response{Text: `{"ready": true, "to_owner": "It rounds once, the tests pass, and the note has the 15% case.", "to_cli": ""}`, CostUSD: 0.02}, nil
}

// The agent checks each round before the owner sees it: Tinio runs the
// repository's tests in the copy, the agent reads the changes against the
// brief, sends the first round back with what to fix, and passes the second;
// only then is it ready for the owner, with the agent's word on its card.
func TestTheAgentChecksTheWork(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	fake, reviewer := &fakeCoder{}, &fakeReviewer{}
	ta.jobs.Adapter = fake
	ta.jobs.SetReviewer(reviewer)
	t.Cleanup(func() { ta.jobs.SetReviewer(nil) })
	repo := gitRepo(t)
	os.Remove(filepath.Join(repo, "package.json"))
	os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module shop\n\ngo 1.22\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "basket_test.go"), []byte("package shop\n\nimport \"testing\"\n\nfunc TestTotal(t *testing.T) {\n\tif Total() != 1131 {\n\t\tt.Fatal(Total())\n\t}\n}\n"), 0o644)
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "a test"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	ctx := context.Background()
	st := store.New(ta.db).For(zara)
	j, err := ta.jobs.Begin(ctx, zara, jobs.Folder{ID: "f1", Name: "shop-site", Path: repo}, "Fix the basket rounding", "Round once, on the total.")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			rows, _ := st.Chat(ctx, "", 100)
			for _, r := range rows {
				if r.Kind == memory.ChatJobAsk && r.SettledAt.IsZero() {
					var p struct{ Job, Ask string }
					json.Unmarshal(r.Payload, &p)
					ta.jobs.Answer(ctx, zara, p.Job, p.Ask, jobs.Once, "")
				}
			}
		}
	}()
	for end := time.Now().Add(60 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		g, _ := ta.jobs.Get(ctx, zara, j.ID)
		if g.Status == jobs.Ready && g.Rounds == 2 {
			break
		}
		if time.Now().After(end) {
			steps, _ := ta.jobs.Steps(ctx, j.ID)
			t.Fatalf("waited for the second round to pass; it's %s after %d rounds: %+v", g.Status, g.Rounds, steps)
		}
	}

	fake.mu.Lock()
	started := fake.started
	fake.mu.Unlock()
	if len(started) != 2 || started[1].Resume != "s-1" || started[1].Brief != "Add a note with the 15% case." {
		t.Fatalf("the second round should carry on the session with what the agent said: %+v", started)
	}
	reviewer.mu.Lock()
	seen := reviewer.seen
	reviewer.mu.Unlock()
	if len(seen) != 2 || !strings.Contains(seen[0], "Round once, on the total.") || !strings.Contains(seen[0], "+func Total() int { return 1131 }") ||
		!strings.Contains(seen[0], "$ go test ./...: passed") || !strings.Contains(seen[1], "notes.txt") || strings.Contains(seen[0], "asked for since") {
		t.Fatalf("the agent should read the brief, the tests and the changes: %q", seen)
	}
	steps, err := ta.jobs.Steps(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	var story []string
	for _, s := range steps {
		if s.Who == jobs.WhoAgent {
			story = append(story, s.What)
		}
	}
	want := []string{"Wrote the brief", "Ran go test ./...: passed", "Checked it and sent it back", "Ran go test ./...: passed", "Checked it: ready for you"}
	if strings.Join(story, " | ") != strings.Join(want, " | ") {
		t.Errorf("the agent's part of the story:\n%q\nwant\n%q", story, want)
	}
	g, _ := ta.jobs.Get(ctx, zara, j.ID)
	card, _, _ := st.ChatMessage(ctx, g.Card)
	var c jobs.Card
	json.Unmarshal(card.Payload, &c)
	if c.Status != jobs.Ready || c.Verdict != "It rounds once, the tests pass, and the note has the 15% case." || c.Summary != "Added the test." {
		t.Errorf("the card: %+v", c)
	}
	if g.CostUSD < 0.23 { // two rounds (0.15 and 0.05) and two checks (0.02 each)
		t.Errorf("the job's cost %v should count the checks", g.CostUSD)
	}

	// What the owner asks for later is part of what the agent checks against.
	if err := ta.jobs.Reply(ctx, zara, j.ID, "Also cover 20% off."); err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(60 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		reviewer.mu.Lock()
		n := len(reviewer.seen)
		reviewer.mu.Unlock()
		g, _ := ta.jobs.Get(ctx, zara, j.ID)
		if n == 3 && g.Status == jobs.Ready {
			break
		}
		if time.Now().After(end) {
			t.Fatalf("waited for the third check; %d so far, the job %s", n, g.Status)
		}
	}
	reviewer.mu.Lock()
	third := reviewer.seen[2]
	reviewer.mu.Unlock()
	if !strings.Contains(third, "asked for since") || !strings.Contains(third, "- Also cover 20% off.") {
		t.Errorf("the check should know what the owner asked for since the brief:\n%s", third)
	}
}

// A committed job teaches the agent about its folder: a few notes, from its
// story, that the next job there starts with (in what Claude Code is told, and
// in the brief they write); the job's page shows them, and the owner can take
// one out.
func TestAJobTeachesTheAgentAboutItsFolder(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	fake, reviewer := &fakeCoder{}, &fakeReviewer{}
	ta.jobs.Adapter = fake
	ta.jobs.SetReviewer(reviewer)
	t.Cleanup(func() { ta.jobs.SetReviewer(nil) })
	repo := gitRepo(t)
	ctx := context.Background()
	st := store.New(ta.db).For(zara)
	folder := jobs.Folder{ID: "f1", Name: "shop-site", Path: repo}
	j, err := ta.jobs.Begin(ctx, zara, folder, "Fix the basket rounding", "Round once.")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			rows, _ := st.Chat(ctx, "", 100)
			for _, r := range rows {
				if r.Kind == memory.ChatJobAsk && r.SettledAt.IsZero() {
					var p struct{ Job, Ask string }
					json.Unmarshal(r.Payload, &p)
					ta.jobs.Answer(ctx, zara, p.Job, p.Ask, jobs.Once, "No new packages: use money.ts.")
				}
			}
		}
	}()
	waitFor(t, "the job to be ready", func() bool { g, _ := ta.jobs.Get(ctx, zara, j.ID); return g.Status == jobs.Ready })
	rec, out := ta.do(t, "POST", "/v1/agents/"+zara+"/jobs/"+j.ID+"/commit", map[string]any{"message": "Round the basket total once"}, ada, site)
	if rec.Code != http.StatusOK {
		t.Fatalf("commit: %d %v", rec.Code, out)
	}
	waitFor(t, "the notes", func() bool { n, _ := ta.jobs.Notes(ctx, zara, "f1"); return len(n) == 2 })
	reviewer.mu.Lock()
	learnt := reviewer.learnt
	reviewer.mu.Unlock()
	if len(learnt) != 1 || !strings.Contains(learnt[0], "Committed it") || !strings.Contains(learnt[0], "(nothing yet)") {
		t.Errorf("it should learn from the job's story, from nothing yet: %q", learnt)
	}

	// The page shows them, and the next job there starts with them.
	_, out = ta.do(t, "GET", "/v1/agents/"+zara+"/jobs/"+j.ID, nil, ada, "")
	if notes := data(out)["notes"].([]any); len(notes) != 2 || notes[0] != "Money is in whole pence: use money.ts, no new packages." {
		t.Errorf("the page's notes: %v", notes)
	}
	if d := ta.jobs.Tool(zara, []jobs.Folder{folder}, "Opus 5.5").Describe(); !strings.Contains(d, "What you keep about shop-site") || !strings.Contains(d, "use money.ts") {
		t.Errorf("the brief isn't told what they keep:\n%s", d)
	}
	fake.mu.Lock()
	before := len(fake.started)
	fake.mu.Unlock()
	next, err := ta.jobs.Begin(ctx, zara, folder, "Add a test", "Add a test.")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the next job to start", func() bool { fake.mu.Lock(); defer fake.mu.Unlock(); return len(fake.started) > before })
	fake.mu.Lock()
	system := fake.started[before].System
	fake.mu.Unlock()
	if !strings.Contains(system, "What earlier jobs in this repository taught") || !strings.Contains(system, "use money.ts") {
		t.Errorf("Claude Code isn't told what earlier jobs taught:\n%s", system)
	}
	waitFor(t, "the next job to end", func() bool { g, _ := ta.jobs.Get(ctx, zara, next.ID); return g.Status == jobs.Ready })

	// The owner takes one out.
	rec, out = ta.do(t, "POST", "/v1/agents/"+zara+"/jobs/"+j.ID+"/notes", map[string]any{"notes": []string{"go test ./... runs everything; it takes a few seconds."}}, ada, site)
	if rec.Code != http.StatusOK || len(data(out)["notes"].([]any)) != 1 {
		t.Errorf("taking one out: %d %v", rec.Code, out)
	}
}
