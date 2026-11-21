package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/store"
)

// What an account's agents cost is counted against its week as each decision
// is saved; the app is told how much is used, by which agent and on what, and
// the state that follows.
func TestUsageIsCountedAndShown(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	ctx, st, now := context.Background(), store.New(ta.db).For(zara), time.Now().UTC()
	p := perception.Percept{ID: ulid.Make(), Source: memory.ChatSource, ExternalID: ulid.Make().String(), Thread: memory.ChatSource,
		Speaker: "account:x", Kind: perception.KindMessage, Text: "Draft the newsletter?", At: now}
	if _, err := st.SavePercept(ctx, p); err != nil {
		t.Fatal(err)
	}
	spend := func(usd float64, percept ulid.ULID) {
		t.Helper()
		d := decision.Decision{ID: ulid.Make(), At: now, PerceptID: percept, SetID: "x@1", Tier: decision.TierSystem2,
			Action: "reply", Gate: decision.GateAuto, CostUSD: usd}
		if err := st.SaveDecision(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	usage := func() map[string]any {
		t.Helper()
		rec, out := ta.do(t, "GET", "/v1/usage", nil, ada, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("usage: %d %v", rec.Code, out)
		}
		return data(out)
	}

	// Free, in its trial: a week of $0.23. A reply ($0.0345) and some of their day ($0.0115).
	spend(0.0345, p.ID)
	spend(0.0115, ulid.ULID{})
	u := usage()
	agent := u["agents"].([]any)[0].(map[string]any)
	if u["used"] != 20.0 || u["state"] != "normal" || agent["chat"] != 15.0 || agent["day"] != 5.0 || u["own_day_until"] == nil {
		t.Errorf("after a reply and some of their day: %v", u)
	}

	// Well past the week, and in an hour: they rest.
	spend(0.4, p.ID)
	if u := usage(); u["state"] != "resting" {
		t.Errorf("well past the week: %v", u)
	}
}
