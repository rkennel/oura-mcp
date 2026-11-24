package fakeoura

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Oura's paths, which the fake serves as Oura does.
const (
	TokenPath      = "/oauth/token" //nolint:gosec // G101: a URL path, not a credential.
	DailySleepPath = "/v2/usercollection/" + DailySleep
	SleepPath      = "/v2/usercollection/" + Sleep
)

// The paths of the controls that end-to-end tests use to set the fake up.
const (
	ResetPath              = "/_control/reset"
	SeedPath               = "/_control/seed"
	IssueTokensPath        = "/_control/tokens"
	ExpireAccessTokensPath = "/_control/expire-access-tokens"
	FailuresPath           = "/_control/failures"
)

// ouraPaths are the Oura endpoints a test can make fail.
var ouraPaths = []string{TokenPath, DailySleepPath, SleepPath}

const (
	// accessTokenLifetime is the expires_in the fake reports, in seconds:
	// Oura's access tokens last a day. The fake never expires them on its
	// own; tests do that with the expire-access-tokens control.
	accessTokenLifetime = 24 * 60 * 60

	// maxBodyBytes caps every request body the fake reads.
	maxBodyBytes = 1 << 20
)

// Client is the OAuth client the fake accepts at its token endpoint.
type Client struct {
	ID     string
	Secret string
}

// tokenResponse is the body of a successful answer from the token
// endpoint, in Oura's form.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// oauthError is the body of a failed answer from the token endpoint.
type oauthError struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

// detail is the body of a failed answer from any other endpoint.
type detail struct {
	Detail string `json:"detail"`
}

// collectionResponse is the body of a successful answer from a collection
// endpoint. The fake returns every match at once, so next_token is null.
type collectionResponse struct {
	Data      []json.RawMessage `json:"data"`
	NextToken *string           `json:"next_token"`
}

// failureRequest is the body of a request to FailuresPath.
type failureRequest struct {
	Endpoint string `json:"endpoint"`
	Status   int    `json:"status"`
	Count    *int   `json:"count"`
}

// NewHandler returns the fake's routes, backed by state, with its token
// endpoint accepting only client.
func NewHandler(state *State, client Client) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)

	mux.Handle("POST "+TokenPath, failWhenAsked(state, TokenPath, handleToken(state, client)))
	mux.Handle("GET "+DailySleepPath, failWhenAsked(state, DailySleepPath, handleCollection(state, DailySleep)))
	mux.Handle("GET "+SleepPath, failWhenAsked(state, SleepPath, handleCollection(state, Sleep)))

	mux.HandleFunc("POST "+ResetPath, handleReset(state))
	mux.HandleFunc("POST "+SeedPath, handleSeed(state))
	mux.HandleFunc("POST "+IssueTokensPath, handleIssueTokens(state))
	mux.HandleFunc("POST "+ExpireAccessTokensPath, handleExpireAccessTokens(state))
	mux.HandleFunc("POST "+FailuresPath, handleFailures(state))
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

// failWhenAsked answers with a simulated failure while one is pending for
// path, before next sees the request, and otherwise passes it to next.
func failWhenAsked(state *State, path string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status, fail := state.takeFailure(path); fail {
			writeJSON(w, status, detail{Detail: "simulated failure"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleToken serves Oura's token endpoint for the refresh_token grant.
// The client authenticates with HTTP Basic or with client_id and
// client_secret in the form, as OAuth allows.
func handleToken(state *State, client Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		if err := r.ParseForm(); err != nil {
			writeJSON(w, http.StatusBadRequest, oauthError{"invalid_request", "the body is not a valid form"})
			return
		}
		if !client.presentedIn(r) {
			writeJSON(w, http.StatusUnauthorized, oauthError{"invalid_client", "unknown client or wrong secret"})
			return
		}
		if grant := r.PostForm.Get("grant_type"); grant != "refresh_token" {
			writeJSON(w, http.StatusBadRequest, oauthError{"unsupported_grant_type", "the fake supports only refresh_token"})
			return
		}
		tokens, err := state.Refresh(r.PostForm.Get("refresh_token"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, oauthError{"invalid_grant", err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, newTokenResponse(tokens))
	}
}

// presentedIn reports whether r carries this client's credentials. The
// secret is compared in constant time, as a real server must.
func (c Client) presentedIn(r *http.Request) bool {
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	idMatches := subtle.ConstantTimeCompare([]byte(id), []byte(c.ID)) == 1
	secretMatches := subtle.ConstantTimeCompare([]byte(secret), []byte(c.Secret)) == 1
	return idMatches && secretMatches
}

// handleCollection serves an Oura collection endpoint: the documents of
// collection whose day is from start_date to end_date, both included, for
// a caller with a current access token.
func handleCollection(state *State, collection string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !state.Authorized(bearerToken(r)) {
			writeJSON(w, http.StatusUnauthorized, detail{Detail: "missing, invalid or expired access token"})
			return
		}
		start, end, err := dateRange(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, detail{Detail: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, collectionResponse{Data: state.Documents(collection, start, end)})
	}
}

// bearerToken returns the token in r's Authorization header, or "" if it
// has none.
func bearerToken(r *http.Request) string {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return token
}

// dateRange reads start_date and end_date from r's query. Oura defaults
// them when they are missing; the fake requires both, so that a client
// relying on Oura's defaults fails loudly rather than by chance.
func dateRange(r *http.Request) (start, end time.Time, err error) {
	query := r.URL.Query()
	start, err = time.Parse(dayLayout, query.Get("start_date"))
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("start_date must be a date in YYYY-MM-DD form")
	}
	end, err = time.Parse(dayLayout, query.Get("end_date"))
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("end_date must be a date in YYYY-MM-DD form")
	}
	return start, end, nil
}

func handleReset(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		state.Reset()
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleSeed replaces the documents of each collection named in the body,
// a JSON object such as {"daily_sleep": [...], "sleep": [...]}. Nothing is
// seeded unless every collection in the body is valid.
func handleSeed(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var collections map[string][]json.RawMessage
		if err := decodeJSON(w, r, &collections); err != nil {
			writeJSON(w, http.StatusBadRequest, detail{Detail: err.Error()})
			return
		}
		if err := state.Seed(collections); err != nil {
			writeJSON(w, http.StatusBadRequest, detail{Detail: err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleIssueTokens issues a token pair as Oura does at the end of a login,
// and returns it in the token endpoint's form.
func handleIssueTokens(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, newTokenResponse(state.IssueTokens()))
	}
}

func handleExpireAccessTokens(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		state.ExpireAccessTokens()
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleFailures makes the next requests to an Oura endpoint fail. The body
// is a JSON object such as {"endpoint": "/v2/usercollection/sleep",
// "status": 503, "count": 2}; count is 1 when left out.
func handleFailures(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request failureRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeJSON(w, http.StatusBadRequest, detail{Detail: err.Error()})
			return
		}
		count := 1
		if request.Count != nil {
			count = *request.Count
		}
		if err := state.FailNext(request.Endpoint, request.Status, count); err != nil {
			writeJSON(w, http.StatusBadRequest, detail{Detail: err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func newTokenResponse(tokens Tokens) tokenResponse {
	return tokenResponse{
		AccessToken:  tokens.AccessToken,
		TokenType:    "bearer",
		ExpiresIn:    accessTokenLifetime,
		RefreshToken: tokens.RefreshToken,
	}
}

// decodeJSON reads r's body, at most maxBodyBytes of it, into v, refusing
// fields v does not have so that a typo in a test fails loudly.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("the body is not the JSON this control expects: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
