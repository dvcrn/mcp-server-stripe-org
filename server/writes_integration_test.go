//go:build integration

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvcrn/mcp-server-stripe-org/internal/httpclient"
	"github.com/dvcrn/mcp-server-stripe-org/stripe"
)

// liveWrites connects to the server with the sandbox key. It needs
// STRIPE_ORG_KEY, STRIPE_ACCOUNTS and STRIPE_TEST_ACCOUNT (a sandbox acct_ ID
// listed in STRIPE_ACCOUNTS), for example via fnox exec.
func liveWrites(t *testing.T) *liveStripe {
	t.Helper()

	key, accounts, account := os.Getenv(EnvOrgKey), os.Getenv(EnvAccounts), os.Getenv("STRIPE_TEST_ACCOUNT")
	if key == "" || accounts == "" || account == "" {
		t.Skip("STRIPE_ORG_KEY, STRIPE_ACCOUNTS and STRIPE_TEST_ACCOUNT must be set")
	}

	if !strings.HasPrefix(key, "sk_org_test_") && !strings.HasPrefix(key, "rk_org_test_") {
		t.Fatal("write integration tests only run with a sandbox org key")
	}

	env, err := NewEnv(key, accounts, httpclient.New())
	if err != nil {
		t.Fatal(err)
	}

	return &liveStripe{
		cs:      connectEnv(t, WithEnv(context.Background(), env)),
		client:  env.Client,
		account: account,
		suffix:  strconv.FormatInt(time.Now().UnixNano(), 36),
	}
}

type liveStripe struct {
	cs      *mcp.ClientSession
	client  *stripe.Client
	account string
	// suffix makes fixture names and keys unique per run.
	suffix string
}

// fixture calls Stripe directly to set up test data, skipping the test when
// the sandbox key lacks the permission.
func (l *liveStripe) fixture(t *testing.T, method, path string, params map[string]any) map[string]any {
	t.Helper()

	body, err := l.client.DoV1(context.Background(), stripe.V1Request{Method: method, Path: path, Account: l.account, Params: params})
	var stripeErr *stripe.Error
	if errors.As(err, &stripeErr) && stripeErr.Status == http.StatusForbidden {
		t.Skipf("fixture %s %s: the sandbox key lacks the permission: %v", method, path, err)
	}

	if err != nil {
		t.Fatalf("fixture %s %s: %v", method, path, err)
	}

	return decode(t, string(body))
}

// cleanup runs a Stripe call after the test and logs, rather than fails on,
// errors. It skips 400 and 404, which mean the test already deleted, voided
// or archived the object.
func (l *liveStripe) cleanup(t *testing.T, method, path string, params map[string]any) {
	t.Cleanup(func() {
		_, err := l.client.DoV1(context.Background(), stripe.V1Request{Method: method, Path: path, Account: l.account, Params: params})
		var stripeErr *stripe.Error
		if errors.As(err, &stripeErr) && (stripeErr.Status == http.StatusBadRequest || stripeErr.Status == http.StatusNotFound) {
			return
		}

		if err != nil {
			t.Logf("cleanup %s %s: %v", method, path, err)
		}
	})
}

// call runs a tool against the test account, skipping the test when the
// sandbox key lacks the permission. Any other 403 fails the test.
func (l *liveStripe) call(t *testing.T, tool string, args map[string]any) map[string]any {
	t.Helper()

	args["account"] = l.account
	res := call(t, l.cs, tool, args)
	if res.IsError && strings.Contains(text(t, res), "HTTP 403") && strings.Contains(text(t, res), "required permissions") {
		t.Skipf("%s: the sandbox key lacks the permission: %s", tool, text(t, res))
	}

	return decode(t, wantOK(t, res))
}

func decode(t *testing.T, body string) map[string]any {
	t.Helper()

	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}

	return out
}

func id(obj map[string]any) string {
	s, _ := obj["id"].(string)
	return s
}

func metadata(obj map[string]any) map[string]any {
	m, _ := obj["metadata"].(map[string]any)
	return m
}

func expect(t *testing.T, obj map[string]any, field string, want any) {
	t.Helper()

	if got := obj[field]; plain(got) != plain(want) {
		t.Errorf("%s %s = %v, want %v", id(obj), field, got, want)
	}
}

