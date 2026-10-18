package api

import (
	"bytes"
	"encoding/json"
	"errors"
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
	// Any store write would surface as a non-400 status below.
	store.updateErr = errors.New("update must not be called")
	store.updateStatusErr = errors.New("update status must not be called")

	patch := []byte(`[{"op":"replace","path":"","value":{"status":"submitted","is_confidential":true}}]`)
	c, w := CreateTestGinContextWithBody("PATCH", fmt.Sprintf("/intake/survey_responses/%s", id), "application/json-patch+json", patch)
	TestUsers.Owner.SetContext(c)
	(&Server{}).PatchIntakeSurveyResponse(c, id, PatchIntakeSurveyResponseParams{})

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid_input")
	stored := store.responses[id]
	assert.Equal(t, ResponseStatusDraft, *stored.Status)
	assert.False(t, *stored.IsConfidential)
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

func TestPatchProjectAndTeam_RejectProhibitedFromAndSubPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}
	bodies := map[string]string{
		"move from prohibited path": `[{"op":"move","path":"/description","from":"/id"}]`,
		"copy from prohibited path": `[{"op":"copy","path":"/description","from":"/created_by"}]`,
		"prohibited sub-path":       `[{"op":"replace","path":"/id/x","value":"v"}]`,
	}
	for name, body := range bodies {
		t.Run("project "+name, func(t *testing.T) {
			projectStore := newMockProjectStore()
			seedProjectInStore(projectStore, testProjectID, "Project", testTeamID)
			saveTeamProjectStores(t, nil, projectStore)
			db := setupTestTeamAuthDB(t)
			seedTeamAuthData(t, db, testTeamID, testUserUUID)
			seedProjectAuthData(t, db, testProjectID, testTeamID)

			projectUUID, _ := uuid.Parse(testProjectID)
			c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID, "application/json-patch+json", []byte(body))
			TestUsers.Owner.SetContext(c)
			server.PatchProject(c, projectUUID, PatchProjectParams{})

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "not allowed")
		})
		t.Run("team "+name, func(t *testing.T) {
			store := newMockTeamStore()
			seedTeamInStore(store, testTeamID, "Team")
			saveTeamProjectStores(t, store, nil)
			db := setupTestTeamAuthDB(t)
			seedTeamAuthData(t, db, testTeamID, testUserUUID)

			teamUUID, _ := uuid.Parse(testTeamID)
			c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID, "application/json-patch+json", []byte(body))
			TestUsers.Owner.SetContext(c)
			server.PatchTeam(c, teamUUID, PatchTeamParams{})

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "not allowed")
		})
	}
}

func TestBulkPatchThreats_ValidatesAllEntriesBeforeApplying(t *testing.T) {
	r, mockStore := setupThreatSubResourceHandler()
	mockStore.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&Threat{}, nil).Maybe()

	body, err := json.Marshal(map[string]any{
		"patches": []map[string]any{
			{"id": testUUID2, "operations": []map[string]any{{"op": "replace", "path": "/name", "value": "ok"}}},
			{"id": "00000000-0000-0000-0000-000000000003", "operations": []map[string]any{{"op": "replace", "path": "/id", "value": testUUID1}}},
		},
	})
	require.NoError(t, err)
	req := httptest.NewRequest("PATCH", "/threat_models/"+testUUID1+"/threats/bulk", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	mockStore.AssertNotCalled(t, "Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestPatchGuards_RejectMoveCopyFromGuardedPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}
	patches := map[string]string{
		"move from guarded": `[{"op":"move","path":"/description","from":"%s"}]`,
		"copy from guarded": `[{"op":"copy","path":"/description","from":"%s"}]`,
	}

	t.Run("admin survey", func(t *testing.T) {
		for name, tmpl := range patches {
			t.Run(name, func(t *testing.T) {
				store := newMockSurveyStore()
				saveSurveyStores(t, store, nil)
				surveyID := seedSurvey(store, "Original", "v1", SurveyStatusActive)
				c, w := CreateTestGinContextWithBody(http.MethodPatch, fmt.Sprintf("/admin/surveys/%s", surveyID),
					"application/json-patch+json", []byte(fmt.Sprintf(tmpl, "/created_by")))
				TestUsers.Owner.SetContext(c)
				server.PatchAdminSurvey(c, surveyID)
				assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
				assert.Contains(t, w.Body.String(), "not allowed")
			})
		}
	})

	t.Run("triage survey response", func(t *testing.T) {
		for name, tmpl := range patches {
			t.Run(name, func(t *testing.T) {
				store := newMockSurveyResponseStore()
				saveSurveyStores(t, nil, store)
				id := seedSurveyResponse(store, uuid.New(), ResponseStatusSubmitted, TestUsers.Owner.InternalUUID)
				c, w := CreateTestGinContextWithBody("PATCH", fmt.Sprintf("/triage/survey_responses/%s", id),
					"application/json-patch+json", []byte(fmt.Sprintf(tmpl, "/owner")))
				asTriageUser(t, c, GroupSecurityReviewers)
				server.PatchTriageSurveyResponse(c, id)
				assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
				assert.Contains(t, w.Body.String(), "not allowed")
			})
		}
	})

	t.Run("team note sharable", func(t *testing.T) {
		for name, tmpl := range patches {
			t.Run(name, func(t *testing.T) {
				store := newMockTeamNoteStore()
				seedTeamNoteInStore(store, testTeamNoteID, true)
				saveTeamNoteStore(t, store)
				db := setupTestTeamAuthDB(t)
				seedTeamAuthData(t, db, testTeamID, testUserUUID)
				teamUUID, _ := uuid.Parse(testTeamID)
				noteUUID, _ := uuid.Parse(testTeamNoteID)
				c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID+"/notes/"+testTeamNoteID,
					"application/json", []byte(fmt.Sprintf(tmpl, "/sharable")))
				TestUsers.Owner.SetContext(c)
				server.PatchTeamNote(c, teamUUID, noteUUID)
				assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			})
		}
	})

	t.Run("project note sharable", func(t *testing.T) {
		for name, tmpl := range patches {
			t.Run(name, func(t *testing.T) {
				store := newMockProjectNoteStore()
				seedProjectNoteInStore(store, testProjectNoteID, true)
				saveProjectNoteStore(t, store)
				db := setupTestTeamAuthDB(t)
				seedTeamAuthData(t, db, testTeamID, testUserUUID)
				seedProjectAuthData(t, db, testProjectID, testTeamID)
				projectUUID, _ := uuid.Parse(testProjectID)
				noteUUID, _ := uuid.Parse(testProjectNoteID)
				c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID+"/notes/"+testProjectNoteID,
					"application/json", []byte(fmt.Sprintf(tmpl, "/sharable")))
				TestUsers.Owner.SetContext(c)
				server.PatchProjectNote(c, projectUUID, noteUUID)
				assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			})
		}
	})
}

