//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The OAuth client the fake Oura API accepts, as e2e/compose.yaml sets it.
const (
	fakeOuraClientID     = "e2e-client"
	fakeOuraClientSecret = "e2e-client-secret" //nolint:gosec // G101: a made-up value for the fake.
)

// fakeOuraTokens is a token pair from the fake, in Oura's form.
type fakeOuraTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// callFakeOura sends a request to the fake Oura API and returns the
// response status and body. It fails the test if the request fails.
func callFakeOura(t *testing.T, method, path string, body io.Reader, header http.Header) (status int, content []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, fakeOuraURL()+path, body)
	if err != nil {
		t.Fatalf("build request for %s: %v", path, err)
	}
	for name, values := range header {
		request.Header[name] = values
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer closeBody(t, response)
	content, err = io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body of %s: %v", path, err)
	}
	return response.StatusCode, content
}

// controlFakeOura posts body to one of the fake's /_control/ endpoints and
// fails the test unless it succeeds.
func controlFakeOura(t *testing.T, path, body string) []byte {
	t.Helper()
	header := http.Header{"Content-Type": {"application/json"}}
	status, content := callFakeOura(t, http.MethodPost, path, strings.NewReader(body), header)
	if status != http.StatusOK && status != http.StatusNoContent {
		t.Fatalf("POST %s: status %d, body %s", path, status, content)
	}
	return content
}

// resetFakeOura empties the fake, so a test starts from a known state.
func resetFakeOura(t *testing.T) {
	t.Helper()
	controlFakeOura(t, "/_control/reset", "")
}

// issueFakeOuraTokens has the fake issue a token pair, as a login would.
func issueFakeOuraTokens(t *testing.T) fakeOuraTokens {
	t.Helper()
	var tokens fakeOuraTokens
	if err := json.Unmarshal(controlFakeOura(t, "/_control/tokens", ""), &tokens); err != nil {
		t.Fatalf("decode issued tokens: %v", err)
	}
	return tokens
}

// refreshAtFakeOura presents refreshToken to the fake's token endpoint the
// way an OAuth client does, and returns the status and body.
func refreshAtFakeOura(t *testing.T, refreshToken string) (int, []byte) {
	t.Helper()
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {fakeOuraClientID},
		"client_secret": {fakeOuraClientSecret},
	}
	header := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
	return callFakeOura(t, http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()), header)
}

// sleepDay is the day the tests seed and ask the fake for.
const sleepDay = "2026-11-01"

// getFakeOuraSleep asks the fake's sleep collection for sleepDay with
// accessToken, and returns the status and body.
func getFakeOuraSleep(t *testing.T, accessToken string) (int, string) {
	t.Helper()
	path := "/v2/usercollection/sleep?start_date=" + sleepDay + "&end_date=" + sleepDay
	header := http.Header{"Authorization": {"Bearer " + accessToken}}
	status, content := callFakeOura(t, http.MethodGet, path, nil, header)
	return status, string(content)
}

func TestFakeOuraRejectsARefreshTokenThatWasAlreadyUsed(t *testing.T) {
	resetFakeOura(t)
	issued := issueFakeOuraTokens(t)
	if status, body := refreshAtFakeOura(t, issued.RefreshToken); status != http.StatusOK {
		t.Fatalf("first refresh: status %d, body %s", status, body)
	}

	status, body := refreshAtFakeOura(t, issued.RefreshToken)

	if status != http.StatusBadRequest || !strings.Contains(string(body), "invalid_grant") {
		t.Errorf("second refresh: status %d, body %s; want 400 invalid_grant", status, body)
	}
}

func TestFakeOuraServesSeededDataAndNoneAfterAReset(t *testing.T) {
	resetFakeOura(t)
	controlFakeOura(t, "/_control/seed", `{"sleep":[{"day":"2026-11-01","average_hrv":42,"lowest_heart_rate":51}]}`)
	token := issueFakeOuraTokens(t).AccessToken

	status, body := getFakeOuraSleep(t, token)
	want := `{"data":[{"day":"2026-11-01","average_hrv":42,"lowest_heart_rate":51}],"next_token":null}` + "\n"
	if status != http.StatusOK || body != want {
		t.Errorf("after seeding: status %d, body %s; want 200 %s", status, body, want)
	}

	resetFakeOura(t)
	token = issueFakeOuraTokens(t).AccessToken

	status, body = getFakeOuraSleep(t, token)
	if want := `{"data":[],"next_token":null}` + "\n"; status != http.StatusOK || body != want {
		t.Errorf("after a reset: status %d, body %s; want 200 %s", status, body, want)
	}
}

func TestFakeOuraExpiresAccessTokensButNotRefreshTokens(t *testing.T) {
	resetFakeOura(t)
	issued := issueFakeOuraTokens(t)

	controlFakeOura(t, "/_control/expire-access-tokens", "")

	if status, _ := getFakeOuraSleep(t, issued.AccessToken); status != http.StatusUnauthorized {
		t.Errorf("with an expired access token: status %d, want %d", status, http.StatusUnauthorized)
	}
	if status, body := refreshAtFakeOura(t, issued.RefreshToken); status != http.StatusOK {
		t.Errorf("refresh after expiry: status %d, body %s; want 200", status, body)
	}
}

func TestFakeOuraSimulatesAFailure(t *testing.T) {
	resetFakeOura(t)
	token := issueFakeOuraTokens(t).AccessToken

	controlFakeOura(t, "/_control/failures", `{"endpoint":"/v2/usercollection/sleep","status":503,"count":1}`)

	if status, _ := getFakeOuraSleep(t, token); status != http.StatusServiceUnavailable {
		t.Errorf("first request: status %d, want %d", status, http.StatusServiceUnavailable)
	}
	if status, _ := getFakeOuraSleep(t, token); status != http.StatusOK {
		t.Errorf("second request: status %d, want %d", status, http.StatusOK)
	}
}
