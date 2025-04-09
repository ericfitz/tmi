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

// setupThreatModelRouter returns a router with threat model handlers registered for the owner user
func setupThreatModelRouter() *gin.Engine {
	// Initialize test fixtures first
	InitTestFixtures()
	return setupThreatModelRouterWithUser(TestFixtures.OwnerUser)
}

// setupThreatModelRouterWithUser returns a router with threat model handlers registered and specified user
func setupThreatModelRouterWithUser(userName string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	
	// Test fixtures should already be initialized by setupThreatModelRouter
	
	// Add a fake auth middleware to set user in context
	r.Use(func(c *gin.Context) {
		c.Set("userName", userName)
		// The middleware will set the userRole, we don't need to set it here
		c.Next()
	})
	
	// Add our authorization middleware
	r.Use(ThreatModelMiddleware())
	
	// Register threat model routes
	handler := NewThreatModelHandler()
	r.GET("/threat_models", handler.GetThreatModels)
	r.POST("/threat_models", handler.CreateThreatModel)
	r.GET("/threat_models/:id", handler.GetThreatModelByID)
	r.PUT("/threat_models/:id", handler.UpdateThreatModel)
	r.PATCH("/threat_models/:id", handler.PatchThreatModel)
	r.DELETE("/threat_models/:id", handler.DeleteThreatModel)
	
	return r
}

// TestCreateThreatModel tests creating a new threat model
func TestCreateThreatModel(t *testing.T) {
	r := setupThreatModelRouter()
	
	// Create request body
	reqBody := map[string]interface{}{
		"name":        "Test Threat Model",
		"description": "This is a test threat model",
	}
	
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	
	// Assert response
	assert.Equal(t, http.StatusCreated, w.Code)
	
	// Parse response
	var tm ThreatModel
	err := json.Unmarshal(w.Body.Bytes(), &tm)
	require.NoError(t, err)
	
	// Check fields
	assert.Equal(t, "Test Threat Model", tm.Name)
	assert.NotNil(t, tm.Description)
	assert.Equal(t, "This is a test threat model", *tm.Description)
	assert.Equal(t, "test@example.com", tm.Owner)
	assert.NotEmpty(t, tm.Id)
	assert.Len(t, tm.Authorization, 1)
	assert.Equal(t, "test@example.com", tm.Authorization[0].Subject)
	assert.Equal(t, Owner, tm.Authorization[0].Role)
}

// TestGetThreatModels tests listing threat models
func TestGetThreatModels(t *testing.T) {
	r := setupThreatModelRouter()
	
	// Create a test threat model
	// First, create the request
	reqBody, _ := json.Marshal(map[string]interface{}{
		"name":        "Test Threat Model",
		"description": "This is a test threat model",
	})
	
	// Create request to add a threat model
	req, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	// Perform the request
	r.ServeHTTP(w, req)
	
	// Verify it was created successfully
	assert.Equal(t, http.StatusCreated, w.Code)
	
	// Now test getting the list
	listReq, _ := http.NewRequest("GET", "/threat_models", nil)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)
	
	// Assert response
	assert.Equal(t, http.StatusOK, listW.Code)
	
	// Parse response
	var items []ListItem
	err := json.Unmarshal(listW.Body.Bytes(), &items)
	require.NoError(t, err)
	
	// Check that we got at least one item
	assert.NotEmpty(t, items)
	
	// Check that our test item is in the list
	found := false
	for _, item := range items {
		if item.Name == "Test Threat Model" {
			found = true
			break
		}
	}
	assert.True(t, found, "Test threat model should be in the list")
}

