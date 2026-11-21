package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenAPIValidation_ThreatStatusCappedAtColumnSize guards CATS run
// 20261121T201842Z: threats.status is varchar(128) but the spec allowed 256
// characters, so a 129..256 char status reached the database and returned 500.
// The request validator must reject it with 400 before any handler runs.
func TestOpenAPIValidation_ThreatStatusCappedAtColumnSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	validator, err := SetupOpenAPIValidation()
	require.NoError(t, err)

	r := gin.New()
	r.Use(validator)
	r.POST("/threat_models/:threat_model_id/threats", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	tests := []struct {
		name   string
		status string
		want   int
	}{
		{"at column size", strings.Repeat("a", 128), http.StatusCreated},
		{"one over column size", strings.Repeat("a", 129), http.StatusBadRequest},
		{"spec-old maximum", strings.Repeat("a", 256), http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"name":"T","status":"` + tt.status + `"}`
			req := httptest.NewRequest(http.MethodPost, "/threat_models/"+testUUID1+"/threats", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tt.want, w.Code, w.Body.String())
		})
	}
}

// TestValidateSparseAuthorizationEntries_RejectsOverlongSubject guards the
// groups.group_name varchar(500) and groups.provider varchar(100) columns on
// the JSON Patch path, which bypasses the request-body schema limits.
func TestValidateSparseAuthorizationEntries_RejectsOverlongSubject(t *testing.T) {
	entry := func(provider, providerID string) []Authorization {
		return []Authorization{{
			Provider:      provider,
			ProviderId:    providerID,
			PrincipalType: AuthorizationPrincipalTypeGroup,
			Role:          AuthorizationRoleReader,
		}}
	}
	tests := []struct {
		name    string
		entries []Authorization
		wantErr bool
	}{
		{"group name at limit", entry("tmi", strings.Repeat("g", 500)), false},
		{"group name over limit", entry("tmi", strings.Repeat("g", 501)), true},
		{"provider at limit", entry(strings.Repeat("p", 100), "grp"), false},
		{"provider over limit", entry(strings.Repeat("p", 101), "grp"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSparseAuthorizationEntries(tt.entries)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			var reqErr *RequestError
			require.ErrorAs(t, err, &reqErr)
			assert.Equal(t, http.StatusBadRequest, reqErr.Status)
		})
	}
}

// TestApplyPatchOperations_EnforcesRequestSchemaLimits guards the JSON Patch
// bypass of body-schema limits: an over-long or otherwise invalid field must be
// a 400 for every patchable entity type, and a valid patch must still apply.
func TestApplyPatchOperations_EnforcesRequestSchemaLimits(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	replace := func(path string, v any) []PatchOperation {
		return []PatchOperation{{Op: "replace", Path: path, Value: v}}
	}
	type tc struct {
		name  string
		apply func(ops []PatchOperation) error
		ops   []PatchOperation
	}
	apply := func(entity any) func([]PatchOperation) error {
		return func(ops []PatchOperation) error {
			var err error
			switch e := entity.(type) {
			case ThreatModel:
				_, err = ApplyPatchOperations(e, ops)
			case Threat:
				_, err = ApplyPatchOperations(e, ops)
			case Asset:
				_, err = ApplyPatchOperations(e, ops)
			case Document:
				_, err = ApplyPatchOperations(e, ops)
			case Repository:
				_, err = ApplyPatchOperations(e, ops)
			case DfdDiagram:
				_, err = ApplyPatchOperations(e, ops)
			case Team:
				_, err = ApplyPatchOperations(e, ops)
			case Project:
				_, err = ApplyPatchOperations(e, ops)
			case Note:
				_, err = ApplyPatchOperations(e, ops)
			default:
				t.Fatalf("unhandled entity type %T", entity)
			}
			return err
		}
	}
	bad := []tc{
		{"threat name", apply(Threat{}), replace("/name", long(257))},
		{"threat status", apply(Threat{}), replace("/status", long(129))},
		{"threat severity", apply(Threat{}), replace("/severity", long(51))},
		{"threat priority", apply(Threat{}), replace("/priority", long(257))},
		{"threat model name", apply(patchTestThreatModel()), replace("/name", long(257))},
		{"threat model status", apply(patchTestThreatModel()), replace("/status", long(129))},
		{"asset name", apply(Asset{}), replace("/name", long(257))},
		{"asset criticality", apply(Asset{}), replace("/criticality", long(129))},
		{"document name", apply(Document{}), replace("/name", long(257))},
		{"repository name", apply(Repository{}), replace("/name", long(257))},
		{"diagram name", apply(DfdDiagram{}), replace("/name", long(257))},
		{"team name", apply(Team{}), replace("/name", long(257))},
		{"project name", apply(Project{}), replace("/name", long(257))},
		{"note name", apply(Note{}), replace("/name", long(257))},
	}
	for _, tt := range bad {
		t.Run("rejects "+tt.name, func(t *testing.T) {
			err := tt.apply(tt.ops)
			var reqErr *RequestError
			require.ErrorAs(t, err, &reqErr)
			assert.Equal(t, http.StatusBadRequest, reqErr.Status)
			assert.NotContains(t, reqErr.Message, long(129), "message must not echo the value")
		})
	}
	good := []tc{
		{"threat", apply(Threat{}), replace("/status", long(128))},
		{"threat model", apply(patchTestThreatModel()), replace("/name", "ok")},
		{"asset", apply(Asset{}), replace("/name", long(256))},
		{"document", apply(Document{}), replace("/name", "ok")},
		{"repository", apply(Repository{}), replace("/name", "ok")},
		{"diagram", apply(DfdDiagram{}), replace("/name", "ok")},
		{"team", apply(Team{}), replace("/name", "ok")},
		{"project", apply(Project{}), replace("/name", "ok")},
		{"note", apply(Note{}), replace("/name", "ok")},
	}
	for _, tt := range good {
		t.Run("accepts "+tt.name, func(t *testing.T) {
			require.NoError(t, tt.apply(tt.ops))
		})
	}
}

// TestOpenAPIValidation_PinnedColumnLimits pins the spec caps that match
// column sizes for usability feedback fields.
func TestOpenAPIValidation_PinnedColumnLimits(t *testing.T) {
	swagger, err := GetSwagger()
	require.NoError(t, err)
	pins := map[string]map[string]uint64{
		"UsabilityFeedbackInput": {"client_build": 12, "viewport": 11},
		"AdminUser":              {"provider": 100},
		"AdminGroup":             {"provider": 100},
		"ThreatBase":             {"status": 128},
		"EmbeddingIngestionItem": {"entity_type": 30, "entity_id": 36, "content_hash": 64, "embedding_model": 100},
	}
	for schema, props := range pins {
		for prop, want := range props {
			p := patchSchemaProperty(swagger.Components.Schemas[schema].Value, prop, 0)
			require.NotNil(t, p, "%s.%s", schema, prop)
			require.NotNil(t, p.Value.MaxLength, "%s.%s", schema, prop)
			assert.Equal(t, want, *p.Value.MaxLength, "%s.%s", schema, prop)
		}
	}
}

func patchTestThreatModel() ThreatModel {
	return ThreatModel{Owner: User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      "tmi",
		ProviderId:    "owner",
		DisplayName:   "Owner",
		Email:         "owner@example.com",
	}}
}

// TestApplyPatchOperations_ValidatesOnlyWrittenFields: legacy stored values
// that no longer satisfy the schema must not block patches that do not write them.
func TestApplyPatchOperations_ValidatesOnlyWrittenFields(t *testing.T) {
	legacy := Threat{Name: strings.Repeat("n", 300)}

	t.Run("replace of a different field", func(t *testing.T) {
		got, err := ApplyPatchOperations(legacy, []PatchOperation{{Op: "replace", Path: "/status", Value: "open"}})
		require.NoError(t, err)
		require.NotNil(t, got.Status)
		assert.Equal(t, "open", *got.Status)
	})
	t.Run("copy from legacy field validates destination only", func(t *testing.T) {
		_, err := ApplyPatchOperations(legacy, []PatchOperation{{Op: "copy", From: "/name", Path: "/mitigation"}})
		require.NoError(t, err)
		// the destination is still validated: a 300-char name copied into status (max 128) is rejected
		_, err = ApplyPatchOperations(legacy, []PatchOperation{{Op: "copy", From: "/name", Path: "/status"}})
		var reqErr *RequestError
		require.ErrorAs(t, err, &reqErr)
		assert.Equal(t, http.StatusBadRequest, reqErr.Status)
	})
	t.Run("test op on legacy field", func(t *testing.T) {
		_, err := ApplyPatchOperations(legacy, []PatchOperation{
			{Op: "test", Path: "/name", Value: legacy.Name},
			{Op: "replace", Path: "/status", Value: "open"},
		})
		require.NoError(t, err)
	})
}
