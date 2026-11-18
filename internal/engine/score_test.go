package engine

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestWeightedScore(t *testing.T) {
	custom := Weights{d("2"), d("0"), d("1"), d("1"), d("1")}
	tests := []struct {
		name    string
		f       Factors
		w       Weights
		want    string
		wantErr bool
	}{
		// 3*1 + 4*1 + 5*1.5 + 2*1.5 + 3*1 = 20.5 ; / 6 = 3.41666...
		{"defaults mixed", Factors{3, 4, 5, 2, 3}, DefaultWeights(), "3.4167", false},
		{"all ones", Factors{1, 1, 1, 1, 1}, DefaultWeights(), "1", false},
		{"all fives", Factors{5, 5, 5, 5, 5}, DefaultWeights(), "5", false},
		// moat and predictability weigh more: 5,5,1,1,5 -> (5+5+1.5+1.5+5)/6 = 3
		{"weights favour moat", Factors{5, 5, 1, 1, 5}, DefaultWeights(), "3", false},
		// 2*5 + 0*1 + 1*3 + 1*4 + 1*2 = 19 ; / 5 = 3.8
		{"custom weights incl. zero weight", Factors{5, 1, 3, 4, 2}, custom, "3.8", false},
		{"equal weights is the mean", Factors{1, 2, 3, 4, 5}, Weights{d("1"), d("1"), d("1"), d("1"), d("1")}, "3", false},
		{"rating too high", Factors{6, 1, 1, 1, 1}, DefaultWeights(), "", true},
		{"rating zero", Factors{1, 0, 1, 1, 1}, DefaultWeights(), "", true},
		{"negative weight", Factors{1, 1, 1, 1, 1}, Weights{d("-1"), d("1"), d("1"), d("1"), d("1")}, "", true},
		{"all-zero weights", Factors{1, 1, 1, 1, 1}, Weights{}, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := WeightedScore(tc.f, tc.w)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil || !got.Equal(d(tc.want)) {
				t.Fatalf("got %s (%v), want %s", got, err, tc.want)
			}
		})
	}
}
