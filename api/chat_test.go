package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/channel/web"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/store"
)

// raiseZara is Zara, raised by who.
func (ta *testAPI) raiseZara(t *testing.T, who *http.Cookie) string {
	t.Helper()
	rec, out := ta.do(t, "POST", "/v1/agents", map[string]any{"character": "zara", "owner_name": "Ada", "relation": "friend"}, who, site)
	agent, _ := data(out)["agent"].(map[string]any)
	if rec.Code != http.StatusCreated || agent == nil {
		t.Fatalf("raising Zara: %d %v", rec.Code, out)
	}
	return agent["id"].(string)
}

// Only an agent's owner can read, write, answer, download, watch or mark read
// their chat, see their day, works, growth or mind, or change who they are, how
// they decide or what they remember: anyone else's is not found.
func TestOnlyTheOwnerReachesTheirAgentsChat(t *testing.T) {
	ta := newTestAPI(t)
	ada, bea := ta.signUp(t), ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	file := memory.SentFile{ID: ulid.Make().String(), Name: "poem.pdf", ContentType: "application/pdf", Data: []byte("%PDF"), At: time.Now()}
	if err := store.New(ta.db).For(zara).SaveFile(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{"GET", "", nil},
		{"POST", "/hello", nil},
		{"GET", "/messages", nil},
		{"POST", "/messages", map[string]string{"text": "hi"}},
		{"POST", "/answers", map[string]any{"approval": ulid.Make().String(), "approve": true}},
		{"GET", "/files/" + file.ID, nil},
		{"GET", "/stream", nil},
		{"GET", "/today", nil},
		{"GET", "/works", nil},
		{"GET", "/growth", nil},
		{"GET", "/judgment", nil},
		{"GET", "/mind", nil},
		{"GET", "/mind/" + ulid.Make().String(), nil},
		{"POST", "/labels", map[string]string{"decision": ulid.Make().String(), "question": "intent", "value": "share"}},
		{"GET", "/memory", nil},
		{"POST", "/facts/" + ulid.Make().String(), map[string]string{"text": "x"}},
		{"POST", "/facts/" + ulid.Make().String() + "/forget", nil},
		{"GET", "/character", nil},
		{"POST", "/character", map[string]any{"story": []string{"x"}}},
		{"POST", "/character/versions/" + ulid.Make().String(), nil},
		{"GET", "/skills", nil},
		{"POST", "/skills/brand-voice/remove", nil},
		{"GET", "/settings", nil},
		{"POST", "/settings", map[string]any{"classes": map[string]string{"dream": "auto"}}},
		{"POST", "/delete", map[string]string{"name": "Zara"}},
		{"POST", "/principles", map[string]string{"text": "Be kind"}},
		{"POST", "/principles/" + ulid.Make().String(), map[string]any{"keep": true}},
		{"POST", "/read", map[string]string{"until": ulid.Make().String()}},
		{"GET", "/topics", nil},
		{"POST", "/topics/" + ulid.Make().String(), map[string]string{"title": "x"}},
		{"POST", "/topics/" + ulid.Make().String() + "/close", nil},
		{"POST", "/topics/" + ulid.Make().String() + "/merge", map[string]string{"into": ulid.Make().String()}},
		{"POST", "/messages/" + ulid.Make().String() + "/topic", map[string]string{"topic": ""}},
	} {
		if rec, _ := ta.do(t, r.method, "/v1/agents/"+zara+r.path, r.body, bea, site); rec.Code != http.StatusNotFound {
			t.Errorf("someone else's %s %s: %d", r.method, r.path, rec.Code)
		}
	}
	rec, _ := ta.do(t, "GET", "/v1/agents/"+zara+"/files/"+file.ID, nil, ada, "")
	if rec.Code != http.StatusOK || rec.Body.String() != "%PDF" || !strings.Contains(rec.Header().Get("Content-Disposition"), `filename=poem.pdf`) {
		t.Errorf("Ada's own file: %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Disposition"))
	}
}

