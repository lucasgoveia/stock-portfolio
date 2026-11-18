package engine

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func day(y int, m time.Month, dd int) time.Time { return time.Date(y, m, dd, 0, 0, 0, 0, time.UTC) }

// ---- cash (§7) ----

func TestCashTarget(t *testing.T) {
	p := DefaultParams()
	tests := []struct {
		name      string
		core      []CoreWeight
		share     string
		target    string
		missingID []string
	}{
		{"no holdings", nil, "0", "0.04", nil},
		{"all overvalued", []CoreWeight{{"A", d("0.5"), d("140"), dp("100")}}, "1", "0.20", nil},
		{"price exactly FV x 1.3 is not overvalued", []CoreWeight{{"A", d("0.5"), d("130"), dp("100")}}, "0", "0.04", nil},
		{
			"mixed with missing fair value",
			[]CoreWeight{{"A", d("0.3"), d("140"), dp("100")}, {"B", d("0.1"), d("120"), dp("100")}, {"C", d("0.1"), d("50"), nil}},
			"0.6", "0.136", []string{"C"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := p.CashTarget(tc.core)
			nearD(t, "share", r.OvervaluedShare, d(tc.share))
			nearD(t, "target", r.Target, d(tc.target))
			if len(r.MissingFairValue) != len(tc.missingID) {
				t.Fatalf("missing = %v, want %v", r.MissingFairValue, tc.missingID)
			}
			for i := range tc.missingID {
				if r.MissingFairValue[i] != tc.missingID[i] {
					t.Fatalf("missing = %v, want %v", r.MissingFairValue, tc.missingID)
				}
			}
		})
	}
}

func TestCashBand(t *testing.T) {
	p := DefaultParams()
	tests := []struct {
		actual    string
		bootstrap bool
		want      CashSignal
	}{
		{"0.039", false, CashRaise},
		{"0.04", false, CashOK},
		{"0.20", false, CashOK},
		{"0.2001", false, CashDeploy},
		{"0.01", true, CashOK},
		{"0.5", true, CashOK},
	}
	for _, tc := range tests {
		if got := p.CashBand(d(tc.actual), tc.bootstrap); got != tc.want {
			t.Errorf("CashBand(%s, %v) = %v, want %v", tc.actual, tc.bootstrap, got, tc.want)
		}
	}
}

// ---- buy gate and opportunity (§8.1, §8.2, §10.1) ----

func TestEvaluateGate(t *testing.T) {
	p := DefaultParams()
	now := day(2026, 11, 18)
	base := func() GateInput {
		return GateInput{
			Ratings: Factors{3, 3, 3, 3, 3}, BreakConditions: 1,
			FairValue: dp("100"), FairValueDate: day(2026, 1, 1), Price: d("90"), Now: now,
		}
	}
	tests := []struct {
		name                   string
		mod                    func(*GateInput)
		passed, opp, deep, cat bool
		check                  func(*testing.T, GateResult)
	}{
		{name: "passes, no opportunity", mod: func(*GateInput) {}, passed: true,
			check: func(t *testing.T, r GateResult) { nearD(t, "discount", r.Discount, d("0.1")) }},
		{name: "deep discount at exactly FV x 0.8", mod: func(g *GateInput) { g.Price = d("80") }, passed: true, opp: true, deep: true,
			check: func(t *testing.T, r GateResult) { nearD(t, "upside", r.Upside, d("0.25")) }},
		{name: "catalyst-only opportunity", mod: func(g *GateInput) { g.Price = d("95"); g.ActiveCatalyst = true }, passed: true, opp: true, cat: true},
		{name: "catalyst does not bypass gate", mod: func(g *GateInput) { g.Price = d("101"); g.ActiveCatalyst = true },
			check: func(t *testing.T, r GateResult) {
				if r.PriceOK {
					t.Error("price above FV must fail")
				}
			}},
		{name: "low score", mod: func(g *GateInput) { g.Ratings = Factors{2, 2, 2, 2, 2} },
			check: func(t *testing.T, r GateResult) {
				if r.ScoreOK {
					t.Error("score 2 must fail")
				}
			}},
		{name: "score exactly 2.5 passes", mod: func(g *GateInput) { g.Ratings = Factors{3, 3, 2, 2, 3} }, passed: true},
		{name: "no break conditions", mod: func(g *GateInput) { g.BreakConditions = 0 },
			check: func(t *testing.T, r GateResult) {
				if r.ThesisOK {
					t.Error("thesis must fail")
				}
			}},
		{name: "fair value exactly 12 months old is fresh", mod: func(g *GateInput) { g.FairValueDate = day(2025, 11, 18) }, passed: true},
		{name: "fair value older than 12 months is stale", mod: func(g *GateInput) { g.FairValueDate = day(2025, 11, 17) },
			check: func(t *testing.T, r GateResult) {
				if !r.FairValueSet || r.FairValueFresh {
					t.Errorf("set=%v fresh=%v", r.FairValueSet, r.FairValueFresh)
				}
			}},
		{name: "missing fair value", mod: func(g *GateInput) { g.FairValue = nil },
			check: func(t *testing.T, r GateResult) {
				if r.FairValueSet || r.PriceOK {
					t.Error("no fair value must fail set and price checks")
				}
			}},
		{name: "tactical ignores unset horizon", mod: func(g *GateInput) { g.Tactical = true; g.Ratings.Horizon = 0 }, passed: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := base()
			tc.mod(&in)
			r, err := p.EvaluateGate(in)
			if err != nil {
				t.Fatal(err)
			}
			if r.Passed != tc.passed || r.Opportunity != tc.opp || r.DeepDiscount != tc.deep || r.CatalystOnly != tc.cat {
				t.Errorf("passed=%v opp=%v deep=%v cat=%v, want %v %v %v %v",
					r.Passed, r.Opportunity, r.DeepDiscount, r.CatalystOnly, tc.passed, tc.opp, tc.deep, tc.cat)
			}
			if tc.check != nil {
				tc.check(t, r)
			}
		})
	}
	if _, err := p.EvaluateGate(GateInput{Ratings: Factors{0, 3, 3, 3, 3}}); err == nil {
		t.Error("invalid ratings must error")
	}
}

