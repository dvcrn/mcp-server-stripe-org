// Package httpclient builds the outbound HTTP client for the current platform.
package httpclient

import (
	"net/http"
	"time"
)

// New returns an outbound client backed by fetch on Workers and net/http elsewhere.
func New() *http.Client {
	client := newClient()
	client.Timeout = 10 * time.Second
	return client
}
