package marketdata

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ErrPDFNoText reports a PDF without an extractable text layer, typically a
// scanned document made of images.
var ErrPDFNoText = errors.New("marketdata: the PDF has no extractable text (probably a scanned image); open the link in a browser")

// PDFToText extracts the plain text of a PDF, page by page, in pure Go. Text
// runs are regrouped into lines by position so that kerned or ligature-split
// words read as one. It returns ErrPDFNoText when the document has no text
// layer. A malformed document yields an error rather than a panic.
func PDFToText(b []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("marketdata: reading PDF: %v", r)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return "", fmt.Errorf("marketdata: reading PDF: %w", err)
	}
	var sb strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		sb.WriteString(pageText(p.Content().Text))
		sb.WriteString("\n\n")
	}
	text = tidyText(strings.ToValidUTF8(sb.String(), ""))
	if text == "" {
		return "", ErrPDFNoText
	}
	return text, nil
}

// pageText joins the text runs of one page into lines, top to bottom and left
// to right, adding a space where the horizontal gap between runs is wide.
func pageText(runs []pdf.Text) string {
	var items []pdf.Text
	for _, t := range runs {
		if strings.TrimRight(t.S, "\r\n") != "" {
			items = append(items, t)
		}
	}
	// Top to bottom; a vertical jump larger than half the font size starts a
	// new line, whose runs are then ordered left to right.
	sort.SliceStable(items, func(i, j int) bool { return items[i].Y > items[j].Y })
	var sb strings.Builder
	for start := 0; start < len(items); {
		end := start + 1
		for end < len(items) && items[end-1].Y-items[end].Y <= 0.5*lineTol(items[end], items[end-1]) {
			end++
		}
		line := items[start:end]
		sort.SliceStable(line, func(i, j int) bool { return line[i].X < line[j].X })
		for i, t := range line {
			if i > 0 {
				prev := line[i-1]
				if t.X-(prev.X+prev.W) > 0.15*math.Max(t.FontSize, 1) &&
					!strings.HasSuffix(prev.S, " ") && !strings.HasPrefix(t.S, " ") {
					sb.WriteByte(' ')
				}
			}
			sb.WriteString(t.S)
		}
		sb.WriteByte('\n')
		start = end
	}
	return sb.String()
}

func lineTol(a, b pdf.Text) float64 {
	return math.Max(math.Max(a.FontSize, b.FontSize), 1)
}
