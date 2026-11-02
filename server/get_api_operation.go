package server

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvcrn/mcp-server-stripe-org/stripe"
)

type getAPIOperationInput struct {
	OperationID string `json:"operation_id" jsonschema:"operation ID from search_api_operations, e.g. GetCustomersCustomer"`
}

type queryParamOutput struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type getAPIOperationOutput struct {
	OperationID     string             `json:"operation_id"`
	Path            string             `json:"path"`
	Summary         string             `json:"summary,omitempty"`
	AccountRequired bool               `json:"account_required"`
	PathParams      []string           `json:"path_params"`
	QueryParams     []queryParamOutput `json:"query_params"`
}

func getAPIOperation(_ context.Context, _ *Env, in getAPIOperationInput) (*mcp.CallToolResult, getAPIOperationOutput, error) {
	op, err := lookupOperation(in.OperationID)
	if err != nil {
		return nil, getAPIOperationOutput{}, err
	}

	out := getAPIOperationOutput{
		OperationID:     op.ID,
		Path:            op.Path,
		Summary:         op.Summary,
		AccountRequired: !op.IsV2(),
		PathParams:      append([]string{}, op.PathParams...),
		QueryParams:     make([]queryParamOutput, len(op.QueryParams)),
	}
	for i, p := range op.QueryParams {
		out.QueryParams[i] = queryParamOutput{Name: p.Name, Type: p.Type, Description: p.Description, Required: p.Required, Enum: p.Enum}
	}

	return nil, out, nil
}

func lookupOperation(id string) (stripe.Operation, error) {
	op, ok := stripe.LookupOperation(id)
	if !ok {
		return stripe.Operation{}, fmt.Errorf("unknown operation %q; find GET operation IDs with search_api_operations", id)
	}

	return op, nil
}
