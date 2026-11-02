// Package server is the Stripe org MCP server: tool registrations, auth and
// the HTTP wiring shared by the worker and the native binary.
package server

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	serverOnce sync.Once
	server     *mcp.Server
)

// New returns the MCP server. Tool schemas are built once per process (one
// Workers isolate); per-request config comes from the context, see WithEnv.
func New() *mcp.Server {
	serverOnce.Do(func() {
		server = mcp.NewServer(&mcp.Implementation{Name: "stripe-org", Version: "0.1.0"}, nil)
		tools(server)
	})

	return server
}

func tools(s *mcp.Server) {
	add(s, &mcp.Tool{
		Name:        "list_accounts",
		Description: "List the Stripe accounts this server can read and write, with the alias to pass as `account` to other tools and whether the key is live or sandbox.",
		Annotations: readOnly(),
	}, listAccounts)

	add(s, &mcp.Tool{
		Name: "search_api_operations",
		Description: "Search the Stripe API's GET operations by keywords, for example \"list invoices\" or \"subscription schedule\". " +
			"Returns operation IDs to pass to get_api_operation and api_read.",
		Annotations: readOnly(),
	}, searchAPIOperations)

	add(s, &mcp.Tool{
		Name:        "get_api_operation",
		Description: "Show a Stripe GET operation's path parameters, query parameters (types, descriptions, required, enums) and whether api_read needs an account.",
		Annotations: readOnly(),
	}, getAPIOperation)

	add(s, &mcp.Tool{
		Name: "api_read",
		Description: "Call a Stripe GET operation by its operation ID and return the raw JSON response. " +
			"Find IDs with search_api_operations and parameters with get_api_operation. " +
			"`account` is required for /v1 operations; /v2 operations without it run across the whole organization. " +
			"List operations return 10 items unless params.limit is set (max 100). " +
			"Page /v1 lists with params.starting_after and /v2 lists with params.page from next_page_url.",
		Annotations: readOnly(),
	}, apiRead)

	add(s, &mcp.Tool{
		Name: "run_sql_query",
		Description: "Run a read-only Stripe Sigma SQL query (Trino dialect) and return the first 100 rows as CSV; page through the rest with get_query_run. " +
			"Find tables and columns with list_query_tables. Without `account` the query covers every account in the organization. " +
			"Queries often take 30 s or more; if the run has not finished within 10 s this returns its query_run_id and status for get_query_run. " +
			"Rerunning an identical query reuses the earlier run.",
		Annotations: readOnly(),
	}, runSQLQuery)

	add(s, &mcp.Tool{
		Name: "get_query_run",
		Description: "Check a run_sql_query run and return its rows as CSV once it has succeeded. " +
			"Pass the same `account` the query ran with. Page through results with `page` from next_page.",
		Annotations: readOnly(),
	}, getQueryRun)

	add(s, &mcp.Tool{
		Name: "list_query_tables",
		Description: "List the tables run_sql_query can read, with a one-line description each. " +
			"Pass `tables` to get those tables' columns, types, keys and usage notes.",
		Annotations: readOnly(),
	}, listQueryTables)
	writeTools(s)
}

