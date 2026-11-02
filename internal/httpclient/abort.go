package httpclient

import (
	"context"
	"io"
	"net/http"
)

// abortTransport enforces request context cancellation on a transport that
// ignores it, using a per-request abort hook. http.Client.Timeout reaches it
// as a deadline on the request context.
type abortTransport struct {
	// prepare returns the round tripper for one request and abort to fail
	// that request and its response body.
	prepare func() (rt http.RoundTripper, abort func())
}

func (t *abortTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if err := ctx.Err(); err != nil {
		if req.Body != nil {
			req.Body.Close()
		}
		return nil, err
	}

	rt, abort := t.prepare()
	stop := context.AfterFunc(ctx, abort)
	res, err := rt.RoundTrip(req)
	if err != nil {
		stop()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}

	res.Body = &abortBody{ReadCloser: res.Body, ctx: ctx, stop: stop}
	return res, nil
}

// abortBody reports a body read cut short by abort as the context's error.
type abortBody struct {
	io.ReadCloser
	ctx  context.Context
	stop func() bool
}

func (b *abortBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && err != io.EOF {
		if ctxErr := b.ctx.Err(); ctxErr != nil {
			return n, ctxErr
		}
	}
	return n, err
}

func (b *abortBody) Close() error {
	b.stop()
	return b.ReadCloser.Close()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
