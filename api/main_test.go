package main

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
	"sync"
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
	setupRouter(config.Config{}, &controllers.Auth{}, &controllers.Agents{}, &controllers.Chat{}, &controllers.Today{}, &controllers.Works{}, &controllers.Growth{}, &controllers.Judgment{}, &controllers.Mind{}, &controllers.Memory{}, &controllers.Character{}, &controllers.Settings{}, &controllers.Skills{}, &controllers.Connections{}, slog.New(slog.NewTextHandler(io.Discard, nil))).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

// inbox keeps what would have been emailed.
type inbox struct {
	mu   sync.Mutex
	sent []utils.Message
}

func (b *inbox) Send(_ context.Context, m utils.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent = append(b.sent, m)
	return nil
}

// code is the last code sent to email.
func (b *inbox) code(t *testing.T, email string) string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.sent) - 1; i >= 0; i-- {
		if b.sent[i].To == email {
			return strings.TrimPrefix(b.sent[i].Subject, "Your Tinio code: ")
		}
	}
	t.Fatalf("no code sent to %s", email)
	return ""
}

type testAPI struct {
	router      *gin.Engine
	connections *controllers.Connections
	db          *tdb.DB
	inbox       *inbox
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
	ta := &testAPI{db: db, inbox: &inbox{}, now: time.Now().UTC()}
	cfg := config.Config{AppOrigin: site, CookieSecure: true, SigninSecret: []byte(strings.Repeat("s", 32))}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	auth := &controllers.Auth{DB: db, Mail: ta.inbox, Secret: cfg.SigninSecret, Secure: true, Log: log,
		CodesPerEmail: utils.NewLimiter(3*time.Minute, 5), Now: func() time.Time { return ta.now }}
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
		ta.connections, log)
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

