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
		// Set userRole to owner - this is needed for the handler
		c.Set("userRole", RoleOwner)
		c.Next()
	})

	// Register handler
	handler := NewThreatModelHandler()
	router.PUT("/threat_models/:threat_model_id", handler.UpdateThreatModel)

	// Create a simplified update payload - note: we don't include 'id' as it's read-only
	updatePayload := map[string]interface{}{
		"name":                   "Updated Name",
		"owner":                  TestFixtures.OwnerUser,
		"threat_model_framework": "STRIDE",
		"authorization": []map[string]interface{}{
			{
				"subject": TestFixtures.WriterUser,
				"role":    "writer",
			},
			{
				"subject": TestFixtures.ReaderUser,
				"role":    "reader",
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
		// Add userRole to context - this is crucial
		c.Set("userRole", RoleOwner)
		c.Next()
	})

	// Register handler
	handler := NewThreatModelHandler()
	router.PUT("/threat_models/:threat_model_id", handler.UpdateThreatModel)

	// Get the current threat model
	origTM, err := ThreatModelStore.Get(TestFixtures.ThreatModelID)
	require.NoError(t, err)

	// Create an update payload with a new owner
	newOwner := "new-owner@example.com"

	// Create a more minimal payload with just the essential fields - note: we don't include 'id' as it's read-only
	updatePayload := map[string]interface{}{
		"name":                   "Updated Name",
		"description":            *origTM.Description,
		"owner":                  newOwner,
		"threat_model_framework": "STRIDE", // Required field
		"authorization": []map[string]interface{}{
			{
				"subject": TestFixtures.WriterUser,
				"role":    "writer",
			},
			{
				"subject": TestFixtures.ReaderUser,
				"role":    "reader",
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
	assert.Equal(t, newOwner, tm.Owner, "Owner should be updated to the new owner")

	// Check that the original owner was preserved in authorization
	originalOwnerFound := false
	for _, auth := range tm.Authorization {
		if auth.Subject == TestFixtures.OwnerUser {
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
		// Add userRole to context - this is crucial
		c.Set("userRole", RoleOwner)
		c.Next()
	})

	// Register handler
	handler := NewThreatModelHandler()
	router.PUT("/threat_models/:threat_model_id", handler.UpdateThreatModel)

	// Create an update payload with duplicate subjects - note: we don't include 'id' as it's read-only
	updatePayload := map[string]interface{}{
		"name":                   "Updated Name",
		"owner":                  TestFixtures.OwnerUser,
		"threat_model_framework": "STRIDE", // Required field
		"authorization": []map[string]interface{}{
			{
				"subject": TestFixtures.WriterUser,
				"role":    "writer",
			},
			{
				"subject": TestFixtures.WriterUser, // Duplicate subject
				"role":    "reader",
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
