// Command tinio-desktop is Tinio as an app on your computer: its agents and
// its API run inside the app, and its window is the app you use, with no port
// open and no sign-in. Build it with the wails command (see README).
package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	goruntime "runtime"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/Grandbusta/tinio/api"
	"github.com/Grandbusta/tinio/api/controllers"
	"github.com/Grandbusta/tinio/api/middlewares"
)

//go:embed all:frontend/dist
var dist embed.FS

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t, err := api.Start(ctx, logger)
	if err != nil {
		log.Fatal(err)
	}
	if err := firstOpen(t.Config.DataDir); err != nil {
		logger.Error("open at login", "err", err)
	}
	assets, err := fs.Sub(dist, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	a := newApp(t, logger)
	start := options.Normal
	if openedAtLogin() {
		start = options.Minimised // out of the way: they're living their day
	}
	err = wails.Run(&options.App{
		Title:            "Tinio",
		Width:            1180,
		Height:           820,
		MinWidth:         420,
		MinHeight:        560,
		WindowStartState: start,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: apiOf(t.Handler, assets, a),
		},
		BackgroundColour:  &options.RGBA{R: 250, G: 248, B: 244, A: 255},
		HideWindowOnClose: goruntime.GOOS == "darwin",
		OnBeforeClose:     a.beforeClose,
		OnStartup:         a.started,
		OnShutdown: func(context.Context) {
			if err := t.Stop(); err != nil {
				logger.Error("stopping", "err", err)
			}
		},
		// Opening it again while it runs brings back the window it has.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "app.tinio.desktop",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { a.show() },
		},
		Menu: appMenu(),
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHidden(),
			About:    &mac.AboutInfo{Title: "Tinio", Message: "Agents with lives of their own, on your computer."},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// appMenu is macOS's menu bar: Tinio (About, Hide, Quit), Edit (so copying
// and pasting work in the window) and Window. Elsewhere there's none.
func appMenu() *menu.Menu {
	if goruntime.GOOS != "darwin" {
		return nil
	}
	return menu.NewMenuFromItems(menu.AppMenu(), menu.EditMenu(), menu.WindowMenu())
}

// chatEvents tells the window what's said in the chats, as the app's events
// (the window can't hold a stream open on every system: Windows reads a whole
// answer first), and tells the app, for its notifications.
func chatEvents(ctx context.Context, a *app) {
	controllers.ChatEvents(ctx, a.t.DB, func(event string, data any) {
		runtime.EventsEmit(ctx, event, data)
		a.chatHappened(event, data)
	})
}

// apiOf is what the window asks for that isn't a file of the app's: /api/app
// is the app's own (app.serve); the rest of /api/... is the API's /v1/..., the
// way the web app calls it, every request the app's own (InApp), let in as
// this machine's owner; any other address is one of the app's pages, so it's
// the app (index.html), which shows it. Nothing outside the window reaches it.
func apiOf(h http.Handler, assets fs.FS, a *app) http.Handler {
	in := middlewares.InApp(h)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/app" || strings.HasPrefix(r.URL.Path, "/api/app/") {
			a.serve(w, r)
			return
		}
		if rest, ok := strings.CutPrefix(r.URL.Path, "/api/"); ok {
			r = r.Clone(r.Context())
			r.URL.Path = "/v1/" + rest
			r.URL.RawPath = ""
			in.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		page, err := fs.ReadFile(assets, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
}