// plain formats v for comparison. JSON numbers decode as float64, which fmt
// prints in exponent form.
func plain(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}

	return fmt.Sprint(v)
}

func (l *liveStripe) customer(t *testing.T) string {
	t.Helper()

	c := l.fixture(t, http.MethodPost, "/v1/customers", map[string]any{
		"name":     "mcp-integration " + l.suffix,
		"email":    "mcp-integration-" + l.suffix + "@example.com",
		"metadata": map[string]string{"keep": "1", "drop": "1"},
	})
	l.cleanup(t, http.MethodDelete, "/v1/customers/"+id(c), nil)

	return id(c)
}

func (l *liveStripe) product(t *testing.T) string {
	t.Helper()

	p := l.fixture(t, http.MethodPost, "/v1/products", map[string]any{
		"name":     "mcp-integration " + l.suffix,
		"metadata": map[string]string{"keep": "1", "drop": "1"},
	})
	l.cleanup(t, http.MethodPost, "/v1/products/"+id(p), map[string]any{"active": false})

	return id(p)
}

func (l *liveStripe) price(t *testing.T, product string, recurring bool) string {
	t.Helper()

	params := map[string]any{"product": product, "unit_amount": 1000, "currency": "usd"}
	if recurring {
		params["recurring"] = map[string]any{"interval": "month"}
	}

	p := l.fixture(t, http.MethodPost, "/v1/prices", params)
	l.cleanup(t, http.MethodPost, "/v1/prices/"+id(p), map[string]any{"active": false})

	return id(p)
}

func TestLiveUpdateCustomer(t *testing.T) {
	l := liveWrites(t)
	customer := l.customer(t)

	c := l.call(t, "update_customer", map[string]any{
		"customer":        customer,
		"name":            "mcp-integration renamed " + l.suffix,
		"description":     "dummy description",
		"metadata":        map[string]any{"drop": "", "added": "2"},
		"idempotency_key": "mcp-integration-customer-" + l.suffix,
	})
	expect(t, c, "name", "mcp-integration renamed "+l.suffix)
	expect(t, c, "description", "dummy description")
	if m := metadata(c); m["keep"] != "1" || m["added"] != "2" || m["drop"] != nil {
		t.Errorf("metadata = %v, want keep=1 added=2 and no drop", m)
	}

	c = l.call(t, "update_customer", map[string]any{"customer": customer, "description": ""})
	expect(t, c, "description", nil)
}

func TestLiveInvoiceTools(t *testing.T) {
	l := liveWrites(t)
	customer := l.customer(t)

	inv := l.fixture(t, http.MethodPost, "/v1/invoices", map[string]any{
		"customer":          customer,
		"collection_method": "send_invoice",
		"days_until_due":    30,
		"auto_advance":      false,
		"currency":          "usd",
	})
	invoice := id(inv)
	// Covers a test that stops early: drafts can be deleted, open invoices voided.
	l.cleanup(t, http.MethodPost, "/v1/invoices/"+invoice+"/void", nil)
	l.cleanup(t, http.MethodDelete, "/v1/invoices/"+invoice, nil)

	l.fixture(t, http.MethodPost, "/v1/invoiceitems", map[string]any{
		"customer": customer,
		"invoice":  invoice,
		"amount":   1000,
		"currency": "usd",
	})

	inv = l.call(t, "finalize_invoice", map[string]any{
		"invoice":         invoice,
		"auto_advance":    false,
		"idempotency_key": "mcp-integration-finalize-" + l.suffix,
	})
	expect(t, inv, "status", "open")

	inv = l.call(t, "send_invoice", map[string]any{"invoice": invoice, "idempotency_key": "mcp-integration-send-" + l.suffix})
	expect(t, inv, "status", "open")

	inv = l.call(t, "void_invoice", map[string]any{"invoice": invoice, "idempotency_key": "mcp-integration-void-" + l.suffix})
	expect(t, inv, "status", "void")
}

