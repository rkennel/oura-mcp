package oura

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testBaseURL     = "https://oura.example"
	testAccessToken = "test-access-token"
)

// fixedToken is a TokenSource that always supplies the same token, or err.
type fixedToken struct {
	token string
	err   error
}

func (f fixedToken) AccessToken(context.Context) (string, error) {
	return f.token, f.err
}

// fakeOura is a Doer that answers each request with respond and records
// the requests it was sent.
type fakeOura struct {
	mu       sync.Mutex
	requests []*http.Request
	respond  func(request *http.Request) (*http.Response, error)
}

func (f *fakeOura) Do(request *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, request)
	f.mu.Unlock()
	return f.respond(request)
}

// answer returns a response with status and body.
func answer(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// answering returns a fakeOura that gives every request status and body.
func answering(status int, body string) *fakeOura {
	return &fakeOura{respond: func(*http.Request) (*http.Response, error) {
		return answer(status, body), nil
	}}
}

// byCollection returns a fakeOura that answers each collection with its
// body, as one page.
func byCollection(bodies map[string]string) *fakeOura {
	return &fakeOura{respond: func(request *http.Request) (*http.Response, error) {
		collection := strings.TrimPrefix(request.URL.Path, collectionPath)
		body, ok := bodies[collection]
		if !ok {
			return answer(http.StatusNotFound, `{"detail":"Not Found"}`), nil
		}
		return answer(http.StatusOK, body), nil
	}}
}

func newTestClient(doer Doer) *Client {
	return NewClient(testBaseURL+"/", fixedToken{token: testAccessToken}, doer)
}

func TestClientAsksOuraForTheCollectionWithTheAccessToken(t *testing.T) {
	oura := answering(http.StatusOK, `{"data": [], "next_token": null}`)

	if _, err := newTestClient(oura).Sleep(t.Context(), mustDays(t, "2026-11-01", "2026-11-03")); err != nil {
		t.Fatal(err)
	}

	if len(oura.requests) != 1 {
		t.Fatalf("sent %d requests, want 1", len(oura.requests))
	}
	request := oura.requests[0]
	if request.Method != http.MethodGet {
		t.Errorf("method = %s, want GET", request.Method)
	}
	if got, want := request.URL.Scheme+"://"+request.URL.Host+request.URL.Path, testBaseURL+"/v2/usercollection/sleep"; got != want {
		t.Errorf("URL = %s, want %s", got, want)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer "+testAccessToken {
		t.Errorf("Authorization = %q, want the bearer token", got)
	}
	query := request.URL.Query()
	if query.Get("start_date") != "2026-10-31" || query.Get("end_date") != "2026-11-04" {
		t.Errorf("dates = %s to %s, want a day either side of the range: 2026-10-31 to 2026-11-04",
			query.Get("start_date"), query.Get("end_date"))
	}
	if query.Has("next_token") {
		t.Error("the first request sent a next_token")
	}
}

func TestClientReadsDailySleepDocuments(t *testing.T) {
	oura := byCollection(map[string]string{
		"daily_sleep": `{"data": [{"id": "a", "day": "2026-11-01", "score": 82, "contributors": {"deep_sleep": 90}}], "next_token": null}`,
	})

	got, err := newTestClient(oura).DailySleep(t.Context(), mustDays(t, "2026-11-01", "2026-11-01"))
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].Day != "2026-11-01" || got[0].Score == nil || *got[0].Score != 82 {
		t.Errorf("DailySleep() = %+v, want one document for 2026-11-01 scored 82", got)
	}
}

func TestClientReadsSleepDocuments(t *testing.T) {
	oura := byCollection(map[string]string{
		"sleep": `{"data": [{"day": "2026-11-01", "type": "long_sleep", "total_sleep_duration": 25200,
			"lowest_heart_rate": 52, "average_hrv": 41.6, "bedtime_start": "2026-10-31T23:01:00-04:00"}], "next_token": null}`,
	})

	got, err := newTestClient(oura).Sleep(t.Context(), mustDays(t, "2026-11-01", "2026-11-01"))
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 {
		t.Fatalf("Sleep() returned %d documents, want 1", len(got))
	}
	period := got[0]
	if period.Day != "2026-11-01" || period.Type != longSleep || period.sleepSeconds() != 25200 ||
		*period.LowestHeartRate != 52 || *period.AverageHRV != 41.6 {
		t.Errorf("Sleep() = %+v, want the long sleep as Oura sent it", period)
	}
}

