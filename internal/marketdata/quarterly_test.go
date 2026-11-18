package marketdata

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// itrZip builds an ITR zip of one year from the Latin-1 Petrobras fixtures.
func itrZip(t *testing.T, year string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for kind, file := range map[string]string{
		"DRE_con":            "itr_" + year + "_DRE_con_petrobras_latin1.csv",
		"composicao_capital": "itr_" + year + "_composicao_capital_petrobras_latin1.csv",
	} {
		w, err := zw.Create("itr_cia_aberta_" + kind + "_" + year + ".csv")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(fixture(t, "cvm", file)); err != nil {
			t.Fatal(err)
		}
	}
	w, err := zw.Create("itr_cia_aberta_" + year + ".csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(fixture(t, "cvm", "itr_"+year+"_index_petrobras_latin1.csv")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func quarterByEnd(t *testing.T, qs []QuarterFact, end string) QuarterFact {
	t.Helper()
	for _, q := range qs {
		if q.End.Format("2006-01-02") == end {
			return q
		}
	}
	t.Fatalf("no quarter ending %s in %d quarters", end, len(qs))
	return QuarterFact{}
}

func wantValue(t *testing.T, name string, got interface{ String() string }, ok bool, want string) {
	t.Helper()
	if !ok || got.String() != want {
		t.Errorf("%s = %s (present %v), want %s", name, got.String(), ok, want)
	}
}

func TestCVMQuarterlyFinancialsITR(t *testing.T) {
	zips := map[string][]byte{
		"/DOC/ITR/DADOS/itr_cia_aberta_2025.zip": itrZip(t, "2025"),
		"/DOC/ITR/DADOS/itr_cia_aberta_2026.zip": itrZip(t, "2026"),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := zips[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	// 2027 has no file and is skipped silently.
	qs, notes, err := NewCVM(srv.Client(), srv.URL).QuarterlyFinancials(context.Background(), "33.000.167/0001-01", []int{2027, 2026, 2025})
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 6 || len(notes) != 0 {
		t.Fatalf("quarters %d notes %v", len(qs), notes)
	}
	for i := 1; i < len(qs); i++ {
		if !qs[i].End.After(qs[i-1].End) {
			t.Fatalf("not ascending at %d", i)
		}
	}

	// Q1 has one column: the three months are the year to date. VERSAO 2 wins.
	q1 := quarterByEnd(t, qs, "2026-03-31")
	if q1.Quarter != 1 || q1.Filed != "2026-05-11" {
		t.Errorf("Q1 2026: quarter %d filed %s", q1.Quarter, q1.Filed)
	}
	// Revenue is in thousands in the file: 123686000 x 1000.
	got, ok := q1.Values[ConceptRevenue]
	wantValue(t, "Q1 revenue", got, ok, "123686000000")
	got, ok = q1.YTD[ConceptRevenue]
	wantValue(t, "Q1 ytd revenue", got, ok, "123686000000")
	got, ok = q1.Values[ConceptNetIncome] // 3.11.01, not 3.11
	wantValue(t, "Q1 net income", got, ok, "32663000000")

	// Q2 carries both the 3-month and the year-to-date column: take the 3-month.
	q2 := quarterByEnd(t, qs, "2026-06-30")
	if q2.Quarter != 2 {
		t.Errorf("Q2 quarter = %d", q2.Quarter)
	}
	got, ok = q2.Values[ConceptRevenue]
	wantValue(t, "Q2 revenue", got, ok, "169530000000")
	got, ok = q2.YTD[ConceptRevenue]
	wantValue(t, "Q2 ytd revenue", got, ok, "293216000000")
	got, ok = q2.Values[ConceptNetIncome]
	wantValue(t, "Q2 net income", got, ok, "52445000000")
	// EPS = net income / (total shares - treasury) = 52.445bn / 12,888,732,761.
	got, ok = q2.Values[ConceptEPSDiluted]
	wantValue(t, "Q2 eps", got, ok, "4.069058")
	if q2.Filed != "2026-08-06" {
		t.Errorf("Q2 filed = %s", q2.Filed)
	}

	q3 := quarterByEnd(t, qs, "2025-09-30")
	if q3.Quarter != 3 {
		t.Errorf("Q3 quarter = %d", q3.Quarter)
	}
	got, ok = q3.Values[ConceptRevenue]
	wantValue(t, "Q3 revenue", got, ok, "127906000000")
	got, ok = q3.YTD[ConceptRevenue]
	wantValue(t, "Q3 ytd revenue", got, ok, "370178000000")
}

func TestCVMFourthQuarterFromDFP(t *testing.T) {
	// Annual figures from the DFP fixture (synthetic restated revenue 500 bn),
	// Q3 year to date from the 2025 ITR.
	periods, _, _, err := func() ([]FinancialPeriod, string, []string, error) {
		zr, err := zip.NewReader(bytes.NewReader(dfpZip(t)), int64(len(dfpZip(t))))
		if err != nil {
			t.Fatal(err)
		}
		return parseDFP(zr, "33.000.167/0001-01", 2025)
	}()
	if err != nil {
		t.Fatal(err)
	}
	b := itrZip(t, "2025")
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	qs, _, err := parseITR(zr, "33.000.167/0001-01", 2025)
	if err != nil {
		t.Fatal(err)
	}
	all := DeriveFourthQuarters(periods, qs)
	if len(all) != 4 {
		t.Fatalf("quarters: %d", len(all))
	}
	q4 := quarterByEnd(t, all, "2025-12-31")
	if q4.Quarter != 4 || !q4.Derived {
		t.Errorf("Q4: %+v", q4)
	}
	// 500,000,000,000 annual - 370,178,000,000 nine months.
	got, ok := q4.Values[ConceptRevenue]
	wantValue(t, "Q4 revenue", got, ok, "129822000000")
	// 110,129,000,000 annual parent net income - 94,566,000,000 nine months.
	got, ok = q4.Values[ConceptNetIncome]
	wantValue(t, "Q4 net income", got, ok, "15563000000")
	// No reported EPS in the DFP: net income over the year-end shares.
	got, ok = q4.Values[ConceptEPSDiluted]
	wantValue(t, "Q4 eps", got, ok, "1.207489")
	// Deriving again changes nothing.
	if again := DeriveFourthQuarters(periods, all); len(again) != 4 {
		t.Errorf("second derive: %d quarters", len(again))
	}
}

func TestEDGARQuarters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "edgar", "companyfacts_AAPL_quarterly.json"))
	}))
	defer srv.Close()
	f, err := NewEDGAR(srv.Client(), srv.URL, srv.URL, "test ops@example.com").Financials(context.Background(), "320193")
	if err != nil {
		t.Fatal(err)
	}
	qs := f.Quarters
	if len(qs) != 5 {
		t.Fatalf("10-Q quarters: %d", len(qs))
	}
	// Q1 FY2026: one three-month fact, which is also the year to date.
	q1 := quarterByEnd(t, qs, "2025-12-27")
	if q1.Quarter != 1 {
		t.Errorf("Q1 quarter = %d", q1.Quarter)
	}
	got, ok := q1.Values[ConceptRevenue]
	wantValue(t, "Q1 revenue", got, ok, "143756000000")
	got, ok = q1.YTD[ConceptRevenue]
	wantValue(t, "Q1 ytd revenue", got, ok, "143756000000")
	got, ok = q1.Values[ConceptEPSDiluted]
	wantValue(t, "Q1 eps", got, ok, "2.84")

	// Q3 FY2026: three months (2026-03-29 to 06-27) and nine months to date.
	q3 := quarterByEnd(t, qs, "2026-06-27")
	if q3.Quarter != 3 || q3.Filed != "2026-07-31" {
		t.Errorf("Q3: quarter %d filed %s", q3.Quarter, q3.Filed)
	}
	got, ok = q3.Values[ConceptRevenue]
	wantValue(t, "Q3 revenue", got, ok, "109417000000")
	got, ok = q3.YTD[ConceptRevenue]
	wantValue(t, "Q3 ytd revenue", got, ok, "364357000000")
	got, ok = q3.YTD[ConceptEPSDiluted]
	wantValue(t, "Q3 ytd eps", got, ok, "6.88")
	got, ok = q3.Values[ConceptSharesDilAvg]
	wantValue(t, "Q3 shares", got, ok, "14714676000")

	// Q2 FY2025 (period ending 2025-06-28) carries a 6-month and a 3-month fact.
	q2 := quarterByEnd(t, qs, "2025-06-28")
	if q2.Quarter != 3 { // FY2025 Q3: ends June 28 for a September fiscal year
		t.Errorf("quarter for 2025-06-28 = %d", q2.Quarter)
	}

	// Q4 FY2026 = 10-K FY (478,834) minus nine months (364,357).
	all := DeriveFourthQuarters(f.Periods, qs)
	q4 := quarterByEnd(t, all, "2026-09-26")
	if q4.Quarter != 4 || !q4.Derived || q4.Filed != "2026-11-03" {
		t.Errorf("Q4: %+v", q4)
	}
	got, ok = q4.Values[ConceptRevenue]
	wantValue(t, "Q4 revenue", got, ok, "114477000000")
	got, ok = q4.Values[ConceptNetIncome]
	wantValue(t, "Q4 net income", got, ok, "30711000000")
	got, ok = q4.Values[ConceptEPSDiluted] // 8.98 - 6.88
	wantValue(t, "Q4 eps", got, ok, "2.1")
	// FY2025 Q4 is derived too when its Q3 is present.
	// FY2025 has its Q3 (2025-06-28) and 10-K, so its Q4 is derived as well.
	if len(all) != len(qs)+2 {
		t.Errorf("quarters with Q4: %d, want %d", len(all), len(qs)+2)
	}
	q4old := quarterByEnd(t, all, "2025-09-27")
	got, ok = q4old.Values[ConceptRevenue] // 416,161 - 313,695
	wantValue(t, "FY2025 Q4 revenue", got, ok, "102466000000")
}
