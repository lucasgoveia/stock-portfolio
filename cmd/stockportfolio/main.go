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
	"github.com/lucasgoveia/stock-portfolio/internal/analyst"
	"github.com/lucasgoveia/stock-portfolio/internal/api"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/config"
	"github.com/lucasgoveia/stock-portfolio/internal/corpactions"
	"github.com/lucasgoveia/stock-portfolio/internal/dividends"
	"github.com/lucasgoveia/stock-portfolio/internal/fundamentals"
	"github.com/lucasgoveia/stock-portfolio/internal/jobs"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/performance"
	"github.com/lucasgoveia/stock-portfolio/internal/portfolio"
	"github.com/lucasgoveia/stock-portfolio/internal/ratings"
	"github.com/lucasgoveia/stock-portfolio/internal/research"
	"github.com/lucasgoveia/stock-portfolio/internal/settings"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/tactical"
	"github.com/lucasgoveia/stock-portfolio/internal/tax"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
	"github.com/lucasgoveia/stock-portfolio/internal/tracking"
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
	tradeSvc := trades.NewService(conn, assetSvc)
	cashSvc := cash.NewService(conn, rateStore, nil)
	researchSvc := research.NewService(conn, assetSvc, nil)
	tacticalSvc := tactical.NewService(conn, assetSvc, researchSvc)
	dividendSvc := dividends.NewService(conn, assetSvc, tradeSvc, cashSvc, nil)
	taxSvc := tax.NewService(assetSvc, tradeSvc, dividendSvc, nil)
	settingsSvc := settings.NewService(conn, cfg.KeyStatuses())
	tacticalSvc.UseParams(settingsSvc)
	dividendSvc.UseParams(settingsSvc)
	taxSvc.UseParams(settingsSvc)
	deps.Dividends = dividendSvc
	actionSvc := corpactions.NewService(conn, assetSvc, tradeSvc, alertSvc, nil)
	deps.Actions = actionSvc
	perfSvc := performance.NewService(conn, assetSvc, tradeSvc, cashSvc, nil)
	deps.Snapshots = perfSvc
	cvm := marketdata.NewCVM(nil, "")
	edgar := marketdata.NewEDGAR(nil, "", "", cfg.SECUserAgent)
	fundSvc := fundamentals.NewService(conn, assetSvc, fundamentals.Sources{
		BR:        cvm,
		US:        edgar,
		Dividends: marketdata.NewBrapi(nil, "", cfg.BrapiToken),
	}, nil)
	deps.Fundamentals = fundSvc
	var aiClient analyst.Client // nil without a key: runs are refused with a clear message
	if cfg.AnthropicAPIKey != "" {
		aiClient = analyst.NewAnthropicClient(cfg.AnthropicAPIKey)
	}
	analystSvc := analyst.NewService(analyst.Deps{
		Conn: conn, Assets: assetSvc, Settings: settingsSvc, Thesis: thesisSvc, Valuation: valuationSvc,
		Research: researchSvc, Fundamentals: fundSvc, US: edgar, BR: cvm, Client: aiClient,
	})
	settingsSvc.UseUsage(analystSvc.MonthSpend)
	trackSvc := tracking.NewService(conn, assetSvc, valuationSvc, alertSvc, thesisSvc, settingsSvc)
	fundSvc.OnRefresh(trackSvc.Evaluate)
	portfolioSvc := portfolio.NewService(portfolio.Deps{
		Conn: conn, Assets: assetSvc, Trades: tradeSvc, Cash: cashSvc, Rates: rateStore, Ratings: ratingSvc,
		Valuation: valuationSvc, Thesis: thesisSvc, Research: researchSvc, Alerts: alertSvc, Tactical: tacticalSvc, Tax: taxSvc, Dividends: dividendSvc,
		Settings: settingsSvc,
	})
	deps.Evaluators = []jobs.Evaluator{
		{Name: "thesis", Run: thesisSvc.Evaluate},
		{Name: "fair_value", Run: valuationSvc.Evaluate},
		{Name: "portfolio", Run: portfolioSvc.Evaluate},
		{Name: "corporate_actions", Run: actionSvc.Evaluate},
		{Name: "expected_vs_actual", Run: trackSvc.Evaluate},
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
		api.WithCash(cashSvc),
		api.WithRatings(ratingSvc),
		api.WithResearch(researchSvc),
		api.WithTrades(tradeSvc),
		api.WithAlerts(alertSvc),
		api.WithThesis(thesisSvc),
		api.WithValuation(valuationSvc),
		api.WithPortfolio(portfolioSvc),
		api.WithJobs(runner),
		api.WithTactical(tacticalSvc, portfolioSvc),
		api.WithTaxes(taxSvc),
		api.WithDividends(dividendSvc),
		api.WithCorporateActions(actionSvc),
		api.WithPerformance(perfSvc),
		api.WithSettings(settingsSvc),
		api.WithFundamentals(fundSvc),
		api.WithAnalyst(analystSvc),
		api.WithExpectedVsActual(trackSvc),
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
	brapi := marketdata.NewBrapi(nil, "", cfg.BrapiToken)
	d := jobs.Deps{
		Conn:    conn,
		Assets:  a,
		Rates:   rates,
		FX:      marketdata.NewPTAX(nil, ""),
		CDI:     marketdata.NewSGS(nil, ""),
		Tesouro: marketdata.NewTesouro(nil, ""),
		Quotes:  brapi,
		// brapi's dividends endpoint also announces JCP and dividends.
		DividendFeed: brapi,
		Cotahist:     marketdata.NewCotahist(nil, ""),
		IBOV:         marketdata.NewB3Index(nil, "", "IBOV"),
		SP500:        marketdata.NewFRED(nil, "", "SP500"),
	}
	if cfg.TiingoToken != "" {
		d.US = marketdata.NewTiingo(nil, "", cfg.TiingoToken)
	}
	return d
}
