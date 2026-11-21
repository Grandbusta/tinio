// Package system1 defines the provider-neutral interface to fast decision
// models. Each provider lives in its own subpackage.
package system1

import (
	"context"
	"fmt"

	"github.com/Grandbusta/tinio/internal/decision"
)

type DecisionModel interface {
	Decide(ctx context.Context, req Request) (Response, error)
}

// ByModel sends each request to the model its questions name, or to Default
// when they name none. A request's questions come from one set, so share one.
type ByModel struct {
	Default DecisionModel
	Models  map[string]DecisionModel
}

func (b ByModel) Decide(ctx context.Context, req Request) (Response, error) {
	if len(req.Questions) == 0 || req.Questions[0].Model == "" {
		return b.Default.Decide(ctx, req)
	}
	m, ok := b.Models[req.Questions[0].Model]
	if !ok {
		return Response{}, fmt.Errorf("system1: no client for model %q", req.Questions[0].Model)
	}
	return m.Decide(ctx, req)
}

type Request struct {
	// State is the evidence every question is about: a string, or any value
	// that marshals to a JSON object (or array of objects).
	State     any
	Questions []Question // independent questions, all answered in one call
}

type Question struct {
	Name         string                `yaml:"name"`
	Type         decision.QuestionType `yaml:"type"`
	Instructions string                `yaml:"instructions"`
	Choices      []Choice              `yaml:"choices"` // choice only, 2–255
	Levels       []Level               `yaml:"levels"`  // score only, 2–10, lowest first
	// Model answers it, from its set's `model:`; "" the Decisions API. ByModel
	// routes on it.
	Model string `yaml:"-"`
}

type Choice struct {
	Value       string `yaml:"value"`
	Description string `yaml:"description"`
}

type Level struct {
	Label       string `yaml:"label"`
	Description string `yaml:"description"`
}

type Response struct {
	Provider string
	Answers  map[string]decision.Answer // by question name
	Usage    Usage
	CostUSD  float64
}

type Usage struct {
	InputTokens  int
	CachedTokens int
}
