package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateThreatModel validates that the UpdateThreatModel handler works correctly
func TestUpdateThreatModel(t *testing.T) {
	// Initialize test fixtures
	InitTestFixtures()
	// Reset stores to ensure clean state
	InitTestFixtures()

	// Setup Gin
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userID", TestFixtures.OwnerUser+"-provider-id") // Provider ID for testing
		// Set userRole to owner - this is needed for the handler
		c.Set("userRole", RoleOwner)
		c.Next()
	})

	// Register handler
	handler := NewThreatModelHandler(NewWebSocketHubForTests())
	router.PUT("/threat_models/:threat_model_id", handler.UpdateThreatModel)

	// Create a simplified update payload - note: we don't include 'id' as it's read-only
	updatePayload := map[string]any{
		"name":                   "Updated Name",
		"owner":                  TestFixtures.OwnerUser,
		"threat_model_framework": "STRIDE",
		"authorization": []map[string]any{
			{
				"principal_type": "user",
				"provider":       "tmi",
				"provider_id":    TestFixtures.WriterUser,
				"role":           "writer",
			},
			{
				"principal_type": "user",
				"provider":       "tmi",
				"provider_id":    TestFixtures.ReaderUser,
				"role":           "reader",
			},
		},
	}

	jsonData, err := json.Marshal(updatePayload)
	require.NoError(t, err)

	// Debug print the JSON
	t.Logf("Request JSON: %s", string(jsonData))

	// Create request
	req, _ := http.NewRequest("PUT", "/threat_models/"+TestFixtures.ThreatModelID, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Send request
	router.ServeHTTP(w, req)

	// Print the response for debugging
	t.Logf("Response status: %d", w.Code)
	t.Logf("Response body: %s", w.Body.String())

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code, "Update request should succeed")

	// Verify the name was changed
	tm, err := ThreatModelStore.Get(TestFixtures.ThreatModelID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Name", tm.Name, "Name should be updated")
}

// TestUpdateTMOwnershipPreservesOriginalOwner validates that the original owner is preserved
// when ownership changes
func TestUpdateTMOwnershipPreservesOriginalOwner(t *testing.T) {
	// Initialize test fixtures
	InitTestFixtures()

	// Setup Gin
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userID", TestFixtures.OwnerUser+"-provider-id") // Provider ID for testing
		// Add userRole to context - this is crucial
		c.Set("userRole", RoleOwner)
		c.Next()
	})

	// Register handler
	handler := NewThreatModelHandler(NewWebSocketHubForTests())
	router.PUT("/threat_models/:threat_model_id", handler.UpdateThreatModel)

	// Get the current threat model
	origTM, err := ThreatModelStore.Get(TestFixtures.ThreatModelID)
	require.NoError(t, err)

	// Create an update payload with a new owner
	newOwner := "new-owner@example.com"

	// Create a more minimal payload with just the essential fields - note: we don't include 'id' as it's read-only
	updatePayload := map[string]any{
		"name":                   "Updated Name",
		"description":            *origTM.Description,
		"owner":                  newOwner,
		"threat_model_framework": "STRIDE", // Required field
		"authorization": []map[string]any{
			{
				"principal_type": "user",
				"provider":       "tmi",
				"provider_id":    TestFixtures.WriterUser,
				"role":           "writer",
			},
			{
				"principal_type": "user",
				"provider":       "tmi",
				"provider_id":    TestFixtures.ReaderUser,
				"role":           "reader",
			},
		},
	}

	jsonData, err := json.Marshal(updatePayload)
	require.NoError(t, err)

	// Debug print the JSON
	t.Logf("Request JSON: %s", string(jsonData))

	// Create request
	req, _ := http.NewRequest("PUT", "/threat_models/"+TestFixtures.ThreatModelID, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Send request
	router.ServeHTTP(w, req)

	// Print the response for debugging
	t.Logf("Response status: %d", w.Code)
	t.Logf("Response body: %s", w.Body.String())

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code, "Owner change request should succeed")

	// Verify the owner was changed
	tm, err := ThreatModelStore.Get(TestFixtures.ThreatModelID)
	require.NoError(t, err)
	assert.Equal(t, newOwner, tm.Owner.ProviderId, "Owner should be updated to the new owner")

	// Check that the original owner was preserved in authorization
	originalOwnerFound := false
	for _, auth := range tm.Authorization {
		if auth.ProviderId == TestFixtures.OwnerUser {
			originalOwnerFound = true
			assert.Equal(t, RoleOwner, auth.Role, "Original owner should have owner role")
			break
		}
	}
	assert.True(t, originalOwnerFound, "Original owner should be preserved in authorization")
}

