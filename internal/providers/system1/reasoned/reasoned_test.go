package reasoned

import (
	"context"
	"testing"

	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/providers/system1"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

type fakeReasoner struct {
	text string
	got  system2.Request
}

func (f *fakeReasoner) Reason(_ context.Context, req system2.Request) (system2.Response, error) {
	f.got = req
	return system2.Response{Provider: "openai/test", Text: f.text, CostUSD: 0.01}, nil
}

func TestAnswersTheQuestionsAsTyped(t *testing.T) {
	qs := []system1.Question{
		{Name: "met", Type: decision.QuestionPredicate, Instructions: "Is it met?"},
		{Name: "intent", Type: decision.QuestionChoice, Instructions: "What is it?",
			Choices: []system1.Choice{{Value: "chat"}, {Value: "task"}}},
		{Name: "level", Type: decision.QuestionScore, Instructions: "How good?",
			Levels: []system1.Level{{Label: "poor"}, {Label: "fair"}, {Label: "good"}}},
	}
	f := &fakeReasoner{text: `{"met":{"answer":false,"confidence":0.8},` +
		`"intent":{"choice":"task","confidence":0.9},"level":{"level":"good","confidence":0.7}}`}
	resp, err := Model{Reasoner: f}.Decide(context.Background(), system1.Request{State: map[string]any{"x": 1}, Questions: qs})
	if err != nil {
		t.Fatal(err)
	}
	schema := f.got.Format.Schema
	if req := schema["required"].([]string); len(req) != 3 {
		t.Errorf("schema requires %v", req)
	}
	level := schema["properties"].(map[string]any)["level"].(map[string]any)["properties"].(map[string]any)["level"].(map[string]any)
	if enum := level["enum"].([]string); len(enum) != 3 || enum[2] != "good" {
		t.Errorf("level enum = %v", enum)
	}
	a := resp.Answers
	// A no said with 0.8 confidence is a 0.2 probability of yes.
	if p := a["met"].Probability; p < 0.19 || p > 0.21 {
		t.Errorf("met = %v, want 0.2", p)
	}
	if a["intent"].Choice != "task" || a["intent"].Confidence != 0.9 {
		t.Errorf("intent = %+v", a["intent"])
	}
	if a["level"].Score != 2 {
		t.Errorf("level score = %v, want 2 (the index of good)", a["level"].Score)
	}
	if resp.Provider != "openai/test" || resp.CostUSD != 0.01 {
		t.Errorf("provider %q, cost %v", resp.Provider, resp.CostUSD)
	}

	f.text = `{"met":{"answer":true,"confidence":1}}`
	if _, err := (Model{Reasoner: f}).Decide(context.Background(), system1.Request{State: "x", Questions: qs}); err == nil {
		t.Error("answers missing, no error")
	}
}
