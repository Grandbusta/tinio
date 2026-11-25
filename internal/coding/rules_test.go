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
		{"Bash", map[string]any{"command": "go test ./... | tail"}, true},
		{"Bash", map[string]any{"command": "go test -v ./... 2>&1 | tail -20"}, true},
		{"Bash", map[string]any{"command": "go vet ./... && echo vet ok; gofmt -l .; echo \"exit $?\""}, true},
		{"Bash", map[string]any{"command": "CGO_ENABLED=0 go test ./..."}, true},
		{"Bash", map[string]any{"command": "PATH=/tmp/evil:$PATH go test ./..."}, false},
		{"Bash", map[string]any{"command": "cd " + in("pkg") + " && go test ./..."}, true},
		{"Bash", map[string]any{"command": "cd /etc && cat passwd"}, false},
		{"Bash", map[string]any{"command": "grep -rn \"Draft\" ."}, true},
		{"Bash", map[string]any{"command": "grep -rn root /etc"}, false},
		{"Bash", map[string]any{"command": "find . -name '*.go'"}, true},
		{"Bash", map[string]any{"command": "find . -name '*.go' -delete"}, false},
		{"Bash", map[string]any{"command": "go test ./... 2>/dev/null"}, true},
		{"Bash", map[string]any{"command": "go test ./... > out.txt"}, false},
		{"Bash", map[string]any{"command": "go test ./... | sh"}, false},
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
	// A command that only works allowed once for the job runs again with other arguments.
	lint := Event{Kind: Ask, Tool: "Bash", Input: map[string]any{"command": "golangci-lint run ./... && make lint-fix"}}
	g.AllowForJob(lint)
	again := Event{Kind: Ask, Tool: "Bash", Input: map[string]any{"command": "make lint-fix && golangci-lint run --fix ./basket/... | tail -5"}}
	if ok, decided, _ := g.Decide(again); !ok || !decided {
		t.Fatal("the same commands with other arguments were asked again")
	}
	script := Event{Kind: Ask, Tool: "Bash", Input: map[string]any{"command": "python3 scripts/check.py"}}
	g.AllowForJob(script)
	if _, decided, _ := g.Decide(Event{Kind: Ask, Tool: "Bash", Input: map[string]any{"command": "python3 -c 'import os'"}}); decided {
		t.Fatal("allowing one script allowed any python")
	}
}