// ---- contribution planner (§8.3, §8.4) ----

func orderSum(pl Plan) decimal.Decimal {
	s := zero
	for _, o := range pl.Orders {
		if o.Kind == OrderRemit {
			continue
		}
		if o.Kind == OrderBuy && o.Market == MarketUS && pl.PendingUSAfter.IsZero() {
			continue // released US buys are covered by the remit line
		}
		s = s.Add(o.Amount)
	}
	return s
}

func assertPlanBounded(t *testing.T, in PlanInput, pl Plan) {
	t.Helper()
	if pl.NewMoney.GreaterThan(in.Amount) {
		t.Fatalf("NewMoney %s > Amount %s", pl.NewMoney, in.Amount)
	}
	nearD(t, "new+unallocated", pl.NewMoney.Add(pl.Unallocated), in.Amount)
}

func TestPlanBootstrapFreshContribution(t *testing.T) {
	p := DefaultParams()
	in := PlanInput{
		Amount: d("1000"), PortfolioValue: zero, CashValue: zero, CashTarget: d("0.04"), Bootstrap: true,
		Assets: []PlanAsset{
			{ID: "Y", Market: MarketB3, Price: d("20"), Target: d("0.36"), Upper: d("0.36"), Eligible: true},
			{ID: "X", Market: MarketB3, Price: d("10"), Target: d("0.6"), Upper: d("0.6"), Eligible: true},
		},
	}
	pl := p.PlanContribution(in)
	assertPlanBounded(t, in, pl)
	want := []struct {
		kind  OrderKind
		id    string
		qty   string
		amt   string
		stage int
	}{
		{OrderBuy, "X", "60", "600", 3},
		{OrderBuy, "Y", "18", "360", 3},
		{OrderCash, CashAssetID, "0", "40", 0},
	}
	if len(pl.Orders) != len(want) {
		t.Fatalf("orders = %+v", pl.Orders)
	}
	for i, w := range want {
		o := pl.Orders[i]
		if o.Kind != w.kind || o.AssetID != w.id || o.Seq != i+1 || o.RampStageAfter != w.stage {
			t.Errorf("order %d = %+v, want %+v", i, o, w)
		}
		eqD(t, "amount", o.Amount, d(w.amt))
		if w.kind == OrderBuy {
			eqD(t, "qty", o.Quantity, d(w.qty))
		}
	}
	eqD(t, "unallocated", pl.Unallocated, zero)
}

