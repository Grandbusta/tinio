// Package clis finds the coding CLIs on this computer (Claude Code, OpenCode,
// Codex): where each is, its version, and whether it's signed in. It asks each
// CLI, and never reads a CLI's own sign-in files.
package clis

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// The CLIs Tinio knows, by the name kept in a model choice ("claude:...").
const (
	Claude   = "claude"
	OpenCode = "opencode"
	Codex    = "codex"
)

// A CLI is one coding CLI as found on this computer.
type CLI struct {
	ID        string `json:"id"`        // Claude, OpenCode or Codex
	Name      string `json:"name"`      // as shown: "Claude Code"
	Path      string `json:"-"`         // the program; "" when it isn't found
	Installed bool   `json:"installed"` //
	Version   string `json:"version"`   // as it says, e.g. "2.1.331"
	SignedIn  bool   `json:"signed_in"` // false when unknown too
	Account   string `json:"account"`   // the email it's signed in with, when it says
	Plan      string `json:"plan"`      // e.g. "max", when it says
	// Usable is whether Tinio can think with it today: only Claude Code so far.
	Usable bool `json:"usable"`
}

var names = map[string]string{Claude: "Claude Code", OpenCode: "OpenCode", Codex: "Codex"}

// Finder finds the CLIs. Its zero value looks on PATH and in the usual places.
type Finder struct {
	// Dirs are looked in after PATH; nil: the usual places for this system.
	Dirs []string
	// Timeout for each question asked of a CLI; 0: 10 seconds.
	Timeout time.Duration

	mu    sync.Mutex
	found []CLI
	at    time.Time
}

// usualDirs are where the CLIs install themselves, which an app started from
// the Dock or Start menu doesn't have on its PATH.
func usualDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".claude", "local"),
		filepath.Join(home, ".opencode", "bin"),
		filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, "bin"),
	}
	switch runtime.GOOS {
	case "darwin":
		dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
	case "windows":
		if app := os.Getenv("APPDATA"); app != "" {
			dirs = append(dirs, filepath.Join(app, "npm"))
		}
	default:
		dirs = append(dirs, "/usr/local/bin", "/usr/bin")
	}
	return dirs
}

// Path is where program is: on PATH, or in one of dirs; "" if not found.
func (f *Finder) Path(program string) string {
	if p, err := exec.LookPath(program); err == nil {
		return p
	}
	dirs := f.Dirs
	if dirs == nil {
		dirs = usualDirs()
	}
	for _, d := range dirs {
		for _, name := range []string{program, program + ".exe", program + ".cmd"} {
			p := filepath.Join(d, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}

// All is every CLI Tinio knows, found or not, asked again at most once a
// minute.
func (f *Finder) All(ctx context.Context) []CLI {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.found != nil && time.Since(f.at) < time.Minute {
		return f.found
	}
	out := make([]CLI, 3)
	var wg sync.WaitGroup
	for i, id := range []string{Claude, OpenCode, Codex} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = f.find(ctx, id)
		}()
	}
	wg.Wait()
	f.found, f.at = out, time.Now()
	return out
}

// Forget makes the next All ask again: after signing in, say.
func (f *Finder) Forget() {
	f.mu.Lock()
	f.found = nil
	f.mu.Unlock()
}

// Get is the CLI by id, as All found it.
func (f *Finder) Get(ctx context.Context, id string) (CLI, bool) {
	for _, c := range f.All(ctx) {
		if c.ID == id {
			return c, c.Installed
		}
	}
	return CLI{}, false
}

func (f *Finder) find(ctx context.Context, id string) CLI {
	c := CLI{ID: id, Name: names[id], Path: f.Path(id)}
	if c.Path == "" {
		return c
	}
	c.Installed = true
	if out, err := f.run(ctx, c.Path, "--version"); err == nil {
		c.Version = version(out)
	}
	switch id {
	case Claude:
		// `claude auth status --json` says whether it's signed in, as whom and
		// on which plan, without using any of the plan.
		if out, err := f.run(ctx, c.Path, "auth", "status", "--json"); err == nil {
			var st struct {
				LoggedIn         bool   `json:"loggedIn"`
				Email            string `json:"email"`
				SubscriptionType string `json:"subscriptionType"`
			}
			if json.Unmarshal([]byte(out), &st) == nil {
				c.SignedIn, c.Account, c.Plan = st.LoggedIn, st.Email, st.SubscriptionType
			}
		}
		c.Usable = c.SignedIn
	}
	return c
}

// version is the first word of out that starts with a digit: "2.1.331 (Claude
// Code)" is 2.1.331.
func version(out string) string {
	for _, w := range strings.Fields(out) {
		if w != "" && w[0] >= '0' && w[0] <= '9' {
			return strings.TrimPrefix(w, "v")
		}
	}
	return strings.TrimSpace(out)
}

func (f *Finder) run(ctx context.Context, program string, args ...string) (string, error) {
	t := f.Timeout
	if t == 0 {
		t = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, t)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = Env(os.Environ(), program)
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", errors.New("it didn't answer in time")
	}
	return string(out), err
}

// Env is the owner's environment for a CLI, without Tinio's own keys and
// settings, with the CLI's own folder on PATH (a CLI may start helpers beside
// itself).
func Env(environ []string, program string) []string {
	var out []string
	path := ""
	for _, e := range environ {
		name, value, _ := strings.Cut(e, "=")
		switch {
		case strings.HasPrefix(name, "TINIO_"), strings.HasPrefix(name, "OPENAI_"), name == "MAX_AGENTS", name == "PASSCODE":
			continue
		case name == "PATH":
			path = value
			continue
		}
		out = append(out, e)
	}
	dirs := []string{filepath.Dir(program)}
	if path != "" {
		dirs = append(dirs, path)
	}
	return append(out, "PATH="+strings.Join(dirs, string(os.PathListSeparator)))
}

// A GitHub is the `gh` on this computer, which pushes and opens pull requests
// for coding jobs, as the owner: Tinio holds no GitHub token.
type GitHub struct {
	Path      string `json:"-"`
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	SignedIn  bool   `json:"signed_in"`
	Account   string `json:"account"` // their GitHub login, when it says
}

// account is the login in `gh auth status`'s "Logged in to github.com account X".
var account = regexp.MustCompile(`account (\S+)`)

// GH is the `gh` on this computer, asked whether it's signed in to github.com.
func (f *Finder) GH(ctx context.Context) GitHub {
	g := GitHub{Path: f.Path("gh")}
	if g.Path == "" {
		return g
	}
	g.Installed = true
	if out, err := f.run(ctx, g.Path, "--version"); err == nil {
		g.Version = version(out)
	}
	// It says on stderr, and exits 1 when signed out.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, g.Path, "auth", "status", "--hostname", "github.com")
	cmd.Env = Env(os.Environ(), g.Path)
	out, err := cmd.CombinedOutput()
	if err == nil {
		g.SignedIn = true
		if m := account.FindSubmatch(out); m != nil {
			g.Account = string(m[1])
		}
	}
	return g
}
