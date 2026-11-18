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
	"github.com/lucasgoveia/stock-portfolio/internal/tactical"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

func TestTacticalAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := assets.NewService(conn)
	al := alerts.New(conn, nil)
	rs := cash.NewRateStore(conn)
	rn := research.NewService(conn, as, nil)
	ts := tactical.NewService(conn, as, rn)
	pf := portfolio.NewService(portfolio.Deps{
		Conn: conn, Assets: as, Trades: trades.NewService(conn, as), Cash: cash.NewService(conn, rs, nil), Rates: rs,
		Ratings: ratings.NewService(conn, as, nil), Valuation: valuation.NewService(conn, as, al, nil),
		Thesis: thesis.NewService(conn, as, al, nil), Research: rn, Alerts: al, Tactical: ts,
	})
	h := NewRouter(WithAssets(as), WithResearch(rn), WithTactical(ts, pf))

	do(t, h, "POST", "/api/assets", `{"ticker":"PRIO3","market":"B3","kind":"stock","sleeve":"tactical"}`)
	do(t, h, "POST", "/api/assets/1/notes", `{"text":"FPSO start-up"}`)
	if code, _, _ := do(t, h, "GET", "/api/assets/1/tactical", ""); code != 404 {
		t.Fatalf("get before put: %d", code)
	}
	body := `{"note_id":1,"entry_price":"40","target_price":"52","stop_price":"36","expires_on":"2027-03-31"}`
	code, st, _ := do(t, h, "PUT", "/api/assets/1/tactical", body)
	if code != 200 || st["target_price"] != "52" {
		t.Fatalf("put: %d %v", code, st)
	}
	if sz, _ := st["sizing"].(map[string]any); sz["size"] != "0.02" || sz["stop_distance"] != "0.1" {
		t.Fatalf("sizing: %v", st["sizing"])
	}
	if code, _, _ := do(t, h, "PUT", "/api/assets/1/tactical", `{"note_id":1,"entry_price":"40","target_price":"30","stop_price":"36","expires_on":"2027-03-31"}`); code != 400 {
		t.Fatalf("target below entry: %d", code)
	}
	if code, _, _ := do(t, h, "PUT", "/api/assets/1/tactical", `{"note_id":99,"entry_price":"40","target_price":"52","stop_price":"36","expires_on":"2027-03-31"}`); code != 400 {
		t.Fatalf("missing note: %d", code)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/notes/1", ""); code != 400 {
		t.Fatalf("delete catalyst note: %d", code)
	}
	// No rating or price yet: promotion is refused with a reason.
	if code, r, _ := do(t, h, "POST", "/api/assets/1/promote", ""); code != 400 || r["error"] == nil {
		t.Fatalf("promote: %d %v", code, r)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/assets/1/tactical", ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/assets/1/tactical", ""); code != 404 {
		t.Fatalf("delete again: %d", code)
	}
}
