package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Dummy values, not real Stripe credentials or accounts.
const (
	testKey     = "sk_org_test_dummykey"
	testAccount = "acct_dummy123"
)

type captured struct {
	method string
	path   string
	query  string
	header http.Header
	body   string
}

// newTestClient returns a client whose requests are recorded and answered
// with the given status and body.
func newTestClient(t *testing.T, status int, respBody string) (*Client, *captured) {
	t.Helper()

	got := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*got = captured{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), string(body)}

		w.Header().Set("Request-Id", "req_dummy")
		w.WriteHeader(status)
		io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)

	return New(srv.Client(), testKey, WithBaseURL(srv.URL)), got
}

func TestDoV1Get(t *testing.T) {
	c, got := newTestClient(t, http.StatusOK, `{"object":"balance"}`)

	body, err := c.DoV1(context.Background(), V1Request{
		Method:         http.MethodGet,
		Path:           "/v1/customers",
		Account:        testAccount,
		Params:         map[string]any{"limit": 10, "expand": []string{"data.default_source"}},
		IdempotencyKey: "dummy-key",
	})
	if err != nil {
		t.Fatalf("DoV1: %v", err)
	}

	if string(body) != `{"object":"balance"}` {
		t.Errorf("body = %s", body)
	}

	if got.method != http.MethodGet || got.path != "/v1/customers" {
		t.Errorf("request = %s %s", got.method, got.path)
	}

	if got.query != "expand%5B0%5D=data.default_source&limit=10" {
		t.Errorf("query = %q", got.query)
	}

	if got.body != "" {
		t.Errorf("GET sent a body: %q", got.body)
	}

	wantHeaders := map[string]string{
		"Authorization":   "Bearer " + testKey,
		"Stripe-Version":  APIVersion,
		"Stripe-Context":  testAccount,
		"Idempotency-Key": "",
		"Content-Type":    "",
	}
	checkHeaders(t, got.header, wantHeaders)
}

func TestDoV1Post(t *testing.T) {
	c, got := newTestClient(t, http.StatusOK, `{}`)

	_, err := c.DoV1(context.Background(), V1Request{
		Method:         http.MethodPost,
		Path:           "/v1/customers/cus_dummy",
		Account:        testAccount,
		Params:         map[string]any{"metadata": map[string]string{"old": ""}, "name": "Dummy"},
		IdempotencyKey: "dummy-key",
	})
	if err != nil {
		t.Fatalf("DoV1: %v", err)
	}

	if got.body != "metadata%5Bold%5D=&name=Dummy" {
		t.Errorf("body = %q", got.body)
	}

	if got.query != "" {
		t.Errorf("POST sent a query: %q", got.query)
	}

	checkHeaders(t, got.header, map[string]string{
		"Stripe-Version":  APIVersion,
		"Stripe-Context":  testAccount,
		"Idempotency-Key": "dummy-key",
		"Content-Type":    "application/x-www-form-urlencoded",
	})
}

func TestDoV1DeleteHasNoIdempotencyKey(t *testing.T) {
	c, got := newTestClient(t, http.StatusOK, `{}`)

	_, err := c.DoV1(context.Background(), V1Request{
		Method:         http.MethodDelete,
		Path:           "/v1/subscriptions/sub_dummy",
		Account:        testAccount,
		Params:         map[string]any{"invoice_now": true},
		IdempotencyKey: "dummy-key",
	})
	if err != nil {
		t.Fatalf("DoV1: %v", err)
	}

	if got.method != http.MethodDelete || got.query != "invoice_now=true" || got.body != "" {
		t.Errorf("request = %s ?%s body %q", got.method, got.query, got.body)
	}

	checkHeaders(t, got.header, map[string]string{"Idempotency-Key": ""})
}

