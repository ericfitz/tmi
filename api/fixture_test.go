package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestFixturesInitialization(t *testing.T) {
	// Initialize fixtures
	InitTestFixtures()

	// Verify threat model fixture is in the store
	tm, err := ThreatModelStore.Get(TestFixtures.ThreatModelID)
	assert.NoError(t, err)
	assert.Equal(t, TestFixtures.ThreatModel.Name, tm.Name)
	assert.Equal(t, TestFixtures.ThreatModel.Owner, tm.Owner)

	// Verify diagram fixture is in the store
	d, err := DiagramStore.Get(TestFixtures.DiagramID)
	assert.NoError(t, err)
	assert.Equal(t, TestFixtures.Diagram.Name, d.Name)
	// Owner is now stored in TestFixtures.Owner, not in the Diagram struct
}

func TestGetFixturesThreatModel(t *testing.T) {
	// Initialize test fixtures to ensure a new, valid threat model ID
	InitTestFixtures()

	// Verify the threat model exists in the store directly
	tm, err := ThreatModelStore.Get(TestFixtures.ThreatModelID)
	assert.NoError(t, err, "Test threat model should exist in the store")
	assert.Equal(t, TestFixtures.ThreatModel.Name, tm.Name)

	// Setup router with owner user
	r := setupThreatModelRouterWithUser(TestFixtures.OwnerUser)

	// Request the threat model using current ID
	req, _ := http.NewRequest("GET", "/threat_models/"+TestFixtures.ThreatModelID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Log for debugging
	t.Logf("Response status: %d, Body: %s", w.Code, w.Body.String())

	// Assert successful retrieval
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetFixturesDiagram(t *testing.T) {
	// Initialize test fixtures to ensure a new, valid diagram ID
	InitTestFixtures()

	// Verify the diagram exists in the store directly
	d, err := DiagramStore.Get(TestFixtures.DiagramID)
	assert.NoError(t, err, "Test diagram should exist in the store")
	assert.Equal(t, TestFixtures.Diagram.Name, d.Name)

	// Setup router with owner user using sub-entity pattern
	r := setupThreatModelDiagramRouterWithUser(TestFixtures.OwnerUser)

	// Request the diagram using current ID via sub-entity endpoint
	diagramURL := fmt.Sprintf("/threat_models/%s/diagrams/%s", TestFixtures.ThreatModelID, TestFixtures.DiagramID)
	req, _ := http.NewRequest("GET", diagramURL, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Log for debugging
	t.Logf("Response status: %d, Body: %s", w.Code, w.Body.String())

	// Assert successful retrieval
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestThreatModelRoleBasedAccess(t *testing.T) {
	// Initialize test fixtures
	InitTestFixtures()

	// Get the current ID for this test run
	threatModelID := TestFixtures.ThreatModelID

	// Setup our own test routers for threat models
	gin.SetMode(gin.TestMode)

	// Create routers for different users
	ownerRouter := gin.New()
	ownerRouter.Use(func(c *gin.Context) {
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userID", TestFixtures.OwnerUser+"-provider-id")  // Provider ID for testing
		c.Next()
	})
	ownerRouter.Use(ThreatModelMiddleware())

	writerRouter := gin.New()
	writerRouter.Use(func(c *gin.Context) {
		c.Set("userEmail", TestFixtures.WriterUser)
		c.Set("userID", TestFixtures.WriterUser+"-provider-id")  // Provider ID for testing
		c.Next()
	})
	writerRouter.Use(ThreatModelMiddleware())

	readerRouter := gin.New()
	readerRouter.Use(func(c *gin.Context) {
		c.Set("userEmail", TestFixtures.ReaderUser)
		c.Set("userID", TestFixtures.ReaderUser+"-provider-id")  // Provider ID for testing
		c.Next()
	})
	readerRouter.Use(ThreatModelMiddleware())

	// Add handlers
	handler := NewThreatModelHandler(NewWebSocketHubForTests())
	for _, r := range []*gin.Engine{ownerRouter, writerRouter, readerRouter} {
		r.GET("/threat_models/:threat_model_id", handler.GetThreatModelByID)
		r.DELETE("/threat_models/:threat_model_id", handler.DeleteThreatModel)
	}

	// Test owner access
	req, _ := http.NewRequest("GET", "/threat_models/"+threatModelID, nil)
	w := httptest.NewRecorder()
	ownerRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "Owner should be able to read threat model")

	// Test writer access
	w = httptest.NewRecorder()
	writerRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "Writer should be able to read threat model")

	// Test reader access
	w = httptest.NewRecorder()
	readerRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "Reader should be able to read threat model")

	// The next tests will check DELETE permissions
	// Add separate delete tests for each role

	// 1. Test writer DELETE permission (should be forbidden)
	// Reinitialize for new ID and ensure the writer has access
	InitTestFixtures()
	threatModelID = TestFixtures.ThreatModelID

	deleteReq, _ := http.NewRequest("DELETE", "/threat_models/"+threatModelID, nil)
	w = httptest.NewRecorder()
	writerRouter.ServeHTTP(w, deleteReq)
	assert.Equal(t, http.StatusForbidden, w.Code, "Writer should not be able to delete threat model")

	// 2. Test reader DELETE permission (should be forbidden)
	// No need to reinitialize since we didn't delete the object
	w = httptest.NewRecorder()
	readerRouter.ServeHTTP(w, deleteReq)
	assert.Equal(t, http.StatusForbidden, w.Code, "Reader should not be able to delete threat model")

	// 3. Test owner DELETE permission (should succeed)
	// No need to reinitialize since we didn't delete the object
	w = httptest.NewRecorder()
	ownerRouter.ServeHTTP(w, deleteReq)
	assert.Equal(t, http.StatusNoContent, w.Code, "Owner should be able to delete threat model")
}

// TestThreatModelCustomAuthRules tests the custom authorization rules for threat models
func TestThreatModelCustomAuthRules(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	// Initialize test fixtures - this will reset the stores and create fresh data
	InitTestFixtures()
	threatModelID := TestFixtures.ThreatModelID

	// Setup router with owner user
	gin.SetMode(gin.TestMode)
	ownerRouter := gin.New()
	// Add a debug middleware to log request bodies
	ownerRouter.Use(func(c *gin.Context) {
		// Log the user making the request
		t.Logf("User: %s", TestFixtures.OwnerUser)

		// Safely read the request body for debugging
		if c.Request.Body != nil {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err == nil {
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
				t.Logf("Request body: %s", string(bodyBytes))
			} else {
				t.Logf("Error reading request body: %v", err)
			}
		} else {
			t.Logf("Request body is nil")
		}

		// Set the user name
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userID", TestFixtures.OwnerUser+"-provider-id")  // Provider ID for testing
		c.Next()
	})
	ownerRouter.Use(ThreatModelMiddleware())

	// Add handlers
	handler := NewThreatModelHandler(NewWebSocketHubForTests())
	ownerRouter.PUT("/threat_models/:threat_model_id", handler.UpdateThreatModel)
	ownerRouter.PATCH("/threat_models/:threat_model_id", handler.PatchThreatModel)
	ownerRouter.GET("/threat_models/:threat_model_id", handler.GetThreatModelByID)

	// Verify the test threat model exists before starting
	req, _ := http.NewRequest("GET", "/threat_models/"+threatModelID, nil)
	w := httptest.NewRecorder()
	ownerRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "Test threat model should exist in the store")

	// Test 1: Duplicate subjects in authorization array (should be rejected)
	// Create a PUT request with duplicate authorization subjects
	duplicateAuth := fmt.Sprintf(`{
		"id": "%s",
		"name": "Updated Threat Model",
		"owner": "%s",
		"threat_model_framework": "STRIDE",
		"authorization": [
			{"principal_type": "user", "provider": "test", "provider_id": "%s", "role": "writer"},
			{"principal_type": "user", "provider": "test", "provider_id": "%s", "role": "reader"}
		]
	}`, threatModelID, TestFixtures.OwnerUser, TestFixtures.WriterUser, TestFixtures.WriterUser)

	req, _ = http.NewRequest("PUT", "/threat_models/"+threatModelID, strings.NewReader(duplicateAuth))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	ownerRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, "Request with duplicate subjects should be rejected")

	// Test 2: Owner change should preserve original owner with owner role
	// Create a PUT request changing the owner
	newOwner := "new-owner@example.com"

	// Initialize fresh fixtures for the next test
	InitTestFixtures()
	threatModelID = TestFixtures.ThreatModelID // Get the new ID

	// Create a minimal update request
	changeOwnerReq := fmt.Sprintf(`{
		"id": "%s",
		"name": "Updated Threat Model",
		"owner": "%s",
		"threat_model_framework": "STRIDE",
		"authorization": [
			{"principal_type": "user", "provider": "test", "provider_id": "%s", "role": "writer"},
			{"principal_type": "user", "provider": "test", "provider_id": "%s", "role": "reader"}
		]
	}`, threatModelID, newOwner, TestFixtures.WriterUser, TestFixtures.ReaderUser)

	req, _ = http.NewRequest("PUT", "/threat_models/"+threatModelID, strings.NewReader(changeOwnerReq))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	ownerRouter.ServeHTTP(w, req)
	t.Logf("PUT Response Code: %d, Body: %s", w.Code, w.Body.String())
	assert.Equal(t, http.StatusOK, w.Code, "Owner change request should succeed")

	// Check that the original owner is still in the authorization list with owner role
	tm, err := ThreatModelStore.Get(threatModelID)
	assert.NoError(t, err)
	assert.Equal(t, newOwner, tm.Owner.ProviderId, "Owner should be changed to the new owner")

	originalOwnerFound := false
	for _, auth := range tm.Authorization {
		if auth.ProviderId == TestFixtures.OwnerUser {
			originalOwnerFound = true
			assert.Equal(t, RoleOwner, auth.Role, "Original owner should have Owner role")
			break
		}
	}
	assert.True(t, originalOwnerFound, "Original owner should be preserved in authorization")

	// Test 3: PATCH operation with owner change should preserve original owner
	// Initialize fresh fixtures for the next test
	InitTestFixtures()
	threatModelID = TestFixtures.ThreatModelID // Get the new ID

	patchChangeOwner := fmt.Sprintf(`[
		{"op":"replace","path":"/owner","value":"%s"},
		{"op":"replace","path":"/authorization","value":[{"principal_type":"user","provider":"test","provider_id":"%s","role":"writer"}]},
		{"op":"replace","path":"/name","value":"Patched Model"}
	]`, newOwner, TestFixtures.WriterUser)

	req, _ = http.NewRequest("PATCH", "/threat_models/"+threatModelID, strings.NewReader(patchChangeOwner))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	ownerRouter.ServeHTTP(w, req)
	t.Logf("PATCH Response Code: %d, Body: %s", w.Code, w.Body.String())
	assert.Equal(t, http.StatusOK, w.Code, "Patch request with owner change should succeed")

	// Check that the original owner is still in the authorization list with owner role
	tm, err = ThreatModelStore.Get(threatModelID)
	assert.NoError(t, err)
	assert.Equal(t, newOwner, tm.Owner.ProviderId, "Owner should be changed to the new owner")

	originalOwnerFound = false
	for _, auth := range tm.Authorization {
		if auth.ProviderId == TestFixtures.OwnerUser {
			originalOwnerFound = true
			assert.Equal(t, RoleOwner, auth.Role, "Original owner should have Owner role")
			break
		}
	}
	assert.True(t, originalOwnerFound, "Original owner should be preserved in authorization after PATCH")
}
