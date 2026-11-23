//go:build e2e

package e2e

import (
	"net/http"
	"strings"
	"testing"
)

func TestRequestsWithoutTheKeyAreUnauthorized(t *testing.T) {
	for _, path := range []string{"/", "/mcp", "/no-such-path"} {
		t.Run(path, func(t *testing.T) {
			status, _ := getForTest(t, path, "")

			if status != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", status, http.StatusUnauthorized)
			}
		})
	}
}

func TestRequestsWithAWrongKeyAreUnauthorized(t *testing.T) {
	for name, key := range map[string]string{
		"wrong, same length": strings.Repeat("x", len(apiKey())),
		"shorter":            apiKey()[:len(apiKey())-1],
		"longer":             apiKey() + "x",
	} {
		t.Run(name, func(t *testing.T) {
			status, _ := getForTest(t, "/mcp", key)

			if status != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", status, http.StatusUnauthorized)
			}
		})
	}
}

func TestAMissingAndAWrongKeyGetTheSameAnswer(t *testing.T) {
	missingStatus, missingBody := getForTest(t, "/mcp", "")
	wrongStatus, wrongBody := getForTest(t, "/mcp", "wrong-key")

	if missingStatus != wrongStatus || missingBody != wrongBody {
		t.Errorf("missing key got %d %q, wrong key got %d %q", missingStatus, missingBody, wrongStatus, wrongBody)
	}
}

// A request for a path with no route that passes the check is answered by
// routing: not found, rather than unauthorized.
func TestRequestsWithTheKeyPassTheCheck(t *testing.T) {
	status, _ := getForTest(t, "/no-such-path", apiKey())

	if status != http.StatusNotFound {
		t.Errorf("status = %d, want %d from routing once the key is accepted", status, http.StatusNotFound)
	}
}
