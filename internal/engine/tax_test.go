package engine

import "testing"

func sale(date, kind, proceeds, gain string) TaxSale {
	return TaxSale{AssetID: "1", Date: date, Kind: kind, Proceeds: d(proceeds), Gain: d(gain)}
}

func TestTaxMonthsExemption(t *testing.T) {
	tp := DefaultTaxParams()
	for _, c := range []struct {
		name     string
		sales    []TaxSale
		exempt   bool
		taxable  string
		tax      string
		darf     string
		lossOut  string
		exemptGn string
	}{
		{"exactly 20k is exempt", []TaxSale{sale("2026-03-10", "stock", "20000", "3000")}, true, "0", "0", "0", "0", "3000"},
		{"one cent over is taxed", []TaxSale{sale("2026-03-10", "stock", "20000.01", "3000")}, false, "3000", "450", "450", "0", "0"},
		{"split sales add up", []TaxSale{sale("2026-03-02", "stock", "12000", "1000"), sale("2026-03-20", "stock", "9000", "500")}, false, "1500", "225", "225", "0", "0"},
		{"bdr never exempt", []TaxSale{sale("2026-03-10", "bdr", "5000", "1000")}, true, "1000", "150", "150", "0", "0"},
		{"exempt month loss carries", []TaxSale{sale("2026-03-10", "stock", "8000", "-700")}, true, "0", "0", "0", "700", "0"},
		{"exempt gains do not offset bdr", []TaxSale{sale("2026-03-10", "stock", "8000", "900"), sale("2026-03-11", "bdr", "3000", "200")}, true, "200", "30", "30", "0", "900"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ms := tp.TaxMonths(c.sales)
			if len(ms) != 1 {
				t.Fatalf("months = %d", len(ms))
			}
			m := ms[0]
			if m.Exempt != c.exempt || !m.SwingTaxable.Equal(d(c.taxable)) || !m.Tax.Equal(d(c.tax)) ||
				!m.DARF.Equal(d(c.darf)) || !m.SwingLossOut.Equal(d(c.lossOut)) || !m.ExemptGain.Equal(d(c.exemptGn)) {
				t.Fatalf("got %+v", m)
			}
		})
	}
}

func TestTaxMonthsCarryForward(t *testing.T) {
	tp := DefaultTaxParams()
	ms := tp.TaxMonths([]TaxSale{
		// Jan: R$25k sold at a R$4k loss -> loss 4000 carried.
		sale("2026-01-15", "stock", "25000", "-4000"),
		// Feb: taxable R$3k gain eats 3000 of the loss -> 1000 left, no tax.
		sale("2026-02-10", "stock", "30000", "3000"),
		// Mar: taxable R$5k gain - 1000 loss = 4000 x 15% = 600.
		sale("2026-03-05", "stock", "21000", "5000"),
		// Apr: day trade gain 100 with no day-trade loss: 20 tax; swing loss pool untouched.
		{AssetID: "2", Date: "2026-04-01", Kind: "stock", Proceeds: d("1000"), Gain: d("100"), DayTrade: true},
	})
	want := []struct{ month, lossOut, taxable, tax, darf string }{
		{"2026-01", "4000", "0", "0", "0"},
		{"2026-02", "1000", "0", "0", "0"},
		{"2026-03", "0", "4000", "600", "600"},
		{"2026-04", "0", "0", "20", "20"},
	}
	if len(ms) != len(want) {
		t.Fatalf("months = %+v", ms)
	}
	for i, w := range want {
		m := ms[i]
		if m.Month != w.month || !m.SwingLossOut.Equal(d(w.lossOut)) || !m.SwingTaxable.Equal(d(w.taxable)) ||
			!m.Tax.Equal(d(w.tax)) || !m.DARF.Equal(d(w.darf)) {
			t.Errorf("%s: got %+v", w.month, m)
		}
	}
	if !ms[3].DayTaxable.Equal(d("100")) || ms[3].StockSales.Sign() != 0 {
		t.Errorf("day trade month: %+v", ms[3])
	}
}

func TestTaxDayTradeLossesSeparate(t *testing.T) {
	tp := DefaultTaxParams()
	ms := tp.TaxMonths([]TaxSale{
		{Date: "2026-05-02", Kind: "stock", Proceeds: d("5000"), Gain: d("-300"), DayTrade: true},
		sale("2026-06-02", "stock", "30000", "1000"),
		{Date: "2026-06-03", Kind: "stock", Proceeds: d("5000"), Gain: d("500"), DayTrade: true},
	})
	jun := ms[1]
	// Swing 1000 x 15% = 150; day trade 500 - 300 loss = 200 x 20% = 40.
	if !jun.SwingTaxable.Equal(d("1000")) || !jun.DayTaxable.Equal(d("200")) || !jun.Tax.Equal(d("190")) {
		t.Fatalf("june = %+v", jun)
	}
}

func TestTaxDARFMinimumRollsForward(t *testing.T) {
	tp := DefaultTaxParams()
	ms := tp.TaxMonths([]TaxSale{
		sale("2026-07-10", "bdr", "1000", "40"), // 6.00 tax, below R$10
		sale("2026-08-10", "bdr", "1000", "30"), // 4.50 + 6.00 carried = 10.50
	})
	if !ms[0].DARF.IsZero() || !ms[0].Tax.Equal(d("6")) {
		t.Fatalf("july = %+v", ms[0])
	}
	if !ms[1].CarriedIn.Equal(d("6")) || !ms[1].DARF.Equal(d("10.5")) {
		t.Fatalf("august = %+v", ms[1])
	}
}

func TestDARFDueDate(t *testing.T) {
	for month, want := range map[string]string{
		"2026-01": "2026-02-27", // Feb 28 2026 is a Saturday
		"2026-04": "2026-05-29", // May 31 is a Sunday
		"2026-11": "2026-12-31", // Thursday
		"2026-12": "2027-01-29", // Jan 31 2027 is a Sunday
	} {
		if got := DARFDueDate(month); got != want {
			t.Errorf("%s: got %s want %s", month, got, want)
		}
	}
}

func TestTrimTaxImpact(t *testing.T) {
	tp := DefaultTaxParams()
	// 15k sold already this month with 2k gains; trim 100 x 60 (avg 40).
	r := tp.TrimTaxImpact("1", "stock", d("100"), d("60"), d("40"), d("15000"), d("2000"))
	if !r.ExceedsExemption || !r.MonthSalesAfter.Equal(d("21000")) || !r.EstimatedTax.Equal(d("600")) || !r.FitQuantity.Equal(d("83")) {
		t.Fatalf("over limit: %+v", r)
	}
	// Same trim with an empty month fits.
	r = tp.TrimTaxImpact("1", "stock", d("100"), d("60"), d("40"), d("0"), d("0"))
	if r.ExceedsExemption || !r.EstimatedTax.IsZero() || !r.FitQuantity.Equal(d("100")) {
		t.Fatalf("under limit: %+v", r)
	}
	// BDRs always pay.
	r = tp.TrimTaxImpact("1", "bdr", d("10"), d("60"), d("40"), d("0"), d("0"))
	if !r.EstimatedTax.Equal(d("30")) {
		t.Fatalf("bdr: %+v", r)
	}
}
