package tax

import (
	"bytes"
	"encoding/csv"
	"strconv"
)

// CSV renders the report as one CSV file with a section column, for the
// annual DIRPF. Decimals use a dot; amounts are BRL unless the column says USD.
func (r AnnualReport) CSV() ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	rows := [][]string{{"section", "ticker", "market", "date", "kind", "quantity", "amount", "amount_brl", "withheld_brl", "note"}}
	for _, p := range r.Positions {
		rows = append(rows, []string{"position_31_12", p.Ticker, p.Market, strconv.Itoa(r.Year) + "-12-31", p.Currency,
			p.Quantity.String(), p.Cost.String(), p.CostBRL.String(), "", "cost basis (" + p.Currency + ")"})
	}
	for _, s := range r.Sales {
		rows = append(rows, []string{"us_sale", s.Ticker, "US", s.Date, "sale", s.Quantity.String(), s.GainUSD.String(),
			s.GainBRL.String(), "", "gain; proceeds USD " + s.Proceeds.String()})
	}
	for _, i := range r.Income {
		rows = append(rows, []string{"income", i.Ticker, i.Market, i.PayDate, i.Kind, "", i.NetBRL.String(), i.GrossBRL.String(),
			i.WithheldBRL.String(), "amount = net BRL; amount_brl = gross"})
	}
	f := r.Foreign
	for _, x := range []struct{ name, v string }{
		{"us_gains", f.Gains.String()}, {"us_dividends", f.Dividends.String()}, {"us_withheld", f.Withheld.String()},
		{"us_loss_in", f.LossIn.String()}, {"us_taxable", f.Taxable.String()}, {"us_tax_15pct", f.Tax.String()},
		{"us_loss_out", f.LossOut.String()}, {"b3_dividends_exempt", r.B3Dividends.String()},
		{"b3_jcp_gross", r.B3JCPGross.String()}, {"b3_jcp_withheld", r.B3JCPWithheld.String()},
	} {
		rows = append(rows, []string{"summary", "", "", strconv.Itoa(r.Year), x.name, "", "", x.v, "", ""})
	}
	if err := w.WriteAll(rows); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
