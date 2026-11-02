package server

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvcrn/mcp-server-stripe-org/stripe"
)

const (
	defaultSearchLimit = 10
	maxSearchLimit     = 50
)

type searchAPIOperationsInput struct {
	Query string `json:"query" jsonschema:"keywords that must all appear in the operation ID, summary or path, e.g. list invoices"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of operations to return (default 10, max 50)"`
}

type operationSummary struct {
	OperationID string `json:"operation_id"`
	Path        string `json:"path"`
	Summary     string `json:"summary,omitempty"`
}

type searchAPIOperationsOutput struct {
	Operations []operationSummary `json:"operations"`
	Note       string             `json:"note,omitempty"`
}

func searchAPIOperations(_ context.Context, _ *Env, in searchAPIOperationsInput) (*mcp.CallToolResult, searchAPIOperationsOutput, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	limit = min(limit, maxSearchLimit)

	ops := stripe.SearchOperations(in.Query, limit)
	out := searchAPIOperationsOutput{Operations: make([]operationSummary, len(ops))}
	for i, op := range ops {
		out.Operations[i] = operationSummary{OperationID: op.ID, Path: op.Path, Summary: op.Summary}
	}

	if len(ops) == 0 {
		out.Note = fmt.Sprintf("no GET operation matches every word of %q; try fewer or more general words, such as a resource name like \"invoices\"", in.Query)
	}

	return nil, out, nil
}
