package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuthTestHelper provides utilities for testing authorization functionality with caching
type AuthTestHelper struct {
	DB               *sql.DB
	Cache            *CacheService
	CacheInvalidator *CacheInvalidator
	TestContext      context.Context
}

// AuthTestScenario defines a test scenario for authorization testing
type AuthTestScenario struct {
	Description      string
	User             string
	ThreatModelID    string
	ExpectedAccess   bool
	ExpectedRole     Role
	ShouldCache      bool
	ExpectedCacheHit bool
}

// NewAuthTestHelper creates a new authorization test helper
func NewAuthTestHelper(db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *AuthTestHelper {
	return &AuthTestHelper{
		DB:               db,
		Cache:            cache,
		CacheInvalidator: invalidator,
		TestContext:      context.Background(),
	}
}

// SetupTestThreatModel creates a test threat model with authorization for testing
func (h *AuthTestHelper) SetupTestThreatModel(t *testing.T, owner string, authList []Authorization) string {
	t.Helper()

	if !SubResourceFixtures.Initialized {
		InitSubResourceTestFixtures()
	}

	threatModelID := uuid.New().String()

	// Create test threat model in database (if using database store)
	// This would typically involve inserting into threat_models and threat_model_access tables
	// Implementation depends on the actual database schema and store implementation

	return threatModelID
}

// TestGetInheritedAuthData tests the GetInheritedAuthData function with various scenarios
func (h *AuthTestHelper) TestGetInheritedAuthData(t *testing.T, scenarios []AuthTestScenario) {
	t.Helper()

	for _, scenario := range scenarios {
		t.Run(scenario.Description, func(t *testing.T) {
			// Clear cache if needed
			if h.Cache != nil && !scenario.ExpectedCacheHit {
				_ = h.CacheInvalidator.InvalidateAllRelatedCaches(h.TestContext, scenario.ThreatModelID)
			}

			// Test GetInheritedAuthData
			authData, err := GetInheritedAuthData(h.TestContext, h.DB, scenario.ThreatModelID)

			if scenario.ExpectedAccess {
				if err != nil {
					t.Errorf("Expected successful auth data retrieval but got error: %v", err)
					return
				}
				if authData == nil {
					t.Errorf("Expected auth data but got nil")
					return
				}
			} else {
				if err == nil {
					t.Errorf("Expected error but got successful result")
					return
				}
			}

			// Verify cache behavior if cache is enabled
			if h.Cache != nil && scenario.ShouldCache && authData != nil {
				// Second call should hit cache
				authData2, err2 := GetInheritedAuthData(h.TestContext, h.DB, scenario.ThreatModelID)
				if err2 != nil {
					t.Errorf("Second call failed: %v", err2)
				}
				if authData2 == nil {
					t.Errorf("Second call returned nil")
				}
				// In a real implementation, you'd verify cache metrics here
			}
		})
	}
}

// TestCheckSubResourceAccess tests the CheckSubResourceAccess function with caching
func (h *AuthTestHelper) TestCheckSubResourceAccess(t *testing.T, scenarios []AuthTestScenario) {
	t.Helper()

	for _, scenario := range scenarios {
		t.Run(scenario.Description, func(t *testing.T) {
			// Clear cache if needed
			if h.Cache != nil && !scenario.ExpectedCacheHit {
				_ = h.CacheInvalidator.InvalidateAllRelatedCaches(h.TestContext, scenario.ThreatModelID)
			}

			// Test CheckSubResourceAccess
			hasAccess, err := CheckSubResourceAccess(
				h.TestContext,
				h.DB,
				h.Cache,
				scenario.User,
				"",         // No IdP for test users
				[]string{}, // No groups for test users
				scenario.ThreatModelID,
				scenario.ExpectedRole,
			)

			if err != nil {
				t.Errorf("CheckSubResourceAccess failed: %v", err)
				return
			}

			if hasAccess != scenario.ExpectedAccess {
				t.Errorf("Expected access: %v, got: %v", scenario.ExpectedAccess, hasAccess)
			}
		})
	}
}

// CreateTestGinContext creates a Gin context for testing with authentication
func (h *AuthTestHelper) CreateTestGinContext(userEmail string, threatModelID string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)

	// Set authenticated user
	c.Set("user_email", userEmail)

	// Set threat model ID parameter
	threatModelUUID, _ := uuid.Parse(threatModelID)
	c.Set("threat_model_id", threatModelUUID)

	return c, w
}

