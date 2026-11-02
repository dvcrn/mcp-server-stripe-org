package server

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dvcrn/mcp-server-stripe-org/stripe"
)

// fakeQueryRuns serves /v2/data/query_runs: the run is created running and
// reports finalStatus once it has been read pollsUntilDone times.
type fakeQueryRuns struct {
	t              *testing.T
	pollsUntilDone int
	finalStatus    string
	createStatus   string
	// noInline makes reads omit inline rows; failRead makes them return 500;
	// readDelay slows each read down.
	noInline  bool
	failRead  bool
	readDelay time.Duration

	mu      sync.Mutex
	polls   int
	created []string
	reads   []stripeCall
	sql     string
}

const fakeRunID = "qrun_test_dummy1"

// Dummy rows; the third has a null name and the fourth an empty one.
var fakeRows = []string{
	`{"data":{"id":"1","name":"plain"}}`,
	`{"data":{"id":"2","name":"a, \"quoted\" value"}}`,
	`{"data":{"id":"3","name":null}}`,
	`{"data":{"id":"4","name":""}}`,
}

func (f *fakeQueryRuns) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if v := r.Header.Get("Stripe-Version"); v != stripe.PreviewAPIVersion {
		f.t.Errorf("Stripe-Version = %q, want %q", v, stripe.PreviewAPIVersion)
	}

	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == queryRunsPath:
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query   struct{ SQL string } `json:"query"`
			Dataset string               `json:"dataset"`
			Format  string               `json:"format"`
		}
		if err := json.Unmarshal(body, &req); err != nil || req.Dataset != "analytical" || req.Format != "csv" {
			f.t.Errorf("create body = %s", body)
		}

		f.sql = req.Query.SQL
		f.created = append(f.created, r.Header.Get("Stripe-Context"))
		status := f.createStatus
		if status == "" {
			status = statusRunning
		}

		writeRun(w, status, "", false)
	case r.Method == http.MethodGet && r.URL.Path == queryRunsPath+"/"+fakeRunID:
		if f.readDelay > 0 {
			f.mu.Unlock()
			time.Sleep(f.readDelay)
			f.mu.Lock()
		}

		if f.failRead {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":{"type":"api_error","message":"dummy outage"}}`))
			return
		}

		f.reads = append(f.reads, stripeCall{r.URL.Path, r.URL.RawQuery, r.Header.Get("Stripe-Context")})
		f.polls++
		if f.polls < f.pollsUntilDone {
			writeRun(w, statusRunning, "", true)
			return
		}

		writeRun(w, f.finalStatus, r.URL.Query().Get("page"), !f.noInline)
	default:
		f.t.Errorf("unexpected Stripe request %s %s", r.Method, r.URL)
		http.Error(w, "unexpected", http.StatusTeapot)
	}
}

// writeRun writes a run in status. A succeeded run read with inline rows has
// two pages of two rows; created ones carry only counts, as Stripe's do.
func writeRun(w http.ResponseWriter, status, page string, inline bool) {
	result, details := "null", "null"
	switch {
	case status == statusSucceeded && !inline:
		result = `{"row_count":"4","col_count":"2"}`
	case status == statusSucceeded:
		rows, next := fakeRows[:2], `"/v2/data/query_runs/`+fakeRunID+`?page=page_dummy2&include%5B0%5D=result.inline"`
		if page == "page_dummy2" {
			rows, next = fakeRows[2:], "null"
		}

		result = `{"row_count":"4","col_count":"2","inline":{"columns":[{"name":"id","type":"bigint"},{"name":"name","type":"varchar"}],` +
			`"rows":[` + strings.Join(rows, ",") + `],"next_page_url":` + next + `,"previous_page_url":null}}`
	case status == "failed":
		details = `{"canceled_at":null,"code":"execution_failed","message":"Your query failed during execution.","query_errors":[]}`
	}

	w.Write([]byte(`{"id":"` + fakeRunID + `","object":"v2.data.query_run","status":"` + status + `","result":` + result + `,"status_details":` + details + `}`))
}

