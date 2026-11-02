// Package stripe is a thin HTTP client for the Stripe API using an
// organization key and a per-request Stripe-Context account.
package stripe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	// APIVersion is sent as Stripe-Version on every v1 request and is the v2 default.
	APIVersion = "2026-10-28.endive"

	// PreviewAPIVersion is required by preview v2 endpoints such as /v2/data/*.
	PreviewAPIVersion = "2026-10-28.preview"

	// MaxResponseBytes is the largest response body the client returns.
	MaxResponseBytes = 1 << 20

	defaultBaseURL = "https://api.stripe.com"
	userAgent      = "mcp-server-stripe-org"

	maxIdempotencyKey = 255
)

var accountPattern = regexp.MustCompile(`^acct_[A-Za-z0-9]+$`)

// Client calls the Stripe API with an organization key.
type Client struct {
	httpClient *http.Client
	key        string
	baseURL    string
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at another host, such as an httptest server.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

// New returns a client that authenticates with the given organization key.
func New(httpClient *http.Client, key string, opts ...Option) *Client {
	c := &Client{httpClient: httpClient, key: key, baseURL: defaultBaseURL}
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// V1Request is a call to a /v1 endpoint.
type V1Request struct {
	Method string
	// Path starts with /v1/ and has its path parameters already escaped.
	Path string
	// Account is the acct_ ID sent as Stripe-Context. Required.
	Account string
	// Params is form-encoded into the body for POST and the query string otherwise.
	Params map[string]any
	// IdempotencyKey is sent on POST only.
	IdempotencyKey string
}

// V2Request is a call to a /v2 endpoint.
type V2Request struct {
	Method string
	// Path starts with /v2/ and has its path parameters already escaped.
	Path string
	// Account is the acct_ ID sent as Stripe-Context. Empty runs in org mode.
	Account string
	// Version overrides APIVersion, for example with PreviewAPIVersion.
	Version string
	// Query is appended to the URL.
	Query map[string]any
	// Body is JSON-encoded for POST; nil sends {}. Not allowed on GET or DELETE.
	Body any
	// IdempotencyKey is sent on POST only.
	IdempotencyKey string
}

// DoV1 sends a form-encoded request and returns the raw JSON response body.
func (c *Client) DoV1(ctx context.Context, req V1Request) ([]byte, error) {
	if err := checkRequest(req.Method, req.Path, "/v1/", req.IdempotencyKey); err != nil {
		return nil, err
	}

	if req.Account == "" {
		return nil, errors.New("stripe: v1 requests need an account for Stripe-Context")
	}

	if err := checkAccount(req.Account); err != nil {
		return nil, err
	}

	form, err := EncodeForm(req.Params)
	if err != nil {
		return nil, err
	}

	target := c.baseURL + req.Path
	var body io.Reader
	contentType := ""
	if req.Method == http.MethodPost {
		body = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else if len(form) > 0 {
		target += "?" + form.Encode()
	}

	return c.do(ctx, req.Method, target, body, contentType, APIVersion, req.Account, req.IdempotencyKey)
}

// DoV2 sends a JSON request and returns the raw JSON response body.
func (c *Client) DoV2(ctx context.Context, req V2Request) ([]byte, error) {
	if err := checkRequest(req.Method, req.Path, "/v2/", req.IdempotencyKey); err != nil {
		return nil, err
	}

	if req.Body != nil && req.Method != http.MethodPost {
		return nil, fmt.Errorf("stripe: %s requests cannot have a body", req.Method)
	}

	if req.Account != "" {
		if err := checkAccount(req.Account); err != nil {
			return nil, err
		}
	}

	query, err := EncodeForm(req.Query)
	if err != nil {
		return nil, err
	}

	target := c.baseURL + req.Path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var body io.Reader
	contentType := ""
	if req.Method == http.MethodPost {
		payload := []byte("{}")
		if req.Body != nil {
			payload, err = json.Marshal(req.Body)
			if err != nil {
				return nil, fmt.Errorf("stripe: encode request body: %w", err)
			}
		}

		body = bytes.NewReader(payload)
		contentType = "application/json"
	}

	version := req.Version
	if version == "" {
		version = APIVersion
	}

	return c.do(ctx, req.Method, target, body, contentType, version, req.Account, req.IdempotencyKey)
}

func (c *Client) do(ctx context.Context, method, target string, body io.Reader, contentType, version, account, idempotencyKey string) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("stripe: build request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.key)
	httpReq.Header.Set("Stripe-Version", version)
	httpReq.Header.Set("User-Agent", userAgent)
	if account != "" {
		httpReq.Header.Set("Stripe-Context", account)
	}

	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}

	if method == http.MethodPost && idempotencyKey != "" {
		httpReq.Header.Set("Idempotency-Key", idempotencyKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// url.Error repeats the full URL, whose query can hold customer data.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}

		return nil, fmt.Errorf("stripe: %s %s: %w", method, httpReq.URL.Path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("stripe: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, parseError(resp, data[:min(len(data), MaxResponseBytes)])
	}

	if len(data) > MaxResponseBytes {
		return nil, &ResponseTooLargeError{Limit: MaxResponseBytes}
	}

	return data, nil
}

func checkRequest(method, path, prefix, idempotencyKey string) error {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodDelete:
	default:
		return fmt.Errorf("stripe: unsupported method %q", method)
	}

	if !strings.HasPrefix(path, prefix) {
		return fmt.Errorf("stripe: path %q must start with %s", path, prefix)
	}

	if err := checkPath(path); err != nil {
		return err
	}

	if idempotencyKey != "" && (len(idempotencyKey) > maxIdempotencyKey || !isPrintableASCII(idempotencyKey)) {
		return fmt.Errorf("stripe: idempotency key must be at most %d printable ASCII characters", maxIdempotencyKey)
	}

	return nil
}

// checkPath rejects paths that could resolve to a different endpoint than
// the one the caller built, such as an ID of ".." or one containing "?".
func checkPath(path string) error {
	for _, segment := range strings.Split(path[1:], "/") {
		unescaped, err := url.PathUnescape(segment)
		if err != nil || segment == "" || unescaped == "." || unescaped == ".." ||
			strings.ContainsAny(segment, "?#") || !isPrintableASCII(segment) {
			return fmt.Errorf("stripe: invalid path %q", path)
		}
	}

	return nil
}

func isPrintableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}

	return true
}

// ValidAccountID reports whether id is an acct_ ID, the form Stripe-Context takes.
func ValidAccountID(id string) bool {
	return accountPattern.MatchString(id)
}

func checkAccount(account string) error {
	if !ValidAccountID(account) {
		return fmt.Errorf("stripe: invalid account %q, want an acct_ ID", account)
	}

	return nil
}
