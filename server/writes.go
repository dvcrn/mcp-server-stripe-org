package server

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvcrn/mcp-server-stripe-org/stripe"
)

// accountInput is the target account of a write. Writes have no org mode.
type accountInput struct {
	Account string `json:"account" jsonschema:"account alias or acct_ ID from list_accounts, e.g. acme"`
}

// postInput adds the idempotency key that POST writes accept.
type postInput struct {
	accountInput
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"optional key (up to 255 printable ASCII characters) so a retried call is applied once, e.g. a UUID"`
}

// writeRequest is one Stripe call made by a write tool.
type writeRequest struct {
	method string
	// path is a /v1 template such as /v1/customers/{customer}; ids fills it.
	path           string
	ids            map[string]string
	account        string
	params         map[string]any
	idempotencyKey string
}

// writeV1 validates the IDs, resolves the account and sends the request,
// returning Stripe's JSON response as text content like api_read does.
func writeV1(ctx context.Context, env *Env, tool string, req writeRequest) (*mcp.CallToolResult, any, error) {
	pathParams := slices.Collect(maps.Keys(req.ids))
	path, err := stripe.Operation{ID: tool, Path: req.path, PathParams: pathParams}.BuildPath(req.ids)
	if err != nil {
		return nil, nil, err
	}

	account, err := env.resolveAccount(req.account)
	if err != nil {
		return nil, nil, err
	}

	body, err := env.Client.DoV1(ctx, stripe.V1Request{
		Method:         req.method,
		Path:           path,
		Account:        account,
		Params:         req.params,
		IdempotencyKey: req.idempotencyKey,
	})
	if err != nil {
		return nil, nil, err
	}

	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
}

func post(path string, ids map[string]string, in postInput, params map[string]any) writeRequest {
	return writeRequest{method: http.MethodPost, path: path, ids: ids, account: in.Account, params: params, idempotencyKey: in.IdempotencyKey}
}

func del(path string, ids map[string]string, in accountInput) writeRequest {
	return writeRequest{method: http.MethodDelete, path: path, ids: ids, account: in.Account}
}

// setParams copies the set fields into params: nil pointers and empty maps
// are left out, so a zero value never overwrites anything in Stripe.
func setParams(params map[string]any, fields map[string]any) map[string]any {
	for name, v := range fields {
		switch v := v.(type) {
		case *string:
			if v != nil {
				params[name] = *v
			}
		case *bool:
			if v != nil {
				params[name] = *v
			}
		case *int64:
			if v != nil {
				params[name] = *v
			}
		case *float64:
			if v != nil {
				params[name] = *v
			}
		case string:
			if v != "" {
				params[name] = v
			}
		case map[string]string:
			if len(v) > 0 {
				params[name] = v
			}
		default:
			// TestWriteRequests calls every tool, so an unhandled field type fails there first.
			panic(fmt.Sprintf("setParams: unsupported type %T for %s", v, name))
		}
	}

	return params
}

// requireChange fails an update that would send no fields.
func requireChange(params map[string]any, fields ...string) error {
	if len(params) > 0 {
		return nil
	}

	return fmt.Errorf("nothing to update; pass at least one of: %s", strings.Join(fields, ", "))
}
