package clis

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// fake writes a program called name in dir that prints out for each set of
// arguments it's given (joined with spaces), and nothing otherwise.
func fake(t *testing.T, dir, name string, out map[string]string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fakes are shell scripts")
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\ncase \"$*\" in\n")
	for args, text := range out {
		b.WriteString("  \"" + args + "\") printf '%s\\n' '" + text + "' ;;\n") // printf: PATH may be empty
	}
	b.WriteString("esac\n")
	if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFindsClaudeSignedIn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	fake(t, dir, "claude", map[string]string{
		"--version":          "2.1.331 (Claude Code)",
		"auth status --json": `{"loggedIn": true, "email": "ada@example.com", "subscriptionType": "max"}`,
	})
	fake(t, dir, "opencode", map[string]string{"--version": "1.18.30"})
	f := &Finder{Dirs: []string{dir}}
	all := f.All(context.Background())
	if len(all) != 3 {
		t.Fatalf("want 3 CLIs, have %d", len(all))
	}
	c := all[0]
	if c.ID != Claude || !c.Installed || c.Version != "2.1.331" || !c.SignedIn || c.Account != "ada@example.com" || c.Plan != "max" || !c.Usable {
		t.Fatalf("claude = %+v", c)
	}
	if o := all[1]; !o.Installed || o.Version != "1.18.30" || o.Usable {
		t.Fatalf("opencode = %+v", o)
	}
	if x := all[2]; x.Installed {
		t.Fatalf("codex isn't there, have %+v", x)
	}
}

func TestClaudeSignedOutIsNotUsable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	fake(t, dir, "claude", map[string]string{"--version": "2.1.331", "auth status --json": `{"loggedIn": false}`})
	c, ok := (&Finder{Dirs: []string{dir}}).Get(context.Background(), Claude)
	if !ok || c.SignedIn || c.Usable {
		t.Fatalf("claude = %+v", c)
	}
}

func TestEnvLeavesOutTiniosOwn(t *testing.T) {
	env := Env([]string{"HOME=/h", "TINIO_KEY=k", "OPENAI_API_KEY=sk", "PATH=/usr/bin"}, "/opt/x/claude")
	if slices.ContainsFunc(env, func(e string) bool { return strings.HasPrefix(e, "TINIO_") || strings.HasPrefix(e, "OPENAI_") }) {
		t.Fatalf("Tinio's own leaked: %v", env)
	}
	if !slices.Contains(env, "HOME=/h") || !slices.Contains(env, "PATH=/opt/x"+string(os.PathListSeparator)+"/usr/bin") {
		t.Fatalf("env = %v", env)
	}
}
