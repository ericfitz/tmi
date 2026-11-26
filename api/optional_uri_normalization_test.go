package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #1233: blank optional URIs normalize to absent on threat-model, team and
// project write paths; padded values are trimmed.

func TestNormalizeOptionalURI(t *testing.T) {
	assert.Nil(t, NormalizeOptionalURI(nil))
	for _, blank := range []string{"", " ", "\t\n "} {
		b := blank
		assert.Nil(t, NormalizeOptionalURI(&b), "%q", blank)
	}
	padded := "  https://example.com/x \n"
	got := NormalizeOptionalURI(&padded)
	require.NotNil(t, got)
	assert.Equal(t, "https://example.com/x", *got)
	assert.Equal(t, "  https://example.com/x \n", padded, "input must not be mutated")
}

func TestNormalizeURIPatchOperations(t *testing.T) {
	ops := []PatchOperation{
		{Op: "replace", Path: "/issue_uri", Value: "  "},
		{Op: "add", Path: "/issue_uri", Value: " https://example.com/y "},
		{Op: "replace", Path: "/name", Value: "  keep  "},
		{Op: "remove", Path: "/issue_uri"},
		{Op: "replace", Path: "/issue_uri", Value: 42},
	}
	NormalizeURIPatchOperations(ops, []string{"/issue_uri"})
	assert.Nil(t, ops[0].Value)
	assert.Equal(t, "https://example.com/y", ops[1].Value)
	assert.Equal(t, "  keep  ", ops[2].Value)
	assert.Equal(t, 42, ops[4].Value, "non-string values are left for the applier")
}

func TestProcessMappedAnswers_BlankIssueURIIsAbsent(t *testing.T) {
	field := "issue_uri"
	rows := []SurveyAnswerRow{{MapsToTmField: &field, AnswerValue: json.RawMessage(`"   "`)}}
	result := processMappedAnswers(rows)
	assert.Nil(t, result.issueURI)
}

func TestThreatModelIssueURI_BlankNormalizedOnWrite(t *testing.T) {
	for _, blank := range []string{"", "   "} {
		t.Run("blank="+blank, func(t *testing.T) {
			r := setupThreatModelRouter()
			send := func(method, url string, body any) *httptest.ResponseRecorder {
				b, _ := json.Marshal(body)
				req, _ := http.NewRequest(method, url, bytes.NewBuffer(b))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				return w
			}
			absent := func(w *httptest.ResponseRecorder, what string) {
				var m map[string]any
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m))
				v, present := m["issue_uri"]
				assert.True(t, !present || v == nil, "%s echoed issue_uri=%v", what, v)
			}

			w := send("POST", "/threat_models", map[string]any{
				"name": "TM", "threat_model_framework": "STRIDE", "issue_uri": blank,
			})
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			absent(w, "POST")
			var tm ThreatModel
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tm))
			url := "/threat_models/" + tm.Id.String()

			// Set a real URI, then clear via PUT.
			w = send("PUT", url, map[string]any{"name": "TM", "threat_model_framework": "STRIDE", "issue_uri": "https://example.com/i/1"})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			w = send("PUT", url, map[string]any{"name": "TM", "threat_model_framework": "STRIDE", "issue_uri": blank})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			absent(w, "PUT")
			w = send("GET", url, nil)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			absent(w, "GET after PUT")

			// Set again, then clear via PATCH.
			w = send("PUT", url, map[string]any{"name": "TM", "threat_model_framework": "STRIDE", "issue_uri": "https://example.com/i/2"})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			w = send("PATCH", url, []map[string]any{{"op": "replace", "path": "/issue_uri", "value": blank}})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			absent(w, "PATCH")
			w = send("GET", url, nil)
			absent(w, "GET after PATCH")
		})
	}
}

func uriFromRecorder(t *testing.T, w *httptest.ResponseRecorder) (any, bool) {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m))
	v, ok := m["uri"]
	return v, ok
}