func TestLiveSubscriptionTools(t *testing.T) {
	l := liveWrites(t)
	customer := l.customer(t)
	price := l.price(t, l.product(t), true)

	sub := l.fixture(t, http.MethodPost, "/v1/subscriptions", map[string]any{
		"customer":          customer,
		"items":             []any{map[string]any{"price": price}},
		"collection_method": "send_invoice",
		"days_until_due":    30,
	})
	subscription := id(sub)
	l.cleanup(t, http.MethodDelete, "/v1/subscriptions/"+subscription, nil)

	trialEnd := time.Now().Add(7 * 24 * time.Hour).Unix()
	sub = l.call(t, "update_subscription", map[string]any{
		"subscription":    subscription,
		"trial_end":       strconv.FormatInt(trialEnd, 10),
		"metadata":        map[string]any{"run": l.suffix},
		"idempotency_key": "mcp-integration-trial-" + l.suffix,
	})
	expect(t, sub, "status", "trialing")
	expect(t, sub, "trial_end", trialEnd)
	if metadata(sub)["run"] != l.suffix {
		t.Errorf("metadata = %v", metadata(sub))
	}

	sub = l.call(t, "update_subscription", map[string]any{"subscription": subscription, "trial_end": "now", "metadata": map[string]any{"run": ""}})
	expect(t, sub, "status", "active")
	if len(metadata(sub)) != 0 {
		t.Errorf("metadata = %v, want none", metadata(sub))
	}

	sub = l.call(t, "cancel_subscription", map[string]any{
		"subscription":    subscription,
		"at_period_end":   true,
		"idempotency_key": "mcp-integration-cancel-" + l.suffix,
	})
	expect(t, sub, "cancel_at_period_end", true)
	expect(t, sub, "status", "active")

	sub = l.call(t, "resume_subscription", map[string]any{"subscription": subscription})
	expect(t, sub, "cancel_at_period_end", false)
	expect(t, sub, "cancel_at", nil)

	sub = l.call(t, "cancel_subscription", map[string]any{"subscription": subscription})
	expect(t, sub, "status", "canceled")
}

func TestLiveCatalogTools(t *testing.T) {
	l := liveWrites(t)
	product := l.product(t)
	archived := l.price(t, product, false)
	defaultPrice := l.price(t, product, false)

	p := l.call(t, "update_product", map[string]any{
		"product":         product,
		"name":            "mcp-integration renamed " + l.suffix,
		"description":     "dummy description",
		"default_price":   defaultPrice,
		"metadata":        map[string]any{"drop": ""},
		"idempotency_key": "mcp-integration-product-" + l.suffix,
	})
	expect(t, p, "name", "mcp-integration renamed "+l.suffix)
	expect(t, p, "description", "dummy description")
	expect(t, p, "default_price", defaultPrice)
	if m := metadata(p); m["keep"] != "1" || m["drop"] != nil {
		t.Errorf("product metadata = %v, want keep=1 and no drop", m)
	}

	lookupKey := "mcp_integration_" + l.suffix
	pr := l.call(t, "update_price", map[string]any{
		"price":           archived,
		"nickname":        "mcp-integration",
		"lookup_key":      lookupKey,
		"metadata":        map[string]any{"run": l.suffix},
		"idempotency_key": "mcp-integration-price-" + l.suffix,
	})
	expect(t, pr, "nickname", "mcp-integration")
	expect(t, pr, "lookup_key", lookupKey)

	pr = l.call(t, "update_price", map[string]any{"price": archived, "active": false, "lookup_key": ""})
	expect(t, pr, "active", false)
	expect(t, pr, "lookup_key", nil)

	p = l.call(t, "update_product", map[string]any{"product": product, "active": false, "description": ""})
	expect(t, p, "active", false)
	expect(t, p, "description", nil)
}

func TestLiveUpdateFeature(t *testing.T) {
	l := liveWrites(t)

	f := l.fixture(t, http.MethodPost, "/v1/entitlements/features", map[string]any{
		"name":       "mcp-integration " + l.suffix,
		"lookup_key": "mcp-integration-" + l.suffix,
		"metadata":   map[string]string{"drop": "1"},
	})
	feature := id(f)
	// Features can't be deleted, so archive the fixture if the test stops early.
	l.cleanup(t, http.MethodPost, "/v1/entitlements/features/"+feature, map[string]any{"active": false})

	f = l.call(t, "update_feature", map[string]any{
		"feature":         feature,
		"name":            "mcp-integration renamed " + l.suffix,
		"metadata":        map[string]any{"drop": "", "run": l.suffix},
		"idempotency_key": "mcp-integration-feature-" + l.suffix,
	})
	expect(t, f, "name", "mcp-integration renamed "+l.suffix)
	if m := metadata(f); m["run"] != l.suffix || m["drop"] != nil {
		t.Errorf("metadata = %v", m)
	}

	// Archiving is permanent, so it runs last.
	f = l.call(t, "update_feature", map[string]any{"feature": feature, "active": false})
	expect(t, f, "active", false)
}