func TestDoV1RejectsBadRequests(t *testing.T) {
	c, got := newTestClient(t, http.StatusOK, `{}`)

	tests := []struct {
		name string
		req  V1Request
		want string
	}{
		{"missing account", V1Request{Method: http.MethodGet, Path: "/v1/balance"}, "need an account"},
		{"malformed account", V1Request{Method: http.MethodGet, Path: "/v1/balance", Account: "acct_x\r\nX-Evil: 1"}, "invalid account"},
		{"not an account", V1Request{Method: http.MethodGet, Path: "/v1/balance", Account: "cus_dummy"}, "invalid account"},
		{"v2 path", V1Request{Method: http.MethodGet, Path: "/v2/core/events", Account: testAccount}, "must start with /v1/"},
		{"method", V1Request{Method: http.MethodPut, Path: "/v1/balance", Account: testAccount}, "unsupported method"},
		{"dot-dot segment", V1Request{Method: http.MethodGet, Path: "/v1/customers/..", Account: testAccount}, "invalid path"},
		{"escaped dot-dot segment", V1Request{Method: http.MethodGet, Path: "/v1/customers/%2e%2E/x", Account: testAccount}, "invalid path"},
		{"dot segment", V1Request{Method: http.MethodGet, Path: "/v1/./balance", Account: testAccount}, "invalid path"},
		{"empty segment", V1Request{Method: http.MethodGet, Path: "/v1//balance", Account: testAccount}, "invalid path"},
		{"query in path", V1Request{Method: http.MethodGet, Path: "/v1/customers/cus_x?limit=100", Account: testAccount}, "invalid path"},
		{"fragment in path", V1Request{Method: http.MethodGet, Path: "/v1/customers/cus_x#a", Account: testAccount}, "invalid path"},
		{"control character", V1Request{Method: http.MethodGet, Path: "/v1/customers/cus\n", Account: testAccount}, "invalid path"},
		{"bad escape", V1Request{Method: http.MethodGet, Path: "/v1/customers/%zz", Account: testAccount}, "invalid path"},
		{"idempotency key newline", V1Request{Method: http.MethodPost, Path: "/v1/coupons", Account: testAccount, IdempotencyKey: "a\r\nX-Evil: 1"}, "idempotency key"},
		{"idempotency key too long", V1Request{Method: http.MethodPost, Path: "/v1/coupons", Account: testAccount, IdempotencyKey: strings.Repeat("k", 256)}, "idempotency key"},
		{"bad params", V1Request{Method: http.MethodPost, Path: "/v1/coupons", Account: testAccount, Params: map[string]any{"metadata": map[string]string{"a[b]": "c"}}}, "invalid key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.DoV1(context.Background(), tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error containing %q", err, tt.want)
			}
		})
	}

	if got.method != "" {
		t.Errorf("a rejected request reached the server: %s %s", got.method, got.path)
	}
}

func TestDoV2OrgMode(t *testing.T) {
	c, got := newTestClient(t, http.StatusOK, `{"id":"qrun_dummy"}`)

	_, err := c.DoV2(context.Background(), V2Request{
		Method:         http.MethodPost,
		Path:           "/v2/data/query_runs",
		Version:        PreviewAPIVersion,
		Body:           map[string]any{"query": map[string]string{"sql": "SELECT 1"}},
		IdempotencyKey: "dummy-key",
	})
	if err != nil {
		t.Fatalf("DoV2: %v", err)
	}

	if _, ok := got.header["Stripe-Context"]; ok {
		t.Errorf("Stripe-Context sent in org mode: %q", got.header.Get("Stripe-Context"))
	}

	checkHeaders(t, got.header, map[string]string{
		"Authorization":   "Bearer " + testKey,
		"Stripe-Version":  PreviewAPIVersion,
		"Content-Type":    "application/json",
		"Idempotency-Key": "dummy-key",
	})

	var body map[string]map[string]string
	if err := json.Unmarshal([]byte(got.body), &body); err != nil || body["query"]["sql"] != "SELECT 1" {
		t.Errorf("body = %q (%v)", got.body, err)
	}
}

func TestDoV2GetWithAccount(t *testing.T) {
	c, got := newTestClient(t, http.StatusOK, `{}`)

	_, err := c.DoV2(context.Background(), V2Request{
		Method:         http.MethodGet,
		Path:           "/v2/data/query_runs/qrun_dummy",
		Account:        testAccount,
		Query:          map[string]any{"include": []string{"result.inline"}},
		IdempotencyKey: "dummy-key",
	})
	if err != nil {
		t.Fatalf("DoV2: %v", err)
	}

	if got.query != "include%5B0%5D=result.inline" || got.body != "" {
		t.Errorf("query = %q, body = %q", got.query, got.body)
	}

	checkHeaders(t, got.header, map[string]string{
		"Stripe-Version":  APIVersion,
		"Stripe-Context":  testAccount,
		"Idempotency-Key": "",
		"Content-Type":    "",
	})
}

