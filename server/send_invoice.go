package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type sendInvoiceInput struct {
	postInput
	Invoice string `json:"invoice" jsonschema:"open invoice ID with collection_method send_invoice, e.g. in_123"`
}

func sendInvoice(ctx context.Context, env *Env, in sendInvoiceInput) (*mcp.CallToolResult, any, error) {
	return writeV1(ctx, env, post("/v1/invoices/{invoice}/send", map[string]string{"invoice": in.Invoice}, in.postInput, nil))
}
