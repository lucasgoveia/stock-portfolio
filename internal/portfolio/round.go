package portfolio

import (
	"reflect"

	"github.com/shopspring/decimal"
)

// Snapshot precision at the service boundary: BRL amounts to centavos,
// everything else (weights, ratios, prices, quantities) to 8 places. The
// engine keeps full precision; rounding happens once, on the way out.
const (
	brlPlaces   = 2
	otherPlaces = 8
)

var decimalType = reflect.TypeOf(decimal.Decimal{})

// roundSnapshot rounds the BRL amounts of s to 2 places, then caps every other
// decimal in it (nested structs, slices, pointers) at 8 places.
func roundSnapshot(s *Snapshot) {
	r2 := func(d *decimal.Decimal) { *d = d.Round(brlPlaces) }
	for _, d := range []*decimal.Decimal{&s.TotalValue, &s.EquityValue, &s.CashValue, &s.CoreBudget, &s.Undeployed,
		&s.PendingUS, &s.UndeployedDividends, &s.Cash.TotalBRL, &s.Cash.BRL, &s.Cash.USD} {
		r2(d)
	}
	for i := range s.Cash.Instruments {
		r2(&s.Cash.Instruments[i].ValueBRL)
	}
	for _, list := range [][]Position{s.Holdings, s.Watchlist} {
		for i := range list {
			p := &list[i]
			r2(&p.CostBRL)
			r2(&p.DividendsTTM)
			r2(&p.ValueBRL)
			r2(&p.GroupValueBRL)
			if p.Tactical != nil {
				r2(&p.Tactical.SuggestedValueBRL)
			}
		}
	}
	capDecimals(reflect.ValueOf(s).Elem(), otherPlaces)
}

// capAll caps every decimal reachable from the pointer ptr at 8 places.
func capAll(ptr any) { capDecimals(reflect.ValueOf(ptr).Elem(), otherPlaces) }

// capDecimals rounds every decimal reachable from v to at most places.
func capDecimals(v reflect.Value, places int32) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			capDecimals(v.Elem(), places)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			capDecimals(v.Index(i), places)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			e := v.MapIndex(k)
			c := reflect.New(e.Type()).Elem()
			c.Set(e)
			capDecimals(c, places)
			v.SetMapIndex(k, c)
		}
	case reflect.Struct:
		if v.Type() == decimalType {
			if v.CanSet() {
				v.Set(reflect.ValueOf(v.Interface().(decimal.Decimal).Round(places)))
			}
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				capDecimals(v.Field(i), places)
			}
		}
	}
}
