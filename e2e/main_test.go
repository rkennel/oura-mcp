//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

const (
	requestTimeout = 5 * time.Second
	startupTimeout = 30 * time.Second
)

var client = &http.Client{Timeout: requestTimeout}

// baseURL is the address of the server under test. Override with E2E_BASE_URL.
func baseURL() string {
	if url := os.Getenv("E2E_BASE_URL"); url != "" {
		return url
	}
	return "http://localhost:8080"
}

// TestMain waits for the server to accept requests before running any test,
// so a slow container start is not reported as a test failure.
func TestMain(m *testing.M) {
	if err := waitUntilHealthy(); err != nil {
		fmt.Fprintf(os.Stderr,
			"could not reach the server at %s: %v\nIs it running? Start it and run these tests with scripts/e2e-test.sh\n",
			baseURL(), err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func waitUntilHealthy() error {
	deadline := time.Now().Add(startupTimeout)
	for {
		response, err := client.Get(baseURL() + "/health")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
			err = fmt.Errorf("health check returned status %d", response.StatusCode)
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}
