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

func secServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("User-Agent"), "@") {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>blocked</html>"))
			return
		}
		switch {
		case r.URL.Path == "/submissions/CIK0000320193.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fixture(t, "edgar", "submissions_AAPL_filings.json"))
		case strings.HasSuffix(r.URL.Path, "/index.json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fixture(t, "edgar", "filing_index_AAPL_8K.json"))
		case strings.HasSuffix(r.URL.Path, "a8-kex991q4202609262026.htm"):
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(fixture(t, "edgar", "filing_AAPL_8K_ex991.htm"))
		case strings.HasSuffix(r.URL.Path, "aapl-20261102.htm"):
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(fixture(t, "edgar", "filing_AAPL_8K_cover.htm"))
		case strings.HasSuffix(r.URL.Path, ".pdf"):
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRecentFilings(t *testing.T) {
	srv := secServer(t)
	e := NewEDGAR(nil, srv.URL, srv.URL, "stock-portfolio admin@example.com")
	got, err := e.RecentFilings(context.Background(), "320193", []string{"10-K", "10-Q", "8-K"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want one of each form, got %d: %+v", len(got), got)
	}
	forms := map[string]Filing{}
	for _, f := range got {
		forms[f.Form] = f
	}
	k := forms["10-K"]
	if k.FilingDate != "2026-11-03" || k.URL != srv.URL+"/Archives/edgar/data/320193/000032019326000026/aapl-20260926.htm" {
		t.Errorf("10-K = %+v", k)
	}
	if got[0].FilingDate < got[len(got)-1].FilingDate {
		t.Errorf("not newest first: %+v", got)
	}
	// Form 4 (XSL rendering) is never returned.
	all, err := e.RecentFilings(context.Background(), "320193", []string{"4"}, 5)
	if err != nil || len(all) != 0 {
		t.Errorf("xml forms should be skipped: %v %v", all, err)
	}
	if _, err := e.RecentFilings(context.Background(), "abc", nil, 1); err == nil {
		t.Error("invalid cik should fail")
	}
}

func TestEDGARDocumentAndExhibit(t *testing.T) {
	srv := secServer(t)
	e := NewEDGAR(nil, srv.URL, srv.URL, "stock-portfolio admin@example.com")
	ctx := context.Background()
	filings, err := e.RecentFilings(ctx, "320193", []string{"8-K"}, 3)
	if err != nil || len(filings) == 0 {
		t.Fatalf("filings: %v %v", filings, err)
	}
	var earnings Filing
	for _, f := range filings {
		if strings.Contains(f.Items, "2.02") {
			earnings = f
		}
	}
	if earnings.Accession == "" {
		t.Fatal("no earnings 8-K in fixture")
	}
	ex, err := e.Exhibit99(ctx, earnings)
	if err != nil || !strings.HasSuffix(ex, "a8-kex991q4202609262026.htm") {
		t.Fatalf("exhibit = %q, %v", ex, err)
	}
	text, err := e.Document(ctx, ex, 3000)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "<div") || strings.Contains(text, "<td") || len(text) < 200 {
		t.Errorf("not converted to text: %q", text[:min(len(text), 200)])
	}
	if !strings.Contains(text, "[truncated]") || len([]rune(text)) > 3000+len("\n[truncated]") {
		t.Errorf("not truncated to budget: %d chars", len(text))
	}
	if !strings.Contains(strings.ToLower(text), "apple") {
		t.Errorf("expected press release text, got %q", text[:min(len(text), 300)])
	}
}

func TestEDGARDocumentRejectsOtherTypes(t *testing.T) {
	srv := secServer(t)
	e := NewEDGAR(nil, srv.URL, srv.URL, "stock-portfolio admin@example.com")
	if _, err := e.Document(context.Background(), srv.URL+"/x.pdf", 1000); err == nil || !strings.Contains(err.Error(), "content type") {
		t.Errorf("pdf should be rejected: %v", err)
	}
	// Without a descriptive user agent the SEC serves an HTML block page for
	// JSON endpoints, which the JSON helper refuses.
	bad := NewEDGAR(nil, srv.URL, srv.URL, "x")
	bad.UserAgent = "x"
	if _, err := bad.RecentFilings(context.Background(), "320193", []string{"10-K"}, 1); err == nil {
		t.Error("blocked response should fail")
	}
}

func TestHTMLToText(t *testing.T) {
	in := `<html><head><title>t</title><style>p{}</style></head><body><ix:header>hidden</ix:header>
<script>var a=1</script><p>Revenue   grew <b>10%</b></p><table><tr><td>Q1</td><td>5</td></tr></table><div>end</div></body></html>`
	got := HTMLToText([]byte(in))
	for _, bad := range []string{"hidden", "var a", "p{}"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q leaked into %q", bad, got)
		}
	}
	if !strings.Contains(got, "Revenue grew 10%") || !strings.Contains(got, "Q1 | 5") {
		t.Errorf("got %q", got)
	}
}

func latin1Bytes(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		out = append(out, byte(r))
	}
	return out
}

func TestCVMAnnouncements(t *testing.T) {
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	w, _ := zw.Create("ipe_cia_aberta_2026.csv")
	_, _ = w.Write(latin1Bytes(string(fixture(t, "cvm", "ipe_fato_relevante_petrobras_sample.csv"))))
	_ = zw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/DOC/IPE/DADOS/ipe_cia_aberta_2026.zip" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(zb.Bytes())
	}))
	defer srv.Close()
	c := NewCVM(nil, srv.URL)
	got, err := c.Announcements(context.Background(), "33000167000101", 2026, []string{"Fato Relevante"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d: %+v", len(got), got)
	}
	if got[0].Date < got[1].Date {
		t.Errorf("not newest first: %+v", got)
	}
	if got[0].Subject == "" || !strings.HasPrefix(got[0].URL, "https://www.rad.cvm.gov.br/") || got[0].Category != "Fato Relevante" {
		t.Errorf("entry = %+v", got[0])
	}
	other, err := c.Announcements(context.Background(), "33000167000101", 2026, []string{"Comunicado ao Mercado"}, 5)
	if err != nil || len(other) != 0 {
		t.Errorf("category filter: %v %v", other, err)
	}
	if _, err := c.Announcements(context.Background(), "123", 2026, nil, 1); err == nil {
		t.Error("invalid cnpj should fail")
	}
	if _, err := c.Announcements(context.Background(), "33000167000101", 2020, nil, 1); !IsNotFound(err) {
		t.Errorf("missing year should be not found: %v", err)
	}
}
