package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type deleteCouponInput struct {
	accountInput
	Coupon string `json:"coupon" jsonschema:"coupon ID, e.g. SUMMER25"`
}

func deleteCoupon(ctx context.Context, env *Env, in deleteCouponInput) (*mcp.CallToolResult, any, error) {
	return writeV1(ctx, env, del("/v1/coupons/{coupon}", map[string]string{"coupon": in.Coupon}, in.accountInput))
}
