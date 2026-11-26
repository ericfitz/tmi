package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression tests for #1234: spec corrections from CATS run 20261122, where
// the server behavior is correct and the embedded spec was wrong.

// TestSpec1234_MeSessionsIsArray: GET /me/sessions returns a JSON array; the
// 200 schema must accept the handler's actual body.
func TestSpec1234_MeSessionsIsArray(t *testing.T) {
	swagger, err := GetSwagger()
	require.NoError(t, err)
	op := swagger.Paths.Find("/me/sessions").Get
	require.NotNil(t, op)
	schema := op.Responses.Status(http.StatusOK).Value.Content.Get("application/json").Schema
	require.NotNil(t, schema)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	server := &Server{wsHub: NewWebSocketHubForTests()}
	router.GET("/me/sessions", server.GetCurrentUserSessions)
	req, err := http.NewRequest("GET", "/me/sessions", nil)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.NoError(t, schema.Value.VisitJSON(body), "handler body must match the documented 200 schema")

	// The example must also be an array, or CATS compares against an object.
	example := op.Responses.Status(http.StatusOK).Value.Content.Get("application/json").Example
	_, isArray := example.([]any)
	assert.True(t, isArray, "the /me/sessions 200 example must be an array")
}

// TestSpec1234_SourceSnapshotUncapped: a chat session's source_snapshot reflects
// the threat model's size, so large snapshots (CATS saw 495) must validate.
func TestSpec1234_SourceSnapshotUncapped(t *testing.T) {
	swagger, err := GetSwagger()
	require.NoError(t, err)
	session := swagger.Components.Schemas["TimmyChatSession"]
	require.NotNil(t, session)
	prop := session.Value.Properties["source_snapshot"]
	require.NotNil(t, prop)

	items := make([]any, 0, 500)
	for i := range 500 {
		items = append(items, map[string]any{
			"entity_type": "threat",
			"entity_id":   fmt.Sprintf("00000000-0000-4000-8000-%012d", i),
		})
	}
	assert.NoError(t, prop.Value.VisitJSON(items))
}

// TestSpec1234_ClientCallbackRequired: /oauth2/authorize and /oauth2/step_up
// cannot complete without client_callback, so the spec marks it required;
// SAML login genuinely treats it as optional.
func TestSpec1234_ClientCallbackRequired(t *testing.T) {
	swagger, err := GetSwagger()
	require.NoError(t, err)
	find := func(path string) *openapi3.Parameter {
		op := swagger.Paths.Find(path).Get
		require.NotNil(t, op, path)
		p := op.Parameters.GetByInAndName("query", "client_callback")
		require.NotNil(t, p, path)
		return p
	}
	assert.True(t, find("/oauth2/authorize").Required, "/oauth2/authorize")
	assert.True(t, find("/oauth2/step_up").Required, "/oauth2/step_up")
	assert.False(t, find("/saml/{provider}/login").Required, "/saml/{provider}/login")
}