func TestClientKeepsOnlyDocumentsDatedInTheRange(t *testing.T) {
	oura := byCollection(map[string]string{
		"daily_sleep": `{"data": [
			{"day": "2026-10-31", "score": 70},
			{"day": "2026-11-01", "score": 82},
			{"day": "2026-11-02", "score": 90}
		], "next_token": null}`,
	})

	got, err := newTestClient(oura).DailySleep(t.Context(), mustDays(t, "2026-11-01", "2026-11-01"))
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].Day != "2026-11-01" {
		t.Errorf("DailySleep() = %+v, want only 2026-11-01", got)
	}
}

// TestClientGetsTheLastDayWhenOuraLeavesOutEndDate covers what users report
// the sleep collection does: it leaves the end_date day out.
func TestClientGetsTheLastDayWhenOuraLeavesOutEndDate(t *testing.T) {
	documents := map[string]string{
		"2026-11-01": `{"day": "2026-11-01", "type": "long_sleep", "lowest_heart_rate": 52, "average_hrv": 41}`,
		"2026-11-02": `{"day": "2026-11-02", "type": "long_sleep", "lowest_heart_rate": 50, "average_hrv": 44}`,
	}
	oura := &fakeOura{respond: func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		var data []string
		for day, document := range documents {
			if day >= query.Get("start_date") && day < query.Get("end_date") {
				data = append(data, document)
			}
		}
		return answer(http.StatusOK, `{"data": [`+strings.Join(data, ",")+`], "next_token": null}`), nil
	}}

	got, err := newTestClient(oura).Sleep(t.Context(), mustDays(t, "2026-11-02", "2026-11-02"))
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].Day != "2026-11-02" {
		t.Errorf("Sleep() = %+v, want the night woken from on 2026-11-02", got)
	}
}

func TestClientFollowsNextToken(t *testing.T) {
	oura := &fakeOura{respond: func(request *http.Request) (*http.Response, error) {
		if request.URL.Query().Get("next_token") == "page-2" {
			return answer(http.StatusOK, `{"data": [{"day": "2026-11-02", "score": 75}], "next_token": null}`), nil
		}
		return answer(http.StatusOK, `{"data": [{"day": "2026-11-01", "score": 82}], "next_token": "page-2"}`), nil
	}}

	got, err := newTestClient(oura).DailySleep(t.Context(), mustDays(t, "2026-11-01", "2026-11-02"))
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].Day != "2026-11-01" || got[1].Day != "2026-11-02" {
		t.Errorf("DailySleep() = %+v, want both pages in order", got)
	}
	if len(oura.requests) != 2 {
		t.Fatalf("sent %d requests, want 2", len(oura.requests))
	}
	second := oura.requests[1].URL.Query()
	if second.Get("start_date") != "2026-10-31" || second.Get("end_date") != "2026-11-03" {
		t.Errorf("the second page asked for %s to %s, want the same dates as the first",
			second.Get("start_date"), second.Get("end_date"))
	}
}

func TestClientStopsWhenNextTokenNeverRunsOut(t *testing.T) {
	oura := answering(http.StatusOK, `{"data": [], "next_token": "again"}`)

	_, err := newTestClient(oura).DailySleep(t.Context(), mustDays(t, "2026-11-01", "2026-11-01"))

	if !errors.Is(err, errTooManyPages) {
		t.Errorf("error = %v, want %v", err, errTooManyPages)
	}
	if len(oura.requests) != maxPages {
		t.Errorf("sent %d requests, want %d", len(oura.requests), maxPages)
	}
}

