// Package fakeoura is a stand-in for the Oura API, run as its own container
// by the end-to-end environment so that tests never touch the real Oura.
//
// It serves the parts of Oura the server uses: the OAuth token endpoint and
// the daily_sleep and sleep collections. Like Oura, it accepts each refresh
// token only once, so a server that reuses one fails its tests. Endpoints
// under /_control/ let a test seed data, issue and expire tokens, and make
// the Oura endpoints fail.
//
// It is a test double. It holds no real data, is never deployed, and its
// control endpoints are deliberately open: the end-to-end environment
// publishes it on the loopback interface only.
package fakeoura

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
)

// The collections the fake serves, named as in Oura's paths.
const (
	DailySleep = "daily_sleep"
	Sleep      = "sleep"
)

// dayLayout is how Oura writes a day, and how start_date and end_date are
// given.
const dayLayout = time.DateOnly

var (
	// ErrRefreshTokenRejected means the refresh token was never issued or
	// has already been used.
	ErrRefreshTokenRejected = errors.New("refresh token is unknown or already used")
	errUnknownCollection    = errors.New("unknown collection")
	errDocumentDay          = errors.New(`each document must be a JSON object with a "day" in YYYY-MM-DD form`)
	errUnknownEndpoint      = errors.New("not an Oura endpoint the fake serves")
	errFailureStatus        = errors.New("status must be between 400 and 599")
	errFailureCount         = errors.New("count must not be negative")
)

// Tokens is a pair the fake issued: an access token for the data endpoints
// and a refresh token that can be exchanged once for a new pair.
type Tokens struct {
	AccessToken  string
	RefreshToken string
}

// document is one seeded Oura document. The fake keeps the JSON exactly as
// seeded, so tests control every field Oura would return, and reads only
// its day.
type document struct {
	day  string
	body json.RawMessage
}

// State is everything the fake remembers. It is safe for concurrent use.
type State struct {
	mu            sync.Mutex
	accessTokens  map[string]bool
	refreshTokens map[string]bool
	documents     map[string][]document
	failures      map[string]failure
}

// failure is a simulated error that the next count requests to an endpoint
// get instead of an answer.
type failure struct {
	status int
	count  int
}

// NewState returns an empty State: no tokens, no data and no failures.
func NewState() *State {
	state := &State{}
	state.Reset()
	return state
}

// Reset forgets every token, document and pending failure.
func (s *State) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accessTokens = map[string]bool{}
	s.refreshTokens = map[string]bool{}
	s.documents = map[string][]document{}
	s.failures = map[string]failure{}
}

// IssueTokens issues a new token pair, as Oura does when a user completes
// the login.
func (s *State) IssueTokens() Tokens {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.issueTokens()
}

// issueTokens is IssueTokens for a caller that holds the lock.
func (s *State) issueTokens() Tokens {
	tokens := Tokens{AccessToken: rand.Text(), RefreshToken: rand.Text()}
	s.accessTokens[tokens.AccessToken] = true
	s.refreshTokens[tokens.RefreshToken] = true
	return tokens
}

// Refresh exchanges refreshToken for a new pair and spends it, so that
// presenting it again fails with ErrRefreshTokenRejected, as with Oura.
func (s *State) Refresh(refreshToken string) (Tokens, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.refreshTokens[refreshToken] {
		return Tokens{}, ErrRefreshTokenRejected
	}
	delete(s.refreshTokens, refreshToken)
	return s.issueTokens(), nil
}

// ExpireAccessTokens makes every access token issued so far invalid, as if
// it had reached its expiry. Refresh tokens stay usable.
func (s *State) ExpireAccessTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accessTokens = map[string]bool{}
}

// Authorized reports whether accessToken is a current access token.
func (s *State) Authorized(accessToken string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accessTokens[accessToken]
}

// Seed replaces the documents of each collection in collections, keeping
// their order; collections it does not name keep theirs. Each document must
// be a JSON object with a "day" in YYYY-MM-DD form. On an error nothing
// changes.
func (s *State) Seed(collections map[string][]json.RawMessage) error {
	parsed := make(map[string][]document, len(collections))
	for name, documents := range collections {
		if !isCollection(name) {
			return fmt.Errorf("%w %q", errUnknownCollection, name)
		}
		var err error
		if parsed[name], err = parseDocuments(name, documents); err != nil {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	maps.Copy(s.documents, parsed)
	return nil
}

// Documents returns the documents of collection whose day is from start to
// end, both included, in the order they were seeded.
func (s *State) Documents(collection string, start, end time.Time) []json.RawMessage {
	first, last := start.Format(dayLayout), end.Format(dayLayout)

	s.mu.Lock()
	defer s.mu.Unlock()
	matching := []json.RawMessage{}
	for _, document := range s.documents[collection] {
		// Days in YYYY-MM-DD form sort as strings in date order.
		if document.day >= first && document.day <= last {
			matching = append(matching, document.body)
		}
	}
	return matching
}

// FailNext makes the next count requests to the Oura endpoint path fail
// with status, replacing any failure already pending there. A count of
// zero clears it.
func (s *State) FailNext(path string, status, count int) error {
	if !slices.Contains(ouraPaths, path) {
		return fmt.Errorf("%q: %w", path, errUnknownEndpoint)
	}
	if status < 400 || status > 599 {
		return fmt.Errorf("%d: %w", status, errFailureStatus)
	}
	if count < 0 {
		return fmt.Errorf("%d: %w", count, errFailureCount)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[path] = failure{status: status, count: count}
	return nil
}

// takeFailure reports whether a request to path should fail, and with
// which status, using up one of its pending failures.
func (s *State) takeFailure(path string) (status int, fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.failures[path]
	if pending.count == 0 {
		return 0, false
	}
	pending.count--
	s.failures[path] = pending
	return pending.status, true
}

// parseDocuments reads the day of each document seeded into collection.
func parseDocuments(collection string, bodies []json.RawMessage) ([]document, error) {
	documents := make([]document, 0, len(bodies))
	for i, body := range bodies {
		day, err := dayOf(body)
		if err != nil {
			return nil, fmt.Errorf("%s document %d: %w", collection, i, err)
		}
		documents = append(documents, document{day: day, body: body})
	}
	return documents, nil
}

func isCollection(name string) bool {
	return name == DailySleep || name == Sleep
}

// dayOf returns the "day" of a seeded document.
func dayOf(body json.RawMessage) (string, error) {
	var fields struct {
		Day string `json:"day"`
	}
	if err := json.Unmarshal(body, &fields); err != nil {
		return "", errDocumentDay
	}
	if _, err := time.Parse(dayLayout, fields.Day); err != nil {
		return "", errDocumentDay
	}
	return fields.Day, nil
}
