package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: test that collaboration sessions list every authorized user as a participant (#1046)
func TestCollaborationSessionParticipantsIncludeAuthorizedUsers(t *testing.T) {
	InitializeMockStores()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "alice@example.com")
		c.Set("userID", "alice-provider-id")
		c.Set("userProvider", "test")
		c.Set("userIdP", "test")
		c.Set("userDisplayName", "Alice")
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
		assert.Len(t, cs.Participants, 3, "groups are skipped; participants: %v", perms)
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

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: test participant dedupe, email fallback and display name defaults when building session participants (#1046)
func TestBuildSessionParticipantsEdgeCases(t *testing.T) {
	owner := User{PrincipalType: UserPrincipalTypeUser, Provider: "test", ProviderId: "alice-id", Email: "alice@example.com", DisplayName: "Alice"}
	email := func(s string) *openapi_types.Email { e := openapi_types.Email(s); return &e }
	tm := &ThreatModel{
		Owner: owner,
		Authorization: &[]Authorization{
			{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "test", ProviderId: "alice-id", Email: email("alice@example.com"), Role: AuthorizationRoleOwner},
			{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "test", ProviderId: "Dave <dave@example.com>", Role: AuthorizationRoleReader},
			{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "test", ProviderId: "opaque-sub-123", Role: AuthorizationRoleReader},
			{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "test", ProviderId: "erin-id", Email: email("erin@example.com"), Role: AuthorizationRoleWriter},
		},
	}
	session := &DiagramSession{ID: uuid.New().String(), Clients: map[*WebSocketClient]bool{}}

	got := buildSessionParticipants(nil, session, tm, ResolvedUser{})

	byEmail := map[string]Participant{}
	for _, p := range got {
		byEmail[string(p.User.Email)] = p
		assert.NotEmpty(t, p.User.DisplayName, "display_name must be non-empty")
	}
	assert.Len(t, got, 3, "owner deduped, opaque provider_id omitted")
	assert.Equal(t, ParticipantPermissionsWriter, byEmail["alice@example.com"].Permissions)
	assert.Equal(t, "Alice", byEmail["alice@example.com"].User.DisplayName)
	assert.Equal(t, ParticipantPermissionsReader, byEmail["dave@example.com"].Permissions, "provider_id parsed as email")
	assert.Equal(t, "dave@example.com", byEmail["dave@example.com"].User.DisplayName, "display_name falls back to email")
	assert.Equal(t, ParticipantPermissionsWriter, byEmail["erin@example.com"].Permissions)
}
