package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type updateCustomerInput struct {
	postInput
	Customer    string            `json:"customer" jsonschema:"customer ID, e.g. cus_123"`
	Email       *string           `json:"email,omitempty" jsonschema:"new email address; an empty string removes it"`
	Name        *string           `json:"name,omitempty" jsonschema:"new full name or business name; an empty string removes it"`
	Description *string           `json:"description,omitempty" jsonschema:"new internal description; an empty string removes it"`
	Metadata    map[string]string `json:"metadata,omitempty" jsonschema:"metadata to set, e.g. {\"order\": \"1234\"}; an empty value removes that key, keys left out are kept"`
}

func updateCustomer(ctx context.Context, env *Env, in updateCustomerInput) (*mcp.CallToolResult, any, error) {
	params := setParams(map[string]any{}, map[string]any{
		"email":       in.Email,
		"name":        in.Name,
		"description": in.Description,
		"metadata":    in.Metadata,
	})
	if err := requireChange(params, "email", "name", "description", "metadata"); err != nil {
		return nil, nil, err
	}

	return writeV1(ctx, env, "update_customer", post("/v1/customers/{customer}", map[string]string{"customer": in.Customer}, in.postInput, params))
}
