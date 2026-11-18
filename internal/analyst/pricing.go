package analyst

import (
	"fmt"
	"sort"

	"github.com/shopspring/decimal"
)

// price is USD per million tokens.
type price struct{ In, Out decimal.Decimal }

// prices are Anthropic first-party API list prices per million tokens (from
// the claude-api reference, cached 2026-11-03). A model missing here cannot
// run: the cost cap needs a price, so an unknown model is refused rather
// than guessed.
var prices = map[string]price{
	"claude-fable-5-2":  {dec("8"), dec("40")},
	"claude-fable-5-1":  {dec("10"), dec("50")},
	"claude-opus-5-5":   {dec("4"), dec("20")},
	"claude-opus-5":     {dec("5"), dec("25")},
	"claude-opus-4-8":   {dec("5"), dec("25")},
	"claude-sonnet-5-5": {dec("2"), dec("10")},
	"claude-sonnet-5":   {dec("2"), dec("10")},
	"claude-sonnet-4-6": {dec("3"), dec("15")},
	"claude-haiku-4-5":  {dec("1"), dec("5")},
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// KnownModels lists the models with a configured price.
func KnownModels() []string {
	out := make([]string, 0, len(prices))
	for m := range prices {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// Cost returns the USD cost of a call from its reported token usage, to six
// decimals. Cache tokens are counted at the input price (the analyst does not
// use prompt caching, so this never under-reports).
func Cost(model string, inputTokens, outputTokens int64) (decimal.Decimal, error) {
	p, ok := prices[model]
	if !ok {
		return decimal.Zero, fmt.Errorf("no price configured for model %q (known: %v)", model, KnownModels())
	}
	million := decimal.NewFromInt(1_000_000)
	in := decimal.NewFromInt(inputTokens).Mul(p.In)
	out := decimal.NewFromInt(outputTokens).Mul(p.Out)
	return in.Add(out).Div(million).Round(6), nil
}
