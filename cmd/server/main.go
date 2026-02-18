package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/web3-frozen/onchain-monitor/internal/config"
	"github.com/web3-frozen/onchain-monitor/internal/dedup"
	"github.com/web3-frozen/onchain-monitor/internal/handler"
	"github.com/web3-frozen/onchain-monitor/internal/middleware"
	"github.com/web3-frozen/onchain-monitor/internal/monitor"
	"github.com/web3-frozen/onchain-monitor/internal/monitor/sources"
	"github.com/web3-frozen/onchain-monitor/internal/store"
	"github.com/web3-frozen/onchain-monitor/internal/telegram"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := config.Load()

	if cfg.DatabaseURL == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	if cfg.TelegramToken == "" {
		logger.Error("TELEGRAM_BOT_TOKEN is required")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Database
	db, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("database connected and migrated")

	// Telegram bot
	bot := telegram.NewBot(cfg.TelegramToken, db, logger)

	// Redis dedup (retry up to 30s for ExternalSecret to sync)
	var dd *dedup.Deduplicator
	for i := 0; i < 6; i++ {
		dd, err = dedup.New(cfg.RedisURL, cfg.RedisPassword)
		if err == nil {
			break
		}
		logger.Warn("redis not ready, retrying...", "attempt", i+1, "error", err)
		time.Sleep(5 * time.Second)
	}
	if err != nil {
		logger.Error("failed to connect to redis after retries", "error", err)
		os.Exit(1)
	}
	defer dd.Close()
	logger.Info("redis connected for alert dedup")

	// Monitoring engine
	engine := monitor.NewEngine(db, logger, bot.SendMessage, dd)
	engine.Register(sources.NewAltura())
	engine.Register(sources.NewNeverland())
	engine.Register(sources.NewFearGreed())
	engine.Register(sources.NewMaxPain(logger))
	engine.Register(sources.NewMerkl())

	// Start background goroutines
	go bot.Run(ctx)
	go engine.Run(ctx)

	// HTTP routes
	r := chi.NewRouter()
	r.Use(middleware.Recover(logger))
	r.Use(middleware.Logger(logger))
	r.Use(middleware.Metrics())
	r.Use(middleware.CORS(cfg.FrontendOrigin))

	r.Handle("/metrics", promhttp.Handler())
	r.Get("/healthz", handler.Health())
	r.Get("/readyz", handler.Ready(db))

	r.Route("/api", func(r chi.Router) {
		r.Get("/events", handler.ListEvents(db))
		r.Post("/link", handler.LinkTelegram(db))
		r.Post("/unlink", handler.UnlinkTelegram(db))
		r.Get("/subscriptions", handler.ListSubscriptions(db))
		r.Post("/subscriptions", handler.Subscribe(db))
		r.Put("/subscriptions/{id}", handler.UpdateSubscription(db))
		r.Delete("/subscriptions/{id}", handler.Unsubscribe(db))
		r.Get("/stats", handler.Stats(engine))
		r.Get("/stats/meta", handler.StatsMetadata(engine))
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down gracefully")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}
