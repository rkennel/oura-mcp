package app

import (
	"fmt"
	"strconv"
)

// defaultPort is used when PORT is not set. Cloud Run always sets PORT.
const defaultPort = "8080"

// Config holds the settings the server reads from its environment.
type Config struct {
	// Addr is the TCP address the server listens on, such as ":8080".
	Addr string
}

// LoadConfig builds a Config from environment variables, looked up through
// getenv so that tests can supply their own values.
func LoadConfig(getenv func(string) string) (Config, error) {
	port := getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return Config{}, fmt.Errorf("PORT %q is not a valid TCP port", port)
	}
	return Config{Addr: ":" + port}, nil
}
