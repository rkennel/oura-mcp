//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

func TestHealthEndpointReportsOK(t *testing.T) {
	status, body := getForTest(t, "/health")

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if want := "ok\n"; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestUnknownPathIsNotFound(t *testing.T) {
	status, _ := getForTest(t, "/no-such-path")

	if status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", status, http.StatusNotFound)
	}
}
