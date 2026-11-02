package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvcrn/mcp-server-stripe-org/stripe"
)

const (
	queryRunsPath = "/v2/data/query_runs"

	// maxQueryRows is the most inline rows Stripe returns per page, and the default.
	maxQueryRows = 100

	statusRunning   = "running"
	statusSucceeded = "succeeded"
	statusFailed    = "failed"
	statusCanceled  = "canceled"
)

var queryRunIDPattern = regexp.MustCompile(`^qrun_[A-Za-z0-9_]+$`)

// queryRun holds the fields of a v2.data.query_run the tools read. The
// result's file download URL is not decoded so the tools never hand out download links.
type queryRun struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	StatusDetails *struct {
		Code        string            `json:"code"`
		Message     string            `json:"message"`
		QueryErrors []json.RawMessage `json:"query_errors"`
	} `json:"status_details"`
	Result *struct {
		RowCount string `json:"row_count"`
		Inline   *struct {
			Columns []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"columns"`
			Rows []struct {
				Data map[string]json.RawMessage `json:"data"`
			} `json:"rows"`
			NextPageURL string `json:"next_page_url"`
		} `json:"inline"`
	} `json:"result"`
}

// isPending reports a run that has not reached a terminal status. Statuses
// other than the known terminal ones count as pending.
func isPending(status string) bool {
	return status != statusSucceeded && status != statusFailed && status != statusCanceled
}

// optionalAccount resolves ref, where empty means org mode with no Stripe-Context.
func optionalAccount(env *Env, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}

	return env.resolveAccount(ref)
}

func decodeQueryRun(body []byte) (*queryRun, error) {
	var run queryRun
	if err := json.Unmarshal(body, &run); err != nil {
		return nil, fmt.Errorf("decode query run: %w", err)
	}

	return &run, nil
}

// fetchQueryRun reads a run with one page of its inline rows. account must
// be the one the run was created with, since Stripe hides runs across contexts.
func fetchQueryRun(ctx context.Context, env *Env, account, id string, limit int, page string) (*queryRun, error) {
	query := map[string]any{"include": []string{"result.inline"}, "limit": limit}
	if page != "" {
		query["page"] = page
	}

	body, err := env.Client.DoV2(ctx, stripe.V2Request{
		Method:  http.MethodGet,
		Path:    queryRunsPath + "/" + url.PathEscape(id),
		Account: account,
		Version: stripe.PreviewAPIVersion,
		Query:   query,
	})
	if err != nil {
		return nil, err
	}

	return decodeQueryRun(body)
}

// queryRunResult renders run as text: a header of key: value lines, then
// the inline rows as CSV for a succeeded run. accountRef is echoed so the
// follow-up call reuses it.
func queryRunResult(run *queryRun, accountRef string) (*mcp.CallToolResult, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "query_run_id: %s\n", run.ID)
	followUp := "get_query_run with this query_run_id"
	if accountRef != "" {
		fmt.Fprintf(&b, "account: %s\n", accountRef)
		followUp += " and account"
	}

	fmt.Fprintf(&b, "status: %s\n", run.Status)

	switch {
	case run.Status == statusSucceeded && hasInline(run):
		writeRows(&b, run, followUp)
	case run.Status == statusSucceeded:
		fmt.Fprintf(&b, "Call %s to read the rows.\n", followUp)
	case isPending(run.Status):
		fmt.Fprintf(&b, "The query has not finished. Call %s in 30 to 60 seconds. Large queries can take several minutes, so wait longer between later checks.\n", followUp)
	default:
		return nil, queryRunFailure(run)
	}

	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: b.String()}}}, nil
}

func queryRunFailure(run *queryRun) error {
	msg := fmt.Sprintf("query run %s %s", run.ID, run.Status)
	d := run.StatusDetails
	if d == nil {
		return errors.New(msg)
	}

	if d.Code != "" {
		msg += ": " + d.Code
	}

	if d.Message != "" {
		msg += ": " + d.Message
	}

	for _, qe := range d.QueryErrors {
		msg += "\n" + string(qe)
	}

	return errors.New(msg)
}

func writeRows(b *strings.Builder, run *queryRun, followUp string) {
	inline := run.Result.Inline
	if run.Result.RowCount != "" {
		fmt.Fprintf(b, "rows: %s total, %d on this page\n", run.Result.RowCount, len(inline.Rows))
	} else {
		fmt.Fprintf(b, "rows: %d on this page\n", len(inline.Rows))
	}

	if page := pageToken(inline.NextPageURL); page != "" {
		fmt.Fprintf(b, "next_page: %s\n", page)
		fmt.Fprintf(b, "More rows: call %s and page.\n", followUp)
	}

	columns := make([]string, len(inline.Columns))
	names := make([]string, len(inline.Columns))
	for i, c := range inline.Columns {
		columns[i] = c.Name + " " + c.Type
		names[i] = csvField(c.Name)
	}

	fmt.Fprintf(b, "columns: %s\n\n", strings.Join(columns, ", "))
	b.WriteString(strings.Join(names, ","))
	b.WriteString("\n")

	fields := make([]string, len(inline.Columns))
	for _, row := range inline.Rows {
		for i, c := range inline.Columns {
			fields[i] = csvValue(row.Data[c.Name])
		}

		b.WriteString(strings.Join(fields, ","))
		b.WriteString("\n")
	}
}

// pageToken extracts the page parameter from a next_page_url.
func pageToken(nextPageURL string) string {
	if nextPageURL == "" {
		return ""
	}

	u, err := url.Parse(nextPageURL)
	if err != nil {
		return ""
	}

	return u.Query().Get("page")
}

// csvValue renders a cell: null as an empty field, an empty string as "" so
// the two stay distinct, and non-string JSON values as written.
func csvValue(raw json.RawMessage) string {
	if raw == nil || string(raw) == "null" {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return csvField(string(raw))
	}

	if s == "" {
		return `""`
	}

	return csvField(s)
}

func csvField(s string) string {
	if !strings.ContainsAny(s, ",\"\r\n") && strings.TrimSpace(s) == s {
		return s
	}

	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
