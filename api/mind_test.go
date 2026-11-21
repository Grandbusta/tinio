package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio"
	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/decision/questions"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/store"
)

func testSets(t *testing.T) map[string]questions.Set {
	t.Helper()
	sets, err := questions.LoadDir(tinio.Files, "decisions")
	if err != nil {
		t.Fatal(err)
	}
	return sets
}

// Mind is today's decisions counted and the newest of them; a decision opens
// on its whole chain, with the wording of what System 1 asked itself; their
// owner labels a read, and only one of their own agent's decisions.
func TestMindShowsAChainAndTakesALabel(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	ta.onPlan(t, ada, "pro")
	zara, other := ta.raiseZara(t, ada), ta.raiseZara(t, ada)
	ctx, st := context.Background(), store.New(ta.db).For(zara)
	now := time.Now().UTC()
	ta.now = now
	p := perception.Percept{ID: ulid.Make(), Source: memory.ChatSource, ExternalID: ulid.Make().String(), Thread: memory.ChatSource,
		Speaker: "account:x", Kind: perception.KindMessage, Text: "Can you draft the newsletter?", At: now}
	if _, err := st.SavePercept(ctx, p); err != nil {
		t.Fatal(err)
	}
	read := decision.Decision{ID: ulid.Make(), At: now, PerceptID: p.ID, SetID: "incoming_message@1,life_area@1", Tier: decision.TierSystem1,
		Provider: "openai/gpt-6-luna", Action: "ask_for_help", Gate: decision.GateAuto, Latency: 3 * time.Second, CostUSD: 0.0002,
		Answers: map[string]decision.Answer{"intent": {Type: decision.QuestionChoice, Choice: "ask_for_help", Confidence: 0.93}}}
	reply := decision.Decision{ID: ulid.Make(), ParentID: &read.ID, At: now, PerceptID: p.ID, SetID: "reply@x", Tier: decision.TierSystem2,
		Provider: "openai/gpt-6.1-terra", Action: "reply", Reply: "Yes, by Thursday.", Gate: decision.GateAuto, Latency: 2 * time.Second, CostUSD: 0.003}
	for _, d := range []decision.Decision{read, reply} {
		if err := st.SaveDecision(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	rec, out := ta.do(t, "GET", "/v1/agents/"+zara+"/mind", nil, ada, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("mind: %d %v", rec.Code, out)
	}
	today := data(out)["today"].(map[string]any)
	if today["decisions"].(float64) < 2 || len(data(out)["decisions"].([]any)) < 2 {
		t.Errorf("mind = %v", data(out))
	}

	label := func(agent, id string) int {
		rec, _ := ta.do(t, "POST", "/v1/agents/"+agent+"/labels", map[string]string{"decision": id, "question": "intent", "value": "ask_for_help"}, ada, site)
		return rec.Code
	}
	if code := label(zara, read.ID.String()); code != http.StatusCreated {
		t.Fatalf("labelling: %d", code)
	}
	if code := label(other, read.ID.String()); code != http.StatusNotFound {
		t.Errorf("labelling another agent's decision: %d", code)
	}

	rec, out = ta.do(t, "GET", "/v1/agents/"+zara+"/mind/"+reply.ID.String(), nil, ada, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("chain: %d %v", rec.Code, out)
	}
	chain := data(out)["chain"].([]any)
	if len(chain) != 2 || chain[0].(map[string]any)["labels"].(map[string]any)["intent"] != "ask_for_help" || chain[1].(map[string]any)["reply"] != "Yes, by Thursday." {
		t.Errorf("chain = %v", chain)
	}
	intent, _ := data(out)["questions"].(map[string]any)["intent"].(map[string]any)
	if intent == nil || intent["instructions"] == "" || len(intent["choices"].([]any)) < 5 {
		t.Errorf("questions = %v", data(out)["questions"])
	}
}