func TestLiveCouponTools(t *testing.T) {
	l := liveWrites(t)
	couponID := "MCPINT" + strings.ToUpper(l.suffix)
	l.cleanup(t, http.MethodDelete, "/v1/coupons/"+couponID, nil)

	args := func() map[string]any {
		return map[string]any{
			"id":                 couponID,
			"name":               "mcp-integration",
			"percent_off":        12.5,
			"duration":           "repeating",
			"duration_in_months": 3,
			"metadata":           map[string]any{"run": l.suffix},
			"idempotency_key":    "mcp-integration-coupon-" + l.suffix,
		}
	}

	c := l.call(t, "create_coupon", args())
	expect(t, c, "id", couponID)
	expect(t, c, "percent_off", 12.5)
	expect(t, c, "duration_in_months", 3)

	// The same idempotency key replays the first response instead of failing on the duplicate ID.
	again := l.call(t, "create_coupon", args())
	expect(t, again, "created", c["created"])

	code := "MCPINTCODE" + strings.ToUpper(l.suffix)
	promo := l.call(t, "create_promotion_code", map[string]any{
		"coupon":          couponID,
		"code":            code,
		"max_redemptions": 5,
		"metadata":        map[string]any{"drop": "1"},
		"idempotency_key": "mcp-integration-promo-" + l.suffix,
	})
	expect(t, promo, "code", code)
	expect(t, promo, "active", true)
	if p, _ := promo["promotion"].(map[string]any); p["coupon"] != couponID {
		t.Errorf("promotion = %v, want coupon %s", promo["promotion"], couponID)
	}

	promo = l.call(t, "update_promotion_code", map[string]any{
		"promotion_code":  id(promo),
		"active":          false,
		"metadata":        map[string]any{"drop": ""},
		"idempotency_key": "mcp-integration-promo-update-" + l.suffix,
	})
	expect(t, promo, "active", false)
	if len(metadata(promo)) != 0 {
		t.Errorf("metadata = %v, want none", metadata(promo))
	}

	deleted := l.call(t, "delete_coupon", map[string]any{"coupon": couponID})
	expect(t, deleted, "deleted", true)
}

// TestLiveCreateRefund charges a debit card token through /v1/charges
// because the sandbox key can't create payment intents, and the sandbox
// accounts decline direct credit card charges that skip 3DS. Debit cards
// are exempt from that 3DS requirement.
func TestLiveCreateRefund(t *testing.T) {
	l := liveWrites(t)

	ch := l.fixture(t, http.MethodPost, "/v1/charges", map[string]any{
		"amount":   1000,
		"currency": "usd",
		"source":   "tok_visa_debit",
		"metadata": map[string]string{"run": "mcp-integration " + l.suffix},
	})
	charge := id(ch)
	// Refunds the remainder of the charge if the test stops early.
	l.cleanup(t, http.MethodPost, "/v1/refunds", map[string]any{"charge": charge})

	partial := map[string]any{
		"charge":          charge,
		"amount":          400,
		"reason":          "requested_by_customer",
		"metadata":        map[string]any{"run": l.suffix},
		"idempotency_key": "mcp-integration-refund-" + l.suffix,
	}
	r := l.call(t, "create_refund", partial)
	expect(t, r, "amount", 400)
	expect(t, r, "status", "succeeded")
	expect(t, r, "reason", "requested_by_customer")
	if m := metadata(r); m["run"] != l.suffix {
		t.Errorf("metadata = %v, want run=%s", m, l.suffix)
	}

	replay := l.call(t, "create_refund", partial)
	if id(replay) != id(r) {
		t.Fatalf("replay with the same idempotency key created refund %s, want %s", id(replay), id(r))
	}

	r = l.call(t, "create_refund", map[string]any{"charge": charge})
	expect(t, r, "amount", 600)
	expect(t, r, "status", "succeeded")

	ch = l.fixture(t, http.MethodGet, "/v1/charges/"+charge, nil)
	expect(t, ch, "amount_refunded", 1000)
	expect(t, ch, "refunded", true)
}
