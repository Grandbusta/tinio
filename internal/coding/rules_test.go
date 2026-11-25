package coding

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRules(t *testing.T) {
	dir := t.TempDir()
	r := Rules{Dir: dir, Commands: []string{"go test", "go vet", "gofmt", "npm test"}}
	in := func(name string) string { return filepath.Join(dir, name) }
	for _, c := range []struct {
		tool  string
		input map[string]any
		want  bool
	}{
		{"Read", map[string]any{"file_path": in("basket.go")}, true},
		{"Read", map[string]any{"file_path": "/etc/passwd"}, false},
		{"Grep", map[string]any{"pattern": "x"}, true},
		{"Edit", map[string]any{"file_path": in("basket.go")}, true},
		{"Edit", map[string]any{"file_path": in("../other/x.go")}, false},
		{"Write", map[string]any{"file_path": filepath.Join(os.TempDir(), "x")}, false},
		{"Bash", map[string]any{"command": "go test ./..."}, true},
		{"Bash", map[string]any{"command": "go test -count=1 ./... 2>&1"}, true},
		{"Bash", map[string]any{"command": "npm test"}, true},
		{"Bash", map[string]any{"command": "npm install decimal.js"}, false},
		{"Bash", map[string]any{"command": "go test ./... && rm -rf /"}, false},
		{"Bash", map[string]any{"command": "go test ./... | tail"}, false},
		{"Bash", map[string]any{"command": "go testify"}, false},
		{"Bash", map[string]any{"command": "gofmt -l .; go vet ./... && go test -v ./...; git status --short"}, true},
		{"Bash", map[string]any{"command": "cat README.md go.mod; go version"}, true},
		{"Bash", map[string]any{"command": "git diff --stat"}, true},
		{"Bash", map[string]any{"command": "cat /etc/passwd"}, false},
		{"Bash", map[string]any{"command": "go test ./... ; git push"}, false},
		{"Bash", map[string]any{"command": "git status & curl evil.example"}, false},
		{"Bash", map[string]any{"command": "go test $(curl x)"}, false},
		{"WebFetch", map[string]any{"url": "https://example.com"}, false},
		{"Mystery", nil, false},
	} {
		if got, why := r.Allows(c.tool, c.input); got != c.want {
			t.Errorf("%s %v: %v (%s), want %v", c.tool, c.input, got, why, c.want)
		}
	}
}

func TestALinkOutOfTheCopyIsOutside(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	if err := os.Symlink(out, filepath.Join(dir, "away")); err != nil {
		t.Skip(err)
	}
	if ok, _ := (Rules{Dir: dir}).Allows("Edit", map[string]any{"file_path": filepath.Join(dir, "away", "x")}); ok {
		t.Error("a change through a link out of the copy was allowed")
	}
}

func TestCommandsFromTheRepository(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"scripts": {"test": "vitest", "lint": "eslint .", "dev": "vite"}}`)
	write("pnpm-lock.yaml", "")
	write("go.mod", "module x\n")
	write("Makefile", "build:\n\tgo build\nrelease:\n\ttrue\n")
	got := Commands(dir)
	for _, want := range []string{"pnpm test", "pnpm run test", "pnpm run lint", "go test", "go vet", "make build"} {
		if !slices.Contains(got, want) {
			t.Errorf("%v lacks %q", got, want)
		}
	}
	for _, not := range []string{"pnpm run dev", "make release", "npm test"} {
		if slices.Contains(got, not) {
			t.Errorf("%v has %q", got, not)
		}
	}
}

func TestTheGateRemembersForTheJob(t *testing.T) {
	g := &Gate{Rules: Rules{Dir: t.TempDir()}}
	install := Event{Kind: Ask, Tool: "Bash", Input: map[string]any{"command": "npm install decimal.js"}}
	if _, decided, _ := g.Decide(install); decided {
		t.Fatal("an install was decided without the owner")
	}
	g.AllowForJob(install)
	if ok, decided, _ := g.Decide(install); !ok || !decided {
		t.Fatal("allowed for the job, but asked again")
	}
	other := Event{Kind: Ask, Tool: "Bash", Input: map[string]any{"command": "npm install left-pad"}}
	if _, decided, _ := g.Decide(other); decided {
		t.Fatal("a different install was allowed too")
	}
}