func fastPolling(t *testing.T, budget time.Duration) {
	oldBudget, oldDelays, oldMin := pollBudget, pollDelays, minPollRequest
	pollBudget, pollDelays, minPollRequest = budget, []time.Duration{5 * time.Millisecond}, 5*time.Millisecond
	t.Cleanup(func() { pollBudget, pollDelays, minPollRequest = oldBudget, oldDelays, oldMin })
}

const wantFirstPage = `query_run_id: qrun_test_dummy1
status: succeeded
rows: 4 total, 2 on this page
next_page: page_dummy2
More rows: call get_query_run with this query_run_id and page.
columns: id bigint, name varchar

id,name
1,plain
2,"a, ""quoted"" value"
`

func TestRunSQLQueryFinishesInTime(t *testing.T) {
	fastPolling(t, 2*time.Second)
	f := &fakeQueryRuns{t: t, pollsUntilDone: 3, finalStatus: statusSucceeded}
	cs := connect(t, "sk_org_test_dummy", f.ServeHTTP)

	res := call(t, cs, "run_sql_query", map[string]any{"sql": "  SELECT id, name FROM dummy  "})
	if got := wantOK(t, res); got != wantFirstPage {
		t.Errorf("text =\n%s\nwant\n%s", got, wantFirstPage)
	}

	if res.StructuredContent != nil {
		t.Errorf("structured content = %v, want none", res.StructuredContent)
	}

	if f.sql != "SELECT id, name FROM dummy" {
		t.Errorf("sql = %q", f.sql)
	}

	if f.polls != 3 {
		t.Errorf("polled %d times, want 3", f.polls)
	}

	if q := f.reads[0].query; q != "include%5B0%5D=result.inline&limit=100" {
		t.Errorf("poll query = %s", q)
	}
}

func TestRunSQLQueryStillRunning(t *testing.T) {
	fastPolling(t, 100*time.Millisecond)
	f := &fakeQueryRuns{t: t, pollsUntilDone: 1 << 30}
	cs := connect(t, "sk_org_test_dummy", f.ServeHTTP)

	start := time.Now()
	got := wantOK(t, call(t, cs, "run_sql_query", map[string]any{"sql": "SELECT 1", "account": "dummy-one"}))
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Errorf("took %v, want the poll budget to stop it", elapsed)
	}

	want := "query_run_id: qrun_test_dummy1\naccount: dummy-one\nstatus: running\n" +
		"The query has not finished. Call get_query_run with this query_run_id and account in 30 to 60 seconds. Large queries can take several minutes, so wait longer between later checks.\n"
	if got != want {
		t.Errorf("text =\n%s\nwant\n%s", got, want)
	}

	if f.polls == 0 {
		t.Error("never polled")
	}
}

func TestRunSQLQueryDedupedSucceeded(t *testing.T) {
	fastPolling(t, 2*time.Second)
	f := &fakeQueryRuns{t: t, createStatus: statusSucceeded, finalStatus: statusSucceeded}
	cs := connect(t, "sk_org_test_dummy", f.ServeHTTP)

	if got := wantOK(t, call(t, cs, "run_sql_query", map[string]any{"sql": "SELECT 1"})); got != wantFirstPage {
		t.Errorf("text =\n%s", got)
	}

	if f.polls != 1 {
		t.Errorf("read %d times, want 1", f.polls)
	}
}

func TestRunSQLQuerySucceededWithoutInline(t *testing.T) {
	fastPolling(t, 2*time.Second)
	f := &fakeQueryRuns{t: t, createStatus: statusSucceeded, finalStatus: statusSucceeded, noInline: true}
	cs := connect(t, "sk_org_test_dummy", f.ServeHTTP)

	got := wantOK(t, call(t, cs, "run_sql_query", map[string]any{"sql": "SELECT 1"}))
	if !strings.Contains(got, "status: succeeded\nCall get_query_run") {
		t.Errorf("text = %s", got)
	}

	if f.polls != 1 {
		t.Errorf("read %d times, want 1", f.polls)
	}

	wantError(t, call(t, cs, "get_query_run", map[string]any{"query_run_id": fakeRunID}), "no inline rows")
}

