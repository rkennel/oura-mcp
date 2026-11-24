package fakeoura

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

var testClient = Client{ID: "test-client", Secret: "test-client-secret"}

// newTestHandler returns the fake's handler over a fresh State, and the
// State, so a test can set it up directly.
func newTestHandler() (http.Handler, *State) {
	state := NewState()
	return NewHandler(state, testClient), state
}

// serve sends request to handler and returns the recorded answer.
func serve(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// tokenRequest builds a request to the token endpoint with form in its body.
func tokenRequest(t *testing.T, form url.Values) *http.Request {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, TokenPath, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

// refreshForm is the form a client sends to refresh, with its credentials.
func refreshForm(refreshToken string) url.Values {
	return url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {testClient.ID},
		"client_secret": {testClient.Secret},
	}
}

// collectionRequest builds a request for path with the query and, unless
// accessToken is empty, a bearer token.
func collectionRequest(t *testing.T, path, query, accessToken string) *http.Request {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path+"?"+query, nil)
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	return request
}

func controlRequest(t *testing.T, path, body string) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
}

func decode[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(recorder.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body, err)
	}
	return value
}

func assertStatus(t *testing.T, recorder *httptest.ResponseRecorder, want int) {
	t.Helper()
	if recorder.Code != want {
		t.Fatalf("status = %d, want %d; body %s", recorder.Code, want, recorder.Body)
	}
}

func TestHealthAnswersOK(t *testing.T) {
	handler, _ := newTestHandler()

	recorder := serve(handler, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", nil))

	assertStatus(t, recorder, http.StatusOK)
	if recorder.Body.String() != "ok\n" {
		t.Errorf("body = %q, want %q", recorder.Body, "ok\n")
	}
}

func TestTokenEndpointRefreshesInOurasForm(t *testing.T) {
	handler, state := newTestHandler()
	issued := state.IssueTokens()

	recorder := serve(handler, tokenRequest(t, refreshForm(issued.RefreshToken)))

	assertStatus(t, recorder, http.StatusOK)
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	answer := decode[tokenResponse](t, recorder)
	if answer.TokenType != "bearer" || answer.ExpiresIn != accessTokenLifetime {
		t.Errorf("answer = %+v, want a bearer token that lasts %d seconds", answer, accessTokenLifetime)
	}
	if answer.RefreshToken == "" || answer.RefreshToken == issued.RefreshToken {
		t.Errorf("refresh_token = %q, want a new one", answer.RefreshToken)
	}
	if !state.Authorized(answer.AccessToken) {
		t.Error("the returned access token is not authorized")
	}
}

func TestTokenEndpointRejectsARefreshTokenUsedBefore(t *testing.T) {
	handler, state := newTestHandler()
	issued := state.IssueTokens()
	assertStatus(t, serve(handler, tokenRequest(t, refreshForm(issued.RefreshToken))), http.StatusOK)

	recorder := serve(handler, tokenRequest(t, refreshForm(issued.RefreshToken)))

	assertStatus(t, recorder, http.StatusBadRequest)
	if answer := decode[oauthError](t, recorder); answer.Error != "invalid_grant" {
		t.Errorf("error = %q, want invalid_grant", answer.Error)
	}
}

func TestTokenEndpointAcceptsClientCredentialsInBasicAuth(t *testing.T) {
	handler, state := newTestHandler()
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {state.IssueTokens().RefreshToken}}
	request := tokenRequest(t, form)
	request.SetBasicAuth(testClient.ID, testClient.Secret)

	assertStatus(t, serve(handler, request), http.StatusOK)
}

