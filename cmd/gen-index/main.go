// Command gen-index writes the compact GET-operation index embedded by the
// stripe package from a Stripe OpenAPI spec.
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dvcrn/mcp-server-stripe-org/stripe"
)

// specURL is pinned to the stripe/openapi commit for stripe.APIVersion.
const specURL = "https://raw.githubusercontent.com/stripe/openapi/51610f5f061c9723730582a7638b8cc0e646640d/latest/openapi.spec3.sdk.json"

const maxDescription = 160

// deniedPathPrefixes keeps GET endpoints that return secret values out of the
// index, so api_read cannot call them whatever the key's permissions are.
var deniedPathPrefixes = []string{
	"/v1/apps/secrets", // Stripe Apps secrets; expand=payload returns the secret value
}

type spec struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	Paths      map[string]map[string]specOperation `json:"paths"`
	Components struct {
		Schemas map[string]*schema `json:"schemas"`
	} `json:"components"`
}

type specOperation struct {
	OperationID string      `json:"operationId"`
	Summary     string      `json:"summary"`
	Parameters  []specParam `json:"parameters"`
}

type specParam struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Description string  `json:"description"`
	Required    bool    `json:"required"`
	Schema      *schema `json:"schema"`
}

type schema struct {
	Ref        string             `json:"$ref"`
	Type       string             `json:"type"`
	Items      *schema            `json:"items"`
	Properties map[string]*schema `json:"properties"`
	AnyOf      []*schema          `json:"anyOf"`
	OneOf      []*schema          `json:"oneOf"`
	Enum       []any              `json:"enum"`
}

func main() {
	src := flag.String("spec", specURL, "OpenAPI spec file path or URL")
	out := flag.String("out", "stripe/index.json.gz", "output file")
	flag.Parse()

	if err := run(*src, *out); err != nil {
		fmt.Fprintln(os.Stderr, "gen-index:", err)
		os.Exit(1)
	}
}

func run(src, out string) error {
	raw, err := readSpec(src)
	if err != nil {
		return err
	}

	var s spec
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("parse spec: %w", err)
	}

	if s.Info.Version != stripe.APIVersion {
		return fmt.Errorf("spec version %s does not match stripe.APIVersion %s", s.Info.Version, stripe.APIVersion)
	}

	ops := buildIndex(&s)
	idx := stripe.Index{APIVersion: s.Info.Version, Operations: ops}

	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return err
	}

	if err := json.NewEncoder(zw).Encode(idx); err != nil {
		return err
	}

	if err := zw.Close(); err != nil {
		return err
	}

	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "wrote %d operations to %s (%d bytes)\n", len(ops), out, buf.Len())
	return nil
}

func readSpec(src string) ([]byte, error) {
	if !strings.HasPrefix(src, "https://") && !strings.HasPrefix(src, "http://") {
		return os.ReadFile(src)
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(src)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", src, resp.Status)
	}

	return io.ReadAll(resp.Body)
}

func buildIndex(s *spec) []stripe.Operation {
	var ops []stripe.Operation
	for path, methods := range s.Paths {
		op, ok := methods["get"]
		if !ok || denied(path) {
			continue
		}

		entry := stripe.Operation{ID: op.OperationID, Path: path, Summary: op.Summary}
		for _, p := range op.Parameters {
			switch p.In {
			case "path":
				entry.PathParams = append(entry.PathParams, p.Name)
			case "query":
				entry.QueryParams = append(entry.QueryParams, stripe.Param{
					Name:        p.Name,
					Type:        typeName(s, p.Schema),
					Description: shortDescription(p.Description),
					Required:    p.Required,
					Enum:        enumValues(s, p.Schema),
				})
			}
		}

		sort.Slice(entry.QueryParams, func(i, j int) bool { return entry.QueryParams[i].Name < entry.QueryParams[j].Name })
		ops = append(ops, entry)
	}

	sort.Slice(ops, func(i, j int) bool { return ops[i].ID < ops[j].ID })
	return ops
}

func denied(path string) bool {
	for _, prefix := range deniedPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}

func resolve(s *spec, sc *schema) *schema {
	for sc != nil && sc.Ref != "" {
		sc = s.Components.Schemas[strings.TrimPrefix(sc.Ref, "#/components/schemas/")]
	}

	return sc
}

func typeName(s *spec, sc *schema) string {
	sc = resolve(s, sc)
	if sc == nil {
		return "any"
	}

	if variants := variants(sc); len(variants) > 0 {
		var names []string
		for _, v := range variants {
			if name := typeName(s, v); !contains(names, name) {
				names = append(names, name)
			}
		}

		return strings.Join(names, "|")
	}

	switch sc.Type {
	case "array":
		return typeName(s, sc.Items) + "[]"
	case "object":
		if len(sc.Properties) == 0 {
			return "object"
		}

		keys := make([]string, 0, len(sc.Properties))
		for k := range sc.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		return "object{" + strings.Join(keys, ",") + "}"
	case "":
		return "any"
	default:
		return sc.Type
	}
}

func enumValues(s *spec, sc *schema) []string {
	sc = resolve(s, sc)
	if sc == nil {
		return nil
	}

	var out []string
	for _, v := range sc.Enum {
		if str, ok := v.(string); ok && str != "" && !contains(out, str) {
			out = append(out, str)
		}
	}

	for _, variant := range variants(sc) {
		for _, str := range enumValues(s, variant) {
			if !contains(out, str) {
				out = append(out, str)
			}
		}
	}

	return out
}

var (
	tagPattern   = regexp.MustCompile(`<[^>]*>`)
	spacePattern = regexp.MustCompile(`\s+`)
)

// shortDescription returns the first sentence of an HTML description as plain
// text, truncated to maxDescription runes.
func shortDescription(desc string) string {
	text := html.UnescapeString(tagPattern.ReplaceAllString(desc, " "))
	text = strings.TrimSpace(spacePattern.ReplaceAllString(text, " "))

	if i := strings.Index(text, ". "); i >= 0 {
		text = text[:i+1]
	}

	if runes := []rune(text); len(runes) > maxDescription {
		text = strings.TrimSpace(string(runes[:maxDescription-3])) + "..."
	}

	return text
}

func variants(sc *schema) []*schema {
	return append(append([]*schema(nil), sc.AnyOf...), sc.OneOf...)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}

	return false
}
