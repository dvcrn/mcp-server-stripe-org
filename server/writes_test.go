package server

import (
	"context"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

var writeToolNames = []string{
	"cancel_subscription", "create_coupon", "create_promotion_code", "create_refund", "delete_coupon",
	"finalize_invoice", "resume_subscription", "send_invoice", "update_customer", "update_feature",
	"update_price", "update_product", "update_promotion_code", "update_subscription", "void_invoice",
}

var destructiveTools = []string{"cancel_subscription", "create_refund", "delete_coupon", "void_invoice"}

type writeCall struct {
	method      string
	path        string
	form        url.Values
	idempotency string
	context     string
	contentType string
}

// recordWrites answers every request with body and records what was sent.
func recordWrites(t *testing.T, calls *[]writeCall, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}

		form, err := url.ParseQuery(string(raw))
		if err != nil {
			t.Error(err)
		}

		if r.URL.RawQuery != "" {
			t.Errorf("%s %s has a query string %q", r.Method, r.URL.Path, r.URL.RawQuery)
		}

		*calls = append(*calls, writeCall{
			method:      r.Method,
			path:        r.URL.EscapedPath(),
			form:        form,
			idempotency: r.Header.Get("Idempotency-Key"),
			context:     r.Header.Get("Stripe-Context"),
			contentType: r.Header.Get("Content-Type"),
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}
}

func TestWriteAnnotations(t *testing.T) {
	cs := connect(t, "sk_org_test_dummy", noStripe(t))

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, tool := range res.Tools {
		if !slices.Contains(writeToolNames, tool.Name) {
			continue
		}

		a := tool.Annotations
		if a == nil || a.ReadOnlyHint || a.DestructiveHint == nil {
			t.Errorf("%s: annotations %+v, want a write with an explicit destructive hint", tool.Name, a)
			continue
		}

		destructive := slices.Contains(destructiveTools, tool.Name)
		if *a.DestructiveHint != destructive {
			t.Errorf("%s: destructive = %v, want %v", tool.Name, *a.DestructiveHint, destructive)
		}

		if destructive && (a.IdempotentHint || !strings.Contains(tool.Description, "confirm with the user first")) {
			t.Errorf("%s: destructive tools must not be idempotent and must ask for confirmation: %q", tool.Name, tool.Description)
		}

		props := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		required := tool.InputSchema.(map[string]any)["required"]
		if _, ok := props["account"]; !ok || !slices.Contains(required.([]any), any("account")) {
			t.Errorf("%s: account is not a required input: %v", tool.Name, required)
		}

		_, hasKey := props["idempotency_key"]
		if wantKey := tool.Name != "delete_coupon"; hasKey != wantKey {
			t.Errorf("%s: has idempotency_key = %v, want %v", tool.Name, hasKey, wantKey)
		}
	}
}

func TestWriteRequests(t *testing.T) {
	for _, tc := range []struct {
		tool       string
		args       map[string]any
		wantMethod string
		wantPath   string
		wantForm   url.Values
	}{
		{
			tool: "update_customer",
			args: map[string]any{
				"customer": "cus_123", "email": "dummy@example.com", "name": "", "description": "Dummy",
				"metadata": map[string]any{"keep": "1", "drop": ""},
			},
			wantMethod: "POST",
			wantPath:   "/v1/customers/cus_123",
			wantForm: url.Values{
				"email": {"dummy@example.com"}, "name": {""}, "description": {"Dummy"},
				"metadata[keep]": {"1"}, "metadata[drop]": {""},
			},
		},
		{
			tool:       "finalize_invoice",
			args:       map[string]any{"invoice": "in_123", "auto_advance": false},
			wantMethod: "POST",
			wantPath:   "/v1/invoices/in_123/finalize",
			wantForm:   url.Values{"auto_advance": {"false"}},
		},
		{
			tool:       "finalize_invoice",
			args:       map[string]any{"invoice": "in_123"},
			wantMethod: "POST",
			wantPath:   "/v1/invoices/in_123/finalize",
			wantForm:   url.Values{},
		},
		{
			tool:       "send_invoice",
			args:       map[string]any{"invoice": "in_123"},
			wantMethod: "POST",
			wantPath:   "/v1/invoices/in_123/send",
			wantForm:   url.Values{},
		},
		{
			tool:       "void_invoice",
			args:       map[string]any{"invoice": "in_123"},
			wantMethod: "POST",
			wantPath:   "/v1/invoices/in_123/void",
			wantForm:   url.Values{},
		},
		{
			tool:       "cancel_subscription",
			args:       map[string]any{"subscription": "sub_123"},
			wantMethod: "DELETE",
			wantPath:   "/v1/subscriptions/sub_123",
			wantForm:   url.Values{},
		},
		{
			tool:       "cancel_subscription",
			args:       map[string]any{"subscription": "sub_123", "at_period_end": true},
			wantMethod: "POST",
			wantPath:   "/v1/subscriptions/sub_123",
			wantForm:   url.Values{"cancel_at_period_end": {"true"}},
		},
		{
			tool:       "resume_subscription",
			args:       map[string]any{"subscription": "sub_123"},
			wantMethod: "POST",
			wantPath:   "/v1/subscriptions/sub_123",
			wantForm:   url.Values{"cancel_at_period_end": {"false"}},
		},
		{
			tool:       "update_subscription",
			args:       map[string]any{"subscription": "sub_123", "trial_end": "now", "metadata": map[string]any{"k": ""}},
			wantMethod: "POST",
			wantPath:   "/v1/subscriptions/sub_123",
			wantForm:   url.Values{"trial_end": {"now"}, "metadata[k]": {""}},
		},
		{
			tool:       "update_subscription",
			args:       map[string]any{"subscription": "sub_123", "trial_end": "1767225600"},
			wantMethod: "POST",
			wantPath:   "/v1/subscriptions/sub_123",
			wantForm:   url.Values{"trial_end": {"1767225600"}},
		},
		{
			tool: "create_coupon",
			args: map[string]any{
				"id": "DUMMY25", "name": "Dummy", "percent_off": 12.5, "duration": "repeating", "duration_in_months": 3,
				"max_redemptions": 10, "redeem_by": 1767225600, "metadata": map[string]any{"campaign": "dummy"},
			},
			wantMethod: "POST",
			wantPath:   "/v1/coupons",
			wantForm: url.Values{
				"id": {"DUMMY25"}, "name": {"Dummy"}, "percent_off": {"12.5"}, "duration": {"repeating"},
				"duration_in_months": {"3"}, "max_redemptions": {"10"}, "redeem_by": {"1767225600"}, "metadata[campaign]": {"dummy"},
			},
		},
		{
			tool:       "create_coupon",
			args:       map[string]any{"amount_off": 500, "currency": "usd"},
			wantMethod: "POST",
			wantPath:   "/v1/coupons",
			wantForm:   url.Values{"amount_off": {"500"}, "currency": {"usd"}},
		},
		{
			tool:       "delete_coupon",
			args:       map[string]any{"coupon": "Dummy-25_x"},
			wantMethod: "DELETE",
			wantPath:   "/v1/coupons/Dummy-25_x",
			wantForm:   url.Values{},
		},
		{
			tool: "create_promotion_code",
			args: map[string]any{
				"coupon": "DUMMY25", "code": "DUMMYCODE", "customer": "cus_123", "max_redemptions": 5,
				"expires_at": 1767225600, "metadata": map[string]any{"k": "v"},
			},
			wantMethod: "POST",
			wantPath:   "/v1/promotion_codes",
			wantForm: url.Values{
				"promotion[type]": {"coupon"}, "promotion[coupon]": {"DUMMY25"}, "code": {"DUMMYCODE"}, "customer": {"cus_123"},
				"max_redemptions": {"5"}, "expires_at": {"1767225600"}, "metadata[k]": {"v"},
			},
		},
		{
			tool:       "update_promotion_code",
			args:       map[string]any{"promotion_code": "promo_123", "active": false, "metadata": map[string]any{"k": ""}},
			wantMethod: "POST",
			wantPath:   "/v1/promotion_codes/promo_123",
			wantForm:   url.Values{"active": {"false"}, "metadata[k]": {""}},
		},
		{
			tool:       "create_refund",
			args:       map[string]any{"payment_intent": "pi_123", "amount": 250, "reason": "requested_by_customer", "metadata": map[string]any{"ticket": "1"}},
			wantMethod: "POST",
			wantPath:   "/v1/refunds",
			wantForm:   url.Values{"payment_intent": {"pi_123"}, "amount": {"250"}, "reason": {"requested_by_customer"}, "metadata[ticket]": {"1"}},
		},
		{
			tool:       "create_refund",
			args:       map[string]any{"charge": "ch_123"},
			wantMethod: "POST",
			wantPath:   "/v1/refunds",
			wantForm:   url.Values{"charge": {"ch_123"}},
		},
		{
			tool: "update_product",
			args: map[string]any{
				"product": "prod_123", "name": "Dummy", "description": "", "active": false,
				"default_price": "price_123", "metadata": map[string]any{"tier": "pro"},
			},
			wantMethod: "POST",
			wantPath:   "/v1/products/prod_123",
			wantForm: url.Values{
				"name": {"Dummy"}, "description": {""}, "active": {"false"}, "default_price": {"price_123"}, "metadata[tier]": {"pro"},
			},
		},
		{
			tool:       "update_price",
			args:       map[string]any{"price": "price_123", "active": true, "nickname": "Dummy", "lookup_key": "", "metadata": map[string]any{"a": "b"}},
			wantMethod: "POST",
			wantPath:   "/v1/prices/price_123",
			wantForm:   url.Values{"active": {"true"}, "nickname": {"Dummy"}, "lookup_key": {""}, "metadata[a]": {"b"}},
		},
		{
			tool:       "update_feature",
			args:       map[string]any{"feature": "feat_123", "name": "Dummy", "active": false, "metadata": map[string]any{"a": ""}},
			wantMethod: "POST",
			wantPath:   "/v1/entitlements/features/feat_123",
			wantForm:   url.Values{"name": {"Dummy"}, "active": {"false"}, "metadata[a]": {""}},
		},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			var calls []writeCall
			cs := connect(t, "sk_org_test_dummy", recordWrites(t, &calls, `{"id":"dummy"}`))

			args := maps.Clone(tc.args)
			args["account"] = "dummy-two"
			if tc.wantMethod == "POST" {
				args["idempotency_key"] = "dummy-key-1"
			}

			if got := wantOK(t, call(t, cs, tc.tool, args)); got != `{"id":"dummy"}` {
				t.Errorf("text = %s", got)
			}

			if len(calls) != 1 {
				t.Fatalf("got %d Stripe calls, want 1", len(calls))
			}

			c := calls[0]
			if c.method != tc.wantMethod || c.path != tc.wantPath || c.context != "acct_1DUMMYTWO" {
				t.Errorf("got %s %s context %q, want %s %s context acct_1DUMMYTWO", c.method, c.path, c.context, tc.wantMethod, tc.wantPath)
			}

			if !maps.EqualFunc(c.form, tc.wantForm, slices.Equal) {
				t.Errorf("form = %v, want %v", c.form, tc.wantForm)
			}

			wantKey, wantType := "", ""
			if tc.wantMethod == "POST" {
				wantKey, wantType = "dummy-key-1", "application/x-www-form-urlencoded"
			}

			if c.idempotency != wantKey || c.contentType != wantType {
				t.Errorf("Idempotency-Key %q Content-Type %q, want %q %q", c.idempotency, c.contentType, wantKey, wantType)
			}
		})
	}
}

