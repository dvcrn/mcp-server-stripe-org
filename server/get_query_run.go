package server

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type getQueryRunInput struct {
	QueryRunID string `json:"query_run_id" jsonschema:"query_run_id from run_sql_query, e.g. qrun_test_61abc"`
	Account    string `json:"account,omitempty" jsonschema:"the account passed to run_sql_query; omit if the query ran across the organization"`
	Limit      *int   `json:"limit,omitempty" jsonschema:"rows per page (default 100, max 100)"`
	Page       string `json:"page,omitempty" jsonschema:"next_page value from a previous result, e.g. page_abc123"`
}

func getQueryRun(ctx context.Context, env *Env, in getQueryRunInput) (*mcp.CallToolResult, any, error) {
	if !queryRunIDPattern.MatchString(in.QueryRunID) {
		return nil, nil, fmt.Errorf("query_run_id %q is not a qrun_ ID from run_sql_query", in.QueryRunID)
	}

	limit, err := pageLimit(in.Limit, maxQueryRows, maxQueryRows)
	if err != nil {
		return nil, nil, err
	}

	account, err := optionalAccount(env, in.Account)
	if err != nil {
		return nil, nil, err
	}

	run, err := fetchQueryRun(ctx, env, account, in.QueryRunID, limit, in.Page)
	if err != nil {
		return nil, nil, err
	}

	if run.Status == statusSucceeded && !hasInline(run) {
		return nil, nil, fmt.Errorf("query run %s succeeded but Stripe returned no inline rows", run.ID)
	}

	res, err := queryRunResult(run, in.Account)
	if err != nil {
		return nil, nil, err
	}

	return res, nil, nil
}
