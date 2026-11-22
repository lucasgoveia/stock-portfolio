package engine

import "testing"

func freshAsset(id string, market Market, price, target, score string) PlanAsset {
	return PlanAsset{ID: id, Market: market, Price: d(price), Target: d(target), Upper: d(target), Eligible: true, Score: d(score)}
}

func buys(pl Plan) []Order {
	var out []Order
	for _, o := range pl.Orders {
		if o.Kind == OrderBuy {
			out = append(out, o)
		}
	}
	return out
}

func skipOf(pl Plan, id string) *Skip {
	for i := range pl.Skipped {
		if pl.Skipped[i].AssetID == id {
			return &pl.Skipped[i]
		}
	}
	return nil
}

func hasNote(pl Plan, code string) bool {
	for _, n := range pl.Notes {
		if n.Code == code {
			return true
		}
	}
	return false
}

// An empty portfolio with many eligible names opens at most MaxNewPositions
// orders, each above the minimum, instead of splitting R$1,000 into slivers.
func TestPlanEmptyPortfolioBoundsNewPositions(t *testing.T) {
	p := DefaultParams()
	in := PlanInput{Amount: d("1000"), CashTarget: d("0.04"), Bootstrap: true}
	for _, id := range []string{"A", "B", "C", "D", "E", "F", "G", "H"} {
		in.Assets = append(in.Assets, freshAsset(id, MarketB3, "10", "0.12", "3"))
	}
	in.Assets[3].Score = d("4.5") // D is the best score
	in.Assets[5].Score = d("4")   // F second
	pl := p.PlanContribution(in)
	assertPlanBounded(t, in, pl)
	bs := buys(pl)
	if len(bs) != 2 || bs[0].AssetID != "D" || bs[1].AssetID != "F" {
		t.Fatalf("buys = %+v", bs)
	}
	for _, o := range bs {
		if o.Amount.LessThan(p.MinOrderB3) {
			t.Errorf("%s order %s below the minimum", o.AssetID, o.Amount)
		}
	}
	// 2 x 480 + 40 cash: everything is placed.
	eqD(t, "unallocated", pl.Unallocated, zero)
	for _, id := range []string{"A", "B", "C", "E", "G", "H"} {
		if s := skipOf(pl, id); s == nil || s.Reason != ReasonMaxNewPositions {
			t.Errorf("%s skip = %+v", id, s)
		}
	}
}

func TestPlanNewPositionsOpportunitiesFirst(t *testing.T) {
	p := DefaultParams()
	op := freshAsset("OPP", MarketB3, "10", "0.4", "2.6")
	op.Opportunity, op.Discount = true, d("0.3")
	in := PlanInput{Amount: d("1000"), CashTarget: d("0.04"), Bootstrap: true, Assets: []PlanAsset{
		freshAsset("HI", MarketB3, "10", "0.3", "5"),
		freshAsset("MID", MarketB3, "10", "0.26", "4"),
		op,
	}}
	pl := p.PlanContribution(in)
	bs := buys(pl)
	if len(bs) != 2 || bs[0].AssetID != "OPP" || bs[0].Step != "opportunity" || bs[1].AssetID != "HI" {
		t.Fatalf("buys = %+v", bs)
	}
	if s := skipOf(pl, "MID"); s == nil || s.Reason != ReasonMaxNewPositions {
		t.Errorf("MID skip = %+v", s)
	}
}

// A pick that cannot be sized is replaced by the next candidate.
func TestPlanFailedPickIsReplaced(t *testing.T) {
	p := DefaultParams()
	p.MaxNewPositions = 1
	in := PlanInput{Amount: d("1000"), CashTarget: d("0.04"), Bootstrap: true, Assets: []PlanAsset{
		freshAsset("EXPENSIVE", MarketB3, "5000", "0.32", "5"),
		freshAsset("B", MarketB3, "10", "0.32", "4"),
		freshAsset("C", MarketB3, "10", "0.32", "3"),
	}}
	pl := p.PlanContribution(in)
	assertPlanBounded(t, in, pl)
	bs := buys(pl)
	if len(bs) != 1 || bs[0].AssetID != "B" {
		t.Fatalf("buys = %+v", bs)
	}
	if s := skipOf(pl, "EXPENSIVE"); s == nil || s.Reason != ReasonBelowOneShare {
		t.Errorf("EXPENSIVE skip = %+v", s)
	}
	if s := skipOf(pl, "C"); s == nil || s.Reason != ReasonMaxNewPositions {
		t.Errorf("C skip = %+v", s)
	}
	// B takes everything the targets leave after the cash target.
	eqD(t, "B amount", bs[0].Amount, d("960"))
}

