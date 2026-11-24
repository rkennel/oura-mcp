package oura

import (
	"math"
	"strconv"
)

// NoDataYet is how a value Oura does not have yet is reported, rather than
// estimated.
const NoDataYet = "no data yet"

// Reading is one value of a summary, or no data yet. Its zero value is no
// data yet.
type Reading struct {
	value int
	known bool
}

// knownReading returns a Reading of value.
func knownReading(value int) Reading {
	return Reading{value: value, known: true}
}

// roundedReading returns value rounded to a whole number, or no data yet
// when value is nil.
func roundedReading(value *float64) Reading {
	if value == nil {
		return Reading{}
	}
	return knownReading(int(math.Round(*value)))
}

// Value returns the reading and true, or 0 and false when there is no data
// yet.
func (r Reading) Value() (int, bool) {
	return r.value, r.known
}

// String returns the reading as a number, or NoDataYet.
func (r Reading) String() string {
	if !r.known {
		return NoDataYet
	}
	return strconv.Itoa(r.value)
}

// Summary is what the tracker needs for one wake-up date.
type Summary struct {
	// Day is the wake-up date, in YYYY-MM-DD form.
	Day string
	// SleepScore is Oura's sleep score for the night.
	SleepScore Reading
	// LowestHeartRate is from the night's main sleep, in beats per minute.
	LowestHeartRate Reading
	// AverageHRV is from the night's main sleep, in milliseconds, rounded
	// to a whole number.
	AverageHRV Reading
}

// Summarize builds a Summary for each day in days from Oura's daily_sleep
// and sleep documents. Heart rate and HRV come from the night's main sleep
// only, so naps never count. A value Oura does not have, because the ring
// has not synced, the night is not scored or there is no main sleep, is
// reported as no data yet and never estimated.
func Summarize(days DayRange, daily []DailySleep, sleeps []SleepPeriod) []Summary {
	scores := scoresByDay(daily)
	mainSleeps := mainSleepsByDay(sleeps)

	var summaries []Summary
	for _, day := range days.Days() {
		summary := Summary{Day: day, SleepScore: scores[day]}
		if main, ok := mainSleeps[day]; ok {
			summary.LowestHeartRate = roundedReading(main.LowestHeartRate)
			summary.AverageHRV = roundedReading(main.AverageHRV)
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

// scoresByDay returns each day's sleep score, keeping the first scored
// document when a day has more than one.
func scoresByDay(daily []DailySleep) map[string]Reading {
	scores := map[string]Reading{}
	for _, document := range daily {
		if _, seen := scores[document.Day]; seen || document.Score == nil {
			continue
		}
		scores[document.Day] = knownReading(*document.Score)
	}
	return scores
}

// mainSleepsByDay returns each day's main sleep: its long sleep, or the
// longest of them when a day has several, as when a night's sleep is
// broken into two. Days with only naps or short sleeps have none.
func mainSleepsByDay(sleeps []SleepPeriod) map[string]SleepPeriod {
	mainSleeps := map[string]SleepPeriod{}
	for _, period := range sleeps {
		if period.Type != longSleep {
			continue
		}
		current, seen := mainSleeps[period.Day]
		if !seen || period.sleepSeconds() > current.sleepSeconds() {
			mainSleeps[period.Day] = period
		}
	}
	return mainSleeps
}
