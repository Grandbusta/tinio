package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/api/config"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// An owner sends each agent so many messages a day: a guard against a loop
// running up their bill.
func TestMessagesADayAreCapped(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)

	st, ctx := store.New(ta.db).For(zara), context.Background()
	for range config.MessagesPerDay - 1 {
		m := memory.ChatMessage{ID: ulid.Make().String(), Author: memory.ChatOwner, Text: "hi", At: time.Now().UTC()}
		if err := st.SaveChat(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if rec, out := ta.do(t, "POST", "/v1/agents/"+zara+"/messages", map[string]string{"text": "the last today"}, ada, site); rec.Code != http.StatusCreated {
		t.Fatalf("the day's last message: %d %v", rec.Code, out)
	}
	rec, out := ta.do(t, "POST", "/v1/agents/"+zara+"/messages", map[string]string{"text": "one more"}, ada, site)
	if rec.Code != http.StatusTooManyRequests || out["code"] != "daily_limit" {
		t.Errorf("one message too many: %d %v", rec.Code, out)
	}
}