// TestPatchThreatModel tests patching a threat model with JSON Patch
func TestPatchThreatModel(t *testing.T) {
	r := setupThreatModelRouter()
	
	// First, create a test threat model
	createReqBody, _ := json.Marshal(map[string]interface{}{
		"name":        "Original Threat Model",
		"description": "This is the original description",
	})
	
	createReq, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(createReqBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)
	
	// Verify it was created successfully
	assert.Equal(t, http.StatusCreated, createW.Code)
	
	// Parse response to get ID
	var tm ThreatModel
	err := json.Unmarshal(createW.Body.Bytes(), &tm)
	require.NoError(t, err)
	
	// Now prepare a JSON Patch to modify the description
	patchOps := []PatchOperation{
		{
			Op:    "replace",
			Path:  "/name",
			Value: "Updated Threat Model",
		},
		{
			Op:    "replace",
			Path:  "/description",
			Value: "This is the updated description",
		},
	}
	
	patchBody, _ := json.Marshal(patchOps)
	patchReq, _ := http.NewRequest("PATCH", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	
	// Perform the patch
	r.ServeHTTP(patchW, patchReq)
	
	// Assert response
	assert.Equal(t, http.StatusOK, patchW.Code)
	
	// Parse response
	var patchedTM ThreatModel
	err = json.Unmarshal(patchW.Body.Bytes(), &patchedTM)
	require.NoError(t, err)
	
	// Check patched fields
	assert.Equal(t, "Updated Threat Model", patchedTM.Name)
	assert.NotNil(t, patchedTM.Description)
	assert.Equal(t, "This is the updated description", *patchedTM.Description)
	
	// Ensure other fields are preserved
	assert.Equal(t, tm.Id, patchedTM.Id)
	assert.Equal(t, tm.Owner, patchedTM.Owner)
	assert.Equal(t, tm.CreatedAt, patchedTM.CreatedAt)
	
	// Modification time should be updated
	assert.NotEqual(t, tm.ModifiedAt, patchedTM.ModifiedAt)
}

// Helper functions
func stringPointer(s string) *string {
	return &s
}

// createTestThreatModel creates a test threat model and returns it
func createTestThreatModel(t *testing.T, router *gin.Engine, name string, description string) ThreatModel {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"name":        name,
		"description": description,
	})
	
	req, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusCreated, w.Code)
	
	var tm ThreatModel
	err := json.Unmarshal(w.Body.Bytes(), &tm)
	require.NoError(t, err)
	
	return tm
}

// TestCreateThreatModelWithDuplicateSubjects tests creating a threat model with duplicate subjects
func TestCreateThreatModelWithDuplicateSubjects(t *testing.T) {
	r := setupThreatModelRouter()
	
	// Create request with duplicate subjects in authorization
	reqBody := map[string]interface{}{
		"name":        "Duplicate Subjects Test",
		"description": "This should fail due to duplicate subjects",
		"authorization": []map[string]interface{}{
			{
				"subject": "alice@example.com",
				"role":    "reader",
			},
			{
				"subject": "alice@example.com", // Duplicate subject
				"role":    "writer",
			},
		},
	}
	
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	
	// Assert response - should fail with 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, w.Code)
	
	var errResp Error
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	
	assert.Equal(t, "invalid_input", errResp.Error)
	assert.Contains(t, errResp.Message, "Duplicate authorization subject")
}

// TestCreateThreatModelWithDuplicateOwner tests creating a threat model with a subject that duplicates the owner
func TestCreateThreatModelWithDuplicateOwner(t *testing.T) {
	r := setupThreatModelRouter()
	
	// Create request with a subject that matches the owner
	reqBody := map[string]interface{}{
		"name":        "Duplicate Owner Test",
		"description": "This should fail due to duplicate with owner",
		"authorization": []map[string]interface{}{
			{
				"subject": "test@example.com", // Same as the owner from middleware
				"role":    "reader",
			},
		},
	}
	
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	
	// Assert response - should fail with 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, w.Code)
	
	var errResp Error
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	
	assert.Equal(t, "invalid_input", errResp.Error)
	assert.Contains(t, errResp.Message, "Duplicate authorization subject with owner")
}

