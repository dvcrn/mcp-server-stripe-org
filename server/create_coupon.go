package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvcrn/mcp-server-stripe-org/stripe"
)

type createCouponInput struct {
	postInput
	ID               string            `json:"id,omitempty" jsonschema:"custom coupon ID of letters, digits, _ or -, e.g. SUMMER25; Stripe generates one when omitted"`
	Name             string            `json:"name,omitempty" jsonschema:"name shown to customers on invoices and receipts, e.g. Summer sale"`
	PercentOff       *float64          `json:"percent_off,omitempty" jsonschema:"percentage discount, above 0 and up to 100, e.g. 25; set this or amount_off"`
	AmountOff        *int64            `json:"amount_off,omitempty" jsonschema:"fixed discount in the currency's smallest unit, e.g. 500 for $5.00; set this or percent_off"`
	Currency         string            `json:"currency,omitempty" jsonschema:"three-letter ISO currency code for amount_off, e.g. usd"`
	Duration         string            `json:"duration,omitempty" jsonschema:"how long the discount applies to a subscription: once (default), repeating or forever"`
	DurationInMonths *int64            `json:"duration_in_months,omitempty" jsonschema:"number of months the discount applies; required when duration is repeating"`
	MaxRedemptions   *int64            `json:"max_redemptions,omitempty" jsonschema:"maximum total redemptions across all customers, e.g. 100"`
	RedeemBy         *int64            `json:"redeem_by,omitempty" jsonschema:"last time the coupon can be redeemed, Unix seconds, e.g. 1767225600"`
	Metadata         map[string]string `json:"metadata,omitempty" jsonschema:"metadata to set, e.g. {\"campaign\": \"summer\"}"`
}

func createCoupon(ctx context.Context, env *Env, in createCouponInput) (*mcp.CallToolResult, any, error) {
	if err := checkCoupon(in); err != nil {
		return nil, nil, err
	}

	params := setParams(map[string]any{}, map[string]any{
		"id":                 in.ID,
		"name":               in.Name,
		"percent_off":        in.PercentOff,
		"amount_off":         in.AmountOff,
		"currency":           in.Currency,
		"duration":           in.Duration,
		"duration_in_months": in.DurationInMonths,
		"max_redemptions":    in.MaxRedemptions,
		"redeem_by":          in.RedeemBy,
		"metadata":           in.Metadata,
	})

	return writeV1(ctx, env, post("/v1/coupons", nil, in.postInput, params))
}

// checkCoupon rejects parameter combinations that Stripe refuses.
func checkCoupon(in createCouponInput) error {
	switch {
	case in.ID != "" && !stripe.ValidPathParam(in.ID):
		return fmt.Errorf("id may contain only letters, digits, _ and - so delete_coupon can address it, got %q", in.ID)
	case (in.PercentOff == nil) == (in.AmountOff == nil):
		return errors.New("set exactly one of percent_off or amount_off")
	case in.PercentOff != nil && (*in.PercentOff <= 0 || *in.PercentOff > 100):
		return fmt.Errorf("percent_off must be above 0 and at most 100, got %v", *in.PercentOff)
	case in.AmountOff != nil && *in.AmountOff <= 0:
		return fmt.Errorf("amount_off must be positive, got %d", *in.AmountOff)
	case in.AmountOff != nil && in.Currency == "":
		return errors.New("currency is required with amount_off, e.g. usd")
	case in.AmountOff == nil && in.Currency != "":
		return errors.New("currency only applies with amount_off")
	}

	switch in.Duration {
	case "", "once", "forever":
		if in.DurationInMonths != nil {
			return errors.New("duration_in_months only applies with duration repeating")
		}
	case "repeating":
		if in.DurationInMonths == nil || *in.DurationInMonths < 1 {
			return errors.New("duration repeating needs duration_in_months of 1 or more")
		}
	default:
		return fmt.Errorf("duration must be once, repeating or forever, got %q", in.Duration)
	}

	return nil
}
