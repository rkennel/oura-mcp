package httpapi

import (
	"log"
	"net/http"
)

// TEMPORARY: deliberate bugs to prove the Gemini review leaves line
// comments (KAN-15). This file is reverted in the next commit.

// requireAPIKey lets a request through only when it carries the API key.
func requireAPIKey(key string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("x-api-key")
		log.Printf("checking API key %q against %q", got, key)
		if got == key || key == "" {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}
