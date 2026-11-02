package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type updateProductInput struct {
	postInput
	Product      string            `json:"product" jsonschema:"product ID, e.g. prod_123"`
	Name         string            `json:"name,omitempty" jsonschema:"new name shown to customers"`
	Description  *string           `json:"description,omitempty" jsonschema:"new description shown to customers; an empty string removes it"`
	Active       *bool             `json:"active,omitempty" jsonschema:"false to archive the product so it can't be used in new purchases, true to restore it"`
	DefaultPrice string            `json:"default_price,omitempty" jsonschema:"ID of one of the product's prices to make its default, e.g. price_123"`
	Metadata     map[string]string `json:"metadata,omitempty" jsonschema:"metadata to set, e.g. {\"tier\": \"pro\"}; an empty value removes that key, keys left out are kept"`
}

func updateProduct(ctx context.Context, env *Env, in updateProductInput) (*mcp.CallToolResult, any, error) {
	params := setParams(map[string]any{}, map[string]any{
		"name":          in.Name,
		"description":   in.Description,
		"active":        in.Active,
		"default_price": in.DefaultPrice,
		"metadata":      in.Metadata,
	})
	if err := requireChange(params, "name", "description", "active", "default_price", "metadata"); err != nil {
		return nil, nil, err
	}

	return writeV1(ctx, env, "update_product", post("/v1/products/{product}", map[string]string{"product": in.Product}, in.postInput, params))
}
