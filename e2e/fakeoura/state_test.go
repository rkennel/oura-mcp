package fakeoura

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

func day(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(dayLayout, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

func documentsOf(values ...string) []json.RawMessage {
	documents := make([]json.RawMessage, 0, len(values))
	for _, value := range values {
		documents = append(documents, json.RawMessage(value))
	}
	return documents
}

func TestIssuedTokensAreDistinctAndTheAccessTokenIsAuthorized(t *testing.T) {
	state := NewState()

	first, second := state.IssueTokens(), state.IssueTokens()

	if first.AccessToken == "" || first.RefreshToken == "" || first.AccessToken == first.RefreshToken {
		t.Errorf("tokens = %+v, want two different non-empty tokens", first)
	}
	if first == second {
		t.Error("two issues returned the same tokens")
	}
	if !state.Authorized(first.AccessToken) || !state.Authorized(second.AccessToken) {
		t.Error("an issued access token is not authorized")
	}
}

func TestRefreshIssuesANewPair(t *testing.T) {
	state := NewState()
	issued := state.IssueTokens()

	refreshed, err := state.Refresh(issued.RefreshToken)

	if err != nil {
		t.Fatalf("Refresh returned an error: %v", err)
	}
	if refreshed.RefreshToken == issued.RefreshToken || refreshed.AccessToken == issued.AccessToken {
		t.Errorf("Refresh returned %+v, want a new pair", refreshed)
	}
	if !state.Authorized(refreshed.AccessToken) {
		t.Error("the refreshed access token is not authorized")
	}
}

func TestARefreshTokenWorksOnlyOnce(t *testing.T) {
	state := NewState()
	issued := state.IssueTokens()
	if _, err := state.Refresh(issued.RefreshToken); err != nil {
		t.Fatalf("first Refresh returned an error: %v", err)
	}

	_, err := state.Refresh(issued.RefreshToken)

	if !errors.Is(err, ErrRefreshTokenRejected) {
		t.Errorf("second Refresh error = %v, want %v", err, ErrRefreshTokenRejected)
	}
}

func TestRefreshRejectsATokenItNeverIssued(t *testing.T) {
	state := NewState()

	for name, token := range map[string]string{"unknown": "never-issued", "empty": ""} {
		t.Run(name, func(t *testing.T) {
			if _, err := state.Refresh(token); !errors.Is(err, ErrRefreshTokenRejected) {
				t.Errorf("error = %v, want %v", err, ErrRefreshTokenRejected)
			}
		})
	}
}

func TestRefreshRejectsAnAccessToken(t *testing.T) {
	state := NewState()
	issued := state.IssueTokens()

	if _, err := state.Refresh(issued.AccessToken); !errors.Is(err, ErrRefreshTokenRejected) {
		t.Errorf("error = %v, want %v", err, ErrRefreshTokenRejected)
	}
}

func TestExpireAccessTokensKeepsRefreshTokensUsable(t *testing.T) {
	state := NewState()
	issued := state.IssueTokens()

	state.ExpireAccessTokens()

	if state.Authorized(issued.AccessToken) {
		t.Error("the access token is still authorized after expiry")
	}
	refreshed, err := state.Refresh(issued.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh after expiry returned an error: %v", err)
	}
	if !state.Authorized(refreshed.AccessToken) {
		t.Error("the access token issued after expiry is not authorized")
	}
}

func TestAuthorizedRejectsUnknownTokens(t *testing.T) {
	state := NewState()
	issued := state.IssueTokens()

	for name, token := range map[string]string{"unknown": "never-issued", "empty": "", "refresh token": issued.RefreshToken} {
		t.Run(name, func(t *testing.T) {
			if state.Authorized(token) {
				t.Errorf("Authorized(%q) = true", token)
			}
		})
	}
}

func TestDocumentsReturnsTheSeededDaysInRangeInSeedOrder(t *testing.T) {
	state := NewState()
	err := state.Seed(map[string][]json.RawMessage{DailySleep: documentsOf(
		`{"day":"2026-11-03","score":70}`,
		`{"day":"2026-11-01","score":80}`,
		`{"day":"2026-10-31","score":60}`,
		`{"day":"2026-11-02","score":90}`,
		`{"day":"2026-11-04","score":50}`,
	)})
	if err != nil {
		t.Fatalf("Seed returned an error: %v", err)
	}

	got := state.Documents(DailySleep, day(t, "2026-11-01"), day(t, "2026-11-03"))

	want := []string{`{"day":"2026-11-03","score":70}`, `{"day":"2026-11-01","score":80}`, `{"day":"2026-11-02","score":90}`}
	if len(got) != len(want) {
		t.Fatalf("got %d documents %s, want %d", len(got), got, len(want))
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Errorf("document %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestDocumentsIsEmptyWhenNothingMatches(t *testing.T) {
	state := NewState()

	got := state.Documents(Sleep, day(t, "2026-11-01"), day(t, "2026-11-30"))

	if got == nil || len(got) != 0 {
		t.Errorf("got %#v, want an empty, non-nil slice so the answer has \"data\": []", got)
	}
}

func TestSeedKeepsCollectionsApart(t *testing.T) {
	state := NewState()
	err := state.Seed(map[string][]json.RawMessage{
		DailySleep: documentsOf(`{"day":"2026-11-01","kind":"daily"}`),
		Sleep:      documentsOf(`{"day":"2026-11-01","kind":"period"}`),
	})
	if err != nil {
		t.Fatalf("Seed returned an error: %v", err)
	}

	got := state.Documents(Sleep, day(t, "2026-11-01"), day(t, "2026-11-01"))

	if len(got) != 1 || string(got[0]) != `{"day":"2026-11-01","kind":"period"}` {
		t.Errorf("sleep documents = %s, want only the period", got)
	}
}

func TestSeedReplacesOnlyTheCollectionsItNames(t *testing.T) {
	state := NewState()
	mustSeed(t, state, map[string][]json.RawMessage{
		DailySleep: documentsOf(`{"day":"2026-11-01","version":1}`),
		Sleep:      documentsOf(`{"day":"2026-11-01","kept":true}`),
	})

	mustSeed(t, state, map[string][]json.RawMessage{DailySleep: documentsOf(`{"day":"2026-11-01","version":2}`)})

	november1 := day(t, "2026-11-01")
	if got := state.Documents(DailySleep, november1, november1); len(got) != 1 || string(got[0]) != `{"day":"2026-11-01","version":2}` {
		t.Errorf("daily_sleep = %s, want only the second seed", got)
	}
	if got := state.Documents(Sleep, november1, november1); len(got) != 1 {
		t.Errorf("sleep = %s, want the document seeded before", got)
	}
}

func TestSeedRejectsBadInputAndChangesNothing(t *testing.T) {
	cases := map[string]map[string][]json.RawMessage{
		"unknown collection": {"heart_rate": documentsOf(`{"day":"2026-11-01"}`)},
		"no day":             {Sleep: documentsOf(`{"score":80}`)},
		"day not a date":     {Sleep: documentsOf(`{"day":"yesterday"}`)},
		"not an object":      {Sleep: documentsOf(`["2026-11-01"]`)},
		"null document":      {Sleep: documentsOf(`null`)},
		"one bad among good": {
			DailySleep: documentsOf(`{"day":"2026-11-02","new":true}`),
			Sleep:      documentsOf(`{"day":"2026-11-02"}`, `{"score":1}`),
		},
	}
	for name, collections := range cases {
		t.Run(name, func(t *testing.T) {
			state := NewState()
			mustSeed(t, state, map[string][]json.RawMessage{DailySleep: documentsOf(`{"day":"2026-11-02","old":true}`)})

			if err := state.Seed(collections); err == nil {
				t.Fatal("Seed accepted bad input")
			}

			november2 := day(t, "2026-11-02")
			if got := state.Documents(DailySleep, november2, november2); len(got) != 1 || string(got[0]) != `{"day":"2026-11-02","old":true}` {
				t.Errorf("daily_sleep = %s, want it unchanged", got)
			}
			if got := state.Documents(Sleep, november2, november2); len(got) != 0 {
				t.Errorf("sleep = %s, want it unchanged and empty", got)
			}
		})
	}
}

func TestResetForgetsTokensDataAndFailures(t *testing.T) {
	state := NewState()
	issued := state.IssueTokens()
	mustSeed(t, state, map[string][]json.RawMessage{Sleep: documentsOf(`{"day":"2026-11-01"}`)})
	if err := state.FailNext(SleepPath, http.StatusServiceUnavailable, 1); err != nil {
		t.Fatalf("FailNext returned an error: %v", err)
	}

	state.Reset()

	if state.Authorized(issued.AccessToken) {
		t.Error("the access token survived a reset")
	}
	if _, err := state.Refresh(issued.RefreshToken); err == nil {
		t.Error("the refresh token survived a reset")
	}
	if got := state.Documents(Sleep, day(t, "2026-11-01"), day(t, "2026-11-01")); len(got) != 0 {
		t.Errorf("documents survived a reset: %s", got)
	}
	if _, fail := state.takeFailure(SleepPath); fail {
		t.Error("a pending failure survived a reset")
	}
}

func TestFailNextFailsExactlyCountRequests(t *testing.T) {
	state := NewState()
	if err := state.FailNext(TokenPath, http.StatusServiceUnavailable, 2); err != nil {
		t.Fatalf("FailNext returned an error: %v", err)
	}

	for i := range 2 {
		if status, fail := state.takeFailure(TokenPath); !fail || status != http.StatusServiceUnavailable {
			t.Errorf("request %d: takeFailure = %d, %t, want %d, true", i+1, status, fail, http.StatusServiceUnavailable)
		}
	}
	if _, fail := state.takeFailure(TokenPath); fail {
		t.Error("request 3 failed, want only 2 failures")
	}
}

func TestFailNextAffectsOnlyItsEndpoint(t *testing.T) {
	state := NewState()
	if err := state.FailNext(SleepPath, http.StatusInternalServerError, 1); err != nil {
		t.Fatalf("FailNext returned an error: %v", err)
	}

	if _, fail := state.takeFailure(DailySleepPath); fail {
		t.Error("daily_sleep failed, but only sleep was set to fail")
	}
}

func TestFailNextWithZeroCountClearsAPendingFailure(t *testing.T) {
	state := NewState()
	if err := state.FailNext(SleepPath, http.StatusInternalServerError, 3); err != nil {
		t.Fatalf("FailNext returned an error: %v", err)
	}

	if err := state.FailNext(SleepPath, http.StatusInternalServerError, 0); err != nil {
		t.Fatalf("FailNext returned an error: %v", err)
	}

	if _, fail := state.takeFailure(SleepPath); fail {
		t.Error("the failure is still pending")
	}
}

func TestFailNextRejectsBadInput(t *testing.T) {
	cases := map[string]struct {
		path          string
		status, count int
	}{
		"not an Oura endpoint": {"/_control/reset", 500, 1},
		"status below 400":     {SleepPath, 302, 1},
		"status above 599":     {SleepPath, 600, 1},
		"negative count":       {SleepPath, 500, -1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if err := NewState().FailNext(c.path, c.status, c.count); err == nil {
				t.Errorf("FailNext(%q, %d, %d) returned nil", c.path, c.status, c.count)
			}
		})
	}
}

func mustSeed(t *testing.T, state *State, collections map[string][]json.RawMessage) {
	t.Helper()
	if err := state.Seed(collections); err != nil {
		t.Fatalf("Seed returned an error: %v", err)
	}
}
