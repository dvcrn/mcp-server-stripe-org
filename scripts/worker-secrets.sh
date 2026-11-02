#!/bin/sh
# Reads STRIPE_ORG_KEY, STRIPE_ACCOUNTS and MCP_TOKEN from fnox and uploads them to a worker.
#
# The target picks the fnox profile, the key mode it accepts and the wrangler env together:
# production reads -P production, needs a live key and targets the top-level worker; dev reads
# the active profile, needs a test key and targets env.dev. So a sandbox key can't reach the
# production worker and a live key can't reach dev.
set -eu

usage() {
	cat >&2 <<-'USAGE'
		Usage: worker-secrets.sh upload production|dev   upload to that worker
		       worker-secrets.sh dev-json                print the checked dev secrets as JSON
	USAGE
	exit 2
}

check() {
	jq -n --arg mode "$1" '
	  $ENV | {STRIPE_ORG_KEY, STRIPE_ACCOUNTS, MCP_TOKEN}
	  | [to_entries[] | select(.value == null or .value == "") | .key] as $missing
	  | if $missing != [] then error("empty in the fnox profile: \($missing | join(", "))") else . end
	  | if .STRIPE_ORG_KEY | test("^[sr]k_org_\($mode)_") then . else
	      error("STRIPE_ORG_KEY must be an sk_org_\($mode)_ or rk_org_\($mode)_ key for this worker") end
	  | .STRIPE_ACCOUNTS |= (fromjson | tojson)
	'
}

# Unset first so values from the calling shell can't stand in for ones the profile lacks,
# and CLOUDFLARE_ENV can't redirect wrangler to the other worker.
in_profile() {
	profile="$1"
	shift
	env -u STRIPE_ORG_KEY -u STRIPE_ACCOUNTS -u MCP_TOKEN -u CLOUDFLARE_ENV \
		fnox ${profile:+-P "$profile"} exec -- "$0" "$@"
}

case "${1:-}" in
upload)
	case "${2:-}" in
	production) in_profile production upload-from-env live ;;
	dev) in_profile "" upload-from-env test ;;
	*) usage ;;
	esac
	;;
dev-json)
	in_profile "" check-from-env test
	;;
# Internal: run by in_profile inside fnox exec.
check-from-env)
	check "$2"
	;;
upload-from-env)
	case "$2" in live) wrangler_env= ;; test) wrangler_env=dev ;; *) usage ;; esac
	secrets="$(check "$2")"
	# The Stripe secrets reach wrangler only on stdin; --env="" pins production to the top level.
	printf %s "$secrets" | env -u STRIPE_ORG_KEY -u STRIPE_ACCOUNTS -u MCP_TOKEN \
		wrangler secret bulk --env="$wrangler_env"
	;;
*) usage ;;
esac