// The owner writes, which wakes their agent first in line; what the agent says
// back reaches an open stream as it's written and when it's said, and the
// history has both.
func TestTheOwnerWritesAndTheReplyIsStreamed(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	srv := httptest.NewServer(ta.router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/v1/agents/"+zara+"/stream", nil)
	req.Host = "tinio.test" // not this machine by name, so it's Ada's session that counts
	req.AddCookie(ada)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("stream: %v %v", resp, err)
	}
	defer resp.Body.Close()
	events := make(chan map[string]any) // each event's data, with its "event" and "event_id"
	go func() {
		sc := bufio.NewScanner(resp.Body)
		id, event := "", ""
		for sc.Scan() {
			if v, ok := strings.CutPrefix(sc.Text(), "id: "); ok {
				id = v
			}
			if v, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
				event = v
			}
			if d, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var row map[string]any
				json.Unmarshal([]byte(d), &row)
				row["event"], row["event_id"], id = event, id, ""
				events <- row
			}
		}
	}()
	next := func(what string) map[string]any {
		t.Helper()
		select {
		case row := <-events:
			return row
		case <-ctx.Done():
			t.Fatalf("no %s on the stream", what)
			return nil
		}
	}
	time.Sleep(200 * time.Millisecond) // the stream is watching before anything is said

	if rec, out := ta.do(t, "POST", "/v1/agents/"+zara+"/messages", map[string]string{"text": "  morning!  "}, ada, site); rec.Code != http.StatusCreated {
		t.Fatalf("writing: %d %v", rec.Code, out)
	}
	var waiting bool
	if err := ta.db.QueryRow(ctx, `SELECT wake_at = '0001-01-01T00:00:00.000000000Z' FROM agent_runs WHERE agent_id = $1`, zara).Scan(&waiting); err != nil || !waiting {
		t.Errorf("writing didn't wake Zara first in line: %v %v", waiting, err)
	}
	if row := next("owner's message"); row["author"] != "owner" || row["text"] != "morning!" {
		t.Errorf("streamed %v", row)
	}
	chat := web.Channel{Store: store.New(ta.db).For(zara)}
	if err := chat.Draft(ctx, perception.Percept{}, "Morning! The"); err != nil {
		t.Fatal(err)
	}
	if row := next("draft"); row["event"] != "draft" || row["text"] != "Morning! The" || row["event_id"] != "" {
		t.Errorf("streamed %v; want the draft, without an event id", row)
	}
	if err := chat.Reply(ctx, perception.Percept{}, "Morning! The kettle's on."); err != nil {
		t.Fatal(err)
	}
	if row := next("reply"); row["author"] != "agent" || row["text"] != "Morning! The kettle's on." || row["event_id"] != row["id"] {
		t.Errorf("streamed %v", row)
	}

	// A request settled after something newer comes again without an id: the
	// browser's place in the chat never goes back.
	where, err := chat.Ask(ctx, perception.Percept{}, memory.Request{Text: "May I post?"}, ulid.Make().String())
	if err != nil {
		t.Fatal(err)
	}
	asked := next("request")
	if err := chat.Reply(ctx, perception.Percept{}, "Something newer."); err != nil {
		t.Fatal(err)
	}
	next("newer reply")
	if err := chat.Settle(ctx, where, "May I post?\n\n✅ You said yes."); err != nil {
		t.Fatal(err)
	}
	if row := next("settled request"); row["id"] != asked["id"] || row["settled_at"] == nil || row["event_id"] != "" {
		t.Errorf("settled request streamed %v; want it without an event id", row)
	}
	_, out := ta.do(t, "GET", "/v1/agents/"+zara+"/messages", nil, ada, "")
	if rows, _ := data(out)["messages"].([]any); len(rows) != 4 {
		t.Errorf("history: %v", out)
	}
}

