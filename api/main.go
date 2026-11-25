// Package api is Tinio's HTTP API and the agents behind it, run together by
// the tinio command (Main):
//
//	tinio        serve the API and run every agent, in one process
//	tinio eval   test System 1 on the cases in evals/ (flags: tinio eval -h)
//
// The app passes /api/* to the API as /v1/*, so the session cookie belongs
// to the app alone.
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	_ "time/tzdata" // agents' time zones work without the system's database, as in a slim container

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"github.com/Grandbusta/tinio"
	"github.com/Grandbusta/tinio/api/config"
	"github.com/Grandbusta/tinio/api/controllers"
	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	internalconfig "github.com/Grandbusta/tinio/internal/config"
	"github.com/Grandbusta/tinio/internal/connect"
	"github.com/Grandbusta/tinio/internal/db"
	"github.com/Grandbusta/tinio/internal/decision/questions"
	"github.com/Grandbusta/tinio/internal/folders"
	"github.com/Grandbusta/tinio/internal/host"
)

// Main runs the tinio command, as os.Args say.
func Main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	if err := run(cmd, log); err != nil {
		log.Error("stopped", "err", err)
		os.Exit(1)
	}
}

func run(cmd string, log *slog.Logger) error {
	if cmd != "" && cmd != "eval" {
		return fmt.Errorf("unknown command %q (want eval, or none to run Tinio)", cmd)
	}
	godotenv.Load() // a .env beside it, if there is one; the environment wins
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cmd == "eval" {
		return evaluate(ctx, os.Args[2:], log)
	}
	t, err := Start(ctx, log)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: t.Config.Addr, Handler: t.Handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		select {
		case <-ctx.Done():
		case <-t.Done():
		}
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Info("api listening", "addr", srv.Addr, "app", t.Config.AppOrigin)
	served := srv.ListenAndServe()
	if errors.Is(served, http.ErrServerClosed) {
		served = nil
	}
	return errors.Join(served, t.Stop())
}

// Tinio is Tinio running: its database, its agents and its API, which isn't
// listening anywhere yet (Handler): the tinio command serves it over HTTP, the
// desktop app inside its window.
type Tinio struct {
	Config  config.Config
	DB      *db.DB
	Handler http.Handler
	stop    context.CancelFunc
	worked  chan error
}

// Start opens Tinio's data folder and database (made the first time, with its
// key) and starts the agents. Stop ends them.
func Start(ctx context.Context, log *slog.Logger) (*Tinio, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	if err := cfg.LoadKey(); err != nil {
		return nil, err
	}
	d, err := db.Open(ctx, cfg.DatabasePath())
	if err != nil {
		return nil, err
	}
	log.Info("Tinio's data", "folder", cfg.DataDir)
	ctx, cancel := context.WithCancel(ctx)
	h, err := api(ctx, cfg, d, log)
	if err != nil {
		cancel()
		d.Close()
		return nil, err
	}
	t := &Tinio{Config: cfg, DB: d, Handler: h, stop: cancel, worked: make(chan error, 1)}
	go func() { t.worked <- work(ctx, cfg, d, log) }()
	return t, nil
}

// Done is closed once the agents have stopped: Stop was called, or their work
// failed (Stop says how).
func (t *Tinio) Done() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		err := <-t.worked
		t.worked <- err
		close(done)
	}()
	return done
}

// Stop ends the agents and closes the database.
func (t *Tinio) Stop() error {
	t.stop()
	err := <-t.worked
	t.worked <- err
	return errors.Join(err, t.DB.Close())
}

// work runs every agent on the owner's OpenAI key (OPENAI_API_KEY, or the one
// set in the app) and the models chosen for each job. With no key it waits for
// one, and when the owner changes the key or a model the agents start again.
func work(ctx context.Context, cfg config.Config, d *db.DB, log *slog.Logger) error {
	for {
		changed, stop := d.Listen(models.KeysChanged, 1)
		err := workOnce(ctx, cfg, d, changed, log)
		stop()
		if ctx.Err() != nil || err != nil {
			return err
		}
	}
}

