//go:build js && wasm

// Command worker serves the Stripe org MCP server as a Cloudflare Worker.
package main

import (
	"net/http"

	"github.com/syumai/workers-go"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})
	workers.Serve(mux)
}
