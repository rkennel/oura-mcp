package oura

// The Oura collections the client reads, named as in Oura's paths.
const (
	dailySleepCollection = "daily_sleep"
	sleepCollection      = "sleep"
)

// longSleep is the type Oura gives the main sleep of a night. Naps and
// shorter sleeps have other types, such as late_nap, sleep or rest.
const longSleep = "long_sleep"

// DailySleep is the part of an Oura daily_sleep document the summary uses.
type DailySleep struct {
	// Day is the wake-up date, in YYYY-MM-DD form.
	Day string `json:"day"`
	// Score is the sleep score, or nil when Oura has not scored the night.
	Score *int `json:"score"`
}

// SleepPeriod is the part of an Oura sleep document the summary uses. Oura
// has one for every sleep it detects, naps included.
type SleepPeriod struct {
	// Day is the wake-up date, in YYYY-MM-DD form.
	Day string `json:"day"`
	// Type tells the main sleep of a night (long_sleep) from naps and
	// shorter sleeps.
	Type string `json:"type"`
	// TotalSleepDuration is the time asleep, in seconds.
	TotalSleepDuration *int `json:"total_sleep_duration"`
	// LowestHeartRate is in beats per minute.
	LowestHeartRate *float64 `json:"lowest_heart_rate"`
	// AverageHRV is in milliseconds.
	AverageHRV *float64 `json:"average_hrv"`
}

func (d DailySleep) day() string  { return d.Day }
func (s SleepPeriod) day() string { return s.Day }

// sleepSeconds returns the time asleep, counting an unknown duration as
// none.
func (s SleepPeriod) sleepSeconds() int {
	if s.TotalSleepDuration == nil {
		return 0
	}
	return *s.TotalSleepDuration
}
