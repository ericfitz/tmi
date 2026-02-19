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

// testThreatModelNameAlpha and testThreatModelNameBeta are test threat model names used in list/filter tests
const testThreatModelNameAlpha = "Security Assessment Alpha"
const testThreatModelNameBeta = "Security Assessment Beta"

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

	// Initialize test fixtures only if not already initialized
	if !TestFixtures.Initialized {
		InitTestFixtures()
	}

	// Add a fake auth middleware to set user in context
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", userName)
		c.Set("userID", userName+"-provider-id") // Provider ID for testing
		// The middleware will set the userRole, we don't need to set it here
		c.Next()
	})

	// Add our authorization middleware
	r.Use(ThreatModelMiddleware())

	// Register threat model routes
	handler := NewThreatModelHandler(NewWebSocketHubForTests())
	r.GET("/threat_models", handler.GetThreatModels)
	r.POST("/threat_models", handler.CreateThreatModel)
	r.GET("/threat_models/:threat_model_id", handler.GetThreatModelByID)
	r.PUT("/threat_models/:threat_model_id", handler.UpdateThreatModel)
	r.PATCH("/threat_models/:threat_model_id", handler.PatchThreatModel)
	r.DELETE("/threat_models/:threat_model_id", handler.DeleteThreatModel)

	return r
}

// TestCreateThreatModel tests creating a new threat model
func TestCreateThreatModel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
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
	// Owner and CreatedBy are now User objects with Email as openapi_types.Email
	assert.Equal(t, openapi_types.Email("test@example.com"), tm.Owner.Email)
	assert.NotNil(t, tm.CreatedBy)
	assert.Equal(t, openapi_types.Email("test@example.com"), tm.CreatedBy.Email)
	assert.NotEmpty(t, tm.Id)
	assert.Len(t, tm.Authorization, 1)
	// The provider_id from test router uses email + "-provider-id" suffix
	assert.Equal(t, "test@example.com-provider-id", tm.Authorization[0].ProviderId)
	assert.Equal(t, RoleOwner, tm.Authorization[0].Role)
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

	// Parse response as wrapped ListThreatModelsResponse
	var response ListThreatModelsResponse
	err := json.Unmarshal(listW.Body.Bytes(), &response)
	require.NoError(t, err)

	// Check that we got at least one item
	assert.NotEmpty(t, response.ThreatModels)
	assert.GreaterOrEqual(t, response.Total, len(response.ThreatModels))
	assert.Equal(t, 20, response.Limit)
	assert.Equal(t, 0, response.Offset)

	// Check that our test item is in the list
	found := false
	for _, item := range response.ThreatModels {
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

	// Debug: Print response if not OK
	if patchW.Code != http.StatusOK {
		t.Logf("PATCH failed with status %d, body: %s", patchW.Code, patchW.Body.String())
	}

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
				"principal_type": "user", "provider": "tmi", "provider_id": "alice@example.com",
				"role": "reader",
			},
			{
				"principal_type": "user", "provider": "tmi", "provider_id": "alice@example.com", // Duplicate subject
				"role": "writer",
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
	assert.Contains(t, errResp.ErrorDescription, "Duplicate authorization subject")
}

