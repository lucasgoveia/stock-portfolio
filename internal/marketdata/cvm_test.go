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

// dfpZip builds a DFP zip for year 2025 from the Latin-1 Petrobras fixtures.
func dfpZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, kind := range []string{"DRE_con", "BPA_con", "BPP_con", "DFC_MI_con", "composicao_capital"} {
		w, err := zw.Create("dfp_cia_aberta_" + kind + "_2025.csv")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(fixture(t, "cvm", "dfp_2025_"+kind+"_petrobras_latin1.csv")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCVMAnnualFinancials(t *testing.T) {
	zipBytes := dfpZip(t)
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/DOC/DFP/DADOS/dfp_cia_aberta_2025.zip" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()

	f, err := NewCVM(srv.Client(), srv.URL).AnnualFinancials(context.Background(), "33000167000101", []int{2026, 2025})
	if err != nil {
		t.Fatal(err)
	}
	if f.Source != "cvm" || f.Currency != "BRL" || !strings.Contains(f.Entity, "PETROLEO") {
		t.Errorf("header: %+v", f)
	}
	if len(f.Periods) != 1 {
		t.Fatalf("periods: %d", len(f.Periods))
	}
	p := f.Periods[0]
	if p.End != Date(2025, 12, 31) || p.Type != PeriodAnnual {
		t.Errorf("period %v %s", p.End, p.Type)
	}
	want := map[string]string{
		// 3.01 comes from VERSAO 2 (restated), thousands scaled to units.
		ConceptRevenue:   "500000000000",
		ConceptEBIT:      "145628000000",
		ConceptNetIncome: "110129000000", // 3.11.01, not the consolidated 3.11
		ConceptEquity:    "415786000000", // 2.03 less non-controlling 2.03.09
		ConceptCash:      "50608000000",  // 1.01.01 + 1.01.02
		ConceptDebt:      "384025000000", // 2.01.04 + 2.02.01
		ConceptOCF:       "200333000000",
		ConceptCapex:     "108714000000",
		ConceptDA:        "84388000000",
		ConceptShares:    "12888732761",
	}
	for k, v := range want {
		got, ok := p.Values[k]
		if !ok || got.String() != v {
			t.Errorf("%s = %s (present %v), want %s", k, got.String(), ok, v)
		}
	}
	if len(f.Notes) != 1 || !strings.Contains(f.Notes[0], "2026") {
		t.Errorf("notes: %v", f.Notes)
	}
}

func TestCVMUnknownCompanyAndBadCNPJ(t *testing.T) {
	zipBytes := dfpZip(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(zipBytes) }))
	defer srv.Close()
	c := NewCVM(srv.Client(), srv.URL)
	f, err := c.AnnualFinancials(context.Background(), "11.444.777/0001-61", []int{2025})
	if err != nil || len(f.Periods) != 0 || len(f.Notes) == 0 {
		t.Errorf("unknown company: %+v %v", f, err)
	}
	if _, err := c.AnnualFinancials(context.Background(), "123", []int{2025}); err == nil {
		t.Error("expected invalid CNPJ error")
	}
}

func TestLatin1Decoding(t *testing.T) {
	// "Participação" and "ÚLTIMO" in Latin-1 must decode to UTF-8.
	in := []byte{'P', 'a', 'r', 't', 'i', 'c', 'i', 'p', 'a', 0xE7, 0xE3, 'o', ';', 0xDA, 'L', 'T', 'I', 'M', 'O'}
	var out bytes.Buffer
	buf := make([]byte, 64)
	l := &latin1Reader{r: bytes.NewReader(in)}
	for {
		n, err := l.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if out.String() != "Participação;ÚLTIMO" {
		t.Errorf("got %q", out.String())
	}
}

func TestCNPJ(t *testing.T) {
	if got := FormatCNPJ("33000167000101"); got != "33.000.167/0001-01" {
		t.Errorf("format: %q", got)
	}
	if FormatCNPJ("1234") != "" {
		t.Error("short CNPJ formatted")
	}
	if !ValidCNPJ("33.000.167/0001-01") || ValidCNPJ("33.000.167/0001-02") || ValidCNPJ("11.111.111/1111-11") {
		t.Error("check digit validation")
	}
}
