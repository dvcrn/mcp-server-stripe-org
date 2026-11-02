//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)

	return http.DefaultTransport.RoundTrip(r)
}

// TestLiveAPIReadBalance needs STRIPE_ORG_KEY (the sandbox org key) and
// STRIPE_ACCOUNTS with at least two sandbox accounts, for example via fnox exec.
func TestLiveAPIReadBalance(t *testing.T) {
	var accounts []Account
	if raw := os.Getenv(EnvAccounts); raw != "" {
		if err := json.Unmarshal([]byte(raw), &accounts); err != nil {
			t.Fatalf("%s does not parse: %v", EnvAccounts, err)
		}
	}

	if os.Getenv(EnvOrgKey) == "" || len(accounts) < 2 {
		t.Skip("STRIPE_ORG_KEY and STRIPE_ACCOUNTS with two accounts must be set")
	}

	const token = "integration-token"
	srv := httptest.NewServer(Handler(func(name string) string {
		if name == EnvToken {
			return token
		}

		return os.Getenv(name)
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	transport := &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{token}}}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "integration", Version: "0"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })

	for _, a := range accounts[:2] {
		res := call(t, cs, "api_read", map[string]any{"operation_id": "GetBalance", "account": a.Alias})

		var balance struct {
			Object   string `json:"object"`
			Livemode bool   `json:"livemode"`
		}
		if err := json.Unmarshal([]byte(wantOK(t, res)), &balance); err != nil {
			t.Fatal(err)
		}

		if balance.Object != "balance" || balance.Livemode {
			t.Errorf("%s: got object %q livemode %v, want a sandbox balance", a.Alias, balance.Object, balance.Livemode)
		}
	}
}