func TestPlanMinOrderRollover(t *testing.T) {
	p := DefaultParams()
	in := PlanInput{
		Amount: d("1000"), CashTarget: d("0.04"), Bootstrap: true,
		Assets: []PlanAsset{
			{ID: "X", Market: MarketB3, Price: d("10"), Target: d("0.6"), Upper: d("0.6"), Eligible: true},
			{ID: "Y", Market: MarketB3, Price: d("20"), Target: d("0.2"), Upper: d("0.2"), Eligible: true},
		},
	}
	pl := p.PlanContribution(in)
	assertPlanBounded(t, in, pl)
	if len(pl.Skipped) != 1 || pl.Skipped[0] != (Skip{"Y", "below_min_order"}) {
		t.Fatalf("skipped = %+v", pl.Skipped)
	}
	// X 600 + cash 40; Y's 200 is below R$300 and rolls on, nothing else wants it.
	eqD(t, "placed", orderSum(pl), d("640"))
	eqD(t, "unallocated", pl.Unallocated, d("360"))
}

func TestPlanRampFirstTranche(t *testing.T) {
	p := DefaultParams()
	in := PlanInput{
		Amount: d("5000"), PortfolioValue: d("100000"), CashValue: d("4200"), CashTarget: d("0.04"),
		Assets: []PlanAsset{{ID: "N", Market: MarketB3, Price: d("10"), Target: d("0.06"), Upper: d("0.075"), Eligible: true}},
	}
	pl := p.PlanContribution(in)
	assertPlanBounded(t, in, pl)
	// V' = 105000; full target 6300; first tranche = 2100.
	if len(pl.Orders) != 1 || pl.Orders[0].RampStageAfter != 1 {
		t.Fatalf("orders = %+v", pl.Orders)
	}
	eqD(t, "qty", pl.Orders[0].Quantity, d("210"))
	eqD(t, "unallocated", pl.Unallocated, d("2900"))
}

func TestPlanOpportunitiesFirst(t *testing.T) {
	p := DefaultParams()
	est := func(id string, disc string, catalyst bool) PlanAsset {
		return PlanAsset{ID: id, Market: MarketB3, Price: d("10"), ActualValue: d("5000"), Target: d("0.05"), Upper: d("0.0625"),
			Eligible: true, Opportunity: true, Discount: d(disc), CatalystOnly: catalyst, RampStage: 3}
	}
	in := PlanInput{
		Amount: d("3000"), PortfolioValue: d("100000"), CashValue: d("4120"), CashTarget: d("0.04"),
		Assets: []PlanAsset{est("C", "0", true), est("O2", "0.25", false), est("O1", "0.3", false)},
	}
	pl := p.PlanContribution(in)
	assertPlanBounded(t, in, pl)
	// V' = 103000, upper = 6437.5, room = 1437.5 -> 143 shares each; C gets the
	// remaining 140, below the minimum order.
	if len(pl.Orders) != 2 || pl.Orders[0].AssetID != "O1" || pl.Orders[1].AssetID != "O2" {
		t.Fatalf("orders = %+v", pl.Orders)
	}
	for _, o := range pl.Orders {
		if o.Step != "opportunity" {
			t.Errorf("%s step = %s", o.AssetID, o.Step)
		}
		eqD(t, o.AssetID+" qty", o.Quantity, d("143"))
	}
	if len(pl.Skipped) == 0 || pl.Skipped[0].AssetID != "C" {
		t.Errorf("skipped = %+v", pl.Skipped)
	}
}

func TestPlanUSBucket(t *testing.T) {
	p := DefaultParams()
	us := []PlanAsset{{ID: "U", Market: MarketUS, Price: d("100"), Target: d("0.5"), Upper: d("0.5"), Eligible: true}}

	hold := p.PlanContribution(PlanInput{Amount: d("1000"), Bootstrap: true, Assets: us})
	if len(hold.Orders) != 1 || hold.Orders[0].Kind != OrderHold {
		t.Fatalf("orders = %+v", hold.Orders)
	}
	eqD(t, "held", hold.Orders[0].Amount, d("500"))
	eqD(t, "pending after", hold.PendingUSAfter, d("500"))

	rel := p.PlanContribution(PlanInput{Amount: d("1000"), PendingUS: d("1600"), Bootstrap: true, Assets: us})
	if len(rel.Orders) != 2 || rel.Orders[0].Kind != OrderRemit || rel.Orders[1].Kind != OrderBuy {
		t.Fatalf("orders = %+v", rel.Orders)
	}
	eqD(t, "remit", rel.Orders[0].Amount, d("2100"))
	eqD(t, "buy qty", rel.Orders[1].Quantity, d("21"))
	eqD(t, "pending after", rel.PendingUSAfter, zero)
}

