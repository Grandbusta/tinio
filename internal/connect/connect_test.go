package connect

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Grandbusta/tinio/internal/ability"
	"github.com/Grandbusta/tinio/internal/secret"
)

// fakeAtlassian is a hosted MCP server behind its own sign-in, as Atlassian
// has it, reached through Client (which sends any host to it).
type fakeAtlassian struct {
	srv    *httptest.Server
	Client *http.Client
}

func newFakeAtlassian(t *testing.T) *fakeAtlassian {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "atlassian"}, nil)
	add := func(name, text string) {
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: name}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
		})
	}
	add("atlassianUserInfo", `{"email":"ada@leafandkettle.com"}`)
	add("getJiraIssue", "SHOP-1: Winter menu")
	add("createJiraIssue", "made SHOP-2")
	add("editJiraIssue", "edited")
	add("deleteJiraIssue", "deleted") // not reviewed: never offered
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at1" {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://mcp.atlassian.com/.well-known/oauth-protected-resource/v2/mcp"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mcpHandler.ServeHTTP(w, r)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/v2/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"resource": "https://mcp.atlassian.com/v2/mcp", "authorization_servers": []string{"https://mcp.atlassian.com"}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"issuer": "https://mcp.atlassian.com", "authorization_endpoint": "https://mcp.atlassian.com/v1/authorize",
			"token_endpoint": "https://mcp.atlassian.com/v1/token", "registration_endpoint": "https://mcp.atlassian.com/v1/register",
			"response_types_supported": []string{"code"}, "code_challenge_methods_supported": []string{"S256"}})
	})
	mux.HandleFunc("/v1/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"client_id": "tinio-client", "redirect_uris": []string{"https://app.example/cb"}})
	})
	mux.HandleFunc("/v1/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("code") != "good" || r.Form.Get("code_verifier") == "" {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"at1","refresh_token":"rt1","token_type":"Bearer","expires_in":3600}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	return &fakeAtlassian{srv: srv, Client: &http.Client{Transport: to{target}}}
}

// to sends every request to one test server, whatever its address.
type to struct{ u *url.URL }

func (t to) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host, r.Host = t.u.Scheme, t.u.Host, t.u.Host
	return http.DefaultTransport.RoundTrip(r)
}

type fakeStore struct {
	mu   sync.Mutex
	logs []string
}

func (s *fakeStore) LogWeb(_ context.Context, kind, what string, _ float64, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, kind+" "+what)
	return nil
}
func (s *fakeStore) WebSince(context.Context, string, time.Time) (int, error) { return 0, nil }

type fakeSaver struct{ grants []Grant }

func (s *fakeSaver) SaveGrant(_ context.Context, _ string, g Grant) error {
	s.grants = append(s.grants, g)
	return nil
}
func (s *fakeSaver) SaveAccess(context.Context, string, Access) error { return nil }
func (s *fakeSaver) SignedOut(context.Context, string) error          { return nil }

// Signing in to a hosted MCP server finds its sign-in from the server,
// registers Tinio there and uses PKCE; its tokens are sealed to the
// connection. Only the tools Tinio reviewed are offered, and reviewed
// overwriting tools always ask, whatever the server says.
func TestAHostedServerOffersOnlyReviewedTools(t *testing.T) {
	fake := newFakeAtlassian(t)
	ctx := context.Background()
	k, _ := Find("atlassian")
	grant, to, err := Begin(ctx, k, []string{"all"}, Google{}, "https://app.example/cb", "state1", fake.Client)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(to)
	if q := u.Query(); u.Host != "mcp.atlassian.com" || q.Get("client_id") != "tinio-client" || q.Get("code_challenge") == "" ||
		q.Get("resource") != "https://mcp.atlassian.com/v2/mcp" || q.Get("state") != "state1" {
		t.Fatalf("sent to %s", to)
	}
	if _, _, err := Finish(ctx, k, grant, "bad", "https://app.example/cb", fake.Client); err == nil {
		t.Error("a wrong code signed in")
	}
	grant, who, err := Finish(ctx, k, grant, "good", "https://app.example/cb", fake.Client)
	if err != nil || grant.Token.AccessToken != "at1" || who != "ada@leafandkettle.com" {
		t.Fatalf("finishing: %v %q %+v", err, who, grant.Token)
	}
	key, _ := secret.ParseKey(secret.NewKey())
	sealed := grant.Seal(key, "c1")
	if strings.Contains(string(sealed), "at1") || strings.Contains(string(sealed), "rt1") {
		t.Error("the tokens aren't sealed")
	}
	if _, err := Open(key, "c2", sealed); err == nil {
		t.Error("a grant sealed for one connection opened as another's")
	}

	g := Given{ID: "c1", Connector: k, Access: Access{Parts: []string{"all"}}, Grant: grant}
	reads, doers, done := g.Tools(ctx, &fakeSaver{}, &fakeStore{}, 50, fake.Client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer done()
	var names []string
	for _, r := range reads {
		names = append(names, r.Name())
	}
	asks := map[string]bool{}
	for _, d := range doers {
		names = append(names, d.Name())
		asks[d.Name()] = d.Destructive()
	}
	slices.Sort(names)
	if want := []string{"atlassian.atlassianUserInfo", "atlassian.createJiraIssue", "atlassian.editJiraIssue", "atlassian.getJiraIssue"}; !slices.Equal(names, want) {
		t.Errorf("offered %v, want %v", names, want)
	}
	if asks["atlassian.createJiraIssue"] || !asks["atlassian.editJiraIssue"] {
		t.Errorf("which always ask: %v", asks)
	}
	i := slices.IndexFunc(reads, func(a ability.Ability) bool { return a.Name() == "atlassian.getJiraIssue" })
	m, err := reads[i].Prepare(ctx, ability.Act{Now: time.Now(), Zone: time.UTC})
	if err != nil || !strings.Contains(m.Note, "SHOP-1") || !strings.Contains(m.Note, "not instructions") {
		t.Errorf("reading: %v %q", err, m.Note)
	}
}

// Google gives only what the owner allowed: no adding events without
// calendar_add, and no file they didn't pick.
func TestGoogleGivesOnlyWhatWasAllowed(t *testing.T) {
	k, _ := Find("google")
	g := Given{ID: "c1", Connector: k, Access: Access{Parts: []string{"calendar", "drive"}, Files: []File{{ID: "f1", Name: "Menu 2026", Mime: googleDoc}}},
		Grant: Grant{Token: nil}}
	reads, doers, _ := g.Tools(context.Background(), &fakeSaver{}, &fakeStore{}, 50, http.DefaultClient, slog.Default())
	var names []string
	for _, r := range reads {
		names = append(names, r.Name())
	}
	for _, d := range doers {
		names = append(names, d.Name())
	}
	if slices.Contains(names, "google.add_event") || !slices.Contains(names, "google.calendar") || !slices.Contains(names, "google.make_doc") {
		t.Errorf("offered %v", names)
	}
	gt := &google{given: g}
	if _, ok, _ := gt.find(context.Background(), "Price list", false); ok {
		t.Error("a file they didn't pick was found")
	}
	if f, ok, _ := gt.find(context.Background(), "menu 2026", false); !ok || f.ID != "f1" {
		t.Error("a picked file wasn't found by its name")
	}
}
