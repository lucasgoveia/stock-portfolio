// Command stockportfolio runs the server.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/api"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/config"
	"github.com/lucasgoveia/stock-portfolio/internal/ratings"
	"github.com/lucasgoveia/stock-portfolio/internal/research"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
	"github.com/lucasgoveia/stock-portfolio/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", "config.local.toml", "path to config file (falls back to defaults if missing)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	conn, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	assetSvc := assets.NewService(conn)
	srv := &http.Server{Addr: cfg.ListenAddr, Handler: api.NewRouter(
		api.WithAssets(assetSvc),
		api.WithCash(cash.NewService(conn, cash.NewRateStore(conn), nil)),
		api.WithRatings(ratings.NewService(conn, assetSvc, nil)),
		api.WithResearch(research.NewService(conn, assetSvc, nil)),
		api.WithTrades(trades.NewService(conn, assetSvc)),
		api.WithAlerts(alerts.New(conn, nil)),
		api.WithUI(web.Dist()),
	), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	slog.Info("listening", "addr", cfg.ListenAddr, "db", cfg.DBPath)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