// writeTools registers the curated write tools.
func writeTools(s *mcp.Server) {
	add(s, &mcp.Tool{
		Name:        "update_customer",
		Description: "Update a customer's email, name, description or metadata. Returns the updated customer.",
		Annotations: idempotent(),
	}, updateCustomer)

	add(s, &mcp.Tool{
		Name: "finalize_invoice",
		Description: "Finalize a draft invoice so it can be paid or sent. A finalized invoice can no longer be edited, only voided. " +
			"Returns the invoice.",
		Annotations: mutating(),
	}, finalizeInvoice)

	add(s, &mcp.Tool{
		Name: "send_invoice",
		Description: "Email an open invoice to its customer. Only works for invoices with collection_method send_invoice. " +
			"This emails the customer; confirm with the user first. Returns the invoice.",
		Annotations: mutating(),
	}, sendInvoice)

	add(s, &mcp.Tool{
		Name: "void_invoice",
		Description: "Void a finalized invoice so it can never be paid. This can't be undone; confirm with the user first. " +
			"Returns the invoice.",
		Annotations: destructive(),
	}, voidInvoice)

	add(s, &mcp.Tool{
		Name: "cancel_subscription",
		Description: "Cancel a subscription now, or at the end of the current period with at_period_end. " +
			"Cancelling now can't be undone and stops billing immediately; confirm with the user first. " +
			"idempotency_key is accepted only with at_period_end. Returns the subscription.",
		Annotations: destructive(),
	}, cancelSubscription)

	add(s, &mcp.Tool{
		Name: "resume_subscription",
		Description: "Undo a pending cancellation made with cancel_subscription at_period_end, so the subscription renews. " +
			"It sets cancel_at_period_end to false and leaves any cancel_at date in place. " +
			"Doesn't apply to subscriptions that are already canceled or paused. Returns the subscription.",
		Annotations: idempotent(),
	}, resumeSubscription)

	add(s, &mcp.Tool{
		Name: "update_subscription",
		Description: "Update a subscription's metadata or trial end. Changing trial_end moves the billing cycle anchor " +
			"and can create a prorated invoice; \"now\" ends the trial and bills immediately. Returns the subscription.",
		Annotations: mutating(),
	}, updateSubscription)

	add(s, &mcp.Tool{
		Name: "create_coupon",
		Description: "Create a coupon with either percent_off or amount_off plus currency. " +
			"To give customers a code to type, create a promotion code for it with create_promotion_code. Returns the coupon.",
		Annotations: mutating(),
	}, createCoupon)

	add(s, &mcp.Tool{
		Name: "delete_coupon",
		Description: "Delete a coupon so it can't be redeemed again; existing discounts stay. " +
			"This can't be undone; confirm with the user first.",
		Annotations: destructive(),
	}, deleteCoupon)

	add(s, &mcp.Tool{
		Name:        "create_promotion_code",
		Description: "Create a customer-facing promotion code for a coupon. Returns the promotion code, including its promo_ ID.",
		Annotations: mutating(),
	}, createPromotionCode)

	add(s, &mcp.Tool{
		Name:        "update_promotion_code",
		Description: "Activate or deactivate a promotion code, or change its metadata. Returns the promotion code.",
		Annotations: idempotent(),
	}, updatePromotionCode)

	add(s, &mcp.Tool{
		Name: "create_refund",
		Description: "Refund a payment intent or charge, in full or for part of the amount. " +
			"This returns money to the customer and can't be undone; confirm with the user first, including the payment and amount. " +
			"Pass an idempotency_key so a retried call can't refund twice. Returns the refund.",
		Annotations: destructive(),
	}, createRefund)

	add(s, &mcp.Tool{
		Name:        "update_product",
		Description: "Update a product's name, description, active state, default price or metadata. Returns the product.",
		Annotations: idempotent(),
	}, updateProduct)

	add(s, &mcp.Tool{
		Name: "update_price",
		Description: "Update a price's active state, nickname, lookup key or metadata. A price's amount can't change; " +
			"archive it and create a new one instead. Returns the price.",
		Annotations: idempotent(),
	}, updatePrice)

	add(s, &mcp.Tool{
		Name:        "update_feature",
		Description: "Update an entitlement feature's name, active state or metadata. Returns the feature.",
		Annotations: idempotent(),
	}, updateFeature)
}

// add registers a tool whose handler receives the request's Env. An Out of
// any gives the tool no output schema.
func add[In, Out any](s *mcp.Server, tool *mcp.Tool, h func(context.Context, *Env, In) (*mcp.CallToolResult, Out, error)) {
	t := *tool
	t.InputSchema = schemaFor[In](t.Name)
	if reflect.TypeFor[Out]().Kind() != reflect.Interface {
		t.OutputSchema = schemaFor[Out](t.Name)
	}

	mcp.AddTool(s, &t, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		env, err := envFrom(ctx)
		if err != nil {
			var zero Out
			return nil, zero, err
		}

		return h(ctx, env, in)
	})
}

// schemaFor infers T's schema and drops "null" from nilable fields (slices,
// maps, pointers), which tools always fill or omit and which some clients
// read as a different kind of parameter.
func schemaFor[T any](tool string) *jsonschema.Schema {
	s, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("tool %s: %v", tool, err))
	}

	dropNull(s)

	return s
}

func dropNull(s *jsonschema.Schema) {
	if s == nil {
		return
	}

	if types := slices.DeleteFunc(slices.Clone(s.Types), func(t string) bool { return t == "null" }); len(types) != len(s.Types) {
		s.Types = nil
		if len(types) == 1 {
			s.Type = types[0]
		} else {
			s.Types = types
		}
	}

	dropNull(s.Items)
	dropNull(s.AdditionalProperties)
	for _, p := range s.Properties {
		dropNull(p)
	}
}

func readOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)}
}

func mutating() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(true)}
}

// idempotent marks a non-destructive write that has no further effect when repeated with the same arguments.
func idempotent() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(true)}
}

func destructive() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(true)}
}

func ptr[T any](v T) *T { return &v }