func TestPlanSkipsTacticalAndIneligible(t *testing.T) {
	p := DefaultParams()
	in := PlanInput{
		Amount: d("1000"), Bootstrap: true, CashTarget: d("0.04"),
		Assets: []PlanAsset{
			{ID: "T", Market: MarketB3, Price: d("10"), Target: d("0.5"), Upper: d("0.5"), Eligible: true, Tactical: true},
			{ID: "I", Market: MarketB3, Price: d("10"), Target: d("0.5"), Upper: d("0.5"), Eligible: false},
		},
	}
	pl := p.PlanContribution(in)
	assertPlanBounded(t, in, pl)
	for _, o := range pl.Orders {
		if o.Kind == OrderBuy {
			t.Fatalf("unexpected buy %+v", o)
		}
	}
	eqD(t, "cash", orderSum(pl), d("40"))
}

func TestPlanDeterministic(t *testing.T) {
	p := DefaultParams()
	mk := func(ids ...string) PlanInput {
		in := PlanInput{Amount: d("2000"), Bootstrap: true, CashTarget: d("0.04")}
		for _, id := range ids {
			in.Assets = append(in.Assets, PlanAsset{ID: id, Market: MarketB3, Price: d("10"), Target: d("0.3"), Upper: d("0.3"), Eligible: true})
		}
		return in
	}
	a := p.PlanContribution(mk("A", "B", "C"))
	b := p.PlanContribution(mk("C", "A", "B"))
	if len(a.Orders) != len(b.Orders) {
		t.Fatal("different order counts")
	}
	for i := range a.Orders {
		if a.Orders[i].AssetID != b.Orders[i].AssetID || !a.Orders[i].Amount.Equal(b.Orders[i].Amount) {
			t.Fatalf("not deterministic: %+v vs %+v", a.Orders, b.Orders)
		}
	}
}

// ---- trims (§9, §10.2) ----

var coreTarget = Target{Weight: d("0.05"), Lower: d("0.0375"), Upper: d("0.0625")}

func holding(id string, qty string, fv *decimal.Decimal) TrimHolding {
	return TrimHolding{ID: id, Market: MarketB3, Score: d("3"), Quantity: d(qty), Price: d("10"), Target: coreTarget, FairValue: fv}
}

func findSug(ss []TrimSuggestion, id string) *TrimSuggestion {
	for i := range ss {
		if ss[i].AssetID == id {
			return &ss[i]
		}
	}
	return nil
}

func TestPlanTrimTriggers(t *testing.T) {
	p := DefaultParams()
	now := day(2026, 11, 18)
	recent := now.AddDate(0, 0, -10)
	tac := holding("T", "1500", nil)
	tac.Tactical = true
	tacSmall := holding("TS", "1000", nil)
	tacSmall.Tactical = true
	broken := holding("D", "500", nil)
	broken.ThesisBroken = true
	broken.LastSell = &recent
	cooling := holding("K", "800", nil)
	cooling.LastSell = &recent

	in := TrimInput{
		PortfolioValue: d("100000"), CashWeight: d("0.1"), CashTarget: d("0.06"), Now: now,
		Holdings: []TrimHolding{
			holding("A", "800", nil),      // 8% > upper: 25% of 3000
			holding("B", "500", dp("7")),  // overvalued at target: 25% of 1250
			holding("C", "800", dp("7")),  // both; overvalued basis is larger
			holding("OK", "500", dp("9")), // inside band, not overvalued (10 <= 11.7)
			broken, cooling, tac, tacSmall,
		},
	}
	pl := p.PlanTrims(in)
	tests := []struct {
		id      string
		reason  TrimReason
		qty     string
		reasons int
		full    bool
	}{
		{"A", ReasonOverweight, "75", 1, false},
		{"B", ReasonOvervalued, "31", 1, false},
		{"C", ReasonOvervalued, "106", 2, false},
		{"D", ReasonThesisBroken, "500", 1, true},
		{"T", ReasonOverweight, "75", 1, false}, // tactical 15% > 12% cap: 25% of 3000
	}
	for _, tc := range tests {
		s := findSug(pl.Suggestions, tc.id)
		if s == nil {
			t.Errorf("%s: no suggestion", tc.id)
			continue
		}
		if s.Reason != tc.reason || len(s.Reasons) != tc.reasons || s.FullExit != tc.full {
			t.Errorf("%s = %+v", tc.id, *s)
		}
		eqD(t, tc.id+" qty", s.Quantity, d(tc.qty))
	}
	for _, id := range []string{"OK", "K", "TS"} {
		if findSug(pl.Suggestions, id) != nil {
			t.Errorf("%s must not be suggested", id)
		}
	}
	if len(pl.Skipped) != 1 || pl.Skipped[0].AssetID != "K" || !pl.Skipped[0].Until.Equal(recent.AddDate(0, 0, 30)) {
		t.Errorf("skipped = %+v", pl.Skipped)
	}

	in.Bootstrap = true
	bp := p.PlanTrims(in)
	if len(bp.Suggestions) != 1 || bp.Suggestions[0].AssetID != "D" {
		t.Errorf("bootstrap suggestions = %+v", bp.Suggestions)
	}
}

