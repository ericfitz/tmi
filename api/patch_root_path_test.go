package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// A root-path JSON Patch op must be rejected with 400 invalid_input, never
// applied to the in-memory object and answered 200 (#1109).
func TestPatchIntakeSurveyResponse_RejectsRootPathOp(t *testing.T) {
	store := newMockSurveyResponseStore()
	saveSurveyStores(t, nil, store)
	id := seedSurveyResponse(store, uuid.New(), ResponseStatusDraft, TestUsers.Owner.InternalUUID)

	patch := []byte(`[{"op":"replace","path":"","value":{"status":"submitted","is_confidential":true}}]`)
	c, w := CreateTestGinContextWithBody("PATCH", fmt.Sprintf("/intake/survey_responses/%s", id), "application/json-patch+json", patch)
	TestUsers.Owner.SetContext(c)
	(&Server{}).PatchIntakeSurveyResponse(c, id, PatchIntakeSurveyResponseParams{})

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid_input")
}

func TestPatchDiagram_RejectsRootPathOp(t *testing.T) {
	InitializeMockStores()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "bob@example.com")
		c.Set("userID", "bob-provider-id")
		c.Set("userIdP", "test")
		c.Set("userProvider", "test")
		c.Set("userName", "Bob")
		c.Set("userId", "bob@example.com")
		c.Next()
	})
	r.Use(ThreatModelMiddleware())

	wsHub := NewWebSocketHubForTests()
	tmHandler := NewThreatModelHandler(wsHub)
	diagramHandler := NewThreatModelDiagramHandler(wsHub)
	r.POST("/threat_models", tmHandler.CreateThreatModel)
	r.POST("/threat_models/:threat_model_id/diagrams", func(c *gin.Context) {
		diagramHandler.CreateDiagram(c, c.Param("threat_model_id"))
	})
	r.PATCH("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		diagramHandler.PatchDiagram(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})

	do := func(method, path string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	tmBody, _ := json.Marshal(map[string]any{"name": "Test TM", "threat_model_framework": "STRIDE"})
	w := do("POST", "/threat_models", tmBody)
	require.Equal(t, http.StatusCreated, w.Code)
	var tm ThreatModel
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tm))

	dBody, _ := json.Marshal(map[string]any{"name": "D", "type": "DFD-1.0.0"})
	w = do("POST", "/threat_models/"+tm.Id.String()+"/diagrams", dBody)
	require.Equal(t, http.StatusCreated, w.Code)
	var d DfdDiagram
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &d))

	w = do("PATCH", "/threat_models/"+tm.Id.String()+"/diagrams/"+d.Id.String(),
		[]byte(`[{"op":"replace","path":"","value":{"name":"x"}}]`))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid_input")

	// A path without a leading slash passes the binding tags, so only the
	// shared validator catches it.
	w = do("PATCH", "/threat_models/"+tm.Id.String()+"/diagrams/"+d.Id.String(),
		[]byte(`[{"op":"replace","path":"name","value":"x"}]`))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid_input")

	// move removes its source, so a prohibited "from" must be rejected too.
	w = do("PATCH", "/threat_models/"+tm.Id.String()+"/diagrams/"+d.Id.String(),
		[]byte(`[{"op":"move","path":"/description","from":"/id"}]`))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid_input")
}

// The OpenAPI request validator must enforce the JsonPatchDocument schema:
// root path "" is rejected with 400, and move/copy "from" is accepted.
func TestOpenAPIValidation_JSONPatchPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mw, err := SetupOpenAPIValidation()
	require.NoError(t, err)
	r := gin.New()
	r.Use(mw)
	r.PATCH("/projects/:project_id", func(c *gin.Context) { c.Status(http.StatusOK) })
	url := "/projects/" + uuid.New().String()

	tests := []struct {
		name string
		body string
		want int
	}{
		{"root path rejected", `[{"op":"replace","path":"","value":{"name":"x"}}]`, http.StatusBadRequest},
		{"path without slash rejected", `[{"op":"replace","path":"name","value":"x"}]`, http.StatusBadRequest},
		{"from without slash rejected", `[{"op":"move","path":"/a","from":"b"}]`, http.StatusBadRequest},
		{"normal replace accepted", `[{"op":"replace","path":"/name","value":"x"}]`, http.StatusOK},
		{"move with from accepted", `[{"op":"move","path":"/a","from":"/b"}]`, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("PATCH", url, bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json-patch+json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tt.want, w.Code, w.Body.String())
		})
	}
}

func TestPatchIntakeSurveyResponse_RejectsProhibitedFromPath(t *testing.T) {
	for _, op := range []string{"move", "copy"} {
		t.Run(op, func(t *testing.T) {
			store := newMockSurveyResponseStore()
			saveSurveyStores(t, nil, store)
			id := seedSurveyResponse(store, uuid.New(), ResponseStatusDraft, TestUsers.Owner.InternalUUID)

			patch := []byte(fmt.Sprintf(`[{"op":%q,"path":"/ui_state","from":"/owner"}]`, op))
			c, w := CreateTestGinContextWithBody("PATCH", fmt.Sprintf("/intake/survey_responses/%s", id), "application/json-patch+json", patch)
			TestUsers.Owner.SetContext(c)
			(&Server{}).PatchIntakeSurveyResponse(c, id, PatchIntakeSurveyResponseParams{})

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "invalid_input")
		})
	}
}

func TestValidatePatchProhibitedPaths_FromAndPrefix(t *testing.T) {
	assert.NotNil(t, ValidatePatchProhibitedPaths([]PatchOperation{{Op: "move", Path: "/description", From: "/id"}}))
	assert.NotNil(t, ValidatePatchProhibitedPaths([]PatchOperation{{Op: "copy", Path: "/description", From: "/created_at"}}))
	assert.NotNil(t, ValidatePatchProhibitedPaths([]PatchOperation{{Op: "move", Path: "/description", From: "/id/x"}}))
	assert.Nil(t, ValidatePatchProhibitedPaths([]PatchOperation{{Op: "move", Path: "/description", From: "/name"}}))
	// "from" is ignored for ops that do not use it.
	assert.Nil(t, ValidatePatchProhibitedPaths([]PatchOperation{{Op: "replace", Path: "/name", From: "/id"}}))
}

func TestBulkPatchThreats_RejectsProhibitedPaths(t *testing.T) {
	ops := []map[string]any{
		{"op": "replace", "path": "/id", "value": testUUID1},
		{"op": "replace", "path": "/created_at", "value": "2020-01-01T00:00:00Z"},
		{"op": "move", "path": "/description", "from": "/id"},
	}
	for _, op := range ops {
		t.Run(fmt.Sprintf("%v_%v", op["op"], op["path"]), func(t *testing.T) {
			r, mockStore := setupThreatSubResourceHandler()
			mockStore.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&Threat{}, nil).Maybe()
			mockStore.On("Get", mock.Anything, mock.Anything).Return(&Threat{}, nil).Maybe()

			body, err := json.Marshal(map[string]any{
				"patches": []map[string]any{{"id": testUUID2, "operations": []map[string]any{op}}},
			})
			require.NoError(t, err)
			req := httptest.NewRequest("PATCH", "/threat_models/"+testUUID1+"/threats/bulk", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "invalid_input")
			mockStore.AssertNotCalled(t, "Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		})
	}
}
