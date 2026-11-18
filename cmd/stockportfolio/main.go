// Command stockportfolio runs the server.
package main

import (
	"context"
	"database/sql"
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
	"github.com/lucasgoveia/stock-portfolio/internal/jobs"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/ratings"
	"github.com/lucasgoveia/stock-portfolio/internal/research"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
	"github.com/lucasgoveia/stock-portfolio/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet("stockportfolio", flag.ExitOnError)
	cfgPath := fs.String("config", "config.local.toml", "path to config file (falls back to defaults if missing)")
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && (args[0] == "serve" || args[0] == "backfill") {
		cmd, args = args[0], args[1:]
	}
	years := fs.Int("years", 2, "backfill: years of history to load")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	conn, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	assetSvc := assets.NewService(conn)
	alertSvc := alerts.New(conn, nil)
	rateStore := cash.NewRateStore(conn)
	thesisSvc := thesis.NewService(conn, assetSvc, alertSvc, nil)
	valuationSvc := valuation.NewService(conn, assetSvc, alertSvc, nil)
	ratingSvc := ratings.NewService(conn, assetSvc, nil)
	deps := marketDeps(cfg, conn, assetSvc, rateStore)
	deps.Betas = ratingSvc
	deps.Evaluators = []jobs.Evaluator{
		{Name: "thesis", Run: thesisSvc.Evaluate},
		{Name: "fair_value", Run: valuationSvc.Evaluate},
	}

	if cmd == "backfill" {
		from := time.Now().AddDate(-*years, 0, 0)
		res, err := jobs.Backfill(ctx, deps, from)
		for _, r := range res {
			slog.Info("backfill", "step", r.Name, "status", r.Status, "note", r.Note, "error", r.Error)
		}
		return err
	}

	runner := jobs.NewRunner(conn, alertSvc, jobs.DailySteps(deps), nil)
	if !cfg.DisableScheduler {
		if err := runner.Schedule(ctx, cfg.DailyJobAt); err != nil {
			return err
		}
	}

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: api.NewRouter(
		api.WithAssets(assetSvc),
		api.WithCash(cash.NewService(conn, rateStore, nil)),
		api.WithRatings(ratingSvc),
		api.WithResearch(research.NewService(conn, assetSvc, nil)),
		api.WithTrades(trades.NewService(conn, assetSvc)),
		api.WithAlerts(alertSvc),
		api.WithThesis(thesisSvc),
		api.WithValuation(valuationSvc),
		api.WithJobs(runner),
		api.WithUI(web.Dist()),
	), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	slog.Info("listening", "addr", cfg.ListenAddr, "db", cfg.DBPath, "daily_job_at", cfg.DailyJobAt, "scheduler", !cfg.DisableScheduler)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// marketDeps wires the real market data clients from config.
func marketDeps(cfg config.Config, conn *sql.DB, a *assets.Service, rates *cash.RateStore) jobs.Deps {
	d := jobs.Deps{
		Conn:     conn,
		Assets:   a,
		Rates:    rates,
		FX:       marketdata.NewPTAX(nil, ""),
		CDI:      marketdata.NewSGS(nil, ""),
		Tesouro:  marketdata.NewTesouro(nil, ""),
		Quotes:   marketdata.NewBrapi(nil, "", cfg.BrapiToken),
		Cotahist: marketdata.NewCotahist(nil, ""),
		IBOV:     marketdata.NewB3Index(nil, "", "IBOV"),
		SP500:    marketdata.NewFRED(nil, "", "SP500"),
	}
	if cfg.TiingoToken != "" {
		d.US = marketdata.NewTiingo(nil, "", cfg.TiingoToken)
	}
	return d
}
