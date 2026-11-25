package coding

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/Grandbusta/tinio/internal/clis"
)

// Claude starts jobs with Claude Code, over its stream-json protocol: the
// brief and anything sent later go in on stdin, what it does comes back as
// JSON lines, and every permission it needs comes as a control_request that
// Tinio answers (--permission-prompt-tool stdio). Reads don't ask.
type Claude struct {
	Program string // the `claude` to run
}

func (c Claude) args(job Job) []string {
	args := []string{"-p",
		"--setting-sources", "project", // the repository's CLAUDE.md and .claude settings, none of the owner's
		"--strict-mcp-config", // and none of their MCP servers
		"--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--permission-prompt-tool", "stdio", "--permission-mode", "manual"}
	if job.System != "" {
		args = append(args, "--append-system-prompt", job.System)
	}
	if job.Model != "" {
		args = append(args, "--model", job.Model)
	}
	if job.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(job.MaxTurns))
	}
	if job.Resume != "" {
		args = append(args, "--resume", job.Resume)
	}
	return args
}

func (c Claude) Start(ctx context.Context, job Job) (Session, error) {
	if c.Program == "" {
		return nil, fmt.Errorf("coding: Claude Code isn't on this computer")
	}
	cmd := exec.Command(c.Program, c.args(job)...)
	cmd.Dir = job.Dir
	cmd.Env = clis.Env(os.Environ(), c.Program)
	ownGroup(cmd) // so ending the job ends everything it started
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	s := &claudeSession{cmd: cmd, in: in, events: make(chan Event, 64), done: make(chan struct{})}
	cmd.Stderr = &s.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("coding: start claude code: %w", err)
	}
	go s.read(out)
	go func() {
		select {
		case <-ctx.Done():
			s.kill()
		case <-s.done:
		}
	}()
	if err := s.Send(job.Brief); err != nil {
		s.kill()
		return nil, err
	}
	return s, nil
}

type claudeSession struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	events chan Event
	done   chan struct{}
	stderr bytes.Buffer

	mu     sync.Mutex
	closed bool
	asked  map[string]map[string]any // each open ask's input, to allow it as asked
	next   int
}

func (s *claudeSession) Events() <-chan Event { return s.events }

func (s *claudeSession) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrEnded
	}
	if _, err := s.in.Write(append(b, '\n')); err != nil {
		return ErrEnded
	}
	return nil
}

func (s *claudeSession) Send(text string) error {
	return s.write(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}})
}

func (s *claudeSession) Answer(askID string, allow bool, why string, stop bool) error {
	s.mu.Lock()
	input := s.asked[askID]
	delete(s.asked, askID)
	s.mu.Unlock()
	resp := map[string]any{"behavior": "deny", "message": why, "interrupt": stop}
	if allow {
		if input == nil {
			input = map[string]any{}
		}
		resp = map[string]any{"behavior": "allow", "updatedInput": input}
	}
	return s.write(map[string]any{"type": "control_response",
		"response": map[string]any{"subtype": "success", "request_id": askID, "response": resp}})
}

func (s *claudeSession) Choose(askID string, answers map[string]string) error {
	s.mu.Lock()
	input := s.asked[askID]
	delete(s.asked, askID)
	s.mu.Unlock()
	in := map[string]any{}
	for k, v := range input {
		in[k] = v
	}
	in["answers"] = answers // the questions as asked, and the answers: how Claude Code hears them
	return s.write(map[string]any{"type": "control_response",
		"response": map[string]any{"subtype": "success", "request_id": askID, "response": map[string]any{"behavior": "allow", "updatedInput": in}}})
}

func (s *claudeSession) Interrupt() error {
	s.mu.Lock()
	s.next++
	id := fmt.Sprintf("tinio-%d", s.next)
	s.mu.Unlock()
	return s.write(map[string]any{"type": "control_request", "request_id": id, "request": map[string]any{"subtype": "interrupt"}})
}

func (s *claudeSession) Close() error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		s.in.Close()
	}
	s.mu.Unlock()
	<-s.done
	return nil
}

func (s *claudeSession) kill() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		s.in.Close()
	}
	s.mu.Unlock()
	killGroup(s.cmd)
}

// cline is a line of stream-json: only what's read here.
type cline struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	SessionID string          `json:"session_id"`
	Message   json.RawMessage `json:"message"`
	Result    string          `json:"result"`
	IsError   bool            `json:"is_error"`
	Cost      float64         `json:"total_cost_usd"`
	Turns     int             `json:"num_turns"`
	RequestID string          `json:"request_id"`
	Request   struct {
		Subtype  string         `json:"subtype"`
		ToolName string         `json:"tool_name"`
		Input    map[string]any `json:"input"`
	} `json:"request"`
}

func (s *claudeSession) read(out io.Reader) {
	defer close(s.done)
	defer close(s.events)
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	ended := false
	for sc.Scan() {
		var l cline
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		switch l.Type {
		case "system":
			if l.Subtype == "init" {
				s.events <- Event{Kind: Started, SessionID: l.SessionID}
			}
		case "assistant":
			var m struct {
				Content []struct {
					Type  string         `json:"type"`
					Text  string         `json:"text"`
					Name  string         `json:"name"`
					Input map[string]any `json:"input"`
				} `json:"content"`
			}
			json.Unmarshal(l.Message, &m)
			for _, c := range m.Content {
				switch c.Type {
				case "text":
					if t := strings.TrimSpace(c.Text); t != "" {
						s.events <- Event{Kind: Said, Text: t}
					}
				case "tool_use":
					s.events <- Event{Kind: Step, Tool: c.Name, Input: c.Input}
					if p := changedPath(c.Name, c.Input); p != "" {
						s.events <- Event{Kind: Changed, Tool: c.Name, Path: p}
					}
				}
			}
		case "control_request":
			if l.Request.Subtype != "can_use_tool" {
				continue
			}
			s.mu.Lock()
			if s.asked == nil {
				s.asked = map[string]map[string]any{}
			}
			s.asked[l.RequestID] = l.Request.Input
			s.mu.Unlock()
			s.events <- Event{Kind: Ask, AskID: l.RequestID, Tool: l.Request.ToolName, Input: l.Request.Input}
		case "result":
			status := Completed
			switch {
			case l.Subtype == "error_max_turns":
				status = Stopped
			case l.IsError || l.Subtype != "success":
				status = Failed
			}
			s.events <- Event{Kind: Done, Status: status, Text: strings.TrimSpace(l.Result), CostUSD: l.Cost, Turns: l.Turns}
			ended = true
		}
	}
	s.cmd.Wait()
	if !ended {
		why := strings.TrimSpace(s.stderr.String())
		if why == "" {
			why = "Claude Code ended without saying why"
		}
		s.events <- Event{Kind: Done, Status: Failed, Text: why}
	}
}

// changedPath is the file a tool changes, if it's one that changes files.
func changedPath(tool string, input map[string]any) string {
	switch tool {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		for _, k := range []string{"file_path", "notebook_path"} {
			if p, ok := input[k].(string); ok {
				return p
			}
		}
	}
	return ""
}
