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
	NewHandler(testAPIKey, &recordingHandler{}).ServeHTTP(recorder, request)
	return recorder
}

// serveMCP sends method to MCPPath through the server's handler, with key
// in the x-api-key header unless key is empty, and reports whether the
// request reached the MCP handler.
func serveMCP(t *testing.T, method, key string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, MCPPath, nil)
	if key != "" {
		request.Header.Set(apiKeyHeader, key)
	}
	mcp := &recordingHandler{}
	recorder := httptest.NewRecorder()
	NewHandler(testAPIKey, mcp).ServeHTTP(recorder, request)
	return recorder, mcp.called
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

func TestTheMCPEndpointReceivesRequestsWithTheKey(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			recorder, reached := serveMCP(t, method, testAPIKey)

			if !reached {
				t.Errorf("%s %s with the key did not reach the MCP handler", method, MCPPath)
			}
			if recorder.Code != http.StatusOK {
				t.Errorf("status = %d, want the MCP handler's %d", recorder.Code, http.StatusOK)
			}
		})
	}
}

func TestTheMCPEndpointNeedsTheKey(t *testing.T) {
	for name, key := range map[string]string{"missing": "", "wrong": "wrong-key"} {
		t.Run(name, func(t *testing.T) {
			recorder, reached := serveMCP(t, http.MethodPost, key)

			if reached {
				t.Errorf("a request with a %s key reached the MCP handler", name)
			}
			if recorder.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
		})
	}
}
