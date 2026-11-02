# mcp-server-stripe-org

An MCP server for a Stripe organization, written in Go. Read tools: `list_accounts`, `search_api_operations`, `get_api_operation`, `api_read`. Analytics tools: `run_sql_query`, `get_query_run`, `list_query_tables`. Write tools cover customers, invoices, subscriptions, coupons, promotion codes, refunds, products, prices and features. Tools take an `account` alias from `list_accounts`, and only the accounts in `STRIPE_ACCOUNTS` are reachable.

It runs as the Cloudflare Worker `mcp-server-stripe-org` at https://mcp-server-stripe-org.dsh.workers.dev, where `/mcp` requires `Authorization: Bearer $MCP_TOKEN`. The toolchain is pinned in `mise.toml`, so use `mise run` and `mise install`.

## Configuration

Secrets come from fnox, so run commands under `fnox exec`. The server needs `STRIPE_ORG_KEY` and `STRIPE_ACCOUNTS`, and the HTTP endpoint also needs `MCP_TOKEN`. The cloud profile (`fnox.cloud.toml`) defines all three. The default and `production` profiles in `fnox.toml` define only `STRIPE_ORG_KEY`, so add the other two there before using them.

## Run locally

```sh
fnox exec -- go run ./cmd/mcp                # MCP over stdio
fnox exec -- go run ./cmd/mcp -http :8080    # MCP over HTTP at /mcp
mise run dev                                 # the worker under wrangler dev, with a .dev.vars written from the active profile
```

## Deploy

```sh
mise run deploy          # build and deploy the worker (needs CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID)
mise run worker-secrets  # upload STRIPE_ORG_KEY, STRIPE_ACCOUNTS and MCP_TOKEN from the active profile
```

`worker-secrets` fails if any of the three is empty in the profile, and refuses a live key unless you pass `-P production`.

## Use from Claude Code

`.mcp.json` registers the HTTP endpoint as `stripe-org` and reads the bearer token from `MCP_TOKEN`. Start Claude Code under fnox so the variable is set from a profile that defines it, such as the cloud profile:

```sh
fnox exec -- claude
```

The token must match the `MCP_TOKEN` last uploaded with `mise run worker-secrets`. Claude Code asks once to approve the project server.