// A tap counts only on a request or call of the agent's own that's still
// waiting, and reaches them as the answer Telegram's buttons give.
func TestAnswersOnlyWhatsWaiting(t *testing.T) {
	ta := newTestAPI(t)
	ctx := context.Background()
	ada := ta.signUp(t)
	ta.onPlan(t, ada, "pro")
	zara, other := ta.raiseZara(t, ada), ta.raiseZara(t, ada)
	st := store.New(ta.db).For(zara)
	request := func(on string, sent bool) string {
		t.Helper()
		a := memory.Approval{ID: ulid.Make(), Class: "dream", What: "add a dream", Text: "x", Status: memory.ApprovalPending, AskedAt: time.Now().UTC()}
		s := store.New(ta.db).For(on)
		if err := s.SaveApproval(ctx, a); err != nil {
			t.Fatal(err)
		}
		if sent {
			if err := s.MarkApprovalSent(ctx, a.ID, []byte(`{"message":"m"}`), time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
		}
		return a.ID.String()
	}
	answer := func(body map[string]any) int {
		rec, _ := ta.do(t, "POST", "/v1/agents/"+zara+"/answers", body, ada, site)
		return rec.Code
	}
	waiting, unsent, his := request(zara, true), request(zara, false), request(other, true)
	if code := answer(map[string]any{"approval": unsent, "approve": true}); code != http.StatusConflict {
		t.Errorf("a request never asked: %d", code)
	}
	if code := answer(map[string]any{"approval": his, "approve": true}); code != http.StatusConflict {
		t.Errorf("another agent's request: %d", code)
	}
	if code := answer(map[string]any{"approval": waiting, "approve": false, "text": "mine"}); code != http.StatusBadRequest {
		t.Errorf("your version with a no: %d", code)
	}
	if code := answer(map[string]any{"approval": waiting, "approve": true, "text": "  Zara wants to write a cookbook  "}); code != http.StatusCreated {
		t.Fatalf("Zara's waiting request, with your version: %d", code)
	}
	unheard, err := st.Unheard(ctx, 10)
	if err != nil || len(unheard) != 1 || unheard[0].Kind != memory.ChatApprove {
		t.Fatalf("the tap, for Zara to hear: %+v %v", unheard, err)
	}
	var got perception.Answer
	if json.Unmarshal(unheard[0].Payload, &got); got.Approval != waiting || !got.Approve || got.Text != "Zara wants to write a cookbook" {
		t.Errorf("the tap says %+v", got)
	}

	call := memory.Call{ID: ulid.Make(), Type: memory.CallFocus, Situation: "today", Options: []string{"write", "read"},
		Mode: memory.CallBefore, Status: memory.CallAsked, AskedAt: time.Now().UTC(), CreatedAt: time.Now().UTC()}
	if err := st.SaveCall(ctx, call); err != nil {
		t.Fatal(err)
	}
	if code := answer(map[string]any{"call": call.ID.String(), "option": 2}); code != http.StatusBadRequest {
		t.Errorf("an option there isn't: %d", code)
	}
	if code := answer(map[string]any{"call": call.ID.String(), "option": -1}); code != http.StatusCreated {
		t.Errorf("something else: %d", code)
	}
}

// Opening the chat for the first time asks the agent to say hello, once,
// however often the chat is opened.
func TestHelloIsAskedOnce(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	for _, want := range []bool{true, false} {
		rec, out := ta.do(t, "POST", "/v1/agents/"+zara+"/hello", nil, ada, site)
		if rec.Code != http.StatusOK || data(out)["hello"] != want {
			t.Errorf("hello: %d %v; want hello %v", rec.Code, out, want)
		}
	}
	unheard, err := store.New(ta.db).For(zara).Unheard(context.Background(), 10)
	if err != nil || len(unheard) != 1 || unheard[0].Kind != memory.ChatHello {
		t.Errorf("for Zara to hear: %+v %v", unheard, err)
	}
}
