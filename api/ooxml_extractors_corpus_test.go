//go:build corpus

package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOOXMLCorpus runs each file in testdata/ooxml-corpus/ through the
// matching extractor and compares against a sibling .expected.md fixture.
//
// Build-tagged "corpus" because real-document fixtures may be heavy or
// require regenerating expected output when extractor behavior intentionally
// changes. Run via `make test-corpus-ooxml`.
//
// If the corpus directory is empty or absent, the test skips with a clear
// message (developer hint to populate the corpus). This is intentional:
// the scaffold lands without fixtures so it can be exercised the moment
// fixtures are added.
func TestOOXMLCorpus(t *testing.T) {
	dir := "../testdata/ooxml-corpus"
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("corpus dir not found at %s — populate it to enable corpus tests", dir)
		}
		t.Fatalf("reading corpus dir: %v", err)
	}

	seen := 0
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".expected.md") || e.Name() == "README.md" {
			continue
		}
		name := e.Name()

		var ct string
		var ext ContentExtractor
		switch {
		case strings.HasSuffix(name, ".docx"):
			ct = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
			ext = NewDOCXExtractor(defaultOOXMLLimits())
		case strings.HasSuffix(name, ".pptx"):
			ct = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
			ext = NewPPTXExtractor(defaultOOXMLLimits())
		case strings.HasSuffix(name, ".xlsx"):
			ct = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
			ext = NewXLSXExtractor(defaultOOXMLLimits())
		default:
			t.Logf("corpus: skipping unrecognized file %q (no matching extractor)", name)
			continue
		}
		seen++

		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, name))
			require.NoError(t, err, "read corpus file")

			out, err := ext.Extract(data, ct)
			require.NoError(t, err, "extract corpus file")

			expected, err := os.ReadFile(filepath.Join(dir, name+".expected.md"))
			require.NoError(t, err, "missing .expected.md sibling — create it with the extractor's current output to lock in expected behavior")

			if string(expected) != out.Text {
				t.Errorf("corpus mismatch for %s:\n--- expected ---\n%s\n--- got ---\n%s",
					name, string(expected), out.Text)
			}
		})
	}

	if seen == 0 {
		t.Skip("corpus dir present but contains no .docx/.pptx/.xlsx files — populate to enable")
	}
}
