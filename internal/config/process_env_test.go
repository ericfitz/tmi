package config

import (
	"regexp"
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

func TestProcessEnvVars_ReturnsACopy(t *testing.T) {
	a := ProcessEnvVars()
	a[0].Name = "MUTATED"
	if ProcessEnvVars()[0].Name == "MUTATED" {
		t.Error("ProcessEnvVars must return a copy, not the backing slice")
	}
}
