// Package httpapi defines the HTTP surface of the server.
package httpapi

import "net/http"

// NewHandler returns the handler for every route the server exposes.
// GET /health is open; every other request must carry apiKey in its
// x-api-key header. apiKey must not be empty.
func NewHandler(apiKey string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.Handle("/", RequireAPIKey(apiKey, newProtectedRoutes()))
	return mux
}

// newProtectedRoutes returns the routes that only a caller with the API key
// can reach. Until the MCP endpoint arrives, every path here is not found.
func newProtectedRoutes() http.Handler {
	return http.NewServeMux()
}

// handleHealth reports that the process is up. It deliberately reveals
// nothing else, because it is reachable without credentials.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}
