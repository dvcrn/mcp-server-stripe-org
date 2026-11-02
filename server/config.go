package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/dvcrn/mcp-server-stripe-org/stripe"
)

// Environment variables the server reads.
const (
	EnvOrgKey   = "STRIPE_ORG_KEY"
	EnvToken    = "MCP_TOKEN"
	EnvAccounts = "STRIPE_ACCOUNTS"
)

// Account is one entry of STRIPE_ACCOUNTS, the accounts tools may act on.
type Account struct {
	Alias   string `json:"alias"`
	Name    string `json:"name"`
	Account string `json:"account"`
}

// Env is the per-request configuration tools read from the context.
type Env struct {
	Client   *stripe.Client
	Accounts []Account
	// Livemode is true for a live org key and false for a sandbox one.
	Livemode bool
}

var (
	accountIDPattern = regexp.MustCompile(`^acct_[A-Za-z0-9]+$`)
	aliasPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
)

// NewEnv validates the values of STRIPE_ORG_KEY and STRIPE_ACCOUNTS.
func NewEnv(key, accountsJSON string, httpClient *http.Client, opts ...stripe.Option) (*Env, error) {
	livemode, err := keyLivemode(key)
	if err != nil {
		return nil, err
	}

	accounts, err := parseAccounts(accountsJSON)
	if err != nil {
		return nil, err
	}

	return &Env{Client: stripe.New(httpClient, key, opts...), Accounts: accounts, Livemode: livemode}, nil
}

// keyLivemode accepts only organization keys, since a plain account key
// would ignore Stripe-Context and act on its own account.
func keyLivemode(key string) (bool, error) {
	for _, prefix := range []string{"sk_org_", "rk_org_"} {
		mode, ok := strings.CutPrefix(key, prefix)
		switch {
		case !ok:
			continue
		case strings.HasPrefix(mode, "live_"):
			return true, nil
		case strings.HasPrefix(mode, "test_"):
			return false, nil
		}
	}

	if key == "" {
		return false, fmt.Errorf("%s is not set", EnvOrgKey)
	}

	return false, fmt.Errorf("%s must be an organization key (sk_org_live_, sk_org_test_ or the rk_org_ equivalents)", EnvOrgKey)
}

func parseAccounts(raw string) ([]Account, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%s is not set", EnvAccounts)
	}

	var accounts []Account
	if err := json.Unmarshal([]byte(raw), &accounts); err != nil {
		return nil, fmt.Errorf("%s must be a JSON array of {alias, name, account}: %w", EnvAccounts, err)
	}

	if len(accounts) == 0 {
		return nil, fmt.Errorf("%s has no accounts", EnvAccounts)
	}

	seen := map[string]bool{}
	for _, a := range accounts {
		if !aliasPattern.MatchString(a.Alias) {
			return nil, fmt.Errorf("%s: alias %q must be lowercase letters, digits, - or _", EnvAccounts, a.Alias)
		}

		if !accountIDPattern.MatchString(a.Account) {
			return nil, fmt.Errorf("%s: account %q for alias %q is not an acct_ ID", EnvAccounts, a.Account, a.Alias)
		}

		if seen[a.Alias] || seen[a.Account] {
			return nil, fmt.Errorf("%s lists %q or %q twice", EnvAccounts, a.Alias, a.Account)
		}

		seen[a.Alias], seen[a.Account] = true, true
	}

	return accounts, nil
}

// resolveAccount returns the acct_ ID for an alias or an allowlisted acct_ ID.
func (e *Env) resolveAccount(ref string) (string, error) {
	for _, a := range e.Accounts {
		if ref == a.Alias || ref == a.Account {
			return a.Account, nil
		}
	}

	aliases := make([]string, len(e.Accounts))
	for i, a := range e.Accounts {
		aliases[i] = a.Alias
	}

	if ref == "" {
		return "", fmt.Errorf("account is required; use one of: %s", strings.Join(aliases, ", "))
	}

	return "", fmt.Errorf("unknown account %q; use one of: %s (see list_accounts)", ref, strings.Join(aliases, ", "))
}

type envKey struct{}

// WithEnv returns a context whose tool calls use env.
func WithEnv(ctx context.Context, env *Env) context.Context {
	return context.WithValue(ctx, envKey{}, env)
}

var errNoEnv = errors.New("server is not configured")

func envFrom(ctx context.Context) (*Env, error) {
	env, ok := ctx.Value(envKey{}).(*Env)
	if !ok || env == nil {
		return nil, errNoEnv
	}

	return env, nil
}