func TestDoV2RejectsBadRequests(t *testing.T) {
	c, _ := newTestClient(t, http.StatusOK, `{}`)

	if _, err := c.DoV2(context.Background(), V2Request{Method: http.MethodGet, Path: "/v1/balance"}); err == nil {
		t.Error("v1 path accepted")
	}

	if _, err := c.DoV2(context.Background(), V2Request{Method: http.MethodGet, Path: "/v2/core/events", Account: "bad"}); err == nil {
		t.Error("malformed account accepted")
	}

	if _, err := c.DoV2(context.Background(), V2Request{Method: http.MethodGet, Path: "/v2/core/events", Body: map[string]any{"a": 1}}); err == nil {
		t.Error("body on GET accepted")
	}

	if _, err := c.DoV2(context.Background(), V2Request{Method: http.MethodGet, Path: "/v2/core/events/.."}); err == nil {
		t.Error("dot-dot path accepted")
	}
}

func TestTransportErrorOmitsQuery(t *testing.T) {
	c := New(&http.Client{}, testKey, WithBaseURL("http://127.0.0.1:1"))
	_, err := c.DoV1(context.Background(), V1Request{
		Method:  http.MethodGet,
		Path:    "/v1/customers",
		Account: testAccount,
		Params:  map[string]any{"email": "dummy@example.com"},
	})
	if err == nil {
		t.Fatal("expected a connection error")
	}

	if strings.Contains(err.Error(), "dummy") || strings.Contains(err.Error(), testKey) {
		t.Errorf("error leaks request data: %v", err)
	}

	if !strings.Contains(err.Error(), "GET /v1/customers") {
		t.Errorf("error lacks method and path: %v", err)
	}
}

func TestResponseSizeCap(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		tooBig  bool
		request func(*Client) ([]byte, error)
	}{
		{"v1 at limit", MaxResponseBytes, false, getBalance},
		{"v1 over limit", MaxResponseBytes + 1, true, getBalance},
		{"v2 over limit", MaxResponseBytes + 1, true, getQueryRun},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, http.StatusOK, strings.Repeat("x", tt.size))
			body, err := tt.request(c)

			var tooLarge *ResponseTooLargeError
			if tt.tooBig {
				if !errors.As(err, &tooLarge) {
					t.Fatalf("got %v, want ResponseTooLargeError", err)
				}

				if body != nil {
					t.Errorf("returned %d bytes alongside the error", len(body))
				}

				if !strings.Contains(err.Error(), "smaller limit or narrower filters") {
					t.Errorf("error does not tell the agent what to do: %v", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("DoV1: %v", err)
			}

			if len(body) != tt.size {
				t.Errorf("got %d bytes, want %d", len(body), tt.size)
			}
		})
	}
}

func TestOversizedErrorKeepsStatus(t *testing.T) {
	c, _ := newTestClient(t, http.StatusBadRequest, strings.Repeat("x", MaxResponseBytes+1))
	_, err := getBalance(c)

	var se *Error
	if !errors.As(err, &se) || se.Status != http.StatusBadRequest {
		t.Fatalf("got %v, want a 400 *Error", err)
	}
}

func getBalance(c *Client) ([]byte, error) {
	return c.DoV1(context.Background(), V1Request{Method: http.MethodGet, Path: "/v1/balance", Account: testAccount})
}

