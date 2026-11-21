package config

import "testing"

// Every agent runs on the models the environment names, and by default asks
// before committing to anyone, never spends, and sends nothing outside the chat.
func TestEveryAgentsDefaults(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-x")
	t.Setenv("OPENAI_MODEL", "gpt-6.1-terra")
	t.Setenv("OPENAI_DECISION_MODEL", "")
	t.Setenv("OPENAI_DAY_MODEL", "gpt-6-luna")
	cfg := Load()
	if cfg.Reasoning.Model != "gpt-6.1-terra" || cfg.Reasoning.APIKey != "sk-x" || cfg.Decision.Model != "" || cfg.DailyLife.Model != "gpt-6-luna" {
		t.Errorf("models: %+v %+v %q", cfg.Reasoning.Provider, cfg.Decision.Provider, cfg.DailyLife.Model)
	}
	p, err := cfg.Policy()
	if err != nil {
		t.Fatal(err)
	}
	if p.Classes["commit"] != "ask" || p.Classes["spend"] != "forbid" || p.Classes["external_message"] != "forbid" || p.AskExpires <= 0 {
		t.Errorf("policy = %+v", p)
	}
}
