package marketdata

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// RegisterEntry is one company in the CVM register of listed companies
// (cad_cia_aberta.csv).
type RegisterEntry struct {
	CNPJ      string `json:"cnpj" doc:"XX.XXX.XXX/XXXX-XX"`
	Name      string `json:"name" doc:"Registered corporate name (DENOM_SOCIAL)"`
	TradeName string `json:"trade_name,omitempty"`
	// Situation is the registration status (for example ATIVO, CANCELADA) and
	// IssuerSituation the issuer's phase (for example FASE OPERACIONAL).
	Situation       string `json:"situation"`
	IssuerSituation string `json:"issuer_situation,omitempty"`
	Sector          string `json:"sector,omitempty"`
	CVMCode         string `json:"cvm_code,omitempty"`
}

// Register downloads the CVM register of listed companies (Latin-1 CSV).
func (c *CVM) Register(ctx context.Context) ([]RegisterEntry, error) {
	url := c.BaseURL + "/CAD/DADOS/cad_cia_aberta.csv"
	b, err := getBytes(ctx, c.HTTP, "cvm", url, nil)
	if err != nil {
		return nil, err
	}
	return parseRegister(b)
}

func parseRegister(b []byte) ([]RegisterEntry, error) {
	r := csv.NewReader(strings.NewReader(decodeText(b)))
	r.Comma = ';'
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("marketdata: cvm: register: reading header: %w", err)
	}
	idx := headerIndex(head)
	var out []RegisterEntry
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("marketdata: cvm: register: %w", err)
		}
		cnpj := FormatCNPJ(col(rec, idx, "CNPJ_CIA"))
		if cnpj == "" {
			continue
		}
		out = append(out, RegisterEntry{
			CNPJ: cnpj, Name: strings.TrimSpace(col(rec, idx, "DENOM_SOCIAL")), TradeName: strings.TrimSpace(col(rec, idx, "DENOM_COMERC")),
			Situation: strings.TrimSpace(col(rec, idx, "SIT")), IssuerSituation: strings.TrimSpace(col(rec, idx, "SIT_EMISSOR")),
			Sector: strings.TrimSpace(col(rec, idx, "SETOR_ATIV")), CVMCode: strings.TrimSpace(col(rec, idx, "CD_CVM")),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("marketdata: cvm: register: no companies found")
	}
	return out, nil
}

// PeriodicFiling is one ITR (quarterly) or DFP (annual) filing of a company.
type PeriodicFiling struct {
	Kind      string `json:"kind" enum:"ITR,DFP"`
	Reference string `json:"reference_date" doc:"Period end, YYYY-MM-DD"`
	Received  string `json:"received" doc:"Date the CVM received it"`
	Version   string `json:"version"`
	URL       string `json:"url" doc:"CVM RAD link; the document itself may not be readable text"`
}

// PeriodicFilings lists the company's ITR or DFP filings (kind "ITR" or "DFP")
// from the index file of each year, newest first. It downloads each year's
// zip, so callers should cache.
func (c *CVM) PeriodicFilings(ctx context.Context, cnpj, kind string, years []int) ([]PeriodicFiling, error) {
	formatted := FormatCNPJ(cnpj)
	if formatted == "" {
		return nil, fmt.Errorf("marketdata: cvm: invalid CNPJ %q", cnpj)
	}
	kind = strings.ToUpper(kind)
	if kind != "ITR" && kind != "DFP" {
		return nil, fmt.Errorf("marketdata: cvm: kind must be ITR or DFP")
	}
	lower := strings.ToLower(kind)
	var out []PeriodicFiling
	for _, y := range years {
		var (
			ps     []PeriodicFiling
			parsed error
		)
		err := c.withZip(ctx, fmt.Sprintf("%s/DOC/%s/DADOS/%s_cia_aberta_%d.zip", c.BaseURL, kind, lower, y), func(zr *zip.Reader) {
			ps, parsed = parsePeriodicIndex(zr, formatted, kind, fmt.Sprintf("%s_cia_aberta_%d.csv", lower, y))
		})
		if IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if parsed != nil {
			return nil, parsed
		}
		out = append(out, ps...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Reference != out[j].Reference {
			return out[i].Reference > out[j].Reference
		}
		return out[i].Version > out[j].Version
	})
	return out, nil
}

func parsePeriodicIndex(zr *zip.Reader, cnpj, kind, name string) ([]PeriodicFiling, error) {
	var file *zip.File
	for _, f := range zr.File {
		if f.Name == name {
			file = f
		}
	}
	if file == nil {
		return nil, nil
	}
	r, closer, err := openCSV(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer.Close() }()
	head, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("marketdata: cvm: %s: reading header: %w", name, err)
	}
	idx := headerIndex(head)
	var out []PeriodicFiling
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("marketdata: cvm: %s: %w", name, err)
		}
		if col(rec, idx, "CNPJ_CIA") != cnpj {
			continue
		}
		out = append(out, PeriodicFiling{
			Kind: kind, Reference: col(rec, idx, "DT_REFER"), Received: col(rec, idx, "DT_RECEB"),
			Version: col(rec, idx, "VERSAO"), URL: strings.TrimSpace(col(rec, idx, "LINK_DOC")),
		})
	}
}

// Document fetches a CVM document and returns its text. Only HTML and plain
// text are readable; PDFs and archives are reported as unsupported.
func (c *CVM) Document(ctx context.Context, url string) (string, error) {
	resp, err := get(ctx, c.HTTP, "cvm", url, map[string]string{"Accept": "text/html,text/plain"})
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	isHTML := strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml")
	if !isHTML && !strings.Contains(ct, "text/plain") {
		return "", fmt.Errorf("marketdata: cvm: the document is not readable text (content type %q); open the link in a browser", ct)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxFilingBytes))
	if err != nil {
		return "", fmt.Errorf("marketdata: cvm: reading %s: %w", url, err)
	}
	// CVM serves PDFs and zips under a text content type: go by the bytes.
	if bytes.HasPrefix(b, []byte("%PDF-")) || bytes.HasPrefix(b, []byte("PK\x03\x04")) {
		return "", fmt.Errorf("marketdata: cvm: the document is a PDF or archive, not readable text; open the link in a browser")
	}
	if isHTML {
		return HTMLToText(bytes.NewBufferString(decodeText(b)).Bytes()), nil
	}
	return decodeText(b), nil
}
