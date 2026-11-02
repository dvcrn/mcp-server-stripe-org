//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// liveSession serves the MCP handler with the real environment and returns
// a client session plus the configured accounts. It skips the test unless
// STRIPE_ORG_KEY and STRIPE_ACCOUNTS (sandbox values) are set.
func liveSession(t *testing.T) (*mcp.ClientSession, []Account) {
	t.Helper()

	var accounts []Account
	if raw := os.Getenv(EnvAccounts); raw != "" {
		if err := json.Unmarshal([]byte(raw), &accounts); err != nil {
			t.Fatalf("%s does not parse: %v", EnvAccounts, err)
		}
	}

	if os.Getenv(EnvOrgKey) == "" || len(accounts) == 0 {
		t.Skip("STRIPE_ORG_KEY and STRIPE_ACCOUNTS must be set")
	}

	const token = "integration-token"
	srv := httptest.NewServer(Handler(func(name string) string {
		if name == EnvToken {
			return token
		}

		return os.Getenv(name)
	}))
	t.Cleanup(srv.Close)

	transport := &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{token}}}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "integration", Version: "0"}, nil).Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })

	return cs, accounts
}

var queryRunIDLine = regexp.MustCompile(`(?m)^query_run_id: (qrun_\S+)$`)

// runUntilDone runs sql and follows up with get_query_run until the run
// leaves the running state; sandbox runs can take several minutes.
func runUntilDone(t *testing.T, cs *mcp.ClientSession, sql, account string) string {
	t.Helper()

	args := map[string]any{"sql": sql}
	if account != "" {
		args["account"] = account
	}

	got := wantOK(t, call(t, cs, "run_sql_query", args))
	m := queryRunIDLine.FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("no query_run_id in:\n%s", got)
	}

	delete(args, "sql")
	args["query_run_id"] = m[1]
	for deadline := time.Now().Add(8 * time.Minute); strings.Contains(got, "status: running\n"); {
		if time.Now().After(deadline) {
			t.Fatalf("query run %s still running after 8 minutes", m[1])
		}

		time.Sleep(5 * time.Second)
		got = wantOK(t, call(t, cs, "get_query_run", args))
	}

	return got
}

// TestLiveRunSQLQuery runs in org mode and for STRIPE_TEST_ACCOUNT (an
// allowlisted sandbox acct_ ID), falling back to the first configured account.
func TestLiveRunSQLQuery(t *testing.T) {
	cs, accounts := liveSession(t)

	account := os.Getenv("STRIPE_TEST_ACCOUNT")
	if account == "" {
		account = accounts[0].Alias
	}

	for _, account := range []string{"", account} {
		got := runUntilDone(t, cs, "SELECT 1 AS one", account)
		t.Logf("account set: %v\n%s", account != "", got)

		if !strings.Contains(got, "status: succeeded\n") || !strings.HasSuffix(got, "\n\none\n1\n") {
			t.Errorf("account set %v: want one row with 1, got:\n%s", account != "", got)
		}

		// A limit above the maximum is capped to one Stripe accepts.
		args := map[string]any{"query_run_id": queryRunIDLine.FindStringSubmatch(got)[1], "limit": 1000}
		if account != "" {
			args["account"] = account
		}

		if got := wantOK(t, call(t, cs, "get_query_run", args)); !strings.HasSuffix(got, "\n\none\n1\n") {
			t.Errorf("account set %v: limit 1000: want one row with 1, got:\n%s", account != "", got)
		}
	}
}

func TestLiveAPIReadLimitCapped(t *testing.T) {
	cs, accounts := liveSession(t)

	got := wantOK(t, call(t, cs, "api_read", map[string]any{"operation_id": "GetCustomers", "account": accounts[0].Alias, "params": map[string]any{"limit": 1000}}))
	if !strings.Contains(got, `"object": "list"`) {
		t.Errorf("want a customer list, got:\n%.500s", got)
	}
}

func TestLiveListQueryTables(t *testing.T) {
	cs, _ := liveSession(t)

	got := wantOK(t, call(t, cs, "list_query_tables", nil))
	if !strings.Contains(got, "\ncharges: ") {
		t.Errorf("charges not listed:\n%.2000s", got)
	}

	t.Logf("%.300s", got)

	got = wantOK(t, call(t, cs, "list_query_tables", map[string]any{"tables": []any{"charges"}}))
	if !strings.HasPrefix(got, "## charges\n") || !strings.Contains(got, "\n- id varchar primary key") {
		t.Errorf("charges columns missing:\n%.2000s", got)
	}

	for _, m := range hostOnlyMarkers {
		if strings.Contains(got, m) {
			t.Errorf("charges output mentions %s:\n%.2000s", m, got)
		}
	}

	t.Logf("%.300s", got)
}
