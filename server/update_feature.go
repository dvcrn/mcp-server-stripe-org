package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type updateFeatureInput struct {
	postInput
	Feature  string            `json:"feature" jsonschema:"entitlement feature ID, e.g. feat_123"`
	Name     string            `json:"name,omitempty" jsonschema:"new name shown to customers"`
	Active   *bool             `json:"active,omitempty" jsonschema:"false to archive the feature; archived features can't be attached to new products"`
	Metadata map[string]string `json:"metadata,omitempty" jsonschema:"metadata to set, e.g. {\"tier\": \"pro\"}; an empty value removes that key, keys left out are kept"`
}

func updateFeature(ctx context.Context, env *Env, in updateFeatureInput) (*mcp.CallToolResult, any, error) {
	params := setParams(map[string]any{}, map[string]any{"name": in.Name, "active": in.Active, "metadata": in.Metadata})
	if err := requireChange(params, "name", "active", "metadata"); err != nil {
		return nil, nil, err
	}

	return writeV1(ctx, env, "update_feature", post("/v1/entitlements/features/{feature}", map[string]string{"feature": in.Feature}, in.postInput, params))
}
