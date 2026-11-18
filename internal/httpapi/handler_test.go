package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthReportsOK(t *testing.T) {
	recorder := httptest.NewRecorder()

	NewHandler().ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", nil))

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got, want := recorder.Body.String(), "ok\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestHealthRejectsOtherMethods(t *testing.T) {
	recorder := httptest.NewRecorder()

	NewHandler().ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/health", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestUnknownPathIsNotFound(t *testing.T) {
	recorder := httptest.NewRecorder()

	NewHandler().ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/no-such-path", nil))

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}
