package marketdata

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The *_multi_latin1.csv fixtures are trimmed real rows from the 2025 DFP and
// 2026 ITR zips for Itaú, Banco do Brasil, BB Seguridade, Vale, Klabin and
// Taesa (issues #54 and #55).
const (
	cnpjITUB = "60872504000123"
	cnpjBBAS = "00000000000191"
	cnpjBBSE = "17344597000194"
	cnpjVALE = "33592510000154"
	cnpjKLBN = "89637490000145"
	cnpjTAEE = "07859971000130"
)

func multiServer(t *testing.T) *CVM {
	t.Helper()
	// fixture file names carry the year before the kind
	dfp := func() []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, kind := range []string{"DRE_con", "BPA_con", "BPP_con", "composicao_capital"} {
			w, _ := zw.Create("dfp_cia_aberta_" + kind + "_2025.csv")
			_, _ = w.Write(fixture(t, "cvm", "dfp_2025_"+kind+"_multi_latin1.csv"))
		}
		_ = zw.Close()
		return buf.Bytes()
	}()
	itr := func() []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, kind := range []string{"DRE_con", "composicao_capital"} {
			w, _ := zw.Create("itr_cia_aberta_" + kind + "_2026.csv")
			_, _ = w.Write(fixture(t, "cvm", "itr_2026_"+kind+"_multi_latin1.csv"))
		}
		_ = zw.Close()
		return buf.Bytes()
	}()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/DOC/DFP/DADOS/dfp_cia_aberta_2025.zip":
			_, _ = w.Write(dfp)
		case "/DOC/ITR/DADOS/itr_cia_aberta_2026.zip":
			_, _ = w.Write(itr)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return NewCVM(srv.Client(), srv.URL)
}

func annual(t *testing.T, c *CVM, cnpj string) (FinancialPeriod, FinancialFacts) {
	t.Helper()
	f, err := c.AnnualFinancials(context.Background(), cnpj, []int{2025})
	if err != nil || len(f.Periods) != 1 {
		t.Fatalf("%s: %v periods %d", cnpj, err, len(f.Periods))
	}
	return f.Periods[0], f
}

func checkValues(t *testing.T, name string, p FinancialPeriod, want map[string]string) {
	t.Helper()
	for k, v := range want {
		got, ok := p.Values[k]
		if !ok || got.String() != v {
			t.Errorf("%s %s = %s (present %v), want %s", name, k, got.String(), ok, v)
		}
	}
}

func TestCVMBanksAndInsurers(t *testing.T) {
	c := multiServer(t)

	p, f := annual(t, c, cnpjITUB)
	checkValues(t, "ITUB", p, map[string]string{
		ConceptRevenue:   "387118000000",
		ConceptNetIncome: "44857000000",  // 3.09.01, attributable to controlling shareholders
		ConceptEquity:    "204501000000", // 2.08 less non-controlling 2.08.09
		ConceptShares:    "11026524000",  // reported in thousands
	})
	for _, k := range []string{ConceptEBIT, ConceptDebt, ConceptCapex, ConceptOCF, ConceptCash} {
		if _, ok := p.Values[k]; ok {
			t.Errorf("ITUB %s should be omitted for a bank", k)
		}
	}
	if len(f.Notes) == 0 || !strings.Contains(f.Notes[0], "financial-institution") {
		t.Errorf("ITUB notes: %v", f.Notes)
	}

	p, _ = annual(t, c, cnpjBBAS)
	checkValues(t, "BBAS", p, map[string]string{
		ConceptRevenue:   "319462104000",
		ConceptNetIncome: "13698124000", // 3.11.01
		ConceptEquity:    "189207675000",
		ConceptShares:    "5708463641", // already in units, less treasury
	})
	for _, k := range []string{ConceptEBIT, ConceptDebt} {
		if _, ok := p.Values[k]; ok {
			t.Errorf("BBAS %s should be omitted for a bank", k)
		}
	}

	p, f = annual(t, c, cnpjBBSE)
	checkValues(t, "BBSE", p, map[string]string{
		ConceptRevenue:   "0",
		ConceptNetIncome: "9017329000", // 3.13.01 in the insurer layout
		ConceptEquity:    "10384393000",
	})
	var zeroNote bool
	for _, n := range f.Notes {
		zeroNote = zeroNote || strings.Contains(n, "revenue is zero")
	}
	if !zeroNote {
		t.Errorf("BBSE needs a zero-revenue note: %v", f.Notes)
	}
}

func TestCVMShareScaleAndNetIncomeFallbackAnnual(t *testing.T) {
	c := multiServer(t)

	p, _ := annual(t, c, cnpjVALE)
	checkValues(t, "VALE", p, map[string]string{
		ConceptNetIncome: "13814000000",
		ConceptShares:    "4268779000", // 4,539,007 thousand less 270,228 treasury, x1000
	})
	p, _ = annual(t, c, cnpjTAEE)
	checkValues(t, "TAEE", p, map[string]string{ConceptShares: "1033497000", ConceptNetIncome: "1579863000"})
	p, _ = annual(t, c, cnpjKLBN)
	// 3.11.01 is 0: fall back to 3.11 less the (zero) non-controlling share.
	checkValues(t, "KLBN", p, map[string]string{ConceptNetIncome: "1678211000", ConceptShares: "6135427665"})
}

func TestCVMQuarterlyMulti(t *testing.T) {
	c := multiServer(t)
	get := func(cnpj string) []QuarterFact {
		qs, _, err := c.QuarterlyFinancials(context.Background(), cnpj, []int{2026})
		if err != nil {
			t.Fatal(err)
		}
		return qs
	}

	vale := quarterByEnd(t, get(cnpjVALE), "2026-03-31")
	sh, _ := vale.Values[ConceptShares]
	eps, _ := vale.Values[ConceptEPSDiluted]
	if sh.String() != "4087908000" {
		t.Errorf("VALE shares %s", sh.String())
	}
	if e := eps.String(); !strings.HasPrefix(e, "2.43") {
		t.Errorf("VALE EPS not a per-share value: %s", e)
	}

	taee := quarterByEnd(t, get(cnpjTAEE), "2026-09-30")
	got, ok := taee.Values[ConceptNetIncome]
	wantValue(t, "TAEE Q3 net income", got, ok, "360728000")
	got, ok = taee.Values[ConceptShares]
	wantValue(t, "TAEE Q3 shares", got, ok, "1033497000")

	klbn := quarterByEnd(t, get(cnpjKLBN), "2026-09-30")
	got, ok = klbn.Values[ConceptNetIncome]
	wantValue(t, "KLBN Q3 net income", got, ok, "393122000")

	itub := quarterByEnd(t, get(cnpjITUB), "2026-09-30")
	got, ok = itub.Values[ConceptNetIncome]
	wantValue(t, "ITUB Q3 net income", got, ok, "12227000000")
	got, ok = itub.Values[ConceptShares]
	wantValue(t, "ITUB Q3 shares", got, ok, "11022026000")
	got, ok = itub.Values[ConceptRevenue]
	wantValue(t, "ITUB Q3 revenue", got, ok, "110278000000")

	bbas := quarterByEnd(t, get(cnpjBBAS), "2026-09-30")
	got, ok = bbas.Values[ConceptNetIncome]
	wantValue(t, "BBAS Q3 net income", got, ok, "981680000")
}