// Removing /status (via remove or move-from) is a 400; copy/move into /status
// is validated as a status target. In every case the stored status stays put.
func TestPatchSurveyResponse_StatusViaMoveCopyRemove(t *testing.T) {
	cases := map[string]struct {
		patch string
		want  int
		// writes is true when the case legitimately rewrites revision_notes or
		// reaches the store's own status validation.
		writes bool
	}{
		"move from status":             {`[{"op":"move","path":"/revision_notes","from":"/status"}]`, http.StatusBadRequest, false},
		"remove status":                {`[{"op":"remove","path":"/status"}]`, http.StatusBadRequest, false},
		"copy status elsewhere":        {`[{"op":"copy","path":"/revision_notes","from":"/status"}]`, http.StatusOK, true},
		"copy bogus value into status": {`[{"op":"copy","path":"/status","from":"/revision_notes"}]`, http.StatusBadRequest, true},
		"move bogus value into status": {`[{"op":"move","path":"/status","from":"/revision_notes"}]`, http.StatusBadRequest, true},
	}
	for _, handler := range []string{"intake", "triage"} {
		for name, tc := range cases {
			t.Run(handler+" "+name, func(t *testing.T) {
				store := newMockSurveyResponseStore()
				saveSurveyStores(t, nil, store)
				initial := ResponseStatusDraft
				if handler == "triage" {
					initial = ResponseStatusSubmitted
				}
				id := seedSurveyResponse(store, uuid.New(), initial, TestUsers.Owner.InternalUUID)
				notes := "not-a-status"
				store.responses[id].RevisionNotes = &notes
				// The real store rejects unknown status values; the mock needs telling.
				store.updateStatusErr = errors.New("invalid status value")

				var c *gin.Context
				var w *httptest.ResponseRecorder
				if handler == "intake" {
					c, w = CreateTestGinContextWithBody("PATCH", fmt.Sprintf("/intake/survey_responses/%s", id), "application/json-patch+json", []byte(tc.patch))
					TestUsers.Owner.SetContext(c)
					(&Server{}).PatchIntakeSurveyResponse(c, id, PatchIntakeSurveyResponseParams{})
				} else {
					c, w = CreateTestGinContextWithBody("PATCH", fmt.Sprintf("/triage/survey_responses/%s", id), "application/json-patch+json", []byte(tc.patch))
					asTriageUser(t, c, GroupSecurityReviewers)
					(&Server{}).PatchTriageSurveyResponse(c, id)
				}
				assert.Equal(t, tc.want, w.Code, w.Body.String())
				if !tc.writes {
					assert.Equal(t, initial, *store.responses[id].Status, "status must be unchanged")
					assert.Equal(t, "not-a-status", *store.responses[id].RevisionNotes, "revision_notes must be unchanged")
				}
			})
		}
	}
}

func TestCheckOwnershipChanges_MoveAndCopy(t *testing.T) {
	owner, auth := CheckOwnershipChanges([]PatchOperation{{Op: "move", Path: "/owner", From: "/name"}})
	assert.True(t, owner)
	assert.False(t, auth)
	owner, auth = CheckOwnershipChanges([]PatchOperation{{Op: "move", Path: "/name", From: "/owner"}})
	assert.True(t, owner)
	assert.False(t, auth)
	owner, auth = CheckOwnershipChanges([]PatchOperation{{Op: "copy", Path: "/authorization/0", From: "/name"}})
	assert.False(t, owner)
	assert.True(t, auth)
	owner, auth = CheckOwnershipChanges([]PatchOperation{{Op: "move", Path: "/name", From: "/authorization"}})
	assert.False(t, owner)
	assert.True(t, auth)
	owner, _ = CheckOwnershipChanges([]PatchOperation{{Op: "replace", Path: "/owner/provider_id", Value: "x"}})
	assert.True(t, owner)
	owner, _ = CheckOwnershipChanges([]PatchOperation{{Op: "replace", Path: "/owners", Value: "x"}})
	assert.False(t, owner)
	// copy only reads its source
	owner, auth = CheckOwnershipChanges([]PatchOperation{{Op: "copy", Path: "/name", From: "/owner"}})
	assert.False(t, owner)
	assert.False(t, auth)
}
