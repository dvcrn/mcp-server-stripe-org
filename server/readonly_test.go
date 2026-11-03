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

const testReadToken = "dummy-read-token"

func varsGetenv(vars map[string]string) func(string) string {
	base := map[string]string{EnvOrgKey: "sk_org_test_dummy", EnvAccounts: testAccounts}
	for k, v := range vars {
		base[k] = v
	}

	return func(name string) string { return base[name] }
}

// listToolsHTTP returns the tools/list result the handler gives token.
func listToolsHTTP(t *testing.T, h http.Handler, token string) []*mcp.Tool {
	t.Helper()

	rec := postMCP(t, h, map[string]string{"Authorization": "Bearer " + token}, listToolsBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	var resp struct {
		Result mcp.ListToolsResult `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}

	return resp.Result.Tools
}

func toolNames(tools []*mcp.Tool) []string {
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}
	slices.Sort(names)

	return names
}

func wantReadOnlyTools(t *testing.T, tools []*mcp.Tool) {
	t.Helper()

	for _, tool := range tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("read-only server lists %s, which is not annotated read-only", tool.Name)
		}

		if slices.Contains(writeToolNames, tool.Name) {
			t.Errorf("read-only server lists write tool %s", tool.Name)
		}
	}

	if got, want := toolNames(tools), toolNames(fullReadTools(t)); !slices.Equal(got, want) {
		t.Errorf("tools = %v, want %v", got, want)
	}
}

// fullReadTools returns the full server's tools annotated read-only.
func fullReadTools(t *testing.T) []*mcp.Tool {
	t.Helper()

	all := listToolsHTTP(t, Handler(testGetenv(testToken)), testToken)

	return slices.DeleteFunc(all, func(tool *mcp.Tool) bool { return tool.Annotations == nil || !tool.Annotations.ReadOnlyHint })
}

func TestHTTPAccessLevels(t *testing.T) {
	both := Handler(varsGetenv(map[string]string{EnvToken: testToken, EnvReadToken: testReadToken}))

	full := listToolsHTTP(t, both, testToken)
	if len(full) != 22 {
		t.Errorf("full token lists %d tools, want 22: %v", len(full), toolNames(full))
	}

	read := listToolsHTTP(t, both, testReadToken)
	wantReadOnlyTools(t, read)
	if !slices.Contains(toolNames(read), "run_sql_query") {
		t.Errorf("read-only tools = %v, want run_sql_query among them", toolNames(read))
	}

	if got := listToolsHTTP(t, both, testToken); len(got) != len(full) {
		t.Error("a read-token request changed what the full token gets")
	}

	rec := postMCP(t, both, map[string]string{"X-API-Key": testReadToken}, listToolsBody)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"cancel_subscription"`) {
		t.Errorf("read token via X-API-Key: %d %s", rec.Code, rec.Body)
	}
}

func TestHTTPTokenConfig(t *testing.T) {
	for _, tc := range []struct {
		name  string
		vars  map[string]string
		token string
		want  int
	}{
		{name: "wrong token", vars: map[string]string{EnvToken: testToken, EnvReadToken: testReadToken}, token: "wrong", want: http.StatusUnauthorized},
		{name: "neither set", vars: nil, token: testToken, want: http.StatusInternalServerError},
		{name: "same value", vars: map[string]string{EnvToken: testToken, EnvReadToken: testToken}, token: testToken, want: http.StatusInternalServerError},
		{name: "same value after trim", vars: map[string]string{EnvToken: testToken, EnvReadToken: " " + testToken + "\n"}, token: testToken, want: http.StatusInternalServerError},
		{name: "only read token, read token", vars: map[string]string{EnvReadToken: testReadToken}, token: testReadToken, want: http.StatusOK},
		{name: "only read token, empty bearer", vars: map[string]string{EnvReadToken: testReadToken}, token: "", want: http.StatusUnauthorized},
		{name: "only full token, read token", vars: map[string]string{EnvToken: testToken}, token: testReadToken, want: http.StatusUnauthorized},
		{name: "read-only flag caps full token", vars: map[string]string{EnvToken: testToken, EnvReadOnly: "1"}, token: testToken, want: http.StatusOK},
		{name: "bad read-only flag", vars: map[string]string{EnvToken: testToken, EnvReadOnly: "yes"}, token: testToken, want: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := Handler(varsGetenv(tc.vars))
			if tc.want != http.StatusOK {
				rec := postMCP(t, h, map[string]string{"Authorization": "Bearer " + tc.token}, listToolsBody)
				if rec.Code != tc.want {
					t.Errorf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body)
				}

				return
			}

			wantReadOnlyTools(t, listToolsHTTP(t, h, tc.token))
		})
	}
}

func TestHTTPReadTokenCannotCallWriteTool(t *testing.T) {
	h := Handler(varsGetenv(map[string]string{EnvToken: testToken, EnvReadToken: testReadToken}))

	rec := postMCP(t, h, map[string]string{"Authorization": "Bearer " + testReadToken},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"cancel_subscription","arguments":{"account":"dummy-one","subscription":"sub_dummy"}}}`)
	if !strings.Contains(rec.Body.String(), `unknown tool \"cancel_subscription\"`) {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestReadOnlyServerCannotCallWriteTool(t *testing.T) {
	srv := httptest.NewServer(noStripe(t))
	t.Cleanup(srv.Close)

	env, err := NewEnv("sk_org_test_dummy", testAccounts, srv.Client(), stripe.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}

	cs := connectServer(t, NewReadOnly(), WithEnv(context.Background(), env))

	for _, name := range writeToolNames {
		_, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      name,
			Arguments: map[string]any{"account": "dummy-one", "subscription": "sub_dummy"},
		})
		if err == nil || !strings.Contains(err.Error(), "unknown tool") {
			t.Errorf("%s: err = %v, want unknown tool", name, err)
		}
	}
}

func TestStdioServer(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    *mcp.Server
		wantErr bool
	}{
		{"", New(), false},
		{"0", New(), false},
		{"false", New(), false},
		{"1", NewReadOnly(), false},
		{" true\n", NewReadOnly(), false},
		{"yes", nil, true},
	} {
		got, err := StdioServer(varsGetenv(map[string]string{EnvReadOnly: tc.value}))
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("%s=%q: got %p, %v", EnvReadOnly, tc.value, got, err)
		}
	}

	srv, err := StdioServer(varsGetenv(map[string]string{EnvReadOnly: "1"}))
	if err != nil {
		t.Fatal(err)
	}

	res, err := connectServer(t, srv, context.Background()).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	wantReadOnlyTools(t, res.Tools)
}
