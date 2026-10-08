package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SEM@bdd626ede818b573d8e556f49930fac9f87be4f2: test that collaboration sessions list every authorized user as a participant (#1046)
func TestCollaborationSessionParticipantsIncludeAuthorizedUsers(t *testing.T) {
	InitializeMockStores()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "alice@example.com")
		c.Set("userID", "alice-provider-id")
		c.Set("userProvider", "test")
		c.Set("userIdP", "test")
		c.Set("userName", "Alice")
		c.Set("userId", "alice@example.com")
		c.Next()
	})
	r.Use(ThreatModelMiddleware())

	wsHub := NewWebSocketHubForTests()
	tmHandler := NewThreatModelHandler(wsHub)
	dh := NewThreatModelDiagramHandler(wsHub)
	r.POST("/threat_models", tmHandler.CreateThreatModel)
	r.POST("/threat_models/:threat_model_id/diagrams", func(c *gin.Context) {
		dh.CreateDiagram(c, c.Param("threat_model_id"))
	})
	r.GET("/threat_models/:threat_model_id/diagrams/:diagram_id/collaborate", func(c *gin.Context) {
		dh.GetDiagramCollaborate(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.POST("/threat_models/:threat_model_id/diagrams/:diagram_id/collaborate", func(c *gin.Context) {
		dh.CreateDiagramCollaborate(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})

	do := func(method, path string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			require.NoError(t, json.NewEncoder(&buf).Encode(body))
		}
		req := httptest.NewRequest(method, path, &buf)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := do("POST", "/threat_models", map[string]any{
		"name":                   "TM",
		"threat_model_framework": "STRIDE",
		"authorization": []map[string]any{{
			"principal_type": "user", "provider": "test",
			"provider_id": "bob-provider-id", "email": "bob@example.com", "role": "writer",
		}, {
			"principal_type": "user", "provider": "test",
			"provider_id": "carol-provider-id", "email": "carol@example.com", "role": "reader",
		}, {
			"principal_type": "group", "provider": "test",
			"provider_id": "reviewers", "role": "reader",
		}},
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var tm ThreatModel
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tm))
	tmID := tm.Id.String()

	w = do("POST", "/threat_models/"+tmID+"/diagrams", map[string]any{"name": "D", "type": "DFD-1.0.0"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var d DfdDiagram
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &d))
	collab := "/threat_models/" + tmID + "/diagrams/" + d.Id.String() + "/collaborate"

	check := func(w *httptest.ResponseRecorder) {
		var cs CollaborationSession
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cs))
		perms := map[string]ParticipantPermissions{}
		for _, p := range cs.Participants {
			perms[string(p.User.Email)] = p.Permissions
		}
		assert.Len(t, cs.Participants, 3, "groups are skipped;", "participants: %v", perms)
		assert.Equal(t, ParticipantPermissionsWriter, perms["alice@example.com"], "owner is a writer participant")
		assert.Equal(t, ParticipantPermissionsWriter, perms["bob@example.com"])
		assert.Equal(t, ParticipantPermissionsReader, perms["carol@example.com"])
	}

	w = do("POST", collab, nil)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	check(w)
	w = do("POST", collab, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	check(w)
	w = do("GET", collab, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	check(w)
}
