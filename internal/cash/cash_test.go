package cash

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func dec(s string) money.Decimal { return money.New(d(s)) }

func mv(date, typ, amount string) Movement {
	return Movement{Date: date, Type: typ, Amount: d(amount)}
}

func rate(date, pct string) CDIRate { return CDIRate{Date: date, Pct: d(pct)} }

func TestCDIBalance(t *testing.T) {
	series := []CDIRate{rate("2025-01-02", "1"), rate("2025-01-03", "1"), rate("2025-01-06", "1")}
	tests := []struct {
		name  string
		moves []Movement
		rates []CDIRate
		pct   string
		asof  string
		want  string
	}{
		{"100% two accrual days (deposit day itself does not accrue)",
			[]Movement{mv("2025-01-02", "deposit", "1000")}, series, "100", "2025-01-06", "1020.1"},
		{"110% of CDI",
			[]Movement{mv("2025-01-02", "deposit", "1000")}, series, "110", "2025-01-06", "1022.121"},
		{"as-of cutoff ignores later rates",
			[]Movement{mv("2025-01-02", "deposit", "1000")}, series, "100", "2025-01-03", "1010"},
		{"second deposit accrues from the following rate",
			[]Movement{mv("2025-01-02", "deposit", "1000"), mv("2025-01-03", "deposit", "500")}, series, "100", "2025-01-06", "1525.1"},
		{"withdraw",
			[]Movement{mv("2025-01-02", "deposit", "1000"), mv("2025-01-03", "withdraw", "500")}, series, "100", "2025-01-06", "515.1"},
		{"manual balance override then accrual continues",
			[]Movement{mv("2025-01-02", "deposit", "1000"), mv("2025-01-03", "balance", "2000")}, series, "100", "2025-01-06", "2020"},
		{"yield adjust after last rate",
			[]Movement{mv("2025-01-02", "deposit", "1000"), mv("2025-01-06", "yield_adjust", "5")}, series, "100", "2025-01-06", "1025.1"},
		{"negative yield adjust (tax)",
			[]Movement{mv("2025-01-02", "deposit", "1000"), mv("2025-01-06", "yield_adjust", "-0.1")}, series, "100", "2025-01-06", "1020"},
		{"no rates: ledger only",
			[]Movement{mv("2025-01-02", "deposit", "1000")}, nil, "100", "2025-01-06", "1000"},
		{"movement after as-of ignored",
			[]Movement{mv("2025-01-02", "deposit", "1000"), mv("2025-02-01", "deposit", "9")}, series, "100", "2025-01-06", "1020.1"},
		{"realistic CDI daily 0.055131 for one day",
			[]Movement{mv("2025-01-02", "deposit", "10000")}, []CDIRate{rate("2025-01-03", "0.055131")}, "100", "2025-01-03", "10005.5131"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CDIBalance(tc.moves, tc.rates, d(tc.pct), tc.asof)
			if !got.Equal(d(tc.want)) {
				t.Errorf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestSimpleBalanceAndTesouroQuantity(t *testing.T) {
	ms := []Movement{mv("2025-01-02", "deposit", "100"), mv("2025-01-03", "withdraw", "30"), mv("2025-01-04", "yield_adjust", "1.5")}
	if got, err := SimpleBalance(ms, "2025-12-31"); err != nil || !got.Equal(d("71.5")) {
		t.Fatalf("balance %s %v", got, err)
	}
	if got, _ := SimpleBalance(ms, "2025-01-03"); !got.Equal(d("70")) {
		t.Fatalf("asof balance %s", got)
	}
	over := append(ms, mv("2025-01-05", "withdraw", "100"))
	if _, err := SimpleBalance(over, "2025-12-31"); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("overdraw err %v", err)
	}
	ts := []Movement{{Date: "2025-01-02", Type: "deposit", Quantity: d("2.5")}, {Date: "2025-01-03", Type: "withdraw", Quantity: d("1")}}
	if q, err := TesouroQuantity(ts, "2025-12-31"); err != nil || !q.Equal(d("1.5")) {
		t.Fatalf("qty %s %v", q, err)
	}
}

func newSvc(t *testing.T) (*Service, *RateStore) {
	t.Helper()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	rs := NewRateStore(conn)
	clock := func() time.Time { return time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC) }
	return NewService(conn, rs, clock), rs
}

func TestSummaryTotalAndSplit(t *testing.T) {
	ctx := context.Background()
	svc, rs := newSvc(t)

	tes, err := svc.Create(ctx, CreateInput{Kind: "tesouro_selic", Name: "Tesouro Selic 2031"})
	if err != nil {
		t.Fatal(err)
	}
	cdi, _ := svc.Create(ctx, CreateInput{Kind: "cdi", Name: "CDB 110% CDI", PctCDI: ptr(dec("110"))})
	usd, _ := svc.Create(ctx, CreateInput{Kind: "usd_cash", Name: "USD broker"})

	q := dec("2")
	if _, err := svc.AddMovement(ctx, tes.ID, MovementInput{Date: "2025-01-02", Type: "deposit", Amount: dec("28000"), Quantity: &q}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddMovement(ctx, cdi.ID, MovementInput{Date: "2025-01-02", Type: "deposit", Amount: dec("1000")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddMovement(ctx, usd.ID, MovementInput{Date: "2025-01-02", Type: "deposit", Amount: dec("100")}); err != nil {
		t.Fatal(err)
	}

	// No Tesouro price yet: valuation reports the missing input.
	if _, err := svc.Summary(ctx, ""); !errors.Is(err, ErrNoRate) {
		t.Fatalf("expected ErrNoRate, got %v", err)
	}

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	// Tesouro published D-1 (01-03); price on 01-06 uses the latest point.
	must(rs.UpsertTesouroSelicPrice(ctx, "2025-01-03", d("15000")))
	must(rs.UpsertTesouroSelicPrice(ctx, "2025-01-02", d("14900")))
	must(rs.UpsertCDI(ctx, "2025-01-03", d("1")))
	must(rs.UpsertCDI(ctx, "2025-01-06", d("1")))
	must(rs.UpsertUSDBRL(ctx, "2025-01-03", d("5.5")))

	sum, err := svc.Summary(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	// Tesouro 2 x 15000 = 30000; CDI 1000 x 1.011^2 = 1022.121; USD 100 x 5.5 = 550.
	if !sum.BRL.Equal(d("31022.121")) || !sum.USD.Equal(d("550")) || !sum.TotalBRL.Equal(d("31572.121")) ||
		!sum.USDBalance.Equal(d("100")) {
		t.Fatalf("summary: %+v", sum)
	}
	// 550 / 31572.121 = 0.017420...
	if !sum.USDShare.Equal(d("0.0174")) {
		t.Fatalf("usd share %s", sum.USDShare)
	}
	if len(sum.Instruments) != 3 || *sum.Instruments[0].RateDate != "2025-01-03" {
		t.Fatalf("instruments: %+v", sum.Instruments)
	}

	// Manual balance override on the CDI instrument.
	if _, err := svc.AddMovement(ctx, cdi.ID, MovementInput{Date: "2025-01-06", Type: "balance", Amount: dec("1030.5")}); err != nil {
		t.Fatal(err)
	}
	v, err := svc.Value(ctx, cdi.ID, "2025-01-06")
	if err != nil || !v.ValueBRL.Equal(d("1030.5")) {
		t.Fatalf("override value: %+v %v", v, err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestMovementValidation(t *testing.T) {
	ctx := context.Background()
	svc, _ := newSvc(t)
	tes, _ := svc.Create(ctx, CreateInput{Kind: "tesouro_selic", Name: "T"})
	cdi, _ := svc.Create(ctx, CreateInput{Kind: "cdi", Name: "C"})
	usd, _ := svc.Create(ctx, CreateInput{Kind: "usd_cash", Name: "U"})
	q1 := dec("1")
	tests := []struct {
		name string
		id   int64
		in   MovementInput
		want error
	}{
		{"bad date", usd.ID, MovementInput{Date: "x", Type: "deposit", Amount: dec("1")}, ErrValidation},
		{"bad type", usd.ID, MovementInput{Date: "2025-01-02", Type: "gift", Amount: dec("1")}, ErrValidation},
		{"zero deposit", usd.ID, MovementInput{Date: "2025-01-02", Type: "deposit", Amount: dec("0")}, ErrValidation},
		{"usd overdraw", usd.ID, MovementInput{Date: "2025-01-02", Type: "withdraw", Amount: dec("1")}, ErrInsufficientBalance},
		{"tesouro needs quantity", tes.ID, MovementInput{Date: "2025-01-02", Type: "deposit", Amount: dec("1")}, ErrValidation},
		{"tesouro no yield adjust", tes.ID, MovementInput{Date: "2025-01-02", Type: "yield_adjust", Amount: dec("1"), Quantity: &q1}, ErrValidation},
		{"tesouro overdraw", tes.ID, MovementInput{Date: "2025-01-02", Type: "withdraw", Amount: dec("1"), Quantity: &q1}, ErrInsufficientBalance},
		{"quantity on cdi", cdi.ID, MovementInput{Date: "2025-01-02", Type: "deposit", Amount: dec("1"), Quantity: &q1}, ErrValidation},
		{"missing instrument", 99, MovementInput{Date: "2025-01-02", Type: "deposit", Amount: dec("1")}, ErrNotFound},
	}
	for _, tc := range tests {
		if _, err := svc.AddMovement(ctx, tc.id, tc.in); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if ms, _ := svc.Movements(ctx, usd.ID); len(ms) != 0 {
		t.Errorf("rejected movement must not persist: %v", ms)
	}
	if _, err := svc.Create(ctx, CreateInput{Kind: "stock", Name: "x"}); !errors.Is(err, ErrValidation) {
		t.Errorf("bad kind: %v", err)
	}
	if _, err := svc.Create(ctx, CreateInput{Kind: "usd_cash", Name: "x", PctCDI: ptr(dec("100"))}); !errors.Is(err, ErrValidation) {
		t.Errorf("pct on usd: %v", err)
	}

	// Deleting a deposit that later withdrawals depend on is rejected.
	dep, _ := svc.AddMovement(ctx, usd.ID, MovementInput{Date: "2025-01-02", Type: "deposit", Amount: dec("10")})
	if _, err := svc.AddMovement(ctx, usd.ID, MovementInput{Date: "2025-01-03", Type: "withdraw", Amount: dec("5")}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteMovement(ctx, dep.ID); !errors.Is(err, ErrInsufficientBalance) {
		t.Errorf("delete dependency: %v", err)
	}
	if ms, _ := svc.Movements(ctx, usd.ID); len(ms) != 2 {
		t.Errorf("movements after rejected delete: %d", len(ms))
	}
}
