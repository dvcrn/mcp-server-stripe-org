package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// requireToken lets a request through only when it carries MCP_TOKEN (every
// tool) or MCP_READ_TOKEN (read-only tools), and stores the matching server
// in the request context for serverFrom. MCP_READ_ONLY limits MCP_TOKEN to
// the read-only tools too. No token, equal tokens or an invalid MCP_READ_ONLY
// is a server error, so a missing secret never opens the server.
func requireToken(getenv func(string) string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		full := strings.TrimSpace(getenv(EnvToken))
		read := strings.TrimSpace(getenv(EnvReadToken))
		readOnly, err := readOnlyFromEnv(getenv)
		if err == nil {
			err = checkTokens(full, read)
		}

		if err != nil {
			log.Printf("server: %v", err)
			http.Error(w, "server is not configured", http.StatusInternalServerError)
			return
		}

		provided := providedToken(r)
		fullOK := tokenMatches(provided, full)
		readOK := tokenMatches(provided, read)

		var srv *mcp.Server
		switch {
		case fullOK && !readOnly:
			srv = New()
		case fullOK || readOK:
			srv = NewReadOnly()
		default:
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), serverKey{}, srv)))
	})
}

func checkTokens(full, read string) error {
	switch {
	case full == "" && read == "":
		return fmt.Errorf("neither %s nor %s is set", EnvToken, EnvReadToken)
	case full == read:
		// One value for both tokens would make the access level ambiguous.
		return fmt.Errorf("%s and %s are the same", EnvToken, EnvReadToken)
	}

	return nil
}

type serverKey struct{}

// serverFrom returns the server requireToken chose. It is nil without auth,
// which the MCP handler refuses.
func serverFrom(r *http.Request) *mcp.Server {
	srv, _ := r.Context().Value(serverKey{}).(*mcp.Server)

	return srv
}

func providedToken(r *http.Request) string {
	if scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " "); ok && strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(token)
	}

	return r.Header.Get("X-API-Key")
}

// tokenMatches compares hashes in constant time so neither length nor content
// leaks. An unset token matches nothing.
func tokenMatches(provided, expected string) bool {
	p := sha256.Sum256([]byte(provided))
	e := sha256.Sum256([]byte(expected))

	return subtle.ConstantTimeCompare(p[:], e[:]) == 1 && provided != "" && expected != ""
}