// TestTMDuplicateSubjectsRejection validates that duplicate subjects are rejected
func TestTMDuplicateSubjectsRejection(t *testing.T) {
	// Initialize test fixtures
	InitTestFixtures()

	// Setup Gin
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userID", TestFixtures.OwnerUser+"-provider-id") // Provider ID for testing
		// Add userRole to context - this is crucial
		c.Set("userRole", RoleOwner)
		c.Next()
	})

	// Register handler
	handler := NewThreatModelHandler(NewWebSocketHubForTests())
	router.PUT("/threat_models/:threat_model_id", handler.UpdateThreatModel)

	// Create an update payload with duplicate subjects - note: we don't include 'id' as it's read-only
	updatePayload := map[string]any{
		"name":                   "Updated Name",
		"owner":                  TestFixtures.OwnerUser,
		"threat_model_framework": "STRIDE", // Required field
		"authorization": []map[string]any{
			{
				"principal_type": "user",
				"provider":       "tmi",
				"provider_id":    TestFixtures.WriterUser,
				"role":           "writer",
			},
			{
				"principal_type": "user",
				"provider":       "tmi",
				"provider_id":    TestFixtures.WriterUser, // Duplicate subject
				"role":           "reader",
			},
		},
	}

	jsonData, err := json.Marshal(updatePayload)
	require.NoError(t, err)

	// Debug print the JSON
	t.Logf("Request JSON: %s", string(jsonData))

	// Create request
	req, _ := http.NewRequest("PUT", "/threat_models/"+TestFixtures.ThreatModelID, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Send request
	router.ServeHTTP(w, req)

	// Print the response for debugging
	t.Logf("Response status: %d", w.Code)
	t.Logf("Response body: %s", w.Body.String())

	// Verify response shows a bad request
	assert.Equal(t, http.StatusBadRequest, w.Code, "Request with duplicate subjects should be rejected")

	// Parse the response
	var resp struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	// Check the error message
	assert.Equal(t, "invalid_input", resp.Error, "Error code should be 'invalid_input'")
	assert.Contains(t, resp.ErrorDescription, "Duplicate authorization subject", "Message should mention duplicate subject")
}

// TestSecurityReviewerAutoAddOnCreate validates that setting a security reviewer
// on creation automatically adds them to the authorization list with owner role.
func TestSecurityReviewerAutoAddOnCreate(t *testing.T) {
	InitTestFixtures()

	router := setupThreatModelRouter()

	reviewerEmail := "reviewer@example.com"
	reqBody := map[string]any{
		"name":        "TM with Security Reviewer",
		"description": "Test auto-add of security reviewer",
		"security_reviewer": map[string]any{
			"principal_type": "user",
			"provider":       "tmi",
			"provider_id":    reviewerEmail,
			"email":          reviewerEmail,
		},
	}

	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code, "Create should succeed")

	var tm ThreatModel
	err := json.Unmarshal(w.Body.Bytes(), &tm)
	require.NoError(t, err)

	// Verify the security reviewer was auto-added to authorization with owner role
	reviewerFound := false
	for _, auth := range tm.Authorization {
		if auth.ProviderId == reviewerEmail {
			reviewerFound = true
			assert.Equal(t, AuthorizationRoleOwner, auth.Role, "Security reviewer should have owner role")
			break
		}
	}
	assert.True(t, reviewerFound, "Security reviewer should be in the authorization list")
}

