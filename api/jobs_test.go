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
	if n > 1 { // carried on: it says it did it, and ends
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
