package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// fakeSpecAutomation covers the operations annotated by slice 5 (#368) for
// the automation role gate.
const fakeSpecAutomation = `{
  "openapi": "3.0.3",
  "info": {"title": "test", "version": "0"},
  "paths": {
    "/automation/embeddings/{threat_model_id}": {
      "post": {
        "operationId": "ingestEmbeddings",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none", "roles": ["automation"]}
      },
      "delete": {
        "operationId": "deleteEmbeddings",
        "responses": {"204": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none", "roles": ["automation"]}
      }
    }
  }
}`

// mockAutomationGroupRepo is a minimal GroupMemberRepository that returns
// IsEffectiveMember=true only for users explicitly registered as automation
// members via addAutomation. Every other method is a no-op zero-value return
// — the role gate only consults IsEffectiveMember.
type mockAutomationGroupRepo struct {
	tmiAutoUsers map[uuid.UUID]bool
	embAutoUsers map[uuid.UUID]bool
}

func newMockAutomationGroupRepo() *mockAutomationGroupRepo {
	return &mockAutomationGroupRepo{
		tmiAutoUsers: make(map[uuid.UUID]bool),
		embAutoUsers: make(map[uuid.UUID]bool),
	}
}

func (m *mockAutomationGroupRepo) addTMIAuto(userUUID uuid.UUID) { m.tmiAutoUsers[userUUID] = true }
func (m *mockAutomationGroupRepo) addEmbeddingAuto(userUUID uuid.UUID) {
	m.embAutoUsers[userUUID] = true
}

func (m *mockAutomationGroupRepo) ListMembers(_ context.Context, _ GroupMemberFilter) ([]GroupMember, error) {
	return nil, nil
}
func (m *mockAutomationGroupRepo) CountMembers(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}
func (m *mockAutomationGroupRepo) AddMember(_ context.Context, _, _ uuid.UUID, _ *uuid.UUID, _ *string) (*GroupMember, error) {
	return nil, nil
}
func (m *mockAutomationGroupRepo) RemoveMember(_ context.Context, _, _ uuid.UUID) error { return nil }
func (m *mockAutomationGroupRepo) IsMember(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return false, nil
}
func (m *mockAutomationGroupRepo) AddGroupMember(_ context.Context, _, _ uuid.UUID, _ *uuid.UUID, _ *string) (*GroupMember, error) {
	return nil, nil
}
func (m *mockAutomationGroupRepo) RemoveGroupMember(_ context.Context, _, _ uuid.UUID) error {
	return nil
}
func (m *mockAutomationGroupRepo) IsEffectiveMember(_ context.Context, groupUUID, userUUID uuid.UUID, _ []uuid.UUID) (bool, error) {
	switch groupUUID {
	case GroupTMIAutomation.UUID:
		return m.tmiAutoUsers[userUUID], nil
	case GroupEmbeddingAutomation.UUID:
		return m.embAutoUsers[userUUID], nil
	}
	return false, nil
}
func (m *mockAutomationGroupRepo) HasAnyMembers(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}
func (m *mockAutomationGroupRepo) GetGroupsForUser(_ context.Context, _ uuid.UUID) ([]Group, error) {
	return nil, nil
}

// newAutomationAuthzRouter wires AuthzMiddleware against fakeSpecAutomation
// with a mocked GlobalGroupMemberRepository. The closure-style construction
// lets each test set the UUID of the "current user" so the membership check
// resolves deterministically.
func newAutomationAuthzRouter(t *testing.T, userUUID uuid.UUID, repo *mockAutomationGroupRepo, authenticated bool) *gin.Engine {
	t.Helper()
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecAutomation))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if authenticated {
			c.Set("userEmail", "test@example.com")
			c.Set("userInternalUUID", userUUID.String())
			c.Set("userIdP", "test")
			c.Set("userProvider", "test")
		}
		c.Next()
	})
	r.Use(authzMiddlewareWithTable(tbl))
	r.POST("/automation/embeddings/:threat_model_id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"path": c.Request.URL.Path})
	})
	r.DELETE("/automation/embeddings/:threat_model_id", func(c *gin.Context) {
		c.JSON(http.StatusNoContent, gin.H{})
	})
	return r
}

func TestAuthzMiddleware_AutomationRole_AllowsTMIAutomationMember(t *testing.T) {
	originalRepo := GlobalGroupMemberRepository
	defer func() { GlobalGroupMemberRepository = originalRepo }()

	userUUID := uuid.New()
	repo := newMockAutomationGroupRepo()
	repo.addTMIAuto(userUUID)
	GlobalGroupMemberRepository = repo

	r := newAutomationAuthzRouter(t, userUUID, repo, true)
	req := httptest.NewRequest(http.MethodPost,
		"/automation/embeddings/00000000-0000-0000-0000-000000000001", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_AutomationRole_AllowsEmbeddingAutomationMember(t *testing.T) {
	originalRepo := GlobalGroupMemberRepository
	defer func() { GlobalGroupMemberRepository = originalRepo }()

	userUUID := uuid.New()
	repo := newMockAutomationGroupRepo()
	repo.addEmbeddingAuto(userUUID)
	GlobalGroupMemberRepository = repo

	r := newAutomationAuthzRouter(t, userUUID, repo, true)
	req := httptest.NewRequest(http.MethodDelete,
		"/automation/embeddings/00000000-0000-0000-0000-000000000001", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want 204; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_AutomationRole_RejectsNonAutomationUser(t *testing.T) {
	originalRepo := GlobalGroupMemberRepository
	defer func() { GlobalGroupMemberRepository = originalRepo }()

	userUUID := uuid.New()
	repo := newMockAutomationGroupRepo() // no automation membership added
	GlobalGroupMemberRepository = repo

	r := newAutomationAuthzRouter(t, userUUID, repo, true)
	req := httptest.NewRequest(http.MethodPost,
		"/automation/embeddings/00000000-0000-0000-0000-000000000001", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_AutomationRole_RejectsAnonymous(t *testing.T) {
	originalRepo := GlobalGroupMemberRepository
	defer func() { GlobalGroupMemberRepository = originalRepo }()

	repo := newMockAutomationGroupRepo()
	GlobalGroupMemberRepository = repo

	// authenticated=false => no userEmail/userInternalUUID set; ResolveMembershipContext
	// fails and checkAutomationRole returns false → 403 from the OR-list reducer.
	r := newAutomationAuthzRouter(t, uuid.New(), repo, false)
	req := httptest.NewRequest(http.MethodPost,
		"/automation/embeddings/00000000-0000-0000-0000-000000000001", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403 for anonymous; body=%s", w.Code, w.Body.String())
	}
}