// workOnce runs the agents until ctx is done or the key changes (nil), or
// waits for a key if there's none.
func workOnce(ctx context.Context, cfg config.Config, d *db.DB, changed <-chan string, log *slog.Logger) error {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		var err error
		if apiKey, err = models.OpenKey(ctx, d, *cfg.Key, models.KeyOpenAI); err != nil {
			return err
		}
	}
	if apiKey == "" {
		log.Info("waiting for your OpenAI key: set it in the app")
		select {
		case <-ctx.Done():
		case <-changed:
		}
		return nil
	}
	chosen, err := models.ChosenModels(ctx, d)
	if err != nil {
		return err
	}
	ms, err := host.OpenAIWith(log, apiKey, host.Chosen{Reply: chosen["reply"].Model, Decision: chosen["decision"].Model,
		Day: chosen["day"].Model, Light: chosen["light"].Model, Work: chosen["work"].Model})
	if err != nil {
		return err
	}
	h, err := host.New(d, tinio.Files, ms, log)
	if err != nil {
		return err
	}
	h.Days = true
	h.Connections = &connect.Store{DB: d, Key: *cfg.Key}
	h.Folders = &folders.Store{DB: d, Rules: folders.Rules{Data: cfg.DataDir}, Versions: filepath.Join(cfg.DataDir, "versions")}
	if s := os.Getenv("MAX_AGENTS"); s != "" {
		if h.Max, err = strconv.Atoi(s); err != nil || h.Max < 1 {
			return fmt.Errorf("MAX_AGENTS must be a whole number above 0, have %q", s)
		}
	}
	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	go func() {
		select {
		case <-changed:
			log.Info("your key or models changed: starting the agents again")
			stopRun()
		case <-runCtx.Done():
		}
	}()
	log.Info("running agents", "most", h.Max)
	return h.Run(runCtx)
}

// api is the API, its chat streams running until ctx is done.
func api(ctx context.Context, cfg config.Config, d *db.DB, log *slog.Logger) (http.Handler, error) {
	owner, err := models.Owner(ctx, d, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	characters, err := controllers.LoadCharacters(ctx, d, tinio.Files)
	if err != nil {
		return nil, err
	}
	sets, err := questions.LoadDir(tinio.Files, internalconfig.Load().Decision.QuestionSets)
	if err != nil {
		return nil, err
	}
	auth := &controllers.Auth{DB: d, Owner: owner, Passcode: cfg.Passcode, Secure: cfg.CookieSecure, Log: log}
	agents := &controllers.Agents{DB: d, Characters: characters}
	hub := &controllers.ChatHub{DB: d, Log: log.With("component", "chat")}
	go hub.Run(ctx)
	chat := &controllers.Chat{DB: d, Hub: hub, PerAccount: utils.NewLimiter(2*time.Second, 30)}
	r := setupRouter(cfg, auth, agents, chat, &controllers.Today{DB: d}, &controllers.Works{DB: d, Formats: host.Formats()},
		&controllers.Growth{DB: d}, &controllers.Judgment{DB: d}, &controllers.Mind{DB: d, Sets: sets},
		&controllers.Memory{DB: d}, &controllers.Character{DB: d}, &controllers.Settings{DB: d}, &controllers.Skills{DB: d},
		&controllers.Connections{DB: d, Key: cfg.Key, Google: cfg.Google, Picker: cfg.GooglePicker, App: cfg.AppOrigin,
			HTTP: &http.Client{Timeout: 30 * time.Second}, Log: log.With("component", "connections")},
		&controllers.Keys{DB: d, Key: *cfg.Key}, log)
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}
	return r, nil
}

