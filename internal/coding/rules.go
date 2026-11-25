package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Rules are what a job may do on its own: read anything and change files in
// its copy, and run the repository's own commands (Commands). Everything else
// is asked of the owner: installing, the internet, anything outside the copy.
type Rules struct {
	Dir      string   // the job's copy
	Commands []string // run as they are, or with more arguments: "go test ./...", "npm test"
}

// reading is the tools that only read, or only plan.
var reading = []string{"Read", "Glob", "Grep", "LS", "NotebookRead", "TodoWrite", "Task", "ExitPlanMode"}

// changing is the tools that change files, and where they say which.
var changing = map[string]string{"Edit": "file_path", "Write": "file_path", "MultiEdit": "file_path", "NotebookEdit": "notebook_path"}

// chained is a command that runs more than one, or runs another inside it.
var chained = regexp.MustCompile("&&|\\|\\||;|\\||`|\\$\\(|>|<|\n")

// Allows is whether the rules let a job use tool with input on its own, and
// why, in a few words, for the job's record or the owner's card.
func (r Rules) Allows(tool string, input map[string]any) (bool, string) {
	switch {
	case slices.Contains(reading, tool):
		if p := pathIn(input); p != "" && !r.inside(p) {
			return false, "it's outside the copy"
		}
		return true, "reading"
	case changing[tool] != "":
		p, _ := input[changing[tool]].(string)
		if p == "" || !r.inside(p) {
			return false, "it's outside the copy"
		}
		return true, "a change inside the copy"
	case tool == "Bash":
		c, _ := input["command"].(string)
		c = strings.TrimSpace(strings.ReplaceAll(c, "2>&1", "")) // its errors with its output: still one command
		if chained.MatchString(c) {
			return false, "it runs more than one command"
		}
		for _, allowed := range r.Commands {
			if c == allowed || strings.HasPrefix(c, allowed+" ") {
				return true, "one of the repository's own commands"
			}
		}
		return false, "a command the job doesn't run on its own"
	case tool == "WebFetch" || tool == "WebSearch":
		return false, "it reaches the internet"
	}
	return false, "not in the job's rules"
}

func pathIn(input map[string]any) string {
	for _, k := range []string{"file_path", "path", "notebook_path"} {
		if p, ok := input[k].(string); ok && p != "" {
			return p
		}
	}
	return ""
}

// inside is whether p is in the copy, symbolic links followed where they can be.
func (r Rules) inside(p string) bool {
	if r.Dir == "" {
		return false
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(r.Dir, p)
	}
	dir := real(filepath.Clean(r.Dir))
	p = real(filepath.Clean(p))
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// real is p with its symbolic links followed, as far as it exists.
func real(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if parent := filepath.Dir(p); parent != p {
		return filepath.Join(real(parent), filepath.Base(p))
	}
	return p
}

// Commands are a repository's own test, lint and build commands, read from its
// package.json, go.mod, Makefile, Cargo.toml and pyproject.toml.
func Commands(dir string) []string {
	var out []string
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	if b, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		json.Unmarshal(b, &pkg)
		run := "npm"
		switch {
		case has("pnpm-lock.yaml"):
			run = "pnpm"
		case has("yarn.lock"):
			run = "yarn"
		case has("bun.lockb"), has("bun.lock"):
			run = "bun"
		}
		for _, s := range []string{"test", "lint", "build", "typecheck", "check", "format"} {
			if _, ok := pkg.Scripts[s]; ok {
				if s == "test" {
					out = append(out, run+" test")
				}
				out = append(out, run+" run "+s)
			}
		}
	}
	if has("go.mod") {
		out = append(out, "go test", "go vet", "go build", "gofmt")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "Makefile")); err == nil {
		for _, t := range []string{"test", "lint", "build", "check"} {
			if regexp.MustCompile(`(?m)^` + t + `\s*:`).Match(b) {
				out = append(out, "make "+t)
			}
		}
	}
	if has("Cargo.toml") {
		out = append(out, "cargo test", "cargo build", "cargo check", "cargo clippy", "cargo fmt")
	}
	if has("pyproject.toml") || has("pytest.ini") {
		out = append(out, "pytest", "python -m pytest")
	}
	return out
}

// A Gate answers a job's asks: by its rules, by what the owner allowed for this
// job, or not at all, when the owner has to.
type Gate struct {
	Rules Rules
	ok    map[string]bool // what the owner allowed for the rest of the job
}

// key is what "for this job" allows again: the same command, or the same tool
// on the same file.
func key(tool string, input map[string]any) string {
	if c, ok := input["command"].(string); ok {
		return tool + " " + strings.TrimSpace(c)
	}
	return tool + " " + pathIn(input)
}

// Decide is the answer to an Ask, if the gate can give it: ok is whether it
// may, decided whether it's answered here; when not, the owner is asked.
func (g *Gate) Decide(e Event) (ok, decided bool, why string) {
	if allow, why := g.Rules.Allows(e.Tool, e.Input); allow {
		return true, true, why
	}
	if g.ok[key(e.Tool, e.Input)] {
		return true, true, "you allowed it for this job"
	}
	return false, false, ""
}

// AllowForJob remembers the owner's "allow for this job" for e.
func (g *Gate) AllowForJob(e Event) {
	if g.ok == nil {
		g.ok = map[string]bool{}
	}
	g.ok[key(e.Tool, e.Input)] = true
}