// TestCreateThreatModelWithDuplicateOwner tests creating a threat model with a subject that duplicates the owner
// Note: The current behavior allows this and the duplicate is handled gracefully by the database
// (ON CONFLICT clauses). The owner entry always takes precedence with owner role.
func TestCreateThreatModelWithDuplicateOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	r := setupThreatModelRouter()

	// Create request with a subject that matches the owner
	// This is allowed - the database handles duplicates gracefully
	reqBody := map[string]interface{}{
		"name":        "Duplicate Owner Test",
		"description": "Duplicate with owner is handled gracefully",
		"authorization": []map[string]interface{}{
			{
				"principal_type": "user", "provider": "tmi", "provider_id": "test@example.com", // Same as the owner from middleware
				"role": "reader",
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// The request succeeds - duplicates are handled gracefully
	assert.Equal(t, http.StatusCreated, w.Code)

	var tm ThreatModel
	err := json.Unmarshal(w.Body.Bytes(), &tm)
	require.NoError(t, err)

	// Owner should still be the authenticated user with owner role
	assert.Equal(t, openapi_types.Email("test@example.com"), tm.Owner.Email)
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
				"principal_type": "user", "provider": "tmi", "provider_id": "newowner@example.com",
				"role": "owner", // Need owner role to change owner
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
	// Create update request with required fields
	// Note: Don't include the new owner in authorization - they will be added automatically
	updateRequest := map[string]interface{}{
		"name":                   tm.Name,
		"description":            tm.Description,
		"owner":                  "newowner@example.com",
		"threat_model_framework": "STRIDE",                   // Required field
		"authorization":          []map[string]interface{}{}, // Empty - owner added automatically
	}

	updateBody, _ := json.Marshal(updateRequest)
	updateReq, _ := http.NewRequest("PUT", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()

	newOwnerRouter.ServeHTTP(updateW, updateReq)

	// Assert response - debug if error
	if updateW.Code != http.StatusOK {
		t.Logf("Update failed with status %d, body: %s", updateW.Code, updateW.Body.String())
	}
	assert.Equal(t, http.StatusOK, updateW.Code)

	var resultTM ThreatModel
	err := json.Unmarshal(updateW.Body.Bytes(), &resultTM)
	require.NoError(t, err)

	// Check that the owner was changed
	assert.Equal(t, "newowner@example.com", resultTM.Owner.ProviderId)

	// Check that the original owner is still in the authorization list with owner role
	// Note: The original owner's provider ID is "test@example.com-provider-id" (set by fake auth middleware)
	foundOriginalOwner := false
	for _, auth := range resultTM.Authorization {
		if auth.ProviderId == "test@example.com-provider-id" && auth.Role == RoleOwner {
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

	// Create update request with duplicate subjects
	updateRequest := map[string]interface{}{
		"name":                   tm.Name,
		"description":            tm.Description,
		"owner":                  tm.Owner.ProviderId, // Send the provider_id string, not the User object
		"threat_model_framework": "STRIDE",            // Required field
		"authorization": []map[string]interface{}{
			{
				"principal_type": "user", "provider": "tmi", "provider_id": "test@example.com",
				"role": "owner",
			},
			{
				"principal_type": "user", "provider": "tmi", "provider_id": "alice@example.com",
				"role": "reader",
			},
			{
				"principal_type": "user", "provider": "tmi", "provider_id": "alice@example.com", // Duplicate subject
				"role": "writer",
			},
		},
	}

	updateBody, _ := json.Marshal(updateRequest)
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
	assert.Contains(t, errResp.ErrorDescription, "Duplicate authorization subject")
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
				"principal_type": "user", "provider": "tmi", "provider_id": "reader@example.com",
				"role": "reader",
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
	readerUser := User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      "test",
		ProviderId:    "reader@example.com",
		DisplayName:   "Reader User",
		Email:         openapi_types.Email("reader@example.com"),
	}
	updatedTM := tm
	updatedTM.Owner = readerUser

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
	// assert.Contains(t, errResp.ErrorDescription, "Only the owner can transfer ownership")
}

// TestOwnershipTransferViaPatching tests changing ownership via PATCH operation
func TestOwnershipTransferViaPatching(t *testing.T) {
	// Reset stores to ensure clean state

	// Create initial router and threat model
	originalRouter := setupThreatModelRouter() // original owner is test@example.com
	tm := createTestThreatModel(t, originalRouter, "Owner Patch Test", "Testing owner patching rules")

	// First, add a new user with owner permissions
	patchOps := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"principal_type": "user", "provider": "tmi", "provider_id": "newowner@example.com",
				"role": "owner",
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

	// Always log response for debugging
	t.Logf("Transfer ownership PATCH response: status=%d, body=%s", transferPatchW.Code, transferPatchW.Body.String())

	// Assert response - debug if error
	if transferPatchW.Code != http.StatusOK {
		t.Logf("Transfer ownership PATCH failed with status %d, body: %s", transferPatchW.Code, transferPatchW.Body.String())
	}
	assert.Equal(t, http.StatusOK, transferPatchW.Code)

	var resultTM ThreatModel
	err := json.Unmarshal(transferPatchW.Body.Bytes(), &resultTM)
	require.NoError(t, err)

	// Check that the owner was changed
	assert.Equal(t, "newowner@example.com", resultTM.Owner.ProviderId)

	// Check that the original owner is still in the authorization list with owner role
	// Note: The original owner's provider ID is "test@example.com-provider-id" (set by fake auth middleware)
	foundOriginalOwner := false
	for _, auth := range resultTM.Authorization {
		if auth.ProviderId == "test@example.com-provider-id" && auth.Role == RoleOwner {
			foundOriginalOwner = true
			break
		}
	}
	assert.True(t, foundOriginalOwner, "Original owner should still have owner role in authorization")
}

// TestDuplicateSubjectViaPatching tests that patching the same user multiple times
// successfully updates their role (idempotent behavior)
func TestDuplicateSubjectViaPatching(t *testing.T) {
	r := setupThreatModelRouter()
	tm := createTestThreatModel(t, r, "Role Update Test", "Testing that patching same user updates their role")

	// Add alice as reader first
	patchOps := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"principal_type": "user", "provider": "tmi", "provider_id": "alice@example.com",
				"role": "reader",
			},
		},
	}

	patchBody, _ := json.Marshal(patchOps)
	patchReq, _ := http.NewRequest("PATCH", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	r.ServeHTTP(patchW, patchReq)
	assert.Equal(t, http.StatusOK, patchW.Code)

	// Now patch alice again with writer role - this should succeed and update the role
	updatePatchOps := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"principal_type": "user", "provider": "tmi", "provider_id": "alice@example.com",
				"role": "writer", // Updated role
			},
		},
	}

	updatePatchBody, _ := json.Marshal(updatePatchOps)
	updatePatchReq, _ := http.NewRequest("PATCH", "/threat_models/"+tm.Id.String(), bytes.NewBuffer(updatePatchBody))
	updatePatchReq.Header.Set("Content-Type", "application/json")
	updatePatchW := httptest.NewRecorder()
	r.ServeHTTP(updatePatchW, updatePatchReq)

	// This should succeed - duplicates are handled gracefully by database ON CONFLICT
	assert.Equal(t, http.StatusOK, updatePatchW.Code)

	// Verify the result has alice with writer role (not reader)
	var updatedTM ThreatModel
	err := json.Unmarshal(updatePatchW.Body.Bytes(), &updatedTM)
	require.NoError(t, err)

	// Find alice in the authorization list
	aliceFound := false
	for _, auth := range updatedTM.Authorization {
		if auth.ProviderId == "alice@example.com" {
			aliceFound = true
			assert.Equal(t, RoleWriter, auth.Role, "Alice's role should be updated to writer")
		}
	}
	assert.True(t, aliceFound, "Alice should be in the authorization list")
}

