// Package httpapi defines the HTTP surface of the server.
package httpapi

import "net/http"

// MCPPath is where the MCP endpoint is served.
const MCPPath = "/mcp"

// NewHandler returns the handler for every route the server exposes.
// GET /health is open; every other request must carry apiKey in its
// x-api-key header. Requests to MCPPath that pass the key check go to mcp.
// apiKey must not be empty.
func NewHandler(apiKey string, mcp http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.Handle("/", RequireAPIKey(apiKey, newProtectedRoutes(mcp)))
	return mux
}

// newProtectedRoutes returns the routes that only a caller with the API key
// can reach. Every other path is not found.
func newProtectedRoutes(mcp http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(MCPPath, mcp)
	return mux
}

// handleHealth reports that the process is up. It deliberately reveals
// nothing else, because it is reachable without credentials.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}
