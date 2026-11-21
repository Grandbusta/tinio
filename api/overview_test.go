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

// The overview is the account's own agents, each with the last thing said
// in their chat (never the owner's taps) and their requests waiting, and what
// they made and asked lately across all of them, newest first.
func TestTheOverviewIsYourAgents(t *testing.T) {
	ta := newTestAPI(t)
	ada, bea := ta.signUp(t), ta.signUp(t)
	ta.onPlan(t, ada, "pro")
	first, second := ta.raiseZara(t, ada), ta.raiseZara(t, ada)
	ta.raiseZara(t, bea)

	ctx, now := context.Background(), time.Now().UTC()
	st := store.New(ta.db).For(first)
	var asked string
	for _, m := range []memory.ChatMessage{
		{Author: memory.ChatOwner, Text: "Morning!", At: now.Add(-time.Hour)},
		{Author: memory.ChatAgent, Kind: memory.ChatAsk, Text: "May I post the oat milk post?", At: now.Add(-50 * time.Minute)},
		{Author: memory.ChatOwner, Kind: memory.ChatApprove, Payload: []byte(`{}`), At: now.Add(-40 * time.Minute)},
	} {
		m.ID = ulid.Make().String()
		if err := st.SaveChat(ctx, m); err != nil {
			t.Fatal(err)
		}
		if m.Kind == memory.ChatAsk {
			asked = m.ID
		}
	}
	work := memory.Work{ID: ulid.Make(), Kind: "post", Title: "Three hooks for oat milk", CreatedAt: now.Add(-30 * time.Minute)}
	if err := store.New(ta.db).For(second).SaveWork(ctx, work); err != nil {
		t.Fatal(err)
	}

	rec, out := ta.do(t, "GET", "/v1/overview", nil, ada, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("overview: %d %v", rec.Code, out)
	}
	agents := data(out)["agents"].([]any)
	if len(agents) != 2 {
		t.Fatalf("%d agents, want Ada's 2", len(agents))
	}
	a := agents[0].(map[string]any)
	last, _ := a["last"].(map[string]any)
	if a["id"] != first || a["waiting"] != float64(1) || a["unread"] != float64(1) || last == nil || last["text"] != "May I post the oat milk post?" {
		t.Errorf("the first agent: %v", a)
	}
	if b := agents[1].(map[string]any); b["last"] != nil || b["waiting"] != float64(0) {
		t.Errorf("the second agent, who's said nothing: %v", b)
	}
	feed := data(out)["lately"].([]any)
	if len(feed) != 2 || feed[0].(map[string]any)["text"] != work.Title || feed[1].(map[string]any)["kind"] != "asked" {
		t.Errorf("lately = %v", feed)
	}

	// Ada opens the chat: what Zara said is read, and stays read.
	if rec, out := ta.do(t, "POST", "/v1/agents/"+first+"/read", map[string]string{"until": asked}, ada, site); rec.Code != http.StatusOK {
		t.Fatalf("marking read: %d %v", rec.Code, out)
	}
	ta.do(t, "POST", "/v1/agents/"+first+"/read", map[string]string{"until": "00000000000000000000000000"}, ada, site) // an older row: no going back
	_, out = ta.do(t, "GET", "/v1/overview", nil, ada, "")
	if a := data(out)["agents"].([]any)[0].(map[string]any); a["unread"] != float64(0) {
		t.Errorf("after reading: %v unread", a["unread"])
	}
}
