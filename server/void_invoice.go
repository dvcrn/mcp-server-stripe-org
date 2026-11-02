package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type voidInvoiceInput struct {
	postInput
	Invoice string `json:"invoice" jsonschema:"finalized invoice ID, e.g. in_123"`
}

func voidInvoice(ctx context.Context, env *Env, in voidInvoiceInput) (*mcp.CallToolResult, any, error) {
	return writeV1(ctx, env, post("/v1/invoices/{invoice}/void", map[string]string{"invoice": in.Invoice}, in.postInput, nil))
}