func getQueryRun(c *Client) ([]byte, error) {
	return c.DoV2(context.Background(), V2Request{Method: http.MethodGet, Path: "/v2/data/query_runs/qrun_dummy"})
}

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   Error
		text   string
	}{
		{
			"v1 invalid request",
			http.StatusNotFound,
			`{"error":{"type":"invalid_request_error","code":"resource_missing","param":"id","message":"No such customer: 'cus_dummy'","request_log_url":"https://dashboard.stripe.com/logs/req_dummy"}}`,
			Error{Status: 404, Type: "invalid_request_error", Code: "resource_missing", Param: "id", Message: "No such customer: 'cus_dummy'", RequestID: "req_dummy"},
			"stripe: HTTP 404 invalid_request_error resource_missing: No such customer: 'cus_dummy' (param: id, request: req_dummy)",
		},
		{
			"v1 card error",
			http.StatusPaymentRequired,
			`{"error":{"type":"card_error","code":"card_declined","decline_code":"insufficient_funds","message":"Your card has insufficient funds."}}`,
			Error{Status: 402, Type: "card_error", Code: "card_declined", DeclineCode: "insufficient_funds", Message: "Your card has insufficient funds.", RequestID: "req_dummy"},
			"stripe: HTTP 402 card_error card_declined insufficient_funds: Your card has insufficient funds. (request: req_dummy)",
		},
		{
			"v2 without type",
			http.StatusNotFound,
			`{"error":{"code":"not_found","message":"The API method cannot be found."}}`,
			Error{Status: 404, Code: "not_found", Message: "The API method cannot be found.", RequestID: "req_dummy"},
			"stripe: HTTP 404 not_found: The API method cannot be found. (request: req_dummy)",
		},
		{
			"permission denied",
			http.StatusForbidden,
			`{"error":{"type":"invalid_request_error","message":"The provided key does not have the required permissions for this endpoint."}}`,
			Error{Status: 403, Type: "invalid_request_error", Message: "The provided key does not have the required permissions for this endpoint.", RequestID: "req_dummy"},
			"stripe: HTTP 403 invalid_request_error: The provided key does not have the required permissions for this endpoint. (request: req_dummy)",
		},
		{
			"non-JSON body",
			http.StatusBadGateway,
			"<html>upstream unavailable</html>",
			Error{Status: 502, Message: "Bad Gateway: <html>upstream unavailable</html>", RequestID: "req_dummy"},
			"stripe: HTTP 502: Bad Gateway: <html>upstream unavailable</html> (request: req_dummy)",
		},
		{
			"empty body",
			http.StatusServiceUnavailable,
			"",
			Error{Status: 503, Message: "Service Unavailable", RequestID: "req_dummy"},
			"stripe: HTTP 503: Service Unavailable (request: req_dummy)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, tt.status, tt.body)
			_, err := getBalance(c)

			var se *Error
			if !errors.As(err, &se) {
				t.Fatalf("got %T %v, want *Error", err, err)
			}

			if *se != tt.want {
				t.Errorf("got %+v, want %+v", *se, tt.want)
			}

			if err.Error() != tt.text {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.text)
			}

			if strings.Contains(err.Error(), testKey) {
				t.Error("error leaks the API key")
			}
		})
	}
}

func TestNonJSONErrorIsTruncated(t *testing.T) {
	c, _ := newTestClient(t, http.StatusInternalServerError, strings.Repeat("é", 300))
	_, err := getBalance(c)

	var se *Error
	if !errors.As(err, &se) {
		t.Fatalf("got %v", err)
	}

	if len(se.Message) > 250 || !strings.HasSuffix(se.Message, "...") {
		t.Errorf("message not truncated: %d bytes", len(se.Message))
	}

	if !strings.Contains(se.Message, "é") || strings.ContainsRune(se.Message, '�') {
		t.Errorf("truncation broke UTF-8: %q", se.Message)
	}
}

func checkHeaders(t *testing.T, h http.Header, want map[string]string) {
	t.Helper()

	for name, value := range want {
		if got := h.Get(name); got != value {
			t.Errorf("header %s = %q, want %q", name, got, value)
		}
	}
}

func TestValidAccountID(t *testing.T) {
	for id, want := range map[string]bool{
		"acct_dummy123":   true,
		"acct_":           false,
		"cus_dummy123":    false,
		"acct_dummy 1":    false,
		"acct_dummy\r\n1": false,
		"":                false,
	} {
		if got := ValidAccountID(id); got != want {
			t.Errorf("ValidAccountID(%q) = %v, want %v", id, got, want)
		}
	}
}
