package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const testAPIKey = "test-connector-key-0123456789"

// recordingHandler stands in for the protected routes and records whether a
// request reached it.
type recordingHandler struct {
	called bool
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.called = true
	w.WriteHeader(http.StatusOK)
}

// requestWithKey builds a GET request that sends key in the x-api-key header,
// or no header at all when key is empty.
func requestWithKey(t *testing.T, key string) *http.Request {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/protected", nil)
	if key != "" {
		request.Header.Set(apiKeyHeader, key)
	}
	return request
}

func serveThroughRequireAPIKey(t *testing.T, key string) (*httptest.ResponseRecorder, *recordingHandler) {
	t.Helper()
	next := &recordingHandler{}
	recorder := httptest.NewRecorder()
	RequireAPIKey(testAPIKey, next).ServeHTTP(recorder, requestWithKey(t, key))
	return recorder, next
}

func TestRequireAPIKeyLetsTheCorrectKeyThrough(t *testing.T) {
	recorder, next := serveThroughRequireAPIKey(t, testAPIKey)

	if !next.called {
		t.Error("the request with the correct key did not reach the protected handler")
	}
	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestRequireAPIKeyRejectsBadKeys(t *testing.T) {
	cases := []struct {
		name string
		key  string
	}{
		{"missing", ""},
		{"wrong, same length", "test-connector-key-9876543210"},
		{"shorter", "test-connector-key"},
		{"longer", testAPIKey + "-and-more"},
		{"different case", "TEST-CONNECTOR-KEY-0123456789"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder, next := serveThroughRequireAPIKey(t, tc.key)

			if next.called {
				t.Error("the request reached the protected handler")
			}
			if recorder.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestRequireAPIKeyAnswersAMissingAndAWrongKeyTheSameWay(t *testing.T) {
	missing, _ := serveThroughRequireAPIKey(t, "")
	wrong, _ := serveThroughRequireAPIKey(t, "not-the-key")

	if missing.Code != wrong.Code {
		t.Errorf("status: missing key %d, wrong key %d", missing.Code, wrong.Code)
	}
	if missing.Body.String() != wrong.Body.String() {
		t.Errorf("body: missing key %q, wrong key %q", missing.Body.String(), wrong.Body.String())
	}
	if got, want := missing.Header().Get("Content-Type"), wrong.Header().Get("Content-Type"); got != want {
		t.Errorf("Content-Type: missing key %q, wrong key %q", got, want)
	}
}

func TestRequireAPIKeyRejectsAnEmptyHeaderEvenWithAnEmptyExpectedKey(t *testing.T) {
	next := &recordingHandler{}
	recorder := httptest.NewRecorder()

	RequireAPIKey("", next).ServeHTTP(recorder, requestWithKey(t, ""))

	if next.called {
		t.Error("an empty expected key let a request without a key through")
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}
