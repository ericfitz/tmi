package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

// SEM@f7eaeac5d84cc3e6042933b926820caa7b7c6825: load and validate the OpenAPI spec from the repository file (test helper)
func loadSpecFromFile(t *testing.T) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(filepath.Join("..", "api-schema", "tmi-openapi.json"))
	require.NoError(t, err)
	require.NoError(t, doc.Validate(loader.Context))
	return doc
}

// SEM@f7eaeac5d84cc3e6042933b926820caa7b7c6825: validate frozen diagram payloads against their schemas, valid and invalid (test)
func TestDiagramPayloadCorpus(t *testing.T) {
	doc := loadSpecFromFile(t)
	for _, dir := range []string{"valid", "invalid"} {
		files, err := filepath.Glob(filepath.Join("testdata", "diagram_payloads", dir, "*.json"))
		require.NoError(t, err)
		require.NotEmpty(t, files, "no corpus files in %s", dir)
		for _, f := range files {
			name := strings.TrimSuffix(filepath.Base(f), ".json")
			schemaName, _, ok := strings.Cut(name, "__")
			require.True(t, ok, "corpus file %s must be named <Schema>__<case>.json", f)
			t.Run(dir+"/"+name, func(t *testing.T) {
				ref, found := doc.Components.Schemas[schemaName]
				require.True(t, found, "schema %s not in spec", schemaName)
				raw, err := os.ReadFile(f)
				require.NoError(t, err)
				var v any
				require.NoError(t, json.Unmarshal(raw, &v))
				err = ref.Value.VisitJSON(v)
				if dir == "valid" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}
