package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serve sends a request for method and path through the server's handler,
// with key in the x-api-key header unless key is empty.
func serve(t *testing.T, method, path, key string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	if key != "" {
		request.Header.Set(apiKeyHeader, key)
	}
	recorder := httptest.NewRecorder()
	NewHandler(testAPIKey).ServeHTTP(recorder, request)
	return recorder
}

func TestHealthReportsOKWithoutAKey(t *testing.T) {
	recorder := serve(t, http.MethodGet, "/health", "")

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got, want := recorder.Body.String(), "ok\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestOtherMethodsOnHealthNeedTheKey(t *testing.T) {
	recorder := serve(t, http.MethodPost, "/health", "")

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestEveryOtherPathNeedsTheKey(t *testing.T) {
	for _, path := range []string{"/", "/mcp", "/no-such-path", "/health/extra"} {
		t.Run(path, func(t *testing.T) {
			recorder := serve(t, http.MethodGet, path, "")

			if recorder.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestAWrongKeyIsRejected(t *testing.T) {
	recorder := serve(t, http.MethodGet, "/no-such-path", "wrong-key")

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestTheCorrectKeyReachesTheRoutes(t *testing.T) {
	recorder := serve(t, http.MethodGet, "/no-such-path", testAPIKey)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d: the key was accepted, so routing should answer", recorder.Code, http.StatusNotFound)
	}
}

func TestTheRejectionDoesNotEchoTheKey(t *testing.T) {
	recorder := serve(t, http.MethodGet, "/no-such-path", "presented-key-value")

	if strings.Contains(recorder.Body.String(), "presented-key-value") {
		t.Errorf("body %q echoes the presented key", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), testAPIKey) {
		t.Errorf("body %q contains the expected key", recorder.Body.String())
	}
}