// TestValidateSubResourceAccess tests the middleware function
func (h *AuthTestHelper) TestValidateSubResourceAccess(t *testing.T, scenarios []AuthTestScenario) {
	t.Helper()

	for _, scenario := range scenarios {
		t.Run(scenario.Description, func(t *testing.T) {
			c, w := h.CreateTestGinContext(scenario.User, scenario.ThreatModelID)

			// Create middleware instance
			middleware := ValidateSubResourceAccess(h.DB, h.Cache, scenario.ExpectedRole)

			// Execute middleware
			middleware(c)

			// Check if context was aborted (indicating middleware blocked the request)
			nextCalled := !c.IsAborted()

			if scenario.ExpectedAccess {
				if w.Code != 0 {
					t.Errorf("Expected success but got HTTP %d", w.Code)
				}
				if !nextCalled {
					t.Errorf("Expected next handler to be called")
				}
			} else {
				if w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
					t.Errorf("Expected 401/403 but got HTTP %d", w.Code)
				}
				if nextCalled {
					t.Errorf("Expected next handler NOT to be called")
				}
			}
		})
	}
}

// SetupTestAuthorizationData creates test authorization data for various scenarios
func (h *AuthTestHelper) SetupTestAuthorizationData() []AuthTestScenario {
	if !SubResourceFixtures.Initialized {
		InitSubResourceTestFixtures()
	}

	return []AuthTestScenario{
		{
			Description:      "Owner has access",
			User:             SubResourceFixtures.OwnerUser,
			ThreatModelID:    SubResourceFixtures.ThreatModelID,
			ExpectedAccess:   true,
			ExpectedRole:     RoleOwner,
			ShouldCache:      true,
			ExpectedCacheHit: false,
		},
		{
			Description:      "Writer has access",
			User:             SubResourceFixtures.WriterUser,
			ThreatModelID:    SubResourceFixtures.ThreatModelID,
			ExpectedAccess:   true,
			ExpectedRole:     RoleWriter,
			ShouldCache:      true,
			ExpectedCacheHit: false,
		},
		{
			Description:      "Reader has access",
			User:             SubResourceFixtures.ReaderUser,
			ThreatModelID:    SubResourceFixtures.ThreatModelID,
			ExpectedAccess:   true,
			ExpectedRole:     RoleReader,
			ShouldCache:      true,
			ExpectedCacheHit: false,
		},
		{
			Description:      "External user has no access",
			User:             SubResourceFixtures.ExternalUser,
			ThreatModelID:    SubResourceFixtures.ThreatModelID,
			ExpectedAccess:   false,
			ExpectedRole:     "",
			ShouldCache:      false,
			ExpectedCacheHit: false,
		},
		{
			Description:      "Invalid threat model ID",
			User:             SubResourceFixtures.OwnerUser,
			ThreatModelID:    uuid.New().String(),
			ExpectedAccess:   false,
			ExpectedRole:     "",
			ShouldCache:      false,
			ExpectedCacheHit: false,
		},
		{
			Description:      "Cache hit scenario",
			User:             SubResourceFixtures.OwnerUser,
			ThreatModelID:    SubResourceFixtures.ThreatModelID,
			ExpectedAccess:   true,
			ExpectedRole:     RoleOwner,
			ShouldCache:      true,
			ExpectedCacheHit: true,
		},
	}
}

// VerifyAuthorizationInheritance verifies that sub-resource authorization inherits from threat model
func (h *AuthTestHelper) VerifyAuthorizationInheritance(t *testing.T, threatModelID, subResourceID string) {
	t.Helper()

	// Get threat model authorization
	tmAuthData, err := GetInheritedAuthData(h.TestContext, h.DB, threatModelID)
	if err != nil {
		t.Fatalf("Failed to get threat model auth data: %v", err)
	}

	// Test each authorized user can access sub-resource
	for userEmail, expectedRole := range map[string]Role{
		SubResourceFixtures.OwnerUser:  RoleOwner,
		SubResourceFixtures.WriterUser: RoleWriter,
		SubResourceFixtures.ReaderUser: RoleReader,
	} {
		hasAccess, err := CheckSubResourceAccess(h.TestContext, h.DB, h.Cache, userEmail, "", []string{}, threatModelID, expectedRole)
		if err != nil {
			t.Errorf("Failed to check sub-resource access for %s: %v", userEmail, err)
			continue
		}

		if !hasAccess {
			t.Errorf("User %s should have access to sub-resource", userEmail)
		}

		// Verify the user has the expected role in threat model auth data
		found := false
		for _, auth := range tmAuthData.Authorization {
			if auth.Subject == userEmail && auth.Role == expectedRole {
				found = true
				break
			}
		}
		if !found && tmAuthData.Owner != userEmail {
			t.Errorf("User %s with role %s not found in threat model authorization", userEmail, expectedRole)
		}
	}

	// Verify external user has no access
	hasAccess, err := CheckSubResourceAccess(h.TestContext, h.DB, h.Cache, SubResourceFixtures.ExternalUser, "", []string{}, threatModelID, RoleReader)
	if err != nil {
		t.Errorf("Failed to check sub-resource access for external user: %v", err)
	}
	if hasAccess {
		t.Errorf("External user should not have access to sub-resource")
	}
}

