//go:build js && wasm

package httpclient

import (
	"errors"
	"net/http"
	"syscall/js"

	"github.com/syumai/workers-go/cloudflare/fetch"
)

var errRedirect = errors.New("httpclient: redirects are not followed")

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

	// Workers rejects redirect mode "error", so fetch returns 3xx responses
	// and they are refused here to keep the org key on the Stripe host.
	rt := fetch.NewClient(fetch.WithBinding(binding)).HTTPClient(fetch.RedirectModeManual).Transport
	send := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		defer fn.Release()
		res, err := rt.RoundTrip(req)
		if err == nil && res.StatusCode >= 300 && res.StatusCode < 400 {
			res.Body.Close()
			return nil, errRedirect
		}
		return res, err
	})
	abort := func() { controller.Call("abort") }
	return send, abort
}
