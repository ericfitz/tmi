package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupDiagramRouter returns a router with diagram handlers registered for the owner user
func setupDiagramRouter() *gin.Engine {
	// Initialize test fixtures first
	InitTestFixtures()
	return setupDiagramRouterWithUser(TestFixtures.OwnerUser)
}

// setupDiagramRouterWithUser returns a router with diagram handlers registered and specified user
func setupDiagramRouterWithUser(userName string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	
	// Test fixtures should already be initialized by setupDiagramRouter
	
	// Add a fake auth middleware to set user in context
	r.Use(func(c *gin.Context) {
		fmt.Printf("[TEST DEBUG] User name: %s, Request: %s %s\n", 
			userName, c.Request.Method, c.Request.URL.Path)
		c.Set("userName", userName)
		c.Next()
	})
	
	// Add our authorization middleware
	r.Use(DiagramMiddleware())
	
	// Register diagram routes
	handler := NewDiagramHandler()
	r.GET("/diagrams", handler.GetDiagrams)
	r.POST("/diagrams", handler.CreateDiagram)
	r.GET("/diagrams/:id", handler.GetDiagramByID)
	r.PUT("/diagrams/:id", handler.UpdateDiagram)
	r.PATCH("/diagrams/:id", handler.PatchDiagram)
	r.DELETE("/diagrams/:id", handler.DeleteDiagram)
	r.GET("/diagrams/:id/collaborate", handler.GetDiagramCollaborate)
	r.POST("/diagrams/:id/collaborate", handler.PostDiagramCollaborate)
	r.DELETE("/diagrams/:id/collaborate", handler.DeleteDiagramCollaborate)
	
	return r
}

// TestCreateDiagram tests creating a new diagram
func TestCreateDiagram(t *testing.T) {
	r := setupDiagramRouter()
	
	// Create request body
	reqBody := map[string]interface{}{
		"name":        "Test Diagram",
		"description": "This is a test diagram",
	}
	
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/diagrams", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	
	// Debug output for response
	fmt.Printf("[TEST RESPONSE] Status: %d, Body: %s\n", w.Code, w.Body.String())
	
	// Assert response
	assert.Equal(t, http.StatusCreated, w.Code)
	
	// Parse response
	var d Diagram
	err := json.Unmarshal(w.Body.Bytes(), &d)
	require.NoError(t, err)
	
	// Check fields
	assert.Equal(t, "Test Diagram", d.Name)
	assert.NotNil(t, d.Description)
	assert.Equal(t, "This is a test diagram", *d.Description)
	assert.Equal(t, "test@example.com", d.Owner)
	assert.NotEmpty(t, d.Id)
	assert.Len(t, d.Authorization, 1)
	assert.Equal(t, "test@example.com", d.Authorization[0].Subject)
	assert.Equal(t, Owner, d.Authorization[0].Role)
}

// createTestDiagram creates a test diagram and returns it
func createTestDiagram(t *testing.T, router *gin.Engine, name string, description string) Diagram {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"name":        name,
		"description": description,
	})
	
	req, _ := http.NewRequest("POST", "/diagrams", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusCreated, w.Code)
	
	var d Diagram
	err := json.Unmarshal(w.Body.Bytes(), &d)
	require.NoError(t, err)
	
	return d
}

