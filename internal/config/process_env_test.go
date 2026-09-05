package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// wordBoundaryUnderscore matches an underscore adjacent to whitespace or a
// string boundary. CommonMark treats such an underscore as emphasis-capable;
// a pair of them swallows the text between as italics and drops both
// characters from the rendered Markdown (#810).
var wordBoundaryUnderscore = regexp.MustCompile(`(^|\s)_|_(\s|$)`)

// TestProcessEnvVars_WellFormed keeps the hand-maintained inventory
// renderable: every row is complete, names are unique, Pattern agrees with
// the <placeholder> convention the allowlist gate relies on, and Purpose
// cannot break the Markdown table or vanish as HTML.
// SEM@fd5abea29b3b98c68562c6b83c5f6f3778484f1a: verify the process-environment inventory rows are unique, pattern-consistent and Markdown-safe
func TestProcessEnvVars_WellFormed(t *testing.T) {
	vars := ProcessEnvVars()
	if len(vars) == 0 {
		t.Fatal("ProcessEnvVars is empty")
	}
	seen := map[string]bool{}
	for _, p := range vars {
		if p.Name == "" || p.Binary == "" || p.Purpose == "" {
			t.Errorf("%+v: Name, Binary and Purpose are all required", p)
		}
		if seen[p.Name] {
			t.Errorf("%s is declared twice", p.Name)
		}
		seen[p.Name] = true
		if p.Pattern != strings.Contains(p.Name, "<") {
			t.Errorf("%s: Pattern must be true exactly when Name carries a <placeholder>", p.Name)
		}
		if p.Pattern && strings.HasPrefix(p.Name, "<") {
			t.Errorf("%s: a pattern needs a literal prefix before its first placeholder", p.Name)
		}
		if strings.ContainsAny(p.Purpose, "<>`|\n") {
			t.Errorf("%s: Purpose must not contain <, >, backticks, | or newlines", p.Name)
		}
		if wordBoundaryUnderscore.MatchString(p.Purpose) {
			t.Errorf("%s: Purpose has a whitespace-adjacent underscore, which Markdown "+
				"pairs as emphasis and drops from the rendered text: %q", p.Name, p.Purpose)
		}
	}
}

// SEM@4077bab0af75093efb1660aa34801b2c752c9c0f: verify the process-environment inventory accessor returns a defensive copy
func TestProcessEnvVars_ReturnsACopy(t *testing.T) {
	a := ProcessEnvVars()
	a[0].Name = "MUTATED"
	if ProcessEnvVars()[0].Name == "MUTATED" {
		t.Error("ProcessEnvVars must return a copy, not the backing slice")
	}
}

// tmiTokenExclusions lists TMI_-prefixed tokens that occur in Go source but
// are not environment variables. Every entry carries its justification;
// adding one is a deliberate act.
var tmiTokenExclusions = map[string]string{
	"TMI_DLQ":                    "JetStream stream name (internal/worker/names.go)",
	"TMI_DLQ_ADVISORY":           "JetStream stream name (internal/worker/names.go)",
	"TMI_RESULTS":                "JetStream stream name (internal/worker/names.go)",
	"TMI_PAYLOADS":               "JetStream KV bucket name (internal/worker/names.go)",
	"TMI_SCHEMA_VERSIONS":        "Oracle upper-folded table name (internal/dbschema/schema_version.go)",
	"TMI_THREAT_MODEL_ALIAS_SEQ": "Oracle sequence name (internal/dbschema/alias_sequence.go)",
	"TMI_TMI_EXTRACTOR":          "derived JetStream consumer name in a doc comment (internal/worker/names.go)",
	"TMI_CHUNK_EMBED_CONSUMER":   "derived JetStream consumer name in a doc comment (internal/worker/names.go)",
	"TMI_SERVER_URL":             "read only by the integration-test framework under test/integration/, never by a shipped binary",
	"TMI_CONTENT_EXTRACTORS_":    "bare prefix in a doc comment (cmd/extractor/main.go); every concrete TMI_CONTENT_EXTRACTORS_* name is a registry EnvVar",
	// The prefix argument cmd/server/main.go passes to buildURIValidator,
	// which appends _ALLOWLIST / _SCHEMES; the ten resulting names are
	// ProcessEnvVars.
	"TMI_SSRF_ISSUE_URI":      "buildURIValidator env-var prefix (cmd/server/main.go)",
	"TMI_SSRF_DOCUMENT_URI":   "buildURIValidator env-var prefix (cmd/server/main.go)",
	"TMI_SSRF_REPOSITORY_URI": "buildURIValidator env-var prefix (cmd/server/main.go)",
	"TMI_SSRF_TIMMY":          "buildURIValidator env-var prefix (cmd/server/main.go)",
	"TMI_SSRF_WEBHOOK":        "buildURIValidator env-var prefix (cmd/server/main.go)",
}

// TestRepoTMIEnvTokens_AreAllDocumented is the allowlist gate for #810.
// Every TMI_[A-Z0-9_]+ token in the repository's non-test Go source must be
// a registry EnvVar, a ProcessEnvVar name, an instance of a ProcessEnvVar
// prefix pattern, or a justified tmiTokenExclusions entry. Anything else is
// an env var — or a new non-env-var naming collision — that
// config-reference.md, the TMI_* allowlist, does not know about. Ceiling: a
// name composed at runtime (`"TMI_" + x`, fmt.Sprintf("TMI_%s", ...)) is
// invisible to this token scan; today the only such construction is a
// JetStream stream name in internal/platform/controller/render_jetstream.go,
// not an env var.
// SEM@b888c597849425243a4e3184a5c552fdcab92488: verify every TMI_ token in Go source is a documented env var or justified exclusion (reads repo)
func TestRepoTMIEnvTokens_AreAllDocumented(t *testing.T) {
	known := map[string]bool{}
	for _, d := range AllSettingDefs() {
		if d.EnvVar != "" {
			known[d.EnvVar] = true
		}
	}
	var prefixes []string
	for _, p := range ProcessEnvVars() {
		if p.Pattern {
			if idx := strings.Index(p.Name, "<"); idx >= 0 {
				prefixes = append(prefixes, p.Name[:idx])
			}
			continue
		}
		known[p.Name] = true
	}
	documented := func(tok string) bool {
		if known[tok] || tmiTokenExclusions[tok] != "" {
			return true
		}
		for _, pre := range prefixes {
			if strings.HasPrefix(tok, pre) {
				return true
			}
		}
		return false
	}

	const root = "../.." // this package is internal/config
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("%s is not the repo root (no go.mod): %v; the gate would scan nothing and pass vacuously", root, err)
	}
	tokenRe := regexp.MustCompile(`TMI_[A-Z0-9_]+`)
	offenders := map[string]bool{}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path) // #nosec G304 G122 -- walking the repository's own source tree
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		for _, tok := range tokenRe.FindAllString(string(src), -1) {
			if !documented(tok) {
				offenders[tok+"  ("+rel+")"] = true
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}

	var list []string
	for o := range offenders {
		list = append(list, o)
	}
	sort.Strings(list)
	if len(list) > 0 {
		t.Errorf("%d TMI_* tokens in Go source that config-reference.md does not document; "+
			"add a SettingDef EnvVar, a ProcessEnvVar, or a justified tmiTokenExclusions entry:\n  %s",
			len(list), strings.Join(list, "\n  "))
	}
}
