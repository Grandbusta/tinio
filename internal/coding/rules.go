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
	Allowed  []string // what the owner allowed for this job, the same way (Gate.AllowForJob)
}

// reading is the tools that only read, or only plan.
var reading = []string{"Read", "Glob", "Grep", "LS", "NotebookRead", "TodoWrite", "Task", "ExitPlanMode"}

// changing is the tools that change files, and where they say which.
var changing = map[string]string{"Edit": "file_path", "Write": "file_path", "MultiEdit": "file_path", "NotebookEdit": "notebook_path"}

// sequence splits a chain into its commands: run one after another, on the
// last one's success or failure, or piped one into the next.
var sequence = regexp.MustCompile(`&&|\|\||\||;|\n`)

// quiet is output sent nowhere, or errors sent with the output: still one command.
var quiet = regexp.MustCompile(`\s*(?:[12&]?>\s*/dev/null|2>&1)`)

// chained is a command that redirects, runs another inside it, or runs in the
// background.
var chained = regexp.MustCompile("`|\\$\\(|>|<|&")

// readOnly are commands any job may run: they only look, or only say something.
var readOnly = []string{"git status", "git diff", "git log", "git show", "git branch", "git rev-parse", "git ls-files", "git blame",
	"pwd", "go version", "node --version", "echo", "printf", "true", "false", "which", "command -v", "type", "date", "sleep", "test", "["}

// looking are commands that only read the files they're given, all of which must
// be in the copy.
var looking = []string{"cat", "head", "tail", "wc", "ls", "tree", "stat", "file", "diff", "sort", "uniq", "cut", "tr", "grep", "rg", "find", "basename", "dirname", "cd"}

// runs is whether a single command is one the job runs on its own.
func (r Rules) runs(c string) bool {
	for _, allowed := range append(append(append([]string{}, r.Commands...), r.Allowed...), readOnly...) {
		if c == allowed || strings.HasPrefix(c, allowed+" ") {
			return true
		}
	}
	for _, allowed := range looking {
		if c == allowed || strings.HasPrefix(c, allowed+" ") {
			if allowed == "find" && regexp.MustCompile(`\s-(exec|execdir|ok|delete|fprint)`).MatchString(c) {
				return false // find that runs or deletes
			}
			return r.argsInside(c)
		}
	}
	return false
}

// argsInside is whether every path c names is inside the copy. A word without a
// slash is a pattern or a name in the copy; one with a slash, or ~, is a path.
func (r Rules) argsInside(c string) bool {
	for _, f := range strings.Fields(c)[1:] {
		f = strings.Trim(f, `"'`)
		if strings.HasPrefix(f, "-") {
			continue
		}
		if (strings.ContainsAny(f, "/~") || f == "..") && !r.inside(f) {
			return false
		}
	}
	return true
}

// unset are the variables a command may not be given in front of it: they'd
// change what runs.
var unset = regexp.MustCompile(`^(PATH|LD_\w*|DYLD_\w*|NODE_OPTIONS|BASH_ENV|ENV)=`)

// bare is c without the variables set in front of it ("CGO_ENABLED=0 go test"),
// or "" when one of them would change what runs.
func bare(c string) string {
	for {
		f := strings.Fields(c)
		if len(f) < 2 || !regexp.MustCompile(`^[A-Za-z_]\w*=\S*$`).MatchString(f[0]) {
			return c
		}
		if unset.MatchString(f[0]) {
			return ""
		}
		c = strings.TrimSpace(strings.TrimPrefix(c, f[0]))
	}
}

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
		c = quiet.ReplaceAllString(c, "")
		// A chain of commands is allowed when each one is: "gofmt -l .; go test ./... | tail -20".
		parts := sequence.Split(c, -1)
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if chained.MatchString(part) {
				return false, "it redirects, runs in the background or runs a command inside another"
			}
			if part = bare(part); part == "" {
				return false, "it changes what runs, with PATH or the like"
			}
			if !r.runs(part) {
				if len(parts) > 1 {
					return false, "one of its commands isn't one the job runs on its own"
				}
				return false, "a command the job doesn't run on its own"
			}
		}
		if len(parts) > 1 {
			return true, "each of its commands is one the job runs on its own"
		}
		return true, "one of the repository's own commands"
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

// AllowForJob remembers the owner's "allow for this job" for e. For a command,
// each command in it is allowed again with other arguments ("make lint-fix",
// "golangci-lint run"), unless it installs, removes, sends or reaches out:
// those only as they were.
func (g *Gate) AllowForJob(e Event) {
	if g.ok == nil {
		g.ok = map[string]bool{}
	}
	g.ok[key(e.Tool, e.Input)] = true
	c, _ := e.Input["command"].(string)
	if e.Tool != "Bash" || c == "" {
		return
	}
	for _, part := range sequence.Split(quiet.ReplaceAllString(c, ""), -1) {
		part = bare(strings.TrimSpace(part))
		if part == "" || chained.MatchString(part) {
			continue
		}
		g.Rules.Allowed = append(g.Rules.Allowed, head(part))
	}
}

// risky are the words after which a command is allowed again only as it was.
var risky = regexp.MustCompile(`^(install|i|add|remove|rm|uninstall|publish|push|deploy|exec|x|dlx)$`)

// head is what of command part "allow for this job" allows again: the program
// and its subcommand ("go generate", "make lint"), or the whole of it when it
// installs, removes, sends or reaches out, or the program is one that does.
func head(part string) string {
	f := strings.Fields(part)
	if len(f) == 0 || regexp.MustCompile(`^(curl|wget|ssh|scp|rsync|sudo|rm|mv|chmod|chown|git|npx|bunx|pip|pipx|brew|apt|docker|kubectl|python3?|node|bash|sh|zsh|ruby|perl|php|deno|bun|eval|xargs|env)$`).MatchString(f[0]) {
		return part
	}
	if len(f) > 1 && !strings.HasPrefix(f[1], "-") && !strings.ContainsAny(f[1], "/.") {
		if risky.MatchString(f[1]) {
			return part
		}
		return f[0] + " " + f[1]
	}
	return f[0]
}
