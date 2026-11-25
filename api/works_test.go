package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// What an agent made is theirs to show their owner: made for their owner, never
// something made for someone else. Any of it
// downloads in the formats it can be made into, or is sent into their chat.
func TestWorksAreWhatTheyMadeForThemselvesAndYou(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	var owner string
	if err := ta.db.QueryRow(context.Background(), `SELECT 'account:' || account_id FROM agents WHERE id = $1`, zara).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	ctx, st, now := context.Background(), store.New(ta.db).For(zara), time.Now().UTC()
	promised := func(subject string) ulid.ULID {
		i := memory.Intention{ID: ulid.Make(), Subject: subject, Kind: memory.IntentionTask, Text: "make it", DueAt: now,
			Status: memory.IntentionDone, CreatedAt: now}
		if err := st.SaveIntention(ctx, i); err != nil {
			t.Fatal(err)
		}
		return i.ID
	}
	works := map[string]memory.Work{
		"own":    {Kind: "post", Title: "Oat milk hooks", Text: "Three hooks.\n\nOne: oats, but make it morning."},
		"yours":  {Kind: "content_calendar", Title: "December calendar", Text: "Week one: gifts.", IntentionID: promised(owner)},
		"theirs": {Kind: "post", Title: "For someone else", Text: "Not for Ada.", IntentionID: promised("telegram:9")},
	}
	for name, w := range works {
		w.ID, w.CreatedAt = ulid.Make(), now
		if err := st.SaveWork(ctx, w); err != nil {
			t.Fatal(err)
		}
		works[name] = w
	}

	rec, out := ta.do(t, "GET", "/v1/agents/"+zara+"/works", nil, ada, "")
	list, _ := data(out)["works"].([]any)
	if rec.Code != http.StatusOK || len(list) != 2 {
		t.Fatalf("works: %d %v; want her own and Ada's", rec.Code, out)
	}
	for _, x := range list {
		w := x.(map[string]any)
		switch w["title"] {
		case "December calendar":
			if w["for_you"] != true || w["kind"] != "content calendar" {
				t.Errorf("Ada's: %v", w)
			}
		case "Oat milk hooks":
			if w["for_you"] != false || !strings.HasPrefix(w["peek"].(string), "Three hooks. One:") || w["text"] != nil {
				t.Errorf("her own: %v", w)
			}
		default:
			t.Errorf("listed %v", w)
		}
	}

	for name, want := range map[string]int{"own": http.StatusOK, "theirs": http.StatusNotFound} {
		if rec, _ := ta.do(t, "GET", "/v1/agents/"+zara+"/works/"+works[name].ID.String(), nil, ada, ""); rec.Code != want {
			t.Errorf("reading %s: %d, want %d", name, rec.Code, want)
		}
	}
	rec, _ = ta.do(t, "GET", "/v1/agents/"+zara+"/works/"+works["own"].ID.String()+"/files/pdf", nil, ada, "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "oat-milk-hooks.pdf") {
		t.Errorf("as a PDF: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Content-Disposition"))
	}
	if rec, _ := ta.do(t, "GET", "/v1/agents/"+zara+"/works/"+works["own"].ID.String()+"/files/exe", nil, ada, ""); rec.Code != http.StatusNotFound {
		t.Errorf("a format there isn't: %d", rec.Code)
	}

	if rec, out := ta.do(t, "POST", "/v1/agents/"+zara+"/works/"+works["own"].ID.String()+"/send", map[string]any{}, ada, site); rec.Code != http.StatusCreated {
		t.Fatalf("sending it: %d %v", rec.Code, out)
	}
	if rec, _ := ta.do(t, "POST", "/v1/agents/"+zara+"/works/"+works["theirs"].ID.String()+"/send", map[string]any{}, ada, site); rec.Code != http.StatusNotFound {
		t.Errorf("sending one made for someone else: %d", rec.Code)
	}
	chat, err := st.Chat(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if last := chat[len(chat)-1]; last.Author != memory.ChatAgent || last.Kind != memory.ChatFile || last.Text != "Oat milk hooks" {
		t.Errorf("in the chat: %+v", last)
	}
}
