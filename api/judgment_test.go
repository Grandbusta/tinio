package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// How you decide is the principles the agent may use and those waiting for
// their owner, the calls their owner answered and how often it guessed them;
// their owner keeps or removes a principle, or writes one, and only kept ones
// are used.
func TestTheOwnerKeepsRemovesAndWritesPrinciples(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	ctx, st := context.Background(), store.New(ta.db).For(zara)
	now := time.Now().UTC()
	ta.now = now
	proposed := memory.Principle{ID: ulid.Make(), Text: "In December, what sells comes before what's new.", Kind: memory.PrincipleRule,
		Status: memory.JudgmentProposed, Source: memory.JudgmentFromReview, CreatedAt: now.Add(-time.Hour)}
	removed := memory.Principle{ID: ulid.Make(), Text: "Removed before", Kind: memory.PrincipleRule, Status: memory.JudgmentRejected,
		Source: memory.JudgmentFromReview, CreatedAt: now.Add(-time.Hour)}
	for _, p := range []memory.Principle{proposed, removed} {
		if err := st.SavePrinciple(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	for i, owner := range []int{0, 1} {
		c := memory.Call{ID: ulid.Make(), Type: memory.CallFocus, Mode: memory.CallAfter, Situation: "Gift guide first, or the oolong launch?",
			Options: []string{"Gift guide first", "The oolong launch"}, Status: memory.CallAsked, CreatedAt: now.Add(-48 * time.Hour)}
		if err := st.SaveCall(ctx, c); err != nil {
			t.Fatal(err)
		}
		c.OwnerPick, c.OwnerReason, c.AnsweredBy, c.AnsweredAt = owner, "December pays the rent.", "telegram", now.Add(time.Duration(i-30)*time.Hour)
		if _, err := st.AnswerCall(ctx, c, false); err != nil {
			t.Fatal(err)
		}
	}
	get := func() map[string]any {
		t.Helper()
		rec, out := ta.do(t, "GET", "/v1/agents/"+zara+"/judgment", nil, ada, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("judgment: %d %v", rec.Code, out)
		}
		return data(out)
	}
	statusOf := func(d map[string]any, text string) any {
		for _, p := range d["principles"].([]any) {
			if p.(map[string]any)["text"] == text {
				return p.(map[string]any)["status"]
			}
		}
		return nil
	}

	d := get()
	if statusOf(d, proposed.Text) != memory.JudgmentProposed || statusOf(d, removed.Text) != nil {
		t.Errorf("principles = %v", d["principles"])
	}
	if g := d["guessed"].(map[string]any); g["right"] != 1.0 || g["of"] != 2.0 {
		t.Errorf("guessed = %v", g)
	}
	calls := d["calls"].([]any)
	if c := calls[0].(map[string]any); len(calls) != 2 || c["chose"] != "The oolong launch" || c["guessed"] != "Gift guide first" || c["why"] != "December pays the rent." {
		t.Errorf("calls = %v", calls)
	}

	if rec, out := ta.do(t, "POST", "/v1/agents/"+zara+"/principles/"+proposed.ID.String(), map[string]any{"keep": true}, ada, site); rec.Code != http.StatusOK {
		t.Fatalf("keeping: %d %v", rec.Code, out)
	}
	if rec, out := ta.do(t, "POST", "/v1/agents/"+zara+"/principles", map[string]any{"text": "  Regulars come   before new customers. "}, ada, site); rec.Code != http.StatusCreated {
		t.Fatalf("writing one: %d %v", rec.Code, out)
	}
	if rec, _ := ta.do(t, "POST", "/v1/agents/"+zara+"/principles", map[string]any{"text": " "}, ada, site); rec.Code != http.StatusBadRequest {
		t.Errorf("an empty one: %d", rec.Code)
	}
	kept, err := st.Principles(ctx, memory.JudgmentApproved)
	if err != nil {
		t.Fatal(err)
	}
	has := map[string]string{}
	for _, p := range kept {
		has[p.Text] = p.Source
	}
	if has[proposed.Text] != memory.JudgmentFromReview || has["Regulars come before new customers."] != memory.JudgmentFromOwner {
		t.Errorf("kept = %v", has)
	}

	if rec, _ := ta.do(t, "POST", "/v1/agents/"+zara+"/principles/"+proposed.ID.String(), map[string]any{"keep": false}, ada, site); rec.Code != http.StatusOK {
		t.Fatalf("removing: %d", rec.Code)
	}
	if s := statusOf(get(), proposed.Text); s != nil {
		t.Errorf("a removed principle is still listed, %v", s)
	}
	if rec, _ := ta.do(t, "POST", "/v1/agents/"+zara+"/principles/"+ulid.Make().String(), map[string]any{"keep": true}, ada, site); rec.Code != http.StatusNotFound {
		t.Errorf("no such principle: %d", rec.Code)
	}
}
