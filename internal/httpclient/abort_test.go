package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

var errAborted = errors.New("AbortError: The operation was aborted")

// stallTransport mimics workers-go's fetch transport: it ignores the request
// context and only gives up when abort is called.
type stallTransport struct {
	aborted chan struct{}
	// respond makes RoundTrip return at once with a body that stalls until abort.
	respond bool
}

func (s *stallTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if s.respond {
		return &http.Response{StatusCode: http.StatusOK, Body: &stallBody{aborted: s.aborted}, Request: req}, nil
	}
	<-s.aborted
	return nil, errAborted
}

type stallBody struct{ aborted chan struct{} }

func (b *stallBody) Read([]byte) (int, error) {
	<-b.aborted
	return 0, errAborted
}

func (b *stallBody) Close() error { return nil }

type harness struct {
	transport *abortTransport
	stall     *stallTransport
	aborts    atomic.Int32
	prepares  atomic.Int32
}

func newHarness(respond bool) *harness {
	h := &harness{stall: &stallTransport{aborted: make(chan struct{}), respond: respond}}
	h.transport = &abortTransport{prepare: func() (http.RoundTripper, func()) {
		h.prepares.Add(1)
		return h.stall, func() {
			if h.aborts.Add(1) == 1 {
				close(h.stall.aborted)
			}
		}
	}}
	return h
}

func newRequest(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.stripe.test/v1/balance", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestAbortTransportContextDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(false)
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		start := time.Now()
		if _, err := h.transport.RoundTrip(newRequest(t, ctx)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want context.DeadlineExceeded", err)
		}
		if elapsed := time.Since(start); elapsed != 50*time.Millisecond {
			t.Fatalf("RoundTrip returned after %v, want the 50ms deadline", elapsed)
		}
		if got := h.aborts.Load(); got != 1 {
			t.Fatalf("aborts = %d, want 1", got)
		}
	})
}

func TestAbortTransportClientTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(false)
		client := &http.Client{Transport: h.transport, Timeout: 50 * time.Millisecond}

		start := time.Now()
		_, err := client.Do(newRequest(t, t.Context()))
		var netErr interface{ Timeout() bool }
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("err = %v, want a timeout error", err)
		}
		if elapsed := time.Since(start); elapsed != 50*time.Millisecond {
			t.Fatalf("Do returned after %v, want the 50ms client timeout", elapsed)
		}
	})
}

func TestAbortTransportCancelledBeforeSend(t *testing.T) {
	h := newHarness(false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	body := &trackingBody{Reader: strings.NewReader("amount=100")}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.stripe.test/v1/refunds", body)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.transport.RoundTrip(req); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := h.prepares.Load(); got != 0 {
		t.Fatalf("prepare called %d times for an already cancelled request", got)
	}
	if !body.closed {
		t.Fatal("request body was not closed")
	}
}

func TestAbortTransportBodyReadCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(true)
		ctx, cancel := context.WithCancel(t.Context())
		res, err := h.transport.RoundTrip(newRequest(t, ctx))
		if err != nil {
			t.Fatalf("RoundTrip: %v", err)
		}
		defer res.Body.Close()
		time.AfterFunc(20*time.Millisecond, cancel)

		if _, err := io.ReadAll(res.Body); !errors.Is(err, context.Canceled) {
			t.Fatalf("read err = %v, want context.Canceled", err)
		}
	})
}

func TestAbortTransportBodyEOFAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	transport := &abortTransport{prepare: func() (http.RoundTripper, func()) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		})
		return rt, func() {}
	}}
	res, err := transport.RoundTrip(newRequest(t, ctx))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer res.Body.Close()
	cancel()

	if _, err := res.Body.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("read err = %v, want io.EOF", err)
	}
}

func TestAbortTransportCloseStopsAbort(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(true)
		ctx, cancel := context.WithCancel(t.Context())
		res, err := h.transport.RoundTrip(newRequest(t, ctx))
		if err != nil {
			t.Fatalf("RoundTrip: %v", err)
		}
		res.Body.Close()
		cancel()
		synctest.Wait()

		if got := h.aborts.Load(); got != 0 {
			t.Fatalf("abort called %d times after the body was closed", got)
		}
	})
}

func TestAbortTransportPassesThroughErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		want := errors.New("TypeError: network connection lost")
		var aborts atomic.Int32
		transport := &abortTransport{prepare: func() (http.RoundTripper, func()) {
			rt := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, want })
			return rt, func() { aborts.Add(1) }
		}}
		ctx, cancel := context.WithCancel(t.Context())

		if _, err := transport.RoundTrip(newRequest(t, ctx)); !errors.Is(err, want) {
			t.Fatalf("err = %v, want %v", err, want)
		}
		cancel()
		synctest.Wait()
		if got := aborts.Load(); got != 0 {
			t.Fatalf("abort called %d times after RoundTrip failed", got)
		}
	})
}

type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}