func TestTeamUri_BlankNormalizedOnWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}
	for _, blank := range []string{"", "  "} {
		t.Run("blank="+blank, func(t *testing.T) {
			store := newMockTeamStore()
			seedTeamInStore(store, testTeamID, "Team")
			existingURI := "https://example.com/team"
			store.teams[testTeamID].Uri = &existingURI
			saveTeamProjectStores(t, store, nil)
			db := setupTestTeamAuthDB(t)
			seedTeamAuthData(t, db, testTeamID, testUserUUID)
			teamUUID, _ := uuid.Parse(testTeamID)

			body, _ := json.Marshal(map[string]any{"name": "Team", "uri": blank})
			c, w := CreateTestGinContextWithBody("POST", "/teams", "application/json", body)
			TestUsers.Owner.SetContext(c)
			server.CreateTeam(c)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			v, present := uriFromRecorder(t, w)
			assert.True(t, !present || v == nil, "create echoed uri=%v", v)

			c, w = CreateTestGinContextWithBody("PUT", "/teams/"+testTeamID, "application/json", body)
			TestUsers.Owner.SetContext(c)
			server.UpdateTeam(c, teamUUID, UpdateTeamParams{})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			v, present = uriFromRecorder(t, w)
			assert.True(t, !present || v == nil, "update echoed uri=%v", v)
			assert.Nil(t, store.teams[testTeamID].Uri, "update must hand the store nil")
			store.teams[testTeamID].Uri = &existingURI

			patch, _ := json.Marshal([]map[string]any{{"op": "replace", "path": "/uri", "value": blank}})
			c, w = CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID, "application/json-patch+json", patch)
			TestUsers.Owner.SetContext(c)
			server.PatchTeam(c, teamUUID, PatchTeamParams{})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			v, present = uriFromRecorder(t, w)
			assert.True(t, !present || v == nil, "patch echoed uri=%v", v)
			assert.Nil(t, store.teams[testTeamID].Uri, "patch must hand the store nil")
		})
	}
}

func TestProjectUri_BlankNormalizedOnWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}
	for _, blank := range []string{"", "  "} {
		t.Run("blank="+blank, func(t *testing.T) {
			teamStore := newMockTeamStore()
			teamStore.isMember = true
			projectStore := newMockProjectStore()
			seedProjectInStore(projectStore, testProjectID, "Proj", testTeamID)
			existingURI := "https://example.com/project"
			projectStore.projects[testProjectID].Uri = &existingURI
			saveTeamProjectStores(t, teamStore, projectStore)
			db := setupTestTeamAuthDB(t)
			seedTeamAuthData(t, db, testTeamID, testUserUUID)
			seedProjectAuthData(t, db, testProjectID, testTeamID)
			projectUUID, _ := uuid.Parse(testProjectID)

			body, _ := json.Marshal(map[string]any{"name": "Proj", "team_id": testTeamID, "uri": blank})
			c, w := CreateTestGinContextWithBody("POST", "/projects", "application/json", body)
			TestUsers.Owner.SetContext(c)
			server.CreateProject(c)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			v, present := uriFromRecorder(t, w)
			assert.True(t, !present || v == nil, "create echoed uri=%v", v)

			c, w = CreateTestGinContextWithBody("PUT", "/projects/"+testProjectID, "application/json", body)
			TestUsers.Owner.SetContext(c)
			server.UpdateProject(c, projectUUID, UpdateProjectParams{})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			v, present = uriFromRecorder(t, w)
			assert.True(t, !present || v == nil, "update echoed uri=%v", v)
			assert.Nil(t, projectStore.projects[testProjectID].Uri, "update must hand the store nil")
			projectStore.projects[testProjectID].Uri = &existingURI

			patch, _ := json.Marshal([]map[string]any{{"op": "replace", "path": "/uri", "value": blank}})
			c, w = CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID, "application/json-patch+json", patch)
			TestUsers.Owner.SetContext(c)
			server.PatchProject(c, projectUUID, PatchProjectParams{})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			v, present = uriFromRecorder(t, w)
			assert.True(t, !present || v == nil, "patch echoed uri=%v", v)
			assert.Nil(t, projectStore.projects[testProjectID].Uri, "patch must hand the store nil")
		})
	}
}
