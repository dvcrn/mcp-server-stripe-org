package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type updatePromotionCodeInput struct {
	postInput
	PromotionCode string            `json:"promotion_code" jsonschema:"promotion code ID (not the code customers type), e.g. promo_123"`
	Active        *bool             `json:"active,omitempty" jsonschema:"false to stop the code from being redeemed, true to allow it again"`
	Metadata      map[string]string `json:"metadata,omitempty" jsonschema:"metadata to set, e.g. {\"campaign\": \"summer\"}; an empty value removes that key, keys left out are kept"`
}

func updatePromotionCode(ctx context.Context, env *Env, in updatePromotionCodeInput) (*mcp.CallToolResult, any, error) {
	params := setParams(map[string]any{}, map[string]any{"active": in.Active, "metadata": in.Metadata})
	if err := requireChange(params, "active", "metadata"); err != nil {
		return nil, nil, err
	}

	return writeV1(ctx, env, post("/v1/promotion_codes/{promotion_code}", map[string]string{"promotion_code": in.PromotionCode}, in.postInput, params))
}
