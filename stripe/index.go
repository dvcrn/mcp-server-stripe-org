package stripe

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Operation is a GET endpoint from the Stripe OpenAPI spec.
type Operation struct {
	ID          string   `json:"id"`
	Path        string   `json:"path"`
	Summary     string   `json:"summary,omitempty"`
	PathParams  []string `json:"path_params,omitempty"`
	QueryParams []Param  `json:"query,omitempty"`
}

// Param is a query parameter an operation declares.
type Param struct {
	Name string `json:"name"`
	// Type is compact, for example "string", "string[]",
	// "object{gt,gte,lt,lte}|integer".
	Type        string   `json:"type"`
	Description string   `json:"desc,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// Index is the decoded form of index.json.gz.
type Index struct {
	APIVersion string      `json:"api_version"`
	Operations []Operation `json:"operations"`
}

//go:embed index.json.gz
var indexGz []byte

var index struct {
	once       sync.Once
	apiVersion string
	ops        []Operation
	byID       map[string]*Operation
}

func loadIndex() {
	index.once.Do(func() {
		idx, err := decodeIndex(indexGz)
		if err != nil {
			panic(fmt.Sprintf("stripe: embedded index.json.gz is invalid: %v", err))
		}

		ops := idx.Operations
		index.apiVersion = idx.APIVersion
		index.ops = ops
		index.byID = make(map[string]*Operation, len(ops))
		for i := range ops {
			index.byID[ops[i].ID] = &ops[i]
		}
	})
}

func decodeIndex(data []byte) (Index, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return Index{}, err
	}
	defer zr.Close()

	var idx Index
	if err := json.NewDecoder(zr).Decode(&idx); err != nil {
		return Index{}, err
	}

	return idx, nil
}

// LookupOperation returns the GET operation with the given OpenAPI operation ID.
// The returned slices are shared with the index and must not be modified.
func LookupOperation(id string) (Operation, bool) {
	loadIndex()

	op, ok := index.byID[id]
	if !ok {
		return Operation{}, false
	}

	return *op, true
}

// SearchOperations returns up to limit operations whose ID, path or summary
// contain every whitespace-separated term of query, best matches first.
// The returned slices are shared with the index and must not be modified.
func SearchOperations(query string, limit int) []Operation {
	loadIndex()

	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 || limit <= 0 {
		return nil
	}

	type match struct {
		op    *Operation
		score int
	}

	var matches []match
	for i := range index.ops {
		if score := scoreOperation(&index.ops[i], terms); score > 0 {
			matches = append(matches, match{&index.ops[i], score})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}

		return matches[i].op.ID < matches[j].op.ID
	})

	if len(matches) > limit {
		matches = matches[:limit]
	}

	out := make([]Operation, len(matches))
	for i, m := range matches {
		out[i] = *m.op
	}

	return out
}

// scoreOperation weights ID matches over summary over path, and returns 0
// unless every term matches some field.
func scoreOperation(op *Operation, terms []string) int {
	id := strings.ToLower(op.ID)
	path := strings.ToLower(op.Path)
	summary := strings.ToLower(op.Summary)

	total := 0
	for _, term := range terms {
		score := 0
		if strings.Contains(id, term) {
			score += 3
		}

		if strings.Contains(summary, term) {
			score += 2
		}

		if strings.Contains(path, term) {
			score++
		}

		if score == 0 {
			return 0
		}

		total += score
	}

	return total
}

var pathParamPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// IsV2 reports whether the operation is a /v2 endpoint, which DoV2 serves.
func (op Operation) IsV2() bool {
	return strings.HasPrefix(op.Path, "/v2/")
}

// BuildPath fills the operation's path template with params. Every declared
// parameter must be present and match ^[A-Za-z0-9_-]+$, and no undeclared
// parameter is accepted, so a value can never add or change path segments.
func (op Operation) BuildPath(params map[string]string) (string, error) {
	for name := range params {
		if !slices.Contains(op.PathParams, name) {
			return "", fmt.Errorf("%s has no path parameter %q; it takes %s", op.ID, name, listOrNone(op.PathParams))
		}
	}

	path := op.Path
	for _, name := range op.PathParams {
		value := params[name]
		if value == "" {
			return "", fmt.Errorf("%s needs path parameter %q", op.ID, name)
		}

		if !pathParamPattern.MatchString(value) {
			return "", fmt.Errorf("path parameter %q must match ^[A-Za-z0-9_-]+$, got %q", name, value)
		}

		path = strings.Replace(path, "{"+name+"}", url.PathEscape(value), 1)
	}

	return path, nil
}

func listOrNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}

	return strings.Join(names, ", ")
}
