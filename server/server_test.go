package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvcrn/mcp-server-stripe-org/stripe"
)

// Dummy accounts; the IDs are made up.
const testAccounts = `[
	{"alias": "dummy-one", "name": "Dummy One", "account": "acct_1DUMMYONE"},
	{"alias": "dummy-two", "name": "Dummy Two", "account": "acct_1DUMMYTWO"}
]`

// connect starts the server against a fake Stripe API and returns a client session.
func connect(t *testing.T, key string, upstream http.HandlerFunc) *mcp.ClientSession {
	t.Helper()

	srv := httptest.NewServer(upstream)
	t.Cleanup(srv.Close)

	env, err := NewEnv(key, testAccounts, srv.Client(), stripe.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}

	return connectEnv(t, WithEnv(context.Background(), env))
}

func connectEnv(t *testing.T, ctx context.Context) *mcp.ClientSession {
	t.Helper()

	st, ct := mcp.NewInMemoryTransports()
	ss, err := New().Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })

	return cs
}

func noStripe(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected Stripe request %s %s", r.Method, r.URL)
		http.Error(w, "unexpected", http.StatusTeapot)
	}
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}

	return res
}

func text(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()

	if len(res.Content) != 1 {
		t.Fatalf("got %d content items, want 1", len(res.Content))
	}

	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want text", res.Content[0])
	}

	return tc.Text
}

func wantOK(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()

	got := text(t, res)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", got)
	}

	return got
}

func wantError(t *testing.T, res *mcp.CallToolResult, substr string) {
	t.Helper()

	got := text(t, res)
	if !res.IsError {
		t.Fatalf("want tool error containing %q, got success: %s", substr, got)
	}

	if !strings.Contains(got, substr) {
		t.Errorf("error %q does not contain %q", got, substr)
	}
}

func structured[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()

	wantOK(t, res)

	var out T
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}

	return out
}

func TestToolsRegistered(t *testing.T) {
	cs := connect(t, "sk_org_test_dummy", noStripe(t))

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	readTools := []string{
		"api_read", "get_api_operation", "get_query_run", "list_accounts", "list_query_tables", "run_sql_query", "search_api_operations",
	}

	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)

		if slices.Contains(readTools, tool.Name) && (tool.Annotations == nil || !tool.Annotations.ReadOnlyHint) {
			t.Errorf("%s is not annotated read-only", tool.Name)
		}
	}

	slices.Sort(names)
	want := slices.Sorted(slices.Values(append(slices.Clone(readTools), writeToolNames...)))
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
}

func TestNoNullInSchemas(t *testing.T) {
	cs := connect(t, "sk_org_test_dummy", noStripe(t))

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, tool := range res.Tools {
		for kind, schema := range map[string]any{"input": tool.InputSchema, "output": tool.OutputSchema} {
			b, _ := json.Marshal(schema)
			if strings.Contains(string(b), `"null"`) {
				t.Errorf("%s %s schema declares null: %s", tool.Name, kind, b)
			}
		}
	}
}

func TestNewIsBuiltOnce(t *testing.T) {
	if New() != New() {
		t.Error("New returned different servers")
	}
}

func TestToolWithoutEnv(t *testing.T) {
	cs := connectEnv(t, context.Background())

	wantError(t, call(t, cs, "list_accounts", nil), "server is not configured")
}

func TestListAccounts(t *testing.T) {
	for _, tc := range []struct {
		key      string
		livemode bool
	}{
		{"sk_org_test_dummy", false},
		{"sk_org_live_dummy", true},
	} {
		cs := connect(t, tc.key, noStripe(t))

		out := structured[listAccountsOutput](t, call(t, cs, "list_accounts", nil))
		want := []accountOutput{
			{Alias: "dummy-one", Name: "Dummy One", Account: "acct_1DUMMYONE", Livemode: tc.livemode},
			{Alias: "dummy-two", Name: "Dummy Two", Account: "acct_1DUMMYTWO", Livemode: tc.livemode},
		}
		if !slices.Equal(out.Accounts, want) {
			t.Errorf("%s: accounts = %+v, want %+v", tc.key, out.Accounts, want)
		}
	}
}

