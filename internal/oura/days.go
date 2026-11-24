package oura

import (
	"errors"
	"time"
)

// dayLayout is how Oura writes a day, and how it takes start_date and
// end_date.
const dayLayout = time.DateOnly

var errRangeReversed = errors.New("the date range ends before it starts")

// DayRange is a run of calendar days, first to last, both included. Days
// are wake-up dates, which is how Oura dates both daily_sleep and sleep.
type DayRange struct {
	first time.Time
	last  time.Time
}

// NewDayRange returns the days from first to last, both included. Only the
// calendar date of each is used, in its own location.
func NewDayRange(first, last time.Time) (DayRange, error) {
	days := DayRange{first: dateOf(first), last: dateOf(last)}
	if days.last.Before(days.first) {
		return DayRange{}, errRangeReversed
	}
	return days, nil
}

// Days returns each day in the range, in order, in Oura's YYYY-MM-DD form.
func (r DayRange) Days() []string {
	var days []string
	for day := r.first; !day.After(r.last); day = day.AddDate(0, 0, 1) {
		days = append(days, day.Format(dayLayout))
	}
	return days
}

// contains reports whether day, in YYYY-MM-DD form, is in the range.
// Days in that form sort as strings in date order.
func (r DayRange) contains(day string) bool {
	return day >= r.first.Format(dayLayout) && day <= r.last.Format(dayLayout)
}

// queryBounds returns the start_date and end_date to ask Oura for: a day
// either side of the range. Oura does not document whether end_date is
// included, and users report that the sleep collection leaves it out while
// daily_sleep keeps it. Asking for a wider range and keeping only the
// documents whose day is in the range gives the same answer either way.
func (r DayRange) queryBounds() (start, end string) {
	return r.first.AddDate(0, 0, -1).Format(dayLayout), r.last.AddDate(0, 0, 1).Format(dayLayout)
}

// dateOf returns t's calendar date as midnight UTC, so that adding days
// never crosses a daylight saving change.
func dateOf(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
