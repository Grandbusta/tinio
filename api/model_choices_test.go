package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/Grandbusta/tinio/api/models"
)

// Each job's model is Tinio's default until the owner picks another of the
// priced ones; picking tells the agents, who start again on it; one set in the
// environment wins and can't be changed in the app.
func TestTheOwnerChoosesTheModels(t *testing.T) {
	ta := newTestAPI(t)
	for _, j := range models.Jobs {
		t.Setenv(j.Env, "")
	}
	owner := ta.signUp(t)
	job := func(out map[string]any, name string) map[string]any {
		t.Helper()
		for _, j := range data(out)["jobs"].([]any) {
			if j := j.(map[string]any); j["job"] == name {
				return j
			}
		}
		t.Fatalf("no job %s", name)
		return nil
	}

	_, out := ta.do(t, "GET", "/v1/models", nil, owner, "")
	if r := job(out, "reply"); r["model"] != "gpt-6.1-terra" || r["from"] != "default" || len(r["models"].([]any)) != 5 {
		t.Errorf("replies to start with: %v", r)
	}
	if w := job(out, "work"); w["model"] != "" {
		t.Errorf("works to start with: %v", w)
	}

	changed, stop := ta.db.Listen(models.KeysChanged, 1)
	defer stop()
	rec, out := ta.do(t, "POST", "/v1/models", map[string]any{"job": "reply", "model": "gpt-6.1-sol"}, owner, site)
	if r := job(out, "reply"); rec.Code != http.StatusOK || r["model"] != "gpt-6.1-sol" || r["from"] != "app" {
		t.Fatalf("choosing: %d %v", rec.Code, r)
	}
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Error("the agents weren't told")
	}

	for _, bad := range []map[string]any{
		{"job": "reply", "model": "gpt-4o"},
		{"job": "decision", "model": "gpt-6.1-terra"},
		{"job": "sleeping", "model": "gpt-6-luna"},
	} {
		if rec, _ := ta.do(t, "POST", "/v1/models", bad, owner, site); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d", bad, rec.Code)
		}
	}

	if _, out := ta.do(t, "POST", "/v1/models", map[string]any{"job": "reply", "model": ""}, owner, site); job(out, "reply")["from"] != "default" {
		t.Errorf("back to the default: %v", job(out, "reply"))
	}

	t.Setenv("OPENAI_DAY_MODEL", "gpt-6.2-sol")
	_, out = ta.do(t, "GET", "/v1/models", nil, owner, "")
	if d := job(out, "day"); d["model"] != "gpt-6.2-sol" || d["from"] != "environment" {
		t.Errorf("from the environment: %v", d)
	}
	if rec, _ := ta.do(t, "POST", "/v1/models", map[string]any{"job": "day", "model": "gpt-6-luna"}, owner, site); rec.Code != http.StatusBadRequest {
		t.Errorf("changing one set in the environment: %d", rec.Code)
	}
}
