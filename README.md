# mcp-server-stripe-org

An MCP server for Stripe organizations, written in Go. It exposes read tools (`api_read`, `list_accounts`, API operation search), Sigma SQL analytics (`run_sql_query`, `get_query_run`, `list_query_tables`) and a set of write tools across every account of one Stripe organization. It runs as the Cloudflare Worker `mcp-server-stripe-org` at https://mcp-server-stripe-org.dsh.workers.dev, and `/mcp` requires `Authorization: Bearer $MCP_TOKEN`.

## Run locally

Secrets come from fnox, so run everything under `fnox exec`.

```sh
fnox exec -- go run ./cmd/mcp   # native build
mise run dev                    # the worker under wrangler dev, using .dev.vars from the active fnox profile
```

## Deploy

```sh
mise run deploy          # build and deploy the worker with the active fnox profile
mise run worker-secrets  # upload STRIPE_ORG_KEY, STRIPE_ACCOUNTS and MCP_TOKEN
```

`worker-secrets` refuses a live key unless you pass `-P production`.

## Use from Claude Code

`.mcp.json` registers the server as `stripe-org` and reads the bearer token from `MCP_TOKEN`. Start Claude Code under fnox so the token is in its environment and not in your shell history or process arguments:

```sh
fnox exec -- claude
```
