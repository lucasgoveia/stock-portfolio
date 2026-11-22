package capm

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/settings"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestForAsset(t *testing.T) {
	ctx := context.Background()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as, q, set := assets.NewService(conn), db.New(conn), settings.NewService(conn, nil)
	svc := New(conn, set)
	mk := func(ticker, market string) db.Asset {
		a, err := as.Create(ctx, assets.CreateInput{Ticker: ticker, Market: market, Kind: "stock"})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	amd, ko, wege := mk("AMD", "US"), mk("KO", "US"), mk("WEGE3", "B3")
	beta := func(a db.Asset, b string) {
		if err := q.UpsertAssetBeta(ctx, db.UpsertAssetBetaParams{AssetID: a.ID, IndexCode: "SP500", Beta: b, Weeks: 104, ComputedOn: "2026-11-18"}); err != nil {
			t.Fatal(err)
		}
	}

	// Nothing stored: not available, not an error.
	if _, ok, err := svc.ForAsset(ctx, amd); ok || err != nil {
		t.Fatalf("empty: ok=%v err=%v", ok, err)
	}
	beta(amd, "2.18")
	if _, ok, err := svc.ForAsset(ctx, amd); ok || err != nil {
		t.Fatalf("beta without rf: ok=%v err=%v", ok, err)
	}
	// Latest risk-free date wins.
	for date, rate := range map[string]string{"2026-11-14": "0.0400", "2026-11-17": "0.0412"} {
		if err := q.UpsertRiskFreeRate(ctx, db.UpsertRiskFreeRateParams{Code: "US", RefDate: date, Rate: rate}); err != nil {
			t.Fatal(err)
		}
	}
	b, ok, err := svc.ForAsset(ctx, amd)
	if err != nil || !ok {
		t.Fatalf("amd: ok=%v err=%v", ok, err)
	}
	// 0.0412 + (0.67 x 2.18 + 0.33) x 0.05 = 0.0412 + 1.7906 x 0.05 = 0.13073 -> 0.1307
	if !b.Rate.Equal(dec("0.1307")) || b.Floored || b.RiskFreeDate != "2026-11-17" || b.BetaDate != "2026-11-18" || !b.AdjBeta.Equal(dec("1.7906")) {
		t.Fatalf("amd = %+v", b)
	}

	beta(ko, "0.10")
	b, _, _ = svc.ForAsset(ctx, ko)
	if !b.Floored || !b.Rate.Equal(dec("0.0712")) { // floor 0.0412 + 0.03
		t.Fatalf("ko = %+v", b)
	}

	// B3 uses its own rate and ERP setting.
	if err := q.UpsertRiskFreeRate(ctx, db.UpsertRiskFreeRateParams{Code: "B3", RefDate: "2026-11-17", Rate: "0.1392"}); err != nil {
		t.Fatal(err)
	}
	beta(wege, "1")
	v := settings.DefaultValuation()
	v.ERPB3 = settings.Dec{Decimal: dec("0.06")}
	if _, err := set.SetValuation(ctx, v); err != nil {
		t.Fatal(err)
	}
	b, ok, err = svc.ForAsset(ctx, wege)
	if err != nil || !ok || !b.Rate.Equal(dec("0.1992")) || !b.ERP.Equal(dec("0.06")) {
		t.Fatalf("wege = %+v ok=%v err=%v", b, ok, err)
	}
}
