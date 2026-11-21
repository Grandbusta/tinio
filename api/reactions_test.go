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

// A thumb on a work of their agent's is a row the agent hears, never shown;
// one on a work that isn't theirs, or up with a note, is refused. Search finds
// words anywhere in the chat, and a % in what's searched is only a %.
func TestThumbsAndSearch(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	path := "/v1/agents/" + zara
	ctx := context.Background()
	st := store.New(ta.db).For(zara)
	w := memory.Work{ID: ulid.Make(), Kind: "caption", Title: "Gift 1: the travel tin", Text: "For the commuter.", CreatedAt: time.Now().UTC()}
	if err := st.SaveWork(ctx, w); err != nil {
		t.Fatal(err)
	}
	ownWork := w.ID.String()

	if rec, _ := ta.do(t, "POST", path+"/reactions", map[string]any{"work": ulid.Make().String(), "good": true}, ada, site); rec.Code != http.StatusNotFound {
		t.Fatalf("a thumb on no work of theirs: %d", rec.Code)
	}
	if rec, _ := ta.do(t, "POST", path+"/reactions", map[string]any{"work": ownWork, "good": true, "note": "x"}, ada, site); rec.Code != http.StatusBadRequest {
		t.Fatalf("a thumb up with a note: %d", rec.Code)
	}
	rec, out := ta.do(t, "POST", path+"/reactions", map[string]any{"work": ownWork, "good": false, "note": "Too long"}, ada, site)
	row, _ := data(out)["message"].(map[string]any)
	if rec.Code != http.StatusCreated || row == nil || row["kind"] != memory.ChatReact {
		t.Fatalf("a thumb down: %d %v", rec.Code, out)
	}

	for _, text := range []string{"The travel tin sold best", "Up 50% on last year", "Nothing about it"} {
		if rec, _ := ta.do(t, "POST", path+"/messages", map[string]any{"text": text}, ada, site); rec.Code != http.StatusCreated {
			t.Fatalf("sending: %d", rec.Code)
		}
	}
	found := func(q string) []any {
		t.Helper()
		rec, out := ta.do(t, "GET", path+"/search?q="+q, nil, ada, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("searching %q: %d", q, rec.Code)
		}
		ms, _ := data(out)["messages"].([]any)
		return ms
	}
	if ms := found("TRAVEL%20TIN"); len(ms) != 1 {
		t.Fatalf("searching any case: %v", ms)
	}
	if ms := found("%25"); len(ms) != 1 {
		t.Fatalf("searching for a %%: %v", ms)
	}
}
