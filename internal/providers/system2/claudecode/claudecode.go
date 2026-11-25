// Package claudecode is the System 2 provider that thinks with the owner's own
// Claude Code: the `claude` they installed and signed into, on their own plan.
//
// Each call is one `claude -p` in safe mode (their CLAUDE.md, hooks, plugins
// and MCP servers left out, their sign-in kept), from an empty folder, with
// Tinio's instructions as the whole system prompt and no tools: Claude Code
// is only the model. The conversation goes in on stdin; it keeps no session.
package claudecode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Grandbusta/tinio/internal/clis"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

const requestTimeout = 180 * time.Second

// calls is how many `claude` run at once, across every agent: each is a
// process, and the owner's plan has a rate of its own.
var calls = make(chan struct{}, 4)

// Models are Claude Code's models Tinio offers, by id, with their names.
var Models = []struct{ ID, Name string }{
	{"claude-opus-5-5", "Opus 5.5"},
	{"claude-sonnet-5-5", "Sonnet 5.5"},
	{"claude-haiku-5-5", "Haiku 5.5"},
}

// ModelName is a model's name as shown ("Sonnet 5.5"); id itself if unknown.
func ModelName(id string) string {
	for _, m := range Models {
		if m.ID == id {
			return m.Name
		}
	}
	return id
}

type Client struct {
	Program string // the `claude` to run
	Model   string // e.g. claude-sonnet-5-5
	Effort  string // "low" ... "max"; "": the model's own
	Dir     string // an empty folder to run in
}

// noFolder is added to every prompt: Claude Code tells the model about the
// folder it runs in, and an agent talking with its owner has none.
const noFolder = "\n\nYou are talking with them in a chat app: you have no files, folder or computer open here, so never mention a working directory, an environment or tools you don't have."

// prompt lays the conversation out as text: what was said before, the note,
// then the message to answer (or the cue to speak first).
func prompt(req system2.Request) string {
	var b strings.Builder
	if len(req.History) > 0 {
		b.WriteString("<conversation>\n")
		for _, t := range req.History {
			who := "Them"
			if t.Role == system2.RoleAgent {
				who = "You"
			}
			b.WriteString(who + ": " + t.Text + "\n")
		}
		b.WriteString("</conversation>\n\n")
	}
	if req.Note != "" {
		b.WriteString("<note>\n" + req.Note + "\n</note>\n\n")
	}
	if req.Message != "" {
		b.WriteString("<message>\n" + req.Message + "\n</message>\n")
	} else if b.Len() == 0 {
		b.WriteString("Write it now.")
	}
	return b.String()
}

func (c *Client) args(req system2.Request) []string {
	args := []string{"-p", "--safe-mode", "--output-format", "stream-json", "--verbose",
		"--system-prompt", req.Instructions + noFolder,
		"--tools", "", "--strict-mcp-config", "--no-session-persistence", "--model", c.Model}
	if c.Effort != "" {
		args = append(args, "--effort", c.Effort)
	}
	if req.Draft != nil && req.Format == nil {
		args = append(args, "--include-partial-messages")
	}
	switch {
	case req.CollectFacts:
		args = append(args, "--json-schema", mustJSON(system2.ReplyWithFacts))
	case req.Format != nil:
		args = append(args, "--json-schema", mustJSON(req.Format.Schema))
	}
	return args
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// line is a line of stream-json: only what's read here.
type line struct {
	Type       string          `json:"type"`
	Subtype    string          `json:"subtype"`
	IsError    bool            `json:"is_error"`
	Result     string          `json:"result"`
	Structured json.RawMessage `json:"structured_output"`
	Cost       float64         `json:"total_cost_usd"`
	Usage      struct {
		Input       int `json:"input_tokens"`
		CacheRead   int `json:"cache_read_input_tokens"`
		CacheCreate int `json:"cache_creation_input_tokens"`
		Output      int `json:"output_tokens"`
		Details     struct {
			Thinking int `json:"thinking_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
	ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	Event      struct {
		Type         string `json:"type"`
		ContentBlock struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"content_block"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
	} `json:"event"`
}

func (c *Client) Reason(ctx context.Context, req system2.Request) (system2.Response, error) {
	spent := system2.Response{Provider: "claude-code/" + c.Model}
	select {
	case calls <- struct{}{}:
		defer func() { <-calls }()
	case <-ctx.Done():
		return spent, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Program, c.args(req)...)
	cmd.Dir = c.Dir
	cmd.Env = clis.Env(os.Environ(), c.Program)
	cmd.Stdin = strings.NewReader(prompt(req))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return spent, err
	}
	if err := cmd.Start(); err != nil {
		return spent, fmt.Errorf("claude code: %w", err)
	}

	structured := req.CollectFacts || req.Format != nil
	var (
		result  *line
		written strings.Builder // this turn's text, or its structured output's JSON
		shown   string
	)
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 32<<20)
	for sc.Scan() {
		var l line
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		switch l.Type {
		case "result":
			result = &l
		case "stream_event":
			if req.Draft == nil {
				continue
			}
			switch ev := l.Event; {
			case ev.Type == "message_start":
				written.Reset() // a turn after a tool's answer starts over
			case ev.Delta.Type == "text_delta" && !structured:
				written.WriteString(ev.Delta.Text)
			case ev.Delta.Type == "input_json_delta" && structured:
				written.WriteString(ev.Delta.PartialJSON)
			default:
				continue
			}
			so := written.String()
			if req.CollectFacts {
				so = system2.ReplySoFar(so)
			}
			if so = strings.TrimSpace(so); so != "" && so != shown && !(structured && !req.CollectFacts) {
				shown = so
				req.Draft(so)
			}
		}
	}
	waitErr := cmd.Wait()
	if result == nil {
		why := strings.TrimSpace(lastLines(stderr.String(), 3))
		if ctx.Err() != nil {
			why = "it took too long"
		}
		if why == "" && waitErr != nil {
			why = waitErr.Error()
		}
		return spent, fmt.Errorf("claude code: no answer: %s", why)
	}
	spent.Usage = system2.Usage{
		InputTokens:     result.Usage.Input + result.Usage.CacheRead + result.Usage.CacheCreate,
		CachedTokens:    result.Usage.CacheRead,
		OutputTokens:    result.Usage.Output,
		ReasoningTokens: result.Usage.Details.Thinking,
	}
	spent.CostUSD = result.Cost // what it would cost on a key: on a plan, model time
	for m := range result.ModelUsage {
		spent.Provider = "claude-code/" + m
	}
	if result.IsError || result.Subtype != "success" {
		return spent, fmt.Errorf("claude code: %s: %s", result.Subtype, strings.TrimSpace(result.Result))
	}

	text := strings.TrimSpace(result.Result)
	if structured {
		if len(result.Structured) == 0 || string(result.Structured) == "null" {
			return spent, errors.New("claude code: no answer in the shape asked for")
		}
		text = string(result.Structured)
	}
	got := system2.Response{Text: text}
	if req.CollectFacts {
		if got, err = system2.ParseFacts(text); err != nil {
			return spent, fmt.Errorf("claude code: %w", err)
		}
	}
	got.Provider, got.Usage, got.CostUSD = spent.Provider, spent.Usage, spent.CostUSD
	if len(got.Looks) > 0 {
		return system2.Response{Provider: got.Provider, Looks: got.Looks, Usage: got.Usage, CostUSD: got.CostUSD}, nil
	}
	if got.Text == "" {
		return spent, errors.New("claude code: no reply text")
	}
	return got, nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " ")
}
