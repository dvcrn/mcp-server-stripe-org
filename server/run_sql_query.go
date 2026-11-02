package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvcrn/mcp-server-stripe-org/stripe"
)

var (
	// pollBudget bounds how long run_sql_query waits for a run, counted from the call's start.
	pollBudget = 10 * time.Second
	// pollDelays are the waits before each status check; the last one repeats.
	pollDelays = []time.Duration{time.Second, 2 * time.Second}
	// minPollRequest is the budget a status check needs left to be worth sending.
	minPollRequest = time.Second
)

type runSQLQueryInput struct {
	SQL     string `json:"sql" jsonschema:"Trino SQL over the tables from list_query_tables, e.g. SELECT currency, sum(amount) FROM charges GROUP BY 1"`
	Account string `json:"account,omitempty" jsonschema:"account alias or acct_ ID from list_accounts; omit to query every account in the organization"`
}

func runSQLQuery(ctx context.Context, env *Env, in runSQLQueryInput) (*mcp.CallToolResult, any, error) {
	pollCtx, cancel := context.WithTimeout(ctx, pollBudget)
	defer cancel()

	sql := strings.TrimSpace(in.SQL)
	if sql == "" {
		return nil, nil, errors.New("sql is required")
	}

	account, err := optionalAccount(env, in.Account)
	if err != nil {
		return nil, nil, err
	}

	body, err := env.Client.DoV2(ctx, stripe.V2Request{
		Method:  http.MethodPost,
		Path:    queryRunsPath,
		Account: account,
		Version: stripe.PreviewAPIVersion,
		Body: map[string]any{
			"query":   map[string]string{"sql": sql},
			"dataset": "analytical",
			"format":  "csv",
		},
	})
	if err != nil {
		return nil, nil, err
	}

	run, err := decodeQueryRun(body)
	if err != nil {
		return nil, nil, err
	}

	if run, err = waitForQueryRun(pollCtx, env, account, run); err != nil {
		return nil, nil, err
	}

	res, err := queryRunResult(run, in.Account)
	if err != nil {
		return nil, nil, err
	}

	return res, nil, nil
}

// waitForQueryRun polls a running run until it finishes or ctx's deadline
// is near, then returns the latest state. It fetches a succeeded run once
// for its inline rows, which the create response does not carry.
func waitForQueryRun(ctx context.Context, env *Env, account string, run *queryRun) (*queryRun, error) {
	for attempt := 0; isPending(run.Status); attempt++ {
		delay := pollDelays[min(attempt, len(pollDelays)-1)]
		next, done, err := pollOnce(ctx, env, account, run, delay)
		if done || err != nil {
			return next, err
		}

		run = next
	}

	if run.Status == statusSucceeded && !hasInline(run) {
		next, _, err := pollOnce(ctx, env, account, run, 0)
		return next, err
	}

	return run, nil
}

// pollOnce waits delay and reads run again. done reports that the budget
// ran out, in which case run is returned unchanged.
func pollOnce(ctx context.Context, env *Env, account string, run *queryRun, delay time.Duration) (*queryRun, bool, error) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < delay+minPollRequest {
		return run, true, nil
	}

	select {
	case <-ctx.Done():
		return run, true, nil
	case <-time.After(delay):
	}

	next, err := fetchQueryRun(ctx, env, account, run.ID, maxQueryRows, "")
	switch {
	case err == nil:
		return next, false, nil
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return run, true, nil
	default:
		return nil, false, fmt.Errorf("query run %s: %w; retry with get_query_run", run.ID, err)
	}
}

func hasInline(run *queryRun) bool {
	return run.Result != nil && run.Result.Inline != nil
}
