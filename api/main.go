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
	"github.com/Grandbusta/tinio/internal/db"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
	"github.com/Grandbusta/tinio/internal/decision/questions"
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
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	if err := cfg.LoadKey(); err != nil {
		return err
	}
	d, err := db.Open(ctx, cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer d.Close()
	log.Info("Tinio's data", "folder", cfg.DataDir)
	// The API and the agents run side by side; when either stops, so does the other.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	worked := make(chan error, 1)
	go func() {
		defer cancel()
		worked <- work(ctx, cfg, d, log)
	}()
	served := serve(ctx, cfg, d, log)
	cancel()
	return errors.Join(served, <-worked)
}

func work(ctx context.Context, cfg config.Config, d *db.DB, log *slog.Logger) error {
	models, err := host.OpenAI(log)
	if err != nil {
		return err
	}
	h, err := host.New(d, tinio.Files, models, log)
	if err != nil {
		return err
	}
	h.Days = true
	h.Connections = &connect.Store{DB: d, Key: *cfg.Key}
	if s := os.Getenv("MAX_AGENTS"); s != "" {
		if h.Max, err = strconv.Atoi(s); err != nil || h.Max < 1 {
			return fmt.Errorf("MAX_AGENTS must be a whole number above 0, have %q", s)
		}
	}
	log.Info("running agents", "most", h.Max)
	return h.Run(ctx)
}

func serve(ctx context.Context, cfg config.Config, d *db.DB, log *slog.Logger) error {
	owner, err := models.Owner(ctx, d, time.Now().UTC())
	if err != nil {
		return err
	}
	characters, err := controllers.LoadCharacters(ctx, d, tinio.Files)
	if err != nil {
		return err
	}
	sets, err := questions.LoadDir(tinio.Files, internalconfig.Load().Decision.QuestionSets)
	if err != nil {
		return err
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
			HTTP: &http.Client{Timeout: 30 * time.Second}, Log: log.With("component", "connections")}, log)
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return err
	}
	srv := &http.Server{Addr: cfg.Addr, Handler: r, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Info("api listening", "addr", srv.Addr, "app", cfg.AppOrigin)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// setupRouter is every route the API has.
func setupRouter(cfg config.Config, auth *controllers.Auth, agents *controllers.Agents, chat *controllers.Chat, today *controllers.Today,
	works *controllers.Works, growth *controllers.Growth, judgment *controllers.Judgment, mind *controllers.Mind,
	mem *controllers.Memory, char *controllers.Character, settings *controllers.Settings, skills *controllers.Skills, cn *controllers.Connections, log *slog.Logger) *gin.Engine {
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
	v1.GET("/usage", signedIn, h(agents.Usage))
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
	own.POST("/topics/:topic", ours, h(chat.RenameTopic))
	own.POST("/topics/:topic/close", ours, h(chat.CloseTopic))
	own.POST("/topics/:topic/merge", ours, h(chat.MergeTopic))
	own.POST("/messages/:message/topic", ours, h(chat.MoveMessage))
	own.GET("/today", h(today.Get))
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
