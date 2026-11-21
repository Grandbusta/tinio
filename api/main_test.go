package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	tdb "github.com/Grandbusta/tinio/internal/db"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio"
	"github.com/Grandbusta/tinio/api/config"
	"github.com/Grandbusta/tinio/api/controllers"
	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/character"
	"github.com/Grandbusta/tinio/internal/host"
	"github.com/Grandbusta/tinio/internal/secret"
	"github.com/Grandbusta/tinio/internal/store"
)

const site = "http://localhost:3000"

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	setupRouter(config.Config{}, &controllers.Auth{}, &controllers.Agents{}, &controllers.Chat{}, &controllers.Today{}, &controllers.Works{}, &controllers.Growth{}, &controllers.Judgment{}, &controllers.Mind{}, &controllers.Memory{}, &controllers.Character{}, &controllers.Settings{}, &controllers.Skills{}, &controllers.Connections{}, &controllers.Keys{}, slog.New(slog.NewTextHandler(io.Discard, nil))).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

type testAPI struct {
	router      *gin.Engine
	connections *controllers.Connections
	db          *tdb.DB
	auth        *controllers.Auth
	now         time.Time
}

// newTestAPI is the API on a new database of the test's own.
func newTestAPI(t *testing.T) *testAPI {
	t.Helper()
	db, err := tdb.Open(context.Background(), filepath.Join(t.TempDir(), "tinio.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ta := &testAPI{db: db, now: time.Now().UTC()}
	cfg := config.Config{AppOrigin: site, CookieSecure: true}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	owner, err := models.Owner(context.Background(), db, ta.now)
	if err != nil {
		t.Fatal(err)
	}
	auth := &controllers.Auth{DB: db, Owner: owner, Passcode: testPasscode, Secure: true, Log: log, Now: func() time.Time { return ta.now }}
	characters, err := controllers.LoadCharacters(context.Background(), db, tinio.Files)
	if err != nil {
		t.Fatal(err)
	}
	hub := &controllers.ChatHub{DB: db, Log: log}
	ctx, stop := context.WithCancel(context.Background())
	go hub.Run(ctx)
	t.Cleanup(stop)
	chat := &controllers.Chat{DB: db, Hub: hub, PerAccount: utils.NewLimiter(2*time.Second, 30)}
	ta.auth = auth
	key, _ := secret.ParseKey(secret.NewKey())
	ta.connections = &controllers.Connections{DB: db, Key: &key, App: site, HTTP: http.DefaultClient, Log: log}
	ta.router = setupRouter(cfg, auth, &controllers.Agents{DB: db, Characters: characters}, chat, &controllers.Today{DB: db, Now: func() time.Time { return ta.now }},
		&controllers.Works{DB: db, Formats: host.Formats()}, &controllers.Growth{DB: db, Now: func() time.Time { return ta.now }},
		&controllers.Judgment{DB: db, Now: func() time.Time { return ta.now }}, &controllers.Mind{DB: db, Sets: testSets(t)},
		&controllers.Memory{DB: db, Now: func() time.Time { return ta.now }}, &controllers.Character{DB: db}, &controllers.Settings{DB: db}, &controllers.Skills{DB: db},
		ta.connections, &controllers.Keys{DB: db, Key: key}, log)
	return ta
}

// email is new in each test: the tests share one database.
func email() string { return fmt.Sprintf("ada-%d@example.com", time.Now().UnixNano()) }

func (ta *testAPI) do(t *testing.T, method, path string, body any, cookie *http.Cookie, origin string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	ta.router.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

// data is an answer's data, or nil.
func data(out map[string]any) map[string]any {
	d, _ := out["data"].(map[string]any)
	return d
}

func sessionOf(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == config.SessionCookie {
			return c
		}
	}
	return nil
}

const testPasscode = "a passcode long enough"

// On this machine the owner is let straight in. A page that rebinds its own
// name to this machine, or a request forwarded from elsewhere, isn't.
func TestThisMachineNeedsNoSignIn(t *testing.T) {
	ta := newTestAPI(t)
	me := func(host, forwarded string) int {
		t.Helper()
		req := httptest.NewRequest("GET", "/v1/me", nil)
		req.RemoteAddr, req.Host = "127.0.0.1:52000", host
		if forwarded != "" {
			req.Header.Set("X-Forwarded-For", forwarded)
		}
		rec := httptest.NewRecorder()
		ta.router.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := me("localhost:8081", ""); code != http.StatusOK {
		t.Errorf("this machine: %d", code)
	}
	if code := me("127.0.0.1:8081", "127.0.0.1"); code != http.StatusOK {
		t.Errorf("this machine through its own proxy: %d", code)
	}
	if code := me("rebound.example:8081", ""); code != http.StatusUnauthorized {
		t.Errorf("a rebound name: %d", code)
	}
	if code := me("localhost:8081", "203.0.113.9"); code != http.StatusUnauthorized {
		t.Errorf("forwarded from elsewhere: %d", code)
	}
	if rec, _ := ta.do(t, "GET", "/v1/me", nil, nil, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("another device with no session: %d", rec.Code)
	}
}

// Another device gets in with the passcode, and only with it.
func TestAnotherDeviceSignsInWithThePasscode(t *testing.T) {
	ta := newTestAPI(t)
	for _, wrong := range []string{"", "a passcode long enougH"} {
		if rec, _ := ta.do(t, "POST", "/v1/auth/passcode", map[string]string{"passcode": wrong}, nil, site); rec.Code != http.StatusUnauthorized {
			t.Errorf("passcode %q: %d", wrong, rec.Code)
		}
	}
	if rec, _ := ta.do(t, "POST", "/v1/auth/passcode", map[string]string{"passcode": testPasscode}, nil, "https://evil.example"); rec.Code != http.StatusForbidden {
		t.Errorf("from another site: %d", rec.Code)
	}
	rec, _ := ta.do(t, "POST", "/v1/auth/passcode", map[string]string{"passcode": testPasscode}, nil, site)
	session := sessionOf(rec)
	if rec.Code != http.StatusOK || session == nil {
		t.Fatalf("the passcode: %d", rec.Code)
	}
	if rec, out := ta.do(t, "GET", "/v1/me", nil, session, ""); rec.Code != http.StatusOK || data(out)["account"].(map[string]any)["id"] != ta.auth.Owner.ID {
		t.Errorf("signed in as %v (%d)", out, rec.Code)
	}
	ta.auth.Passcode = ""
	if rec, _ := ta.do(t, "POST", "/v1/auth/passcode", map[string]string{"passcode": testPasscode}, nil, site); rec.Code != http.StatusUnauthorized {
		t.Errorf("with no passcode set: %d", rec.Code)
	}
}

// signUp is a new account with a session, for the tests of who may reach
// what: there's one owner, but the API keeps every agent to its account.
func (ta *testAPI) signUp(t *testing.T) *http.Cookie {
	t.Helper()
	ctx := context.Background()
	acc := models.Account{Email: email(), CreatedAt: ta.now}
	if err := acc.Save(ctx, ta.db); err != nil {
		t.Fatal(err)
	}
	token := utils.NewToken()
	s := models.Session{AccountID: acc.ID, TokenHash: utils.TokenHash(token), CreatedAt: ta.now, ExpiresAt: ta.now.Add(config.SessionTTL)}
	if err := s.Save(ctx, ta.db); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: config.SessionCookie, Value: token}
}

func TestRaisingAnAgentFromAReadyCharacter(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)

	_, out := ta.do(t, "GET", "/v1/characters", nil, ada, "")
	chars, _ := data(out)["characters"].([]any)
	var zara map[string]any
	for _, c := range chars {
		if c.(map[string]any)["key"] == "zara" {
			zara = c.(map[string]any)
		}
	}
	if len(chars) != 6 || zara == nil || zara["toward"] == "" || len(zara["relations"].([]any)) == 0 {
		t.Fatalf("characters: %v", out)
	}

	// A pace asked for is ignored: it comes with the plan.
	rec, out := ta.do(t, "POST", "/v1/agents", map[string]any{"character": "zara", "owner_name": "Ada", "relation": "mentor", "pace": 10}, ada, site)
	agent, _ := data(out)["agent"].(map[string]any)
	if rec.Code != http.StatusCreated || agent["name"] != "Zara" || agent["relation"] != "mentor" || agent["pace"] != float64(config.Pace) {
		t.Fatalf("raising Zara: %d %v", rec.Code, out)
	}
	v, ok, err := store.New(ta.db).For(agent["id"].(string)).Self(context.Background(), "character")
	if err != nil || !ok || !strings.Contains(v.Doc, "Ada is Zara's mentor.") || strings.Contains(v.Doc, "{owner}") {
		t.Fatalf("her character, filled in for Ada: %v %v\n%s", ok, err, v.Doc)
	}
	// Only the owner's words count as the owner's, so the mind must know which speaker they are.
	var account string
	if err := ta.db.QueryRow(context.Background(), `SELECT account_id FROM agents WHERE id = $1`, agent["id"]).Scan(&account); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(v.Doc, `account: "account:`+account+`"`) {
		t.Errorf("her character doesn't know Ada as account:%s:\n%s", account, v.Doc)
	}

	if _, out := ta.do(t, "GET", "/v1/agents", nil, ada, ""); len(data(out)["agents"].([]any)) != 1 {
		t.Errorf("Ada's agents: %v", out)
	}
	if _, out := ta.do(t, "GET", "/v1/agents", nil, ta.signUp(t), ""); len(data(out)["agents"].([]any)) != 0 {
		t.Errorf("someone else sees Ada's agent: %v", out)
	}
}

func TestRaisingAnAgentChecksWhatItsGiven(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	good := map[string]any{"character": "zara", "owner_name": "Ada", "relation": "mentor", "timezone": "Africa/Lagos"}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for kk, vv := range good {
			m[kk] = vv
		}
		m[k] = v
		return m
	}

	if rec, _ := ta.do(t, "POST", "/v1/agents", good, nil, site); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: %d", rec.Code)
	}
	if rec, out := ta.do(t, "POST", "/v1/agents", with("character", "nobody"), ada, site); rec.Code != http.StatusNotFound || out["code"] != "no_such_character" {
		t.Errorf("an unknown character: %d %v", rec.Code, out)
	}
	for field, bad := range map[string]any{"relation": "father", "owner_name": " ", "timezone": "Lagos"} {
		rec, out := ta.do(t, "POST", "/v1/agents", with(field, bad), ada, site)
		if rec.Code != http.StatusBadRequest || out["code"] != "invalid" || data(out)["field"] != field {
			t.Errorf("%s %v: %d %v", field, bad, rec.Code, out)
		}
	}
}

