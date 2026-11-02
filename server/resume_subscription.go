package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type resumeSubscriptionInput struct {
	postInput
	Subscription string `json:"subscription" jsonschema:"subscription ID with a pending cancellation, e.g. sub_123"`
}

func resumeSubscription(ctx context.Context, env *Env, in resumeSubscriptionInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"cancel_at_period_end": false}

	return writeV1(ctx, env, post("/v1/subscriptions/{subscription}", map[string]string{"subscription": in.Subscription}, in.postInput, params))
}
