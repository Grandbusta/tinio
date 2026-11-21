// Package pricing turns token usage into a recorded cost.
package pricing

// Price is a model's rates in USD per 1M tokens.
type Price struct {
	Input       float64
	Output      float64
	CachedInput float64
	CacheWrite  float64 // 30-minute cache writes, the default TTL
}

// OpenAI is OpenAI's prices (standard tier) for the models the platform uses;
// a model missing here is recorded at $0. The Decisions API bills gpt-6-luna's
// input and cache rates; its output is free.
var OpenAI = map[string]Price{
	"gpt-6-astra":            {Input: 10.00, Output: 50.00, CachedInput: 1.00, CacheWrite: 12.50},
	"gpt-6.2-sol":            {Input: 2.00, Output: 10.00, CachedInput: 0.10, CacheWrite: 2.50},
	"gpt-6.1-sol":            {Input: 2.00, Output: 10.00, CachedInput: 0.10, CacheWrite: 2.50},
	"gpt-6.1-terra":          {Input: 1.00, Output: 5.00, CachedInput: 0.05, CacheWrite: 1.25},
	"gpt-6-luna":             {Input: 0.10, Output: 0.50, CachedInput: 0.01, CacheWrite: 0.125},
	"text-embedding-3-small": {Input: 0.02},
}

// Usage counts tokens the way OpenAI reports them: cached and cache-write
// tokens are part of InputTokens, not on top of it.
type Usage struct {
	InputTokens      int
	CachedTokens     int
	CacheWriteTokens int
	OutputTokens     int
}

// Cost in USD. A zero Price (model not in the table) costs 0.
func (p Price) Cost(u Usage) float64 {
	uncached := max(u.InputTokens-u.CachedTokens-u.CacheWriteTokens, 0)
	return (float64(uncached)*p.Input +
		float64(u.CachedTokens)*p.CachedInput +
		float64(u.CacheWriteTokens)*p.CacheWrite +
		float64(u.OutputTokens)*p.Output) / 1e6
}