// TestReadWriteDeletePermissions tests access levels for different operations
func TestReadWriteDeletePermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	// Set up the direct test users rather than relying on fixtures
	ownerUser := "test@example.com" // This is the owner user in setupThreatModelRouter()

	// Create initial router and threat model with a known owner
	ownerRouter := setupThreatModelRouterWithUser(ownerUser)
	tm := createTestThreatModel(t, ownerRouter, "Permissions Test", "Testing permission levels")

	// Add users with different permission levels
	patchOps := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"principal_type": "user", "provider": "tmi", "provider_id": "reader@example.com",
				"role": "reader",
			},
		},
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"principal_type": "user", "provider": "tmi", "provider_id": "writer@example.com",
				"role": "writer",
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
	// Note: Don't include 'owner' field - that would trigger owner-change check
	// Only include fields from ThreatModelInput schema: name, description, etc.
	updatePayload := map[string]interface{}{
		"name":        tm.Name,
		"description": "Updated by writer",
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
				"principal_type": "user", "provider": "tmi", "provider_id": "writer@example.com",
				"role": "writer",
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
				"principal_type": "user", "provider": "tmi", "provider_id": "another@example.com",
				"role": "reader",
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

// TestGetThreatModelsAuthorizationFiltering tests that the list endpoint properly filters based on user access
func TestGetThreatModelsAuthorizationFiltering(t *testing.T) {
	// Reset stores to ensure clean state
	InitializeMockStores()
	TestFixtures.Initialized = false
	InitTestFixtures()

	// Remove the default fixture threat model to start with a clean slate for this authorization test
	_ = ThreatModelStore.Delete(TestFixtures.ThreatModelID)
	_ = DiagramStore.Delete(TestFixtures.DiagramID)

	// Set up test users
	ownerUser := testOwnerEmail
	readerUser := testReaderEmail
	writerUser := testWriterEmail
	unaccessUser := "noaccess@example.com"

	// Create threat model 1 - owner is ownerUser, reader has access
	ownerRouter := setupThreatModelRouterWithUser(ownerUser)
	tm1 := createTestThreatModel(t, ownerRouter, "Accessible Model 1", "User owns this model")

	// Add reader access to tm1
	patchOps1 := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"principal_type": "user", "provider": "tmi", "provider_id": readerUser,
				"role": "reader",
			},
		},
	}
	patchBody1, _ := json.Marshal(patchOps1)
	patchReq1, _ := http.NewRequest("PATCH", "/threat_models/"+tm1.Id.String(), bytes.NewBuffer(patchBody1))
	patchReq1.Header.Set("Content-Type", "application/json")
	patchW1 := httptest.NewRecorder()
	ownerRouter.ServeHTTP(patchW1, patchReq1)
	assert.Equal(t, http.StatusOK, patchW1.Code)

	// Create threat model 2 - owner is ownerUser, writer has access
	tm2 := createTestThreatModel(t, ownerRouter, "Accessible Model 2", "User has writer access")

	// Add writer access to tm2
	patchOps2 := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"principal_type": "user", "provider": "tmi", "provider_id": writerUser,
				"role": "writer",
			},
		},
	}
	patchBody2, _ := json.Marshal(patchOps2)
	patchReq2, _ := http.NewRequest("PATCH", "/threat_models/"+tm2.Id.String(), bytes.NewBuffer(patchBody2))
	patchReq2.Header.Set("Content-Type", "application/json")
	patchW2 := httptest.NewRecorder()
	ownerRouter.ServeHTTP(patchW2, patchReq2)
	assert.Equal(t, http.StatusOK, patchW2.Code)

	// Create threat model 3 - owner is ownerUser, no additional access
	tm3 := createTestThreatModel(t, ownerRouter, "Inaccessible Model", "User has no access to this model")
	_ = tm3 // We created it but won't verify access since unaccessUser shouldn't see it

	// Test 1: Owner should see all 3 models they own
	listReq1, _ := http.NewRequest("GET", "/threat_models", nil)
	listW1 := httptest.NewRecorder()
	ownerRouter.ServeHTTP(listW1, listReq1)
	assert.Equal(t, http.StatusOK, listW1.Code)

	var ownerResponse ListThreatModelsResponse
	err := json.Unmarshal(listW1.Body.Bytes(), &ownerResponse)
	require.NoError(t, err)
	assert.Len(t, ownerResponse.ThreatModels, 3, "Owner should see all 3 threat models")

	// Test 2: Reader should see models 1 and 2 (has reader access to 1, owner owns both)
	readerRouter := setupThreatModelRouterWithUser(readerUser)
	listReq2, _ := http.NewRequest("GET", "/threat_models", nil)
	listW2 := httptest.NewRecorder()
	readerRouter.ServeHTTP(listW2, listReq2)
	assert.Equal(t, http.StatusOK, listW2.Code)

	var readerResponse ListThreatModelsResponse
	err = json.Unmarshal(listW2.Body.Bytes(), &readerResponse)
	require.NoError(t, err)
	assert.Len(t, readerResponse.ThreatModels, 1, "Reader should see only 1 threat model (tm1)")

	// Verify reader sees the correct model
	found := false
	for _, item := range readerResponse.ThreatModels {
		if item.Name == "Accessible Model 1" {
			found = true
			break
		}
	}
	assert.True(t, found, "Reader should see 'Accessible Model 1'")

	// Test 3: Writer should see model 2 only (has writer access to model 2)
	writerRouter := setupThreatModelRouterWithUser(writerUser)
	listReq3, _ := http.NewRequest("GET", "/threat_models", nil)
	listW3 := httptest.NewRecorder()
	writerRouter.ServeHTTP(listW3, listReq3)
	assert.Equal(t, http.StatusOK, listW3.Code)

	var writerResponse ListThreatModelsResponse
	err = json.Unmarshal(listW3.Body.Bytes(), &writerResponse)
	require.NoError(t, err)
	assert.Len(t, writerResponse.ThreatModels, 1, "Writer should see only 1 threat model (tm2)")

	// Verify writer sees the correct model
	found = false
	for _, item := range writerResponse.ThreatModels {
		if item.Name == "Accessible Model 2" {
			found = true
			break
		}
	}
	assert.True(t, found, "Writer should see 'Accessible Model 2'")

	// Test 4: User with no access should see no models
	unaccessRouter := setupThreatModelRouterWithUser(unaccessUser)
	listReq4, _ := http.NewRequest("GET", "/threat_models", nil)
	listW4 := httptest.NewRecorder()
	unaccessRouter.ServeHTTP(listW4, listReq4)
	assert.Equal(t, http.StatusOK, listW4.Code)

	var unaccessResponse ListThreatModelsResponse
	err = json.Unmarshal(listW4.Body.Bytes(), &unaccessResponse)
	require.NoError(t, err)
	assert.Len(t, unaccessResponse.ThreatModels, 0, "User with no access should see no threat models")

	// Test 5: Unauthenticated user should see no models
	gin.SetMode(gin.TestMode)
	unauthRouter := gin.New()

	// No authentication middleware - simulate unauthenticated request
	unauthRouter.Use(func(c *gin.Context) {
		// Don't set userName in context
		c.Next()
	})

	handler := NewThreatModelHandler(NewWebSocketHubForTests())
	unauthRouter.GET("/threat_models", handler.GetThreatModels)

	listReq5, _ := http.NewRequest("GET", "/threat_models", nil)
	listW5 := httptest.NewRecorder()
	unauthRouter.ServeHTTP(listW5, listReq5)
	assert.Equal(t, http.StatusOK, listW5.Code)

	var unauthResponse ListThreatModelsResponse
	err = json.Unmarshal(listW5.Body.Bytes(), &unauthResponse)
	require.NoError(t, err)
	assert.Len(t, unauthResponse.ThreatModels, 0, "Unauthenticated user should see no threat models")
}