// TestCreateDiagramWithDuplicateSubjects tests creating a diagram with duplicate subjects
func TestCreateDiagramWithDuplicateSubjects(t *testing.T) {
	r := setupDiagramRouter()
	
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
	req, _ := http.NewRequest("POST", "/diagrams", bytes.NewBuffer(body))
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

// TestCreateDiagramWithDuplicateOwner tests creating a diagram with a subject that duplicates the owner
func TestCreateDiagramWithDuplicateOwner(t *testing.T) {
	r := setupDiagramRouter()
	
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
	req, _ := http.NewRequest("POST", "/diagrams", bytes.NewBuffer(body))
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

// TestUpdateDiagramOwnerChange tests the rule that when the owner changes, the original owner
// is added to the authorization list with owner role
func TestUpdateDiagramOwnerChange(t *testing.T) {
	// Create initial router and diagram
	originalRouter := setupDiagramRouter() // original owner is test@example.com
	d := createTestDiagram(t, originalRouter, "Owner Change Test", "Testing owner change rules")
	
	// Print the created diagram for debugging
	fmt.Printf("[TEST DEBUG] Created diagram: %+v\n", d)
	
	// Now create a new router with a different user
	newOwnerRouter := setupDiagramRouterWithUser("newowner@example.com")
	
	// First, give the new user access to the diagram
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
	fmt.Printf("[TEST DEBUG] PATCH request body: %s\n", string(patchBody))
	patchReq, _ := http.NewRequest("PATCH", "/diagrams/"+d.Id.String(), bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	originalRouter.ServeHTTP(patchW, patchReq)
	fmt.Printf("[TEST DEBUG] PATCH response code: %d\n", patchW.Code)
	fmt.Printf("[TEST DEBUG] PATCH response body: %s\n", patchW.Body.String())
	assert.Equal(t, http.StatusOK, patchW.Code)
	
	// Now, as the new user, change the owner
	updatedDiagram := d
	updatedDiagram.Owner = "newowner@example.com"
	
	// Remove the original owner from the authorization list to test that it gets added back
	updatedDiagram.Authorization = []Authorization{
		{
			Subject: "newowner@example.com",
			Role:    Owner,
		},
	}
	
	updateBody, _ := json.Marshal(updatedDiagram)
	updateReq, _ := http.NewRequest("PUT", "/diagrams/"+d.Id.String(), bytes.NewBuffer(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	
	newOwnerRouter.ServeHTTP(updateW, updateReq)
	
	// Assert response
	assert.Equal(t, http.StatusOK, updateW.Code)
	
	var resultDiagram Diagram
	err := json.Unmarshal(updateW.Body.Bytes(), &resultDiagram)
	require.NoError(t, err)
	
	// Check that the owner was changed
	assert.Equal(t, "newowner@example.com", resultDiagram.Owner)
	
	// Check that the original owner is still in the authorization list with owner role
	foundOriginalOwner := false
	for _, auth := range resultDiagram.Authorization {
		if auth.Subject == "test@example.com" && auth.Role == Owner {
			foundOriginalOwner = true
			break
		}
	}
	assert.True(t, foundOriginalOwner, "Original owner should still have owner role in authorization")
}

// TestUpdateDiagramWithDuplicateSubjects tests updating a diagram with duplicate subjects
func TestUpdateDiagramWithDuplicateSubjects(t *testing.T) {
	r := setupDiagramRouter()
	d := createTestDiagram(t, r, "Duplicate Subject Update Test", "Testing duplicate subject validation")
	
	// Now try to update with duplicate subjects
	updatedDiagram := d
	updatedDiagram.Authorization = []Authorization{
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
	
	updateBody, _ := json.Marshal(updatedDiagram)
	updateReq, _ := http.NewRequest("PUT", "/diagrams/"+d.Id.String(), bytes.NewBuffer(updateBody))
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

// TestNonOwnerCannotChangeDiagramOwner tests that a non-owner user cannot change the owner
func TestNonOwnerCannotChangeDiagramOwner(t *testing.T) {
	// Create initial router and diagram
	originalRouter := setupDiagramRouter() // original owner is test@example.com
	d := createTestDiagram(t, originalRouter, "Owner Protection Test", "Testing owner protection rules")
	
	// Add a reader user to the diagram
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
	patchReq, _ := http.NewRequest("PATCH", "/diagrams/"+d.Id.String(), bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	originalRouter.ServeHTTP(patchW, patchReq)
	assert.Equal(t, http.StatusOK, patchW.Code)
	
	// Now create a router with the reader user
	readerRouter := setupDiagramRouterWithUser("reader@example.com")
	
	// Try to change the owner as the reader
	updatedDiagram := d
	updatedDiagram.Owner = "reader@example.com"
	
	updateBody, _ := json.Marshal(updatedDiagram)
	updateReq, _ := http.NewRequest("PUT", "/diagrams/"+d.Id.String(), bytes.NewBuffer(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	
	readerRouter.ServeHTTP(updateW, updateReq)
	
	// Assert response - should be forbidden
	assert.Equal(t, http.StatusForbidden, updateW.Code)
	
	var errResp Error
	err := json.Unmarshal(updateW.Body.Bytes(), &errResp)
	require.NoError(t, err)
	
	assert.Equal(t, "forbidden", errResp.Error)
	assert.Contains(t, errResp.Message, "You don't have sufficient permissions")
}

// TestOwnershipTransferViaPatchingDiagram tests changing ownership via PATCH operation
func TestOwnershipTransferViaPatchingDiagram(t *testing.T) {
	// Create initial router and diagram
	originalRouter := setupDiagramRouter() // original owner is test@example.com
	d := createTestDiagram(t, originalRouter, "Owner Patch Test", "Testing owner patching rules")
	
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
	patchReq, _ := http.NewRequest("PATCH", "/diagrams/"+d.Id.String(), bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	originalRouter.ServeHTTP(patchW, patchReq)
	assert.Equal(t, http.StatusOK, patchW.Code)
	
	// Now create a router with the new owner
	newOwnerRouter := setupDiagramRouterWithUser("newowner@example.com")
	
	// Now transfer ownership via PATCH
	transferPatchOps := []PatchOperation{
		{
			Op:    "replace",
			Path:  "/owner",
			Value: "newowner@example.com",
		},
	}
	
	transferPatchBody, _ := json.Marshal(transferPatchOps)
	transferPatchReq, _ := http.NewRequest("PATCH", "/diagrams/"+d.Id.String(), bytes.NewBuffer(transferPatchBody))
	transferPatchReq.Header.Set("Content-Type", "application/json")
	transferPatchW := httptest.NewRecorder()
	
	newOwnerRouter.ServeHTTP(transferPatchW, transferPatchReq)
	
	// Print the response for debugging
	fmt.Printf("[TEST DEBUG] PATCH Owner Change Response Code: %d\n", transferPatchW.Code)
	fmt.Printf("[TEST DEBUG] PATCH Owner Change Response Body: %s\n", transferPatchW.Body.String())
	
	// Assert response
	assert.Equal(t, http.StatusOK, transferPatchW.Code)
	
	var resultDiagram Diagram
	err := json.Unmarshal(transferPatchW.Body.Bytes(), &resultDiagram)
	require.NoError(t, err)
	
	// Check that the owner was changed
	assert.Equal(t, "newowner@example.com", resultDiagram.Owner)
	
	// Check that the original owner is still in the authorization list with owner role
	foundOriginalOwner := false
	for _, auth := range resultDiagram.Authorization {
		if auth.Subject == "test@example.com" && auth.Role == Owner {
			foundOriginalOwner = true
			break
		}
	}
	assert.True(t, foundOriginalOwner, "Original owner should still have owner role in authorization")
}

// TestDuplicateSubjectViaPatchingDiagram tests that patching with duplicate subjects is rejected
func TestDuplicateSubjectViaPatchingDiagram(t *testing.T) {
	r := setupDiagramRouter()
	d := createTestDiagram(t, r, "Duplicate Subject Patch Test", "Testing duplicate subject validation in patching")
	
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
	patchReq, _ := http.NewRequest("PATCH", "/diagrams/"+d.Id.String(), bytes.NewBuffer(patchBody))
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
	duplicatePatchReq, _ := http.NewRequest("PATCH", "/diagrams/"+d.Id.String(), bytes.NewBuffer(duplicatePatchBody))
	duplicatePatchReq.Header.Set("Content-Type", "application/json")
	duplicatePatchW := httptest.NewRecorder()
	r.ServeHTTP(duplicatePatchW, duplicatePatchReq)
	
	// Decoding the patch operation and applying it would create a diagram with duplicate subjects,
	// which should be caught and rejected
	assert.Equal(t, http.StatusBadRequest, duplicatePatchW.Code)
}

// TestReadWriteDeletePermissionsDiagram tests access levels for different operations
func TestReadWriteDeletePermissionsDiagram(t *testing.T) {
	// Reset stores and create a fresh test diagram
	ResetStores()
	originalRouter := setupDiagramRouter() // original owner is test@example.com
	d := createTestDiagram(t, originalRouter, "Permissions Test", "Testing permission levels")
	
	// Store the diagram ID for reference
	diagramID := d.Id.String()
	t.Logf("Created diagram ID: %s", diagramID)
	
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
	patchReq, _ := http.NewRequest("PATCH", "/diagrams/"+diagramID, bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	originalRouter.ServeHTTP(patchW, patchReq)
	assert.Equal(t, http.StatusOK, patchW.Code)
	
	// Test 1: Reader can read but not write or delete
	readerRouter := setupDiagramRouterWithUser("reader@example.com")
	
	// Reader should be able to read
	readReq, _ := http.NewRequest("GET", "/diagrams/"+diagramID, nil)
	readW := httptest.NewRecorder()
	readerRouter.ServeHTTP(readW, readReq)
	assert.Equal(t, http.StatusOK, readW.Code)
	
	// Get the current diagram state
	currentDiagram, err := DiagramStore.Get(diagramID)
	assert.NoError(t, err)
	
	// Reader should not be able to update
	updatedDiagram := currentDiagram
	updatedDiagram.Description = stringPointer("Updated by reader")
	
	updateBody, _ := json.Marshal(updatedDiagram)
	updateReq, _ := http.NewRequest("PUT", "/diagrams/"+diagramID, bytes.NewBuffer(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	readerRouter.ServeHTTP(updateW, updateReq)
	assert.Equal(t, http.StatusForbidden, updateW.Code)
	
	// Reader should not be able to delete
	deleteReq, _ := http.NewRequest("DELETE", "/diagrams/"+diagramID, nil)
	deleteW := httptest.NewRecorder()
	readerRouter.ServeHTTP(deleteW, deleteReq)
	assert.Equal(t, http.StatusForbidden, deleteW.Code)
	
	// Test 2: Writer can read and write but not delete
	writerRouter := setupDiagramRouterWithUser("writer@example.com")
	
	// Writer should be able to read
	readReq2, _ := http.NewRequest("GET", "/diagrams/"+diagramID, nil)
	readW2 := httptest.NewRecorder()
	writerRouter.ServeHTTP(readW2, readReq2)
	assert.Equal(t, http.StatusOK, readW2.Code)
	
	// Get the current diagram state again
	currentDiagram, err = DiagramStore.Get(diagramID)
	assert.NoError(t, err)
	
	// Writer should be able to update the description but not auth fields
	// Create a minimal update payload with just the fields they're allowed to change
	updatePayload := map[string]interface{}{
		"id":          diagramID,
		"name":        currentDiagram.Name,
		"description": "Updated by writer",
		"owner":       currentDiagram.Owner,  // Keep the same owner
	}
	
	updateBody2, _ := json.Marshal(updatePayload)
	updateReq2, _ := http.NewRequest("PUT", "/diagrams/"+diagramID, bytes.NewBuffer(updateBody2))
	updateReq2.Header.Set("Content-Type", "application/json")
	updateW2 := httptest.NewRecorder()
	writerRouter.ServeHTTP(updateW2, updateReq2)
	assert.Equal(t, http.StatusOK, updateW2.Code)
	
	// Writer should not be able to delete
	deleteReq2, _ := http.NewRequest("DELETE", "/diagrams/"+diagramID, nil)
	deleteW2 := httptest.NewRecorder()
	writerRouter.ServeHTTP(deleteW2, deleteReq2)
	assert.Equal(t, http.StatusForbidden, deleteW2.Code)
	
	// Test 3: Owner can read, write and delete
	// Owner should be able to delete
	deleteReq3, _ := http.NewRequest("DELETE", "/diagrams/"+diagramID, nil)
	deleteW3 := httptest.NewRecorder()
	originalRouter.ServeHTTP(deleteW3, deleteReq3)
	assert.Equal(t, http.StatusNoContent, deleteW3.Code)
}

// TestWriterCannotChangeDiagramOwnerOrAuth tests writer cannot change owner or authorization fields
func TestWriterCannotChangeDiagramOwnerOrAuth(t *testing.T) {
	// Create initial router and diagram
	originalRouter := setupDiagramRouter() // original owner is test@example.com
	d := createTestDiagram(t, originalRouter, "Writer Limitations Test", "Testing writer limitations")
	
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
	patchReq, _ := http.NewRequest("PATCH", "/diagrams/"+d.Id.String(), bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	originalRouter.ServeHTTP(patchW, patchReq)
	assert.Equal(t, http.StatusOK, patchW.Code)
	
	// Create router for the writer
	writerRouter := setupDiagramRouterWithUser("writer@example.com")
	
	// Test 1: Writer cannot change owner
	ownerPatchOps := []PatchOperation{
		{
			Op:    "replace",
			Path:  "/owner",
			Value: "writer@example.com",
		},
	}
	
	ownerPatchBody, _ := json.Marshal(ownerPatchOps)
	ownerPatchReq, _ := http.NewRequest("PATCH", "/diagrams/"+d.Id.String(), bytes.NewBuffer(ownerPatchBody))
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
	authPatchReq, _ := http.NewRequest("PATCH", "/diagrams/"+d.Id.String(), bytes.NewBuffer(authPatchBody))
	authPatchReq.Header.Set("Content-Type", "application/json")
	authPatchW := httptest.NewRecorder()
	writerRouter.ServeHTTP(authPatchW, authPatchReq)
	assert.Equal(t, http.StatusForbidden, authPatchW.Code)
}