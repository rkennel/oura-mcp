package app

import (
	"strings"
	"testing"
)

// testAPIKey is a dummy connector key for tests.
const testAPIKey = "test-connector-key"

func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

// validEnv returns an environment with every required variable set, plus
// extra, which may override them.
func validEnv(extra map[string]string) func(string) string {
	values := map[string]string{apiKeyVariable: testAPIKey}
	for key, value := range extra {
		values[key] = value
	}
	return envOf(values)
}

func TestLoadConfigDefaultsThePort(t *testing.T) {
	config, err := LoadConfig(validEnv(nil))

	if err != nil {
		t.Fatalf("LoadConfig returned an error: %v", err)
	}
	if got, want := config.Addr, ":8080"; got != want {
		t.Errorf("Addr = %q, want %q", got, want)
	}
}

func TestLoadConfigUsesThePortVariable(t *testing.T) {
	config, err := LoadConfig(validEnv(map[string]string{"PORT": "9000"}))

	if err != nil {
		t.Fatalf("LoadConfig returned an error: %v", err)
	}
	if got, want := config.Addr, ":9000"; got != want {
		t.Errorf("Addr = %q, want %q", got, want)
	}
}

func TestLoadConfigRejectsAnInvalidPort(t *testing.T) {
	for _, port := range []string{"http", "-1", "65536", "80 80"} {
		t.Run(port, func(t *testing.T) {
			if _, err := LoadConfig(validEnv(map[string]string{"PORT": port})); err == nil {
				t.Errorf("LoadConfig accepted PORT %q", port)
			}
		})
	}
}

func TestLoadConfigReadsTheConnectorAPIKey(t *testing.T) {
	config, err := LoadConfig(validEnv(nil))

	if err != nil {
		t.Fatalf("LoadConfig returned an error: %v", err)
	}
	if config.APIKey != testAPIKey {
		t.Errorf("APIKey = %q, want the value of %s", config.APIKey, apiKeyVariable)
	}
}

func TestLoadConfigRequiresTheConnectorAPIKey(t *testing.T) {
	_, err := LoadConfig(envOf(map[string]string{"PORT": "8080"}))

	if err == nil {
		t.Fatal("LoadConfig accepted an environment without CONNECTOR_API_KEY")
	}
	if !strings.Contains(err.Error(), "CONNECTOR_API_KEY") {
		t.Errorf("error %q does not name CONNECTOR_API_KEY", err)
	}
}

func TestLoadConfigRejectsAKeyWithSurroundingWhitespace(t *testing.T) {
	// HTTP strips surrounding whitespace from header values, so such a key
	// could never be presented. A secret saved with a trailing newline is
	// the usual cause.
	for name, key := range map[string]string{
		"trailing newline": "secret-value\n",
		"leading space":    " secret-value",
		"only whitespace":  " \t ",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadConfig(validEnv(map[string]string{apiKeyVariable: key}))

			if err == nil {
				t.Fatalf("LoadConfig accepted the key %q", key)
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Errorf("error %q reveals the key", err)
			}
		})
	}
}

func TestLoadConfigDefaultsTheOuraBaseURLToTheRealAPI(t *testing.T) {
	config, err := LoadConfig(validEnv(nil))

	if err != nil {
		t.Fatalf("LoadConfig returned an error: %v", err)
	}
	if got, want := config.OuraBaseURL, "https://api.ouraring.com"; got != want {
		t.Errorf("OuraBaseURL = %q, want %q", got, want)
	}
}

func TestLoadConfigReadsTheOuraBaseURL(t *testing.T) {
	for value, want := range map[string]string{
		"http://fakeoura:8081":         "http://fakeoura:8081",
		"http://fakeoura:8081/":        "http://fakeoura:8081",
		"https://oura.example/prefix/": "https://oura.example/prefix",
	} {
		t.Run(value, func(t *testing.T) {
			config, err := LoadConfig(validEnv(map[string]string{ouraBaseURLVariable: value}))

			if err != nil {
				t.Fatalf("LoadConfig returned an error: %v", err)
			}
			if config.OuraBaseURL != want {
				t.Errorf("OuraBaseURL = %q, want %q", config.OuraBaseURL, want)
			}
		})
	}
}

func TestLoadConfigRejectsAnOuraBaseURLThatCannotBeABase(t *testing.T) {
	//nolint:gosec // G101: made-up credentials in a URL the test expects to be rejected.
	for name, value := range map[string]string{
		"no scheme":      "api.ouraring.com",
		"other scheme":   "ftp://api.ouraring.com",
		"no host":        "https://",
		"credentials":    "https://user:hunter2@api.ouraring.com",
		"query":          "https://api.ouraring.com?x=1",
		"empty query":    "https://api.ouraring.com?",
		"fragment":       "https://api.ouraring.com#top",
		"does not parse": "https://api.ouraring.com/%zz",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadConfig(validEnv(map[string]string{ouraBaseURLVariable: value}))

			if err == nil {
				t.Fatalf("LoadConfig accepted %q", value)
			}
			if !strings.Contains(err.Error(), ouraBaseURLVariable) || strings.Contains(err.Error(), "hunter2") {
				t.Errorf("error %q should name %s and never repeat the value", err, ouraBaseURLVariable)
			}
		})
	}
}
