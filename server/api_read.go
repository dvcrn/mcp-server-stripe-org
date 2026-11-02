package server

import (
	"context"
	"fmt"
	"maps"
	"math"
	"net/http"
	"regexp"
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

	for _, name := range []string{"expand", "include"} {
		if err := checkFieldPaths(op, name, params[name]); err != nil {
			return nil, err
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

// blockedFields are field names api_read never lets a caller expand (v1
// expand) or include (v2 include). Stripe leaves these values out of responses
// unless they are requested by name, so blocking the request keeps them out of
// the model's context. A path is blocked if any of its dot-separated segments
// is listed, so data.number on a list and number on a retrieve are both caught.
var blockedFields = map[string]string{
	"number":                  "Issuing card numbers and Identity document numbers",
	"cvc":                     "Issuing card CVCs",
	"network_data":            "device IP, phone and location from Issuing tokens",
	"verified_outputs":        "Identity verified personal data",
	"provided_details":        "email and phone of the person Identity verifies",
	"dob":                     "dates of birth from Identity",
	"id_number":               "government ID numbers from Identity",
	"expiration_date":         "Identity document expiry dates",
	"sex":                     "sex from Identity documents",
	"unparsed_sex":            "sex from Identity documents",
	"unparsed_place_of_birth": "places of birth from Identity documents",
	"account_number":          "full bank account numbers",
}

// fieldPathPattern is the shape of every Stripe expand and include path. Anything
// else is rejected so brackets, spaces or encodings can't hide a blocked segment.
var fieldPathPattern = regexp.MustCompile(`^[A-Za-z0-9_]+(\.[A-Za-z0-9_]+)*$`)

// checkFieldPaths rejects an expand or include value that is not a list of
// field paths or that names a blocked field.
func checkFieldPaths(op stripe.Operation, param string, v any) error {
	if v == nil {
		return nil
	}

	list, ok := v.([]any)
	if !ok {
		return fmt.Errorf("%s: %s must be a list of field paths, got %T", op.ID, param, v)
	}

	for _, item := range list {
		path, ok := item.(string)
		if !ok || !fieldPathPattern.MatchString(path) {
			return fmt.Errorf("%s: %s values must be dot-separated field names like data.customer, got %v", op.ID, param, item)
		}

		for segment := range strings.SplitSeq(strings.ToLower(path), ".") {
			if reason, blocked := blockedFields[segment]; blocked {
				return fmt.Errorf("%s: %s %q is blocked because %q returns %s", op.ID, param, path, segment, reason)
			}
		}
	}

	return nil
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

// readLimit reads params.limit, which arrives untyped, and applies pageLimit.
func readLimit(v any) (int, error) {
	var limit *float64
	if v != nil {
		f, ok := v.(float64)
		if !ok || f != math.Trunc(f) {
			return 0, fmt.Errorf("limit must be a whole number (default %d, max %d), got %v", defaultReadLimit, maxReadLimit, v)
		}

		limit = &f
	}

	return pageLimit(limit, defaultReadLimit, maxReadLimit)
}

func listOrNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}

	return strings.Join(names, ", ")
}
