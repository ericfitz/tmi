package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #1222/#1223: free-text fields must accept and return '<' and '>' (version
// constraints, comparisons, arrows). SanitizePlainText stores decoded text, so
// a value written as "&gt;=" is stored as ">="; the response schema must accept
// what the server stores, and the request schema must accept the same text.

var angleBracketTexts = []string{
	"Providers use open-ended >= constraints, for example aws >= 5.0.0",
	"Use pessimistic constraints, for example aws ~> 2.25",
	"TTL < 300s and key length >= 2048 bits",
	"client -> API -> DB, 0.0.0.0/0 -> NAT",
}

// freeTextFields lists every schema property that used the free-text pattern
// ^[^<>\x00-\x08\x0B\x0C\x0E-\x1F]*$ before #1223.
var freeTextFields = map[string][]string{
	"DfdDiagram":                {"description"},
	"DiagramListItem":           {"description"},
	"ThreatModelBase":           {"description"},
	"ThreatBase":                {"description", "mitigation"},
	"AssetBase":                 {"description"},
	"DocumentBase":              {"description"},
	"NoteBase":                  {"description"},
	"RepositoryBase":            {"description"},
	"SurveyBase":                {"description"},
	"TeamProjectNoteBase":       {"description"},
	"TeamNoteListItem":          {"description"},
	"ProjectNoteListItem":       {"description"},
	"TimmyChatSession":          {"title"},
	"CreateTimmySessionRequest": {"title"},
}

func TestFreeTextFieldsAcceptAngleBrackets(t *testing.T) {
	swagger, err := GetSwagger()
	require.NoError(t, err)

	for schemaName, props := range freeTextFields {
		ref, ok := swagger.Components.Schemas[schemaName]
		require.True(t, ok, "schema %s", schemaName)
		for _, prop := range props {
			p := patchSchemaProperty(ref.Value, prop, 0)
			require.NotNil(t, p, "%s.%s", schemaName, prop)
			for _, text := range angleBracketTexts {
				// What the server stores for both the raw and the entity-encoded form.
				for _, in := range []string{text, strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace(text)} {
					stored := SanitizePlainText(in)
					assert.Equal(t, text, stored, "sanitizer must store decoded text")
					assert.NoError(t, p.Value.VisitJSON(stored), "%s.%s must accept %q", schemaName, prop, stored)
				}
			}
			// Control characters stay forbidden.
			assert.Error(t, p.Value.VisitJSON("bad\x01value"), "%s.%s must still reject control characters", schemaName, prop)
		}
	}
}

// TestNoFreeTextFieldExcludesAngleBrackets fails if any schema property still
// uses the old free-text pattern, so a new field cannot reintroduce the mismatch.
func TestNoFreeTextFieldExcludesAngleBrackets(t *testing.T) {
	spec, err := os.ReadFile("../api-schema/tmi-openapi.json")
	require.NoError(t, err)
	assert.NotContains(t, string(spec), `"^[^<>\\x00-\\x08`, "old free-text pattern still present in the spec")
}

func TestOpenAPIValidation_ThreatCreateAcceptsAngleBrackets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	validator, err := SetupOpenAPIValidation()
	require.NoError(t, err)
	r := gin.New()
	r.Use(validator)
	r.POST("/threat_models/:threat_model_id/threats", func(c *gin.Context) { c.JSON(http.StatusCreated, gin.H{}) })

	body, err := json.Marshal(map[string]any{
		"name":        "Open-ended provider constraints",
		"threat_type": []string{"Tampering"},
		"description": angleBracketTexts[0],
		"mitigation":  angleBracketTexts[1],
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/threat_models/"+testUUID1+"/threats", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}