func TestSearchAPIOperations(t *testing.T) {
	cs := connect(t, "sk_org_test_dummy", noStripe(t))

	out := structured[searchAPIOperationsOutput](t, call(t, cs, "search_api_operations", map[string]any{"query": "balance"}))
	if !slices.ContainsFunc(out.Operations, func(op operationSummary) bool { return op.OperationID == "GetBalance" }) {
		t.Errorf("GetBalance not in %+v", out.Operations)
	}

	if len(out.Operations) > defaultSearchLimit {
		t.Errorf("got %d results, want at most %d", len(out.Operations), defaultSearchLimit)
	}

	out = structured[searchAPIOperationsOutput](t, call(t, cs, "search_api_operations", map[string]any{"query": "a", "limit": 1000}))
	if len(out.Operations) != maxSearchLimit {
		t.Errorf("got %d results, want %d", len(out.Operations), maxSearchLimit)
	}

	for _, q := range []string{"", "   "} {
		wantError(t, call(t, cs, "search_api_operations", map[string]any{"query": q}), "query is required")
	}

	out = structured[searchAPIOperationsOutput](t, call(t, cs, "search_api_operations", map[string]any{"query": "zzzznothing"}))
	if len(out.Operations) != 0 || out.Note == "" {
		t.Errorf("want no results and a note, got %+v", out)
	}
}

func TestGetAPIOperation(t *testing.T) {
	cs := connect(t, "sk_org_test_dummy", noStripe(t))

	out := structured[getAPIOperationOutput](t, call(t, cs, "get_api_operation", map[string]any{"operation_id": "GetCustomersCustomer"}))
	if out.Path != "/v1/customers/{customer}" || !out.AccountRequired || !slices.Equal(out.PathParams, []string{"customer"}) {
		t.Errorf("got %+v", out)
	}

	if !slices.ContainsFunc(out.QueryParams, func(p queryParamOutput) bool { return p.Name == "expand" && p.Type == "string[]" }) {
		t.Errorf("expand not in %+v", out.QueryParams)
	}

	out = structured[getAPIOperationOutput](t, call(t, cs, "get_api_operation", map[string]any{"operation_id": "GetV2CoreAccounts"}))
	if out.AccountRequired {
		t.Error("v2 operation should not require an account")
	}

	if !slices.ContainsFunc(out.QueryParams, func(p queryParamOutput) bool { return p.Name == "page" }) {
		t.Errorf("v2 list lacks page: %+v", out.QueryParams)
	}

	wantError(t, call(t, cs, "get_api_operation", map[string]any{"operation_id": "PostCustomers"}), "search_api_operations")
}

type stripeCall struct {
	path    string
	query   string
	context string
}

// recordStripe answers every request with body and records what was asked.
func recordStripe(calls *[]stripeCall, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, stripeCall{r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("Stripe-Context")})
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}
}

func TestAPIRead(t *testing.T) {
	var calls []stripeCall
	cs := connect(t, "sk_org_test_dummy", recordStripe(&calls, `{"object":"customer","id":"cus_123"}`))

	res := call(t, cs, "api_read", map[string]any{
		"operation_id": "GetCustomersCustomer",
		"account":      "dummy-two",
		"path_params":  map[string]any{"customer": "cus_123"},
		"params":       map[string]any{"expand": []any{"default_source"}},
	})
	if got := wantOK(t, res); got != `{"object":"customer","id":"cus_123"}` {
		t.Errorf("text = %s", got)
	}

	if res.StructuredContent != nil {
		t.Errorf("structured content = %v, want none", res.StructuredContent)
	}

	want := []stripeCall{{"/v1/customers/cus_123", "expand%5B0%5D=default_source", "acct_1DUMMYTWO"}}
	if !slices.Equal(calls, want) {
		t.Errorf("calls = %+v, want %+v", calls, want)
	}
}

