package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Grandbusta/tinio/internal/providers/system2"
)

// What an agent keeps about each of their folders from the jobs done in it: a
// few short notes, learnt when the owner commits a job, from its story (how
// the tests run, the rules the owner gave, what was sent back and why). The
// next job there starts with them: in the brief the agent writes, in what
// Claude Code is told, and in the agent's checks. The owner can take any out.

// MaxNotes is the most an agent keeps about one folder.
const MaxNotes = 8

// Notes is what agent keeps about folder, oldest first.
func (s *Service) Notes(ctx context.Context, agent, folder string) ([]string, error) {
	var raw string
	err := s.DB.QueryRow(ctx, `SELECT notes FROM coding_notes WHERE agent_id = $1 AND folder_id = $2`, agent, folder).Scan(&raw)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return nil, nil
		}
		return nil, fmt.Errorf("jobs: notes: %w", err)
	}
	var notes []string
	if err := json.Unmarshal([]byte(raw), &notes); err != nil {
		return nil, fmt.Errorf("jobs: notes: %w", err)
	}
	return notes, nil
}

// SaveNotes keeps notes as what agent knows of folder, at most MaxNotes, each
// trimmed; the empty ones go.
func (s *Service) SaveNotes(ctx context.Context, agent, folder string, notes []string) error {
	kept := []string{}
	for _, n := range notes {
		if n = strings.TrimSpace(n); n != "" && len(kept) < MaxNotes {
			if r := []rune(n); len(r) > 300 {
				n = string(r[:300])
			}
			kept = append(kept, n)
		}
	}
	b, _ := json.Marshal(kept)
	_, err := s.DB.Exec(ctx, `INSERT INTO coding_notes (agent_id, folder_id, notes, updated_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (agent_id, folder_id) DO UPDATE SET notes = excluded.notes, updated_at = excluded.updated_at`,
		agent, folder, string(b), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("jobs: save notes: %w", err)
	}
	return nil
}

// notesNote is the notes as a few lines for a prompt, under head; "" if none.
func notesNote(head string, notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	return head + "\n- " + strings.Join(notes, "\n- ")
}

var notesSchema = map[string]any{
	"type": "object", "additionalProperties": false, "required": []string{"notes"},
	"properties": map[string]any{
		"notes": map[string]any{"type": "array", "maxItems": MaxNotes, "items": map[string]any{"type": "string"}},
	},
}

// learn has the agent read a job's story, now the owner has committed it, and
// keep what's worth knowing next time in its folder, merged with what they
// knew already.
func (s *Service) learn(ctx context.Context, j Job) {
	rv := s.reviewerNow()
	if rv == nil || ctx == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	steps, err := s.Steps(ctx, j.ID)
	if err != nil {
		s.Log.Warn("jobs: no story to learn from", "job", j.ID, "err", err)
		return
	}
	had, err := s.Notes(ctx, j.Agent, j.Folder)
	if err != nil {
		s.Log.Warn("jobs: the notes weren't read", "job", j.ID, "err", err)
		return
	}
	var story strings.Builder
	for _, st := range steps {
		if st.What == Planned || st.What == RoundStarted {
			continue
		}
		who := map[string]string{WhoAgent: "You", WhoCLI: "Claude Code", WhoOwner: "The owner", WhoTinio: "Tinio"}[st.Who]
		fmt.Fprintf(&story, "- %s: %s", who, st.What)
		if d := strings.TrimSpace(st.Detail); d != "" {
			if r := []rune(d); len(r) > 600 {
				d = string(r[:600]) + "…"
			}
			fmt.Fprintf(&story, ": %s", strings.ReplaceAll(d, "\n", " "))
		}
		story.WriteString("\n")
	}
	name, owner := s.agentName(ctx, j.Agent), s.ownerName(ctx, j.Agent)
	instructions := fmt.Sprintf(`You're %[1]s, a software engineer. %[2]s just committed a coding job you briefed and checked, in their repository %[3]s. Keep what's worth knowing the next time you write a brief for a job there, or check one: at most %[4]d short notes, each one line.

Good notes are specific and lasting: how its tests run and what was already failing, rules %[2]s gave ("use money.ts, no new packages"), what %[2]s sent back and why, a command that needs asking for, where things live. Not what this job did, and not general advice.

Start from what you already keep: keep what's still true, change what this job showed was wrong, add what's new, and drop the least useful when there are too many. No em dashes. British spelling.`, name, owner, filepath.Base(j.Repo), MaxNotes)
	msg := fmt.Sprintf("# What you keep about %s now\n%s\n\n# The job: %s\n%s\n\n# Its story\n%s",
		filepath.Base(j.Repo), orNone(notesNote("", had)), j.Title, j.Brief, story.String())
	res, err := rv.Reason(ctx, system2.Request{Instructions: instructions, Message: msg, Format: &system2.Format{Name: "notes", Schema: notesSchema}})
	if err != nil {
		s.Log.Warn("jobs: learning from the job failed", "job", j.ID, "err", err)
		return
	}
	var out struct {
		Notes []string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(res.Text), &out); err != nil {
		s.Log.Warn("jobs: the notes weren't JSON", "job", j.ID, "err", err)
		return
	}
	if err := s.SaveNotes(ctx, j.Agent, j.Folder, out.Notes); err != nil {
		s.Log.Warn("jobs: keeping the notes failed", "job", j.ID, "err", err)
		return
	}
	s.step(ctx, j.ID, WhoAgent, "Kept what this job taught about "+filepath.Base(j.Repo), strings.Join(out.Notes, "\n"))
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(nothing yet)"
	}
	return s
}
