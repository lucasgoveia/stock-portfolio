package marketdata

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
)

func TestParseRegisterLatin1(t *testing.T) {
	// "COMPANHIA ÁGUA" in Latin-1 (0xC1 for Á).
	csv := "CNPJ_CIA;DENOM_SOCIAL;DENOM_COMERC;SIT;SIT_EMISSOR;SETOR_ATIV;CD_CVM\n" +
		"61.585.865/0001-51;RAIA DROGASIL S.A.;DROGASIL;ATIVO;FASE OPERACIONAL;Comercio;5258\n" +
		"00.000.000/0000-00;COMPANHIA \xc1GUA;;CANCELADA;;;1\n" +
		"inválido;X;;;;;\n"
	got, err := parseRegister([]byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].CNPJ != "61.585.865/0001-51" || got[0].Name != "RAIA DROGASIL S.A." || got[0].Situation != "ATIVO" || got[1].Name != "COMPANHIA ÁGUA" {
		t.Fatalf("register = %+v", got)
	}
	if _, err := parseRegister([]byte("CNPJ_CIA;DENOM_SOCIAL\n")); err == nil {
		t.Error("an empty register should be an error")
	}
}

func TestParsePeriodicIndex(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("itr_cia_aberta_2026.csv")
	_, _ = w.Write([]byte("CNPJ_CIA;DT_REFER;VERSAO;DT_RECEB;LINK_DOC\n" +
		"61.585.865/0001-51;2026-06-30;1;2026-08-01;https://www.rad.cvm.gov.br/ENET/x\n" +
		"99.999.999/0001-91;2026-06-30;1;2026-08-01;https://other\n"))
	_ = zw.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got, err := parsePeriodicIndex(zr, "61.585.865/0001-51", "ITR", "itr_cia_aberta_2026.csv")
	if err != nil || len(got) != 1 || got[0].URL != "https://www.rad.cvm.gov.br/ENET/x" || got[0].Reference != "2026-06-30" || got[0].Kind != "ITR" {
		t.Fatalf("filings = %+v %v", got, err)
	}
}

func TestLatin1ReaderSmallReads(t *testing.T) {
	src := "ação;AÇÃO;ok\n" + strings.Repeat("x;é\n", 3000)
	got, err := io.ReadAll(iotest.OneByteReader(&latin1Reader{r: bytes.NewReader([]byte(latin1Bytes(src)))}))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != src {
		t.Errorf("converted text differs (len %d vs %d)", len(got), len(src))
	}
}

func TestCVMDocumentRejectsArchiveServedAsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/pdf" {
			_, _ = w.Write([]byte("PK\x03\x04zipdata"))
			return
		}
		_, _ = w.Write([]byte("<html><body><p>Fato relevante</p></body></html>"))
	}))
	defer srv.Close()
	c := NewCVM(srv.Client(), srv.URL)
	if _, err := c.Document(context.Background(), srv.URL+"/pdf"); err == nil || !strings.Contains(err.Error(), "archive") {
		t.Errorf("pdf error = %v", err)
	}
	got, err := c.Document(context.Background(), srv.URL+"/html")
	if err != nil || got != "Fato relevante" {
		t.Errorf("html = %q %v", got, err)
	}
}
