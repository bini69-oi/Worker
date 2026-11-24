package main

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/jobs"
	"github.com/bini69-oi/worker/panel"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/pay/cryptobot"
	"github.com/bini69-oi/worker/pay/manual"
	"github.com/bini69-oi/worker/pay/platega"
	"github.com/bini69-oi/worker/pay/stars"
	"github.com/bini69-oi/worker/pay/yookassa"
	"github.com/bini69-oi/worker/redact"
	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/store"
)

// app это собранный процесс: база, настройки, ядро, HTTP и фоновые задачи.
type app struct {
	db      *store.DB
	cfg     *settings.Settings
	core    *core.Core
	handler http.Handler
	jobs    *jobs.Scheduler
	log     *slog.Logger
}

// gateways это все кассы. Новая касса добавляется сюда одной строкой.
func gateways(cfg *settings.Settings) []pay.Gateway {
	return []pay.Gateway{
		manual.New(cfg),
		stars.New(cfg),
		yookassa.New(cfg, "", nil),
		cryptobot.New(cfg, "", nil),
		platega.New(cfg, "", nil),
	}
}

// secretValues это все известные секреты: из окружения и из секретных настроек. Их вычищает журнал.
func secretValues(env Env, cfg *settings.Settings) []string {
	vals := []string{env.BotToken, env.PanelToken, base64.StdEncoding.EncodeToString(env.SecretKey)}
	for _, d := range settings.Registry() {
		if d.Secret {
			vals = append(vals, cfg.Secret(d.Key))
		}
	}
	return vals
}

// build собирает процесс из окружения. Журнал пишет в log, пропуская его через фильтр секретов.
func build(env Env, base slog.Handler) (*app, error) {
	secrets := &redact.Set{}
	secrets.Add(env.BotToken, env.PanelToken, base64.StdEncoding.EncodeToString(env.SecretKey))
	log := slog.New(redact.Handler(base, secrets))

	db, err := store.Open(env.DBPath)
	if err != nil {
		return nil, err
	}
	cfg, err := settings.New(store.NewSettingsStore(db), env.SecretKey, time.Now)
	if err != nil {
		db.Close()
		return nil, err
	}
	secrets.Add(secretValues(env, cfg)...)

	gws := gateways(cfg)
	c := core.New(core.Deps{
		Store:    store.NewCoreStore(db),
		Config:   cfg,
		Panel:    panel.CoreAdapter{Client: panel.New(env.PanelURL, env.PanelToken, nil)},
		Gateways: pay.CoreGateways(gws),
		OwnerID:  env.OwnerID,
		Log:      log,
	})

	mux := http.NewServeMux()
	mux.Handle("POST /pay/{name}", pay.Handler(c, gws, log))
	mux.Handle("POST /panel/webhook", panel.WebhookHandler(c, func() string { return cfg.Secret("panel.webhook_secret") }, log))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	return &app{
		db: db, cfg: cfg, core: c, handler: mux, log: log,
		jobs: &jobs.Scheduler{Jobs: jobs.CoreJobs(c, cfg), Log: log},
	}, nil
}

// serve запускает HTTP на ln, перечитывание настроек и фоновые задачи. После отмены ctx
// HTTP дорабатывает текущие запросы, фоновые задачи доделывают начатое, база закрывается.
func (a *app) serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           http.MaxBytesHandler(a.handler, 1<<20),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	var wg sync.WaitGroup
	tick := time.NewTicker(settings.RefreshEvery)
	defer tick.Stop()
	wg.Go(func() { a.cfg.Run(ctx, tick.C) })
	wg.Go(func() { a.jobs.Run(ctx) })

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	a.log.Info("worker запущен", "addr", ln.Addr().String())

	var err error
	select {
	case <-ctx.Done():
	case err = <-errc:
	}
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if serr := srv.Shutdown(shutdown); serr != nil && err == nil {
		err = serr
	}
	wg.Wait()
	if cerr := a.db.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	a.log.Info("worker остановлен")
	return err
}
