package api

import (
	"bytes"
	"encoding/json"
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

	// Setup router with owner user
	r := setupDiagramRouterWithUser(TestFixtures.OwnerUser)

	// Request the diagram using current ID
	req, _ := http.NewRequest("GET", "/diagrams/"+TestFixtures.DiagramID, nil)
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
		c.Set("userName", TestFixtures.OwnerUser)
		c.Next()
	})
	ownerRouter.Use(ThreatModelMiddleware())

	writerRouter := gin.New()
	writerRouter.Use(func(c *gin.Context) {
		c.Set("userName", TestFixtures.WriterUser)
		c.Next()
	})
	writerRouter.Use(ThreatModelMiddleware())

	readerRouter := gin.New()
	readerRouter.Use(func(c *gin.Context) {
		c.Set("userName", TestFixtures.ReaderUser)
		c.Next()
	})
	readerRouter.Use(ThreatModelMiddleware())

	// Add handlers
	handler := NewThreatModelHandler()
	for _, r := range []*gin.Engine{ownerRouter, writerRouter, readerRouter} {
		r.GET("/threat_models/:id", handler.GetThreatModelByID)
		r.DELETE("/threat_models/:id", handler.DeleteThreatModel)
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

func TestDiagramRoleBasedAccess(t *testing.T) {
	// Initialize test fixtures
	InitTestFixtures()

	// Get the current ID for this test run
	diagramID := TestFixtures.DiagramID

	// Setup our own test routers for diagrams
	gin.SetMode(gin.TestMode)

	// Create routers for different users
	ownerRouter := gin.New()
	ownerRouter.Use(func(c *gin.Context) {
		c.Set("userName", TestFixtures.OwnerUser)
		c.Next()
	})
	ownerRouter.Use(DiagramMiddleware())

	writerRouter := gin.New()
	writerRouter.Use(func(c *gin.Context) {
		c.Set("userName", TestFixtures.WriterUser)
		c.Next()
	})
	writerRouter.Use(DiagramMiddleware())

	readerRouter := gin.New()
	readerRouter.Use(func(c *gin.Context) {
		c.Set("userName", TestFixtures.ReaderUser)
		c.Next()
	})
	readerRouter.Use(DiagramMiddleware())

	// Add handlers
	handler := NewDiagramHandler()
	for _, r := range []*gin.Engine{ownerRouter, writerRouter, readerRouter} {
		r.GET("/diagrams/:id", handler.GetDiagramByID)
		r.DELETE("/diagrams/:id", handler.DeleteDiagram)
	}

	// Test owner access
	req, _ := http.NewRequest("GET", "/diagrams/"+diagramID, nil)
	w := httptest.NewRecorder()
	ownerRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "Owner should be able to read diagram")

	// Test writer access
	w = httptest.NewRecorder()
	writerRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "Writer should be able to read diagram")

	// Test reader access
	w = httptest.NewRecorder()
	readerRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "Reader should be able to read diagram")

	// The next tests will check DELETE permissions
	// Add separate delete tests for each role

	// 1. Test writer DELETE permission (should be forbidden)
	// Reinitialize for new ID and ensure the writer has access
	InitTestFixtures()
	diagramID = TestFixtures.DiagramID

	deleteReq, _ := http.NewRequest("DELETE", "/diagrams/"+diagramID, nil)
	w = httptest.NewRecorder()
	writerRouter.ServeHTTP(w, deleteReq)
	assert.Equal(t, http.StatusForbidden, w.Code, "Writer should not be able to delete diagram")

	// 2. Test reader DELETE permission (should be forbidden)
	// No need to reinitialize since we didn't delete the object
	w = httptest.NewRecorder()
	readerRouter.ServeHTTP(w, deleteReq)
	assert.Equal(t, http.StatusForbidden, w.Code, "Reader should not be able to delete diagram")

	// 3. Test owner DELETE permission (should succeed)
	// No need to reinitialize since we didn't delete the object
	w = httptest.NewRecorder()
	ownerRouter.ServeHTTP(w, deleteReq)
	assert.Equal(t, http.StatusNoContent, w.Code, "Owner should be able to delete diagram")
}