func TestAPIReadHasNoOutputSchema(t *testing.T) {
	cs := connect(t, "sk_org_test_dummy", noStripe(t))

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, tool := range res.Tools {
		if tool.Name == "api_read" && tool.OutputSchema != nil {
			t.Errorf("api_read output schema = %v, want none", tool.OutputSchema)
		}
	}
}

func TestAPIReadAccountByID(t *testing.T) {
	var calls []stripeCall
	cs := connect(t, "sk_org_test_dummy", recordStripe(&calls, `{"object":"balance"}`))

	wantOK(t, call(t, cs, "api_read", map[string]any{"operation_id": "GetBalance", "account": "acct_1DUMMYONE"}))

	if len(calls) != 1 || calls[0].context != "acct_1DUMMYONE" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestAPIReadLimit(t *testing.T) {
	for _, tc := range []struct {
		limit any
		want  string
	}{
		{nil, "limit=10"},
		{25, "limit=25"},
		{1000, "limit=100"},
	} {
		var calls []stripeCall
		cs := connect(t, "sk_org_test_dummy", recordStripe(&calls, `{"object":"list","data":[]}`))

		params := map[string]any{}
		if tc.limit != nil {
			params["limit"] = tc.limit
		}

		wantOK(t, call(t, cs, "api_read", map[string]any{"operation_id": "GetCustomers", "account": "dummy-one", "params": params}))

		if len(calls) != 1 || calls[0].query != tc.want {
			t.Errorf("limit %v: calls = %+v, want query %s", tc.limit, calls, tc.want)
		}
	}
}

func TestAPIReadV2(t *testing.T) {
	var calls []stripeCall
	cs := connect(t, "sk_org_test_dummy", recordStripe(&calls, `{"data":[]}`))

	wantOK(t, call(t, cs, "api_read", map[string]any{
		"operation_id": "GetV2CoreAccounts",
		"params":       map[string]any{"applied_configurations": []any{"customer", "merchant"}},
	}))
	wantOK(t, call(t, cs, "api_read", map[string]any{"operation_id": "GetV2CoreAccounts", "account": "dummy-one"}))
	wantOK(t, call(t, cs, "api_read", map[string]any{"operation_id": "GetV2CoreEvents", "params": map[string]any{"page": "page_dummy", "limit": 5}}))

	want := []stripeCall{
		{"/v2/core/accounts", "applied_configurations%5B0%5D=customer&applied_configurations%5B1%5D=merchant&limit=10", ""},
		{"/v2/core/accounts", "limit=10", "acct_1DUMMYONE"},
		{"/v2/core/events", "limit=5&page=page_dummy", ""},
	}
	if !slices.Equal(calls, want) {
		t.Errorf("calls = %+v, want %+v", calls, want)
	}
}

func TestAPIReadErrors(t *testing.T) {
	cs := connect(t, "sk_org_test_dummy", noStripe(t))

	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"unknown operation", map[string]any{"operation_id": "/v1/customers", "account": "dummy-one"}, "search_api_operations"},
		{"write operation", map[string]any{"operation_id": "PostCustomers", "account": "dummy-one"}, "unknown operation"},
		{"unknown account", map[string]any{"operation_id": "GetBalance", "account": "nope"}, "dummy-one, dummy-two"},
		{"unlisted acct_ ID", map[string]any{"operation_id": "GetBalance", "account": "acct_1OTHER"}, "unknown account"},
		{"missing v1 account", map[string]any{"operation_id": "GetBalance"}, "account is required"},
		{"unknown v2 account", map[string]any{"operation_id": "GetV2CoreAccounts", "account": "nope"}, "unknown account"},
		{"missing path param", map[string]any{"operation_id": "GetCustomersCustomer", "account": "dummy-one"}, `needs path parameter "customer"`},
		{"empty path param", map[string]any{"operation_id": "GetCustomersCustomer", "account": "dummy-one", "path_params": map[string]any{"customer": ""}}, `needs path parameter "customer"`},
		{"traversal path param", map[string]any{"operation_id": "GetCustomersCustomer", "account": "dummy-one", "path_params": map[string]any{"customer": ".."}}, "must match"},
		{"slash in path param", map[string]any{"operation_id": "GetCustomersCustomer", "account": "dummy-one", "path_params": map[string]any{"customer": "cus_1/../../v1/balance"}}, "must match"},
		{"query in path param", map[string]any{"operation_id": "GetCustomersCustomer", "account": "dummy-one", "path_params": map[string]any{"customer": "cus_1?limit=1"}}, "must match"},
		{"undeclared path param", map[string]any{"operation_id": "GetBalance", "account": "dummy-one", "path_params": map[string]any{"id": "x"}}, "has no path parameter"},
		{"undeclared query param", map[string]any{"operation_id": "GetBalance", "account": "dummy-one", "params": map[string]any{"limit": 5}}, `does not accept query parameter "limit"`},
		{"bracket query key", map[string]any{"operation_id": "GetCustomers", "account": "dummy-one", "params": map[string]any{"created[gt]": 1}}, "does not accept query parameter"},
		{"bracket nested key", map[string]any{"operation_id": "GetCustomers", "account": "dummy-one", "params": map[string]any{"created": map[string]any{"gt][x": 1}}}, "invalid key"},
		{"page on v1", map[string]any{"operation_id": "GetCustomers", "account": "dummy-one", "params": map[string]any{"page": "x"}}, `does not accept query parameter "page"`},
		{"page on v2 item", map[string]any{"operation_id": "GetV2CoreAccountsId", "path_params": map[string]any{"id": "x"}, "params": map[string]any{"page": "x"}}, `does not accept query parameter "page"`},
		{"zero limit", map[string]any{"operation_id": "GetCustomers", "account": "dummy-one", "params": map[string]any{"limit": 0}}, "whole number"},
		{"fractional limit", map[string]any{"operation_id": "GetCustomers", "account": "dummy-one", "params": map[string]any{"limit": 2.5}}, "whole number"},
		{"string limit", map[string]any{"operation_id": "GetCustomers", "account": "dummy-one", "params": map[string]any{"limit": "5"}}, "whole number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantError(t, call(t, cs, "api_read", tc.args), tc.want)
		})
	}
}