func TestPlanLimitIgnoresHeldPositions(t *testing.T) {
	p := DefaultParams()
	p.MaxNewPositions = 1
	held := freshAsset("HELD", MarketB3, "10", "0.05", "3")
	held.ActualValue, held.RampStage = d("500"), 3
	in := PlanInput{Amount: d("8000"), PortfolioValue: d("20000"), CashValue: d("1000"), CashTarget: d("0.04"), Assets: []PlanAsset{
		held,
		freshAsset("N1", MarketB3, "10", "0.06", "4"),
		freshAsset("N2", MarketB3, "10", "0.06", "3"),
	}}
	pl := p.PlanContribution(in)
	assertPlanBounded(t, in, pl)
	got := map[string]bool{}
	for _, o := range buys(pl) {
		got[o.AssetID] = true
	}
	if !got["HELD"] || !got["N1"] || got["N2"] {
		t.Fatalf("buys = %+v", buys(pl))
	}
	// First ramp tranche of N1: 1/3 of 6% of 28,000.
	for _, o := range buys(pl) {
		if o.AssetID == "N1" {
			eqD(t, "N1 amount", o.Amount, d("560"))
			if o.RampStageAfter != 1 {
				t.Errorf("ramp stage = %d", o.RampStageAfter)
			}
		}
	}
}

func TestPlanNewUSPositionsGoToPendingBucket(t *testing.T) {
	p := DefaultParams()
	in := PlanInput{Amount: d("1000"), CashTarget: d("0.04"), Bootstrap: true, Assets: []PlanAsset{
		freshAsset("U1", MarketUS, "100", "0.32", "4"),
		freshAsset("U2", MarketUS, "100", "0.32", "3"),
		freshAsset("U3", MarketUS, "100", "0.32", "2.6"),
	}}
	pl := p.PlanContribution(in)
	assertPlanBounded(t, in, pl)
	var hold *Order
	for i := range pl.Orders {
		if pl.Orders[i].Kind == OrderHold {
			hold = &pl.Orders[i]
		}
	}
	if hold == nil || len(hold.Allocations) != 2 {
		t.Fatalf("orders = %+v", pl.Orders)
	}
	if hold.Allocations[0].AssetID != "U1" || hold.Allocations[1].AssetID != "U2" {
		t.Errorf("allocations = %+v", hold.Allocations)
	}
	eqD(t, "pending after", pl.PendingUSAfter, d("960"))
	if !hasNote(pl, NotePendingUS) {
		t.Errorf("notes = %+v", pl.Notes)
	}
}

func TestPlanExplainsUnallocated(t *testing.T) {
	p := DefaultParams()
	none := p.PlanContribution(PlanInput{Amount: d("1000"), CashTarget: d("0.04"), Bootstrap: true, Assets: []PlanAsset{
		{ID: "I", Market: MarketB3, Price: d("10"), Target: d("0.5"), Upper: d("0.5"), Eligible: false},
	}})
	if !hasNote(none, NoteNoEligible) {
		t.Errorf("no-eligible notes = %+v", none.Notes)
	}
	eqD(t, "unallocated", none.Unallocated, d("960"))

	at := p.PlanContribution(PlanInput{Amount: d("1000"), PortfolioValue: d("100000"), CashValue: d("4000"), CashTarget: d("0.04"), Assets: []PlanAsset{
		{ID: "E", Market: MarketB3, Price: d("10"), ActualValue: d("6100"), Target: d("0.06"), Upper: d("0.075"), Eligible: true, RampStage: 3, Score: d("3")},
	}})
	if !hasNote(at, NoteTargetsReached) {
		t.Errorf("targets-reached notes = %+v", at.Notes)
	}
}

// Equal gaps are broken by score, then by id.
func TestPlanEqualGapsPreferHigherScore(t *testing.T) {
	p := DefaultParams()
	p.MaxNewPositions = 0
	in := PlanInput{Amount: d("1000"), CashTarget: d("0.04"), Bootstrap: true, Assets: []PlanAsset{
		freshAsset("A", MarketB3, "10", "0.48", "3"),
		freshAsset("B", MarketB3, "10", "0.48", "4"),
	}}
	pl := p.PlanContribution(in)
	if bs := buys(pl); len(bs) == 0 || bs[0].AssetID != "B" {
		t.Fatalf("buys = %+v", bs)
	}
}