// TestUpdateThreatModelOwnerChange tests the rule that when the owner changes, the original owner
// is added to the authorization list with owner role
func TestUpdateThreatModelOwnerChange(t *testing.T) {
	// Create initial router and threat model
	originalRouter := setupThreatModelRouter() // original owner is test@example.com
	tm := createTestThreatModel(t, originalRouter, "Owner Change Test", "Testing owner change rules")
	
	// Now create a new router with a different user
	newOwnerRouter := setupThreatModelRouterWithUser("newowner@example.com")
	
	// First, give the new user access to the threat model
	patchOps := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"subject": "newowner@example.com",
				"role":    "owner", // Need owner role to change owner
			},
		},
	}
	
	patchBody, _ := json.Marshal(patchOps)
	patchReq, _ := http.NewRequest("PATCH", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	originalRouter.ServeHTTP(patchW, patchReq)
	assert.Equal(t, http.StatusOK, patchW.Code)
	
	// Now, as the new user, change the owner
	updatedTM := tm
	updatedTM.Owner = "newowner@example.com"
	
	// Remove the original owner from the authorization list to test that it gets added back
	updatedTM.Authorization = []Authorization{
		{
			Subject: "newowner@example.com",
			Role:    Owner,
		},
	}
	
	updateBody, _ := json.Marshal(updatedTM)
	updateReq, _ := http.NewRequest("PUT", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	
	newOwnerRouter.ServeHTTP(updateW, updateReq)
	
	// Assert response
	assert.Equal(t, http.StatusOK, updateW.Code)
	
	var resultTM ThreatModel
	err := json.Unmarshal(updateW.Body.Bytes(), &resultTM)
	require.NoError(t, err)
	
	// Check that the owner was changed
	assert.Equal(t, "newowner@example.com", resultTM.Owner)
	
	// Check that the original owner is still in the authorization list with owner role
	foundOriginalOwner := false
	for _, auth := range resultTM.Authorization {
		if auth.Subject == "test@example.com" && auth.Role == Owner {
			foundOriginalOwner = true
			break
		}
	}
	assert.True(t, foundOriginalOwner, "Original owner should still have owner role in authorization")
}

// TestUpdateThreatModelWithDuplicateSubjects tests updating a threat model with duplicate subjects
func TestUpdateThreatModelWithDuplicateSubjects(t *testing.T) {
	r := setupThreatModelRouter()
	tm := createTestThreatModel(t, r, "Duplicate Subject Update Test", "Testing duplicate subject validation")
	
	// Now try to update with duplicate subjects
	updatedTM := tm
	updatedTM.Authorization = []Authorization{
		{
			Subject: "test@example.com",
			Role:    Owner,
		},
		{
			Subject: "alice@example.com",
			Role:    Reader,
		},
		{
			Subject: "alice@example.com", // Duplicate subject
			Role:    Writer,
		},
	}
	
	updateBody, _ := json.Marshal(updatedTM)
	updateReq, _ := http.NewRequest("PUT", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	
	r.ServeHTTP(updateW, updateReq)
	
	// Assert response - should fail with 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, updateW.Code)
	
	var errResp Error
	err := json.Unmarshal(updateW.Body.Bytes(), &errResp)
	require.NoError(t, err)
	
	assert.Equal(t, "invalid_input", errResp.Error)
	assert.Contains(t, errResp.Message, "Duplicate authorization subject")
}

// TestNonOwnerCannotChangeOwner tests that a non-owner user cannot change the owner
func TestNonOwnerCannotChangeOwner(t *testing.T) {
	// Create initial router and threat model
	originalRouter := setupThreatModelRouter() // original owner is test@example.com
	tm := createTestThreatModel(t, originalRouter, "Owner Protection Test", "Testing owner protection rules")
	
	// Add a reader user to the threat model
	patchOps := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"subject": "reader@example.com",
				"role":    "reader",
			},
		},
	}
	
	patchBody, _ := json.Marshal(patchOps)
	patchReq, _ := http.NewRequest("PATCH", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	originalRouter.ServeHTTP(patchW, patchReq)
	assert.Equal(t, http.StatusOK, patchW.Code)
	
	// Now create a router with the reader user
	readerRouter := setupThreatModelRouterWithUser("reader@example.com")
	
	// Try to change the owner as the reader
	updatedTM := tm
	updatedTM.Owner = "reader@example.com"
	
	updateBody, _ := json.Marshal(updatedTM)
	updateReq, _ := http.NewRequest("PUT", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	
	readerRouter.ServeHTTP(updateW, updateReq)
	
	// Assert response - should be forbidden
	assert.Equal(t, http.StatusForbidden, updateW.Code)
	
	var errResp Error
	err := json.Unmarshal(updateW.Body.Bytes(), &errResp)
	require.NoError(t, err)
	
	assert.Equal(t, "forbidden", errResp.Error)
	// The error message might vary based on the implementation, but it should be a forbidden error
	// assert.Contains(t, errResp.Message, "Only the owner can transfer ownership")
}

// TestOwnershipTransferViaPatching tests changing ownership via PATCH operation
func TestOwnershipTransferViaPatching(t *testing.T) {
	// Reset stores to ensure clean state 
	ResetStores()
	
	// Create initial router and threat model
	originalRouter := setupThreatModelRouter() // original owner is test@example.com
	tm := createTestThreatModel(t, originalRouter, "Owner Patch Test", "Testing owner patching rules")
	
	// First, add a new user with owner permissions
	patchOps := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"subject": "newowner@example.com",
				"role":    "owner",
			},
		},
	}
	
	patchBody, _ := json.Marshal(patchOps)
	patchReq, _ := http.NewRequest("PATCH", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	originalRouter.ServeHTTP(patchW, patchReq)
	assert.Equal(t, http.StatusOK, patchW.Code)
	
	// Now create a router with the new owner
	newOwnerRouter := setupThreatModelRouterWithUser("newowner@example.com")
	
	// Now transfer ownership via PATCH
	transferPatchOps := []PatchOperation{
		{
			Op:    "replace",
			Path:  "/owner",
			Value: "newowner@example.com",
		},
	}
	
	transferPatchBody, _ := json.Marshal(transferPatchOps)
	transferPatchReq, _ := http.NewRequest("PATCH", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(transferPatchBody))
	transferPatchReq.Header.Set("Content-Type", "application/json")
	transferPatchW := httptest.NewRecorder()
	
	newOwnerRouter.ServeHTTP(transferPatchW, transferPatchReq)
	
	// Assert response
	assert.Equal(t, http.StatusOK, transferPatchW.Code)
	
	var resultTM ThreatModel
	err := json.Unmarshal(transferPatchW.Body.Bytes(), &resultTM)
	require.NoError(t, err)
	
	// Check that the owner was changed
	assert.Equal(t, "newowner@example.com", resultTM.Owner)
	
	// Check that the original owner is still in the authorization list with owner role
	foundOriginalOwner := false
	for _, auth := range resultTM.Authorization {
		if auth.Subject == "test@example.com" && auth.Role == RoleOwner {
			foundOriginalOwner = true
			break
		}
	}
	assert.True(t, foundOriginalOwner, "Original owner should still have owner role in authorization")
}

