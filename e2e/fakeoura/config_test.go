package fakeoura

import (
	"errors"
	"testing"
)

func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadConfigReadsThePortAndClient(t *testing.T) {
	config, err := LoadConfig(envOf(map[string]string{
		"PORT": "9191", "OURA_CLIENT_ID": "id", "OURA_CLIENT_SECRET": "secret",
	}))

	if err != nil {
		t.Fatalf("LoadConfig returned an error: %v", err)
	}
	want := Config{Addr: ":9191", Client: Client{ID: "id", Secret: "secret"}}
	if config != want {
		t.Errorf("config = %+v, want %+v", config, want)
	}
}

func TestLoadConfigDefaultsThePort(t *testing.T) {
	config, err := LoadConfig(envOf(map[string]string{"OURA_CLIENT_ID": "id", "OURA_CLIENT_SECRET": "secret"}))

	if err != nil {
		t.Fatalf("LoadConfig returned an error: %v", err)
	}
	if config.Addr != ":8081" {
		t.Errorf("Addr = %q, want %q", config.Addr, ":8081")
	}
}

func TestLoadConfigRequiresTheClient(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"no id":     {"OURA_CLIENT_SECRET": "secret"},
		"no secret": {"OURA_CLIENT_ID": "id"},
		"neither":   {},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadConfig(envOf(env)); !errors.Is(err, errClientMissing) {
				t.Errorf("error = %v, want %v", err, errClientMissing)
			}
		})
	}
}