// TestSecurityReviewerAutoAddOnPUT validates that assigning a security reviewer
// via PUT automatically adds them to the authorization list with owner role.
func TestSecurityReviewerAutoAddOnPUT(t *testing.T) {
	InitTestFixtures()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userID", TestFixtures.OwnerUser+"-provider-id")
		c.Set("userRole", RoleOwner)
		c.Next()
	})
	handler := NewThreatModelHandler(NewWebSocketHubForTests())
	router.PUT("/threat_models/:threat_model_id", handler.UpdateThreatModel)

	reviewerEmail := "new-reviewer@example.com"
	updatePayload := map[string]any{
		"name": "Updated TM",
		"security_reviewer": map[string]any{
			"principal_type": "user",
			"provider":       "tmi",
			"provider_id":    reviewerEmail,
			"email":          reviewerEmail,
		},
		"authorization": []map[string]any{
			{
				"principal_type": "user",
				"provider":       "tmi",
				"provider_id":    TestFixtures.OwnerUser,
				"role":           "owner",
			},
		},
	}

	jsonData, _ := json.Marshal(updatePayload)
	req, _ := http.NewRequest("PUT", "/threat_models/"+TestFixtures.ThreatModelID, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "PUT should succeed")

	// Verify from the store (avoids response serialization issues with in-memory store)
	storedTM, err := ThreatModelStore.Get(TestFixtures.ThreatModelID)
	require.NoError(t, err)

	// Verify the security reviewer was auto-added to authorization with owner role
	reviewerFound := false
	for _, auth := range storedTM.Authorization {
		if auth.ProviderId == reviewerEmail {
			reviewerFound = true
			assert.Equal(t, AuthorizationRoleOwner, auth.Role, "Security reviewer should have owner role")
			break
		}
	}
	assert.True(t, reviewerFound, "Security reviewer should be auto-added to authorization")
}

// TestSecurityReviewerProtectionOnPUT validates that a PUT request cannot remove
// or downgrade the security reviewer's owner role.
func TestSecurityReviewerProtectionOnPUT(t *testing.T) {
	InitTestFixtures()

	reviewerEmail := "protected-reviewer@example.com"

	// First, set a security reviewer on the test threat model directly in the store
	tm, err := ThreatModelStore.Get(TestFixtures.ThreatModelID)
	require.NoError(t, err)
	tm.SecurityReviewer = &User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      "tmi",
		ProviderId:    reviewerEmail,
		Email:         openapi_types.Email(reviewerEmail),
	}
	tm.Authorization = append(tm.Authorization, Authorization{
		PrincipalType: AuthorizationPrincipalTypeUser,
		Provider:      "tmi",
		ProviderId:    reviewerEmail,
		Role:          RoleOwner,
	})
	err = ThreatModelStore.Update(TestFixtures.ThreatModelID, tm)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userID", TestFixtures.OwnerUser+"-provider-id")
		c.Set("userRole", RoleOwner)
		c.Next()
	})
	handler := NewThreatModelHandler(NewWebSocketHubForTests())
	router.PUT("/threat_models/:threat_model_id", handler.UpdateThreatModel)

	t.Run("removing security reviewer from authorization returns 409", func(t *testing.T) {
		updatePayload := map[string]any{
			"name": "Updated TM",
			"authorization": []map[string]any{
				{
					"principal_type": "user",
					"provider":       "tmi",
					"provider_id":    TestFixtures.OwnerUser,
					"role":           "owner",
				},
				// Note: security reviewer is NOT in this list
			},
		}

		jsonData, _ := json.Marshal(updatePayload)
		req, _ := http.NewRequest("PUT", "/threat_models/"+TestFixtures.ThreatModelID, bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code, "Removing security reviewer from auth should return 409")
		assert.Contains(t, w.Body.String(), "Cannot remove security reviewer")
	})

	t.Run("downgrading security reviewer to writer returns 409", func(t *testing.T) {
		updatePayload := map[string]any{
			"name": "Updated TM",
			"authorization": []map[string]any{
				{
					"principal_type": "user",
					"provider":       "tmi",
					"provider_id":    TestFixtures.OwnerUser,
					"role":           "owner",
				},
				{
					"principal_type": "user",
					"provider":       "tmi",
					"provider_id":    reviewerEmail,
					"role":           "writer", // Downgraded!
				},
			},
		}

		jsonData, _ := json.Marshal(updatePayload)
		req, _ := http.NewRequest("PUT", "/threat_models/"+TestFixtures.ThreatModelID, bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code, "Downgrading security reviewer should return 409")
		assert.Contains(t, w.Body.String(), "Cannot change role for security reviewer")
	})
}

