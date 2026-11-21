package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/Grandbusta/tinio/api"
	"github.com/Grandbusta/tinio/api/middlewares"
)

// app is what the desktop app adds to Tinio: staying open when its window
// closes, opening at login, telling you when an agent writes while you're
// elsewhere, and saving their files where you choose. Its routes are under
// /api/app, the window's alone.
type app struct {
	t   *api.Tinio
	log *slog.Logger

	ctx      atomic.Pointer[context.Context] // Wails', once it has started
	quitting atomic.Bool                     // Quit was asked for: closing quits
	focused  atomic.Bool                     // the window is in front of you
	notify   atomic.Bool                     // notifications can be sent
	refused  atomic.Bool                     // one wasn't, and that's been logged

	mu   sync.Mutex
	told map[string]bool // rows already notified (a settled ask comes again)
}

func newApp(t *api.Tinio, log *slog.Logger) *app {
	a := &app{t: t, log: log, told: map[string]bool{}}
	a.focused.Store(!openedAtLogin())
	return a
}

func (a *app) wails() context.Context {
	if c := a.ctx.Load(); c != nil {
		return *c
	}
	return nil
}

// started is Wails starting: the window's events, notifications, and the
// chats as events.
func (a *app) started(ctx context.Context) {
	a.ctx.Store(&ctx)
	runtime.EventsOn(ctx, "focus", func(data ...any) {
		if len(data) > 0 {
			f, _ := data[0].(bool)
			a.focused.Store(f)
		}
	})
	if err := runtime.InitializeNotifications(ctx); err != nil {
		a.log.Info("no notifications", "why", err)
	} else {
		a.notify.Store(true)
		runtime.OnNotificationResponse(ctx, func(r runtime.NotificationResult) {
			if r.Error != nil {
				return
			}
			agent, _ := r.Response.UserInfo["agent"].(string)
			a.show()
			if agent != "" {
				runtime.EventsEmit(ctx, "open", agent)
			}
		})
		if goruntime.GOOS == "darwin" {
			go func() {
				if ok, err := runtime.CheckNotificationAuthorization(ctx); err == nil && !ok {
					runtime.RequestNotificationAuthorization(ctx)
				}
			}()
		}
	}
	go chatEvents(ctx, a)
}

// show brings the window back, wherever closing or login put it.
func (a *app) show() {
	ctx := a.wails()
	if ctx == nil {
		return
	}
	runtime.Show(ctx)
	runtime.WindowUnminimise(ctx)
	runtime.WindowShow(ctx)
}

// beforeClose keeps Tinio running when its window is closed: on macOS Wails
// hides the app (HideWindowOnClose), and this is only ever quitting; elsewhere,
// with no tray to go back to, the window goes to the taskbar instead. Quitting
// itself (Quit on You, or the app menu) goes through.
func (a *app) beforeClose(ctx context.Context) (prevent bool) {
	if goruntime.GOOS == "darwin" || a.quitting.Load() {
		return false
	}
	runtime.WindowMinimise(ctx)
	return true
}

