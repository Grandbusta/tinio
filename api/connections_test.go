package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeSignIn is a hosted MCP server's sign-in (Atlassian's), reached through
// the returned client whatever the address.
func fakeSignIn(t *testing.T) *http.Client {
	t.Helper()
	mux := http.NewServeMux()
	jsonOf := func(v any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(v)
		}
	}
	mux.HandleFunc("/v2/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://mcp.atlassian.com/.well-known/oauth-protected-resource/v2/mcp"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/v2/mcp", jsonOf(map[string]any{"resource": "https://mcp.atlassian.com/v2/mcp", "authorization_servers": []string{"https://mcp.atlassian.com"}}))
	mux.HandleFunc("/.well-known/oauth-authorization-server", jsonOf(map[string]any{"issuer": "https://mcp.atlassian.com",
		"authorization_endpoint": "https://mcp.atlassian.com/v1/authorize", "token_endpoint": "https://mcp.atlassian.com/v1/token",
		"registration_endpoint": "https://mcp.atlassian.com/v1/register", "response_types_supported": []string{"code"},
		"code_challenge_methods_supported": []string{"S256"}}))
	mux.HandleFunc("/v1/register", jsonOf(map[string]any{"client_id": "tinio-client", "redirect_uris": []string{site + "/api/v1/connections/callback"}}))
	mux.HandleFunc("/v1/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"at1","refresh_token":"rt1","token_type":"Bearer","expires_in":3600}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	return &http.Client{Transport: roundTo{target}}
}

type roundTo struct{ u *url.URL }

func (t roundTo) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host, r.Host = t.u.Scheme, t.u.Host, t.u.Host
	return http.DefaultTransport.RoundTrip(r)
}

// Connecting signs in at the service and back, as the account that began
// it; the tokens are sealed and never answered; only the account's own
// agents can be given it, and no one else sees or removes it.
func TestConnectingAnAccountForChosenAgents(t *testing.T) {
	ta := newTestAPI(t)
	ta.connections.HTTP = fakeSignIn(t)
	ana, bo := ta.signUp(t), ta.signUp(t)
	mei, bosAgent := ta.raiseZara(t, ana), ta.raiseZara(t, bo)

	rec, out := ta.do(t, "POST", "/v1/connections/start", map[string]any{"connector": "atlassian", "parts": []string{"all"}, "agent": mei}, ana, site)
	if rec.Code != http.StatusOK {
		t.Fatalf("starting: %d %v", rec.Code, out)
	}
	to, _ := url.Parse(data(out)["url"].(string))
	state := to.Query().Get("state")
	if to.Query().Get("code_challenge") == "" || state == "" {
		t.Fatalf("sent to %s", to)
	}

	if rec, _ := ta.do(t, "GET", "/v1/connections/callback?code=good&state="+state, nil, bo, ""); !strings.Contains(rec.Header().Get("Location"), "failed=expired") {
		t.Errorf("another account finished the sign-in: %s", rec.Header().Get("Location"))
	}
	rec, _ = ta.do(t, "GET", "/v1/connections/callback?code=good&state="+state, nil, ana, "")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/connections/atlassian?connected=") {
		t.Fatalf("finishing: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	rec, out = ta.do(t, "GET", "/v1/connections", nil, ana, "")
	conns := data(out)["connections"].([]any)
	if len(conns) != 1 || strings.Contains(rec.Body.String(), "at1") || strings.Contains(rec.Body.String(), "rt1") {
		t.Fatalf("the connections answered: %s", rec.Body)
	}
	id := conns[0].(map[string]any)["id"].(string)
	var sealed []byte
	ta.db.QueryRow(context.Background(), `SELECT secret FROM connections WHERE id = $1`, id).Scan(&sealed)
	if len(sealed) == 0 || strings.Contains(string(sealed), "at1") {
		t.Error("the tokens aren't sealed")
	}
	if agents := conns[0].(map[string]any)["agents"].([]any); len(agents) != 1 || agents[0] != mei {
		t.Errorf("not given to the agent it was begun from: %v", agents)
	}

	if rec, _ := ta.do(t, "POST", "/v1/connections/"+id+"/agents", map[string]any{"agents": []string{bosAgent}}, ana, site); rec.Code != http.StatusNotFound {
		t.Errorf("given to someone else's agent: %d", rec.Code)
	}
	if rec, _ := ta.do(t, "POST", "/v1/connections/"+id+"/remove", nil, bo, site); rec.Code != http.StatusNotFound {
		t.Errorf("someone else removed it: %d", rec.Code)
	}
	if rec, _ := ta.do(t, "POST", "/v1/agents/"+bosAgent+"/tools/"+id, map[string]bool{"use": true}, bo, site); rec.Code != http.StatusNotFound {
		t.Errorf("someone else's agent was given it: %d", rec.Code)
	}
	_, out = ta.do(t, "GET", "/v1/agents/"+mei+"/tools", nil, ana, "")
	if given := data(out)["given"].([]any); len(given) != 1 || data(out)["changes"] != "ask" {
		t.Errorf("their tools: %v", out)
	}
	if rec, _ := ta.do(t, "POST", "/v1/connections/"+id+"/remove", nil, ana, site); rec.Code != http.StatusOK {
		t.Errorf("removing: %d", rec.Code)
	}
}
