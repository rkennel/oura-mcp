package app

import "testing"

func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadConfigDefaultsThePort(t *testing.T) {
	config, err := LoadConfig(envOf(nil))

	if err != nil {
		t.Fatalf("LoadConfig returned an error: %v", err)
	}
	if got, want := config.Addr, ":8080"; got != want {
		t.Errorf("Addr = %q, want %q", got, want)
	}
}

func TestLoadConfigUsesThePortVariable(t *testing.T) {
	config, err := LoadConfig(envOf(map[string]string{"PORT": "9000"}))

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
			if _, err := LoadConfig(envOf(map[string]string{"PORT": port})); err == nil {
				t.Errorf("LoadConfig accepted PORT %q", port)
			}
		})
	}
}
