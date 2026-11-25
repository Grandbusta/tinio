package coding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeClaude is a `claude` that plays a job: it says what it's doing, asks
// four things, notes how each was answered, finishes, and finishes again if
// it's sent more.
func fakeClaude(t *testing.T, repo string) (prog, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake is a shell script")
	}
	dir = t.TempDir()
	ask := func(id, tool, input string) string {
		return fmt.Sprintf(`echo '{"type":"control_request","request_id":"%s","request":{"subtype":"can_use_tool","tool_name":"%s","input":%s}}'`+"\nread -r a_%s\n", id, tool, input, id)
	}
	edit := `{"file_path":"` + repo + `/basket.go"}`
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + dir + "/args\"\nread -r brief\n" +
		`echo '{"type":"system","subtype":"init","session_id":"s-1"}'` + "\n" +
		`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Looking at the basket."},{"type":"tool_use","name":"Edit","input":` + edit + `}]}}'` + "\n" +
		ask("r1", "Edit", edit) +
		ask("r2", "Bash", `{"command":"npm install decimal.js"}`) +
		ask("r3", "Bash", `{"command":"npm install decimal.js"}`) +
		ask("r4", "WebFetch", `{"url":"https://example.com"}`) +
		`for x in "$a_r1" "$a_r2" "$a_r3" "$a_r4"; do case "$x" in *'"behavior":"allow"'*) printf 'allow ';; *'"behavior":"deny"'*) printf 'deny ';; *) printf '? ';; esac; done > "` + dir + `/answers"` + "\n" +
		`case "$a_r4" in *"Ada said no"*) printf yes > "` + dir + `/why";; esac` + "\n" +
		`echo '{"type":"result","subtype":"success","is_error":false,"result":"Fixed it.","total_cost_usd":0.15,"num_turns":5}'` + "\n" +
		"read -r more\n" +
		`[ -n "$more" ] && echo '{"type":"result","subtype":"success","is_error":false,"result":"Added the test.","total_cost_usd":0.05,"num_turns":2}'` + "\n" +
		"read -r rest\n"
	prog = filepath.Join(dir, "claude")
	if err := os.WriteFile(prog, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return prog, dir
}

// A job runs to the end: the rules answer what they allow, the owner the rest,
// once for a single thing and "for this job" for another, which isn't asked
// again; then it's sent more and finishes again.
func TestAJobAnsweredBothWays(t *testing.T) {
	repo := t.TempDir()
	prog, dir := fakeClaude(t, repo)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := Claude{Program: prog}.Start(ctx, Job{Dir: repo, Brief: "Fix the basket rounding.", Model: "claude-opus-5-5", MaxTurns: 80})
	if err != nil {
		t.Fatal(err)
	}
	gate := &Gate{Rules: Rules{Dir: repo, Commands: []string{"npm test"}}}
	var (
		session, said, changed string
		ownerAsked             []string
		done                   []Event
	)
	for e := range s.Events() {
		switch e.Kind {
		case Started:
			session = e.SessionID
		case Said:
			said = e.Text
		case Changed:
			changed = e.Path
		case Ask:
			ok, decided, why := gate.Decide(e)
			if !decided {
				ownerAsked = append(ownerAsked, e.Tool)
				switch e.Tool {
				case "Bash": // the owner: allow for this job
					gate.AllowForJob(e)
					ok = true
				default: // the owner: no
					ok, why = false, "Ada said no: use the docs in the repository."
				}
			}
			if err := s.Answer(e.AskID, ok, why, false); err != nil {
				t.Fatal(err)
			}
		case Done:
			done = append(done, e)
			if len(done) == 1 {
				if err := s.Send("Also add a test for 15% off £13.30."); err != nil {
					t.Fatal(err)
				}
			} else {
				go s.Close()
			}
		}
	}

	if session != "s-1" || said != "Looking at the basket." || changed != repo+"/basket.go" {
		t.Errorf("session %q, said %q, changed %q", session, said, changed)
	}
	if !slices.Equal(ownerAsked, []string{"Bash", "WebFetch"}) {
		t.Errorf("the owner was asked about %v; want the first install and the fetch", ownerAsked)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "answers")); string(b) != "allow allow allow deny " {
		t.Errorf("answers %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "why")); string(b) != "yes" {
		t.Error("the no didn't carry the owner's reason")
	}
	if len(done) != 2 || done[0].Status != Completed || done[0].Text != "Fixed it." || done[0].CostUSD != 0.15 || done[1].Text != "Added the test." {
		t.Errorf("done %+v", done)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	for _, want := range []string{"--setting-sources\nproject\n", "--strict-mcp-config\n", "--permission-prompt-tool\nstdio\n", "--model\nclaude-opus-5-5\n", "--max-turns\n80\n"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("args lack %q:\n%s", want, args)
		}
	}
	if strings.Contains(string(args), "--safe-mode") {
		t.Error("a job runs in safe mode: the repository's CLAUDE.md would be left out")
	}
}

// A CLI that ends without a result is a failed job, with what it said.
func TestACLIThatDiesFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake is a shell script")
	}
	dir := t.TempDir()
	prog := filepath.Join(dir, "claude")
	os.WriteFile(prog, []byte("#!/bin/sh\nread -r brief\necho 'Not logged in' >&2\nexit 1\n"), 0o755)
	s, err := Claude{Program: prog}.Start(context.Background(), Job{Dir: dir, Brief: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var last Event
	for e := range s.Events() {
		last = e
	}
	if last.Kind != Done || last.Status != Failed || !strings.Contains(last.Text, "Not logged in") {
		t.Fatalf("last %+v", last)
	}
}
