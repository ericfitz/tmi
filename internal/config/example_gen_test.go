package config

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGenerateExampleConfig_ContainsBootstrapKeys(t *testing.T) {
	out, err := GenerateExampleConfig()
	if err != nil {
		t.Fatalf("GenerateExampleConfig: %v", err)
	}
	s := string(out)
	for _, want := range []string{"database:", "server:", "logging:"} {
		if !strings.Contains(s, want) {
			t.Errorf("generated example missing %q", want)
		}
	}
}

func TestGenerateExampleConfig_OmitsOperationalKeys(t *testing.T) {
	out, err := GenerateExampleConfig()
	if err != nil {
		t.Fatalf("GenerateExampleConfig: %v", err)
	}
	if strings.Contains(string(out), "inactivity_timeout_seconds") {
		t.Error("generated example should not contain operational keys")
	}
}

// TestCoerceSettingValue_NilSliceJSON pins the fix for a dormant bug: a
// JSON-typed setting whose Value is the marshaled form of a nil Go slice
// ("null") must coerce to an empty YAML list, not the literal string
// "null" (which is invalid YAML for a list-typed config key).
func TestCoerceSettingValue_NilSliceJSON(t *testing.T) {
	s := MigratableSetting{Type: "json", Value: "null"}
	got := coerceSettingValue(s)

	out, err := yaml.Marshal(map[string]any{"trusted_proxies": got})
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	if strings.TrimSpace(string(out)) != "trusted_proxies: []" {
		t.Errorf("nil-slice JSON value coerced to %q, want an empty YAML list; got YAML %q", got, out)
	}
}

// TestCoerceSettingValue_EmptyArrayJSON pins the companion case: an
// explicitly-empty JSON array ("[]") must also coerce to an empty YAML list
// rather than the quoted string "[]".
func TestCoerceSettingValue_EmptyArrayJSON(t *testing.T) {
	s := MigratableSetting{Type: "json", Value: "[]"}
	got := coerceSettingValue(s)

	out, err := yaml.Marshal(map[string]any{"trusted_proxies": got})
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	if strings.TrimSpace(string(out)) != "trusted_proxies: []" {
		t.Errorf("empty-array JSON value coerced to %q, want an empty YAML list; got YAML %q", got, out)
	}
}

// TestCoerceSettingValue_PopulatedJSONArray confirms a populated JSON array
// decodes into a real YAML list, not a quoted JSON string.
func TestCoerceSettingValue_PopulatedJSONArray(t *testing.T) {
	s := MigratableSetting{Type: "json", Value: `["10.0.0.0/8","172.16.0.0/12"]`}
	got := coerceSettingValue(s)

	out, err := yaml.Marshal(map[string]any{"trusted_proxies": got})
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	want := "trusted_proxies:\n    - 10.0.0.0/8\n    - 172.16.0.0/12"
	if strings.TrimSpace(string(out)) != want {
		t.Errorf("populated JSON array coerced to YAML %q, want %q", out, want)
	}
}

// TestGenerateExampleConfig_OmitsDatabaseOnlySettings guards against a
// database-only setting appearing in the config-file template. A setting
// that is CategoryOperational and not Transitional has no config-file
// representation by construction (Phase E's design: it lives only in the
// database), so offering one in the sample file would invite an operator to
// set something the server silently ignores.
//
// The generated document is nested YAML (setNested + yaml.Marshal), so a
// dotted key like "timmy.enabled" never appears as a literal substring even
// when the setting IS emitted — it renders as "timmy:\n    enabled: ...".
// A substring check on the raw text can never see the thing it is supposed
// to guard against, so this parses the generated YAML back into a tree and
// checks dotted paths reconstructed from that tree instead.
func TestGenerateExampleConfig_OmitsDatabaseOnlySettings(t *testing.T) {
	out, err := GenerateExampleConfig()
	if err != nil {
		t.Fatalf("GenerateExampleConfig: %v", err)
	}
	present := yamlDottedPaths(t, out)
	for _, d := range AllSettingDefs() {
		if d.Class.Category == CategoryOperational && !d.Transitional {
			if present[d.Key] {
				t.Errorf("%s is database-only and must not appear in the config template", d.Key)
			}
		}
	}
}

// yamlDottedPaths parses generated YAML and returns the set of every dotted
// path present in it — both intermediate mapping keys and leaf scalar keys
// — so a test can check "is this dotted setting key present" against the
// document's real structure rather than its raw text.
func yamlDottedPaths(t *testing.T, doc []byte) map[string]bool {
	t.Helper()
	var root map[string]any
	if err := yaml.Unmarshal(doc, &root); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	paths := map[string]bool{}
	var walk func(prefix string, node map[string]any)
	walk = func(prefix string, node map[string]any) {
		for k, v := range node {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			paths[path] = true
			if child, ok := v.(map[string]any); ok {
				walk(path, child)
			}
		}
	}
	walk("", root)
	return paths
}

func TestConfigExampleFile_MatchesRegistry(t *testing.T) {
	generated, err := GenerateExampleConfig()
	if err != nil {
		t.Fatalf("GenerateExampleConfig: %v", err)
	}
	onDisk, err := os.ReadFile("../../config-example.yml")
	if err != nil {
		t.Fatalf("read config-example.yml: %v", err)
	}
	strip := func(b []byte) string {
		var keep []string
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "GENERATED by") {
				continue
			}
			keep = append(keep, line)
		}
		return strings.Join(keep, "\n")
	}
	if strip(generated) != strip(onDisk) {
		t.Error("config-example.yml is stale — run `make generate-config-example`")
	}
}
