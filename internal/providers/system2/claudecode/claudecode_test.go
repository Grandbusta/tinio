package claudecode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Grandbusta/tinio/internal/providers/system2"
)

// fakeClaude is a `claude` that keeps its arguments and stdin and prints out,
// one stream-json line each. It also writes stderr when asked.
func fakeClaude(t *testing.T, out []any, stderr string) (*Client, func() ([]string, string)) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake is a shell script")
	}
	dir := t.TempDir()
	var lines []string
	for _, o := range out {
		b, _ := json.Marshal(o)
		lines = append(lines, string(b))
	}
	if err := os.WriteFile(filepath.Join(dir, "out"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\0' \"$a\"; done > \"" + dir + "/args\"\ncat > \"" + dir + "/stdin\"\n"
	if stderr != "" {
		script += "echo '" + stderr + "' >&2\n"
	}
	script += "cat \"" + dir + "/out\"\n"
	prog := filepath.Join(dir, "claude")
	if err := os.WriteFile(prog, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Client{Program: prog, Model: "claude-sonnet-5-5", Dir: t.TempDir()}
	return c, func() ([]string, string) {
		a, _ := os.ReadFile(filepath.Join(dir, "args"))
		in, _ := os.ReadFile(filepath.Join(dir, "stdin"))
		return strings.Split(strings.TrimSuffix(string(a), "\x00"), "\x00"), string(in)
	}
}

func delta(kind, field, text string) map[string]any {
	return map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_delta", "delta": map[string]any{"type": kind, field: text}}}
}

func result(text string, structured any) map[string]any {
	r := map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": text, "total_cost_usd": 0.0056,
		"usage":      map[string]any{"input_tokens": 2, "cache_read_input_tokens": 900, "cache_creation_input_tokens": 100, "output_tokens": 40, "output_tokens_details": map[string]any{"thinking_tokens": 10}},
		"modelUsage": map[string]any{"claude-sonnet-5-5": map[string]any{}}}
	if structured != nil {
		r["structured_output"] = structured
	}
	return r
}

func TestAReplyIsStreamedAndCharged(t *testing.T) {
	c, sent := fakeClaude(t, []any{
		map[string]any{"type": "system", "subtype": "init"},
		delta("text_delta", "text", "Morning, "),
		delta("text_delta", "text", "Ada."),
		result("Morning, Ada.", nil),
	}, "")
	var drafts []string
	got, err := c.Reason(context.Background(), system2.Request{
		Instructions: "You are Kenji.",
		History:      []system2.Turn{{Role: system2.RolePerson, Text: "Hi"}, {Role: system2.RoleAgent, Text: "Hello"}},
		Note:         "It's Tuesday.", Message: "Morning!",
		Draft: func(s string) { drafts = append(drafts, s) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Morning, Ada." || got.CostUSD != 0.0056 || got.Provider != "claude-code/claude-sonnet-5-5" {
		t.Fatalf("got %+v", got)
	}
	if got.Usage != (system2.Usage{InputTokens: 1002, CachedTokens: 900, OutputTokens: 40, ReasoningTokens: 10}) {
		t.Fatalf("usage %+v", got.Usage)
	}
	if !slices.Equal(drafts, []string{"Morning,", "Morning, Ada."}) {
		t.Fatalf("drafts %q", drafts)
	}
	args, stdin := sent()
	for _, want := range [][]string{{"-p"}, {"--safe-mode"}, {"--tools", ""}, {"--model", "claude-sonnet-5-5"}, {"--no-session-persistence"}, {"--include-partial-messages"}} {
		i := slices.Index(args, want[0])
		if i < 0 || (len(want) == 2 && (i+1 >= len(args) || args[i+1] != want[1])) {
			t.Fatalf("args %q lack %q", args, want)
		}
	}
	if sp := args[slices.Index(args, "--system-prompt")+1]; !strings.HasPrefix(sp, "You are Kenji.") || !strings.Contains(sp, "no files, folder or computer") {
		t.Fatalf("system prompt %q", sp)
	}
	for _, want := range []string{"Them: Hi", "You: Hello", "<note>\nIt's Tuesday.", "<message>\nMorning!"} {
		if !strings.Contains(stdin, want) {
			t.Fatalf("stdin %q lacks %q", stdin, want)
		}
	}
}

func TestFactsComeFromTheStructuredOutput(t *testing.T) {
	facts := map[string]any{"reply": "Noted: £40.", "remember": []any{map[string]any{"about": "speaker", "fact": "Free delivery is from £40.", "areas": []any{"work"}}},
		"who_they_are": "", "reminders": []any{}, "promises": []any{}, "permissions": []any{}, "corrections": []any{}, "misread": "", "feedback": []any{},
		"lessons": []any{}, "tastes": []any{}, "send": map[string]any{"work": "", "as": ""}, "moves": []any{}, "act": []any{}, "look": []any{}, "topic": "", "answers": []any{"Yes", "No"}, "many": false}
	c, sent := fakeClaude(t, []any{
		map[string]any{"type": "stream_event", "event": map[string]any{"type": "message_start"}},
		delta("input_json_delta", "partial_json", `{"remember": [], "reply": "Noted`),
		delta("input_json_delta", "partial_json", `: £40."`),
		result("", facts),
	}, "")
	var drafts []string
	got, err := c.Reason(context.Background(), system2.Request{Instructions: "x", Message: "£40 now", CollectFacts: true, Draft: func(s string) { drafts = append(drafts, s) }})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Noted: £40." || len(got.Remember) != 1 || got.Remember[0].Text != "Free delivery is from £40." || len(got.Answers) != 2 {
		t.Fatalf("got %+v", got)
	}
	if !slices.Equal(drafts, []string{"Noted", "Noted: £40."}) {
		t.Fatalf("drafts %q", drafts)
	}
	args, _ := sent()
	if i := slices.Index(args, "--json-schema"); i < 0 || !strings.Contains(args[i+1], `"who_they_are"`) {
		t.Fatalf("no schema in %q", args)
	}
}

func TestAFormatIsTheStructuredOutputAsJSON(t *testing.T) {
	c, _ := fakeClaude(t, []any{result("", map[string]any{"intent": "request", "confidence": 0.9})}, "")
	got, err := c.Reason(context.Background(), system2.Request{Instructions: "x", Message: "y",
		Format: &system2.Format{Name: "a", Schema: map[string]any{"type": "object"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != `{"confidence":0.9,"intent":"request"}` && got.Text != `{"intent":"request","confidence":0.9}` {
		t.Fatalf("text %q", got.Text)
	}
}

func TestAnErrorResultIsAnError(t *testing.T) {
	c, _ := fakeClaude(t, []any{map[string]any{"type": "result", "subtype": "success", "is_error": true, "result": "Not logged in · Please run /login"}}, "")
	if _, err := c.Reason(context.Background(), system2.Request{Instructions: "x", Message: "y"}); err == nil || !strings.Contains(err.Error(), "Not logged in") {
		t.Fatalf("err = %v", err)
	}
}

func TestNoResultSaysWhatItWrote(t *testing.T) {
	c, _ := fakeClaude(t, nil, "something broke")
	if _, err := c.Reason(context.Background(), system2.Request{Instructions: "x", Message: "y"}); err == nil || !strings.Contains(err.Error(), "something broke") {
		t.Fatalf("err = %v", err)
	}
}
