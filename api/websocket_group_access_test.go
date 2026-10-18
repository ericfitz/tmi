package api

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createGroupACLThreatModel stores a threat model whose only non-owner grants
// are group grants (no direct user grant for the test users), and returns it.
func createGroupACLThreatModel(t *testing.T, diagramID string) ThreatModel {
	t.Helper()
	InitTestFixtures()

	ownerEmail := "owner@example.com"
	diagUUID := uuid.MustParse(diagramID)
	diagrams := []DfdDiagram{{Id: &diagUUID, Name: "d"}}

	now := time.Now().UTC()
	tm := ThreatModel{
		Name: "Group ACL TM",
		Owner: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "test-idp",
			ProviderId:    ownerEmail,
			Email:         openapi_types.Email(ownerEmail),
		},
		Authorization: &[]Authorization{
			{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "test-idp", ProviderId: ownerEmail, Role: "owner"},
			{PrincipalType: AuthorizationPrincipalTypeGroup, Provider: "test-idp", ProviderId: "writers-group", Role: "writer"},
			{PrincipalType: AuthorizationPrincipalTypeGroup, Provider: "test-idp", ProviderId: "readers-group", Role: "reader"},
		},
		Diagrams:   &diagrams,
		CreatedAt:  &now,
		ModifiedAt: &now,
	}
	created, err := ThreatModelStore.Create(context.Background(), tm, func(tm ThreatModel, id string) ThreatModel {
		uid, _ := ParseUUID(id)
		tm.Id = &uid
		return tm
	})
	require.NoError(t, err)
	return created
}

// #1107: WebSocket authorization must honor group-based grants like REST does.
func TestWebSocketGroupBasedAccess(t *testing.T) {
	diagramID := uuid.New().String()
	tm := createGroupACLThreatModel(t, diagramID)
	tmID := tm.Id.String()

	session, _ := createTestSession(t, tmID)

	newClient := func(id string, groups []string) *WebSocketClient {
		c := addClientToSession(t, session, id, id, id, "test-idp")
		c.Groups = groups
		return c
	}

	writer := newClient("gw@example.com", []string{"writers-group"})
	reader := newClient("gr@example.com", []string{"readers-group"})
	nogroup := newClient("ng@example.com", nil)
	other := newClient("og@example.com", []string{"unrelated-group"})

	t.Run("mutation", func(t *testing.T) {
		assert.True(t, session.checkMutationPermission(writer), "group-only writer can mutate")
		assert.False(t, session.checkMutationPermission(reader), "group-only reader cannot mutate")
		assert.False(t, session.checkMutationPermission(nogroup), "user without groups is denied")
		assert.False(t, session.checkMutationPermission(other), "user in an unrelated group is denied")
	})

	t.Run("heartbeat_read", func(t *testing.T) {
		assert.True(t, session.checkReadPermission(writer), "group-only writer retains read")
		assert.True(t, session.checkReadPermission(reader), "group-only reader retains read")
		assert.False(t, session.checkReadPermission(nogroup), "user without groups is denied")
		assert.False(t, session.checkReadPermission(other), "user in an unrelated group is denied")
	})

	t.Run("connect_time", func(t *testing.T) {
		hub := NewWebSocketHubForTests()
		info := func(id string, groups []string) *UserInfo {
			return &UserInfo{UserID: id, UserEmail: id, UserProvider: "test-idp", Groups: groups}
		}
		assert.True(t, hub.validateWebSocketDiagramAccessWithFlexibleMatching(info("gr@example.com", []string{"readers-group"}), tmID, diagramID))
		assert.True(t, hub.validateWebSocketDiagramAccessWithFlexibleMatching(info("gw@example.com", []string{"writers-group"}), tmID, diagramID))
		assert.False(t, hub.validateWebSocketDiagramAccessWithFlexibleMatching(info("ng@example.com", nil), tmID, diagramID))
		assert.False(t, hub.validateWebSocketDiagramAccessWithFlexibleMatching(info("og@example.com", []string{"unrelated-group"}), tmID, diagramID))
	})

	t.Run("presenter_reassignment", func(t *testing.T) {
		// Host has left; the only remaining clients are a group-only reader and
		// a group-only writer, so the writer must become presenter.
		s, host := createTestSession(t, tmID)
		s.mu.Lock()
		delete(s.Clients, host)
		s.mu.Unlock()
		r := addClientToSession(t, s, "gr2@example.com", "gr2@example.com", "r", "test-idp")
		r.Groups = []string{"readers-group"}
		w := addClientToSession(t, s, "gw2@example.com", "gw2@example.com", "w", "test-idp")
		w.Groups = []string{"writers-group"}

		s.handlePresenterDisconnection("someone-else")

		require.NotNil(t, s.CurrentPresenter)
		assert.Equal(t, "gw2@example.com", s.CurrentPresenter.ProviderID)
	})
}

// #1107: UserInfo extraction carries the userGroups context value.
func TestExtractUserInfo_Groups(t *testing.T) {
	ex := &UserInfoExtractor{}

	c, _ := CreateTestGinContext("GET", "/ws/diagrams/x")
	SetFullUserContext(c, "u@example.com", "u-id", "", "test-idp", []string{"g1"})
	info, err := ex.ExtractUserInfo(c)
	require.NoError(t, err)
	assert.Equal(t, []string{"g1"}, info.Groups)

	c2, _ := CreateTestGinContext("GET", "/ws/diagrams/x")
	SetFullUserContext(c2, "u@example.com", "u-id", "", "test-idp", nil)
	info, err = ex.ExtractUserInfo(c2)
	require.NoError(t, err)
	assert.Empty(t, info.Groups)
}