func TestTokenEndpointRejectsAWrongClientWithoutSpendingTheToken(t *testing.T) {
	cases := map[string]func(url.Values){
		"wrong secret":   func(form url.Values) { form.Set("client_secret", "wrong") },
		"wrong id":       func(form url.Values) { form.Set("client_id", "someone-else") },
		"no credentials": func(form url.Values) { form.Del("client_id"); form.Del("client_secret") },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			handler, state := newTestHandler()
			issued := state.IssueTokens()
			form := refreshForm(issued.RefreshToken)
			change(form)

			recorder := serve(handler, tokenRequest(t, form))

			assertStatus(t, recorder, http.StatusUnauthorized)
			if answer := decode[oauthError](t, recorder); answer.Error != "invalid_client" {
				t.Errorf("error = %q, want invalid_client", answer.Error)
			}
			if _, err := state.Refresh(issued.RefreshToken); err != nil {
				t.Errorf("the refresh token was spent by a rejected request: %v", err)
			}
		})
	}
}

func TestTokenEndpointRejectsOtherGrants(t *testing.T) {
	handler, _ := newTestHandler()
	form := refreshForm("")
	form.Set("grant_type", "authorization_code")

	recorder := serve(handler, tokenRequest(t, form))

	assertStatus(t, recorder, http.StatusBadRequest)
	if answer := decode[oauthError](t, recorder); answer.Error != "unsupported_grant_type" {
		t.Errorf("error = %q, want unsupported_grant_type", answer.Error)
	}
}

func TestTokenEndpointRejectsABodyThatIsNotAForm(t *testing.T) {
	handler, _ := newTestHandler()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, TokenPath, strings.NewReader("%zz"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	recorder := serve(handler, request)

	assertStatus(t, recorder, http.StatusBadRequest)
	if answer := decode[oauthError](t, recorder); answer.Error != "invalid_request" {
		t.Errorf("error = %q, want invalid_request", answer.Error)
	}
}

func TestCollectionEndpointsServeTheSeededDocumentsInRange(t *testing.T) {
	for path, collection := range map[string]string{DailySleepPath: DailySleep, SleepPath: Sleep} {
		t.Run(collection, func(t *testing.T) {
			handler, state := newTestHandler()
			mustSeed(t, state, map[string][]json.RawMessage{collection: documentsOf(
				`{"day":"2026-11-01","score":80}`,
				`{"day":"2026-11-05","score":60}`,
			)})

			recorder := serve(handler, collectionRequest(t, path, "start_date=2026-11-01&end_date=2026-11-02", state.IssueTokens().AccessToken))

			assertStatus(t, recorder, http.StatusOK)
			want := `{"data":[{"day":"2026-11-01","score":80}],"next_token":null}` + "\n"
			if recorder.Body.String() != want {
				t.Errorf("body = %s, want %s", recorder.Body, want)
			}
		})
	}
}

func TestCollectionEndpointAnswersAnEmptyListWhenNothingIsSeeded(t *testing.T) {
	handler, state := newTestHandler()

	recorder := serve(handler, collectionRequest(t, SleepPath, "start_date=2026-11-01&end_date=2026-11-02", state.IssueTokens().AccessToken))

	assertStatus(t, recorder, http.StatusOK)
	if want := `{"data":[],"next_token":null}` + "\n"; recorder.Body.String() != want {
		t.Errorf("body = %s, want %s", recorder.Body, want)
	}
}

func TestCollectionEndpointRequiresACurrentAccessToken(t *testing.T) {
	handler, state := newTestHandler()
	expired := state.IssueTokens()
	state.ExpireAccessTokens()

	cases := map[string]func(*http.Request){
		"no token":        func(*http.Request) {},
		"unknown token":   func(r *http.Request) { r.Header.Set("Authorization", "Bearer never-issued") },
		"expired token":   func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+expired.AccessToken) },
		"refresh token":   func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+expired.RefreshToken) },
		"not bearer":      func(r *http.Request) { r.SetBasicAuth(testClient.ID, testClient.Secret) },
		"scheme only":     func(r *http.Request) { r.Header.Set("Authorization", "Bearer") },
		"lowercase token": func(r *http.Request) { r.Header.Set("Authorization", "bearer never-issued") },
	}
	for name, authorize := range cases {
		t.Run(name, func(t *testing.T) {
			request := collectionRequest(t, DailySleepPath, "start_date=2026-11-01&end_date=2026-11-02", "")
			authorize(request)

			assertStatus(t, serve(handler, request), http.StatusUnauthorized)
		})
	}
}