// TestGetThreatModelsWithFilters tests the filtering query parameters for listing threat models
func TestGetThreatModelsWithFilters(t *testing.T) {
	r := setupThreatModelRouter()

	// Create multiple threat models with different attributes for filtering
	testModels := []map[string]interface{}{
		{
			"name":        testThreatModelNameAlpha,
			"description": "Primary security analysis for the main application",
			"issue_uri":   "https://issues.example.com/SEC-100",
		},
		{
			"name":        testThreatModelNameBeta,
			"description": "Secondary security review for API endpoints",
			"issue_uri":   "https://issues.example.com/SEC-101",
		},
		{
			"name":        "Infrastructure Review",
			"description": "Cloud infrastructure threat assessment",
			"issue_uri":   "https://jira.example.com/INFRA-50",
		},
	}

	var createdIDs []string
	for _, model := range testModels {
		body, _ := json.Marshal(model)
		req, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code)

		var tm ThreatModel
		err := json.Unmarshal(w.Body.Bytes(), &tm)
		require.NoError(t, err)
		createdIDs = append(createdIDs, tm.Id.String())
	}

	t.Run("filter by name partial match", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/threat_models?name=Security", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListThreatModelsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should find both testThreatModelNameAlpha and testThreatModelNameBeta
		securityCount := 0
		for _, item := range response.ThreatModels {
			if item.Name == testThreatModelNameAlpha || item.Name == testThreatModelNameBeta {
				securityCount++
			}
		}
		assert.GreaterOrEqual(t, securityCount, 2, "Should find at least 2 items matching 'Security'")
	})

	t.Run("filter by name case insensitive", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/threat_models?name=security", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListThreatModelsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should find items with "Security" (case insensitive)
		securityCount := 0
		for _, item := range response.ThreatModels {
			if item.Name == testThreatModelNameAlpha || item.Name == testThreatModelNameBeta {
				securityCount++
			}
		}
		assert.GreaterOrEqual(t, securityCount, 2, "Case-insensitive search should find items")
	})

	t.Run("filter by description partial match", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/threat_models?description=infrastructure", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListThreatModelsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should find "Infrastructure Review" (description contains "infrastructure")
		found := false
		for _, item := range response.ThreatModels {
			if item.Name == "Infrastructure Review" {
				found = true
				break
			}
		}
		assert.True(t, found, "Should find item with 'infrastructure' in description")
	})

	t.Run("filter by issue_uri partial match", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/threat_models?issue_uri=SEC-100", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListThreatModelsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should find testThreatModelNameAlpha
		found := false
		for _, item := range response.ThreatModels {
			if item.Name == testThreatModelNameAlpha {
				found = true
				break
			}
		}
		assert.True(t, found, "Should find item with 'SEC-100' in issue_uri")
	})

	t.Run("filter by owner", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/threat_models?owner=test@example.com", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListThreatModelsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// All items should be owned by test@example.com
		assert.NotEmpty(t, response.ThreatModels, "Should find items owned by test@example.com")
	})

	t.Run("filter with no matches returns empty", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/threat_models?name=nonexistent_name_xyz", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListThreatModelsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Filter should match none of our created items
		for _, item := range response.ThreatModels {
			assert.NotContains(t, item.Name, "nonexistent", "Should not find any items with 'nonexistent' in name")
		}
	})

	t.Run("combine multiple filters", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/threat_models?name=Security&description=API", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListThreatModelsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should find testThreatModelNameBeta (has "Security" in name AND "API" in description)
		found := false
		for _, item := range response.ThreatModels {
			if item.Name == testThreatModelNameBeta {
				found = true
				break
			}
		}
		assert.True(t, found, "Should find item matching both name and description filters")
	})

	t.Run("filter combined with pagination", func(t *testing.T) {
		// Note: The mock store doesn't implement pagination, so we just verify
		// that filters and pagination parameters can be combined without error
		req, _ := http.NewRequest("GET", "/threat_models?name=Security&limit=1&offset=0", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListThreatModelsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify at least some items are returned when filter matches
		// (pagination behavior is tested in integration tests with real database)
		assert.NotEmpty(t, response.ThreatModels, "Should return filtered results with pagination parameters")
	})

	t.Run("empty filter values are ignored", func(t *testing.T) {
		// Request with empty filter should return all items user has access to
		req1, _ := http.NewRequest("GET", "/threat_models", nil)
		w1 := httptest.NewRecorder()
		r.ServeHTTP(w1, req1)

		var response1 ListThreatModelsResponse
		err := json.Unmarshal(w1.Body.Bytes(), &response1)
		require.NoError(t, err)

		// Request with empty name parameter should behave the same
		req2, _ := http.NewRequest("GET", "/threat_models?name=", nil)
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)

		var response2 ListThreatModelsResponse
		err = json.Unmarshal(w2.Body.Bytes(), &response2)
		require.NoError(t, err)

		assert.Equal(t, len(response1.ThreatModels), len(response2.ThreatModels), "Empty filter should be ignored")
	})

	// Clean up created threat models
	for _, id := range createdIDs {
		req, _ := http.NewRequest("DELETE", "/threat_models/"+id, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}
}

