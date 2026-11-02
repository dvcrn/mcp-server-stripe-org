//go:build js && wasm

// Command mcp-worker serves the Stripe org MCP server as a Cloudflare Worker.
package main

import (
	"runtime/debug"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare"

	"github.com/dvcrn/mcp-server-stripe-org/server"
)

// A Workers isolate has 128 MB shared across concurrent requests, so the GC soft limit is half of that.
func init() {
	debug.SetMemoryLimit(64 << 20)
}

func main() {
	workers.Serve(server.Handler(cloudflare.Getenv))
}
