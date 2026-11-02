//go:build integration

package stripe

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/dvcrn/mcp-server-stripe-org/internal/httpclient"
)

// liveClient needs STRIPE_ORG_KEY (the sandbox org key) and
// STRIPE_TEST_ACCOUNT (a sandbox acct_ ID), for example via fnox exec.
func liveClient(t *testing.T) (*Client, string) {
	t.Helper()

	key := os.Getenv("STRIPE_ORG_KEY")
	account := os.Getenv("STRIPE_TEST_ACCOUNT")
	if key == "" || account == "" {
		t.Skip("STRIPE_ORG_KEY and STRIPE_TEST_ACCOUNT must be set")
	}

	return New(httpclient.New(), key), account
}

func TestLiveBalance(t *testing.T) {
	c, account := liveClient(t)

	body, err := c.DoV1(context.Background(), V1Request{Method: http.MethodGet, Path: "/v1/balance", Account: account})
	if err != nil {
		t.Fatalf("GET /v1/balance: %v", err)
	}

	var balance struct {
		Object   string `json:"object"`
		Livemode bool   `json:"livemode"`
	}
	if err := json.Unmarshal(body, &balance); err != nil {
		t.Fatal(err)
	}

	if balance.Object != "balance" || balance.Livemode {
		t.Errorf("got object %q livemode %v, want a sandbox balance", balance.Object, balance.Livemode)
	}
}

func TestLiveQueryRunOrgMode(t *testing.T) {
	c, _ := liveClient(t)

	body, err := c.DoV2(context.Background(), V2Request{
		Method:  http.MethodPost,
		Path:    "/v2/data/query_runs",
		Version: PreviewAPIVersion,
		Body: map[string]any{
			"query":   map[string]string{"sql": "SELECT 1"},
			"dataset": "analytical",
			"format":  "csv",
		},
	})
	if err != nil {
		t.Fatalf("POST /v2/data/query_runs: %v", err)
	}

	var run struct {
		ID     string `json:"id"`
		Object string `json:"object"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &run); err != nil {
		t.Fatal(err)
	}

	if run.Object != "v2.data.query_run" || run.ID == "" {
		t.Fatalf("got %s", body)
	}

	t.Logf("query run %s status %s", run.ID, run.Status)
}
