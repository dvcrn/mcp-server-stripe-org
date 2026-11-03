package stripe

import (
	"slices"
	"strings"
	"testing"
)

func TestLookupOperation(t *testing.T) {
	op, ok := LookupOperation("GetCustomersCustomer")
	if !ok {
		t.Fatal("GetCustomersCustomer not in index")
	}

	if op.Path != "/v1/customers/{customer}" || op.Summary != "Retrieve a customer" {
		t.Errorf("got %+v", op)
	}

	if !slices.Equal(op.PathParams, []string{"customer"}) {
		t.Errorf("path params = %v", op.PathParams)
	}

	if _, ok := LookupOperation("PostCustomers"); ok {
		t.Error("index contains a non-GET operation")
	}

	if _, ok := LookupOperation("NoSuchOperation"); ok {
		t.Error("unknown ID found")
	}
}

func TestIndexExcludesAppsSecrets(t *testing.T) {
	loadIndex()

	for _, op := range index.ops {
		if strings.HasPrefix(op.Path, "/v1/apps/secrets") {
			t.Errorf("index contains %s (%s)", op.ID, op.Path)
		}
	}

	for _, id := range []string{"GetAppsSecrets", "GetAppsSecretsFind"} {
		if _, ok := LookupOperation(id); ok {
			t.Errorf("LookupOperation(%q) found it", id)
		}
	}
}

func TestIndexQueryParams(t *testing.T) {
	op, ok := LookupOperation("GetSubscriptions")
	if !ok {
		t.Fatal("GetSubscriptions not in index")
	}

	params := map[string]Param{}
	for _, p := range op.QueryParams {
		params[p.Name] = p
	}

	if p := params["limit"]; p.Type != "integer" || p.Description == "" {
		t.Errorf("limit = %+v", p)
	}

	if p := params["created"]; p.Type != "object{gt,gte,lt,lte}|integer" {
		t.Errorf("created type = %q", p.Type)
	}

	if p := params["expand"]; p.Type != "string[]" {
		t.Errorf("expand type = %q", p.Type)
	}

	if p := params["status"]; !slices.Contains(p.Enum, "active") || !slices.Contains(p.Enum, "canceled") {
		t.Errorf("status enum = %v", p.Enum)
	}
}

func TestIndexMatchesAPIVersion(t *testing.T) {
	loadIndex()

	if index.apiVersion != APIVersion {
		t.Errorf("index.json.gz is for %s, client sends %s; run mise run gen-index", index.apiVersion, APIVersion)
	}
}

func TestIndexIsCompact(t *testing.T) {
	loadIndex()

	if len(index.ops) < 250 {
		t.Fatalf("only %d operations in index", len(index.ops))
	}

	for _, op := range index.ops {
		if !strings.HasPrefix(op.ID, "Get") {
			t.Errorf("non-GET operation %s", op.ID)
		}

		for _, p := range op.QueryParams {
			if len(p.Description) > 160 || strings.ContainsAny(p.Description, "<>") {
				t.Errorf("%s.%s description not compacted: %q", op.ID, p.Name, p.Description)
			}
		}
	}
}

func TestSearchOperations(t *testing.T) {
	tests := []struct {
		query string
		first string
	}{
		{"balance", "GetBalance"},
		{"retrieve customer", "GetCustomersCustomer"},
		{"LIST SUBSCRIPTIONS", "GetSubscriptions"},
		{"sigma query runs", "GetSigmaScheduledQueryRuns"},
		{"balance nonexistentterm", ""},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := SearchOperations(tt.query, 5)
			if tt.first == "" {
				if len(got) != 0 {
					t.Errorf("got %d results, want none", len(got))
				}

				return
			}

			if len(got) == 0 || got[0].ID != tt.first {
				t.Errorf("first result = %v, want %s", ids(got), tt.first)
			}
		})
	}
}

func TestSearchOperationsLimits(t *testing.T) {
	if got := SearchOperations("customer", 3); len(got) != 3 {
		t.Errorf("limit 3 returned %d", len(got))
	}

	if got := SearchOperations("  ", 10); got != nil {
		t.Errorf("blank query returned %v", ids(got))
	}

	if got := SearchOperations("customer", 0); got != nil {
		t.Errorf("limit 0 returned %v", ids(got))
	}

	for _, op := range SearchOperations("invoice line", 20) {
		hay := strings.ToLower(op.ID + op.Path + op.Summary)
		if !strings.Contains(hay, "invoice") || !strings.Contains(hay, "line") {
			t.Errorf("%s does not match every term", op.ID)
		}
	}
}

func ids(ops []Operation) []string {
	out := make([]string, len(ops))
	for i, op := range ops {
		out[i] = op.ID
	}

	return out
}

func TestBuildPath(t *testing.T) {
	op, _ := LookupOperation("GetV2CoreAccountsAccountIdPersonsId")

	got, err := op.BuildPath(map[string]string{"account_id": "acct_DUMMY", "id": "person_dummy-1"})
	if err != nil || got != "/v2/core/accounts/acct_DUMMY/persons/person_dummy-1" {
		t.Errorf("got %q, %v", got, err)
	}

	plan, _ := LookupOperation("GetPlansPlan")
	if got, err := plan.BuildPath(map[string]string{"plan": "gold.monthly"}); err != nil || got != "/v1/plans/gold.monthly" {
		t.Errorf("GetPlansPlan: got %q, %v", got, err)
	}

	for _, params := range []map[string]string{
		nil,
		{"account_id": "acct_DUMMY"},
		{"account_id": "acct_DUMMY", "id": ""},
		{"account_id": "acct_DUMMY", "id": "."},
		{"account_id": "acct_DUMMY", "id": ".."},
		{"account_id": "acct_DUMMY", "id": "a/b"},
		{"account_id": "acct_DUMMY", "id": "a%2Fb"},
		{"account_id": "acct_DUMMY", "id": "a?b"},
		{"account_id": "acct_DUMMY", "id": "a b"},
		{"account_id": "acct_DUMMY", "id": "x", "extra": "y"},
	} {
		if got, err := op.BuildPath(params); err == nil {
			t.Errorf("%v: accepted as %q", params, got)
		}
	}

	balance, _ := LookupOperation("GetBalance")
	if got, err := balance.BuildPath(nil); err != nil || got != "/v1/balance" {
		t.Errorf("GetBalance: got %q, %v", got, err)
	}
}

// Every placeholder must be a declared path parameter, or BuildPath would
// send it literally.
func TestIndexPathTemplatesMatchParams(t *testing.T) {
	loadIndex()

	for _, op := range index.ops {
		path := op.Path
		for _, name := range op.PathParams {
			path = strings.Replace(path, "{"+name+"}", "x", 1)
		}

		if strings.ContainsAny(path, "{}") {
			t.Errorf("%s: %s has undeclared placeholders", op.ID, op.Path)
		}
	}
}

func TestIsV2(t *testing.T) {
	v1, _ := LookupOperation("GetBalance")
	v2, _ := LookupOperation("GetV2CoreAccounts")

	if v1.IsV2() || !v2.IsV2() {
		t.Errorf("IsV2: GetBalance %v, GetV2CoreAccounts %v", v1.IsV2(), v2.IsV2())
	}
}