func (ta *testAPI) sendCode(t *testing.T, to string) string {
	t.Helper()
	if rec, _ := ta.do(t, "POST", "/v1/auth/code", map[string]string{"email": to}, nil, site); rec.Code != http.StatusAccepted {
		t.Fatalf("sending a code: %d %s", rec.Code, rec.Body)
	}
	return ta.inbox.code(t, to)
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

func TestSigningUpThenIn(t *testing.T) {
	ta := newTestAPI(t)
	to := email()

	rec, out := ta.do(t, "POST", "/v1/auth/verify", map[string]string{"email": strings.ToUpper(to), "code": ta.sendCode(t, to)}, nil, site)
	cookie := sessionOf(rec)
	if rec.Code != http.StatusOK || out["status"] != "success" || data(out)["new"] != true || cookie == nil || !cookie.HttpOnly || !cookie.Secure {
		t.Fatalf("signing up: %d %v, cookie %+v", rec.Code, out, cookie)
	}
	id := data(out)["account"].(map[string]any)["id"]
	if rec, out := ta.do(t, "GET", "/v1/me", nil, cookie, ""); rec.Code != http.StatusOK || data(out)["account"].(map[string]any)["email"] != to {
		t.Fatalf("me: %d %v", rec.Code, out)
	}

	if rec, _ := ta.do(t, "POST", "/v1/auth/signout", nil, cookie, site); rec.Code != http.StatusOK {
		t.Fatalf("signing out: %d", rec.Code)
	}
	if rec, _ := ta.do(t, "GET", "/v1/me", nil, cookie, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the session still works after signing out: %d", rec.Code)
	}

	rec, out = ta.do(t, "POST", "/v1/auth/verify", map[string]string{"email": to, "code": ta.sendCode(t, to)}, nil, site)
	if rec.Code != http.StatusOK || data(out)["new"] != false || data(out)["account"].(map[string]any)["id"] != id {
		t.Fatalf("signing in again: %d %v, want the same account", rec.Code, out)
	}
}

func TestWrongCodesLockTheCode(t *testing.T) {
	ta := newTestAPI(t)
	to := email()
	code := ta.sendCode(t, to)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for left := config.MaxTries - 1; left > 0; left-- {
		rec, out := ta.do(t, "POST", "/v1/auth/verify", map[string]string{"email": to, "code": wrong}, nil, site)
		if rec.Code != http.StatusBadRequest || out["status"] != "error" || out["code"] != "wrong_code" || data(out)["left"] != float64(left) {
			t.Fatalf("a wrong code: %d %v, want %d tries left", rec.Code, out, left)
		}
	}
	if _, out := ta.do(t, "POST", "/v1/auth/verify", map[string]string{"email": to, "code": wrong}, nil, site); out["code"] != "too_many_tries" {
		t.Fatalf("the last wrong try: %v", out)
	}
	if rec, out := ta.do(t, "POST", "/v1/auth/verify", map[string]string{"email": to, "code": code}, nil, site); out["code"] != "too_many_tries" || sessionOf(rec) != nil {
		t.Fatalf("the right code after too many tries signed in: %v", out)
	}
}

func TestOnlyTheNewestUnexpiredCodeWorks(t *testing.T) {
	ta := newTestAPI(t)
	to := email()
	first := ta.sendCode(t, to)
	second := ta.sendCode(t, to)
	if first != second {
		if _, out := ta.do(t, "POST", "/v1/auth/verify", map[string]string{"email": to, "code": first}, nil, site); out["code"] != "wrong_code" {
			t.Fatalf("a replaced code: %v", out)
		}
	}
	ta.now = ta.now.Add(config.CodeTTL)
	if _, out := ta.do(t, "POST", "/v1/auth/verify", map[string]string{"email": to, "code": second}, nil, site); out["code"] != "expired" {
		t.Fatalf("an expired code: %v", out)
	}

	ta.now = ta.now.Add(time.Minute)
	code := ta.sendCode(t, to)
	if rec, _ := ta.do(t, "POST", "/v1/auth/verify", map[string]string{"email": to, "code": code}, nil, site); rec.Code != http.StatusOK {
		t.Fatalf("a fresh code: %d", rec.Code)
	}
	if _, out := ta.do(t, "POST", "/v1/auth/verify", map[string]string{"email": to, "code": code}, nil, site); out["code"] != "expired" {
		t.Fatalf("a code used twice: %v", out)
	}
}

// 000000 stands in for the code sent only when the development code is on.
func TestTheDevelopmentCode(t *testing.T) {
	ta := newTestAPI(t)
	to := email()
	if ta.sendCode(t, to) == "000000" {
		t.Skip("the code sent happens to be 000000")
	}
	if rec, _ := ta.do(t, "POST", "/v1/auth/verify", map[string]string{"email": to, "code": "000000"}, nil, site); sessionOf(rec) != nil {
		t.Fatal("000000 signed in with the development code off")
	}
	ta.auth.DevCode = true
	ta.sendCode(t, to)
	if rec, _ := ta.do(t, "POST", "/v1/auth/verify", map[string]string{"email": to, "code": "000000"}, nil, site); sessionOf(rec) == nil {
		t.Fatalf("000000 didn't sign in with the development code on: %d %s", rec.Code, rec.Body)
	}
}

func TestNoAnswerSaysWhetherAnEmailHasAnAccount(t *testing.T) {
	ta := newTestAPI(t)
	known := email()
	ta.do(t, "POST", "/v1/auth/verify", map[string]string{"email": known, "code": ta.sendCode(t, known)}, nil, site)

	a, _ := ta.do(t, "POST", "/v1/auth/code", map[string]string{"email": known}, nil, site)
	b, _ := ta.do(t, "POST", "/v1/auth/code", map[string]string{"email": email()}, nil, site)
	if a.Code != b.Code || a.Body.String() != b.Body.String() {
		t.Fatalf("known: %d %s; unknown: %d %s", a.Code, a.Body, b.Code, b.Body)
	}
}

func TestOnlyOurSiteCanSignIn(t *testing.T) {
	ta := newTestAPI(t)
	to := email()
	for _, origin := range []string{"https://evil.example", ""} {
		if rec, _ := ta.do(t, "POST", "/v1/auth/code", map[string]string{"email": to}, nil, origin); rec.Code != http.StatusForbidden {
			t.Errorf("origin %q: %d, want refused", origin, rec.Code)
		}
	}
	if len(ta.inbox.sent) != 0 {
		t.Errorf("a code was sent for another site's request")
	}
}

func TestCodesAreLimitedPerEmail(t *testing.T) {
	ta := newTestAPI(t)
	to := email()
	for i := 0; i < 5; i++ {
		ta.sendCode(t, to)
	}
	if rec, out := ta.do(t, "POST", "/v1/auth/code", map[string]string{"email": to}, nil, site); rec.Code != http.StatusTooManyRequests || out["code"] != "slow_down" {
		t.Fatalf("a sixth code in a row: %d %v", rec.Code, out)
	}
}

// signUp makes a new account and returns its session cookie.
func (ta *testAPI) signUp(t *testing.T) *http.Cookie {
	t.Helper()
	to := email()
	rec, _ := ta.do(t, "POST", "/v1/auth/verify", map[string]string{"email": to, "code": ta.sendCode(t, to)}, nil, site)
	if c := sessionOf(rec); c != nil {
		return c
	}
	t.Fatalf("signing up: %d %s", rec.Code, rec.Body)
	return nil
}

// onPlan puts the signed-in account on plan (a new one is on free, with room for one agent).
func (ta *testAPI) onPlan(t *testing.T, cookie *http.Cookie, plan string) {
	t.Helper()
	_, out := ta.do(t, "GET", "/v1/me", nil, cookie, "")
	id := data(out)["account"].(map[string]any)["id"]
	if _, err := ta.db.Exec(context.Background(), `UPDATE accounts SET plan = $1 WHERE id = $2`, plan, id); err != nil {
		t.Fatal(err)
	}
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
	ta.onPlan(t, ada, "pro")
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
