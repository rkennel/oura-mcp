//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
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
	// 127.0.0.1 rather than localhost: compose publishes the port on IPv4
	// loopback only, and localhost may resolve to ::1 first.
	return "http://127.0.0.1:8080"
}

// apiKey is the connector API key the server under test was started with.
// scripts/e2e-test.sh generates one for each run.
func apiKey() string {
	return os.Getenv("CONNECTOR_API_KEY")
}

// get sends a GET request for path to the server under test, with key in the
// x-api-key header unless key is empty.
func get(ctx context.Context, path, key string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL()+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", path, err)
	}
	if key != "" {
		request.Header.Set("x-api-key", key)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	return response, nil
}

// getForTest sends a GET request for path, with key in the x-api-key header
// unless key is empty, and returns the response status and body. It fails
// the test if the request fails.
func getForTest(t *testing.T, path, key string) (status int, body string) {
	t.Helper()
	response, err := get(t.Context(), path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBody(t, response)

	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body of %s: %v", path, err)
	}
	return response.StatusCode, string(content)
}

func closeBody(t *testing.T, response *http.Response) {
	t.Helper()
	if err := response.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
}

// TestMain waits for the server to accept requests before running any test,
// so a slow container start is not reported as a test failure.
func TestMain(m *testing.M) {
	if apiKey() == "" {
		fmt.Fprintln(os.Stderr,
			"CONNECTOR_API_KEY is not set: set it to the key the server was started with, or run these tests with scripts/e2e-test.sh")
		os.Exit(1)
	}
	if err := waitUntilHealthy(); err != nil {
		fmt.Fprintf(os.Stderr,
			"could not reach the server at %s: %v\nIs it running? Start it and run these tests with scripts/e2e-test.sh\n",
			baseURL(), err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func waitUntilHealthy() error {
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()
	for {
		err := checkHealth(ctx)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// checkHealth makes one health check and returns an error unless it passes.
func checkHealth(ctx context.Context) error {
	response, err := get(ctx, "/health", "")
	if err != nil {
		return err
	}
	if err := response.Body.Close(); err != nil {
		return fmt.Errorf("close health check body: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", response.StatusCode)
	}
	return nil
}