// chatHappened is a row said in a chat: one of an agent's, while you're
// elsewhere, is a notification, once, and opens their chat when clicked.
func (a *app) chatHappened(event string, data any) {
	if event != "chat" || !a.notify.Load() || a.focused.Load() {
		return
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	var e struct {
		Agent string `json:"agent"`
		Row   struct {
			ID        string  `json:"id"`
			Author    string  `json:"author"`
			Kind      string  `json:"kind"`
			Text      string  `json:"text"`
			SettledAt *string `json:"settled_at"`
			Payload   struct {
				Title string `json:"title"`
				Name  string `json:"name"`
			} `json:"payload"`
		} `json:"row"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Row.Author != "agent" || e.Row.SettledAt != nil {
		return
	}
	body := e.Row.Text
	switch e.Row.Kind {
	case "message":
	case "ask", "choice":
		if e.Row.Payload.Title != "" {
			body = e.Row.Payload.Title
		}
	case "file":
		body = "Sent you a file: " + e.Row.Payload.Name
	default:
		return
	}
	a.mu.Lock()
	seen := a.told[e.Row.ID]
	if len(a.told) > 1000 {
		a.told = map[string]bool{}
	}
	a.told[e.Row.ID] = true
	a.mu.Unlock()
	ctx := a.wails()
	if seen || ctx == nil {
		return
	}
	var name string
	if err := a.t.DB.QueryRow(ctx, `SELECT name FROM agents WHERE id = $1`, e.Agent).Scan(&name); err != nil {
		return
	}
	err = runtime.SendNotification(ctx, runtime.NotificationOptions{
		ID: e.Row.ID, Title: name, Body: clip(body, 200), Data: map[string]any{"agent": e.Agent},
	})
	if err != nil && !a.refused.Swap(true) {
		a.log.Info("notifications not sent", "err", err) // until you allow them, once a run
	}
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// serve answers /api/app/...: whether Tinio opens at login (GET, POST
// {open_at_login}), quitting (POST /quit), and saving a file of an agent's
// where you choose (POST /save {url, name}), which a window can't download.
func (a *app) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/app" && r.Method == http.MethodGet:
		a.settings(w)
	case r.URL.Path == "/api/app" && r.Method == http.MethodPost:
		var in struct {
			OpenAtLogin *bool `json:"open_at_login"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.OpenAtLogin == nil {
			fail(w, http.StatusBadRequest, "bad_request", "That isn't a setting the app has.")
			return
		}
		if err := setOpenAtLogin(*in.OpenAtLogin); err != nil {
			a.log.Error("open at login", "err", err)
			fail(w, http.StatusInternalServerError, "login_item", "Tinio couldn't change that on this computer.")
			return
		}
		a.settings(w)
	case r.URL.Path == "/api/app/quit" && r.Method == http.MethodPost:
		ok(w, map[string]any{"quitting": true})
		a.quitting.Store(true)
		if ctx := a.wails(); ctx != nil {
			go runtime.Quit(ctx)
		}
	case r.URL.Path == "/api/app/save" && r.Method == http.MethodPost:
		a.save(w, r)
	case r.URL.Path == "/api/app/pick-folder" && r.Method == http.MethodPost:
		a.pickFolder(w)
	case r.URL.Path == "/api/app/show-folder" && r.Method == http.MethodPost:
		a.showFolder(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (a *app) settings(w http.ResponseWriter) {
	on, err := opensAtLogin()
	if err != nil {
		a.log.Error("open at login", "err", err)
	}
	ok(w, map[string]any{"open_at_login": on, "system": goruntime.GOOS, "notifications": a.notify.Load()})
}

func (a *app) save(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || !strings.HasPrefix(in.URL, "/api/agents/") || strings.Contains(in.URL, "..") {
		fail(w, http.StatusBadRequest, "bad_request", "That isn't a file of an agent's.")
		return
	}
	// The file, from the API in this program, as the window would have had it.
	get := httptest.NewRequest(http.MethodGet, "/v1/"+strings.TrimPrefix(in.URL, "/api/"), nil)
	got := httptest.NewRecorder()
	middlewares.InApp(a.t.Handler).ServeHTTP(got, get.WithContext(r.Context()))
	if got.Code != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(got.Code)
		w.Write(got.Body.Bytes())
		return
	}
	name := in.Name
	if name == "" {
		if _, p, err := mime.ParseMediaType(got.Header().Get("Content-Disposition")); err == nil {
			name = p["filename"]
		}
	}
	if name == "" {
		name = path.Base(strings.SplitN(in.URL, "?", 2)[0])
	}
	name = filepath.Base(name)
	ctx := a.wails()
	if ctx == nil {
		fail(w, http.StatusServiceUnavailable, "starting", "Tinio is still starting.")
		return
	}
	home, _ := os.UserHomeDir()
	to, err := runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{DefaultDirectory: filepath.Join(home, "Downloads"), DefaultFilename: name, Title: "Save " + name})
	if err != nil || to == "" {
		ok(w, map[string]any{"saved": false})
		return
	}
	if err := os.WriteFile(to, got.Body.Bytes(), 0o644); err != nil {
		fail(w, http.StatusInternalServerError, "not_saved", "Tinio couldn't save it there.")
		return
	}
	ok(w, map[string]any{"saved": true, "path": to})
}

// pickFolder is the computer's own folder window, for adding a folder: the
// path chosen, or none if it was closed.
func (a *app) pickFolder(w http.ResponseWriter) {
	ctx := a.wails()
	if ctx == nil {
		fail(w, http.StatusServiceUnavailable, "starting", "Tinio is still starting.")
		return
	}
	home, _ := os.UserHomeDir()
	path, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{
		Title: "Add a folder", DefaultDirectory: filepath.Join(home, "Documents"), CanCreateDirectories: true,
	})
	if err != nil {
		fail(w, http.StatusInternalServerError, "no_window", "The folder window didn't open.")
		return
	}
	ok(w, map[string]any{"path": path})
}

// showFolder opens one of the owner's folders in Finder or Explorer: only a
// folder they added.
func (a *app) showFolder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		fail(w, http.StatusBadRequest, "bad_request", "That isn't a folder.")
		return
	}
	var path string
	if err := a.t.DB.QueryRow(r.Context(), `SELECT path FROM folders WHERE id = $1`, in.ID).Scan(&path); err != nil {
		fail(w, http.StatusNotFound, "no_such_folder", "That isn't one of your folders.")
		return
	}
	if ctx := a.wails(); ctx != nil {
		runtime.BrowserOpenURL(ctx, (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String())
	}
	ok(w, map[string]any{"shown": true})
}

// ok and fail answer the way the API does, so the window's call reads them.
func ok(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data})
}

func fail(w http.ResponseWriter, code int, errCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"status": "error", "code": errCode, "message": message})
}