func TestClientTreatsAnEmptyNextTokenAsTheLastPage(t *testing.T) {
	oura := answering(http.StatusOK, `{"data": [], "next_token": ""}`)

	if _, err := newTestClient(oura).DailySleep(t.Context(), mustDays(t, "2026-11-01", "2026-11-01")); err != nil {
		t.Fatal(err)
	}
	if len(oura.requests) != 1 {
		t.Errorf("sent %d requests, want 1", len(oura.requests))
	}
}

func TestClientSurfacesOurasErrorStatus(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantDetail string
	}{
		{"expired token", http.StatusUnauthorized, `{"detail": "Invalid or expired authentication token."}`, "Invalid or expired authentication token."},
		{"rate limited", http.StatusTooManyRequests, `{"detail": "Rate limit exceeded"}`, "Rate limit exceeded"},
		{"outage, no body", http.StatusServiceUnavailable, ``, "Service Unavailable"},
		{"validation error, detail not text", http.StatusUnprocessableEntity, `{"detail": [{"msg": "bad date"}]}`, "Unprocessable Entity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newTestClient(answering(tc.status, tc.body)).Sleep(t.Context(), mustDays(t, "2026-11-01", "2026-11-01"))

			var statusErr *StatusError
			if !errors.As(err, &statusErr) {
				t.Fatalf("error = %v, want a *StatusError", err)
			}
			if statusErr.Collection != sleepCollection || statusErr.StatusCode != tc.status || statusErr.Detail != tc.wantDetail {
				t.Errorf("error = %+v, want sleep, %d, %q", statusErr, tc.status, tc.wantDetail)
			}
			if !strings.Contains(err.Error(), "oura sleep") || !strings.Contains(err.Error(), tc.wantDetail) {
				t.Errorf("message %q does not say what failed", err.Error())
			}
		})
	}
}

func TestClientTimesOutWhenOuraDoesNotAnswer(t *testing.T) {
	oura := &fakeOura{respond: func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	}}
	client := newTestClient(oura)
	client.timeout = 10 * time.Millisecond

	_, err := client.Sleep(t.Context(), mustDays(t, "2026-11-01", "2026-11-01"))

	if !errors.Is(err, ErrTimeout) {
		t.Errorf("error = %v, want %v", err, ErrTimeout)
	}
	if !strings.Contains(err.Error(), "oura sleep") {
		t.Errorf("message %q does not name the collection", err.Error())
	}
}

// stalledBody is a response body that blocks until its request's context
// ends, as a body does when Oura stops sending halfway.
type stalledBody struct {
	ctx context.Context
}

func (b stalledBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, fmt.Errorf("stalled: %w", b.ctx.Err())
}

func (stalledBody) Close() error { return nil }

func TestClientTimesOutWhenOuraStopsSendingTheAnswer(t *testing.T) {
	oura := &fakeOura{respond: func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: stalledBody{ctx: request.Context()}}, nil
	}}
	client := newTestClient(oura)
	client.timeout = 10 * time.Millisecond

	_, err := client.DailySleep(t.Context(), mustDays(t, "2026-11-01", "2026-11-01"))

	if !errors.Is(err, ErrTimeout) {
		t.Errorf("error = %v, want %v", err, ErrTimeout)
	}
}

func TestClientReportsACancelledCallAsCancelledNotTimedOut(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	oura := &fakeOura{respond: func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	}}

	_, err := newTestClient(oura).Sleep(ctx, mustDays(t, "2026-11-01", "2026-11-01"))

	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap %v", err, context.Canceled)
	}
	if errors.Is(err, ErrTimeout) {
		t.Errorf("error = %v, a cancelled call is not a timeout", err)
	}
}

func TestClientSurfacesAFailedConnection(t *testing.T) {
	refused := errors.New("connection refused")
	oura := &fakeOura{respond: func(*http.Request) (*http.Response, error) { return nil, refused }}

	_, err := newTestClient(oura).Sleep(t.Context(), mustDays(t, "2026-11-01", "2026-11-01"))

	if !errors.Is(err, refused) || !strings.Contains(err.Error(), "oura sleep") {
		t.Errorf("error = %v, want it to name the collection and wrap %v", err, refused)
	}
}

