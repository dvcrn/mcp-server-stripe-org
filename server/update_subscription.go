package server

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type updateSubscriptionInput struct {
	postInput
	Subscription string            `json:"subscription" jsonschema:"subscription ID, e.g. sub_123"`
	TrialEnd     string            `json:"trial_end,omitempty" jsonschema:"new trial end: \"now\" to end the trial immediately, or Unix seconds as a base-10 string, e.g. \"1767225600\""`
	Metadata     map[string]string `json:"metadata,omitempty" jsonschema:"metadata to set, e.g. {\"order\": \"1234\"}; an empty value removes that key, keys left out are kept"`
}

var unixSecondsPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

func updateSubscription(ctx context.Context, env *Env, in updateSubscriptionInput) (*mcp.CallToolResult, any, error) {
	if err := checkTrialEnd(in.TrialEnd); err != nil {
		return nil, nil, err
	}

	params := setParams(map[string]any{}, map[string]any{"trial_end": in.TrialEnd, "metadata": in.Metadata})
	if err := requireChange(params, "trial_end", "metadata"); err != nil {
		return nil, nil, err
	}

	return writeV1(ctx, env, post("/v1/subscriptions/{subscription}", map[string]string{"subscription": in.Subscription}, in.postInput, params))
}

func checkTrialEnd(v string) error {
	if v == "" || v == "now" {
		return nil
	}

	if _, err := strconv.ParseInt(v, 10, 64); err == nil && unixSecondsPattern.MatchString(v) {
		return nil
	}

	return fmt.Errorf("trial_end must be \"now\" or Unix seconds as a base-10 string such as \"1767225600\", got %q", v)
}