func TestRunSQLQueryPollError(t *testing.T) {
	fastPolling(t, 2*time.Second)
	f := &fakeQueryRuns{t: t, failRead: true}
	cs := connect(t, "sk_org_test_dummy", f.ServeHTTP)

	wantError(t, call(t, cs, "run_sql_query", map[string]any{"sql": "SELECT 1"}), "query run qrun_test_dummy1: stripe: HTTP 500")
}

func TestRunSQLQuerySlowRead(t *testing.T) {
	fastPolling(t, 150*time.Millisecond)
	f := &fakeQueryRuns{t: t, pollsUntilDone: 1, finalStatus: statusSucceeded, readDelay: time.Second}
	cs := connect(t, "sk_org_test_dummy", f.ServeHTTP)

	start := time.Now()
	got := wantOK(t, call(t, cs, "run_sql_query", map[string]any{"sql": "SELECT 1"}))
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("took %v, want the poll budget to cut the read short", elapsed)
	}

	if !strings.Contains(got, "status: running\n") {
		t.Errorf("text = %s", got)
	}
}

func TestRunSQLQueryFailed(t *testing.T) {
	fastPolling(t, 2*time.Second)
	f := &fakeQueryRuns{t: t, pollsUntilDone: 1, finalStatus: "failed"}
	cs := connect(t, "sk_org_test_dummy", f.ServeHTTP)

	wantError(t, call(t, cs, "run_sql_query", map[string]any{"sql": "SELECT 1"}), "query run qrun_test_dummy1 failed: execution_failed: Your query failed during execution.")
}

func TestRunSQLQueryOrgModeAndAccount(t *testing.T) {
	fastPolling(t, 2*time.Second)
	f := &fakeQueryRuns{t: t, pollsUntilDone: 1, finalStatus: statusSucceeded}
	cs := connect(t, "sk_org_test_dummy", f.ServeHTTP)

	wantOK(t, call(t, cs, "run_sql_query", map[string]any{"sql": "SELECT 1"}))
	wantOK(t, call(t, cs, "run_sql_query", map[string]any{"sql": "SELECT 1", "account": "dummy-two"}))

	if len(f.created) != 2 || f.created[0] != "" || f.created[1] != "acct_1DUMMYTWO" {
		t.Errorf("create Stripe-Context = %q, want org mode then acct_1DUMMYTWO", f.created)
	}

	if len(f.reads) != 2 || f.reads[0].context != "" || f.reads[1].context != "acct_1DUMMYTWO" {
		t.Errorf("reads = %+v", f.reads)
	}
}

func TestRunSQLQueryErrors(t *testing.T) {
	cs := connect(t, "sk_org_test_dummy", noStripe(t))

	wantError(t, call(t, cs, "run_sql_query", map[string]any{"sql": "  "}), "sql is required")
	wantError(t, call(t, cs, "run_sql_query", map[string]any{"sql": "SELECT 1", "account": "nope"}), "unknown account")
}

func TestGetQueryRunPaging(t *testing.T) {
	f := &fakeQueryRuns{t: t, finalStatus: statusSucceeded}
	cs := connect(t, "sk_org_test_dummy", f.ServeHTTP)

	got := wantOK(t, call(t, cs, "get_query_run", map[string]any{"query_run_id": fakeRunID, "account": "dummy-one", "limit": 2, "page": "page_dummy2"}))
	want := "query_run_id: qrun_test_dummy1\naccount: dummy-one\nstatus: succeeded\nrows: 4 total, 2 on this page\ncolumns: id bigint, name varchar\n\n" +
		"id,name\n3,\n4,\"\"\n"
	if got != want {
		t.Errorf("text =\n%s\nwant\n%s", got, want)
	}

	wantOK(t, call(t, cs, "get_query_run", map[string]any{"query_run_id": fakeRunID}))

	wantReads := []stripeCall{
		{queryRunsPath + "/" + fakeRunID, "include%5B0%5D=result.inline&limit=2&page=page_dummy2", "acct_1DUMMYONE"},
		{queryRunsPath + "/" + fakeRunID, "include%5B0%5D=result.inline&limit=100", ""},
	}
	if len(f.reads) != 2 || f.reads[0] != wantReads[0] || f.reads[1] != wantReads[1] {
		t.Errorf("reads = %+v, want %+v", f.reads, wantReads)
	}
}

