# mcp-server-stripe-org

MCP server for an entire Stripe organization spanning multiple 'accounts', written in Go.

This MCP server solves the problem that

- API token auth against mcp.stripe.com works only for a single product
- The official mcp.stripe.com OAuth expires very frequently (within a few days), prompting for constant re-auth

Deploy this server directly to [MCP Nest](https://mcpnest.dev)

<a href="https://mcpnest.dev/deploy?server=mcp-server-stripe-org&package-manager=npx&env[STRIPE_ORG_KEY]=&env[STRIPE_ACCOUNTS]=">
    <img src="https://mcpnest.dev/images/deploy-on-mcpnest.png" alt="Deploy on MCP Nest" height="32" />
  </a>

## Setup

The server reads its configuration from environment variables:

| Variable          | Required for | Value                                                                         |
| ----------------- | ------------ | ----------------------------------------------------------------------------- |
| `STRIPE_ORG_KEY`  | always       | A Stripe organization key: `sk_org_live_`, `sk_org_test_` or an `rk_org_` key |
| `STRIPE_ACCOUNTS` | always       | JSON array of the accounts the tools may act on (see below)                   |
| `MCP_TOKEN`       | HTTP, unless `MCP_READ_TOKEN` is set | Bearer token for every tool, sent as `Authorization: Bearer <token>` or `X-API-Key`. Generate one with `openssl rand -hex 32` |
| `MCP_READ_TOKEN`  | HTTP, optional | Bearer token for the read-only tools only. Must differ from `MCP_TOKEN` |
| `MCP_READ_ONLY`   | optional     | `1` or `true` limits stdio and `MCP_TOKEN` to the read-only tools. A value that isn't a boolean is a config error |

Over HTTP, set `MCP_TOKEN`, `MCP_READ_TOKEN` or both. With only `MCP_READ_TOKEN`, the server is read-only.

The read-only tools are the ones annotated `readOnlyHint`: `list_accounts`, `search_api_operations`, `get_api_operation`, `api_read`, `run_sql_query`, `get_query_run` and `list_query_tables`. With the read token, write tools are left out of `tools/list` and calling one fails as an unknown tool.

`STRIPE_ACCOUNTS` lists each account with a lowercase alias, a display name and its `acct_` ID:

```json
[
  { "alias": "shop", "name": "My Shop", "account": "acct_123" },
  { "alias": "saas", "name": "My SaaS", "account": "acct_456" }
]
```

Tools refer to accounts by alias or `acct_` ID, and reject any account not in the list.

## Install

Pick one method and add its entry to `mcpServers` in your client configuration. `STRIPE_ACCOUNTS` is a JSON string, so its quotes must be escaped inside the config.

### npx, without installing

```json
"stripe-org": {
  "command": "npx",
  "args": ["-y", "mcp-server-stripe-org"],
  "env": {
    "STRIPE_ORG_KEY": "rk_org_...",
    "STRIPE_ACCOUNTS": "[{\"alias\":\"shop\",\"name\":\"My Shop\",\"account\":\"acct_123\"}]"
  }
}
```

### npm, mise, or Go

Install with one of:

```sh
npm install -g mcp-server-stripe-org
mise use -g go:github.com/dvcrn/mcp-server-stripe-org/cmd/mcp@main
go install github.com/dvcrn/mcp-server-stripe-org/cmd/mcp@latest
```

npm installs the binary as `mcp-server-stripe-org`. mise and `go install` name it `mcp` after the package directory, so use `mcp` as `command` for those:

```json
"stripe-org": {
  "command": "mcp-server-stripe-org",
  "env": {
    "STRIPE_ORG_KEY": "rk_org_...",
    "STRIPE_ACCOUNTS": "[{\"alias\":\"shop\",\"name\":\"My Shop\",\"account\":\"acct_123\"}]"
  }
}
```

### Docker

The image at `ghcr.io/dvcrn/mcp-server-stripe-org` serves HTTP by default. For stdio, override the entrypoint, which also drops the default `-http` argument:

```json
"stripe-org": {
  "command": "docker",
  "args": [
    "run", "-i", "--rm",
    "--entrypoint", "/app/mcp-server-stripe-org",
    "-e", "STRIPE_ORG_KEY", "-e", "STRIPE_ACCOUNTS",
    "ghcr.io/dvcrn/mcp-server-stripe-org:latest"
  ],
  "env": {
    "STRIPE_ORG_KEY": "rk_org_...",
    "STRIPE_ACCOUNTS": "[{\"alias\":\"shop\",\"name\":\"My Shop\",\"account\":\"acct_123\"}]"
  }
}
```

To serve HTTP at `http://localhost:8080/mcp` instead:

```sh
docker run --rm -p 8080:8080 \
  -e STRIPE_ORG_KEY -e STRIPE_ACCOUNTS -e MCP_TOKEN -e MCP_READ_TOKEN \
  ghcr.io/dvcrn/mcp-server-stripe-org:latest
```

## Run on Cloudflare Workers

Log in with `wrangler login` (or set `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID`). List your accounts in `accounts.json`, which is gitignored:

```sh
cp accounts.example.json accounts.json
```

Generate a random `MCP_TOKEN` and keep it, because clients send it as their bearer token. For read-only clients, generate a second one for `MCP_READ_TOKEN`:

```sh
openssl rand -hex 32
```

Then set the secrets and deploy:

```sh
wrangler secret put STRIPE_ORG_KEY
wrangler secret put MCP_TOKEN
wrangler secret put MCP_READ_TOKEN   # optional
mise run worker-accounts   # uploads accounts.json as STRIPE_ACCOUNTS
wrangler deploy
```

To revoke the read token, run `wrangler secret delete MCP_READ_TOKEN`; removing it from your secret store doesn't remove it from the worker.

Run `mise run worker-accounts` again whenever `accounts.json` changes. The new list takes effect without a redeploy.

`wrangler deploy` builds the Go wasm bundle through `mise run build-worker`. The MCP endpoint is `https://<worker>.<subdomain>.workers.dev/mcp`, and requests without the `MCP_TOKEN` or `MCP_READ_TOKEN` bearer token are rejected.

To use it from Claude Code, add it to `.mcp.json`. `stripe-org-readonly` is the same URL with the read token, so enable whichever connector a session or routine should have:

```json
{
  "mcpServers": {
    "stripe-org": {
      "type": "http",
      "url": "https://<worker>.<subdomain>.workers.dev/mcp",
      "headers": { "Authorization": "Bearer ${MCP_TOKEN}" }
    },
    "stripe-org-readonly": {
      "type": "http",
      "url": "https://<worker>.<subdomain>.workers.dev/mcp",
      "headers": { "Authorization": "Bearer ${MCP_READ_TOKEN}" }
    }
  }
}
```
