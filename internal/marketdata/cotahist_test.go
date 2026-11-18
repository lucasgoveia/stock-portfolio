package marketdata

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCotahistFileNamesAndURLs(t *testing.T) {
	d := Date(2026, 11, 17)
	cases := map[CotahistFileKind]string{
		CotahistYearly:  "COTAHIST_A2026.ZIP",
		CotahistMonthly: "COTAHIST_M112026.ZIP",
		CotahistDaily:   "COTAHIST_D17112026.ZIP",
	}
	for k, want := range cases {
		if got := CotahistFileName(k, d); got != want {
			t.Errorf("kind %d: got %s want %s", k, got, want)
		}
	}
	if got := CotahistURL("", CotahistDaily, d); got != DefaultCotahistBaseURL+"COTAHIST_D17112026.ZIP" {
		t.Errorf("default url: %s", got)
	}
	if got := CotahistURL("http://x/y/", CotahistYearly, d); got != "http://x/y/COTAHIST_A2026.ZIP" {
		t.Errorf("custom url: %s", got)
	}
}

func TestParseCotahistPETR4(t *testing.T) {
	recs, err := ParseCotahist(context.Background(), bytes.NewReader(fixture(t, "cotahist", "COTAHIST_sample.TXT")), LotFilter("PETR4"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	r := recs[0]
	if !r.Date.Equal(Date(2026, 11, 17)) || r.Ticker != "PETR4" || r.Market != "010" || r.BDI != "02" {
		t.Errorf("bad header fields: %+v", r)
	}
	dec(t, r.Close, "43.98")
	dec(t, r.Open, "44.3")
	dec(t, r.High, "44.75")
	dec(t, r.Low, "43.9")
	dec(t, r.Avg, "44.27")
	if r.Trades != 35371 {
		t.Errorf("trades %d", r.Trades)
	}
	if r.ISIN != "BRPETRACNPR6" || r.ShortName != "PETROBRAS" || r.Spec != "PN      N2" {
		t.Errorf("isin/name/spec: %q %q %q", r.ISIN, r.ShortName, r.Spec)
	}
	if r.QuoteFact != 1 {
		t.Errorf("fatcot %d", r.QuoteFact)
	}
}

func TestParseCotahistFractionalAndMarkets(t *testing.T) {
	data := fixture(t, "cotahist", "COTAHIST_sample.TXT")
	frac, err := ParseCotahist(context.Background(), bytes.NewReader(data), FractionalFilter("PETR4"))
	if err != nil {
		t.Fatal(err)
	}
	if len(frac) != 1 || frac[0].Ticker != "PETR4F" || frac[0].Market != MarketFractional {
		t.Fatalf("fractional: %+v", frac)
	}
	dec(t, frac[0].Close, "44.02")
	// The lot filter must not return the fractional row, nor options.
	lot, _ := ParseCotahist(context.Background(), bytes.NewReader(data), LotFilter("PETR4F", "PETRJ631"))
	if len(lot) != 0 {
		t.Errorf("lot filter leaked %+v", lot)
	}
	// Whole file, lot market only: options (070) and fractionals excluded.
	all, err := ParseCotahist(context.Background(), bytes.NewReader(data), CotahistFilter{Markets: []string{MarketLot}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range all {
		if r.Market != MarketLot {
			t.Errorf("unexpected market %s", r.Market)
		}
	}
	// DIRR3 has 4 lot rows over 4 days; the split is visible in raw closes.
	var dirr []CotahistRecord
	for _, r := range all {
		if r.Ticker == "DIRR3" {
			dirr = append(dirr, r)
		}
	}
	if len(dirr) != 4 {
		t.Fatalf("DIRR3 rows: %d", len(dirr))
	}
}

func TestParseCotahistUTF8RecordOffsets(t *testing.T) {
	data := fixture(t, "cotahist", "COTAHIST_sample.TXT")
	if !bytes.Contains(data, []byte("OCEÂNI")) {
		t.Fatal("fixture lost its UTF-8 record")
	}
	check := func(name string, data []byte) {
		recs, err := ParseCotahist(context.Background(), bytes.NewReader(data), CotahistFilter{Tickers: []string{"FR0T5L"}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(recs) != 1 {
			t.Fatalf("%s: got %d records", name, len(recs))
		}
		r := recs[0]
		if r.ShortName != "FROTA OCEÂNI" {
			t.Errorf("%s: name %q", name, r.ShortName)
		}
		if r.Market != "017" || !r.Date.Equal(Date(2026, 10, 22)) || r.BDI != "52" {
			t.Errorf("%s: shifted offsets: %+v", name, r)
		}
		dec(t, r.Close, "0.05")
		if r.ISIN == "" {
			t.Errorf("%s: ISIN lost", name)
		}
	}
	check("utf8", data)
	// Same file encoded as Latin-1: every rune is below 256, so convert.
	var lat []byte
	for _, r := range string(data) {
		if r >= 256 {
			t.Fatalf("rune %q not latin1", r)
		}
		lat = append(lat, byte(r))
	}
	if utf8.Valid(lat) {
		t.Fatal("expected invalid UTF-8 in latin1 version")
	}
	check("latin1", lat)
}

func TestParseCotahistShortLine(t *testing.T) {
	_, err := ParseCotahist(context.Background(), strings.NewReader("012026111702PETR4       010\r\n"), CotahistFilter{})
	if err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("want line error, got %v", err)
	}
}

func TestParseCotahistContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	big := strings.Repeat("00x\r\n", 10000)
	if _, err := ParseCotahist(ctx, strings.NewReader(big), CotahistFilter{}); err == nil {
		t.Fatal("expected context error")
	}
}

func zipOf(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCotahistClientScanZip(t *testing.T) {
	z := zipOf(t, "COTAHIST_D17112026.TXT", fixture(t, "cotahist", "COTAHIST_sample.TXT"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/SerHist/COTAHIST_D17112026.ZIP" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(z)
	}))
	defer srv.Close()
	c := NewCotahist(srv.Client(), srv.URL+"/SerHist/")
	var got []CotahistRecord
	err := c.Scan(context.Background(), CotahistDaily, Date(2026, 11, 17), LotFilter("PETR4", "BOVA11"), func(r CotahistRecord) error {
		got = append(got, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	dec(t, got[0].Close, "43.98")

	err = c.Scan(context.Background(), CotahistDaily, Date(2026, 11, 18), CotahistFilter{}, func(CotahistRecord) error { return nil })
	if !IsNotFound(err) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestScanCotahistZipWithoutTxt(t *testing.T) {
	z := zipOf(t, "readme.md", []byte("x"))
	zr, err := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	if err := ScanCotahistZip(context.Background(), zr, CotahistFilter{}, nil); err == nil {
		t.Fatal("expected error")
	}
}

var _ = time.UTC