func TestGetQueryRunRunning(t *testing.T) {
	f := &fakeQueryRuns{t: t, pollsUntilDone: 1 << 30}
	cs := connect(t, "sk_org_test_dummy", f.ServeHTTP)

	got := wantOK(t, call(t, cs, "get_query_run", map[string]any{"query_run_id": fakeRunID}))
	want := "query_run_id: " + fakeRunID + "\nstatus: running\n" +
		"The query has not finished. Call get_query_run with this query_run_id in 30 to 60 seconds. Large queries can take several minutes, so wait longer between later checks.\n"
	if got != want {
		t.Errorf("text =\n%s\nwant\n%s", got, want)
	}
}

func TestGetQueryRunErrors(t *testing.T) {
	cs := connect(t, "sk_org_test_dummy", noStripe(t))

	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"query_run_id": "../v1/files"}, "not a qrun_ ID"},
		{map[string]any{"query_run_id": ""}, "not a qrun_ ID"},
		{map[string]any{"query_run_id": fakeRunID, "limit": -1}, "limit must be between 1 and 100"},
		{map[string]any{"query_run_id": fakeRunID, "limit": 101}, "limit must be between 1 and 100"},
		{map[string]any{"query_run_id": fakeRunID, "account": "nope"}, "unknown account"},
	} {
		wantError(t, call(t, cs, "get_query_run", tc.args), tc.want)
	}
}

func TestGetQueryRunFailed(t *testing.T) {
	f := &fakeQueryRuns{t: t, finalStatus: "failed"}
	cs := connect(t, "sk_org_test_dummy", f.ServeHTTP)

	wantError(t, call(t, cs, "get_query_run", map[string]any{"query_run_id": fakeRunID}), "execution_failed")
}

// fakeSchemas serves two pages of tables and the columns of "charges".
func fakeSchemas(t *testing.T, calls *[]stripeCall) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, stripeCall{r.URL.Path, r.URL.RawQuery, r.Header.Get("Stripe-Context")})
		if r.Method != http.MethodGet || r.URL.Path != schemasPath || r.Header.Get("Stripe-Version") != stripe.PreviewAPIVersion {
			t.Errorf("unexpected Stripe request %s %s (version %s)", r.Method, r.URL, r.Header.Get("Stripe-Version"))
		}

		q := r.URL.Query()
		var body string
		switch {
		case q.Get("name") == "charges":
			body = `{"data":[{"name":"charges","description":"Charge objects.","extended_description":"Amounts are in\nminor units.","columns":[` +
				`{"name":"id","type":"varchar","description":"Unique identifier.","is_primary_key":true,"foreign_keys_to":[]},` +
				`{"name":"customer_id","type":"varchar","description":"The  customer.","is_primary_key":false,"foreign_keys_to":[{"column":"id","schema":"schema_dummy","schema_name":"customers"}]}` +
				`]}],"next_page_url":null}`
		case q.Has("name"):
			body = `{"data":[],"next_page_url":null}`
		case q.Get("page") == "page_dummy2":
			body = `{"data":[{"name":"customers","description":"Customer objects.","columns":[]}],"next_page_url":null}`
		default:
			body = `{"data":[{"name":"charges","description":"Charge\n objects.","columns":[]}],"next_page_url":"/v2/data/schemas?page=page_dummy2&limit=100"}`
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}
}

