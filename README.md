# mcp-server-stripe-org

MCP server for an entire Stripe organization spanning multiple 'accounts', written in Go.

This MCP server solves the problem that

- API token auth against mcp.stripe.com works only for a single product
- The official mcp.stripe.com OAuth expires very frequently (within a few days), prompting for constant re-auth

## Setup

Install the pinned toolchain (Go, wrangler, jq) with [mise](https://mise.jdx.dev):

```sh
mise install
```

The server reads its configuration from environment variables:

| Variable          | Required for | Value                                                                         |
| ----------------- | ------------ | ----------------------------------------------------------------------------- |
| `STRIPE_ORG_KEY`  | always       | A Stripe organization key: `sk_org_live_`, `sk_org_test_` or an `rk_org_` key |
| `STRIPE_ACCOUNTS` | always       | JSON array of the accounts the tools may act on (see below)                   |
| `MCP_TOKEN`       | HTTP only    | Bearer token clients must send in `Authorization: Bearer <token>`             |

`STRIPE_ACCOUNTS` lists each account with a lowercase alias, a display name and its `acct_` ID:

```json
[
  { "alias": "shop", "name": "My Shop", "account": "acct_123" },
  { "alias": "saas", "name": "My SaaS", "account": "acct_456" }
]
```

Tools refer to accounts by alias or `acct_` ID, and reject any account not in the list.

## Run locally

```sh
go run ./cmd/mcp                # MCP over stdio
go run ./cmd/mcp -http :8080    # MCP over HTTP at http://localhost:8080/mcp
```

To run the worker build locally under `wrangler dev`, put the three variables in a gitignored `.dev.vars` file and run:

```sh
wrangler dev
```

## Run on Cloudflare Workers

Log in with `wrangler login` (or set `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID`), set the secrets, then deploy:

```sh
wrangler secret put STRIPE_ORG_KEY
wrangler secret put STRIPE_ACCOUNTS
wrangler secret put MCP_TOKEN
wrangler deploy
```

`wrangler deploy` builds the Go wasm bundle through `mise run build-worker`. The MCP endpoint is `https://<worker>.<subdomain>.workers.dev/mcp`, and requests without the `MCP_TOKEN` bearer token are rejected.

To use it from Claude Code, add it to `.mcp.json`:

```json
{
  "mcpServers": {
    "stripe-org": {
      "type": "http",
      "url": "https://<worker>.<subdomain>.workers.dev/mcp",
      "headers": { "Authorization": "Bearer ${MCP_TOKEN}" }
    }
  }
}
```
