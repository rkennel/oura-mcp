package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// defaultPort is used when PORT is not set. Cloud Run always sets PORT.
	defaultPort = "8080"

	// apiKeyVariable names the environment variable that holds the connector
	// API key. In production it comes from Secret Manager.
	apiKeyVariable = "CONNECTOR_API_KEY" //nolint:gosec // G101: the variable's name, not a credential.
)

var (
	errAPIKeyMissing = errors.New(apiKeyVariable +
		" is not set: the server needs the connector API key to authenticate requests")
	// HTTP strips surrounding whitespace from header values, so a key with
	// any could never be presented. The message never includes the key.
	errAPIKeyWhitespace = errors.New(apiKeyVariable +
		" has leading or trailing whitespace, so no request could match it" +
		" (a secret saved with a trailing newline is the usual cause)")
)

// Config holds the settings the server reads from its environment.
type Config struct {
	// Addr is the TCP address the server listens on, such as ":8080".
	Addr string
	// APIKey is the key every request except the health check must carry.
	// It is a secret: never log it.
	APIKey string
}

// LoadConfig builds a Config from environment variables, looked up through
// getenv so that tests can supply their own values.
func LoadConfig(getenv func(string) string) (Config, error) {
	addr, err := loadAddr(getenv)
	if err != nil {
		return Config{}, err
	}
	apiKey, err := loadAPIKey(getenv)
	if err != nil {
		return Config{}, err
	}
	return Config{Addr: addr, APIKey: apiKey}, nil
}

func loadAddr(getenv func(string) string) (string, error) {
	port := getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return "", fmt.Errorf("PORT %q is not a valid TCP port", port)
	}
	return ":" + port, nil
}

func loadAPIKey(getenv func(string) string) (string, error) {
	apiKey := getenv(apiKeyVariable)
	if apiKey == "" {
		return "", errAPIKeyMissing
	}
	if strings.TrimSpace(apiKey) != apiKey {
		return "", errAPIKeyWhitespace
	}
	return apiKey, nil
}