func TestWriteWithoutIdempotencyKey(t *testing.T) {
	var calls []writeCall
	cs := connect(t, "sk_org_test_dummy", recordWrites(t, &calls, `{}`))

	wantOK(t, call(t, cs, "void_invoice", map[string]any{"account": "acct_1DUMMYONE", "invoice": "in_123"}))

	if len(calls) != 1 || calls[0].idempotency != "" || calls[0].context != "acct_1DUMMYONE" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestWriteValidation(t *testing.T) {
	cs := connect(t, "sk_org_test_dummy", noStripe(t))

	for _, tc := range []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{"missing account", "void_invoice", map[string]any{"invoice": "in_123"}, `missing properties: ["account"]`},
		{"empty account", "void_invoice", map[string]any{"account": "", "invoice": "in_123"}, "account is required"},
		{"unknown account", "void_invoice", map[string]any{"account": "nope", "invoice": "in_123"}, "dummy-one, dummy-two"},
		{"unlisted acct_ ID", "void_invoice", map[string]any{"account": "acct_1OTHER", "invoice": "in_123"}, "unknown account"},
		{"missing ID", "void_invoice", map[string]any{"account": "dummy-one"}, `missing properties: ["invoice"]`},
		{"empty invoice ID", "void_invoice", map[string]any{"account": "dummy-one", "invoice": ""}, `needs path parameter "invoice"`},
		{"empty ID", "update_customer", map[string]any{"account": "dummy-one", "customer": "", "name": "x"}, `needs path parameter "customer"`},
		{"traversal ID", "void_invoice", map[string]any{"account": "dummy-one", "invoice": ".."}, "must match"},
		{"slash in ID", "update_customer", map[string]any{"account": "dummy-one", "customer": "cus_1/../../v1/balance", "name": "x"}, "must match"},
		{"query in ID", "delete_coupon", map[string]any{"account": "dummy-one", "coupon": "C?x=1"}, "must match"},
		{"bad idempotency key", "void_invoice", map[string]any{"account": "dummy-one", "invoice": "in_123", "idempotency_key": "bad\nkey"}, "idempotency key"},
		{"long idempotency key", "void_invoice", map[string]any{"account": "dummy-one", "invoice": "in_123", "idempotency_key": strings.Repeat("k", 256)}, "idempotency key"},
		{"idempotency key on DELETE", "cancel_subscription", map[string]any{"account": "dummy-one", "subscription": "sub_123", "idempotency_key": "k"}, "at_period_end=true"},
		{"bad metadata key", "update_customer", map[string]any{"account": "dummy-one", "customer": "cus_1", "metadata": map[string]any{"a]": "x"}}, "invalid key"},
		{"empty customer update", "update_customer", map[string]any{"account": "dummy-one", "customer": "cus_1"}, "pass at least one of: email, name"},
		{"empty metadata update", "update_customer", map[string]any{"account": "dummy-one", "customer": "cus_1", "metadata": map[string]any{}}, "nothing to update"},
		{"empty subscription update", "update_subscription", map[string]any{"account": "dummy-one", "subscription": "sub_1"}, "trial_end, metadata"},
		{"empty promotion code update", "update_promotion_code", map[string]any{"account": "dummy-one", "promotion_code": "promo_1"}, "nothing to update"},
		{"empty product update", "update_product", map[string]any{"account": "dummy-one", "product": "prod_1"}, "nothing to update"},
		{"empty price update", "update_price", map[string]any{"account": "dummy-one", "price": "price_1"}, "nothing to update"},
		{"empty feature update", "update_feature", map[string]any{"account": "dummy-one", "feature": "feat_1"}, "nothing to update"},
		{"trial_end word", "update_subscription", map[string]any{"account": "dummy-one", "subscription": "sub_1", "trial_end": "tomorrow"}, "trial_end must be"},
		{"trial_end negative", "update_subscription", map[string]any{"account": "dummy-one", "subscription": "sub_1", "trial_end": "-1"}, "trial_end must be"},
		{"trial_end zero", "update_subscription", map[string]any{"account": "dummy-one", "subscription": "sub_1", "trial_end": "0"}, "trial_end must be"},
		{"trial_end fraction", "update_subscription", map[string]any{"account": "dummy-one", "subscription": "sub_1", "trial_end": "1767225600.5"}, "trial_end must be"},
		{"trial_end hex", "update_subscription", map[string]any{"account": "dummy-one", "subscription": "sub_1", "trial_end": "0x10"}, "trial_end must be"},
		{"trial_end overflow", "update_subscription", map[string]any{"account": "dummy-one", "subscription": "sub_1", "trial_end": "99999999999999999999"}, "trial_end must be"},
		{"coupon without discount", "create_coupon", map[string]any{"account": "dummy-one"}, "exactly one of percent_off or amount_off"},
		{"coupon with both discounts", "create_coupon", map[string]any{"account": "dummy-one", "percent_off": 10, "amount_off": 100, "currency": "usd"}, "exactly one of"},
		{"coupon percent over 100", "create_coupon", map[string]any{"account": "dummy-one", "percent_off": 101}, "at most 100"},
		{"coupon percent zero", "create_coupon", map[string]any{"account": "dummy-one", "percent_off": 0}, "above 0"},
		{"coupon amount zero", "create_coupon", map[string]any{"account": "dummy-one", "amount_off": 0, "currency": "usd"}, "must be positive"},
		{"coupon amount without currency", "create_coupon", map[string]any{"account": "dummy-one", "amount_off": 100}, "currency is required"},
		{"coupon currency with percent", "create_coupon", map[string]any{"account": "dummy-one", "percent_off": 10, "currency": "usd"}, "only applies with amount_off"},
		{"coupon repeating without months", "create_coupon", map[string]any{"account": "dummy-one", "percent_off": 10, "duration": "repeating"}, "duration_in_months of 1 or more"},
		{"coupon months without repeating", "create_coupon", map[string]any{"account": "dummy-one", "percent_off": 10, "duration_in_months": 3}, "only applies with duration repeating"},
		{"coupon bad duration", "create_coupon", map[string]any{"account": "dummy-one", "percent_off": 10, "duration": "weekly"}, "once, repeating or forever"},
		{"promotion code without coupon", "create_promotion_code", map[string]any{"account": "dummy-one", "coupon": ""}, "coupon is required"},
		{"refund without target", "create_refund", map[string]any{"account": "dummy-one"}, "exactly one of payment_intent or charge"},
		{"refund with both targets", "create_refund", map[string]any{"account": "dummy-one", "payment_intent": "pi_1", "charge": "ch_1"}, "exactly one of"},
		{"refund zero amount", "create_refund", map[string]any{"account": "dummy-one", "charge": "ch_1", "amount": 0}, "must be positive"},
		{"refund bad reason", "create_refund", map[string]any{"account": "dummy-one", "charge": "ch_1", "reason": "changed_mind"}, "requested_by_customer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantError(t, call(t, cs, tc.tool, tc.args), tc.want)
		})
	}
}

func TestWriteStripeError(t *testing.T) {
	cs := connect(t, "sk_org_test_dummy", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"secret_key_required","message":"This key lacks the required permission."}}`))
	})

	wantError(t, call(t, cs, "void_invoice", map[string]any{"account": "dummy-one", "invoice": "in_123"}), "HTTP 403")
}
