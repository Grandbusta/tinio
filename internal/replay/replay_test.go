package replay

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/decision/questions"
	"github.com/Grandbusta/tinio/internal/providers/system1"
)

type fakeS1 map[string]decision.Answer

func (f fakeS1) Decide(_ context.Context, req system1.Request) (system1.Response, error) {
	a, ok := f[req.State.(map[string]any)["message"].(string)]
	if !ok {
		return system1.Response{}, fmt.Errorf("no answer")
	}
	return system1.Response{Provider: "fake/1", Answers: map[string]decision.Answer{"intent": a}, CostUSD: 0.01}, nil
}

var set = questions.Set{ID: "incoming_message", Version: 2, Primary: "intent",
	Questions: []system1.Question{{Name: "intent", Type: decision.QuestionChoice}}, Thresholds: questions.Thresholds{Act: 0.85, Ask: 0.5}}

func choice(c string, conf float64) decision.Answer {
	return decision.Answer{Type: decision.QuestionChoice, Choice: c, Confidence: conf}
}

// cases are ten right at 0.70 to 0.925, and one wrong at 0.62.
func cases() ([]Case, fakeS1) {
	var cs []Case
	s1 := fakeS1{}
	for i := range 10 {
		text := fmt.Sprintf("msg %d", i)
		cs = append(cs, Case{State: map[string]any{"message": text}, Label: "share"})
		s1[text] = choice("share", 0.70+float64(i)*0.025)
	}
	cs = append(cs, Case{State: map[string]any{"message": "odd"}, Label: "small_talk"})
	s1["odd"] = choice("share", 0.62)
	return cs, s1
}

func TestReplaySuggestsAThreshold(t *testing.T) {
	cs, s1 := cases()
	r, err := Run(context.Background(), s1, set, "", cs, Options{Parallel: 4})
	if err != nil {
		t.Fatal(err)
	}
	if r.Suggest != 0.65 {
		t.Errorf("suggested %.2f, want 0.65: the lowest threshold above the one wrong answer", r.Suggest)
	}
	if s := r.Summary(); s.Right != 10 || s.Acted != 4 || s.ActedRight != 4 {
		t.Errorf("summary %+v", s)
	}
}

// A run never spends much past its cap: once it's reached, no more is asked.
func TestReplayStopsAtTheCostCap(t *testing.T) {
	cs, s1 := cases()
	r, err := Run(context.Background(), s1, set, "", cs, Options{Parallel: 1, MaxCostUSD: 0.03})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 3 || r.Skipped != 8 {
		t.Errorf("asked %d, skipped %d; want 3 asked at a cent each", len(r.Rows), r.Skipped)
	}
}

func TestLoad(t *testing.T) {
	cs, err := Load(strings.NewReader("# a comment\n\n{\"state\":{\"message\":\"Pain\"},\"label\":\"share\"}\n"))
	if err != nil || len(cs) != 1 || cs[0].Label != "share" {
		t.Errorf("load: %+v %v", cs, err)
	}
	if _, err := Load(strings.NewReader("{\"state\":{\"message\":\"x\"}}\n")); err == nil {
		t.Error("a case with no label loaded")
	}
}