// TestSecurityReviewerProtectionOnPATCH validates that a PATCH request cannot remove
// or downgrade the security reviewer's owner role.
func TestSecurityReviewerProtectionOnPATCH(t *testing.T) {
	InitTestFixtures()

	reviewerEmail := "patch-reviewer@example.com"

	// Set a security reviewer on the test threat model directly in the store
	tm, err := ThreatModelStore.Get(TestFixtures.ThreatModelID)
	require.NoError(t, err)
	tm.SecurityReviewer = &User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      "tmi",
		ProviderId:    reviewerEmail,
		Email:         openapi_types.Email(reviewerEmail),
	}
	tm.Authorization = append(tm.Authorization, Authorization{
		PrincipalType: AuthorizationPrincipalTypeUser,
		Provider:      "tmi",
		ProviderId:    reviewerEmail,
		Role:          RoleOwner,
	})
	err = ThreatModelStore.Update(TestFixtures.ThreatModelID, tm)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userID", TestFixtures.OwnerUser+"-provider-id")
		c.Set("userRole", RoleOwner)
		c.Next()
	})
	handler := NewThreatModelHandler(NewWebSocketHubForTests())
	router.PATCH("/threat_models/:threat_model_id", handler.PatchThreatModel)

	t.Run("replacing authorization without security reviewer returns 409", func(t *testing.T) {
		patchOps := []map[string]any{
			{
				"op":   "replace",
				"path": "/authorization",
				"value": []map[string]any{
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    TestFixtures.OwnerUser,
						"role":           "owner",
					},
					// Security reviewer NOT included
				},
			},
		}

		body, _ := json.Marshal(patchOps)
		req, _ := http.NewRequest("PATCH", "/threat_models/"+TestFixtures.ThreatModelID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code, "Removing security reviewer via PATCH should return 409")
		assert.Contains(t, w.Body.String(), "Cannot remove security reviewer")
	})
}

