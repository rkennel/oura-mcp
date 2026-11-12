//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"testing"
)

func TestHealthEndpointReportsOK(t *testing.T) {
	response, err := client.Get(baseURL() + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got, want := string(body), "ok\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestUnknownPathIsNotFound(t *testing.T) {
	response, err := client.Get(baseURL() + "/no-such-path")
	if err != nil {
		t.Fatalf("GET /no-such-path: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", response.StatusCode, http.StatusNotFound)
	}
}
