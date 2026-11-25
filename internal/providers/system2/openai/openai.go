// Package openai is the System 2 provider for OpenAI's Responses API.
package openai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/Grandbusta/tinio/internal/pricing"
	"github.com/Grandbusta/tinio/internal/providers/openaiclient"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

const (
	// defaultMaxOutputTokens includes reasoning. It is a few times what its
	// longest work needs, so it only stops a runaway answer.
	defaultMaxOutputTokens = 8000
	requestTimeout         = 120 * time.Second
)

type Client struct {
	api    openai.Client
	model  string
	price  pricing.Price
	effort string // reasoning effort; "" leaves the model's default
	// tier is the service tier asked for: "" the standard one; "flex" cheaper
	// but slower, falling back to the standard tier when flex has no room.
	tier string
}

func New(apiKey, model, baseURL string, price pricing.Price, effort, tier string) *Client {
	return &Client{api: openaiclient.New(apiKey, baseURL, requestTimeout), model: model, price: price, effort: effort, tier: tier}
}

// input lays the conversation out as chat turns, then the note (as
// developer context), then the message to answer, if any.
func input(req system2.Request) responses.ResponseInputParam {
	var in responses.ResponseInputParam
	for _, t := range req.History {
		role := responses.EasyInputMessageRoleUser
		if t.Role == system2.RoleAgent {
			role = responses.EasyInputMessageRoleAssistant
		}
		in = append(in, responses.ResponseInputItemParamOfMessage(t.Text, role))
	}
	if req.Note != "" {
		in = append(in, responses.ResponseInputItemParamOfMessage(req.Note, responses.EasyInputMessageRoleDeveloper))
	}
	if req.Message == "" {
		return in
	}
	return append(in, responses.ResponseInputItemParamOfMessage(req.Message, responses.EasyInputMessageRoleUser))
}

func jsonFormat(name string, schema map[string]any) responses.ResponseTextConfigParam {
	return responses.ResponseTextConfigParam{Format: responses.ResponseFormatTextConfigUnionParam{
		OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{Name: name, Schema: schema, Strict: param.NewOpt(true)}}}
}

// cacheKey names the instructions, so requests that start with the same ones
// reach the same machines and find their prefix in OpenAI's prompt cache. Its
// own key per prompt, speaker and agent, without naming any of them.
func cacheKey(instructions string) string {
	sum := sha256.Sum256([]byte(instructions))
	return "tinio-" + hex.EncodeToString(sum[:8])
}

