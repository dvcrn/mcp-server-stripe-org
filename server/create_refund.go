package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type createRefundInput struct {
	postInput
	PaymentIntent string            `json:"payment_intent,omitempty" jsonschema:"payment intent ID to refund, e.g. pi_123; set this or charge"`
	Charge        string            `json:"charge,omitempty" jsonschema:"charge ID to refund, e.g. ch_123; set this or payment_intent"`
	Amount        *int64            `json:"amount,omitempty" jsonschema:"amount to refund in the currency's smallest unit, e.g. 500 for $5.00; refunds the full remaining amount when omitted"`
	Reason        string            `json:"reason,omitempty" jsonschema:"duplicate, fraudulent or requested_by_customer"`
	Metadata      map[string]string `json:"metadata,omitempty" jsonschema:"metadata to set, e.g. {\"ticket\": \"1234\"}"`
}

func createRefund(ctx context.Context, env *Env, in createRefundInput) (*mcp.CallToolResult, any, error) {
	if (in.PaymentIntent == "") == (in.Charge == "") {
		return nil, nil, errors.New("set exactly one of payment_intent or charge")
	}

	if in.Amount != nil && *in.Amount <= 0 {
		return nil, nil, fmt.Errorf("amount must be positive, got %d; omit it to refund the full remaining amount", *in.Amount)
	}

	switch in.Reason {
	case "", "duplicate", "fraudulent", "requested_by_customer":
	default:
		return nil, nil, fmt.Errorf("reason must be duplicate, fraudulent or requested_by_customer, got %q", in.Reason)
	}

	params := setParams(map[string]any{}, map[string]any{
		"payment_intent": in.PaymentIntent,
		"charge":         in.Charge,
		"amount":         in.Amount,
		"reason":         in.Reason,
		"metadata":       in.Metadata,
	})

	return writeV1(ctx, env, post("/v1/refunds", nil, in.postInput, params))
}