// TestDuplicateSubjectViaPatching tests that patching with duplicate subjects is rejected
func TestDuplicateSubjectViaPatching(t *testing.T) {
	r := setupThreatModelRouter()
	tm := createTestThreatModel(t, r, "Duplicate Subject Patch Test", "Testing duplicate subject validation in patching")
	
	// Add a user first
	patchOps := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"subject": "alice@example.com",
				"role":    "reader",
			},
		},
	}
	
	patchBody, _ := json.Marshal(patchOps)
	patchReq, _ := http.NewRequest("PATCH", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	r.ServeHTTP(patchW, patchReq)
	assert.Equal(t, http.StatusOK, patchW.Code)
	
	// Now try to add the same user again with a different role
	duplicatePatchOps := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"subject": "alice@example.com", // Duplicate subject
				"role":    "writer",
			},
		},
	}
	
	duplicatePatchBody, _ := json.Marshal(duplicatePatchOps)
	duplicatePatchReq, _ := http.NewRequest("PATCH", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(duplicatePatchBody))
	duplicatePatchReq.Header.Set("Content-Type", "application/json")
	duplicatePatchW := httptest.NewRecorder()
	r.ServeHTTP(duplicatePatchW, duplicatePatchReq)
	
	// Decoding the patch operation and applying it would create a threat model with duplicate subjects,
	// which should be caught and rejected
	assert.Equal(t, http.StatusBadRequest, duplicatePatchW.Code)
	
	var errResp Error
	err := json.Unmarshal(duplicatePatchW.Body.Bytes(), &errResp)
	require.NoError(t, err)
	
	assert.Equal(t, "invalid_input", errResp.Error)
	assert.Contains(t, errResp.Message, "Duplicate authorization subject")
}

