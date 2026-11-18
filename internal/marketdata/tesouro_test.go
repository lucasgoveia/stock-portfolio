package marketdata

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseTesouroSelic(t *testing.T) {
	res, err := ParseTesouroCSV(context.Background(), bytes.NewReader(fixture(t, "tesouro", "precotaxatesourodireto_sample.csv")),
		TesouroFilter{Title: TesouroTitleSelic})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 12 {
		t.Fatalf("rows %d", len(res.Rows))
	}
	if !res.Reference.Equal(Date(2026, 11, 17)) {
		t.Errorf("reference %v", res.Reference)
	}
	r := res.Rows[2] // 01/03/2029 17/11/2026
	if !r.Maturity.Equal(Date(2029, 3, 1)) || !r.BaseDate.Equal(Date(2026, 11, 17)) {
		t.Fatalf("row %+v", r)
	}
	dec(t, r.BuyRate, "0.05")
	dec(t, r.SellRate, "0.06")
	dec(t, r.BuyPU, "20300.05")
	dec(t, r.SellPU, "20285.04")
	dec(t, r.BasePU, "20285.04")
}

func TestParseTesouroMaturityAndLatest(t *testing.T) {
	data := fixture(t, "tesouro", "precotaxatesourodireto_sample.csv")
	res, err := ParseTesouroCSV(context.Background(), bytes.NewReader(data),
		TesouroFilter{Title: "tesouro selic", Maturity: Date(2029, 3, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("rows %d", len(res.Rows))
	}
	res, err = ParseTesouroCSV(context.Background(), bytes.NewReader(data),
		TesouroFilter{Title: TesouroTitleSelic, LatestOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 4 {
		t.Fatalf("latest rows %d", len(res.Rows))
	}
	res, _ = ParseTesouroCSV(context.Background(), bytes.NewReader(data),
		TesouroFilter{Title: TesouroTitleSelic, OnOrAfter: Date(2026, 11, 16)})
	if len(res.Rows) != 8 || !res.Reference.Equal(Date(2026, 11, 17)) {
		t.Fatalf("on-or-after: %d %v", len(res.Rows), res.Reference)
	}
	res, _ = ParseTesouroCSV(context.Background(), bytes.NewReader(data), TesouroFilter{Title: "Tesouro Prefixado"})
	if len(res.Rows) != 0 || !res.Reference.IsZero() {
		t.Fatalf("no match: %+v", res)
	}
}

func TestParseTesouroErrors(t *testing.T) {
	if _, err := ParseTesouroCSV(context.Background(), strings.NewReader("a;b\n1;2\n"), TesouroFilter{}); err == nil || !strings.Contains(err.Error(), "missing column") {
		t.Errorf("got %v", err)
	}
	bad := "Tipo Titulo;Data Vencimento;Data Base;Taxa Compra Manha;Taxa Venda Manha;PU Compra Manha;PU Venda Manha;PU Base Manha\nTesouro Selic;xx;17/11/2026;1;1;1;1;1\n"
	if _, err := ParseTesouroCSV(context.Background(), strings.NewReader(bad), TesouroFilter{}); err == nil {
		t.Error("expected bad date error")
	}
}

func TestTesouroClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write(fixture(t, "tesouro", "precotaxatesourodireto_sample.csv"))
	}))
	defer srv.Close()
	res, err := NewTesouro(srv.Client(), srv.URL).Prices(context.Background(), TesouroFilter{Title: TesouroTitleSelic, LatestOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 4 || !res.Reference.Equal(Date(2026, 11, 17)) {
		t.Fatalf("%d %v", len(res.Rows), res.Reference)
	}
}
