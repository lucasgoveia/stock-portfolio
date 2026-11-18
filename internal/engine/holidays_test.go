package engine

import (
	"testing"
	"time"
)

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestEaster(t *testing.T) {
	for year, want := range map[int]string{
		2020: "2020-04-12", 2022: "2022-04-17", 2023: "2023-04-09", 2024: "2024-03-31",
		2025: "2025-04-20", 2026: "2026-04-05", 2027: "2027-03-28", 2030: "2030-04-21",
		2038: "2038-04-25",
	} {
		if got := Easter(year).Format("2006-01-02"); got != want {
			t.Errorf("%d: got %s want %s", year, got, want)
		}
	}
}

func TestBankHolidays(t *testing.T) {
	for _, c := range []struct {
		date string
		want bool
	}{
		{"2026-01-01", true}, {"2026-04-21", true}, {"2026-05-01", true}, {"2026-09-07", true},
		{"2026-10-12", true}, {"2026-11-02", true}, {"2026-11-15", true}, {"2026-12-25", true},
		{"2026-11-20", true},  // national since 2024
		{"2023-11-20", false}, // not yet national
		{"2026-02-16", true},  // Carnival Monday (Easter Apr 5)
		{"2026-02-17", true},  // Carnival Tuesday
		{"2026-04-03", true},  // Good Friday
		{"2026-06-04", true},  // Corpus Christi
		{"2024-03-29", true},  // Good Friday 2024
		{"2024-05-30", true},  // Corpus Christi 2024
		{"2025-03-04", true},  // Carnival Tuesday 2025
		{"2025-06-19", true},  // Corpus Christi 2025
		{"2026-03-10", false}, // ordinary day
		{"2026-04-20", false}, // Tiradentes eve
	} {
		d := mustDate(t, c.date)
		if got := IsBankHoliday(d); got != c.want {
			t.Errorf("%s: got %v want %v", c.date, got, c.want)
		}
	}
	if IsBusinessDay(mustDate(t, "2026-04-04")) || IsBusinessDay(mustDate(t, "2026-04-03")) || !IsBusinessDay(mustDate(t, "2026-04-06")) {
		t.Error("business day check")
	}
}
