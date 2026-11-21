package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/Grandbusta/tinio/internal/host"
)

// Their owner changes who they are: each change a new version, checked as any
// character is, told to whoever runs them; and goes back to an earlier one.
func TestTheOwnerChangesTheirCharacterAndGoesBack(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	get := func() map[string]any {
		t.Helper()
		rec, out := ta.do(t, "GET", "/v1/agents/"+zara+"/character", nil, ada, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("character: %d %v", rec.Code, out)
		}
		return data(out)
	}
	first := get()
	raised := first["versions"].([]any)[0].(map[string]any)["id"].(string)
	if first["name"] != "Zara" || len(first["story"].([]any)) == 0 || len(first["versions"].([]any)) != 1 {
		t.Fatalf("as raised: %v", first)
	}

	changed, stop := ta.db.Listen(host.Changed, 8)
	defer stop()
	save := func(body map[string]any) int {
		rec, _ := ta.do(t, "POST", "/v1/agents/"+zara+"/character", body, ada, site)
		return rec.Code
	}
	if code := save(map[string]any{"story": []string{"  Zara writes hooks   for a living. ", ""}, "personality": []string{"Dry"},
		"day": map[string]any{"plan_at": "07:30"}}); code != http.StatusOK {
		t.Fatalf("saving: %d", code)
	}
	select {
	case agent := <-changed:
		if agent != zara {
			t.Errorf("told %q, not her", agent)
		}
	case <-time.After(5 * time.Second):
		t.Error("whoever runs her wasn't told")
	}
	now := get()
	if s := now["story"].([]any); len(s) != 1 || s[0] != "Zara writes hooks for a living." || now["day"].(map[string]any)["plan_at"] != "07:30" ||
		now["versions"].([]any)[0].(map[string]any)["source"] != "edit" {
		t.Errorf("after saving: %v", now)
	}
	if code := save(map[string]any{"story": []string{"x"}, "day": map[string]any{"plan_at": "8am"}}); code != http.StatusBadRequest {
		t.Errorf("a day that isn't one: %d", code)
	}
	if code := save(map[string]any{"story": []string{" "}}); code != http.StatusBadRequest {
		t.Errorf("no story: %d", code)
	}

	if rec, out := ta.do(t, "POST", "/v1/agents/"+zara+"/character/versions/"+raised, nil, ada, site); rec.Code != http.StatusOK {
		t.Fatalf("going back: %d %v", rec.Code, out)
	}
	back := get()
	if len(back["story"].([]any)) != len(first["story"].([]any)) || back["versions"].([]any)[0].(map[string]any)["source"] != "rollback" {
		t.Errorf("after going back: %v", back)
	}
}
