//go:build !js

// Command mcp runs the Stripe org MCP server natively: over stdio by default,
// or over HTTP with -http for local development.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvcrn/mcp-server-stripe-org/internal/httpclient"
	"github.com/dvcrn/mcp-server-stripe-org/server"
)

func main() {
	addr := flag.String("http", "", "serve HTTP on this address, e.g. :8080, instead of stdio")
	flag.Parse()

	if *addr != "" {
		log.Printf("serving MCP on http://%s/mcp", *addr)
		srv := &http.Server{Addr: *addr, Handler: server.Handler(os.Getenv), ReadHeaderTimeout: 10 * time.Second}
		log.Fatal(srv.ListenAndServe())
	}

	env, err := server.NewEnv(os.Getenv(server.EnvOrgKey), os.Getenv(server.EnvAccounts), httpclient.New())
	if err != nil {
		log.Fatal(err)
	}

	ctx := server.WithEnv(context.Background(), env)
	if err := server.New().Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
