package server

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type createPromotionCodeInput struct {
	postInput
	Coupon         string            `json:"coupon" jsonschema:"coupon ID the code applies, e.g. SUMMER25"`
	Code           string            `json:"code,omitempty" jsonschema:"code customers type in, e.g. SUMMER25OFF; Stripe generates one when omitted"`
	Customer       string            `json:"customer,omitempty" jsonschema:"restrict the code to this customer ID, e.g. cus_123"`
	MaxRedemptions *int64            `json:"max_redemptions,omitempty" jsonschema:"maximum number of redemptions, e.g. 50"`
	ExpiresAt      *int64            `json:"expires_at,omitempty" jsonschema:"when the code stops working, Unix seconds, e.g. 1767225600"`
	Metadata       map[string]string `json:"metadata,omitempty" jsonschema:"metadata to set, e.g. {\"campaign\": \"summer\"}"`
}

func createPromotionCode(ctx context.Context, env *Env, in createPromotionCodeInput) (*mcp.CallToolResult, any, error) {
	if in.Coupon == "" {
		return nil, nil, errors.New("coupon is required; list coupons with api_read operation GetCoupons")
	}

	params := setParams(map[string]any{
		"promotion": map[string]any{"type": "coupon", "coupon": in.Coupon},
	}, map[string]any{
		"code":            in.Code,
		"customer":        in.Customer,
		"max_redemptions": in.MaxRedemptions,
		"expires_at":      in.ExpiresAt,
		"metadata":        in.Metadata,
	})

	return writeV1(ctx, env, post("/v1/promotion_codes", nil, in.postInput, params))
}
