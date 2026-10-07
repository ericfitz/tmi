package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// diagramSchemaScope reports whether a component schema belongs to the diagram family (#956).
// SEM@0000000: decide whether a component schema is a diagram or cell schema (pure)
func diagramSchemaScope(name string) bool {
	return name == "Node" || name == "Edge" || strings.Contains(name, "Diagram") || strings.Contains(name, "Cell")
}

// SEM@0000000: resolve a local component $ref to its schema name (pure)
func refName(ref string) string {
	return strings.TrimPrefix(ref, "#/components/schemas/")
}

// SEM@0000000: collect property names a schema declares, following allOf refs (pure)
func declaredProps(schemas map[string]map[string]any, name string, seen map[string]bool) map[string]bool {
	out := map[string]bool{}
	if seen[name] {
		return out
	}
	seen[name] = true
	s := schemas[name]
	if p, ok := s["properties"].(map[string]any); ok {
		for k := range p {
			out[k] = true
		}
	}
	if all, ok := s["allOf"].([]any); ok {
		for _, e := range all {
			em, _ := e.(map[string]any)
			if r, ok := em["$ref"].(string); ok {
				for k := range declaredProps(schemas, refName(r), seen) {
					out[k] = true
				}
			} else if p, ok := em["properties"].(map[string]any); ok {
				for k := range p {
					out[k] = true
				}
			}
		}
	}
	return out
}

// SEM@0000000: check diagram schemas for redeclared allOf props and misplaced discriminators (test)
func TestDiagramSchemaStructure(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "api-schema", "tmi-openapi.json"))
	require.NoError(t, err)
	var doc struct {
		Components struct {
			Schemas map[string]map[string]any `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	schemas := doc.Components.Schemas
	var problems []string

	for name, s := range schemas {
		if !diagramSchemaScope(name) {
			continue
		}
		all, _ := s["allOf"].([]any)
		base := map[string]bool{}
		var bases []string
		for _, e := range all {
			em, _ := e.(map[string]any)
			if r, ok := em["$ref"].(string); ok {
				bases = append(bases, refName(r))
				for k := range declaredProps(schemas, refName(r), map[string]bool{}) {
					base[k] = true
				}
			}
		}
		for _, e := range all {
			em, _ := e.(map[string]any)
			if _, isRef := em["$ref"]; isRef {
				continue
			}
			p, _ := em["properties"].(map[string]any)
			for k := range p {
				if base[k] {
					problems = append(problems, fmt.Sprintf("%s: allOf child redeclares %q from %v", name, k, bases))
				}
			}
		}
	}

	// Discriminators: anywhere under a diagram schema, only next to oneOf/anyOf,
	// and no mapping target may allOf the schema that hosts the discriminator.
	var walk func(host, path string, node any)
	walk = func(host, path string, node any) {
		switch v := node.(type) {
		case map[string]any:
			if d, ok := v["discriminator"].(map[string]any); ok {
				_, hasOneOf := v["oneOf"]
				_, hasAnyOf := v["anyOf"]
				if !hasOneOf && !hasAnyOf {
					problems = append(problems, fmt.Sprintf("%s: discriminator without oneOf/anyOf", path))
				}
				if m, ok := d["mapping"].(map[string]any); ok {
					for _, target := range m {
						ts, _ := target.(string)
						tAll, _ := schemas[refName(ts)]["allOf"].([]any)
						for _, e := range tAll {
							em, _ := e.(map[string]any)
							if r, _ := em["$ref"].(string); refName(r) == host {
								problems = append(problems, fmt.Sprintf("%s: mapping target %s is a subtype of %s", path, refName(ts), host))
							}
						}
					}
				}
			}
			for k, c := range v {
				walk(host, path+"."+k, c)
			}
		case []any:
			for i, c := range v {
				walk(host, fmt.Sprintf("%s[%d]", path, i), c)
			}
		}
	}
	for name, s := range schemas {
		if diagramSchemaScope(name) {
			walk(name, name, s)
		}
	}

	sort.Strings(problems)
	require.Empty(t, problems, "diagram schema structure violations:\n%s", strings.Join(problems, "\n"))
}
