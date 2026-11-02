package stripe

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Error is a non-2xx response from Stripe.
type Error struct {
	Status      int
	Type        string
	Code        string
	DeclineCode string
	Param       string
	Message     string
	RequestID   string
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "stripe: HTTP %d", e.Status)
	for _, s := range []string{e.Type, e.Code, e.DeclineCode} {
		if s != "" {
			b.WriteString(" ")
			b.WriteString(s)
		}
	}

	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}

	var details []string
	if e.Param != "" {
		details = append(details, "param: "+e.Param)
	}

	if e.RequestID != "" {
		details = append(details, "request: "+e.RequestID)
	}

	if len(details) > 0 {
		b.WriteString(" (" + strings.Join(details, ", ") + ")")
	}

	return b.String()
}

// ResponseTooLargeError is returned instead of a body over MaxResponseBytes.
type ResponseTooLargeError struct {
	Limit int
}

func (e *ResponseTooLargeError) Error() string {
	return fmt.Sprintf("stripe: response is larger than %d KB; use a smaller limit or narrower filters", e.Limit>>10)
}

const maxErrorSnippet = 200

// parseError reads Stripe's error envelope, falling back to the status text
// and a body snippet when the body is not one.
func parseError(resp *http.Response, body []byte) *Error {
	e := &Error{Status: resp.StatusCode, RequestID: resp.Header.Get("Request-Id")}

	var envelope struct {
		Error struct {
			Type        string `json:"type"`
			Code        string `json:"code"`
			DeclineCode string `json:"decline_code"`
			Param       string `json:"param"`
			Message     string `json:"message"`
		} `json:"error"`
	}

	if json.Unmarshal(body, &envelope) == nil && envelope.Error.Message+envelope.Error.Code+envelope.Error.Type != "" {
		e.Type = envelope.Error.Type
		e.Code = envelope.Error.Code
		e.DeclineCode = envelope.Error.DeclineCode
		e.Param = envelope.Error.Param
		e.Message = envelope.Error.Message
		return e
	}

	snippet := strings.TrimSpace(string(body))
	if len(snippet) > maxErrorSnippet {
		snippet = strings.ToValidUTF8(snippet[:maxErrorSnippet], "") + "..."
	}

	e.Message = http.StatusText(resp.StatusCode)
	if snippet != "" {
		e.Message += ": " + snippet
	}

	return e
}
