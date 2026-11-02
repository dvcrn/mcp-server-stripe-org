package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"log"
	"net/http"
	"strings"
)

// requireToken lets a request through only when it carries MCP_TOKEN. An
// unset token is a server error, so a missing secret never opens the server.
func requireToken(getenv func(string) string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := strings.TrimSpace(getenv(EnvToken))
		if expected == "" {
			log.Printf("server: %s is not set", EnvToken)
			http.Error(w, "server is not configured", http.StatusInternalServerError)
			return
		}

		if !tokenMatches(providedToken(r), expected) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func providedToken(r *http.Request) string {
	if scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " "); ok && strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(token)
	}

	return r.Header.Get("X-API-Key")
}

// tokenMatches compares hashes in constant time so neither length nor content leaks.
func tokenMatches(provided, expected string) bool {
	if provided == "" {
		return false
	}

	p := sha256.Sum256([]byte(provided))
	e := sha256.Sum256([]byte(expected))

	return subtle.ConstantTimeCompare(p[:], e[:]) == 1
}
