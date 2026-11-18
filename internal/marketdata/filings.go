package marketdata

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Filing is one SEC filing from the submissions list.
type Filing struct {
	Form       string `json:"form"`
	FilingDate string `json:"filing_date"`
	ReportDate string `json:"report_date,omitempty"`
	Items      string `json:"items,omitempty"`
	Accession  string `json:"accession"`
	// Title is a short human description, URL the primary document.
	Title string `json:"title"`
	URL   string `json:"url"`
	// Dir is the filing's directory URL (no trailing slash).
	Dir string `json:"-"`
}

type edgarSubmissions struct {
	Filings struct {
		Recent struct {
			Accession  []string `json:"accessionNumber"`
			FilingDate []string `json:"filingDate"`
			ReportDate []string `json:"reportDate"`
			Form       []string `json:"form"`
			Primary    []string `json:"primaryDocument"`
			Desc       []string `json:"primaryDocDescription"`
			Items      []string `json:"items"`
		} `json:"recent"`
	} `json:"filings"`
	Name string `json:"name"`
}

func at2(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

// RecentFilings lists the newest filings of the given forms (exact match, for
// example "10-K", "10-Q", "8-K") from the company's submissions, at most
// perForm of each, newest first. Only HTML or text primary documents are
// returned (XML renderings such as Form 4 are skipped).
func (e *EDGAR) RecentFilings(ctx context.Context, cik string, forms []string, perForm int) ([]Filing, error) {
	c := FormatCIK(cik)
	if c == "" {
		return nil, fmt.Errorf("marketdata: sec: invalid CIK %q", cik)
	}
	var sub edgarSubmissions
	if err := getJSON(ctx, e.HTTP, "sec", e.DataURL+"/submissions/CIK"+c+".json", e.headers(), &sub); err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, f := range forms {
		want[f] = true
	}
	count := map[string]int{}
	cikNum := strings.TrimLeft(c, "0")
	r := sub.Filings.Recent
	var out []Filing
	for i, form := range r.Form {
		if !want[form] || count[form] >= perForm {
			continue
		}
		primary := at2(r.Primary, i)
		low := strings.ToLower(primary)
		if !strings.HasSuffix(low, ".htm") && !strings.HasSuffix(low, ".html") && !strings.HasSuffix(low, ".txt") {
			continue
		}
		if strings.Contains(low, "/") { // XSL renderings of XML forms
			continue
		}
		acc := at2(r.Accession, i)
		dir := e.WWWURL + "/Archives/edgar/data/" + cikNum + "/" + strings.ReplaceAll(acc, "-", "")
		items := at2(r.Items, i)
		title := form + " filed " + at2(r.FilingDate, i)
		if items != "" {
			title += " (items " + items + ")"
		}
		count[form]++
		out = append(out, Filing{
			Form: form, FilingDate: at2(r.FilingDate, i), ReportDate: at2(r.ReportDate, i), Items: items,
			Accession: acc, Title: title, URL: dir + "/" + primary, Dir: dir,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].FilingDate > out[j].FilingDate })
	return out, nil
}

var exhibit99 = regexp.MustCompile(`(?i)ex-?99[-_.]?(1|\.1)?[^/]*\.html?$`)

// Exhibit99 returns the URL of the press release exhibit (99.1) of a filing,
// or "" when the filing has none. Earnings releases are filed as the 99.1
// exhibit of an 8-K.
func (e *EDGAR) Exhibit99(ctx context.Context, f Filing) (string, error) {
	var idx struct {
		Directory struct {
			Item []struct {
				Name string `json:"name"`
			} `json:"item"`
		} `json:"directory"`
	}
	if err := getJSON(ctx, e.HTTP, "sec", f.Dir+"/index.json", e.headers(), &idx); err != nil {
		return "", err
	}
	for _, it := range idx.Directory.Item {
		if exhibit99.MatchString(it.Name) {
			return f.Dir + "/" + it.Name, nil
		}
	}
	return "", nil
}

// maxFilingBytes bounds how much of a filing document is read.
const maxFilingBytes = 8 << 20

// Document fetches a filing document and returns its text, cut to at most
// maxChars characters. The response must be HTML or plain text: the SEC
// answers a missing or bad User-Agent with an HTML block page on JSON
// endpoints, and unexpected types (PDF, XML, images) are not readable.
func (e *EDGAR) Document(ctx context.Context, url string, maxChars int) (string, error) {
	hdr := map[string]string{"User-Agent": e.UserAgent, "Accept": "text/html,text/plain"}
	resp, err := get(ctx, e.HTTP, "sec", url, hdr)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	isHTML := strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml")
	if !isHTML && !strings.Contains(ct, "text/plain") {
		return "", fmt.Errorf("marketdata: sec: unexpected content type %q from %s", ct, url)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxFilingBytes))
	if err != nil {
		return "", fmt.Errorf("marketdata: sec: reading %s: %w", url, err)
	}
	text := string(b)
	if isHTML {
		text = HTMLToText(b)
	}
	return truncateText(text, maxChars), nil
}

