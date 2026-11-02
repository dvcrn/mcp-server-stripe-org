package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type finalizeInvoiceInput struct {
	postInput
	Invoice     string `json:"invoice" jsonschema:"draft invoice ID, e.g. in_123"`
	AutoAdvance *bool  `json:"auto_advance,omitempty" jsonschema:"whether Stripe should then collect or send the invoice automatically; defaults to the invoice's own setting"`
}

func finalizeInvoice(ctx context.Context, env *Env, in finalizeInvoiceInput) (*mcp.CallToolResult, any, error) {
	params := setParams(map[string]any{}, map[string]any{"auto_advance": in.AutoAdvance})

	return writeV1(ctx, env, "finalize_invoice", post("/v1/invoices/{invoice}/finalize", map[string]string{"invoice": in.Invoice}, in.postInput, params))
}
