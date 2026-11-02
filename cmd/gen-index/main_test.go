package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

const fixtureSpec = `{
  "info": {"version": "2026-10-28.endive"},
  "components": {"schemas": {"range": {"type": "object", "properties": {"lt": {"type": "integer"}, "gt": {"type": "integer"}}}}},
  "paths": {
    "/v1/widgets/{widget}": {
      "get": {
        "operationId": "GetWidgetsWidget",
        "summary": "Retrieve a widget",
        "parameters": [
          {"name": "widget", "in": "path", "required": true, "schema": {"type": "string"}},
          {"name": "status", "in": "query", "description": "<p>Only return widgets with this status. Defaults to all.</p>",
           "schema": {"anyOf": [{"type": "string", "enum": ["active", "inactive"]}, {"type": "string", "enum": [""]}]}},
          {"name": "created", "in": "query", "schema": {"anyOf": [{"$ref": "#/components/schemas/range"}, {"type": "integer"}]}},
          {"name": "expand", "in": "query", "schema": {"type": "array", "items": {"type": "string"}}}
        ]
      },
      "post": {"operationId": "PostWidgetsWidget", "summary": "Update a widget"}
    }
  }
}`

func TestBuildIndex(t *testing.T) {
	var s spec
	if err := json.Unmarshal([]byte(fixtureSpec), &s); err != nil {
		t.Fatal(err)
	}

	ops := buildIndex(&s)
	if len(ops) != 1 {
		t.Fatalf("got %d operations, want only the GET", len(ops))
	}

	op := ops[0]
	if op.ID != "GetWidgetsWidget" || op.Path != "/v1/widgets/{widget}" || !slices.Equal(op.PathParams, []string{"widget"}) {
		t.Errorf("got %+v", op)
	}

	want := map[string]string{"created": "object{gt,lt}|integer", "expand": "string[]", "status": "string"}
	for _, p := range op.QueryParams {
		if p.Type != want[p.Name] {
			t.Errorf("%s type = %q, want %q", p.Name, p.Type, want[p.Name])
		}
	}

	status := op.QueryParams[2]
	if status.Description != "Only return widgets with this status." {
		t.Errorf("description = %q", status.Description)
	}

	if !slices.Equal(status.Enum, []string{"active", "inactive"}) {
		t.Errorf("enum = %v", status.Enum)
	}
}

func TestShortDescriptionCapsLength(t *testing.T) {
	got := shortDescription(strings.Repeat("word ", 100))
	if len([]rune(got)) > maxDescription || !strings.HasSuffix(got, "...") {
		t.Errorf("got %d runes: %q", len([]rune(got)), got)
	}
}
