package api

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Grandbusta/tinio/api/models"
)

// Each job's model is Tinio's default until the owner picks another of the
// priced ones; picking tells the agents, who start again on it; one set in the
// environment wins and can't be changed in the app.
func TestTheOwnerChoosesTheModels(t *testing.T) {
	ta := newTestAPI(t)
	for _, j := range models.Jobs {
		t.Setenv(j.Env, "")
	}
	owner := ta.signUp(t)
	job := func(out map[string]any, name string) map[string]any {
		t.Helper()
		for _, j := range data(out)["jobs"].([]any) {
			if j := j.(map[string]any); j["job"] == name {
				return j
			}
		}
		t.Fatalf("no job %s", name)
		return nil
	}

	_, out := ta.do(t, "GET", "/v1/models", nil, owner, "")
	if r := job(out, "reply"); r["model"] != "gpt-6.1-terra" || r["from"] != "default" || len(r["models"].([]any)) != 8 {
		t.Errorf("replies to start with: %v", r)
	}
	if w := job(out, "work"); w["model"] != "" {
		t.Errorf("works to start with: %v", w)
	}

	changed, stop := ta.db.Listen(models.KeysChanged, 1)
	defer stop()
	rec, out := ta.do(t, "POST", "/v1/models", map[string]any{"job": "reply", "model": "gpt-6.1-sol"}, owner, site)
	if r := job(out, "reply"); rec.Code != http.StatusOK || r["model"] != "gpt-6.1-sol" || r["from"] != "app" {
		t.Fatalf("choosing: %d %v", rec.Code, r)
	}
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Error("the agents weren't told")
	}

	for _, bad := range []map[string]any{
		{"job": "reply", "model": "gpt-4o"},
		{"job": "decision", "model": "gpt-6.1-terra"},
		{"job": "sleeping", "model": "gpt-6-luna"},
	} {
		if rec, _ := ta.do(t, "POST", "/v1/models", bad, owner, site); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d", bad, rec.Code)
		}
	}

	if _, out := ta.do(t, "POST", "/v1/models", map[string]any{"job": "reply", "model": ""}, owner, site); job(out, "reply")["from"] != "default" {
		t.Errorf("back to the default: %v", job(out, "reply"))
	}

	t.Setenv("OPENAI_DAY_MODEL", "gpt-6.2-sol")
	_, out = ta.do(t, "GET", "/v1/models", nil, owner, "")
	if d := job(out, "day"); d["model"] != "gpt-6.2-sol" || d["from"] != "environment" {
		t.Errorf("from the environment: %v", d)
	}
	if rec, _ := ta.do(t, "POST", "/v1/models", map[string]any{"job": "day", "model": "gpt-6-luna"}, owner, site); rec.Code != http.StatusBadRequest {
		t.Errorf("changing one set in the environment: %d", rec.Code)
	}
}

// fakeClaude puts a `claude` where the finder looks, signed in or not.
func fakeClaude(t *testing.T, signedIn bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake is a shell script")
	}
	dir := t.TempDir()
	in := "false"
	if signedIn {
		in = "true"
	}
	script := "#!/bin/sh\ncase \"$*\" in\n  \"--version\") echo '2.1.331 (Claude Code)' ;;\n  \"auth status --json\") echo '{\"loggedIn\": " + in + ", \"subscriptionType\": \"max\"}' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	old := finder.Dirs
	finder.Dirs = []string{} // PATH only: the fake
	finder.Forget()
	t.Cleanup(func() { finder.Dirs = old; finder.Forget() })
}

// With Claude Code signed in, the owner can think with it: every job takes its
// Claude Code default (Opus 5.5 for works), each can still be changed, and
// the environment's OpenAI models no longer apply.
func TestThinkingWithClaudeCode(t *testing.T) {
	ta := newTestAPI(t)
	for _, j := range models.Jobs {
		t.Setenv(j.Env, "")
	}
	t.Setenv("OPENAI_API_KEY", "")
	owner := ta.signUp(t)
	use := func(source string) (int, map[string]any) {
		rec, out := ta.do(t, "POST", "/v1/sources", map[string]any{"source": source}, owner, site)
		return rec.Code, out
	}

	fakeClaude(t, false)
	if code, _ := use("claude"); code != http.StatusBadRequest {
		t.Fatalf("Claude Code signed out: %d", code)
	}
	if code, _ := use("openai"); code != http.StatusBadRequest {
		t.Fatalf("no key: %d", code)
	}

	fakeClaude(t, true)
	_, out := ta.do(t, "GET", "/v1/sources", nil, owner, "")
	if d := data(out); d["source"] != "openai" || d["key"] != false || d["clis"].([]any)[0].(map[string]any)["usable"] != true {
		t.Fatalf("sources: %v", d)
	}
	changed, stop := ta.db.Listen(models.KeysChanged, 1)
	defer stop()
	if code, out := use("claude"); code != http.StatusOK || data(out)["source"] != "claude" {
		t.Fatalf("using Claude Code: %d %v", code, out)
	}
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Error("the agents weren't told")
	}

	t.Setenv("OPENAI_DAY_MODEL", "gpt-6.2-sol")
	_, out = ta.do(t, "GET", "/v1/models", nil, owner, "")
	if data(out)["source"] != "claude" {
		t.Fatalf("models' source: %v", data(out))
	}
	want := map[string]string{"reply": "claude:claude-sonnet-5-5", "decision": "claude:claude-haiku-5-5", "day": "claude:claude-haiku-5-5",
		"light": "claude:claude-haiku-5-5", "work": "claude:claude-opus-5-5"}
	for _, j := range data(out)["jobs"].([]any) {
		j := j.(map[string]any)
		if j["model"] != want[j["job"].(string)] || j["from"] != "default" || j["default"] != want[j["job"].(string)] {
			t.Errorf("%s: %v", j["job"], j)
		}
	}

	if rec, _ := ta.do(t, "POST", "/v1/models", map[string]any{"job": "decision", "model": "claude:claude-opus-5-5"}, owner, site); rec.Code != http.StatusOK {
		t.Errorf("choosing Opus for decisions: %d", rec.Code)
	}
	if rec, _ := ta.do(t, "POST", "/v1/models", map[string]any{"job": "reply", "model": "claude:gpt-6-luna"}, owner, site); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown Claude model: %d", rec.Code)
	}
}