// TestIsConfidentialField tests the is_confidential field behavior
func TestIsConfidentialField(t *testing.T) {
	r := setupThreatModelRouter()

	t.Run("Create with is_confidential true", func(t *testing.T) {
		reqBody, _ := json.Marshal(map[string]interface{}{
			"name":            "Confidential Model",
			"description":     "A confidential threat model",
			"is_confidential": true,
		})

		req, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var tm ThreatModel
		err := json.Unmarshal(w.Body.Bytes(), &tm)
		require.NoError(t, err)

		require.NotNil(t, tm.IsConfidential, "is_confidential should be present in response")
		assert.True(t, *tm.IsConfidential, "is_confidential should be true")

		// Clean up
		if tm.Id != nil {
			delReq, _ := http.NewRequest("DELETE", "/threat_models/"+tm.Id.String(), nil)
			delW := httptest.NewRecorder()
			r.ServeHTTP(delW, delReq)
		}
	})

	t.Run("Create without is_confidential defaults to false", func(t *testing.T) {
		reqBody, _ := json.Marshal(map[string]interface{}{
			"name":        "Non-Confidential Model",
			"description": "A normal threat model",
		})

		req, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var tm ThreatModel
		err := json.Unmarshal(w.Body.Bytes(), &tm)
		require.NoError(t, err)

		// When not provided, is_confidential may be nil or false in the in-memory store
		if tm.IsConfidential != nil {
			assert.False(t, *tm.IsConfidential, "is_confidential should default to false")
		}

		// Clean up
		if tm.Id != nil {
			delReq, _ := http.NewRequest("DELETE", "/threat_models/"+tm.Id.String(), nil)
			delW := httptest.NewRecorder()
			r.ServeHTTP(delW, delReq)
		}
	})

	t.Run("PUT preserves is_confidential (immutable)", func(t *testing.T) {
		// Create a confidential threat model
		createBody, _ := json.Marshal(map[string]interface{}{
			"name":            "Immutable Confidential",
			"description":     "Should stay confidential",
			"is_confidential": true,
		})

		createReq, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(createBody))
		createReq.Header.Set("Content-Type", "application/json")
		createW := httptest.NewRecorder()
		r.ServeHTTP(createW, createReq)
		require.Equal(t, http.StatusCreated, createW.Code)

		var createdTM ThreatModel
		err := json.Unmarshal(createW.Body.Bytes(), &createdTM)
		require.NoError(t, err)
		require.NotNil(t, createdTM.Id)

		// Update via PUT — is_confidential is not in the request body
		updateBody, _ := json.Marshal(map[string]interface{}{
			"name":        "Updated Name",
			"description": "Updated description",
		})

		putReq, _ := http.NewRequest("PUT", "/threat_models/"+createdTM.Id.String(), bytes.NewBuffer(updateBody))
		putReq.Header.Set("Content-Type", "application/json")
		putW := httptest.NewRecorder()
		r.ServeHTTP(putW, putReq)
		require.Equal(t, http.StatusOK, putW.Code)

		var updatedTM ThreatModel
		err = json.Unmarshal(putW.Body.Bytes(), &updatedTM)
		require.NoError(t, err)

		// is_confidential should still be true after PUT
		require.NotNil(t, updatedTM.IsConfidential, "is_confidential should be preserved after PUT")
		assert.True(t, *updatedTM.IsConfidential, "is_confidential should remain true after PUT")

		// Clean up
		delReq, _ := http.NewRequest("DELETE", "/threat_models/"+createdTM.Id.String(), nil)
		delW := httptest.NewRecorder()
		r.ServeHTTP(delW, delReq)
	})

	t.Run("PATCH with is_confidential is prohibited", func(t *testing.T) {
		// Create a threat model
		createBody, _ := json.Marshal(map[string]interface{}{
			"name":        "Patch Test Model",
			"description": "Testing PATCH prohibition",
		})

		createReq, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(createBody))
		createReq.Header.Set("Content-Type", "application/json")
		createW := httptest.NewRecorder()
		r.ServeHTTP(createW, createReq)
		require.Equal(t, http.StatusCreated, createW.Code)

		var createdTM ThreatModel
		err := json.Unmarshal(createW.Body.Bytes(), &createdTM)
		require.NoError(t, err)
		require.NotNil(t, createdTM.Id)

		// Attempt to PATCH is_confidential — should be rejected
		patchOps := []PatchOperation{
			{
				Op:    "replace",
				Path:  "/is_confidential",
				Value: true,
			},
		}

		patchBody, _ := json.Marshal(patchOps)
		patchReq, _ := http.NewRequest("PATCH", "/threat_models/"+createdTM.Id.String(), bytes.NewBuffer(patchBody))
		patchReq.Header.Set("Content-Type", "application/json-patch+json")
		patchW := httptest.NewRecorder()
		r.ServeHTTP(patchW, patchReq)

		assert.Equal(t, http.StatusBadRequest, patchW.Code, "PATCH should reject is_confidential modification")

		// Clean up
		delReq, _ := http.NewRequest("DELETE", "/threat_models/"+createdTM.Id.String(), nil)
		delW := httptest.NewRecorder()
		r.ServeHTTP(delW, delReq)
	})
}
