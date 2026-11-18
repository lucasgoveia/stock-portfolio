package engine

import "time"

// Easter is the Gregorian Easter Sunday of year (anonymous Gregorian computus).
func Easter(year int) time.Time {
	a := year % 19
	b, c := year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

// BankHolidays returns the Brazilian national bank holidays of year as
// YYYY-MM-DD strings: fixed dates (Black Awareness Day, Nov 20, is national
// from 2024) plus Carnival Monday and Tuesday, Good Friday and Corpus Christi,
// counted from Easter.
func BankHolidays(year int) map[string]bool {
	out := map[string]bool{}
	fixed := [][2]int{{1, 1}, {4, 21}, {5, 1}, {9, 7}, {10, 12}, {11, 2}, {11, 15}, {12, 25}}
	for _, md := range fixed {
		out[time.Date(year, time.Month(md[0]), md[1], 0, 0, 0, 0, time.UTC).Format("2006-01-02")] = true
	}
	if year >= 2024 {
		out[time.Date(year, time.November, 20, 0, 0, 0, 0, time.UTC).Format("2006-01-02")] = true
	}
	easter := Easter(year)
	for _, off := range []int{-48, -47, -2, 60} {
		out[easter.AddDate(0, 0, off).Format("2006-01-02")] = true
	}
	return out
}

// IsBankHoliday reports whether date (a time of any zone, only its calendar
// day counts) is a Brazilian national bank holiday.
func IsBankHoliday(date time.Time) bool {
	return BankHolidays(date.Year())[date.Format("2006-01-02")]
}

// IsBusinessDay is true for Monday to Friday that is not a bank holiday.
func IsBusinessDay(date time.Time) bool {
	wd := date.Weekday()
	return wd != time.Saturday && wd != time.Sunday && !IsBankHoliday(date)
}
