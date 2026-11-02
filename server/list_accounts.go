package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listAccountsInput struct{}

type accountOutput struct {
	Alias    string `json:"alias"`
	Name     string `json:"name"`
	Account  string `json:"account"`
	Livemode bool   `json:"livemode"`
}

type listAccountsOutput struct {
	Accounts []accountOutput `json:"accounts"`
}

func listAccounts(_ context.Context, env *Env, _ listAccountsInput) (*mcp.CallToolResult, listAccountsOutput, error) {
	out := listAccountsOutput{Accounts: make([]accountOutput, len(env.Accounts))}
	for i, a := range env.Accounts {
		out.Accounts[i] = accountOutput{Alias: a.Alias, Name: a.Name, Account: a.Account, Livemode: env.Livemode}
	}

	return nil, out, nil
}
