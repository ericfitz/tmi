package errcode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func loadSpec(t *testing.T) map[string]any {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "api-schema", "tmi-openapi.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	return spec
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func schemaEnum(spec map[string]any, name string) []string {
	schemas := asMap(asMap(spec["components"])["schemas"])
	prop := asMap(asMap(asMap(schemas[name])["properties"])["error"])
	var out []string
	for _, v := range prop["enum"].([]any) {
		out = append(out, v.(string))
	}
	return out
}

func assertSameSet(t *testing.T, what string, got []string, want []Code) {
	t.Helper()
	g := map[string]bool{}
	for _, s := range got {
		g[s] = true
	}
	w := map[string]bool{}
	for _, c := range want {
		w[string(c)] = true
	}
	var diff []string
	for s := range g {
		if !w[s] {
			diff = append(diff, "spec only: "+s)
		}
	}
	for s := range w {
		if !g[s] {
			diff = append(diff, "constants only: "+s)
		}
	}
	sort.Strings(diff)
	if len(diff) > 0 {
		t.Errorf("%s drift: %s", what, strings.Join(diff, ", "))
	}
}

func TestSpecEnumsMatchConstants(t *testing.T) {
	spec := loadSpec(t)
	assertSameSet(t, "Error.error", schemaEnum(spec, "Error"), REST())
	assertSameSet(t, "OAuthError.error", schemaEnum(spec, "OAuthError"), Protocol())
}

// exampleErrors collects every example error value under a response content entry.
func exampleErrors(content map[string]any) []string {
	var out []string
	if ex := asMap(content["example"]); ex != nil {
		if s, ok := ex["error"].(string); ok {
			out = append(out, s)
		}
	}
	for _, e := range asMap(content["examples"]) {
		if s, ok := asMap(asMap(asMap(e)["value"]))["error"].(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestSpecExamplesUseDocumentedCodes(t *testing.T) {
	spec := loadSpec(t)
	enums := map[string]map[string]bool{}
	for _, name := range []string{"Error", "OAuthError"} {
		enums["#/components/schemas/"+name] = map[string]bool{}
		for _, s := range schemaEnum(spec, name) {
			enums["#/components/schemas/"+name][s] = true
		}
	}
	check := func(where string, resp map[string]any) {
		content := asMap(asMap(resp["content"])["application/json"])
		ref, _ := asMap(content["schema"])["$ref"].(string)
		enum, ok := enums[ref]
		if !ok {
			return
		}
		for _, e := range exampleErrors(content) {
			if !enum[e] {
				t.Errorf("%s: example error %q is not in the %s enum", where, e, ref)
			}
		}
	}
	for p, item := range asMap(spec["paths"]) {
		for method, op := range asMap(item) {
			for status, resp := range asMap(asMap(op)["responses"]) {
				check(p+" "+method+" "+status, asMap(resp))
			}
		}
	}
	for name, resp := range asMap(asMap(spec["components"])["responses"]) {
		check("components.responses."+name, asMap(resp))
	}
}

var snakeCase = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func TestDetailsCodeExamplesAreSnakeCase(t *testing.T) {
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			if d := asMap(x["details"]); d != nil {
				if s, ok := d["code"].(string); ok && !snakeCase.MatchString(s) {
					t.Errorf("%s: details.code %q is not snake_case", path, s)
				}
			}
			if props := asMap(x["properties"]); props != nil {
				if c, ok := asMap(asMap(props["details"])["properties"])["code"].(map[string]any); ok {
					if s, ok := c["example"].(string); ok && !snakeCase.MatchString(s) {
						t.Errorf("%s: details.code example %q is not snake_case", path, s)
					}
				}
			}
			for k, c := range x {
				walk(path+"/"+k, c)
			}
		case []any:
			for i, c := range x {
				walk(path, c)
				_ = i
			}
		}
	}
	walk("", loadSpec(t))
}