func TestCollectionEndpointAcceptsTheBearerSchemeInAnyCase(t *testing.T) {
	handler, state := newTestHandler()
	request := collectionRequest(t, SleepPath, "start_date=2026-11-01&end_date=2026-11-02", "")
	request.Header.Set("Authorization", "bearer "+state.IssueTokens().AccessToken)

	assertStatus(t, serve(handler, request), http.StatusOK)
}

func TestCollectionEndpointRequiresBothDates(t *testing.T) {
	handler, state := newTestHandler()
	token := state.IssueTokens().AccessToken

	for name, query := range map[string]string{
		"no dates":        "",
		"no end_date":     "start_date=2026-11-01",
		"no start_date":   "end_date=2026-11-01",
		"malformed start": "start_date=11/01/2026&end_date=2026-11-02",
		"malformed end":   "start_date=2026-11-01&end_date=tomorrow",
	} {
		t.Run(name, func(t *testing.T) {
			recorder := serve(handler, collectionRequest(t, SleepPath, query, token))

			assertStatus(t, recorder, http.StatusBadRequest)
			if answer := decode[detail](t, recorder); !strings.Contains(answer.Detail, "_date") {
				t.Errorf("detail = %q, want it to name the date", answer.Detail)
			}
		})
	}
}

func TestOuraEndpointsAnswerWithAPendingFailureFirst(t *testing.T) {
	handler, state := newTestHandler()
	issued := state.IssueTokens()
	assertStatus(t, serve(handler, controlRequest(t, FailuresPath,
		`{"endpoint":"/oauth/token","status":503,"count":1}`)), http.StatusNoContent)

	failed := serve(handler, tokenRequest(t, refreshForm(issued.RefreshToken)))

	assertStatus(t, failed, http.StatusServiceUnavailable)
	if answer := decode[detail](t, failed); answer.Detail != "simulated failure" {
		t.Errorf("detail = %q, want %q", answer.Detail, "simulated failure")
	}
	// The failed request never reached the token endpoint, so the refresh
	// token is still unused.
	assertStatus(t, serve(handler, tokenRequest(t, refreshForm(issued.RefreshToken))), http.StatusOK)
}

func TestFailuresControlDefaultsToOneFailure(t *testing.T) {
	handler, state := newTestHandler()
	token := state.IssueTokens().AccessToken
	request := func() *http.Request {
		return collectionRequest(t, SleepPath, "start_date=2026-11-01&end_date=2026-11-02", token)
	}
	assertStatus(t, serve(handler, controlRequest(t, FailuresPath,
		`{"endpoint":"/v2/usercollection/sleep","status":429}`)), http.StatusNoContent)

	assertStatus(t, serve(handler, request()), http.StatusTooManyRequests)
	assertStatus(t, serve(handler, request()), http.StatusOK)
}

func TestFailuresControlRejectsBadRequests(t *testing.T) {
	for name, body := range map[string]string{
		"unknown endpoint": `{"endpoint":"/v2/usercollection/heartrate","status":500}`,
		"bad status":       `{"endpoint":"/oauth/token","status":200}`,
		"negative count":   `{"endpoint":"/oauth/token","status":500,"count":-1}`,
		"unknown field":    `{"endpoint":"/oauth/token","status":500,"times":2}`,
		"not JSON":         `fail please`,
	} {
		t.Run(name, func(t *testing.T) {
			handler, _ := newTestHandler()

			assertStatus(t, serve(handler, controlRequest(t, FailuresPath, body)), http.StatusBadRequest)
		})
	}
}

