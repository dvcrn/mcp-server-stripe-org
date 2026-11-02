#!/bin/sh
# Uploads STRIPE_ORG_KEY, STRIPE_ACCOUNTS and MCP_TOKEN from a fnox profile to the worker.
#
# A live key is only uploaded with an explicit -P production, so a run on the active profile
# can't put production credentials on the worker by accident.
set -eu

self="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
cd "$(dirname "$self")/.."

usage() {
	cat >&2 <<-'USAGE'
		Usage: worker-secrets.sh [-P profile]   upload from that fnox profile (default: the active one)
		       worker-secrets.sh dev-json       print the checked secrets of the active profile as JSON
	USAGE
	exit 2
}

check() {
	jq -n '
	  $ENV | {STRIPE_ORG_KEY, STRIPE_ACCOUNTS, MCP_TOKEN}
	  | [to_entries[] | select(.value == null or .value == "") | .key] as $missing
	  | if $missing != [] then error("empty in the fnox profile: \($missing | join(", "))") else . end
	  | .STRIPE_ACCOUNTS |= (fromjson | tojson)
	'
}

# Unset first so values from the calling shell can't stand in for ones the profile lacks.
in_profile() {
	profile="$1"
	shift
	env -u STRIPE_ORG_KEY -u STRIPE_ACCOUNTS -u MCP_TOKEN \
		fnox ${profile:+-P "$profile"} exec -- "$self" "$@"
}

case "${1:-}" in
"")
	in_profile "" upload-from-env ""
	;;
-P)
	[ -n "${2:-}" ] || usage
	in_profile "$2" upload-from-env "$2"
	;;
dev-json)
	in_profile "" check-from-env
	;;
# Internal: run by in_profile inside fnox exec.
check-from-env)
	check
	;;
upload-from-env)
	profile="${2:-}"
	secrets="$(check)"
	case "$STRIPE_ORG_KEY" in
	sk_org_live_* | rk_org_live_*) mode=live ;;
	sk_org_test_* | rk_org_test_*) mode=test ;;
	*)
		echo "worker-secrets: STRIPE_ORG_KEY is not an organization key" >&2
		exit 1
		;;
	esac
	worker="$(sed '/^[[:space:]]*\/\//d' wrangler.jsonc | jq -er '.name // empty')"
	echo "worker-secrets: $mode key from fnox profile ${profile:-${FNOX_PROFILE:-default}} -> worker $worker" >&2
	if [ "$mode" = live ] && [ "$profile" != production ]; then
		echo "worker-secrets: refusing to upload a live key without an explicit -P production" >&2
		exit 1
	fi
	# The Stripe secrets reach wrangler only on stdin.
	printf %s "$secrets" | env -u STRIPE_ORG_KEY -u STRIPE_ACCOUNTS -u MCP_TOKEN \
		wrangler secret bulk --name "$worker"
	;;
*) usage ;;
esac
