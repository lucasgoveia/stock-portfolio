package engine

import "github.com/shopspring/decimal"

// ForeignYear is the annual tax on foreign financial applications (spec
// §11.3, Lei 14.754/2023): realized gains and dividends in BRL are taxed
// together at 15%; a net loss offsets later years. Foreign withholding is
// reported apart because whether it is creditable is unconfirmed.
type ForeignYear struct {
	Year             int             `json:"year"`
	Gains            decimal.Decimal `json:"gains"`                  // realized, BRL at each trade's FX
	Dividends        decimal.Decimal `json:"dividends"`              // gross, BRL at the payment FX
	Withheld         decimal.Decimal `json:"withheld"`               // foreign tax withheld, BRL
	Base             decimal.Decimal `json:"base"`                   // gains + dividends
	LossIn           decimal.Decimal `json:"loss_in"`                // carried from earlier years
	LossOut          decimal.Decimal `json:"loss_out"`               // carried to next year
	Taxable          decimal.Decimal `json:"taxable"`                // after the carried loss
	Tax              decimal.Decimal `json:"tax"`                    // Rate x Taxable
	Rate             decimal.Decimal `json:"rate"`                   // 15%
	NetOfWithholding decimal.Decimal `json:"tax_net_of_withholding"` // only if withholding were creditable
}

// ForeignTax computes one year given the loss carried in.
func ForeignTax(year int, rate, gains, dividends, withheld, lossIn decimal.Decimal) ForeignYear {
	y := ForeignYear{Year: year, Gains: gains, Dividends: dividends, Withheld: withheld, Rate: rate, LossIn: lossIn}
	y.Base = gains.Add(dividends)
	y.Taxable, y.LossOut = offset(y.Base, lossIn)
	y.Tax = y.Taxable.Mul(rate).Round(2)
	y.NetOfWithholding = maxDec(zero, y.Tax.Sub(withheld)).Round(2)
	return y
}
