package api

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #1222: a write the server accepts must always be readable. For every request
// schema and the response schema of the same entity, any string the request
// accepts for a property must be accepted by the response for that property,
// both as sent and after SanitizePlainText (which decodes HTML entities).

// inputResponsePairs maps a request schema to the response schema of the same entity.
var inputResponsePairs = map[string]string{
	"ThreatModelInput":         "ThreatModel",
	"ThreatInput":              "Threat",
	"AssetInput":               "Asset",
	"DocumentInput":            "Document",
	"RepositoryInput":          "Repository",
	"DfdDiagramInput":          "DfdDiagram",
	"CreateDiagramRequest":     "DfdDiagram",
	"TeamInput":                "Team",
	"ProjectInput":             "Project",
	"NoteInput":                "Note",
	"TeamNoteInput":            "TeamNote",
	"ProjectNoteInput":         "ProjectNote",
	"WebhookSubscriptionInput": "WebhookSubscription",
	"SurveyInput":              "Survey",
	"SurveyResponseInput":      "SurveyResponse",
	"CreateAddonRequest":       "AddonResponse",
}

// roundTripProbes are plain-text values a client may plausibly send.
var roundTripProbes = []string{
	"plain text",
	"R&D review",
	"a > b and c < d",
	"it's \"quoted\"",
	"&lt;b&gt;bold&lt;/b&gt;",
	"&amp; entity",
	"&#1;control",
	"&#10;newline entity",
	"&#9;tab entity",
	"line one\nline two",
	"tab\tseparated",
}

// stringProperties collects the top-level string properties of a schema,
// following allOf members, with every schema that constrains each one.
func stringProperties(s *openapi3.Schema, out map[string][]*openapi3.Schema) {
	for name, p := range s.Properties {
		if p.Value != nil && p.Value.Type != nil && p.Value.Type.Is("string") {
			out[name] = append(out[name], p.Value)
		}
	}
	for _, m := range s.AllOf {
		if m.Value != nil {
			stringProperties(m.Value, out)
		}
	}
}

func acceptsAll(schemas []*openapi3.Schema, v string) bool {
	for _, s := range schemas {
		if s.VisitJSON(v) != nil {
			return false
		}
	}
	return true
}

func TestRequestAcceptedValuesAreReadableInResponse(t *testing.T) {
	swagger, err := GetSwagger()
	require.NoError(t, err)

	var failures []string
	for inName, outName := range inputResponsePairs {
		inRef, ok := swagger.Components.Schemas[inName]
		require.True(t, ok, "request schema %s", inName)
		outRef, ok := swagger.Components.Schemas[outName]
		require.True(t, ok, "response schema %s", outName)

		in, out := map[string][]*openapi3.Schema{}, map[string][]*openapi3.Schema{}
		stringProperties(inRef.Value, in)
		stringProperties(outRef.Value, out)

		for prop, inSchemas := range in {
			outSchemas, ok := out[prop]
			if !ok {
				continue
			}
			for _, probe := range roundTripProbes {
				if !acceptsAll(inSchemas, probe) {
					continue
				}
				for label, stored := range map[string]string{"raw": probe, "sanitized": SanitizePlainText(probe), "markdown-sanitized": SanitizeMarkdownContent(probe)} {
					if !acceptsAll(outSchemas, stored) {
						failures = append(failures, fmt.Sprintf("%s.%s -> %s.%s: request accepts %q, response rejects %s value %q",
							inName, prop, outName, prop, probe, label, stored))
					}
				}
			}
		}
	}
	sort.Strings(failures)
	require.Empty(t, failures, "request/response round-trip mismatches:\n%s", strings.Join(failures, "\n"))
}

func TestSanitizePlainText_DropsDecodedControlChars(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"numeric reference to C0 control is dropped", "a&#1;b", "ab"},
		{"hex reference to unit separator is dropped", "a&#x1F;b", "ab"},
		{"entity newline dropped when input had no raw newline", "a&#10;b", "ab"},
		{"entity tab dropped when input had no raw tab", "a&#9;b", "ab"},
		{"raw newline and tab survive", "line one\nline two\tend", "line one\nline two\tend"},
		{"CRLF is normalized to LF by the HTML tokenizer", "a\r\nb", "a\nb"},
		{"double-encoded control reference is dropped", "a&amp;#1;b", "ab"},
		{"ordinary text unchanged", "aws >= 5.0.0 & ~> 2.25", "aws >= 5.0.0 & ~> 2.25"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, SanitizePlainText(tc.in))
		})
	}
}

// TestSanitizePlainText_NoReassembledMarkup: stripping one layer of markup and
// then unescaping must never leave a live tag behind, whether the outer tag is
// built by nesting, by entity encoding, or by a control character that is
// dropped after decoding (security review of #1222/#1223).
func TestSanitizePlainText_NoReassembledMarkup(t *testing.T) {
	payloads := []string{
		"<<b>img src=x onerror=alert(1)>",
		"<<b>script>alert(1)<</b>/script>",
		"&lt;&lt;b&gt;img src=x onerror=alert(1)&gt;",
		"<&#1;img src=x o&#1;nerror=alert(1)>",
		"<&#1;script>alert(1)</&#1;script>",
		"<&#10;img src=x onerror=alert(1)>",
		"<<<b>b>img src=x onerror=alert(1)>",
		"&amp;lt;&amp;lt;b&amp;gt;img src=x onerror=alert(1)&amp;gt;",
	}
	for _, p := range payloads {
		out := SanitizePlainText(p)
		assert.NotRegexp(t, `<[A-Za-z/!?]`, out, "input %q left markup %q", p, out)
		assert.Equal(t, out, SanitizePlainText(out), "sanitizing must be idempotent for %q", p)
	}
	// Ordinary text with comparison operators still survives unchanged.
	for _, s := range angleBracketTexts {
		assert.Equal(t, s, SanitizePlainText(s))
	}
}

func TestSanitizePlainText_DeepNestingNeverLeavesMarkup(t *testing.T) {
	// Each layer of "<b>" nesting costs one pass; nest past the pass bound.
	p := "img src=x onerror=alert(1)>"
	for i := 0; i < 25; i++ {
		p = "<<b>" + p
	}
	out := SanitizePlainText(p)
	assert.NotRegexp(t, `<[A-Za-z/!?]`, out)
}