// TestThreatModelCustomAuthRules tests the custom authorization rules for threat models
func TestThreatModelCustomAuthRules(t *testing.T) {
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
		c.Set("userName", TestFixtures.OwnerUser)
		c.Next()
	})
	ownerRouter.Use(ThreatModelMiddleware())

	// Add handlers
	handler := NewThreatModelHandler()
	ownerRouter.PUT("/threat_models/:id", handler.UpdateThreatModel)
	ownerRouter.PATCH("/threat_models/:id", handler.PatchThreatModel)
	ownerRouter.GET("/threat_models/:id", handler.GetThreatModelByID)

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
		"authorization": [
			{"subject": "%s", "role": "writer"},
			{"subject": "%s", "role": "reader"}
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
		"authorization": [
			{"subject": "%s", "role": "writer"},
			{"subject": "%s", "role": "reader"}
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
	assert.Equal(t, newOwner, tm.Owner, "Owner should be changed to the new owner")

	originalOwnerFound := false
	for _, auth := range tm.Authorization {
		if auth.Subject == TestFixtures.OwnerUser {
			originalOwnerFound = true
			assert.Equal(t, Owner, auth.Role, "Original owner should have Owner role")
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
		{"op":"replace","path":"/authorization","value":[{"subject":"%s","role":"writer"}]},
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
	assert.Equal(t, newOwner, tm.Owner, "Owner should be changed to the new owner")

	originalOwnerFound = false
	for _, auth := range tm.Authorization {
		if auth.Subject == TestFixtures.OwnerUser {
			originalOwnerFound = true
			assert.Equal(t, Owner, auth.Role, "Original owner should have Owner role")
			break
		}
	}
	assert.True(t, originalOwnerFound, "Original owner should be preserved in authorization after PATCH")
}

// TestDiagramNonOwnerFields tests that non-owner and non-authorization related fields
// can be updated based on the parent threat model's authorization settings
func TestDiagramNonOwnerFields(t *testing.T) {
	// Initialize test fixtures - this will reset the stores and create fresh data
	InitTestFixtures()
	diagramID := TestFixtures.DiagramID

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
		c.Set("userName", TestFixtures.OwnerUser)
		c.Next()
	})
	ownerRouter.Use(DiagramMiddleware())

	// Add handlers
	handler := NewDiagramHandler()
	ownerRouter.PUT("/diagrams/:id", handler.UpdateDiagram)
	ownerRouter.PATCH("/diagrams/:id", handler.PatchDiagram)
	ownerRouter.GET("/diagrams/:id", handler.GetDiagramByID)

	// Verify the test diagram exists before starting
	req, _ := http.NewRequest("GET", "/diagrams/"+diagramID, nil)
	w := httptest.NewRecorder()
	ownerRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "Test diagram should exist in the store")

	// Test: Update non-owner fields (should succeed)
	// Create a PUT request with only non-owner fields
	updateReq := fmt.Sprintf(`{
		"id": "%s",
		"name": "Updated Diagram Name",
		"description": "This is an updated description"
	}`, diagramID)

	req, _ = http.NewRequest("PUT", "/diagrams/"+diagramID, strings.NewReader(updateReq))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	ownerRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "Update of non-owner fields should succeed")

	// Check the response for field updates
	var responseData map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &responseData)
	assert.NoError(t, err)

	// Verify the fields were updated
	assert.Equal(t, "Updated Diagram Name", responseData["name"], "Name should be updated")
	assert.Equal(t, "This is an updated description", responseData["description"], "Description should be updated")

	// Test: PATCH operation for non-owner fields
	// Initialize fresh fixtures for the next test
	InitTestFixtures()
	diagramID = TestFixtures.DiagramID // Get the new ID

	patchReq := `[
		{"op":"replace","path":"/name","value":"Patched Diagram Name"},
		{"op":"replace","path":"/description","value":"This is a patched description"}
	]`

	req, _ = http.NewRequest("PATCH", "/diagrams/"+diagramID, strings.NewReader(patchReq))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	ownerRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "Patch of non-owner fields should succeed")

	// Check the response for field updates
	var patchResponseData map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &patchResponseData)
	assert.NoError(t, err)

	// Verify the fields were updated
	assert.Equal(t, "Patched Diagram Name", patchResponseData["name"], "Name should be updated")
	assert.Equal(t, "This is a patched description", patchResponseData["description"], "Description should be updated")
}
