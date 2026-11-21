package api

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Grandbusta/tinio/internal/host"
)

// The owner adds a folder for the agents they choose, sees what's in it,
// changes what they may do and who has it, and removes it; each change tells
// the agents it touches, who start again with it.
func TestTheOwnerAddsFolders(t *testing.T) {
	ta := newTestAPI(t)
	owner := ta.signUp(t)
	zara := ta.raiseZara(t, owner)
	dir := filepath.Join(t.TempDir(), "Leaf & Kettle")
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, "Price list.txt"), []byte("oolong"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("KEY=x"), 0o644)

	rec, out := ta.do(t, "GET", "/v1/folders/peek?path="+url.QueryEscape(dir), nil, owner, "")
	if p := data(out); rec.Code != http.StatusOK || p["files"] != 1.0 || p["skipped"] != 2.0 || p["name"] != "Leaf & Kettle" {
		t.Fatalf("peek: %d %v", rec.Code, out)
	}
	for _, bad := range []string{"relative/path", filepath.Join(dir, ".git"), filepath.Join(dir, "nowhere"), "/"} {
		if rec, _ := ta.do(t, "POST", "/v1/folders", map[string]any{"path": bad, "agents": []string{zara}}, owner, site); rec.Code != http.StatusBadRequest {
			t.Errorf("adding %q: %d", bad, rec.Code)
		}
	}
	if rec, _ := ta.do(t, "POST", "/v1/folders", map[string]any{"path": dir, "agents": []string{}}, owner, site); rec.Code != http.StatusBadRequest {
		t.Errorf("adding it for nobody: %d", rec.Code)
	}

	changed, stop := ta.db.Listen(host.Changed, 8)
	defer stop()
	told := func(what string) {
		t.Helper()
		select {
		case a := <-changed:
			if a != zara {
				t.Errorf("%s: told %s", what, a)
			}
		case <-time.After(time.Second):
			t.Errorf("%s: Zara wasn't told", what)
		}
	}
	rec, out = ta.do(t, "POST", "/v1/folders", map[string]any{"path": dir, "write": false, "agents": []string{zara, "not-hers"}}, owner, site)
	if rec.Code != http.StatusOK {
		t.Fatalf("adding: %d %v", rec.Code, out)
	}
	told("adding")
	id := data(out)["folder"].(string)
	f := data(out)["folders"].([]any)[0].(map[string]any)
	if f["write"] != false || len(f["agents"].([]any)) != 1 || f["files"] != 1.0 {
		t.Errorf("added as %v", f)
	}
	// Adding it again only gives it to whoever's chosen: it's one folder.
	if _, out := ta.do(t, "POST", "/v1/folders", map[string]any{"path": dir + "/", "agents": []string{zara}}, owner, site); len(data(out)["folders"].([]any)) != 1 {
		t.Errorf("added twice: %v", out)
	}
	told("adding again")

	_, out = ta.do(t, "POST", "/v1/folders/"+id, map[string]any{"write": true}, owner, site)
	if f := data(out)["folders"].([]any)[0].(map[string]any); f["write"] != true {
		t.Errorf("making files: %v", f)
	}
	told("letting her make files")

	if _, out := ta.do(t, "POST", "/v1/agents/"+zara+"/folders/"+id, map[string]any{"use": false}, owner, site); len(data(out)["folders"].([]any)[0].(map[string]any)["agents"].([]any)) != 0 {
		t.Errorf("taking it from her: %v", out)
	}
	told("taking it from her")

	if rec, _ := ta.do(t, "POST", "/v1/folders/nope", map[string]any{"write": true}, owner, site); rec.Code != http.StatusNotFound {
		t.Errorf("a folder that isn't theirs: %d", rec.Code)
	}
	if _, out := ta.do(t, "POST", "/v1/folders/"+id+"/remove", map[string]any{}, owner, site); len(data(out)["folders"].([]any)) != 0 {
		t.Errorf("removing: %v", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "Price list.txt")); err != nil {
		t.Error("removing a folder touched its files")
	}
}
