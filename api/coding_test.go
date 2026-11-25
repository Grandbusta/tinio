package api

import (
	"net/http"
	"testing"
)

// How coding jobs run starts as Tinio's defaults (Opus 5.5 on a copy, checked,
// 30 minutes, 80 steps); the owner changes any of it, within limits.
func TestCodingSettings(t *testing.T) {
	ta := newTestAPI(t)
	owner := ta.signUp(t)
	fakeClaude(t, true)

	_, out := ta.do(t, "GET", "/v1/coding", nil, owner, "")
	s := data(out)["settings"].(map[string]any)
	if s["coder"] != "claude:claude-opus-5-5" || s["copy"] != true || s["review"] != true || s["minutes"] != 30.0 || s["max_steps"] != 80.0 {
		t.Fatalf("defaults: %v", s)
	}
	if c := data(out)["claude"].(map[string]any); c["usable"] != true {
		t.Errorf("claude: %v", c)
	}

	rec, out := ta.do(t, "POST", "/v1/coding", map[string]any{"coder": "claude:claude-sonnet-5-5", "minutes": 45, "review": false}, owner, site)
	s = data(out)["settings"].(map[string]any)
	if rec.Code != http.StatusOK || s["coder"] != "claude:claude-sonnet-5-5" || s["minutes"] != 45.0 || s["review"] != false || s["copy"] != true {
		t.Fatalf("changing: %d %v", rec.Code, s)
	}

	for _, bad := range []map[string]any{{"coder": "gpt-6-luna"}, {"coder": "claude:claude-unknown"}, {"minutes": 1}, {"max_steps": 5000}} {
		if rec, _ := ta.do(t, "POST", "/v1/coding", bad, owner, site); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d", bad, rec.Code)
		}
	}
	if rec, _ := ta.do(t, "POST", "/v1/coding", map[string]any{"minutes": 60}, owner, ""); rec.Code == http.StatusOK {
		t.Error("a change from another site was taken")
	}
}
