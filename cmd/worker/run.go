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

	"github.com/bini69-oi/worker/apps"
	"github.com/bini69-oi/worker/bot"
	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/guard"
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
	"github.com/bini69-oi/worker/texts"
	"github.com/bini69-oi/worker/tg"
)

// app это собранный процесс: база, настройки, ядро, HTTP и фоновые задачи.
type app struct {
	db      *store.DB
	cfg     *settings.Settings
	texts   *texts.Texts
	core    *core.Core
	tg      *telegram
	handler http.Handler
	jobs    *jobs.Scheduler
	log     *slog.Logger
}

// gateways это все кассы. Новая касса добавляется сюда одной строкой. t даёт кассам адрес возврата в бота
// и возврат звёзд через Telegram.
func gateways(cfg *settings.Settings, t *telegram) []pay.Gateway {
	kassa := yookassa.New(cfg, "", nil)
	kassa.RestrictIPs(yookassa.OfficialNets()) // уведомления ЮKassa только с её адресов
	kassa.Return = t.paidURL
	pl := platega.New(cfg, "", nil)
	pl.Return = t.paidURL
	st := stars.New(cfg)
	st.Refunder = t.client.RefundStarPayment
	return []pay.Gateway{
		manual.New(cfg),
		st,
		kassa,
		cryptobot.New(cfg, "", nil),
		pl,
	}
}

// hookLimit это предел запросов с одного IP в минуту на адреса касс и панели. Касса шлёт единицы
// уведомлений в минуту, а поток подделок упрётся в предел и не нагрузит API касс перепроверками.
const hookLimit = 120

// secretValues это все известные секреты: из окружения и из секретных настроек. Их вычищает журнал.
func secretValues(env Env, cfg *settings.Settings) []string {
	vals := []string{env.BotToken, env.PanelToken, base64.StdEncoding.EncodeToString(env.SecretKey)}
	for _, d := range settings.Registry() {
		if d.Secret {
			vals = append(vals, cfg.Secret(d.Key))
		}
	}
	// ЮKassa шлёт пару «магазин:ключ» в заголовке Basic: в base64 ловится только пара целиком.
	if key := cfg.Secret("yookassa.secret_key"); key != "" {
		vals = append(vals, cfg.String("yookassa.shop_id")+":"+key)
	}
	return vals
}

// build собирает процесс из окружения. Журнал пишет в log, пропуская его через фильтр секретов.
func build(env Env, base slog.Handler) (*app, error) {
	secrets := &redact.Set{}
	secrets.Add(env.BotToken, env.PanelToken, base64.StdEncoding.EncodeToString(env.SecretKey))
	log := slog.New(redact.Handler(base, secrets))
	slog.SetDefault(log) // и журнал пакетов, которые пишут в общий журнал, идёт через фильтр

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
	tx, err := texts.New(store.NewTextsStore(db), func() string { return cfg.String("lang.default") }, time.Now)
	if err != nil {
		db.Close()
		return nil, err
	}

	t := newTelegram(env.BotToken, env.TelegramURL, env.PublicURL, func() bool { return cfg.Bool("bot.polling") }, log)
	secrets.Add(t.secret)
	gws := gateways(cfg, t)
	notes := &notifier{}
	c := core.New(core.Deps{
		Store:    store.NewCoreStore(db),
		Config:   cfg,
		Panel:    panel.CoreAdapter{Client: panel.New(env.PanelURL, env.PanelToken, nil)},
		Gateways: pay.CoreGateways(gws),
		Notifier: notes,
		Telegram: bot.Tokens{BaseURL: env.TelegramURL, Hook: t.hook, Secret: func() string { return t.secret }},
		Channels: bot.Channels{API: t.client},
		OwnerID:  env.OwnerID,
		Secrets:  secrets,
		Log:      log,
	})
	t.bot = bot.New(bot.Options{Core: c, Texts: tx, API: t.client, Config: cfg, Log: log, Name: "", URL: env.PublicURL,
		Owner: env.OwnerID, DLKey: env.SecretKey})
	notes.b = t.bot

	hooks := &guard.Limiter{Max: hookLimit, Window: time.Minute}
	mux := http.NewServeMux()
	mux.Handle("POST /pay/{name}", hooks.Wrap(pay.Handler(c, gws, log)))
	mux.Handle("POST /panel/webhook", hooks.Wrap(panel.WebhookHandler(c, func() string { return cfg.Secret("panel.webhook_secret") }, log)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("POST /tg/webhook", tg.NewWebhook(t.secret, t.updates, t.bot.Urgent))
	mux.Handle("GET /dl/{app}/{token}", apps.Handler(c, env.SecretKey, time.Now,
		func(key string) string { return tx.Render(key, cfg.String("lang.default"), nil) }))

	return &app{
		db: db, cfg: cfg, texts: tx, core: c, tg: t, handler: guard.RealIP(mux), log: log,
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
		// Свои ошибки и паники обработчиков сервер пишет через тот же фильтр секретов.
		ErrorLog: slog.NewLogLogger(a.log.Handler(), slog.LevelError),
	}
	var wg sync.WaitGroup
	tick := time.NewTicker(settings.RefreshEvery)
	defer tick.Stop()
	wg.Go(func() { a.cfg.Run(ctx, tick.C) })
	textTick := time.NewTicker(texts.RefreshEvery)
	defer textTick.Stop()
	wg.Go(func() { a.texts.Run(ctx, textTick.C) })
	wg.Go(func() { a.jobs.Run(ctx) })
	wg.Go(func() { a.tg.start(ctx, wg.Go) })

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
