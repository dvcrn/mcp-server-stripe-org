package server

import (
	"slices"
	"testing"
)

func TestAPIReadBlocksSensitiveFields(t *testing.T) {
	cs := connect(t, "sk_org_test_dummy", noStripe(t))

	card := map[string]any{"card": "ic_dummy"}
	session := map[string]any{"session": "vs_dummy"}
	report := map[string]any{"report": "vr_dummy"}
	financialAccount := map[string]any{"financial_account": "fa_dummy"}

	for _, tc := range []struct {
		name       string
		op         string
		pathParams map[string]any
		params     map[string]any
		want       string
	}{
		{"card number", "GetIssuingCardsCard", card, map[string]any{"expand": []any{"number"}}, `expand "number" is blocked`},
		{"card cvc", "GetIssuingCardsCard", card, map[string]any{"expand": []any{"cvc"}}, `expand "cvc" is blocked`},
		{"allowed then blocked", "GetIssuingCardsCard", card, map[string]any{"expand": []any{"cardholder", "cvc"}}, `expand "cvc" is blocked`},
		{"card number on list", "GetIssuingCards", nil, map[string]any{"expand": []any{"data.number"}}, `expand "data.number" is blocked`},
		{"uppercase", "GetIssuingCardsCard", card, map[string]any{"expand": []any{"Number"}}, `"number" returns`},
		{"verified dob", "GetIdentityVerificationSessionsSession", session, map[string]any{"expand": []any{"verified_outputs.dob"}}, `"verified_outputs" returns`},
		{"verified id number", "GetIdentityVerificationSessionsSession", session, map[string]any{"expand": []any{"verified_outputs.id_number"}}, `"verified_outputs" returns`},
		{"verified outputs on list", "GetIdentityVerificationSessions", nil, map[string]any{"expand": []any{"data.verified_outputs.dob"}}, `"verified_outputs" returns`},
		{"report document number", "GetIdentityVerificationReportsReport", report, map[string]any{"expand": []any{"document.number"}}, `"number" returns`},
		{"report document dob", "GetIdentityVerificationReportsReport", report, map[string]any{"expand": []any{"document.dob"}}, `"dob" returns`},
		{"report document expiry", "GetIdentityVerificationReportsReport", report, map[string]any{"expand": []any{"document.expiration_date"}}, `"expiration_date" returns`},
		{"report id number", "GetIdentityVerificationReportsReport", report, map[string]any{"expand": []any{"id_number.id_number"}}, `"id_number" returns`},
		{"treasury account number", "GetTreasuryFinancialAccountsFinancialAccount", financialAccount, map[string]any{"expand": []any{"financial_addresses.aba.account_number"}}, `"account_number" returns`},
		{"bracket syntax", "GetIssuingCards", nil, map[string]any{"expand": []any{"data[number]"}}, "dot-separated field names"},
		{"surrounding space", "GetIssuingCardsCard", card, map[string]any{"expand": []any{" number"}}, "dot-separated field names"},
		{"comma list", "GetIssuingCardsCard", card, map[string]any{"expand": []any{"cardholder,number"}}, "dot-separated field names"},
		{"empty segment", "GetIssuingCards", nil, map[string]any{"expand": []any{"data..number"}}, "dot-separated field names"},
		{"string instead of list", "GetIssuingCardsCard", card, map[string]any{"expand": "number"}, "must be a list"},
		{"object instead of list", "GetIssuingCardsCard", card, map[string]any{"expand": map[string]any{"0": "number"}}, "must be a list"},
		{"nested list", "GetIssuingCardsCard", card, map[string]any{"expand": []any{[]any{"number"}}}, "dot-separated field names"},
		{"v2 include goes through the same check", "GetV2CoreAccountsId", map[string]any{"id": "acct_dummy"}, map[string]any{"include": []any{"identity.individual.dob"}}, `include "identity.individual.dob" is blocked`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"operation_id": tc.op, "account": "dummy-one", "params": tc.params}
			if tc.pathParams != nil {
				args["path_params"] = tc.pathParams
			}

			wantError(t, call(t, cs, "api_read", args), tc.want)
		})
	}
}

func TestAPIReadAllowsOrdinaryExpands(t *testing.T) {
	var calls []stripeCall
	cs := connect(t, "sk_org_test_dummy", recordStripe(&calls, `{"object":"list","data":[]}`))

	wantOK(t, call(t, cs, "api_read", map[string]any{
		"operation_id": "GetSubscriptions",
		"account":      "dummy-one",
		"params":       map[string]any{"expand": []any{"data.customer", "data.latest_invoice"}, "limit": 1},
	}))
	wantOK(t, call(t, cs, "api_read", map[string]any{
		"operation_id": "GetIssuingCardsCard",
		"account":      "dummy-one",
		"path_params":  map[string]any{"card": "ic_dummy"},
		"params":       map[string]any{"expand": []any{"cardholder"}},
	}))
	wantOK(t, call(t, cs, "api_read", map[string]any{
		"operation_id": "GetV2CoreAccountsId",
		"path_params":  map[string]any{"id": "acct_dummy"},
		"params":       map[string]any{"include": []any{"configuration.customer"}},
	}))

	want := []stripeCall{
		{"/v1/subscriptions", "expand%5B0%5D=data.customer&expand%5B1%5D=data.latest_invoice&limit=1", "acct_1DUMMYONE"},
		{"/v1/issuing/cards/ic_dummy", "expand%5B0%5D=cardholder", "acct_1DUMMYONE"},
		{"/v2/core/accounts/acct_dummy", "include%5B0%5D=configuration.customer", ""},
	}
	if !slices.Equal(calls, want) {
		t.Errorf("calls = %+v, want %+v", calls, want)
	}
}
