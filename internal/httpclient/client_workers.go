//go:build js && wasm

package httpclient

import (
	"net/http"
	"syscall/js"

	"github.com/syumai/workers-go/cloudflare/fetch"
)

// newClient wraps workers-go's fetch transport, which ignores the request
// context, so each fetch carries an AbortSignal tied to that context.
func newClient() *http.Client {
	return &http.Client{Transport: &abortTransport{prepare: prepareFetch}}
}

// prepareFetch routes one request through a binding whose fetch adds an
// AbortSignal to the RequestInit before calling the global fetch.
func prepareFetch() (http.RoundTripper, func()) {
	controller := js.Global().Get("AbortController").New()
	globalFetch := js.Global().Get("fetch")
	fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		init := args[1]
		init.Set("signal", controller.Get("signal"))
		return globalFetch.Invoke(args[0], init)
	})
	binding := js.Global().Get("Object").New()
	binding.Set("fetch", fn)

	rt := fetch.NewClient(fetch.WithBinding(binding)).HTTPClient(fetch.RedirectModeFollow).Transport
	send := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		defer fn.Release()
		return rt.RoundTrip(req)
	})
	abort := func() { controller.Call("abort") }
	return send, abort
}