func TestListQueryTables(t *testing.T) {
	var calls []stripeCall
	cs := connect(t, "sk_org_test_dummy", fakeSchemas(t, &calls))

	res := call(t, cs, "list_query_tables", nil)
	want := "2 tables. Call list_query_tables with tables for their columns.\n\ncharges: Charge objects.\ncustomers: Customer objects.\n"
	if got := wantOK(t, res); got != want {
		t.Errorf("text =\n%s\nwant\n%s", got, want)
	}

	if res.StructuredContent != nil {
		t.Errorf("structured content = %v, want none", res.StructuredContent)
	}

	wantCalls := []stripeCall{{schemasPath, "limit=100", ""}, {schemasPath, "limit=100&page=page_dummy2", ""}}
	if len(calls) != 2 || calls[0] != wantCalls[0] || calls[1] != wantCalls[1] {
		t.Errorf("calls = %+v, want %+v", calls, wantCalls)
	}
}

func TestListQueryTablesColumns(t *testing.T) {
	var calls []stripeCall
	cs := connect(t, "sk_org_test_dummy", fakeSchemas(t, &calls))

	got := wantOK(t, call(t, cs, "list_query_tables", map[string]any{"tables": []any{"charges", "charges"}}))
	want := "## charges\nCharge objects.\n\nAmounts are in\nminor units.\n\ncolumns:\n" +
		"- id varchar primary key: Unique identifier.\n- customer_id varchar -> customers.id: The customer.\n\n"
	if got != want {
		t.Errorf("text =\n%s\nwant\n%s", got, want)
	}

	if len(calls) != 1 || calls[0].query != "include%5B0%5D=columns&name=charges" {
		t.Errorf("calls = %+v", calls)
	}

	wantError(t, call(t, cs, "list_query_tables", map[string]any{"tables": []any{"charges", "nope"}}), "unknown tables nope")
	wantError(t, call(t, cs, "list_query_tables", map[string]any{"tables": []any{"a", "b", "c", "d", "e", "f"}}), "at most 5 tables")
	calls = nil
	wantError(t, call(t, cs, "list_query_tables", map[string]any{"tables": []any{"charges", " "}}), "must not be empty")
	if len(calls) != 0 {
		t.Errorf("sent %d requests before rejecting an empty name", len(calls))
	}

	wantOK(t, call(t, cs, "list_query_tables", map[string]any{"tables": []any{" charges "}}))
}

// TestListQueryTablesStripsHostNotes checks that notes naming tools this
// server lacks are dropped and the timezone note becomes utcNote.
func TestListQueryTablesStripsHostNotes(t *testing.T) {
	fixture, err := os.ReadFile("testdata/charges_schema.json")
	if err != nil {
		t.Fatal(err)
	}

	cs := connect(t, "sk_org_test_dummy", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(fixture)
	})

	got := wantOK(t, call(t, cs, "list_query_tables", map[string]any{"tables": []any{"charges"}}))
	for _, m := range hostOnlyMarkers {
		if strings.Contains(got, m) {
			t.Errorf("output mentions %s:\n%s", m, got)
		}
	}

	want := "## charges\nTo charge a credit or a debit card, you create a charge object.\n\n" +
		"SQL — Sigma uses Trino SQL.\n\n" +
		utcNote + "\n\n" +
		"REPORTING — When you report to the user, always be explicit about the start and end\n" +
		"times for time-bounded queries. Make sure the user knows when the data is starting and ending.\n\n" +
		"card_country is the primary geographic signal"
	if !strings.HasPrefix(got, want) {
		t.Errorf("text =\n%s\nwant prefix\n%s", got, want)
	}

	if !strings.Contains(got, "matched the issuer's records.\n\ncolumns:\n- merchant_id varchar -> accounts.id: Field indicating the Stripe merchant ID\n- id varchar primary key: ") {
		t.Errorf("table-specific notes or columns missing:\n%s", got)
	}
}

func TestCSVValue(t *testing.T) {
	for raw, want := range map[string]string{
		`null`:        "",
		`""`:          `""`,
		`"plain"`:     "plain",
		`" padded "`:  `" padded "`,
		`"line\nnew"`: "\"line\nnew\"",
		`42`:          "42",
		`true`:        "true",
	} {
		if got := csvValue(json.RawMessage(raw)); got != want {
			t.Errorf("csvValue(%s) = %q, want %q", raw, got, want)
		}
	}

	if got := csvValue(nil); got != "" {
		t.Errorf("missing value = %q, want empty", got)
	}
}
