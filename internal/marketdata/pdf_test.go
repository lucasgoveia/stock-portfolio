package marketdata

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// buildPDF assembles a minimal one-page PDF with a valid xref table. An empty
// stream yields a page without any text, like a scanned document.
func buildPDF(stream string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

func TestPDFToText(t *testing.T) {
	pdf := buildPDF("BT /F1 12 Tf 20 150 Td (Lucro Liquido Ajustado) Tj 0 -20 Td (de R$ 9 a 11 bilhoes) Tj ET")
	got, err := PDFToText(pdf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Lucro Liquido Ajustado") || !strings.Contains(got, "9 a 11") {
		t.Errorf("text = %q", got)
	}
}

func TestPDFToTextScannedAndGarbage(t *testing.T) {
	if _, err := PDFToText(buildPDF("")); !errors.Is(err, ErrPDFNoText) {
		t.Errorf("image-only pdf error = %v", err)
	}
	if _, err := PDFToText([]byte("%PDF-1.4\nnot really a pdf")); err == nil {
		t.Error("a malformed pdf should be an error")
	}
}

func TestCVMDocumentReadsPDF(t *testing.T) {
	pdf := buildPDF("BT /F1 12 Tf 20 150 Td (Fato Relevante) Tj ET")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" || strings.HasPrefix(r.Header.Get("User-Agent"), "Go-http") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/scan" {
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write(buildPDF(""))
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(pdf)
	}))
	defer srv.Close()
	c := NewCVM(srv.Client(), srv.URL)
	got, err := c.Document(context.Background(), srv.URL+"/doc")
	if err != nil || !strings.Contains(got, "Fato Relevante") {
		t.Errorf("pdf = %q %v", got, err)
	}
	if _, err := c.Document(context.Background(), srv.URL+"/scan"); !errors.Is(err, ErrPDFNoText) {
		t.Errorf("scan error = %v", err)
	}
}
