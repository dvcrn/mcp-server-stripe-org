#!/bin/sh
# Uploads STRIPE_ORG_KEY, STRIPE_ACCOUNTS and MCP_TOKEN to the worker.
#
#   production: fnox production profile and accounts.json, as Worker secrets. Requires a live key.
#   preview:    active fnox profile and accounts.sandbox.json, as the Previews base config. Refuses a live key.
#
# STRIPE_ACCOUNTS falls back to the profile's value when the accounts file is missing (cloud sessions).
set -eu

self="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
cd "$(dirname "$self")/.."

usage() {
	cat >&2 <<-'USAGE'
		Usage: worker-secrets.sh production   upload live secrets to the production worker
		       worker-secrets.sh preview      upload sandbox secrets to the Previews base config
		       worker-secrets.sh dev-json     print the sandbox secrets as JSON for .dev.vars
	USAGE
	exit 2
}

# check <accounts file> prints the three secrets as a JSON object, or fails if any is missing.
check() {
	if [ -f "$1" ]; then
		STRIPE_ACCOUNTS="$(cat "$1")"
		export STRIPE_ACCOUNTS
	fi
	jq -n --arg file "$1" '
	  $ENV | {STRIPE_ORG_KEY, STRIPE_ACCOUNTS, MCP_TOKEN}
	  | [to_entries[] | select(.value == null or .value == "") | .key] as $missing
	  | if $missing != [] then error("missing: \($missing | join(", ")) (STRIPE_ACCOUNTS comes from \($file) or the fnox profile)") else . end
	  | .STRIPE_ACCOUNTS |= (fromjson | if type == "array" and length > 0 then tojson else error("STRIPE_ACCOUNTS must be a non-empty JSON array") end)
	'
}

key_mode() {
	case "$STRIPE_ORG_KEY" in
	sk_org_live_* | rk_org_live_*) echo live ;;
	sk_org_test_* | rk_org_test_*) echo test ;;
	*)
		echo "worker-secrets: STRIPE_ORG_KEY is not an organization key" >&2
		exit 1
		;;
	esac
}

# Unset first so values from the calling shell can't stand in for ones the profile lacks.
in_profile() {
	profile="$1"
	shift
	env -u STRIPE_ORG_KEY -u STRIPE_ACCOUNTS -u MCP_TOKEN \
		fnox ${profile:+-P "$profile"} exec -- "$self" "$@"
}

case "${1:-}" in
production) in_profile production upload-from-env production ;;
preview) in_profile "" upload-from-env preview ;;
dev-json) in_profile "" check-from-env ;;
# Internal: run by in_profile inside fnox exec.
check-from-env)
	check accounts.sandbox.json
	;;
upload-from-env)
	target="${2:-}"
	case "$target" in
	production) accounts=accounts.json want=live profile=production ;;
	preview) accounts=accounts.sandbox.json want=test profile="${FNOX_PROFILE:-default}" ;;
	*) usage ;;
	esac
	secrets="$(check "$accounts")"
	mode="$(key_mode)"
	worker="$(sed '/^[[:space:]]*\/\//d' wrangler.jsonc | jq -er '.name // empty')"
	echo "worker-secrets: $mode key and $accounts from fnox profile $profile -> $target of worker $worker" >&2
	if [ "$mode" != "$want" ]; then
		echo "worker-secrets: $target needs a $want key" >&2
		exit 1
	fi
	# The secrets reach wrangler only on stdin.
	if [ "$target" = production ]; then
		printf %s "$secrets" | env -u STRIPE_ORG_KEY -u STRIPE_ACCOUNTS -u MCP_TOKEN \
			wrangler secret bulk --name "$worker"
	else
		printf %s "$secrets" | env -u STRIPE_ORG_KEY -u STRIPE_ACCOUNTS -u MCP_TOKEN \
			wrangler preview base-config secret bulk --worker-name "$worker"
	fi
	;;
*) usage ;;
esac