func TestClientSurfacesAnAnswerThatIsNotJSON(t *testing.T) {
	_, err := newTestClient(answering(http.StatusOK, `<html>gateway</html>`)).
		DailySleep(t.Context(), mustDays(t, "2026-11-01", "2026-11-01"))

	if err == nil || !strings.Contains(err.Error(), "oura daily_sleep: read the answer") {
		t.Errorf("error = %v, want one saying the daily_sleep answer could not be read", err)
	}
	if errors.Is(err, ErrTimeout) {
		t.Errorf("error = %v, a malformed answer is not a timeout", err)
	}
}

func TestClientSurfacesAFailureToGetTheAccessToken(t *testing.T) {
	noToken := errors.New("no token stored")
	oura := answering(http.StatusOK, `{"data": [], "next_token": null}`)
	client := NewClient(testBaseURL, fixedToken{err: noToken}, oura)

	_, err := client.Sleep(t.Context(), mustDays(t, "2026-11-01", "2026-11-01"))

	if !errors.Is(err, noToken) || !strings.Contains(err.Error(), "access token") {
		t.Errorf("error = %v, want it to say the access token failed and wrap %v", err, noToken)
	}
	if len(oura.requests) != 0 {
		t.Errorf("sent %d requests without a token, want none", len(oura.requests))
	}
}

func TestClientSurfacesABaseURLItCannotUse(t *testing.T) {
	client := NewClient("http://bad host", fixedToken{token: testAccessToken}, answering(http.StatusOK, `{}`))

	_, err := client.Sleep(t.Context(), mustDays(t, "2026-11-01", "2026-11-01"))

	if err == nil || !strings.Contains(err.Error(), "build the request") {
		t.Errorf("error = %v, want one saying the request could not be built", err)
	}
}

func TestSummariesReadsBothCollectionsAndSummarizesThem(t *testing.T) {
	oura := byCollection(map[string]string{
		"daily_sleep": `{"data": [{"day": "2026-11-01", "score": 82}], "next_token": null}`,
		"sleep": `{"data": [
			{"day": "2026-11-01", "type": "long_sleep", "total_sleep_duration": 25200, "lowest_heart_rate": 52, "average_hrv": 41.6},
			{"day": "2026-11-01", "type": "late_nap", "total_sleep_duration": 1800, "lowest_heart_rate": 47, "average_hrv": 70}
		], "next_token": null}`,
	})

	summaries, err := newTestClient(oura).Summaries(t.Context(), mustDays(t, "2026-11-01", "2026-11-02"))
	if err != nil {
		t.Fatal(err)
	}

	if len(summaries) != 2 {
		t.Fatalf("got %d summaries, want 2", len(summaries))
	}
	assertReading(t, "SleepScore", summaries[0].SleepScore, intPointer(82))
	assertReading(t, "LowestHeartRate", summaries[0].LowestHeartRate, intPointer(52))
	assertReading(t, "AverageHRV", summaries[0].AverageHRV, intPointer(42))
	assertReading(t, "unsynced SleepScore", summaries[1].SleepScore, nil)
	assertReading(t, "unsynced AverageHRV", summaries[1].AverageHRV, nil)
}

func TestSummariesSurfacesAFailureInEitherCollection(t *testing.T) {
	for _, failing := range []string{dailySleepCollection, sleepCollection} {
		t.Run(failing, func(t *testing.T) {
			bodies := map[string]string{
				dailySleepCollection: `{"data": [], "next_token": null}`,
				sleepCollection:      `{"data": [], "next_token": null}`,
			}
			delete(bodies, failing)

			_, err := newTestClient(byCollection(bodies)).Summaries(t.Context(), mustDays(t, "2026-11-01", "2026-11-01"))

			var statusErr *StatusError
			if !errors.As(err, &statusErr) || statusErr.Collection != failing {
				t.Errorf("error = %v, want a *StatusError for %s", err, failing)
			}
		})
	}
}