// An edit to a ready character is a new version, which never changes an agent
// already raised; and a character someone built is theirs alone.
func TestCharactersAreVersionedAndOwned(t *testing.T) {
	ta := newTestAPI(t)
	ctx := context.Background()
	ready, err := character.Templates(tinio.Files, "characters")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { models.SyncReady(ctx, ta.db, ready, time.Now().UTC()) }) // the tests share one database
	zaraNow := func() models.Character {
		t.Helper()
		c, ok, err := models.CharacterFor(ctx, ta.db, "", "zara")
		if err != nil || !ok {
			t.Fatalf("zara: %v %v", ok, err)
		}
		return c
	}
	before := zaraNow()
	if err := models.SyncReady(ctx, ta.db, ready, ta.now); err != nil || zaraNow().Version != before.Version {
		t.Fatalf("unchanged files made a new version: %v", err)
	}

	ada := ta.signUp(t)
	raise := func(who *http.Cookie, key string) (*httptest.ResponseRecorder, string) {
		rec, out := ta.do(t, "POST", "/v1/agents", map[string]any{"character": key, "owner_name": "Ada", "relation": "friend"}, who, site)
		agent, _ := data(out)["agent"].(map[string]any) // none if refused
		id, _ := agent["id"].(string)
		return rec, id
	}
	_, first := raise(ada, "zara")

	edited := slices.Clone(ready)
	for i, tpl := range edited {
		if tpl.Key == "zara" {
			docs := maps.Clone(tpl.Docs)
			docs["character.yaml"] = bytes.Replace(docs["character.yaml"], []byte("story:\n"), []byte("story:\n  - Zara learnt to swim at forty.\n"), 1)
			edited[i].Docs = docs
		}
	}
	if err := models.SyncReady(ctx, ta.db, edited, ta.now); err != nil || zaraNow().Version != before.Version+1 {
		t.Fatalf("an edit didn't make a new version: %v", err)
	}
	_, second := raise(ada, "zara")
	for id, swims := range map[string]bool{first: false, second: true} {
		v, _, err := store.New(ta.db).For(id).Self(ctx, "character")
		if err != nil || strings.Contains(v.Doc, "swim at forty") != swims {
			t.Errorf("agent %s from the edit: %v, %v", id, !swims, err)
		}
	}

	// Bea builds her own; only she sees it or raises it.
	bea := ta.signUp(t)
	_, out := ta.do(t, "GET", "/v1/me", nil, bea, "")
	beaID := data(out)["account"].(map[string]any)["id"].(string)
	own := ulid.Make().String()
	henrik := ready[slices.IndexFunc(ready, func(t character.Template) bool { return t.Key == "henrik" })].Docs
	if _, err := ta.db.Exec(ctx, `INSERT INTO characters (key, version, account_id, visibility, template, character, judgment, created_at)
		VALUES ($1, 1, $2, 'private', $3, $4, $5, now())`, own, beaID, string(henrik["template.yaml"]), string(henrik["character.yaml"]), string(henrik["judgment.yaml"])); err != nil {
		t.Fatal(err)
	}
	listed := func(who *http.Cookie) bool {
		_, out := ta.do(t, "GET", "/v1/characters", nil, who, "")
		for _, c := range data(out)["characters"].([]any) {
			if c.(map[string]any)["key"] == own {
				return true
			}
		}
		return false
	}
	if !listed(bea) || listed(ada) {
		t.Errorf("Bea's own listed for her %v, for Ada %v", listed(bea), listed(ada))
	}
	if rec, _ := raise(ada, own); rec.Code != http.StatusNotFound {
		t.Errorf("Ada raised Bea's own: %d", rec.Code)
	}
	if rec, _ := raise(bea, own); rec.Code != http.StatusCreated {
		t.Errorf("Bea couldn't raise her own: %d %s", rec.Code, rec.Body)
	}
}
