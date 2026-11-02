package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestNewEnvErrors(t *testing.T) {
	for _, tc := range []struct {
		name, key, accounts, want string
	}{
		{"no key", "", testAccounts, "STRIPE_ORG_KEY is not set"},
		{"account key", "sk_test_dummy", testAccounts, "organization key"},
		{"no accounts", "sk_org_test_dummy", "", "STRIPE_ACCOUNTS is not set"},
		{"empty list", "sk_org_test_dummy", "[]", "no accounts"},
		{"not json", "sk_org_test_dummy", "dummy-one=acct_1", "JSON array"},
		{"bad id", "sk_org_test_dummy", `[{"alias":"a","account":"cus_1"}]`, "not an acct_ ID"},
		{"bad alias", "sk_org_test_dummy", `[{"alias":"A B","account":"acct_1"}]`, "alias"},
		{"duplicate alias", "sk_org_test_dummy", `[{"alias":"a","account":"acct_1"},{"alias":"a","account":"acct_2"}]`, "twice"},
		{"duplicate id", "sk_org_test_dummy", `[{"alias":"a","account":"acct_1"},{"alias":"b","account":"acct_1"}]`, "twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewEnv(tc.key, tc.accounts, http.DefaultClient)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestKeyLivemode(t *testing.T) {
	for key, want := range map[string]bool{
		"sk_org_live_dummy": true,
		"rk_org_live_dummy": true,
		"sk_org_test_dummy": false,
		"rk_org_test_dummy": false,
	} {
		got, err := keyLivemode(key)
		if err != nil || got != want {
			t.Errorf("%s: got %v, %v; want %v", key, got, err, want)
		}
	}
}
