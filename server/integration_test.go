//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
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

// countingTransport counts the requests that reach Stripe.
type countingTransport struct {
	next  http.RoundTripper
	count atomic.Int32
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.count.Add(1)

	return c.next.RoundTrip(r)
}

// TestLiveAPIReadSensitiveExpand needs STRIPE_ORG_KEY (the sandbox org key),
// STRIPE_ACCOUNTS and STRIPE_TEST_ACCOUNT, for example via fnox exec.
func TestLiveAPIReadSensitiveExpand(t *testing.T) {
	key, accounts, account := os.Getenv(EnvOrgKey), os.Getenv(EnvAccounts), os.Getenv("STRIPE_TEST_ACCOUNT")
	if key == "" || accounts == "" || account == "" {
		t.Skip("STRIPE_ORG_KEY, STRIPE_ACCOUNTS and STRIPE_TEST_ACCOUNT must be set")
	}

	transport := &countingTransport{next: http.DefaultTransport}
	env, err := NewEnv(key, accounts, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}

	cs := connectEnv(t, WithEnv(context.Background(), env))

	wantError(t, call(t, cs, "api_read", map[string]any{
		"operation_id": "GetIssuingCardsCard",
		"account":      account,
		"path_params":  map[string]any{"card": "ic_dummy"},
		"params":       map[string]any{"expand": []any{"number", "cvc"}},
	}), `expand "number" is blocked`)

	if n := transport.count.Load(); n != 0 {
		t.Fatalf("blocked expand sent %d requests to Stripe, want 0", n)
	}

	res := call(t, cs, "api_read", map[string]any{
		"operation_id": "GetSubscriptions",
		"account":      account,
		"params":       map[string]any{"expand": []any{"data.customer", "data.latest_invoice"}, "limit": 1},
	})

	var list struct {
		Object string `json:"object"`
	}
	if err := json.Unmarshal([]byte(wantOK(t, res)), &list); err != nil {
		t.Fatal(err)
	}

	if list.Object != "list" || transport.count.Load() != 1 {
		t.Errorf("got object %q after %d requests, want a list after 1", list.Object, transport.count.Load())
	}
}