func TestAPIReadRequiredQueryParam(t *testing.T) {
	var required string
	for _, id := range []string{"GetCustomersSearch", "GetInvoicesSearch", "GetChargesSearch"} {
		if op, ok := stripe.LookupOperation(id); ok && slices.ContainsFunc(op.QueryParams, func(p stripe.Param) bool { return p.Required }) {
			required = id
			break
		}
	}

	if required == "" {
		t.Skip("no search operation with a required query parameter in the index")
	}

	cs := connect(t, "sk_org_test_dummy", noStripe(t))
	wantError(t, call(t, cs, "api_read", map[string]any{"operation_id": required, "account": "dummy-one"}), "needs query parameter")
	wantError(t, call(t, cs, "api_read", map[string]any{"operation_id": required, "account": "dummy-one", "params": map[string]any{"query": ""}}), "needs query parameter")
}

func TestAPIReadStripeErrors(t *testing.T) {
	cs := connect(t, "sk_org_test_dummy", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/balance" {
			w.Write([]byte(`{"pad":"` + strings.Repeat("x", stripe.MaxResponseBytes) + `"}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"resource_missing","message":"No such customer: 'cus_404'"}}`))
	})

	wantError(t, call(t, cs, "api_read", map[string]any{"operation_id": "GetBalance", "account": "dummy-one"}), "smaller limit")
	wantError(t, call(t, cs, "api_read", map[string]any{
		"operation_id": "GetCustomersCustomer",
		"account":      "dummy-one",
		"path_params":  map[string]any{"customer": "cus_404"},
	}), "No such customer")
}
