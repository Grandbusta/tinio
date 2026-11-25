package host

import (
	"log/slog"
	"strings"
	"testing"
)

func TestModelsFromClaudeCodeNeedNoKey(t *testing.T) {
	all := map[string]string{"reply": "claude:claude-sonnet-5-5", "decision": "claude:claude-haiku-5-5", "day": "claude:claude-haiku-5-5",
		"light": "claude:claude-haiku-5-5", "work": "claude:claude-opus-5-5"}
	th := Thinking{Chosen: all, Claude: "/usr/bin/true", Dir: t.TempDir()}
	if th.NeedsKey() {
		t.Fatal("all on Claude Code needs no key")
	}
	m, err := ModelsFrom(slog.Default(), th)
	if err != nil {
		t.Fatal(err)
	}
	if m.System1 == nil || m.System2 == nil || m.Day == nil || m.Light == nil || m.Work == nil || m.Embedder != nil || m.Reasoner("gpt-6.2-sol", "low") == nil {
		t.Fatalf("models %+v", m)
	}

	th.Claude = ""
	if _, err := ModelsFrom(slog.Default(), th); err == nil || !strings.Contains(err.Error(), "Claude Code") {
		t.Fatalf("no claude: %v", err)
	}

	mixed := map[string]string{"reply": "claude:claude-sonnet-5-5", "decision": ""}
	th = Thinking{Chosen: mixed, Claude: "/usr/bin/true"}
	if !th.NeedsKey() {
		t.Fatal("a job on the default needs the key")
	}
	if _, err := ModelsFrom(slog.Default(), th); err == nil || !strings.Contains(err.Error(), "OpenAI key") {
		t.Fatalf("no key: %v", err)
	}
}
