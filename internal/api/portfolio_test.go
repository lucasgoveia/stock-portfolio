package api

import (
	"path/filepath"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/portfolio"
	"github.com/lucasgoveia/stock-portfolio/internal/ratings"
	"github.com/lucasgoveia/stock-portfolio/internal/research"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

func TestPortfolioAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := assets.NewService(conn)
	al := alerts.New(conn, nil)
	rs := cash.NewRateStore(conn)
	svc := portfolio.NewService(portfolio.Deps{
		Conn: conn, Assets: as, Trades: trades.NewService(conn, as), Cash: cash.NewService(conn, rs, nil), Rates: rs,
		Ratings: ratings.NewService(conn, as, nil), Valuation: valuation.NewService(conn, as, al, nil),
		Thesis: thesis.NewService(conn, as, al, nil), Research: research.NewService(conn, as, nil), Alerts: al,
	})
	h := NewRouter(WithPortfolio(svc))
	code, s, _ := do(t, h, "GET", "/api/portfolio", "")
	if code != 200 || s["bootstrap"] != true || s["total_value"] != "0" {
		t.Fatalf("snapshot: %d %v", code, s)
	}
	if code, _, _ := do(t, h, "POST", "/api/plans/contribution", `{"amount":"0"}`); code != 400 {
		t.Fatalf("zero amount: %d", code)
	}
	code, p, _ := do(t, h, "POST", "/api/plans/contribution", `{"amount":"1000"}`)
	if code != 200 || p["amount"] != "1000" {
		t.Fatalf("plan: %d %v", code, p)
	}
	if code, _, _ := do(t, h, "GET", "/api/trims", ""); code != 200 {
		t.Fatalf("trims: %d", code)
	}
	if code, _, _ := do(t, h, "PUT", "/api/portfolio/pending-us", `{"amount":"-1"}`); code != 400 {
		t.Fatalf("negative pending: %d", code)
	}
	if code, _, _ := do(t, h, "PUT", "/api/portfolio/pending-us", `{"amount":"150"}`); code != 204 {
		t.Fatalf("pending: %d", code)
	}
}
