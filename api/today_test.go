package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// Today is their own day as it stands: what they planned and how each went,
// what they made, the start of their newest journal entry, and their requests
// still waiting. Their reminders for their owner and yesterday aren't in it.
func TestTodayIsTheirOwnDay(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	rec, out := ta.do(t, "POST", "/v1/agents", map[string]any{"character": "zara", "owner_name": "Ada", "relation": "friend", "timezone": "UTC"}, ada, site)
	if rec.Code != http.StatusCreated {
		t.Fatalf("raising Zara: %d %v", rec.Code, out)
	}
	zara := data(out)["agent"].(map[string]any)["id"].(string)
	midnight := time.Now().UTC().Truncate(24 * time.Hour)
	ta.now = midnight.Add(12 * time.Hour)
	at := func(h, m int) time.Time {
		return midnight.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
	}

	ctx, st := context.Background(), store.New(ta.db).For(zara)
	for _, i := range []memory.Intention{
		{Text: "Draft the week's posts", DueAt: at(9, 0), Status: memory.IntentionDone, Result: "Five posts for the week"},
		{Text: "Read about hooks", DueAt: at(11, 0), Status: memory.IntentionMissed},
		{Text: "Plan the launch calendar", DueAt: at(11, 30), Status: memory.IntentionPending},
		{Text: "Sketch a campaign", DueAt: at(15, 0), Status: memory.IntentionPending},
		{Text: "Something dropped", DueAt: at(16, 0), Status: memory.IntentionCancelled},
		{Text: "Yesterday's", DueAt: at(-10, 0), Status: memory.IntentionDone},
		{Subject: "account:someone", Kind: memory.IntentionRemind, Text: "A reminder", DueAt: at(13, 0), Status: memory.IntentionPending},
	} {
		i.ID, i.CreatedAt = ulid.Make(), at(8, 0)
		if i.Subject == "" {
			i.Subject, i.Kind = memory.SubjectSelf, memory.IntentionActivity
		}
		if err := st.SaveIntention(ctx, i); err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range []memory.Work{
		{Kind: "post", Title: "Five posts for the week", CreatedAt: at(9, 30)},
		{Kind: "journal", Title: "Journal", Text: "Shorter is braver. The rest is mine.", CreatedAt: at(-2, 0)},
	} {
		w.ID = ulid.Make()
		if err := st.SaveWork(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SaveChat(ctx, memory.ChatMessage{ID: ulid.Make().String(), Author: memory.ChatAgent, Kind: memory.ChatAsk, Text: "May I post?", At: at(10, 0)}); err != nil {
		t.Fatal(err)
	}

	rec, out = ta.do(t, "GET", "/v1/agents/"+zara+"/today", nil, ada, "")
	today, _ := data(out)["today"].(map[string]any)
	if rec.Code != http.StatusOK || today == nil {
		t.Fatalf("today: %d %v", rec.Code, out)
	}
	var states []string
	for _, p := range today["plan"].([]any) {
		states = append(states, p.(map[string]any)["at"].(string)+" "+p.(map[string]any)["state"].(string))
	}
	want := []string{"09:00 done", "11:00 missed", "11:30 now", "15:00 later"}
	if len(states) != len(want) || states[0] != want[0] || states[1] != want[1] || states[2] != want[2] || states[3] != want[3] {
		t.Errorf("plan = %v, want %v", states, want)
	}
	if d := today["plan"].([]any)[0].(map[string]any)["detail"]; d != "Five posts for the week" {
		t.Errorf("what came of it = %v", d)
	}
	if made := today["made"].([]any); len(made) != 1 || made[0].(map[string]any)["title"] != "Five posts for the week" {
		t.Errorf("made = %v", made)
	}
	if today["journal"] != "Shorter is braver." || today["waiting"] != float64(1) || today["planned_at"] != "08:00" {
		t.Errorf("journal %q, waiting %v, planned at %v", today["journal"], today["waiting"], today["planned_at"])
	}
}
