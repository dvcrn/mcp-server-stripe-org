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
		Description: "List the Stripe accounts this server can read, with the alias to pass as `account` to other tools and whether the key is live or sandbox.",
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
		Description: "Show a Stripe GET operation's path parameters and query parameters with their types and descriptions, so api_read can be called correctly.",
		Annotations: readOnly(),
	}, getAPIOperation)

	add(s, &mcp.Tool{
		Name: "api_read",
		Description: "Call a Stripe GET operation by its operation ID and return the raw JSON response. " +
			"Find IDs with search_api_operations and parameters with get_api_operation. " +
			"`account` is required for /v1 operations; /v2 operations without it run across the whole organization. " +
			"List operations return 10 items unless params.limit is set (max 100); page with starting_after.",
		Annotations: readOnly(),
	}, apiRead)
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

func destructive() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(true)}
}

func ptr[T any](v T) *T { return &v }
