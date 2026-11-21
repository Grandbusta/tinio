package api

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Grandbusta/tinio/api/models"
)

// The owner's key is kept sealed and only ever shown masked; setting it tells
// the agents, who start again on it.
func TestTheOwnersKeyIsSealedAndShownMasked(t *testing.T) {
	ta := newTestAPI(t)
	t.Setenv("OPENAI_API_KEY", "")
	owner := ta.signUp(t)
	const key = "sk-test-0123456789abcdefghijklmnop"
	changed, stop := ta.db.Listen(models.KeysChanged, 1)
	defer stop()

	if rec, _ := ta.do(t, "POST", "/v1/keys", map[string]any{"openai": "not a key"}, owner, site); rec.Code != http.StatusBadRequest {
		t.Errorf("not a key: %d", rec.Code)
	}
	rec, out := ta.do(t, "POST", "/v1/keys", map[string]any{"openai": key}, owner, site)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), key) {
		t.Fatalf("setting the key: %d %s", rec.Code, rec.Body)
	}
	if v := data(out)["openai"].(map[string]any); v["set"] != true || v["shown"] != "sk-…mnop" {
		t.Errorf("shown as %v", v)
	}
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Error("the agents weren't told")
	}
	var sealed []byte
	if err := ta.db.QueryRow(context.Background(), `SELECT sealed FROM keys WHERE name = $1`, models.KeyOpenAI).Scan(&sealed); err != nil ||
		bytes.Contains(sealed, []byte(key)) {
		t.Errorf("kept unsealed: %v", err)
	}
	if got, err := models.OpenKey(context.Background(), ta.db, *ta.connections.Key, models.KeyOpenAI); err != nil || got != key {
		t.Errorf("opened %q %v", got, err)
	}
}
