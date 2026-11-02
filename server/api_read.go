package server

import (
	"context"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvcrn/mcp-server-stripe-org/stripe"
)

const (
	defaultReadLimit = 10
	maxReadLimit     = 100
)

type apiReadInput struct {
	OperationID string            `json:"operation_id" jsonschema:"operation ID from search_api_operations, e.g. GetCustomersCustomer"`
	Account     string            `json:"account,omitempty" jsonschema:"account alias or acct_ ID from list_accounts; required for /v1 operations, omit on /v2 to query the whole organization"`
	PathParams  map[string]string `json:"path_params,omitempty" jsonschema:"values for the {placeholders} in the operation path, e.g. {\"customer\": \"cus_123\"}"`
	Params      map[string]any    `json:"params,omitempty" jsonschema:"query parameters the operation declares, e.g. {\"limit\": 20, \"expand\": [\"data.customer\"], \"created\": {\"gte\": 1700000000}}"`
}

// apiRead returns the response as text content only, so a large payload is
// not duplicated into structured content.
func apiRead(ctx context.Context, env *Env, in apiReadInput) (*mcp.CallToolResult, any, error) {
	op, err := lookupOperation(in.OperationID)
	if err != nil {
		return nil, nil, err
	}

	path, err := op.BuildPath(in.PathParams)
	if err != nil {
		return nil, nil, err
	}

	params, err := queryParams(op, in.Params)
	if err != nil {
		return nil, nil, err
	}

	account := ""
	if in.Account != "" || !op.IsV2() {
		if account, err = env.resolveAccount(in.Account); err != nil {
			return nil, nil, err
		}
	}

	var body []byte
	if op.IsV2() {
		body, err = env.Client.DoV2(ctx, stripe.V2Request{Method: http.MethodGet, Path: path, Account: account, Query: params})
	} else {
		body, err = env.Client.DoV1(ctx, stripe.V1Request{Method: http.MethodGet, Path: path, Account: account, Params: params})
	}

	if err != nil {
		return nil, nil, err
	}

	return textResult(string(body)), nil, nil
}

// queryParams rejects parameters op does not declare, checks required ones
// and applies the default and maximum limit.
func queryParams(op stripe.Operation, in map[string]any) (map[string]any, error) {
	params := maps.Clone(in)
	if params == nil {
		params = map[string]any{}
	}

	accepted := queryParamsOf(op)
	declared := make([]string, len(accepted))
	for i, p := range accepted {
		declared[i] = p.Name
	}

	for _, name := range slices.Sorted(maps.Keys(params)) {
		if !slices.Contains(declared, name) {
			return nil, fmt.Errorf("%s does not accept query parameter %q; it accepts %s", op.ID, name, listOrNone(declared))
		}
	}

	for _, p := range accepted {
		if p.Required && isEmpty(params[p.Name]) {
			return nil, fmt.Errorf("%s needs query parameter %q (%s)", op.ID, p.Name, p.Type)
		}
	}

	if slices.Contains(declared, "limit") {
		limit, err := readLimit(params["limit"])
		if err != nil {
			return nil, err
		}

		params["limit"] = limit
	}

	return params, nil
}

// v2PageParam is the cursor /v2 lists take from next_page_url, which the
// OpenAPI spec leaves out of their declared parameters.
var v2PageParam = stripe.Param{
	Name:        "page",
	Type:        "string",
	Description: "Page token from the page= value of a previous response's next_page_url or previous_page_url.",
}

// queryParamsOf returns the query parameters api_read accepts for op.
func queryParamsOf(op stripe.Operation) []stripe.Param {
	params := op.QueryParams
	if op.IsV2() && slices.ContainsFunc(params, func(p stripe.Param) bool { return p.Name == "limit" }) {
		params = append(slices.Clone(params), v2PageParam)
	}

	return params
}

// isEmpty reports values that leave a required parameter effectively unset:
// nil, "", or an empty list or object.
func isEmpty(v any) bool {
	switch v := v.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	}

	return false
}

func readLimit(v any) (int, error) {
	if v == nil {
		return defaultReadLimit, nil
	}

	f, ok := v.(float64)
	if !ok || f != math.Trunc(f) || f < 1 {
		return 0, fmt.Errorf("limit must be a positive whole number (values above %d are capped), got %v", maxReadLimit, v)
	}

	return int(min(f, maxReadLimit)), nil
}

func listOrNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}

	return strings.Join(names, ", ")
}
