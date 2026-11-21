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

// An owner starts a topic by writing in "new", and a reply to a message is
// in that message's topic. Moving their message to another topic takes the
// agent's answer along; the list shows their topics, never the agent's with
// anyone else, and a topic of someone else's can't be written in.
func TestTopicsInTheChat(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	path := "/v1/agents/" + zara
	ctx := context.Background()
	st := store.New(ta.db).For(zara)

	send := func(body map[string]any) map[string]any {
		t.Helper()
		rec, out := ta.do(t, "POST", path+"/messages", body, ada, site)
		row, _ := data(out)["message"].(map[string]any)
		if rec.Code != http.StatusCreated || row == nil {
			t.Fatalf("sending %v: %d %v", body, rec.Code, out)
		}
		return row
	}
	first := send(map[string]any{"text": "A spring newsletter, out in March.", "topic": "new"})
	topic, _ := first["topic"].(string)
	if topic == "" {
		t.Fatalf("writing in a new topic started none: %v", first)
	}
	answer := memory.ChatMessage{ID: ulid.Make().String(), Author: memory.ChatAgent, Kind: memory.ChatMessageKind,
		Text: "What should it lead with?", At: time.Now().UTC(), Topic: topic}
	if err := st.SaveChat(ctx, answer); err != nil {
		t.Fatal(err)
	}
	if reply := send(map[string]any{"text": "The new oolong.", "reply_to": answer.ID}); reply["topic"] != topic {
		t.Fatalf("a reply is in %v, not its message's topic %s", reply["topic"], topic)
	}

	other := memory.Topic{ID: ulid.Make().String(), Person: "telegram:42", Title: "Someone else's", StartedAt: time.Now().UTC()}
	if err := st.StartTopic(ctx, other); err != nil {
		t.Fatal(err)
	}
	if rec, _ := ta.do(t, "POST", path+"/messages", map[string]any{"text": "hi", "topic": other.ID}, ada, site); rec.Code != http.StatusNotFound {
		t.Fatalf("writing in someone else's topic: %d", rec.Code)
	}

	if rec, out := ta.do(t, "POST", path+"/messages/"+first["id"].(string)+"/topic", map[string]any{"topic": "new"}, ada, site); rec.Code != http.StatusOK {
		t.Fatalf("moving a message: %d %v", rec.Code, out)
	}
	moved, _, err := st.ChatMessage(ctx, answer.ID)
	if err != nil || moved.Topic == topic || moved.Topic == "" {
		t.Fatalf("the agent's answer stayed in %q when its message moved (%v)", moved.Topic, err)
	}

	if rec, _ := ta.do(t, "POST", path+"/topics/"+moved.Topic, map[string]any{"title": "Newsletter"}, ada, site); rec.Code != http.StatusOK {
		t.Fatalf("renaming: %d", rec.Code)
	}
	rec, out := ta.do(t, "GET", path+"/topics", nil, ada, "")
	topics, _ := data(out)["topics"].([]any)
	if rec.Code != http.StatusOK || len(topics) != 2 {
		t.Fatalf("topics: %d %v", rec.Code, out)
	}
	for _, x := range topics {
		v := x.(map[string]any)
		if v["id"] == other.ID {
			t.Fatal("someone else's topic is listed")
		}
		if v["id"] == moved.Topic && v["title"] != "Newsletter" {
			t.Fatalf("renamed to %v", v["title"])
		}
	}
}
