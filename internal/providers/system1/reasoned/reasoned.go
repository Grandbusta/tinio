// Package reasoned answers System 1's questions with a reasoning model (any
// system2.Reasoner), as JSON to a schema made from the questions: slower and
// dearer than the Decisions API, but on any model, for a question set a small
// model gets wrong (its `model:`). Its confidences are what the model states,
// less calibrated than the Decisions API's, so a set moved here takes its
// thresholds from `api eval`.
package reasoned

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/providers/system1"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

type Model struct {
	Reasoner system2.Reasoner
}

// instructions are the same on every call, so the provider's prompt cache holds.
const instructions = "You answer questions about an input, given as JSON after the questions. " +
	"Answer each question by its name, from the input alone, as the question asks. For each, give `confidence`: " +
	"your honest probability, from 0 to 1, that your answer is right. Be as sure as the input lets you be, no more."

func (m Model) Decide(ctx context.Context, req system1.Request) (system1.Response, error) {
	state, err := json.Marshal(req.State)
	if err != nil {
		return system1.Response{}, fmt.Errorf("reasoned: %w", err)
	}
	resp, err := m.Reasoner.Reason(ctx, system2.Request{
		Instructions: instructions,
		Message:      asked(req.Questions) + "\nInput:\n" + string(state),
		Format:       &system2.Format{Name: "answers", Schema: schema(req.Questions)},
	})
	if err != nil {
		return system1.Response{}, err
	}
	var got map[string]struct {
		Answer     *bool   `json:"answer"`
		Choice     string  `json:"choice"`
		Level      string  `json:"level"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(resp.Text), &got); err != nil {
		return system1.Response{}, fmt.Errorf("reasoned: answers: %w", err)
	}
	out := system1.Response{Provider: resp.Provider, CostUSD: resp.CostUSD, Answers: map[string]decision.Answer{}}
	for _, q := range req.Questions {
		g, ok := got[q.Name]
		if !ok {
			return system1.Response{}, fmt.Errorf("reasoned: no answer to %q", q.Name)
		}
		conf := min(max(g.Confidence, 0), 1)
		switch q.Type {
		case decision.QuestionPredicate:
			p := conf
			if g.Answer == nil || !*g.Answer {
				p = 1 - conf
			}
			out.Answers[q.Name] = decision.Answer{Type: q.Type, Probability: p}
		case decision.QuestionChoice:
			out.Answers[q.Name] = decision.Answer{Type: q.Type, Choice: g.Choice, Confidence: conf,
				Probabilities: map[string]float64{g.Choice: conf}}
		case decision.QuestionScore:
			i := slices.IndexFunc(q.Levels, func(l system1.Level) bool { return l.Label == g.Level })
			if i < 0 {
				return system1.Response{}, fmt.Errorf("reasoned: %q is no level of %q", g.Level, q.Name)
			}
			out.Answers[q.Name] = decision.Answer{Type: q.Type, Score: float64(i), Confidence: conf}
		}
	}
	return out, nil
}

// asked is the questions in words, each with its choices or levels.
func asked(qs []system1.Question) string {
	var b strings.Builder
	b.WriteString("Questions, by name:\n")
	for _, q := range qs {
		switch q.Type {
		case decision.QuestionPredicate:
			fmt.Fprintf(&b, "\n%s (yes or no): %s\n", q.Name, q.Instructions)
		case decision.QuestionChoice:
			fmt.Fprintf(&b, "\n%s (pick one): %s\n", q.Name, q.Instructions)
			for _, c := range q.Choices {
				fmt.Fprintf(&b, "- %s: %s\n", c.Value, c.Description)
			}
		case decision.QuestionScore:
			fmt.Fprintf(&b, "\n%s (pick a level, lowest first): %s\n", q.Name, q.Instructions)
			for _, l := range q.Levels {
				fmt.Fprintf(&b, "- %s: %s\n", l.Label, l.Description)
			}
		}
	}
	return b.String()
}

// schema is one object a question, keyed by its name: the answer, limited to
// its choices or levels, and a confidence.
func schema(qs []system1.Question) map[string]any {
	props := map[string]any{}
	names := make([]string, 0, len(qs))
	conf := map[string]any{"type": "number", "description": "probability, 0 to 1, that the answer is right"}
	for _, q := range qs {
		var field string
		var answer map[string]any
		switch q.Type {
		case decision.QuestionPredicate:
			field, answer = "answer", map[string]any{"type": "boolean"}
		case decision.QuestionChoice:
			values := make([]string, len(q.Choices))
			for i, c := range q.Choices {
				values[i] = c.Value
			}
			field, answer = "choice", map[string]any{"type": "string", "enum": values}
		case decision.QuestionScore:
			labels := make([]string, len(q.Levels))
			for i, l := range q.Levels {
				labels[i] = l.Label
			}
			field, answer = "level", map[string]any{"type": "string", "enum": labels}
		}
		props[q.Name] = map[string]any{"type": "object", "additionalProperties": false, "required": []string{field, "confidence"},
			"properties": map[string]any{field: answer, "confidence": conf}}
		names = append(names, q.Name)
	}
	return map[string]any{"type": "object", "additionalProperties": false, "required": names, "properties": props}
}