func truncateText(s string, maxChars int) string {
	if maxChars <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxChars {
		return s
	}
	return string(r[:maxChars]) + "\n[truncated]"
}

var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "tr": true, "li": true, "table": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "section": true, "ul": true, "ol": true, "hr": true,
}

var skipTags = map[string]bool{"script": true, "style": true, "head": true, "ix:header": true, "noscript": true}

var spaceRun = regexp.MustCompile(`[ \t\f\v\x{00a0}]+`)

// HTMLToText turns an HTML document into readable plain text: scripts, styles
// and hidden inline XBRL headers are dropped, block elements become line
// breaks and runs of whitespace collapse.
func HTMLToText(b []byte) string {
	z := html.NewTokenizer(bytes.NewReader(b))
	var sb strings.Builder
	skip := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return tidyText(sb.String())
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			if skipTags[string(name)] {
				if z.Token().Type == html.StartTagToken {
					skip++
				}
				continue
			}
			if blockTags[string(name)] {
				sb.WriteByte('\n')
			} else if string(name) == "td" || string(name) == "th" {
				sb.WriteString(" | ")
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			if skipTags[string(name)] {
				if skip > 0 {
					skip--
				}
				continue
			}
			if blockTags[string(name)] {
				sb.WriteByte('\n')
			}
		case html.TextToken:
			if skip == 0 {
				sb.WriteString(string(z.Text()))
			}
		}
	}
}

func tidyText(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		l = strings.TrimSpace(spaceRun.ReplaceAllString(l, " "))
		l = strings.Trim(l, "| ")
		if l == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		blank = false
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// Announcement is one CVM IPE entry (fato relevante, comunicado ao mercado).
type Announcement struct {
	Category string `json:"category"`
	Subject  string `json:"subject"`
	Date     string `json:"date"`
	URL      string `json:"url"`
}

// Announcements returns the company's newest IPE entries of the given
// categories (for example "Fato Relevante", "Comunicado ao Mercado") in the
// IPE file of year, newest first, at most limit. The link points at the CVM
// RAD page of the document; its content is not fetched.
func (c *CVM) Announcements(ctx context.Context, cnpj string, year int, categories []string, limit int) ([]Announcement, error) {
	formatted := FormatCNPJ(cnpj)
	if formatted == "" {
		return nil, fmt.Errorf("marketdata: cvm: invalid CNPJ %q", cnpj)
	}
	url := c.BaseURL + "/DOC/IPE/DADOS/ipe_cia_aberta_" + strconv.Itoa(year) + ".zip"
	body, err := getBytes(ctx, c.HTTP, "cvm", url, nil)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("marketdata: cvm: opening %s: %w", url, err)
	}
	var file *zip.File
	for _, f := range zr.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".csv") {
			file = f
			break
		}
	}
	if file == nil {
		return nil, fmt.Errorf("marketdata: cvm: no CSV in %s", url)
	}
	return parseIPE(file, formatted, categories, limit)
}

func parseIPE(f *zip.File, cnpj string, categories []string, limit int) ([]Announcement, error) {
	r, closer, err := openCSV(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer.Close() }()
	head, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("marketdata: cvm: %s: reading header: %w", f.Name, err)
	}
	idx := headerIndex(head)
	want := map[string]bool{}
	for _, c := range categories {
		want[fold(c)] = true
	}
	var out []Announcement
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("marketdata: cvm: %s: %w", f.Name, err)
		}
		if col(rec, idx, "CNPJ_Companhia") != cnpj {
			continue
		}
		cat := col(rec, idx, "Categoria")
		if len(want) > 0 && !want[fold(cat)] {
			continue
		}
		date := col(rec, idx, "Data_Entrega")
		if date == "" {
			date = col(rec, idx, "Data_Referencia")
		}
		if len(date) > 10 {
			date = date[:10]
		}
		out = append(out, Announcement{
			Category: cat, Subject: strings.TrimSpace(col(rec, idx, "Assunto")), Date: date,
			URL: strings.TrimSpace(col(rec, idx, "Link_Download")),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
