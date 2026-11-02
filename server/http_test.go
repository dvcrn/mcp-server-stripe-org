package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "dummy-token"

func testGetenv(token string) func(string) string {
	vars := map[string]string{EnvToken: token, EnvOrgKey: "sk_org_test_dummy", EnvAccounts: testAccounts}
	return func(name string) string { return vars[name] }
}

const listToolsBody = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

func postMCP(t *testing.T, h http.Handler, header map[string]string, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range header {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestHealthNeedsNoAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(testGetenv("")).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Errorf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestAuth(t *testing.T) {
	for _, tc := range []struct {
		name   string
		token  string
		header map[string]string
		want   int
	}{
		{"token unset", "", map[string]string{"Authorization": "Bearer "}, http.StatusInternalServerError},
		{"token blank", "  ", map[string]string{"Authorization": "Bearer   "}, http.StatusInternalServerError},
		{"missing", testToken, nil, http.StatusUnauthorized},
		{"wrong", testToken, map[string]string{"Authorization": "Bearer wrong"}, http.StatusUnauthorized},
		{"prefix", testToken, map[string]string{"Authorization": "Bearer dummy"}, http.StatusUnauthorized},
		{"wrong scheme", testToken, map[string]string{"Authorization": "Basic " + testToken}, http.StatusUnauthorized},
		{"bearer", testToken, map[string]string{"Authorization": "Bearer " + testToken}, http.StatusOK},
		{"bearer lowercase", testToken, map[string]string{"Authorization": "bearer " + testToken}, http.StatusOK},
		{"api key", testToken, map[string]string{"X-API-Key": testToken}, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := postMCP(t, Handler(testGetenv(tc.token)), tc.header, listToolsBody)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body)
			}

			if tc.want == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("WWW-Authenticate = %q", rec.Header().Get("WWW-Authenticate"))
			}

			if tc.want == http.StatusOK && !strings.Contains(rec.Body.String(), `"api_read"`) {
				t.Errorf("tools/list response lacks api_read: %s", rec.Body)
			}
		})
	}
}

func TestHTTPToolCallUsesEnv(t *testing.T) {
	rec := postMCP(t, Handler(testGetenv(testToken)), map[string]string{"Authorization": "Bearer " + testToken},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_accounts","arguments":{}}}`)

	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || !strings.Contains(string(body), "acct_1DUMMYTWO") || strings.Contains(string(body), `"isError":true`) {
		t.Errorf("got %d %s", rec.Code, body)
	}
}

func TestHTTPBadConfigIs500(t *testing.T) {
	vars := map[string]string{EnvToken: testToken, EnvOrgKey: "sk_test_notanorgkey", EnvAccounts: testAccounts}

	rec := postMCP(t, Handler(func(name string) string { return vars[name] }), map[string]string{"Authorization": "Bearer " + testToken}, listToolsBody)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestEnvCacheReloadsOnChange(t *testing.T) {
	vars := map[string]string{EnvOrgKey: "sk_org_test_dummy", EnvAccounts: testAccounts}
	getenv := func(name string) string { return vars[name] }
	cache := &envCache{}

	first, err := cache.get(getenv)
	if err != nil {
		t.Fatal(err)
	}

	if again, _ := cache.get(getenv); again != first {
		t.Error("unchanged variables built a new Env")
	}

	vars[EnvOrgKey] = "sk_org_live_dummy"
	changed, err := cache.get(getenv)
	if err != nil {
		t.Fatal(err)
	}

	if changed == first || !changed.Livemode {
		t.Error("changed key did not rebuild the Env")
	}
}
