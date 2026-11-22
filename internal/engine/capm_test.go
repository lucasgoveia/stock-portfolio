package engine

import "testing"

func TestCostOfEquity(t *testing.T) {
	tests := []struct {
		name               string
		rf, beta, erp, min string
		rate, adj          string
		floored            bool
	}{
		{"defensive hits the floor", "0.04", "0.10", "0.05", "0.03", "0.0700", "0.3970", true},
		{"high beta", "0.04", "2.18", "0.05", "0.03", "0.1295", "1.7906", false},
		{"market beta", "0.04", "1", "0.05", "0.03", "0.0900", "1.0000", false},
		{"B3 rate", "0.14", "0.8", "0.05", "0.03", "0.1833", "0.8660", false},
		{"negative beta floors", "0.04", "-0.5", "0.05", "0.03", "0.0700", "-0.0050", true},
		{"exactly at the floor is not floored", "0.04", "0.6", "0.05", "0.0366", "0.0766", "0.7320", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CostOfEquity(d(tt.rf), d(tt.beta), d(tt.erp), d(tt.min))
			if !got.Rate.Equal(d(tt.rate)) || !got.AdjBeta.Equal(d(tt.adj)) || got.Floored != tt.floored {
				t.Fatalf("got rate %s adj %s floored %v, want %s %s %v", got.Rate, got.AdjBeta, got.Floored, tt.rate, tt.adj, tt.floored)
			}
			if !got.RiskFree.Equal(d(tt.rf)) || !got.RawBeta.Equal(d(tt.beta)) || !got.ERP.Equal(d(tt.erp)) {
				t.Fatalf("inputs not echoed: %+v", got)
			}
		})
	}
}