func (c *Client) Reason(ctx context.Context, req system2.Request) (system2.Response, error) {
	max := req.MaxOutputTokens
	if max == 0 {
		max = defaultMaxOutputTokens
	}
	params := responses.ResponseNewParams{Model: c.model, Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input(req)},
		MaxOutputTokens: param.NewOpt(int64(max)), Store: param.NewOpt(false)} // false: OpenAI keeps no copy for later retrieval
	if req.Instructions != "" {
		params.Instructions = param.NewOpt(req.Instructions)
		params.PromptCacheKey = param.NewOpt(cacheKey(req.Instructions))
	}
	switch {
	case req.CollectFacts:
		params.Text = jsonFormat("reply_with_facts", system2.ReplyWithFacts)
	case req.Format != nil:
		params.Text = jsonFormat(req.Format.Name, req.Format.Schema)
	}
	if c.effort != "" {
		params.Reasoning = shared.ReasoningParam{Effort: shared.ReasoningEffort(c.effort)}
	}
	if c.tier != "" {
		params.ServiceTier = responses.ResponseNewParamsServiceTier(c.tier)
	}
	// Cut-off structured output is broken JSON, so it's no answer. Out of
	// room, it tries once more with twice as much.
	structured := req.CollectFacts || req.Format != nil
	// spent is every attempt's cost, the failed ones' too: they're billed.
	spent := system2.Response{Provider: "openai/" + c.model}
	var w *responses.Response
	for attempt := 1; ; attempt++ {
		var err error
		w, err = c.create(ctx, params, req)
		var apiErr *openai.Error
		if err != nil && params.ServiceTier == responses.ResponseNewParamsServiceTierFlex &&
			errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests {
			params.ServiceTier = "" // flex had no room, even after the SDK's retries: the standard tier
			w, err = c.create(ctx, params, req)
		}
		if err != nil {
			return spent, fmt.Errorf("openai responses: %w", err)
		}
		c.charge(&spent, w)
		if w.Error.Code != "" {
			return spent, fmt.Errorf("openai responses: %s: %s", w.Error.Code, w.Error.Message)
		}
		if w.Status != responses.ResponseStatusIncomplete || !structured {
			break
		}
		if attempt == 1 && w.IncompleteDetails.Reason == "max_output_tokens" {
			params.MaxOutputTokens = param.NewOpt(params.MaxOutputTokens.Value * 2)
			continue
		}
		return spent, fmt.Errorf("openai responses: %w (%s)", system2.ErrCutOff, w.IncompleteDetails.Reason)
	}

	var text, refusal strings.Builder
	for _, o := range w.Output {
		if o.Type != "message" {
			continue
		}
		for _, part := range o.Content {
			switch part.Type {
			case "output_text":
				text.WriteString(part.Text)
			case "refusal":
				refusal.WriteString(part.Refusal)
			}
		}
	}
	reply := strings.TrimSpace(text.String())
	out := system2.Response{Text: reply}
	switch {
	case refusal.Len() > 0:
		out.Text = strings.TrimSpace(refusal.String()) // a refusal is plain text, never the JSON format
	case req.CollectFacts && reply != "":
		var err error
		if out, err = system2.ParseFacts(reply); err != nil {
			return spent, fmt.Errorf("openai responses: %w", err)
		}
	}
	out.Provider, out.Usage, out.CostUSD = "openai/"+w.Model, spent.Usage, spent.CostUSD
	if len(out.Looks) > 0 {
		return system2.Response{Provider: out.Provider, Looks: out.Looks, Usage: out.Usage, CostUSD: out.CostUSD}, nil
	}
	if out.Text == "" {
		reason := string(w.Status)
		if w.IncompleteDetails.Reason != "" {
			reason += " (" + string(w.IncompleteDetails.Reason) + ")"
		}
		return spent, errors.New("openai responses: no reply text, status " + reason)
	}
	return out, nil
}

// create asks for a response, streamed when req has a Draft: it's told the
// reply as it's written.
func (c *Client) create(ctx context.Context, params responses.ResponseNewParams, req system2.Request) (*responses.Response, error) {
	if req.Draft == nil || req.Format != nil {
		return c.api.Responses.New(ctx, params)
	}
	stream := c.api.Responses.NewStreaming(ctx, params)
	defer stream.Close()
	var written strings.Builder
	shown := ""
	for stream.Next() {
		ev := stream.Current()
		switch ev.Type {
		case "response.output_text.delta":
			written.WriteString(ev.Delta)
			so := written.String()
			if req.CollectFacts {
				so = system2.ReplySoFar(so)
			}
			if so = strings.TrimSpace(so); so != "" && so != shown {
				shown = so
				req.Draft(so)
			}
		case "response.completed", "response.incomplete", "response.failed":
			return &ev.Response, nil
		case "error":
			return nil, fmt.Errorf("%s: %s", ev.Code, ev.Message)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("the stream ended without a response")
}

// charge adds what w cost to spent.
func (c *Client) charge(spent *system2.Response, w *responses.Response) {
	u := w.Usage
	spent.Usage.InputTokens += int(u.InputTokens)
	spent.Usage.CachedTokens += int(u.InputTokensDetails.CachedTokens)
	spent.Usage.OutputTokens += int(u.OutputTokens)
	spent.Usage.ReasoningTokens += int(u.OutputTokensDetails.ReasoningTokens)
	spent.CostUSD += c.price.Cost(pricing.Usage{
		InputTokens: int(u.InputTokens), CachedTokens: int(u.InputTokensDetails.CachedTokens),
		CacheWriteTokens: int(u.InputTokensDetails.CacheWriteTokens), OutputTokens: int(u.OutputTokens),
	})
}
