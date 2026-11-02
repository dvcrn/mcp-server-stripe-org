package server

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type cancelSubscriptionInput struct {
	accountInput
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"optional key (up to 255 printable ASCII characters) so a retried call is applied once, e.g. a UUID; only with at_period_end=true"`
	Subscription   string `json:"subscription" jsonschema:"subscription ID, e.g. sub_123"`
	AtPeriodEnd    bool   `json:"at_period_end,omitempty" jsonschema:"true to cancel when the current period ends (undo with resume_subscription); false or omitted cancels now"`
}

func cancelSubscription(ctx context.Context, env *Env, in cancelSubscriptionInput) (*mcp.CallToolResult, any, error) {
	ids := map[string]string{"subscription": in.Subscription}
	if in.AtPeriodEnd {
		return writeV1(ctx, env, post("/v1/subscriptions/{subscription}", ids, postInput{in.accountInput, in.IdempotencyKey}, map[string]any{"cancel_at_period_end": true}))
	}

	if in.IdempotencyKey != "" {
		return nil, nil, errors.New("idempotency_key only applies with at_period_end=true; immediate cancellation is a DELETE, which takes no idempotency key, so call again without it")
	}

	return writeV1(ctx, env, del("/v1/subscriptions/{subscription}", ids, in.accountInput))
}
