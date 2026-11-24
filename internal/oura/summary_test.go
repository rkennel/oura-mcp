package oura

import (
	"testing"
)

func intPointer(value int) *int { return &value }

func floatPointer(value float64) *float64 { return &value }

// overnight returns a night's main sleep, woken from on day.
func overnight(day string, hours int, lowestHeartRate, averageHRV float64) SleepPeriod {
	return SleepPeriod{
		Day:                day,
		Type:               longSleep,
		TotalSleepDuration: intPointer(hours * 3600),
		LowestHeartRate:    floatPointer(lowestHeartRate),
		AverageHRV:         floatPointer(averageHRV),
	}
}

// assertReading fails the test unless got is want, or no data yet when
// want is nil.
func assertReading(t *testing.T, name string, got Reading, want *int) {
	t.Helper()
	value, known := got.Value()
	switch {
	case want == nil && known:
		t.Errorf("%s = %d, want %s", name, value, NoDataYet)
	case want != nil && !known:
		t.Errorf("%s = %s, want %d", name, NoDataYet, *want)
	case want != nil && value != *want:
		t.Errorf("%s = %d, want %d", name, value, *want)
	}
}

// testDay is the wake-up date the single-day tests summarize.
const testDay = "2026-11-01"

// onlySummary summarizes testDay and returns its summary.
func onlySummary(t *testing.T, daily []DailySleep, sleeps []SleepPeriod) Summary {
	t.Helper()
	summaries := Summarize(mustDays(t, testDay, testDay), daily, sleeps)
	if len(summaries) != 1 {
		t.Fatalf("got %d summaries, want 1", len(summaries))
	}
	if summaries[0].Day != testDay {
		t.Errorf("Day = %s, want %s", summaries[0].Day, testDay)
	}
	return summaries[0]
}

func TestSummarizeANormalNight(t *testing.T) {
	daily := []DailySleep{{Day: "2026-11-01", Score: intPointer(82)}}
	sleeps := []SleepPeriod{overnight("2026-11-01", 7, 52, 41.6)}

	summary := onlySummary(t, daily, sleeps)

	assertReading(t, "SleepScore", summary.SleepScore, intPointer(82))
	assertReading(t, "LowestHeartRate", summary.LowestHeartRate, intPointer(52))
	assertReading(t, "AverageHRV", summary.AverageHRV, intPointer(42))
}

func TestSummarizeRoundsHRVToAWholeNumber(t *testing.T) {
	cases := map[float64]int{41.4: 41, 41.5: 42, 41.0: 41, 40.9: 41}
	for hrv, want := range cases {
		summary := onlySummary(t, nil, []SleepPeriod{overnight("2026-11-01", 7, 52, hrv)})

		assertReading(t, "AverageHRV", summary.AverageHRV, intPointer(want))
	}
}

func TestSummarizeIgnoresANap(t *testing.T) {
	sleeps := []SleepPeriod{
		overnight("2026-11-01", 7, 52, 41),
		{
			Day:                "2026-11-01",
			Type:               "late_nap",
			TotalSleepDuration: intPointer(1800),
			LowestHeartRate:    floatPointer(48),
			AverageHRV:         floatPointer(60),
		},
	}

	summary := onlySummary(t, nil, sleeps)

	assertReading(t, "LowestHeartRate", summary.LowestHeartRate, intPointer(52))
	assertReading(t, "AverageHRV", summary.AverageHRV, intPointer(41))
}

func TestSummarizeNeverUsesANapWhenThereIsNoMainSleep(t *testing.T) {
	sleeps := []SleepPeriod{
		{Day: "2026-11-01", Type: "late_nap", LowestHeartRate: floatPointer(48), AverageHRV: floatPointer(60)},
		{Day: "2026-11-01", Type: "sleep", LowestHeartRate: floatPointer(50), AverageHRV: floatPointer(55)},
		{Day: "2026-11-01", Type: "rest", LowestHeartRate: floatPointer(51), AverageHRV: floatPointer(50)},
	}

	summary := onlySummary(t, nil, sleeps)

	assertReading(t, "LowestHeartRate", summary.LowestHeartRate, nil)
	assertReading(t, "AverageHRV", summary.AverageHRV, nil)
}

func TestSummarizeUsesTheLongestOfSeveralLongSleeps(t *testing.T) {
	sleeps := []SleepPeriod{
		overnight("2026-11-01", 3, 58, 30),
		overnight("2026-11-01", 5, 50, 45),
		overnight("2026-11-01", 4, 55, 35),
	}

	summary := onlySummary(t, nil, sleeps)

	assertReading(t, "LowestHeartRate", summary.LowestHeartRate, intPointer(50))
	assertReading(t, "AverageHRV", summary.AverageHRV, intPointer(45))
}