func TestPlanTrimRaiseCash(t *testing.T) {
	p := DefaultParams()
	low := holding("L", "500", nil)
	low.Score = d("2")
	high := holding("H", "500", nil)
	high.Score = d("4")
	hs := []TrimHolding{holding("OW", "800", nil), holding("OV", "500", dp("7")), high, low}
	tests := []struct {
		name      string
		target    string
		want      []string
		wantQty   []string
		shortfall string
	}{
		{"overweight covers it", "0.06", []string{"OW"}, []string{"300"}, "0"},
		{"tiers in order", "0.08", []string{"OW", "OV", "L"}, []string{"300", "125", "75"}, "0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pl := p.PlanTrims(TrimInput{Holdings: hs, PortfolioValue: d("100000"), CashWeight: d("0.03"), CashTarget: d(tc.target), Now: day(2026, 11, 18)})
			if len(pl.RaiseCash) != len(tc.want) {
				t.Fatalf("raise cash = %+v", pl.RaiseCash)
			}
			for i, id := range tc.want {
				if pl.RaiseCash[i].AssetID != id {
					t.Errorf("raise[%d] = %s, want %s", i, pl.RaiseCash[i].AssetID, id)
				}
				eqD(t, id+" qty", pl.RaiseCash[i].Quantity, d(tc.wantQty[i]))
			}
			eqD(t, "shortfall", pl.RaiseCashShortfall, d(tc.shortfall))
		})
	}
	// Cash inside the band: no raise-cash plan.
	if pl := p.PlanTrims(TrimInput{Holdings: hs, PortfolioValue: d("100000"), CashWeight: d("0.05"), CashTarget: d("0.08")}); len(pl.RaiseCash) != 0 {
		t.Errorf("unexpected raise cash %+v", pl.RaiseCash)
	}
}

func TestPlanTrimBetterOpportunity(t *testing.T) {
	p := DefaultParams()
	w := holding("W", "500", dp("11")) // score 3, upside 10%
	tests := []struct {
		name  string
		cand  BetterCandidate
		swap  bool
		full  bool
		qty   string
		pairs string
	}{
		{"beats on both at the threshold", BetterCandidate{"K", d("3.5"), d("0.30"), d("1000")}, true, false, "125", "K"},
		{"upside too small", BetterCandidate{"K", d("4"), d("0.29"), d("1000")}, false, false, "", ""},
		{"score too small", BetterCandidate{"K", d("3.4"), d("0.5"), d("1000")}, false, false, "", ""},
		{"first tranche bigger than a trim step: full exit", BetterCandidate{"K", d("4"), d("0.5"), d("2000")}, true, true, "500", "K"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pl := p.PlanTrims(TrimInput{Holdings: []TrimHolding{w}, PortfolioValue: d("100000"), CashWeight: d("0.1"), Candidates: []BetterCandidate{tc.cand}, Now: day(2026, 11, 18)})
			if (len(pl.Swaps) == 1) != tc.swap {
				t.Fatalf("swaps = %+v", pl.Swaps)
			}
			if !tc.swap {
				return
			}
			s := pl.Swaps[0]
			if s.FullExit != tc.full || s.PairedCandidate != tc.pairs || s.Reason != ReasonBetterOpportunity {
				t.Errorf("swap = %+v", s)
			}
			eqD(t, "qty", s.Quantity, d(tc.qty))
		})
	}
}

func TestParamsScore(t *testing.T) {
	p := DefaultParams()
	s, err := p.Score(Factors{4, 3, 5, 2, 1}) // 18.5/6
	if err != nil {
		t.Fatal(err)
	}
	eqD(t, "score", s, d("3.0833"))
	if _, err := p.Score(Factors{3, 3, 3, 3, 0}); err == nil {
		t.Fatal("core score must reject unset horizon")
	}
	ts, err := p.TacticalScore(Factors{4, 3, 5, 2, 0}) // 17.5/5
	if err != nil {
		t.Fatal(err)
	}
	eqD(t, "tactical", ts, d("3.5"))
	for in, want := range map[string]string{"1": "0.02", "3": "0.06", "5": "0.10", "2.5": "0.05"} {
		eqD(t, "raw("+in+")", p.RawWeight(d(in)), d(want))
	}
}
