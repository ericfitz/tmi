package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// mockAutomationGroupMemberStore is a test mock for GroupMemberRepository that supports
// per-group membership results for automation middleware tests.
type mockAutomationGroupMemberStore struct {
	memberOf map[uuid.UUID]bool
	err      error
}

func (m *mockAutomationGroupMemberStore) IsEffectiveMember(_ context.Context, groupUUID uuid.UUID, _ uuid.UUID, _ []uuid.UUID) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.memberOf[groupUUID], nil
}

func (m *mockAutomationGroupMemberStore) ListMembers(_ context.Context, _ GroupMemberFilter) ([]GroupMember, error) {
	return nil, nil
}

func (m *mockAutomationGroupMemberStore) CountMembers(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}

func (m *mockAutomationGroupMemberStore) AddMember(_ context.Context, _, _ uuid.UUID, _ *uuid.UUID, _ *string) (*GroupMember, error) {
	return nil, nil
}

func (m *mockAutomationGroupMemberStore) RemoveMember(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

func (m *mockAutomationGroupMemberStore) IsMember(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (m *mockAutomationGroupMemberStore) AddGroupMember(_ context.Context, _, _ uuid.UUID, _ *uuid.UUID, _ *string) (*GroupMember, error) {
	return nil, nil
}

func (m *mockAutomationGroupMemberStore) RemoveGroupMember(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

func (m *mockAutomationGroupMemberStore) HasAnyMembers(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (m *mockAutomationGroupMemberStore) GetGroupsForUser(_ context.Context, _ uuid.UUID) ([]Group, error) {
	return nil, nil
}

// newAutomationMemberStore creates a mock store configured with per-group membership.
func newAutomationMemberStore(isTMIAutomation, isEmbeddingAutomation bool) *mockAutomationGroupMemberStore {
	return &mockAutomationGroupMemberStore{
		memberOf: map[uuid.UUID]bool{
			GroupTMIAutomation.UUID:       isTMIAutomation,
			GroupEmbeddingAutomation.UUID: isEmbeddingAutomation,
		},
	}
}

// setAutomationTestContext sets the minimum context keys required by ResolveMembershipContext.
func setAutomationTestContext(c *gin.Context) {
	c.Set("userEmail", "bot@example.com")
	c.Set("userInternalUUID", uuid.New().String())
	c.Set("userProvider", "tmi")
}

// --- AutomationMiddleware tests ---

func TestAutomationMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalStore := GlobalGroupMemberRepository
	defer func() { GlobalGroupMemberRepository = originalStore }()

	t.Run("returns 401 when no userEmail in context", func(t *testing.T) {
		GlobalGroupMemberRepository = newAutomationMemberStore(false, false)

		router := gin.New()
		router.Use(AutomationMiddleware())
		router.GET("/automation/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("returns 401 when userEmail is empty", func(t *testing.T) {
		GlobalGroupMemberRepository = newAutomationMemberStore(false, false)

		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("userEmail", "")
			c.Next()
		})
		router.Use(AutomationMiddleware())
		router.GET("/automation/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("returns 401 when userProvider is missing", func(t *testing.T) {
		GlobalGroupMemberRepository = newAutomationMemberStore(false, false)

		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("userEmail", "bot@example.com")
			c.Set("userInternalUUID", uuid.New().String())
			// missing userProvider
			c.Next()
		})
		router.Use(AutomationMiddleware())
		router.GET("/automation/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("returns 403 when user is not in any automation group", func(t *testing.T) {
		GlobalGroupMemberRepository = newAutomationMemberStore(false, false)

		router := gin.New()
		router.Use(func(c *gin.Context) {
			setAutomationTestContext(c)
			c.Next()
		})
		router.Use(AutomationMiddleware())
		router.GET("/automation/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "automation group membership required")
	})

	t.Run("allows access for tmi-automation member", func(t *testing.T) {
		GlobalGroupMemberRepository = newAutomationMemberStore(true, false)

		router := gin.New()
		router.Use(func(c *gin.Context) {
			setAutomationTestContext(c)
			c.Next()
		})
		router.Use(AutomationMiddleware())
		router.GET("/automation/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("allows access for embedding-automation member", func(t *testing.T) {
		GlobalGroupMemberRepository = newAutomationMemberStore(false, true)

		router := gin.New()
		router.Use(func(c *gin.Context) {
			setAutomationTestContext(c)
			c.Next()
		})
		router.Use(AutomationMiddleware())
		router.GET("/automation/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("allows access for member of both automation groups", func(t *testing.T) {
		GlobalGroupMemberRepository = newAutomationMemberStore(true, true)

		router := gin.New()
		router.Use(func(c *gin.Context) {
			setAutomationTestContext(c)
			c.Next()
		})
		router.Use(AutomationMiddleware())
		router.GET("/automation/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("returns 500 when store returns error", func(t *testing.T) {
		GlobalGroupMemberRepository = &mockAutomationGroupMemberStore{
			memberOf: map[uuid.UUID]bool{},
			err:      assert.AnError,
		}

		router := gin.New()
		router.Use(func(c *gin.Context) {
			setAutomationTestContext(c)
			c.Next()
		})
		router.Use(AutomationMiddleware())
		router.GET("/automation/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("returns 500 when store is nil", func(t *testing.T) {
		GlobalGroupMemberRepository = nil

		router := gin.New()
		router.Use(func(c *gin.Context) {
			setAutomationTestContext(c)
			c.Next()
		})
		router.Use(AutomationMiddleware())
		router.GET("/automation/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// --- EmbeddingAutomationMiddleware tests ---

func TestEmbeddingAutomationMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalStore := GlobalGroupMemberRepository
	defer func() { GlobalGroupMemberRepository = originalStore }()

	t.Run("returns 401 when no userEmail in context", func(t *testing.T) {
		GlobalGroupMemberRepository = newAutomationMemberStore(false, false)

		router := gin.New()
		router.Use(EmbeddingAutomationMiddleware())
		router.GET("/automation/embeddings/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/embeddings/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("returns 401 when userProvider is missing", func(t *testing.T) {
		GlobalGroupMemberRepository = newAutomationMemberStore(false, true)

		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("userEmail", "bot@example.com")
			c.Set("userInternalUUID", uuid.New().String())
			// missing userProvider
			c.Next()
		})
		router.Use(EmbeddingAutomationMiddleware())
		router.GET("/automation/embeddings/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/embeddings/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("returns 403 for tmi-automation-only member", func(t *testing.T) {
		GlobalGroupMemberRepository = newAutomationMemberStore(true, false)

		router := gin.New()
		router.Use(func(c *gin.Context) {
			setAutomationTestContext(c)
			c.Next()
		})
		router.Use(EmbeddingAutomationMiddleware())
		router.GET("/automation/embeddings/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/embeddings/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "automation group membership required")
	})

	t.Run("returns 403 when user is not in any automation group", func(t *testing.T) {
		GlobalGroupMemberRepository = newAutomationMemberStore(false, false)

		router := gin.New()
		router.Use(func(c *gin.Context) {
			setAutomationTestContext(c)
			c.Next()
		})
		router.Use(EmbeddingAutomationMiddleware())
		router.GET("/automation/embeddings/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/embeddings/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "automation group membership required")
	})

	t.Run("allows access for embedding-automation member", func(t *testing.T) {
		GlobalGroupMemberRepository = newAutomationMemberStore(false, true)

		router := gin.New()
		router.Use(func(c *gin.Context) {
			setAutomationTestContext(c)
			c.Next()
		})
		router.Use(EmbeddingAutomationMiddleware())
		router.GET("/automation/embeddings/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/embeddings/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("allows access for member of both automation groups", func(t *testing.T) {
		GlobalGroupMemberRepository = newAutomationMemberStore(true, true)

		router := gin.New()
		router.Use(func(c *gin.Context) {
			setAutomationTestContext(c)
			c.Next()
		})
		router.Use(EmbeddingAutomationMiddleware())
		router.GET("/automation/embeddings/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/embeddings/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("returns 500 when store returns error", func(t *testing.T) {
		GlobalGroupMemberRepository = &mockAutomationGroupMemberStore{
			memberOf: map[uuid.UUID]bool{},
			err:      assert.AnError,
		}

		router := gin.New()
		router.Use(func(c *gin.Context) {
			setAutomationTestContext(c)
			c.Next()
		})
		router.Use(EmbeddingAutomationMiddleware())
		router.GET("/automation/embeddings/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/embeddings/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("returns 500 when store is nil", func(t *testing.T) {
		GlobalGroupMemberRepository = nil

		router := gin.New()
		router.Use(func(c *gin.Context) {
			setAutomationTestContext(c)
			c.Next()
		})
		router.Use(EmbeddingAutomationMiddleware())
		router.GET("/automation/embeddings/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/automation/embeddings/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
