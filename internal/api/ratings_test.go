package api

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/ratings"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

func TestRatingsAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := assets.NewService(conn)
	rs := ratings.NewService(conn, as, nil)
	calls := 0
	rs.OnChange(func(_ context.Context, _ int64) error { calls++; return nil })
	h := NewRouter(WithAssets(as), WithRatings(rs))
	do(t, h, "POST", "/api/assets", `{"ticker":"WEGE3","market":"B3","kind":"stock"}`)

	if code, _, _ := do(t, h, "GET", "/api/assets/1/rating", ""); code != 404 {
		t.Fatalf("no rating: %d", code)
	}
	code, r, _ := do(t, h, "POST", "/api/assets/1/ratings", `{"rated_on":"2025-01-10","volatility":3,"understanding":4,"moat":5,"predictability":2,"horizon":3,"reason":"initial"}`)
	if code != 201 || r["score"] != "3.4167" {
		t.Fatalf("add: %d %v", code, r)
	}
	do(t, h, "POST", "/api/assets/1/ratings", `{"rated_on":"2025-02-10","volatility":5,"understanding":5,"moat":5,"predictability":5,"horizon":5,"reason":"better"}`)
	if calls != 2 {
		t.Fatalf("hook calls %d", calls)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/1/ratings", `{"volatility":9,"understanding":4,"moat":5,"predictability":2,"horizon":3,"reason":"x"}`); code != 400 {
		t.Fatalf("invalid: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/9/ratings", `{"volatility":1,"understanding":1,"moat":1,"predictability":1,"horizon":1,"reason":"x"}`); code != 404 {
		t.Fatalf("missing asset: %d", code)
	}
	code, _, list := do(t, h, "GET", "/api/assets/1/ratings", "")
	if code != 200 || len(list) != 2 || list[0]["rated_on"] != "2025-02-10" || list[1]["rated_on"] != "2025-01-10" {
		t.Fatalf("history: %d %v", code, list)
	}
	if code, l, _ := do(t, h, "GET", "/api/assets/1/rating", ""); code != 200 || l["reason"] != "better" {
		t.Fatalf("latest: %d %v", code, l)
	}

	code, w, _ := do(t, h, "GET", "/api/settings/rating-weights", "")
	if code != 200 || w["moat"] != "1.5" || w["volatility"] != "1" {
		t.Fatalf("default weights: %v", w)
	}
	code, w, _ = do(t, h, "PUT", "/api/settings/rating-weights", `{"volatility":"2","understanding":"0","moat":"1","predictability":"1","horizon":"1"}`)
	if code != 200 || w["volatility"] != "2" {
		t.Fatalf("put weights: %d %v", code, w)
	}
	if code, _, _ := do(t, h, "PUT", "/api/settings/rating-weights", `{"volatility":"0","understanding":"0","moat":"0","predictability":"0","horizon":"0"}`); code != 400 {
		t.Fatalf("zero weights: %d", code)
	}
	// 2025-01-10 rating (3,4,5,2,3) under new weights: (6+0+5+2+3)/5 = 3.2
	if _, _, list := do(t, h, "GET", "/api/assets/1/ratings", ""); list[1]["score"] != "3.2" {
		t.Fatalf("rescored: %v", list)
	}
}
