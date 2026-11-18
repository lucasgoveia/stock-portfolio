package marketdata

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseB3IndexMatrix2026UTF8(t *testing.T) {
	pts, err := ParseB3IndexMatrix(fixture(t, "index", "b3_ibov_2026_matrix.csv"))
	if err != nil {
		t.Fatal(err)
	}
	byDate := map[string]string{}
	for _, p := range pts {
		byDate[p.Date.Format("2006-01-02")] = p.Close.String()
	}
	if byDate["2026-11-03"] != "165528.92" {
		t.Errorf("2026-11-03 = %s", byDate["2026-11-03"])
	}
	if byDate["2026-01-02"] != "160538.69" {
		t.Errorf("2026-01-02 = %s", byDate["2026-01-02"])
	}
	// The data doc says 2026-11-17 closed at 169,816.64.
	if byDate["2026-11-17"] != "169816.64" {
		t.Errorf("2026-11-17 = %s", byDate["2026-11-17"])
	}
	for i := 1; i < len(pts); i++ {
		if !pts[i].Date.After(pts[i-1].Date) {
			t.Fatalf("not sorted at %d", i)
		}
	}
	// Summary rows (MINIMO/MAXIMO) must not leak in.
	if _, ok := byDate["0001-01-01"]; ok {
		t.Error("summary leaked")
	}
}

func TestParseB3IndexMatrix2025Latin1(t *testing.T) {
	raw := fixture(t, "index", "ibov_2025_b3_matrix.csv")
	pts, err := ParseB3IndexMatrix(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) < 200 {
		t.Fatalf("only %d points", len(pts))
	}
	if pts[0].Date.Year() != 2025 {
		t.Errorf("year %v", pts[0].Date)
	}
	dec(t, pts[0].Close, "120125.39")
}

func TestParseB3IndexMatrixErrors(t *testing.T) {
	for _, in := range []string{"", "hello\n", "IBOV - 2026\nfoo;bar\n", "IBOV - 2026\nDia;Xyz\n"} {
		if _, err := ParseB3IndexMatrix([]byte(in)); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
	if _, err := ParseB3IndexMatrix([]byte("IBOV - 2026\nDia;Fev\n30;1.000,00\n")); err == nil {
		t.Error("expected invalid date error")
	}
}

func TestB3IndexYearClient(t *testing.T) {
	csvBody := fixture(t, "index", "b3_ibov_2026_matrix.csv")
	var gotParam string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotParam = strings.TrimPrefix(r.URL.Path, "/indexStatisticsProxy/IndexCall/GetDownloadPortfolioDay/")
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(csvBody)))
	}))
	defer srv.Close()
	pts, err := NewB3Index(srv.Client(), srv.URL, "IBOV").Year(context.Background(), 2026)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := base64.StdEncoding.DecodeString(gotParam)
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]string
	if err := json.Unmarshal(dec, &p); err != nil {
		t.Fatal(err)
	}
	if p["index"] != "IBOV" || p["year"] != "2026" || p["language"] != "pt-br" {
		t.Errorf("param %v", p)
	}
	if len(pts) == 0 {
		t.Error("no points")
	}
}

func TestB3IndexRawCSVBodyAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(fixture(t, "index", "ibov_2025_b3_matrix.csv"))
	}))
	defer srv.Close()
	pts, err := NewB3Index(srv.Client(), srv.URL, "IBOV").Year(context.Background(), 2025)
	if err != nil || len(pts) == 0 {
		t.Fatalf("%v %d", err, len(pts))
	}
}

func TestParseFREDCSV(t *testing.T) {
	pts, err := ParseFREDCSV(bytes.NewReader(fixture(t, "index", "fred_sp500.csv")))
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 11 {
		t.Fatalf("got %d", len(pts))
	}
	if !pts[10].Date.Equal(Date(2026, 11, 16)) {
		t.Errorf("last %v", pts[10].Date)
	}
	dec(t, pts[10].Close, "7416.59")
	// Holiday blanks and "." are skipped.
	pts, err = ParseFREDCSV(strings.NewReader("observation_date,SP500\n2026-11-26,\n2026-11-27,.\n2026-11-30,7000.10\n"))
	if err != nil || len(pts) != 1 {
		t.Fatalf("%v %v", err, pts)
	}
	if _, err := ParseFREDCSV(strings.NewReader("<html>")); err == nil {
		t.Error("expected header error")
	}
}

func TestFREDClient(t *testing.T) {
	var q string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write(fixture(t, "index", "fred_sp500.csv"))
	}))
	defer srv.Close()
	pts, err := NewFRED(srv.Client(), srv.URL, "SP500").Closes(context.Background(), Date(2026, 11, 1))
	if err != nil {
		t.Fatal(err)
	}
	if q != "id=SP500&cosd=2026-11-01" || len(pts) != 11 {
		t.Errorf("%s %d", q, len(pts))
	}
}
