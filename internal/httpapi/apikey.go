package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

// apiKeyHeader is the request header that carries the connector API key.
const apiKeyHeader = "x-api-key"

// RequireAPIKey returns a handler that passes a request to next only when its
// x-api-key header holds apiKey. Every other request, with no key or the
// wrong one, gets the same 401 response, so a caller cannot tell the two
// apart. Neither key is ever logged or echoed.
func RequireAPIKey(apiKey string, next http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(apiKey))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !keyMatches(r.Header.Get(apiKeyHeader), expected) {
			rejectUnauthorized(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// keyMatches reports whether presented is the key whose digest is expected.
// Comparing fixed-length digests in constant time means the time taken
// reveals neither the key's content nor its length. An empty key never
// matches, even if the expected key is empty too.
func keyMatches(presented string, expected [sha256.Size]byte) bool {
	digest := sha256.Sum256([]byte(presented))
	matches := subtle.ConstantTimeCompare(digest[:], expected[:]) == 1
	return matches && presented != ""
}

func rejectUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte("unauthorized\n"))
}