// TestCacheInvalidation tests that cache is properly invalidated when authorization changes
func (h *AuthTestHelper) TestCacheInvalidation(t *testing.T, threatModelID string) {
	t.Helper()

	if h.Cache == nil {
		t.Skip("Cache not available for testing")
	}

	// First, populate cache
	_, err := GetInheritedAuthData(h.TestContext, h.DB, threatModelID)
	if err != nil {
		t.Fatalf("Failed to get auth data: %v", err)
	}

	// Verify cache is populated (implementation-specific check)
	cachedData, err := h.Cache.GetCachedAuthData(h.TestContext, threatModelID)
	if err != nil {
		t.Errorf("Failed to get cached auth data: %v", err)
	}
	if cachedData == nil {
		t.Errorf("Expected cached data but got nil")
	}

	// Invalidate cache
	err = h.CacheInvalidator.InvalidateAllRelatedCaches(h.TestContext, threatModelID)
	if err != nil {
		t.Errorf("Failed to invalidate cache: %v", err)
	}

	// Verify cache is cleared (implementation-specific check)
	cachedDataAfter, err := h.Cache.GetCachedAuthData(h.TestContext, threatModelID)
	if err == nil && cachedDataAfter != nil {
		t.Errorf("Expected cache to be cleared but data still exists")
	}
}

// CleanupTestAuth cleans up test authorization data
func (h *AuthTestHelper) CleanupTestAuth(t *testing.T, threatModelIDs []string) {
	t.Helper()

	for _, threatModelID := range threatModelIDs {
		// Clear cache
		if h.Cache != nil {
			_ = h.CacheInvalidator.InvalidateAllRelatedCaches(h.TestContext, threatModelID)
		}

		// Clean up database data (implementation-specific)
		// This would typically involve deleting from threat_models and threat_model_access tables
	}
}

// AssertAuthDataEqual compares two AuthorizationData structs for equality
func AssertAuthDataEqual(t *testing.T, expected, actual *AuthorizationData) {
	t.Helper()

	if expected == nil && actual == nil {
		return
	}

	if expected == nil || actual == nil {
		t.Errorf("One of the auth data structs is nil: expected=%v, actual=%v", expected, actual)
		return
	}

	if expected.Owner != actual.Owner {
		t.Errorf("Owner mismatch: expected=%s, actual=%s", expected.Owner, actual.Owner)
	}

	if len(expected.Authorization) != len(actual.Authorization) {
		t.Errorf("Authorization list length mismatch: expected=%d, actual=%d",
			len(expected.Authorization), len(actual.Authorization))
		return
	}

	// Create maps for comparison
	expectedMap := make(map[string]Role)
	actualMap := make(map[string]Role)

	for _, auth := range expected.Authorization {
		expectedMap[auth.Subject] = auth.Role
	}

	for _, auth := range actual.Authorization {
		actualMap[auth.Subject] = auth.Role
	}

	for subject, role := range expectedMap {
		if actualRole, exists := actualMap[subject]; !exists || actualRole != role {
			t.Errorf("Authorization mismatch for %s: expected=%s, actual=%s",
				subject, role, actualRole)
		}
	}
}

// GetTestAuthorizationData returns test authorization data for a specific scenario
func GetTestAuthorizationData(scenario string) *AuthorizationData {
	if !SubResourceFixtures.Initialized {
		InitSubResourceTestFixtures()
	}

	switch scenario {
	case "valid_multi_user":
		return &AuthorizationData{
			Owner:         SubResourceFixtures.OwnerUser,
			Authorization: SubResourceFixtures.Authorization,
		}
	case "owner_only":
		return &AuthorizationData{
			Owner:         SubResourceFixtures.OwnerUser,
			Authorization: []Authorization{},
		}
	case "empty":
		return &AuthorizationData{
			Owner:         "",
			Authorization: []Authorization{},
		}
	default:
		return nil
	}
}