func TestSummarizeCountsALongSleepWithNoDurationAsShortest(t *testing.T) {
	unmeasured := overnight("2026-11-01", 0, 60, 20)
	unmeasured.TotalSleepDuration = nil
	sleeps := []SleepPeriod{unmeasured, overnight("2026-11-01", 6, 50, 45)}

	summary := onlySummary(t, nil, sleeps)

	assertReading(t, "LowestHeartRate", summary.LowestHeartRate, intPointer(50))
}

func TestSummarizeReportsNoDataYetRatherThanEstimating(t *testing.T) {
	cases := []struct {
		name           string
		daily          []DailySleep
		sleeps         []SleepPeriod
		wantScore      *int
		wantHeartRate  *int
		wantAverageHRV *int
	}{
		{
			name:           "missing score",
			daily:          []DailySleep{{Day: "2026-11-01", Score: nil}},
			sleeps:         []SleepPeriod{overnight("2026-11-01", 7, 52, 41)},
			wantHeartRate:  intPointer(52),
			wantAverageHRV: intPointer(41),
		},
		{
			name:      "missing sleep record",
			daily:     []DailySleep{{Day: "2026-11-01", Score: intPointer(82)}},
			wantScore: intPointer(82),
		},
		{
			name: "ring not yet synced",
		},
		{
			name: "main sleep without heart rate or HRV",
			sleeps: []SleepPeriod{{
				Day: "2026-11-01", Type: longSleep, TotalSleepDuration: intPointer(25000),
			}},
		},
		{
			name:   "documents for other days only",
			daily:  []DailySleep{{Day: "2026-10-31", Score: intPointer(70)}},
			sleeps: []SleepPeriod{overnight("2026-11-02", 7, 52, 41)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary := onlySummary(t, tc.daily, tc.sleeps)

			assertReading(t, "SleepScore", summary.SleepScore, tc.wantScore)
			assertReading(t, "LowestHeartRate", summary.LowestHeartRate, tc.wantHeartRate)
			assertReading(t, "AverageHRV", summary.AverageHRV, tc.wantAverageHRV)
		})
	}
}

func TestSummarizeKeepsTheFirstScoreWhenADayHasTwo(t *testing.T) {
	daily := []DailySleep{
		{Day: "2026-11-01", Score: nil},
		{Day: "2026-11-01", Score: intPointer(80)},
		{Day: "2026-11-01", Score: intPointer(60)},
	}

	summary := onlySummary(t, daily, nil)

	assertReading(t, "SleepScore", summary.SleepScore, intPointer(80))
}

func TestSummarizeGivesEveryDayInTheRangeItsOwnValues(t *testing.T) {
	daily := []DailySleep{
		{Day: "2026-11-01", Score: intPointer(82)},
		{Day: "2026-11-03", Score: intPointer(75)},
	}
	sleeps := []SleepPeriod{
		overnight("2026-11-03", 6, 56, 38),
		overnight("2026-11-01", 7, 52, 41),
	}

	summaries := Summarize(mustDays(t, "2026-11-01", "2026-11-03"), daily, sleeps)

	if len(summaries) != 3 {
		t.Fatalf("got %d summaries, want 3", len(summaries))
	}
	first, second, third := summaries[0], summaries[1], summaries[2]
	if first.Day != "2026-11-01" || second.Day != "2026-11-02" || third.Day != "2026-11-03" {
		t.Fatalf("days = %s, %s, %s, want 2026-11-01 to 2026-11-03 in order", first.Day, second.Day, third.Day)
	}
	assertReading(t, "first SleepScore", first.SleepScore, intPointer(82))
	assertReading(t, "first LowestHeartRate", first.LowestHeartRate, intPointer(52))
	assertReading(t, "second SleepScore", second.SleepScore, nil)
	assertReading(t, "second AverageHRV", second.AverageHRV, nil)
	assertReading(t, "third SleepScore", third.SleepScore, intPointer(75))
	assertReading(t, "third AverageHRV", third.AverageHRV, intPointer(38))
}

func TestReadingStringIsTheNumberOrNoDataYet(t *testing.T) {
	if got := knownReading(57).String(); got != "57" {
		t.Errorf("String() = %q, want %q", got, "57")
	}
	if got := (Reading{}).String(); got != NoDataYet {
		t.Errorf("String() = %q, want %q", got, NoDataYet)
	}
}
