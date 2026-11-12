// Package httpapi defines the HTTP surface of the server.
package httpapi

import "net/http"

// NewHandler returns the handler for every route the server exposes.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	return mux
}

// handleHealth reports that the process is up. It deliberately reveals
// nothing else, because it is reachable without credentials.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}
