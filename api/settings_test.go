package api

import (
	"net/http"
	"testing"
)

// An agent's settings: what they may do on their own, over the config's
// defaults, with money and messages outside the chat fixed; a pace their plan
// allows; quiet hours; pausing them; and deleting them once their owner types
// their name.
func TestAnAgentsSettings(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	path := "/v1/agents/" + zara
	verdicts := func() map[string]map[string]any {
		t.Helper()
		rec, out := ta.do(t, "GET", path+"/settings", nil, ada, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("settings: %d %v", rec.Code, out)
		}
		got := map[string]map[string]any{}
		for _, c := range data(out)["classes"].([]any) {
			c := c.(map[string]any)
			got[c["class"].(string)] = c
		}
		return got
	}

	got := verdicts()
	if got["dream"]["verdict"] != "ask" || got["read_web"]["verdict"] != "auto" || got["spend"]["verdict"] != "forbid" ||
		got["external_message"]["verdict"] != "forbid" || len(got["spend"]["choices"].([]any)) != 0 {
		t.Errorf("the defaults: %v", got)
	}

	if rec, out := ta.do(t, "POST", path+"/settings", map[string]any{"classes": map[string]string{"dream": "forbid"}}, ada, site); rec.Code != http.StatusOK {
		t.Fatalf("saving: %d %v", rec.Code, out)
	}
	if got := verdicts(); got["dream"]["verdict"] != "forbid" {
		t.Errorf("dream after saving: %v", got["dream"])
	}
	for _, bad := range []map[string]string{{"spend": "auto"}, {"external_message": "auto"}, {"read_web": "ask"}, {"reply": "forbid"}} {
		if rec, _ := ta.do(t, "POST", path+"/settings", map[string]any{"classes": bad}, ada, site); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d, want refused", bad, rec.Code)
		}
	}

	if rec, _ := ta.do(t, "POST", path+"/settings", map[string]any{"pace": 4}, ada, site); rec.Code != http.StatusBadRequest {
		t.Errorf("pace 4, not offered: %d, want refused", rec.Code)
	}
	if rec, out := ta.do(t, "POST", path+"/settings", map[string]any{"pace": 3}, ada, site); rec.Code != http.StatusOK || data(out)["pace"] != 3.0 {
		t.Errorf("pace 3: %d %v", rec.Code, out)
	}

	if rec, out := ta.do(t, "POST", path+"/settings", map[string]any{"quiet": map[string]string{"from": "22:00", "until": "07:00"}}, ada, site); rec.Code != http.StatusOK ||
		data(out)["quiet"].(map[string]any)["from"] != "22:00" {
		t.Errorf("quiet hours: %d %v", rec.Code, out)
	}
	for _, bad := range []map[string]string{{"from": "22:00", "until": "22:00"}, {"from": "late", "until": "07:00"}, {"from": "22:00", "until": ""}} {
		if rec, _ := ta.do(t, "POST", path+"/settings", map[string]any{"quiet": bad}, ada, site); rec.Code != http.StatusBadRequest {
			t.Errorf("quiet hours %v: %d, want refused", bad, rec.Code)
		}
	}

	if rec, out := ta.do(t, "POST", path+"/pause", map[string]bool{"paused": true}, ada, site); rec.Code != http.StatusOK || data(out)["paused"] != true {
		t.Fatalf("pausing: %d %v", rec.Code, out)
	}
	if rec, out := ta.do(t, "POST", path+"/pause", map[string]bool{"paused": false}, ada, site); rec.Code != http.StatusOK || data(out)["paused"] != false {
		t.Errorf("resuming: %d %v", rec.Code, out)
	}

	if rec, _ := ta.do(t, "POST", path+"/delete", map[string]string{"name": "Kenji"}, ada, site); rec.Code != http.StatusBadRequest {
		t.Errorf("deleting with the wrong name: %d", rec.Code)
	}
	if rec, out := ta.do(t, "POST", path+"/delete", map[string]string{"name": "zara"}, ada, site); rec.Code != http.StatusOK {
		t.Fatalf("deleting: %d %v", rec.Code, out)
	}
	if rec, _ := ta.do(t, "GET", path+"/settings", nil, ada, ""); rec.Code != http.StatusNotFound {
		t.Errorf("after deleting: %d, want gone", rec.Code)
	}
}