func TestSeedControlSeedsWhatTheCollectionsServe(t *testing.T) {
	handler, state := newTestHandler()
	token := state.IssueTokens().AccessToken

	assertStatus(t, serve(handler, controlRequest(t, SeedPath,
		`{"daily_sleep":[{"day":"2026-11-01","score":81}],"sleep":[{"day":"2026-11-01","average_hrv":42}]}`)),
		http.StatusNoContent)

	daily := serve(handler, collectionRequest(t, DailySleepPath, "start_date=2026-11-01&end_date=2026-11-01", token))
	if want := `{"data":[{"day":"2026-11-01","score":81}],"next_token":null}` + "\n"; daily.Body.String() != want {
		t.Errorf("daily_sleep body = %s, want %s", daily.Body, want)
	}
	sleep := serve(handler, collectionRequest(t, SleepPath, "start_date=2026-11-01&end_date=2026-11-01", token))
	if want := `{"data":[{"day":"2026-11-01","average_hrv":42}],"next_token":null}` + "\n"; sleep.Body.String() != want {
		t.Errorf("sleep body = %s, want %s", sleep.Body, want)
	}
}

func TestSeedControlRejectsBadRequests(t *testing.T) {
	for name, body := range map[string]string{
		"unknown collection":     `{"heart_rate":[{"day":"2026-11-01"}]}`,
		"document without a day": `{"sleep":[{"score":1}]}`,
		"not JSON":               `seed it`,
		"not an object":          `[{"day":"2026-11-01"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			handler, _ := newTestHandler()

			assertStatus(t, serve(handler, controlRequest(t, SeedPath, body)), http.StatusBadRequest)
		})
	}
}

func TestResetControlEmptiesTheFake(t *testing.T) {
	handler, state := newTestHandler()
	issued := state.IssueTokens()
	mustSeed(t, state, map[string][]json.RawMessage{Sleep: documentsOf(`{"day":"2026-11-01"}`)})

	assertStatus(t, serve(handler, controlRequest(t, ResetPath, "")), http.StatusNoContent)

	if state.Authorized(issued.AccessToken) {
		t.Error("the access token survived a reset")
	}
	fresh := state.IssueTokens().AccessToken
	recorder := serve(handler, collectionRequest(t, SleepPath, "start_date=2026-11-01&end_date=2026-11-01", fresh))
	if want := `{"data":[],"next_token":null}` + "\n"; recorder.Body.String() != want {
		t.Errorf("body after reset = %s, want %s", recorder.Body, want)
	}
}

func TestIssueTokensControlReturnsAUsablePair(t *testing.T) {
	handler, state := newTestHandler()

	recorder := serve(handler, controlRequest(t, IssueTokensPath, ""))

	assertStatus(t, recorder, http.StatusOK)
	answer := decode[tokenResponse](t, recorder)
	if answer.TokenType != "bearer" || !state.Authorized(answer.AccessToken) {
		t.Errorf("answer = %+v, want an authorized bearer token", answer)
	}
	if _, err := state.Refresh(answer.RefreshToken); err != nil {
		t.Errorf("the issued refresh token does not refresh: %v", err)
	}
}

func TestExpireAccessTokensControlExpiresThem(t *testing.T) {
	handler, state := newTestHandler()
	issued := state.IssueTokens()

	assertStatus(t, serve(handler, controlRequest(t, ExpireAccessTokensPath, "")), http.StatusNoContent)

	if state.Authorized(issued.AccessToken) {
		t.Error("the access token is still authorized")
	}
}

func TestControlsAcceptOnlyPost(t *testing.T) {
	handler, _ := newTestHandler()

	for _, path := range []string{ResetPath, SeedPath, IssueTokensPath, ExpireAccessTokensPath, FailuresPath} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)

			assertStatus(t, serve(handler, request), http.StatusMethodNotAllowed)
		})
	}
}
