package server

import (
	"log"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvcrn/mcp-server-stripe-org/internal/httpclient"
)

// Handler serves GET /health and the MCP endpoint at /mcp. getenv is called
// per request, so the worker can pass cloudflare.Getenv.
func Handler(getenv func(string) string) http.Handler {
	mcpHandler := mcp.NewStreamableHTTPHandler(serverFrom, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// Access is gated by the token, and deployments sit behind proxies that forward a public Host.
		DisableLocalhostProtection: true,
	})

	envs := &envCache{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok\n"))
	})
	mux.Handle("/mcp", requireToken(getenv, withEnv(getenv, envs, mcpHandler)))

	return mux
}

// withEnv puts the Env built from the current variables into the request context.
func withEnv(getenv func(string) string, envs *envCache, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env, err := envs.get(getenv)
		if err != nil {
			log.Printf("server: %v", err)
			http.Error(w, "server is not configured", http.StatusInternalServerError)
			return
		}

		next.ServeHTTP(w, r.WithContext(WithEnv(r.Context(), env)))
	})
}

// envCache reuses the parsed Env while the variables stay the same, which
// on Workers is the life of the isolate.
type envCache struct {
	mu       sync.Mutex
	key      string
	accounts string
	env      *Env
}

func (c *envCache) get(getenv func(string) string) (*Env, error) {
	key, accounts := getenv(EnvOrgKey), getenv(EnvAccounts)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.env != nil && c.key == key && c.accounts == accounts {
		return c.env, nil
	}

	env, err := NewEnv(key, accounts, httpclient.New())
	if err != nil {
		return nil, err
	}

	c.key, c.accounts, c.env = key, accounts, env

	return env, nil
}
