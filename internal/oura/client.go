// Package oura reads sleep data from the Oura API and turns it into the
// three values the tracker needs for each wake-up date: the sleep score,
// the lowest heart rate and the average HRV.
package oura

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultTimeout bounds each read of an Oura collection, every page
	// included, so a slow Oura gives a clear error rather than a hang.
	DefaultTimeout = 15 * time.Second

	// collectionPath is where Oura serves the user's collections.
	collectionPath = "/v2/usercollection/"

	// maxResponseBytes caps how much of one response the client reads.
	maxResponseBytes = 10 << 20

	// maxErrorBytes caps how much of an error response the client reads
	// for its detail.
	maxErrorBytes = 4 << 10

	// maxPages stops a read whose next_token never runs out.
	maxPages = 100
)

// ErrTimeout means Oura did not answer within the client's timeout.
var ErrTimeout = errors.New("oura did not answer in time")

var errTooManyPages = fmt.Errorf("oura kept returning a next_token after %d pages", maxPages)

// TokenSource supplies the access token for Oura requests. Keeping and
// refreshing the token is its job, not the client's.
type TokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

// Doer sends HTTP requests. *http.Client is one.
type Doer interface {
	Do(request *http.Request) (*http.Response, error)
}

// StatusError is Oura answering with an error status, such as 401 for an
// expired access token or 429 when rate limited.
type StatusError struct {
	// Collection is the Oura collection that was read, such as "sleep".
	Collection string
	// StatusCode is the HTTP status Oura answered with.
	StatusCode int
	// Detail is Oura's explanation, or the status text when it gave none.
	Detail string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("oura %s: HTTP %d: %s", e.Collection, e.StatusCode, e.Detail)
}

// Client reads the user's sleep collections from the Oura API.
type Client struct {
	baseURL string
	tokens  TokenSource
	http    Doer
	timeout time.Duration
}

// NewClient returns a client for the Oura API at baseURL, such as
// "https://api.ouraring.com", that authenticates with tokens and sends its
// requests with httpClient.
func NewClient(baseURL string, tokens TokenSource, httpClient Doer) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		tokens:  tokens,
		http:    httpClient,
		timeout: DefaultTimeout,
	}
}

// Summaries reads both sleep collections for days and summarizes them.
func (c *Client) Summaries(ctx context.Context, days DayRange) ([]Summary, error) {
	daily, err := c.DailySleep(ctx, days)
	if err != nil {
		return nil, err
	}
	sleeps, err := c.Sleep(ctx, days)
	if err != nil {
		return nil, err
	}
	return Summarize(days, daily, sleeps), nil
}

// DailySleep returns the daily_sleep documents, which carry the sleep
// score, whose day is in days.
func (c *Client) DailySleep(ctx context.Context, days DayRange) ([]DailySleep, error) {
	return readCollection[DailySleep](ctx, c, dailySleepCollection, days)
}

// Sleep returns the sleep documents, one for each sleep Oura detected,
// naps included, whose day is in days.
func (c *Client) Sleep(ctx context.Context, days DayRange) ([]SleepPeriod, error) {
	return readCollection[SleepPeriod](ctx, c, sleepCollection, days)
}

// dated is a document Oura dates with a day.
type dated interface {
	day() string
}

// page is one answer from a collection endpoint.
type page[T dated] struct {
	Data      []T     `json:"data"`
	NextToken *string `json:"next_token"`
}

// readCollection returns every document of collection whose day is in
// days, following next_token through the pages.
func readCollection[T dated](ctx context.Context, c *Client, collection string, days DayRange) ([]T, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	documents := []T{}
	nextToken := ""
	for range maxPages {
		answer, err := readPage[T](ctx, c, collection, days, nextToken)
		if err != nil {
			return nil, err
		}
		documents = append(documents, inRange(answer.Data, days)...)
		if answer.NextToken == nil || *answer.NextToken == "" {
			return documents, nil
		}
		nextToken = *answer.NextToken
	}
	return nil, fmt.Errorf("oura %s: %w", collection, errTooManyPages)
}

// readPage reads one page of collection.
func readPage[T dated](ctx context.Context, c *Client, collection string, days DayRange, nextToken string) (page[T], error) {
	request, err := c.newRequest(ctx, collection, days, nextToken)
	if err != nil {
		return page[T]{}, err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return page[T]{}, c.sendError(ctx, collection, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return page[T]{}, statusError(collection, response)
	}
	var answer page[T]
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&answer); err != nil {
		return page[T]{}, c.readError(ctx, collection, err)
	}
	return answer, nil
}

// newRequest builds the request for one page of collection, authenticated
// with the current access token.
func (c *Client) newRequest(ctx context.Context, collection string, days DayRange, nextToken string) (*http.Request, error) {
	token, err := c.tokens.AccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("oura %s: get the access token: %w", collection, err)
	}

	start, end := days.queryBounds()
	query := url.Values{"start_date": {start}, "end_date": {end}}
	if nextToken != "" {
		query.Set("next_token", nextToken)
	}
	address := c.baseURL + collectionPath + collection + "?" + query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("oura %s: build the request: %w", collection, err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	return request, nil
}

// sendError explains why a request got no answer, naming a timeout as
// ErrTimeout.
func (c *Client) sendError(ctx context.Context, collection string, err error) error {
	if timedOut(ctx) {
		return fmt.Errorf("oura %s: no answer within %s: %w", collection, c.timeout, ErrTimeout)
	}
	return fmt.Errorf("oura %s: send the request: %w", collection, err)
}

// readError explains why an answer could not be read, naming a timeout as
// ErrTimeout.
func (c *Client) readError(ctx context.Context, collection string, err error) error {
	if timedOut(ctx) {
		return fmt.Errorf("oura %s: answer not read within %s: %w", collection, c.timeout, ErrTimeout)
	}
	return fmt.Errorf("oura %s: read the answer: %w", collection, err)
}

// timedOut reports whether ctx ended because its deadline passed, rather
// than because the caller cancelled it.
func timedOut(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.DeadlineExceeded)
}

// statusError returns a StatusError for response, with Oura's detail when
// it gave one as text.
func statusError(collection string, response *http.Response) *StatusError {
	var body struct {
		Detail string `json:"detail"`
	}
	detail := http.StatusText(response.StatusCode)
	if err := json.NewDecoder(io.LimitReader(response.Body, maxErrorBytes)).Decode(&body); err == nil && body.Detail != "" {
		detail = body.Detail
	}
	return &StatusError{Collection: collection, StatusCode: response.StatusCode, Detail: detail}
}

// inRange returns the documents whose day is in days.
func inRange[T dated](documents []T, days DayRange) []T {
	var kept []T
	for _, document := range documents {
		if days.contains(document.day()) {
			kept = append(kept, document)
		}
	}
	return kept
}
