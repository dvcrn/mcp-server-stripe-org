package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// requireToken lets a request through only when it carries MCP_TOKEN, which
// gets every tool, or MCP_READ_TOKEN, which gets the read-only tools, and puts
// the matching server into the request context for serverFrom. MCP_READ_ONLY
// limits MCP_TOKEN to the read-only tools too. A missing or ambiguous token
// setup is a server error, so a missing secret never opens the server.
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

// StdioServer returns the server for stdio, which has no token: the
// read-only one when MCP_READ_ONLY is set, otherwise the full one.
func StdioServer(getenv func(string) string) (*mcp.Server, error) {
	readOnly, err := readOnlyFromEnv(getenv)
	if err != nil {
		return nil, err
	}

	if readOnly {
		return NewReadOnly(), nil
	}

	return New(), nil
}

// readOnlyFromEnv reports whether MCP_READ_ONLY limits the server to the
// read-only tools. A value that isn't a boolean is an error, so a typo
// can't silently allow writes.
func readOnlyFromEnv(getenv func(string) string) (bool, error) {
	v := strings.TrimSpace(getenv(EnvReadOnly))
	if v == "" {
		return false, nil
	}

	readOnly, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean such as 1 or 0, got %q", EnvReadOnly, v)
	}

	return readOnly, nil
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