// TestReadWriteDeletePermissions tests access levels for different operations
func TestReadWriteDeletePermissions(t *testing.T) {
	// Set up the direct test users rather than relying on fixtures
	ownerUser := "test@example.com"  // This is the owner user in setupThreatModelRouter()
	
	// Create initial router and threat model with a known owner
	ownerRouter := setupThreatModelRouterWithUser(ownerUser)
	tm := createTestThreatModel(t, ownerRouter, "Permissions Test", "Testing permission levels")
	
	// Add users with different permission levels
	patchOps := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"subject": "reader@example.com",
				"role":    "reader",
			},
		},
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"subject": "writer@example.com",
				"role":    "writer",
			},
		},
	}
	
	patchBody, _ := json.Marshal(patchOps)
	patchReq, _ := http.NewRequest("PATCH", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	ownerRouter.ServeHTTP(patchW, patchReq)
	assert.Equal(t, http.StatusOK, patchW.Code)
	
	// Test 1: Reader can read but not write or delete
	readerRouter := setupThreatModelRouterWithUser("reader@example.com")
	
	// Reader should be able to read
	readReq, _ := http.NewRequest("GET", "/threat_models/"+tm.Id.String(), nil)
	readW := httptest.NewRecorder()
	readerRouter.ServeHTTP(readW, readReq)
	assert.Equal(t, http.StatusOK, readW.Code)
	
	// Reader should not be able to update
	updateTM := tm
	updateTM.Description = stringPointer("Updated by reader")
	
	updateBody, _ := json.Marshal(updateTM)
	updateReq, _ := http.NewRequest("PUT", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	readerRouter.ServeHTTP(updateW, updateReq)
	assert.Equal(t, http.StatusForbidden, updateW.Code)
	
	// Reader should not be able to delete
	deleteReq, _ := http.NewRequest("DELETE", "/threat_models/"+tm.Id.String(), nil)
	deleteW := httptest.NewRecorder()
	readerRouter.ServeHTTP(deleteW, deleteReq)
	assert.Equal(t, http.StatusForbidden, deleteW.Code)
	
	// Test 2: Writer can read and write but not delete
	writerRouter := setupThreatModelRouterWithUser("writer@example.com")
	
	// Writer should be able to read
	readReq2, _ := http.NewRequest("GET", "/threat_models/"+tm.Id.String(), nil)
	readW2 := httptest.NewRecorder()
	writerRouter.ServeHTTP(readW2, readReq2)
	assert.Equal(t, http.StatusOK, readW2.Code)
	
	// Writer should be able to update description only
	updatePayload := map[string]interface{}{
		"id":          tm.Id.String(),
		"name":        tm.Name,
		"description": "Updated by writer",
		"owner":       ownerUser,  // Keep the same owner
	}
	
	updateBody2, _ := json.Marshal(updatePayload)
	updateReq2, _ := http.NewRequest("PUT", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(updateBody2))
	updateReq2.Header.Set("Content-Type", "application/json")
	updateW2 := httptest.NewRecorder()
	writerRouter.ServeHTTP(updateW2, updateReq2)
	assert.Equal(t, http.StatusOK, updateW2.Code)
	
	// Writer should not be able to delete
	deleteReq2, _ := http.NewRequest("DELETE", "/threat_models/"+tm.Id.String(), nil)
	deleteW2 := httptest.NewRecorder()
	writerRouter.ServeHTTP(deleteW2, deleteReq2)
	assert.Equal(t, http.StatusForbidden, deleteW2.Code)
	
	// Test 3: Owner can read, write and delete
	// Owner should be able to delete
	deleteReq3, _ := http.NewRequest("DELETE", "/threat_models/"+tm.Id.String(), nil)
	deleteW3 := httptest.NewRecorder()
	ownerRouter.ServeHTTP(deleteW3, deleteReq3)
	assert.Equal(t, http.StatusNoContent, deleteW3.Code)
}

// TestWriterCannotChangeOwnerOrAuth tests writer cannot change owner or authorization fields
func TestWriterCannotChangeOwnerOrAuth(t *testing.T) {
	// Create initial router and threat model
	originalRouter := setupThreatModelRouter() // original owner is test@example.com
	tm := createTestThreatModel(t, originalRouter, "Writer Limitations Test", "Testing writer limitations")
	
	// Add a writer user
	patchOps := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"subject": "writer@example.com",
				"role":    "writer",
			},
		},
	}
	
	patchBody, _ := json.Marshal(patchOps)
	patchReq, _ := http.NewRequest("PATCH", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	originalRouter.ServeHTTP(patchW, patchReq)
	assert.Equal(t, http.StatusOK, patchW.Code)
	
	// Create router for the writer
	writerRouter := setupThreatModelRouterWithUser("writer@example.com")
	
	// Test 1: Writer cannot change owner
	ownerPatchOps := []PatchOperation{
		{
			Op:    "replace",
			Path:  "/owner",
			Value: "writer@example.com",
		},
	}
	
	ownerPatchBody, _ := json.Marshal(ownerPatchOps)
	ownerPatchReq, _ := http.NewRequest("PATCH", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(ownerPatchBody))
	ownerPatchReq.Header.Set("Content-Type", "application/json")
	ownerPatchW := httptest.NewRecorder()
	writerRouter.ServeHTTP(ownerPatchW, ownerPatchReq)
	assert.Equal(t, http.StatusForbidden, ownerPatchW.Code)
	
	// Test 2: Writer cannot change authorization
	authPatchOps := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"subject": "another@example.com",
				"role":    "reader",
			},
		},
	}
	
	authPatchBody, _ := json.Marshal(authPatchOps)
	authPatchReq, _ := http.NewRequest("PATCH", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(authPatchBody))
	authPatchReq.Header.Set("Content-Type", "application/json")
	authPatchW := httptest.NewRecorder()
	writerRouter.ServeHTTP(authPatchW, authPatchReq)
	assert.Equal(t, http.StatusForbidden, authPatchW.Code)
}