package fakeoura

import "errors"

// defaultPort is the port the fake listens on when PORT is not set. It
// differs from the server's 8080 so that both can run on one machine.
const defaultPort = "8081"

var errClientMissing = errors.New("OURA_CLIENT_ID and OURA_CLIENT_SECRET must both be set: they are the OAuth client the fake accepts")

// Config holds the settings the fake reads from its environment.
type Config struct {
	// Addr is the TCP address the fake listens on, such as ":8081".
	Addr string
	// Client is the OAuth client its token endpoint accepts.
	Client Client
}

// LoadConfig builds a Config from environment variables, looked up through
// getenv so that tests can supply their own values.
func LoadConfig(getenv func(string) string) (Config, error) {
	port := getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	client := Client{ID: getenv("OURA_CLIENT_ID"), Secret: getenv("OURA_CLIENT_SECRET")}
	if client.ID == "" || client.Secret == "" {
		return Config{}, errClientMissing
	}
	return Config{Addr: ":" + port, Client: client}, nil
}