// TestSecurityReviewerClearingAllowsRemoval validates that after clearing the
// security reviewer, the user can be removed from authorization.
func TestSecurityReviewerClearingAllowsRemoval(t *testing.T) {
	InitTestFixtures()

	reviewerEmail := "clearable-reviewer@example.com"

	// Set a security reviewer on the test threat model directly in the store
	tm, err := ThreatModelStore.Get(TestFixtures.ThreatModelID)
	require.NoError(t, err)
	tm.SecurityReviewer = &User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      "tmi",
		ProviderId:    reviewerEmail,
		Email:         openapi_types.Email(reviewerEmail),
	}
	tm.Authorization = append(tm.Authorization, Authorization{
		PrincipalType: AuthorizationPrincipalTypeUser,
		Provider:      "tmi",
		ProviderId:    reviewerEmail,
		Role:          RoleOwner,
	})
	err = ThreatModelStore.Update(TestFixtures.ThreatModelID, tm)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userID", TestFixtures.OwnerUser+"-provider-id")
		c.Set("userRole", RoleOwner)
		c.Next()
	})
	handler := NewThreatModelHandler(NewWebSocketHubForTests())
	router.PATCH("/threat_models/:threat_model_id", handler.PatchThreatModel)

	// Clear security reviewer AND remove from authorization via PATCH
	// (PATCH can distinguish "set to null" from "not provided" unlike PUT)
	patchOps := []map[string]any{
		{
			"op":    "replace",
			"path":  "/security_reviewer",
			"value": nil,
		},
		{
			"op":   "replace",
			"path": "/authorization",
			"value": []map[string]any{
				{
					"principal_type": "user",
					"provider":       "tmi",
					"provider_id":    TestFixtures.OwnerUser,
					"role":           "owner",
				},
				// Reviewer NOT in list — but that's OK because we're clearing the reviewer
			},
		},
	}

	body, _ := json.Marshal(patchOps)
	req, _ := http.NewRequest("PATCH", "/threat_models/"+TestFixtures.ThreatModelID, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "Clearing security reviewer and removing from auth should succeed")
}

// TestSecurityReviewerChangeAllowsOldReviewerRemoval validates that changing the
// security reviewer to a new user means the old reviewer is no longer protected.
func TestSecurityReviewerChangeAllowsOldReviewerRemoval(t *testing.T) {
	InitTestFixtures()

	oldReviewerEmail := "old-reviewer@example.com"
	newReviewerEmail := "new-reviewer@example.com"

	// Set old security reviewer on the test threat model directly in the store
	tm, err := ThreatModelStore.Get(TestFixtures.ThreatModelID)
	require.NoError(t, err)
	tm.SecurityReviewer = &User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      "tmi",
		ProviderId:    oldReviewerEmail,
		Email:         openapi_types.Email(oldReviewerEmail),
	}
	tm.Authorization = append(tm.Authorization, Authorization{
		PrincipalType: AuthorizationPrincipalTypeUser,
		Provider:      "tmi",
		ProviderId:    oldReviewerEmail,
		Role:          RoleOwner,
	})
	err = ThreatModelStore.Update(TestFixtures.ThreatModelID, tm)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userID", TestFixtures.OwnerUser+"-provider-id")
		c.Set("userRole", RoleOwner)
		c.Next()
	})
	handler := NewThreatModelHandler(NewWebSocketHubForTests())
	router.PUT("/threat_models/:threat_model_id", handler.UpdateThreatModel)

	// Change security reviewer to new user, old reviewer NOT in authorization list
	updatePayload := map[string]any{
		"name": "Updated TM",
		"security_reviewer": map[string]any{
			"principal_type": "user",
			"provider":       "tmi",
			"provider_id":    newReviewerEmail,
			"email":          newReviewerEmail,
		},
		"authorization": []map[string]any{
			{
				"principal_type": "user",
				"provider":       "tmi",
				"provider_id":    TestFixtures.OwnerUser,
				"role":           "owner",
			},
			// Old reviewer NOT in list — but that's OK because reviewer is changing
			// New reviewer will be auto-added by the business rule
		},
	}

	jsonData, _ := json.Marshal(updatePayload)
	req, _ := http.NewRequest("PUT", "/threat_models/"+TestFixtures.ThreatModelID, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "Changing security reviewer should succeed")

	// Verify from store (avoids response serialization issues with in-memory store)
	storedTM, err := ThreatModelStore.Get(TestFixtures.ThreatModelID)
	require.NoError(t, err)

	// Verify new reviewer was auto-added with owner role
	newReviewerFound := false
	for _, auth := range storedTM.Authorization {
		if auth.ProviderId == newReviewerEmail {
			newReviewerFound = true
			assert.Equal(t, AuthorizationRoleOwner, auth.Role, "New security reviewer should have owner role")
			break
		}
	}
	assert.True(t, newReviewerFound, "New security reviewer should be auto-added to authorization")
}
