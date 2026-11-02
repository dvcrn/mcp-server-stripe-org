package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvcrn/mcp-server-stripe-org/stripe"
)

const (
	schemasPath = "/v2/data/schemas"

	// maxSchemaPages caps the listing at 500 tables.
	maxSchemaPages = 5
	maxTables      = 5
)

// hostOnlyMarkers mark paragraphs of Stripe's table notes written for its own
// assistant, which has tools and context tags this server lacks.
var hostOnlyMarkers = []string{"query_sigma", "get_sql_template", "available_templates", "<message_datetime>"}

// utcNote replaces Stripe's timezone paragraph, whose reporting timezone
// comes from a <message_datetime> tag this server never sends.
const utcNote = "TIMEZONE — every timestamp column is UTC unless the column states otherwise."

var paragraphBreak = regexp.MustCompile(`\n[ \t]*\n`)

type listQueryTablesInput struct {
	Tables []string `json:"tables,omitempty" jsonschema:"up to 5 table names to show columns for, e.g. [\"charges\", \"customers\"]; omit to list every table"`
}

type querySchema struct {
	Name                string         `json:"name"`
	Description         string         `json:"description"`
	ExtendedDescription string         `json:"extended_description"`
	Columns             []schemaColumn `json:"columns"`
}

type schemaColumn struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Description   string `json:"description"`
	IsPrimaryKey  bool   `json:"is_primary_key"`
	ForeignKeysTo []struct {
		Column     string `json:"column"`
		SchemaName string `json:"schema_name"`
	} `json:"foreign_keys_to"`
}

type schemaList struct {
	Data        []querySchema `json:"data"`
	NextPageURL string        `json:"next_page_url"`
}

func listQueryTables(ctx context.Context, env *Env, in listQueryTablesInput) (*mcp.CallToolResult, any, error) {
	var text string
	var err error
	if len(in.Tables) == 0 {
		text, err = allTables(ctx, env)
	} else {
		text, err = tableColumns(ctx, env, in.Tables)
	}

	if err != nil {
		return nil, nil, err
	}

	return textResult(text), nil, nil
}

func fetchSchemas(ctx context.Context, env *Env, query map[string]any) (*schemaList, error) {
	body, err := env.Client.DoV2(ctx, stripe.V2Request{
		Method:  http.MethodGet,
		Path:    schemasPath,
		Version: stripe.PreviewAPIVersion,
		Query:   query,
	})
	if err != nil {
		return nil, err
	}

	var list schemaList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("decode schemas: %w", err)
	}

	return &list, nil
}

// allTables lists every table's name and one-line description.
func allTables(ctx context.Context, env *Env) (string, error) {
	var lines []string
	page := ""
	for range maxSchemaPages {
		query := map[string]any{"limit": 100}
		if page != "" {
			query["page"] = page
		}

		list, err := fetchSchemas(ctx, env, query)
		if err != nil {
			return "", err
		}

		for _, s := range list.Data {
			lines = append(lines, s.Name+": "+oneLine(s.Description))
		}

		if page = pageToken(list.NextPageURL); page == "" {
			break
		}
	}

	header := fmt.Sprintf("%d tables. Call list_query_tables with tables for their columns.\n", len(lines))
	if page != "" {
		header = fmt.Sprintf("First %d tables; Stripe has more. Call list_query_tables with tables for their columns.\n", len(lines))
	}

	return header + "\n" + strings.Join(lines, "\n") + "\n", nil
}

// tableColumns describes each named table with its columns.
func tableColumns(ctx context.Context, env *Env, tables []string) (string, error) {
	names := make([]string, len(tables))
	for i, name := range tables {
		if names[i] = strings.TrimSpace(name); names[i] == "" {
			return "", errors.New("table names must not be empty")
		}
	}

	tables = slices.Compact(slices.Sorted(slices.Values(names)))
	if len(tables) > maxTables {
		return "", fmt.Errorf("at most %d tables per call, got %d", maxTables, len(tables))
	}

	var b strings.Builder
	var missing []string
	for _, name := range tables {
		list, err := fetchSchemas(ctx, env, map[string]any{"name": name, "include": []string{"columns"}})
		if err != nil {
			return "", err
		}

		if len(list.Data) == 0 {
			missing = append(missing, name)
			continue
		}

		writeTable(&b, list.Data[0])
	}

	if len(missing) > 0 {
		return "", fmt.Errorf("unknown tables %s; call list_query_tables without tables to see every name", strings.Join(missing, ", "))
	}

	return b.String(), nil
}

func writeTable(b *strings.Builder, s querySchema) {
	fmt.Fprintf(b, "## %s\n%s\n", s.Name, oneLine(s.Description))
	if notes := tableNotes(s.ExtendedDescription); notes != "" {
		fmt.Fprintf(b, "\n%s\n", notes)
	}

	b.WriteString("\ncolumns:\n")
	for _, c := range s.Columns {
		fmt.Fprintf(b, "- %s %s", c.Name, c.Type)
		if c.IsPrimaryKey {
			b.WriteString(" primary key")
		}

		for _, fk := range c.ForeignKeysTo {
			fmt.Fprintf(b, " -> %s.%s", fk.SchemaName, fk.Column)
		}

		if c.Description != "" {
			b.WriteString(": " + oneLine(c.Description))
		}

		b.WriteString("\n")
	}

	b.WriteString("\n")
}

// tableNotes returns the paragraphs of a table's extended description that
// apply to this server, with the timezone paragraph replaced by utcNote.
func tableNotes(s string) string {
	var kept []string
	for _, p := range paragraphBreak.Split(s, -1) {
		p = strings.TrimSpace(p)
		switch {
		case p == "":
		case strings.Contains(p, "<message_datetime>"):
			kept = append(kept, utcNote)
		case slices.ContainsFunc(hostOnlyMarkers, func(m string) bool { return strings.Contains(p, m) }):
		default:
			kept = append(kept, p)
		}
	}

	return strings.Join(kept, "\n\n")
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
