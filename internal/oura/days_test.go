package oura

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// mustDays returns the range first to last, given in YYYY-MM-DD form.
func mustDays(t *testing.T, first, last string) DayRange {
	t.Helper()
	days, err := NewDayRange(mustDate(t, first), mustDate(t, last))
	if err != nil {
		t.Fatalf("NewDayRange(%s, %s): %v", first, last, err)
	}
	return days
}

func mustDate(t *testing.T, day string) time.Time {
	t.Helper()
	date, err := time.Parse(dayLayout, day)
	if err != nil {
		t.Fatalf("parse %q: %v", day, err)
	}
	return date
}

func TestDayRangeListsEveryDayFromFirstToLast(t *testing.T) {
	days := mustDays(t, "2026-10-30", "2026-11-02")

	want := []string{"2026-10-30", "2026-10-31", "2026-11-01", "2026-11-02"}
	if got := days.Days(); !slices.Equal(got, want) {
		t.Errorf("Days() = %v, want %v", got, want)
	}
}

func TestDayRangeOfOneDay(t *testing.T) {
	days := mustDays(t, "2026-11-01", "2026-11-01")

	if got := days.Days(); !slices.Equal(got, []string{"2026-11-01"}) {
		t.Errorf("Days() = %v, want [2026-11-01]", got)
	}
}

func TestNewDayRangeRefusesARangeThatEndsBeforeItStarts(t *testing.T) {
	_, err := NewDayRange(mustDate(t, "2026-11-02"), mustDate(t, "2026-11-01"))

	if !errors.Is(err, errRangeReversed) {
		t.Errorf("error = %v, want %v", err, errRangeReversed)
	}
}

func TestNewDayRangeUsesEachTimesOwnCalendarDate(t *testing.T) {
	detroit, err := time.LoadLocation("America/Detroit")
	if err != nil {
		t.Skipf("no time zone data: %v", err)
	}
	// 11 PM in Detroit on November 1 is already November 2 in UTC.
	lateEvening := time.Date(2026, time.November, 1, 23, 0, 0, 0, detroit)

	days, err := NewDayRange(lateEvening, lateEvening)
	if err != nil {
		t.Fatal(err)
	}
	if got := days.Days(); !slices.Equal(got, []string{"2026-11-01"}) {
		t.Errorf("Days() = %v, want [2026-11-01]", got)
	}
}

func TestDayRangeCrossesADaylightSavingChange(t *testing.T) {
	// Daylight saving time ends in the United States on November 1, 2026.
	days := mustDays(t, "2026-10-31", "2026-11-02")

	want := []string{"2026-10-31", "2026-11-01", "2026-11-02"}
	if got := days.Days(); !slices.Equal(got, want) {
		t.Errorf("Days() = %v, want %v", got, want)
	}
}

func TestDayRangeContainsOnlyItsOwnDays(t *testing.T) {
	days := mustDays(t, "2026-11-01", "2026-11-03")

	cases := map[string]bool{
		"2026-10-31": false,
		"2026-11-01": true,
		"2026-11-02": true,
		"2026-11-03": true,
		"2026-11-04": false,
	}
	for day, want := range cases {
		if got := days.contains(day); got != want {
			t.Errorf("contains(%s) = %v, want %v", day, got, want)
		}
	}
}

func TestDayRangeQueriesADayEitherSide(t *testing.T) {
	days := mustDays(t, "2026-11-01", "2026-11-03")

	start, end := days.queryBounds()
	if start != "2026-10-31" || end != "2026-11-04" {
		t.Errorf("queryBounds() = %s, %s, want 2026-10-31, 2026-11-04", start, end)
	}
}
