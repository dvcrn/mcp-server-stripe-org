package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type updatePriceInput struct {
	postInput
	Price     string            `json:"price" jsonschema:"price ID, e.g. price_123"`
	Active    *bool             `json:"active,omitempty" jsonschema:"false to archive the price so it can't be used in new purchases, true to restore it"`
	Nickname  *string           `json:"nickname,omitempty" jsonschema:"internal name, e.g. Pro monthly; an empty string removes it"`
	LookupKey *string           `json:"lookup_key,omitempty" jsonschema:"key for finding the price, unique per account, e.g. pro_monthly; an empty string removes it"`
	Metadata  map[string]string `json:"metadata,omitempty" jsonschema:"metadata to set, e.g. {\"tier\": \"pro\"}; an empty value removes that key, keys left out are kept"`
}

func updatePrice(ctx context.Context, env *Env, in updatePriceInput) (*mcp.CallToolResult, any, error) {
	params := setParams(map[string]any{}, map[string]any{
		"active":     in.Active,
		"nickname":   in.Nickname,
		"lookup_key": in.LookupKey,
		"metadata":   in.Metadata,
	})
	if err := requireChange(params, "active", "nickname", "lookup_key", "metadata"); err != nil {
		return nil, nil, err
	}

	return writeV1(ctx, env, "update_price", post("/v1/prices/{price}", map[string]string{"price": in.Price}, in.postInput, params))
}
