#!/usr/bin/env bash
# Probes which Stripe endpoints the org key can reach without modifying account data.
#
# Usage: probe-permissions.sh [acct_…]
#   The account defaults to $STRIPE_TEST_ACCOUNT, which only the fnox cloud profile sets. Account IDs
#   stay out of this public repo, so anywhere else pass the account as an argument.
#   STRIPE_ORG_KEY must be set, e.g. via `fnox exec -- scripts/probe-permissions.sh`.
set -euo pipefail

readonly API="https://api.stripe.com"
readonly STRIPE_VERSION="2026-10-28.endive"
readonly PREVIEW_VERSION="2026-10-28.preview"

if [[ -z "${STRIPE_ORG_KEY:-}" ]]; then
	echo "STRIPE_ORG_KEY is not set; run via 'fnox exec -- $0' or 'mise run probe'" >&2
	exit 2
fi

account="${1:-${STRIPE_TEST_ACCOUNT:-}}"
if [[ -z "$account" ]]; then
	echo "no account: pass one, e.g. 'mise run probe acct_…', or set STRIPE_TEST_ACCOUNT" >&2
	exit 2
fi
if [[ ! "$account" =~ ^acct_[A-Za-z0-9]+$ ]]; then
	echo "invalid account ID: $account" >&2
	exit 2
fi

body_file="$(mktemp)"
trap 'rm -f "$body_file"' EXIT

granted=0 denied=0 other=0 errors=0

printf '%-48s %-6s %-6s %-8s %s\n' ENDPOINT METHOD STATUS VERDICT NOTE

# probe KIND METHOD PATH [BODY] [CONTENT_TYPE] [STRIPE_VERSION]
# KIND sets which answer means granted: read=200, item=404, collection=400 for the unknown
# parameter, v2=2xx. A 403 or more_permissions_required is always denied.
probe() {
	local kind="$1" method="$2" path="$3" body="${4-}" ctype="${5:-application/x-www-form-urlencoded}"
	local version="${6:-$STRIPE_VERSION}"
	local -a args=(-sS -o "$body_file" -w '%{http_code}' -X "$method" -K -
		-H "Stripe-Version: $version" -H "Stripe-Context: $account")
	if [[ "$method" == POST ]]; then
		args+=(-H "Content-Type: $ctype" --data-binary "$body")
	fi

	: >"$body_file"
	local status
	# The key goes through curl's stdin config so it never shows up in argv.
	if ! status="$(printf 'header = "Authorization: Bearer %s"\n' "$STRIPE_ORG_KEY" |
		curl "${args[@]}" "$API$path" 2>/dev/null)"; then
		status="${status:-000}"
		errors=$((errors + 1))
	fi

	local code message
	code="$(jq -r '.error.code // empty' "$body_file" 2>/dev/null || true)"
	message="$(jq -r '.error.message // empty' "$body_file" 2>/dev/null || true)"

	local verdict=other
	if [[ "$status" == 403 || "$code" == more_permissions_required ]]; then
		verdict=denied
	else
		case "$kind:$status" in
		read:200 | item:404 | v2:2??) verdict=granted ;;
		# Some creates validate required params before permissions, so only the unknown-param error counts.
		collection:400) [[ "$message" == "Received unknown parameter"* ]] && verdict=granted ;;
		esac
	fi

	local note=""
	case "$verdict" in
	granted) granted=$((granted + 1)) ;;
	denied) denied=$((denied + 1)) ;;
	other)
		other=$((other + 1))
		note="${message:-$code}"
		note="${note:0:100}"
		;;
	esac

	printf '%-48s %-6s %-6s %-8s %s\n' "${path%%\?*}" "$method" "$status" "$verdict" "$note"
}

read_paths=(
	/v1/account
	/v1/balance
	/v1/customers
	"/v1/customers/search?query=email%3A%27probe%40example.com%27"
	/v1/subscriptions
	/v1/subscription_schedules
	/v1/invoices
	/v1/credit_notes
	/v1/quotes
	/v1/products
	/v1/prices
	/v1/coupons
	/v1/promotion_codes
	/v1/payment_links
	/v1/entitlements/features
	/v1/charges
	/v1/payment_intents
	/v1/setup_intents
	/v1/refunds
	/v1/disputes
	/v1/payouts
	/v1/balance_transactions
	/v1/checkout/sessions
	/v1/events
	/v1/webhook_endpoints
	/v1/tax_rates
	/v1/billing/meters
	/v1/billing_portal/configurations
	/v1/radar/early_fraud_warnings
	/v1/reporting/report_types
	/v1/sigma/scheduled_query_runs
	/v1/accounts
	/v1/files
	/v1/tax/settings
	/v2/core/accounts
)
for path in "${read_paths[@]}"; do
	sep='?'
	[[ "$path" == *\?* ]] && sep='&'
	case "$path" in
	/v1/account | /v1/balance | /v1/tax/settings | /v1/reporting/report_types | /v2/*) probe read GET "$path" ;;
	*) probe read GET "$path${sep}limit=1" ;;
	esac
done

# Updates on IDs that don't exist: Stripe checks the permission first, so 404 means granted.
# Coupon updates and invoice voids return 404 before checking permissions, so the coupon create
# and invoice finalize and send probes cover those permissions instead.
item_paths=(
	/v1/customers/cus_probe_nonexistent
	/v1/invoices/in_probe_nonexistent/finalize
	/v1/invoices/in_probe_nonexistent/send
	/v1/subscriptions/sub_probe_nonexistent
	/v1/promotion_codes/promo_probe_nonexistent
	/v1/products/prod_probe_nonexistent
	/v1/prices/price_probe_nonexistent
	/v1/refunds/re_probe_nonexistent
	/v1/entitlements/features/feat_probe_nonexistent
	/v1/webhook_endpoints/we_probe_nonexistent
)
for path in "${item_paths[@]}"; do
	probe item POST "$path" ""
done

# Creates with only an unknown parameter, which Stripe rejects with 400 once the permission passes.
for path in /v1/coupons /v1/promotion_codes /v1/refunds; do
	probe collection POST "$path" "invalid_probe_param=1"
done

# Both v2 data endpoints are preview-only. Stripe counts a query run as a read.
probe v2 POST /v2/data/query_runs '{"query":{"sql":"SELECT 1"},"dataset":"analytical","format":"csv"}' \
	application/json "$PREVIEW_VERSION"
probe v2 GET /v2/data/schemas "" "" "$PREVIEW_VERSION"

echo
echo "account $account: $granted granted, $denied denied, $other other"
if ((errors > 0)); then
	echo "$errors request(s) failed to reach Stripe" >&2
	exit 1
fi
