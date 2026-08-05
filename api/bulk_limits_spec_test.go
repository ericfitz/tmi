package api

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requestBodyMaxItems returns the maxItems of an operation's JSON request-body
// array, following one $ref level into a wrapper object's named array property.
// SEM@15be6a9f: read a spec operation's request-body maxItems, optionally through one nested property (pure)
func requestBodyMaxItems(t *testing.T, op *openapi3.Operation, nestedProp string) uint64 {
	t.Helper()
	require.NotNil(t, op, "operation missing")
	require.NotNil(t, op.RequestBody)
	mediaType := op.RequestBody.Value.Content["application/json"]
	require.NotNil(t, mediaType, "request body has no application/json content")
	schema := mediaType.Schema.Value
	if nestedProp != "" {
		schema = schema.Properties[nestedProp].Value
	}
	require.NotNil(t, schema.MaxItems, "schema has no maxItems")
	return *schema.MaxItems
}

// TestBulkLimitsMatchSpec pins each MaxBulk* constant in bulk_limits.go to the
// maxItems declared on the corresponding operation's request body in the
// embedded OpenAPI spec, so handler/spec drift fails unit tests instead of
// shipping.
func TestBulkLimitsMatchSpec(t *testing.T) {
	spec, err := GetSpec()
	require.NoError(t, err)
	paths := spec.Paths

	tm := "/threat_models/{threat_model_id}"
	cases := []struct {
		name     string
		path     string
		methods  []string
		nested   string
		constant int
	}{
		{"MaxBulkThreats", tm + "/threats/bulk", []string{"POST", "PUT"}, "", MaxBulkThreats},
		{"MaxBulkThreatPatches", tm + "/threats/bulk", []string{"PATCH"}, "patches", MaxBulkThreatPatches},
		{"MaxBulkDocuments", tm + "/documents/bulk", []string{"POST", "PUT"}, "", MaxBulkDocuments},
		{"MaxBulkRepositories", tm + "/repositories/bulk", []string{"POST", "PUT"}, "", MaxBulkRepositories},
		{"MaxBulkAssets", tm + "/assets/bulk", []string{"POST", "PUT"}, "", MaxBulkAssets},
	}
	for _, tc := range cases {
		item := paths.Find(tc.path)
		require.NotNil(t, item, "path not in spec: %s", tc.path)
		for _, m := range tc.methods {
			got := requestBodyMaxItems(t, item.GetOperation(m), tc.nested)
			assert.Equal(t, uint64(tc.constant), got,
				"%s: constant %d != spec maxItems %d on %s %s", tc.name, tc.constant, got, m, tc.path)
		}
	}
}

// TestBulkThreatDeletesMatchSpec pins MaxBulkThreatDeletes to the maxItems on
// the shared ThreatIdsQueryParam component used by DELETE .../threats/bulk.
func TestBulkThreatDeletesMatchSpec(t *testing.T) {
	spec, err := GetSpec()
	require.NoError(t, err)
	p := spec.Components.Parameters["ThreatIdsQueryParam"]
	require.NotNil(t, p, "ThreatIdsQueryParam missing from components.parameters (DELETE .../threats/bulk)")
	require.NotNil(t, p.Value.Schema.Value.MaxItems,
		"ThreatIdsQueryParam schema has no maxItems (DELETE .../threats/bulk)")
	got := *p.Value.Schema.Value.MaxItems
	assert.Equal(t, uint64(MaxBulkThreatDeletes), got,
		"MaxBulkThreatDeletes: constant %d != spec maxItems %d on ThreatIdsQueryParam (DELETE .../threats/bulk)",
		MaxBulkThreatDeletes, got)
}

// entityForMetadataPath maps ".../<collection>/{x_id}/metadata/bulk" (or the
// threat-model root "/threat_models/{threat_model_id}/metadata/bulk") to the
// entity-type key used by maxBulkMetadataByEntity.
// SEM@15be6a9f: derive the metadata-cap entity key from a metadata/bulk spec path (pure)
func entityForMetadataPath(path string) string {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	// [..., <collection>, {param}, metadata, bulk]
	if len(segs) < 4 {
		return ""
	}
	coll := segs[len(segs)-4]
	singular := map[string]string{
		"threat_models": "threat_model", "threats": "threat", "documents": "document",
		"repositories": "repository", "diagrams": "diagram", "notes": "note",
		"assets": "asset", "surveys": "survey", "survey_responses": "survey_response",
		"teams": "team", "projects": "project",
	}
	return singular[coll]
}

// TestBulkMetadataLimitsMatchSpec pins maxBulkMetadataByEntity to the spec in
// both directions: every .../metadata/bulk path's maxItems must match
// MaxBulkMetadataFor, and every registered entity must have a corresponding
// spec path (guarding against a stale map entry for a removed handler).
func TestBulkMetadataLimitsMatchSpec(t *testing.T) {
	spec, err := GetSpec()
	require.NoError(t, err)

	seen := map[string]bool{}
	for path, item := range spec.Paths.Map() {
		if !strings.HasSuffix(path, "/metadata/bulk") {
			continue
		}
		entity := entityForMetadataPath(path)
		require.NotEmpty(t, entity, "metadata/bulk path with unknown entity: %s", path)
		seen[entity] = true

		_, known := maxBulkMetadataByEntity[entity]
		assert.True(t, known,
			"entity %q (path %s) missing from maxBulkMetadataByEntity — would silently fall back to 20", entity, path)

		for _, m := range []string{"POST", "PUT"} { // PATCH excluded: team/project route through BulkPatchRequest(20)
			if op := item.GetOperation(m); op != nil {
				got := requestBodyMaxItems(t, op, "")
				assert.Equal(t, uint64(MaxBulkMetadataFor(entity)), got,
					"metadata bulk cap for %q: Go %d != spec %d on %s %s",
					entity, MaxBulkMetadataFor(entity), got, m, path)
			}
		}
	}
	// Reverse direction: every map key must exist in the spec.
	for entity := range maxBulkMetadataByEntity {
		assert.True(t, seen[entity], "maxBulkMetadataByEntity has %q but no .../metadata/bulk path in spec", entity)
	}
	assert.Len(t, seen, 11, "expected 11 metadata/bulk entity types in spec")
}