// setupRouter is every route the API has.
func setupRouter(cfg config.Config, auth *controllers.Auth, agents *controllers.Agents, chat *controllers.Chat, today *controllers.Today,
	works *controllers.Works, growth *controllers.Growth, judgment *controllers.Judgment, mind *controllers.Mind,
	mem *controllers.Memory, char *controllers.Character, settings *controllers.Settings, skills *controllers.Skills, cn *controllers.Connections, keys *controllers.Keys, log *slog.Logger) *gin.Engine {
	if os.Getenv(gin.EnvGinMode) == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok\n") })

	h := func(fn func(*gin.Context) error) gin.HandlerFunc { return utils.Handle(log, fn) }
	ours := middlewares.FromOurSite(cfg.AppOrigin, log)
	signedIn := middlewares.SignedIn(auth.DB, auth.Owner, log)

	v1 := r.Group("/v1")
	v1.GET("/me", signedIn, h(auth.Me))
	v1.GET("/characters", signedIn, h(agents.Characters.List))
	v1.GET("/agents", signedIn, h(agents.List))
	v1.GET("/overview", signedIn, h(agents.Overview))
	v1.GET("/today", signedIn, h(today.Yours))
	v1.GET("/usage", signedIn, h(agents.Usage))
	v1.POST("/usage", ours, signedIn, h(agents.SetCap))
	v1.GET("/keys", signedIn, h(keys.Get))
	v1.POST("/keys", ours, signedIn, h(keys.Set))
	aiModels := &controllers.Models{DB: keys.DB}
	fd := &controllers.Folders{DB: keys.DB, Rules: folders.Rules{Data: cfg.DataDir}}
	v1.GET("/folders", signedIn, h(fd.List))
	v1.GET("/folders/peek", signedIn, h(fd.Peek))
	v1.POST("/folders", ours, signedIn, h(fd.Add))
	v1.POST("/folders/:folder", ours, signedIn, h(fd.Change))
	v1.POST("/folders/:folder/remove", ours, signedIn, h(fd.Remove))
	v1.GET("/models", signedIn, h(aiModels.Get))
	v1.POST("/models", ours, signedIn, h(aiModels.Choose))
	v1.POST("/agents", ours, signedIn, h(agents.Create))
	own := v1.Group("/agents/:id", signedIn, middlewares.OwnAgent(auth.DB, log))
	own.GET("", h(agents.One))
	own.POST("/hello", ours, h(chat.Hello))
	own.GET("/messages", h(chat.History))
	own.POST("/messages", ours, h(chat.Send))
	own.POST("/answers", ours, h(chat.Answer))
	own.POST("/read", ours, h(chat.Read))
	own.GET("/files/:file", h(chat.File))
	own.GET("/stream", h(chat.Stream))
	own.GET("/topics", h(chat.Topics))
	own.POST("/reactions", ours, h(chat.React))
	own.GET("/search", h(chat.Search))
	own.POST("/topics/:topic", ours, h(chat.RenameTopic))
	own.POST("/topics/:topic/close", ours, h(chat.CloseTopic))
	own.POST("/topics/:topic/merge", ours, h(chat.MergeTopic))
	own.POST("/messages/:message/topic", ours, h(chat.MoveMessage))
	own.GET("/today", h(today.Get))
	own.POST("/today/items/:item", ours, h(today.Tick))
	own.GET("/works", h(works.List))
	own.GET("/works/:work", h(works.One))
	own.GET("/works/:work/files/:format", h(works.File))
	own.POST("/works/:work/send", ours, h(works.Send))
	own.GET("/growth", h(growth.Get))
	own.GET("/judgment", h(judgment.Get))
	own.POST("/principles", ours, h(judgment.Add))
	own.POST("/principles/:principle", ours, h(judgment.Review))
	own.GET("/mind", h(mind.Get))
	own.GET("/mind/:decision", h(mind.Chain))
	own.POST("/labels", ours, h(mind.Label))
	own.GET("/memory", h(mem.Get))
	own.GET("/character", h(char.Get))
	own.POST("/character", ours, h(char.Save))
	own.POST("/character/versions/:version", ours, h(char.GoBack))
	own.GET("/skills", h(skills.List))
	own.POST("/skills/import", ours, h(skills.Import))
	own.POST("/skills/:name/remove", ours, h(skills.Remove))
	own.GET("/tools", h(cn.AgentTools))
	own.POST("/tools/:connection", ours, h(cn.Use))
	own.POST("/folders/:folder", ours, h(fd.Use))
	own.GET("/settings", h(settings.Get))
	own.POST("/settings", ours, h(settings.Save))
	own.POST("/pause", ours, h(settings.Pause))
	own.POST("/delete", ours, h(settings.Delete))
	own.POST("/facts/:fact", ours, h(mem.Correct))
	own.POST("/facts/:fact/forget", ours, h(mem.Forget))
	v1.GET("/connections", signedIn, h(cn.List))
	v1.POST("/connections/start", ours, signedIn, h(cn.Start))
	v1.GET("/connections/callback", signedIn, h(cn.Callback))
	v1.POST("/connections/:connection/agents", ours, signedIn, h(cn.Agents))
	v1.POST("/connections/:connection/remove", ours, signedIn, h(cn.Remove))
	v1.GET("/connections/:connection/picker", signedIn, h(cn.PickFiles))
	v1.POST("/connections/:connection/files", ours, signedIn, h(cn.Files))
	authRoutes := v1.Group("/auth", ours)
	authRoutes.POST("/passcode", middlewares.LimitByAddress(utils.NewLimiter(10*time.Second, 5), log), h(auth.SignIn))
	authRoutes.POST("/signout", h(auth.SignOut))
	return r
}
