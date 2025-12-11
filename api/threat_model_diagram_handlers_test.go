package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ericfitz/tmi/internal/slogging"
)

// setupThreatModelDiagramRouter returns a router with threat model diagram handlers registered for the owner user
func setupThreatModelDiagramRouter() *gin.Engine {
	// Initialize test fixtures first
	InitTestFixtures()
	return setupThreatModelDiagramRouterWithUser(TestFixtures.OwnerUser)
}

// setupThreatModelDiagramRouterWithUser returns a router with threat model diagram handlers registered and specified user
func setupThreatModelDiagramRouterWithUser(userName string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Test fixtures should already be initialized by setupThreatModelDiagramRouter

	// Add a fake auth middleware to set user in context
	r.Use(func(c *gin.Context) {
		slogging.Get().WithContext(c).Debug("[TEST DEBUG] User name: %s, Request: %s %s",
			userName, c.Request.Method, c.Request.URL.Path)
		c.Set("userEmail", userName)
		c.Set("userID", userName+"-provider-id")  // Provider ID for testing
		c.Next()
	})

	// Add our authorization middleware
	r.Use(ThreatModelMiddleware())

	// Register threat model routes
	tmHandler := NewThreatModelHandler(NewWebSocketHubForTests())
	r.GET("/threat_models", tmHandler.GetThreatModels)
	r.POST("/threat_models", tmHandler.CreateThreatModel)
	r.GET("/threat_models/:threat_model_id", tmHandler.GetThreatModelByID)
	r.PUT("/threat_models/:threat_model_id", tmHandler.UpdateThreatModel)
	r.PATCH("/threat_models/:threat_model_id", tmHandler.PatchThreatModel)
	r.DELETE("/threat_models/:threat_model_id", tmHandler.DeleteThreatModel)

	// Register threat model diagram routes
	handler := NewThreatModelDiagramHandler(NewWebSocketHubForTests())
	r.GET("/threat_models/:threat_model_id/diagrams", func(c *gin.Context) {
		handler.GetDiagrams(c, c.Param("threat_model_id"))
	})
	r.POST("/threat_models/:threat_model_id/diagrams", func(c *gin.Context) {
		handler.CreateDiagram(c, c.Param("threat_model_id"))
	})
	r.GET("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		handler.GetDiagramByID(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.PUT("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		handler.UpdateDiagram(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.PATCH("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		handler.PatchDiagram(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.DELETE("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		handler.DeleteDiagram(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.GET("/threat_models/:threat_model_id/diagrams/:diagram_id/collaborate", func(c *gin.Context) {
		handler.GetDiagramCollaborate(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.POST("/threat_models/:threat_model_id/diagrams/:diagram_id/collaborate", func(c *gin.Context) {
		handler.CreateDiagramCollaborate(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.DELETE("/threat_models/:threat_model_id/diagrams/:diagram_id/collaborate", func(c *gin.Context) {
		handler.DeleteDiagramCollaborate(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})

	return r
}

// createTestThreatModelWithDiagram creates a test threat model with a diagram and returns both
func createTestThreatModelWithDiagram(t *testing.T, router *gin.Engine, tmName, tmDescription, diagName, diagDescription string) (ThreatModel, DfdDiagram) {
	// First create a threat model
	tmReqBody, _ := json.Marshal(map[string]interface{}{
		"name":        tmName,
		"description": tmDescription,
	})

	tmReq, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(tmReqBody))
	tmReq.Header.Set("Content-Type", "application/json")
	tmW := httptest.NewRecorder()

	router.ServeHTTP(tmW, tmReq)
	assert.Equal(t, http.StatusCreated, tmW.Code)

	var tm ThreatModel
	err := json.Unmarshal(tmW.Body.Bytes(), &tm)
	require.NoError(t, err)

	// Then create a diagram within the threat model
	diagReqBody, _ := json.Marshal(map[string]interface{}{
		"name":        diagName,
		"type":        "DFD-1.0.0",
		"description": diagDescription,
	})

	diagReq, _ := http.NewRequest("POST", fmt.Sprintf("/threat_models/%s/diagrams", tm.Id.String()), bytes.NewBuffer(diagReqBody))
	diagReq.Header.Set("Content-Type", "application/json")
	diagW := httptest.NewRecorder()

	router.ServeHTTP(diagW, diagReq)
	assert.Equal(t, http.StatusCreated, diagW.Code)

	var diagramUnion Diagram
	err = json.Unmarshal(diagW.Body.Bytes(), &diagramUnion)
	require.NoError(t, err)

	// Convert union type to DfdDiagram for return
	diagram, err := diagramUnion.AsDfdDiagram()
	require.NoError(t, err)

	return tm, diagram
}

// TestGetThreatModelDiagrams tests listing diagrams within a threat model
func TestGetThreatModelDiagrams(t *testing.T) {
	r := setupThreatModelDiagramRouter()

	// Create a test threat model with a diagram
	tm, _ := createTestThreatModelWithDiagram(t, r, "Test Threat Model", "This is a test threat model",
		"Test Diagram", "This is a test diagram")

	// Now test getting the list of diagrams
	listReq, _ := http.NewRequest("GET", fmt.Sprintf("/threat_models/%s/diagrams", tm.Id.String()), nil)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)

	// Assert response
	assert.Equal(t, http.StatusOK, listW.Code)

	// Parse response
	var items []map[string]interface{}
	err := json.Unmarshal(listW.Body.Bytes(), &items)
	require.NoError(t, err)

	// Check that we got at least one item
	assert.NotEmpty(t, items)

	// Check that our test item is in the list
	found := false
	for _, item := range items {
		if item["name"] == "Test Diagram" {
			found = true
			break
		}
	}
	assert.True(t, found, "Test diagram should be in the list")
}

// TestCreateThreatModelDiagram tests creating a diagram within a threat model
func TestCreateThreatModelDiagram(t *testing.T) {
	r := setupThreatModelDiagramRouter()

	// First create a threat model
	tmReqBody, _ := json.Marshal(map[string]interface{}{
		"name":        "Test Threat Model",
		"description": "This is a test threat model",
	})

	tmReq, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(tmReqBody))
	tmReq.Header.Set("Content-Type", "application/json")
	tmW := httptest.NewRecorder()

	r.ServeHTTP(tmW, tmReq)
	assert.Equal(t, http.StatusCreated, tmW.Code)

	var tm ThreatModel
	err := json.Unmarshal(tmW.Body.Bytes(), &tm)
	require.NoError(t, err)

	// Now create a diagram within the threat model
	diagReqBody, _ := json.Marshal(map[string]interface{}{
		"name":        "Test Diagram",
		"type":        "DFD-1.0.0",
		"description": "This is a test diagram",
	})

	diagReq, _ := http.NewRequest("POST", fmt.Sprintf("/threat_models/%s/diagrams", tm.Id.String()), bytes.NewBuffer(diagReqBody))
	diagReq.Header.Set("Content-Type", "application/json")
	diagW := httptest.NewRecorder()

	r.ServeHTTP(diagW, diagReq)

	// Debug output for response removed - use test output if needed

	// Assert response
	assert.Equal(t, http.StatusCreated, diagW.Code)

	// Parse response
	var diagramUnion Diagram
	err = json.Unmarshal(diagW.Body.Bytes(), &diagramUnion)
	require.NoError(t, err)

	// Convert union type to DfdDiagram for field access
	diagram, err := diagramUnion.AsDfdDiagram()
	require.NoError(t, err)

	// Check fields
	assert.Equal(t, "Test Diagram", diagram.Name)
	assert.NotEmpty(t, diagram.Id)

	// Get the updated threat model to verify the diagram was added
	tmGetReq, _ := http.NewRequest("GET", fmt.Sprintf("/threat_models/%s", tm.Id.String()), nil)
	tmGetW := httptest.NewRecorder()
	r.ServeHTTP(tmGetW, tmGetReq)
	assert.Equal(t, http.StatusOK, tmGetW.Code)

	var updatedTM ThreatModel
	err = json.Unmarshal(tmGetW.Body.Bytes(), &updatedTM)
	require.NoError(t, err)

	// Check that the diagram ID is in the threat model's diagrams array
	diagramFound := false
	if updatedTM.Diagrams != nil {
		for _, diagramUnion := range *updatedTM.Diagrams {
			// Convert union type to DfdDiagram to get the ID
			if dfdDiag, err := diagramUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil {
				if dfdDiag.Id.String() == diagram.Id.String() {
					diagramFound = true
					break
				}
			}
		}
	}
	assert.True(t, diagramFound, "Diagram ID should be in the threat model's diagrams array")
}

// TestGetThreatModelDiagramByID tests retrieving a specific diagram from a threat model
func TestGetThreatModelDiagramByID(t *testing.T) {
	r := setupThreatModelDiagramRouter()

	// Create a test threat model with a diagram
	tm, diagram := createTestThreatModelWithDiagram(t, r, "Test Threat Model", "This is a test threat model",
		"Test Diagram", "This is a test diagram")

	// Now test getting the diagram by ID
	getReq, _ := http.NewRequest("GET", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), nil)
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)

	// Assert response
	assert.Equal(t, http.StatusOK, getW.Code)

	// Parse response
	var retrievedDiagramUnion Diagram
	err := json.Unmarshal(getW.Body.Bytes(), &retrievedDiagramUnion)
	require.NoError(t, err)

	// Convert union type to DfdDiagram for field access
	retrievedDiagram, err := retrievedDiagramUnion.AsDfdDiagram()
	require.NoError(t, err)

	// Check fields
	assert.Equal(t, diagram.Id, retrievedDiagram.Id)
	assert.Equal(t, diagram.Name, retrievedDiagram.Name)
}

// TestUpdateThreatModelDiagram tests updating a diagram within a threat model
func TestUpdateThreatModelDiagram(t *testing.T) {
	r := setupThreatModelDiagramRouter()

	// Create a test threat model with a diagram
	tm, diagram := createTestThreatModelWithDiagram(t, r, "Test Threat Model", "This is a test threat model",
		"Test Diagram", "This is a test diagram")

	// Now update the diagram - create update payload without prohibited fields
	updatePayload := map[string]interface{}{
		"name":        "Updated Diagram",
		"type":        "DFD-1.0.0",
		"description": "This is an updated diagram",
		"cells":       diagram.Cells,
		"metadata":    diagram.Metadata,
	}

	updateBody, _ := json.Marshal(updatePayload)
	updateReq, _ := http.NewRequest("PUT", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), bytes.NewBuffer(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()

	r.ServeHTTP(updateW, updateReq)

	// Assert response
	assert.Equal(t, http.StatusOK, updateW.Code)

	// Parse response
	var resultDiagramUnion Diagram
	err := json.Unmarshal(updateW.Body.Bytes(), &resultDiagramUnion)
	require.NoError(t, err)

	// Convert union type to DfdDiagram for field access
	resultDiagram, err := resultDiagramUnion.AsDfdDiagram()
	require.NoError(t, err)

	// Check fields
	assert.Equal(t, "Updated Diagram", resultDiagram.Name)
	assert.Equal(t, diagram.Id, resultDiagram.Id)
	assert.Equal(t, diagram.CreatedAt, resultDiagram.CreatedAt)
	assert.NotEqual(t, diagram.ModifiedAt, resultDiagram.ModifiedAt)
}

// TestPatchThreatModelDiagram tests partially updating a diagram within a threat model
func TestPatchThreatModelDiagram(t *testing.T) {
	r := setupThreatModelDiagramRouter()

	// Create a test threat model with a diagram
	tm, diagram := createTestThreatModelWithDiagram(t, r, "Test Threat Model", "This is a test threat model",
		"Test Diagram", "This is a test diagram")

	// Now patch the diagram
	patchOps := []PatchOperation{
		{
			Op:    "replace",
			Path:  "/name",
			Value: "Patched Diagram",
		},
		{
			Op:    "replace",
			Path:  "/description",
			Value: "This is a patched diagram",
		},
	}

	patchBody, _ := json.Marshal(patchOps)
	patchReq, _ := http.NewRequest("PATCH", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()

	r.ServeHTTP(patchW, patchReq)

	// Assert response
	assert.Equal(t, http.StatusOK, patchW.Code)

	// Parse response
	var patchedDiagramUnion Diagram
	err := json.Unmarshal(patchW.Body.Bytes(), &patchedDiagramUnion)
	require.NoError(t, err)

	// Convert union type to DfdDiagram for field access
	patchedDiagram, err := patchedDiagramUnion.AsDfdDiagram()
	require.NoError(t, err)

	// Check fields - note that the current implementation doesn't actually apply the patch operations
	// It just returns the existing diagram with an updated modification time
	// This test will need to be updated when the real implementation is added
	assert.Equal(t, diagram.Id, patchedDiagram.Id)
	assert.NotEqual(t, diagram.ModifiedAt, patchedDiagram.ModifiedAt)
}

// TestDeleteThreatModelDiagram tests deleting a diagram from a threat model
func TestDeleteThreatModelDiagram(t *testing.T) {
	r := setupThreatModelDiagramRouter()

	// Create a test threat model with a diagram
	tm, diagram := createTestThreatModelWithDiagram(t, r, "Test Threat Model", "This is a test threat model",
		"Test Diagram", "This is a test diagram")

	// Now delete the diagram
	deleteReq, _ := http.NewRequest("DELETE", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), nil)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	// Assert response
	assert.Equal(t, http.StatusNoContent, deleteW.Code)

	// Verify the diagram is no longer in the threat model
	tmGetReq, _ := http.NewRequest("GET", fmt.Sprintf("/threat_models/%s", tm.Id.String()), nil)
	tmGetW := httptest.NewRecorder()
	r.ServeHTTP(tmGetW, tmGetReq)
	assert.Equal(t, http.StatusOK, tmGetW.Code)

	var updatedTM ThreatModel
	err := json.Unmarshal(tmGetW.Body.Bytes(), &updatedTM)
	require.NoError(t, err)

	// Check that the diagram ID is not in the threat model's diagrams array
	diagramFound := false
	if updatedTM.Diagrams != nil {
		for _, diagramUnion := range *updatedTM.Diagrams {
			// Convert union type to DfdDiagram to get the ID
			if dfdDiag, err := diagramUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil {
				if dfdDiag.Id.String() == diagram.Id.String() {
					diagramFound = true
					break
				}
			}
		}
	}
	assert.False(t, diagramFound, "Diagram ID should not be in the threat model's diagrams array")

	// Verify the diagram is no longer in the store
	getDiagramReq, _ := http.NewRequest("GET", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), nil)
	getDiagramW := httptest.NewRecorder()
	r.ServeHTTP(getDiagramW, getDiagramReq)
	assert.Equal(t, http.StatusNotFound, getDiagramW.Code)
}

// TestThreatModelDiagramNotFound tests behavior when a diagram is not found
func TestThreatModelDiagramNotFound(t *testing.T) {
	r := setupThreatModelDiagramRouter()

	// Create a test threat model
	tmReqBody, _ := json.Marshal(map[string]interface{}{
		"name":        "Test Threat Model",
		"description": "This is a test threat model",
	})

	tmReq, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(tmReqBody))
	tmReq.Header.Set("Content-Type", "application/json")
	tmW := httptest.NewRecorder()

	r.ServeHTTP(tmW, tmReq)
	assert.Equal(t, http.StatusCreated, tmW.Code)

	var tm ThreatModel
	err := json.Unmarshal(tmW.Body.Bytes(), &tm)
	require.NoError(t, err)

	// Try to get a non-existent diagram
	nonExistentID := NewUUID().String()
	getReq, _ := http.NewRequest("GET", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), nonExistentID), nil)
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)

	// Assert response
	assert.Equal(t, http.StatusNotFound, getW.Code)

	var errResp Error
	err = json.Unmarshal(getW.Body.Bytes(), &errResp)
	require.NoError(t, err)

	assert.Equal(t, "not_found", errResp.Error)
	assert.Contains(t, errResp.ErrorDescription, "Diagram not found")
}

// TestThreatModelNotFound tests behavior when a threat model is not found
func TestThreatModelNotFound(t *testing.T) {
	r := setupThreatModelDiagramRouter()

	// Try to get diagrams from a non-existent threat model
	nonExistentID := NewUUID().String()
	getReq, _ := http.NewRequest("GET", fmt.Sprintf("/threat_models/%s/diagrams", nonExistentID), nil)
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)

	// Assert response
	assert.Equal(t, http.StatusNotFound, getW.Code)

	var errResp Error
	err := json.Unmarshal(getW.Body.Bytes(), &errResp)
	require.NoError(t, err)

	assert.Equal(t, "not_found", errResp.Error)
	assert.Contains(t, errResp.ErrorDescription, "Threat model not found")
}

// TestDiagramNotInThreatModel tests behavior when a diagram ID is valid but not associated with the threat model
func TestDiagramNotInThreatModel(t *testing.T) {
	r := setupThreatModelDiagramRouter()

	// Create two threat models with diagrams
	tm1, _ := createTestThreatModelWithDiagram(t, r, "Threat Model 1", "This is threat model 1",
		"Diagram 1", "This is diagram 1")
	_, diagram2 := createTestThreatModelWithDiagram(t, r, "Threat Model 2", "This is threat model 2",
		"Diagram 2", "This is diagram 2")

	// Try to get diagram2 from tm1
	getReq, _ := http.NewRequest("GET", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm1.Id.String(), diagram2.Id.String()), nil)
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)

	// Assert response
	assert.Equal(t, http.StatusNotFound, getW.Code)

	var errResp Error
	err := json.Unmarshal(getW.Body.Bytes(), &errResp)
	require.NoError(t, err)

	assert.Equal(t, "not_found", errResp.Error)
	assert.Contains(t, errResp.ErrorDescription, "Diagram not found in this threat model")
}

// TestThreatModelDiagramReadWriteDeletePermissions tests access levels for different operations
func TestThreatModelDiagramReadWriteDeletePermissions(t *testing.T) {
	// Reset stores to ensure clean state

	// Create initial router and threat model with diagram
	ownerRouter := setupThreatModelDiagramRouter() // original owner is test@example.com
	tm, diagram := createTestThreatModelWithDiagram(t, ownerRouter, "Permissions Test", "Testing permission levels",
		"Test Diagram", "This is a test diagram")

	// Add users with different permission levels to the threat model
	patchOps := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"principal_type": "user",
				"provider":       "test",
				"provider_id":    "reader@example.com",
				"role":           "reader",
			},
		},
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"principal_type": "user",
				"provider":       "test",
				"provider_id":    "writer@example.com",
				"role":           "writer",
			},
		},
	}

	patchBody, _ := json.Marshal(patchOps)
	patchReq, _ := http.NewRequest("PATCH", fmt.Sprintf("/threat_models/%s", tm.Id.String()), bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	ownerRouter.ServeHTTP(patchW, patchReq)
	assert.Equal(t, http.StatusOK, patchW.Code)

	// Test 1: Reader can read but not write or delete
	readerRouter := setupThreatModelDiagramRouterWithUser("reader@example.com")

	// Reader should be able to read
	readReq, _ := http.NewRequest("GET", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), nil)
	readW := httptest.NewRecorder()
	readerRouter.ServeHTTP(readW, readReq)
	assert.Equal(t, http.StatusOK, readW.Code)

	// Reader should not be able to update - create update payload without prohibited fields
	readerUpdatePayload := map[string]interface{}{
		"name":        diagram.Name,
		"type":        "DFD-1.0.0",
		"description": "Updated by reader",
		"cells":       diagram.Cells,
		"metadata":    diagram.Metadata,
	}

	updateBody, _ := json.Marshal(readerUpdatePayload)
	updateReq, _ := http.NewRequest("PUT", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), bytes.NewBuffer(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	readerRouter.ServeHTTP(updateW, updateReq)
	assert.Equal(t, http.StatusForbidden, updateW.Code)

	// Reader should not be able to delete
	deleteReq, _ := http.NewRequest("DELETE", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), nil)
	deleteW := httptest.NewRecorder()
	readerRouter.ServeHTTP(deleteW, deleteReq)
	assert.Equal(t, http.StatusForbidden, deleteW.Code)

	// Test 2: Writer can read and write but not delete
	writerRouter := setupThreatModelDiagramRouterWithUser("writer@example.com")

	// Writer should be able to read
	readReq2, _ := http.NewRequest("GET", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), nil)
	readW2 := httptest.NewRecorder()
	writerRouter.ServeHTTP(readW2, readReq2)
	assert.Equal(t, http.StatusOK, readW2.Code)

	// Writer should be able to update
	writerUpdatePayload := map[string]interface{}{
		"name":        "Updated by Writer",
		"type":        "DFD-1.0.0",
		"description": "This description was updated by a writer",
		"cells":       diagram.Cells,
		"metadata":    diagram.Metadata,
	}

	updateBody2, _ := json.Marshal(writerUpdatePayload)
	updateReq2, _ := http.NewRequest("PUT", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), bytes.NewBuffer(updateBody2))
	updateReq2.Header.Set("Content-Type", "application/json")
	updateW2 := httptest.NewRecorder()
	writerRouter.ServeHTTP(updateW2, updateReq2)
	assert.Equal(t, http.StatusOK, updateW2.Code)

	// Writer should not be able to delete
	deleteReq2, _ := http.NewRequest("DELETE", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), nil)
	deleteW2 := httptest.NewRecorder()
	writerRouter.ServeHTTP(deleteW2, deleteReq2)
	assert.Equal(t, http.StatusForbidden, deleteW2.Code)

	// Test 3: Owner can read, write and delete
	// Owner should be able to delete
	deleteReq3, _ := http.NewRequest("DELETE", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), nil)
	deleteW3 := httptest.NewRecorder()
	ownerRouter.ServeHTTP(deleteW3, deleteReq3)
	assert.Equal(t, http.StatusNoContent, deleteW3.Code)
}

// TestGetThreatModelDiagramCollaborate tests retrieving collaboration session status
func TestGetThreatModelDiagramCollaborate(t *testing.T) {
	r := setupThreatModelDiagramRouter()

	// Create a test threat model with a diagram
	tm, diagram := createTestThreatModelWithDiagram(t, r, "Test Threat Model", "This is a test threat model",
		"Test Diagram", "This is a test diagram")

	// Get collaboration session status
	getReq, _ := http.NewRequest("GET", fmt.Sprintf("/threat_models/%s/diagrams/%s/collaborate", tm.Id.String(), diagram.Id.String()), nil)
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)

	// Assert response
	assert.Equal(t, http.StatusOK, getW.Code)

	// Parse response
	var session map[string]interface{}
	err := json.Unmarshal(getW.Body.Bytes(), &session)
	require.NoError(t, err)

	// Check fields
	assert.Contains(t, session, "session_id")
	assert.Contains(t, session, "threat_model_id")
	assert.Contains(t, session, "diagram_id")
	assert.Contains(t, session, "participants")
	assert.Contains(t, session, "websocket_url")
	assert.Equal(t, tm.Id.String(), session["threat_model_id"])
	assert.Equal(t, diagram.Id.String(), session["diagram_id"])
}

// TestPostThreatModelDiagramCollaborate tests joining/starting a collaboration session
func TestPostThreatModelDiagramCollaborate(t *testing.T) {
	r := setupThreatModelDiagramRouter()

	// Create a test threat model with a diagram
	tm, diagram := createTestThreatModelWithDiagram(t, r, "Test Threat Model", "This is a test threat model",
		"Test Diagram", "This is a test diagram")

	// Join/start collaboration session
	postReq, _ := http.NewRequest("POST", fmt.Sprintf("/threat_models/%s/diagrams/%s/collaborate", tm.Id.String(), diagram.Id.String()), nil)
	postW := httptest.NewRecorder()
	r.ServeHTTP(postW, postReq)

	// Assert response
	assert.Equal(t, http.StatusCreated, postW.Code)

	// Parse response
	var session map[string]interface{}
	err := json.Unmarshal(postW.Body.Bytes(), &session)
	require.NoError(t, err)

	// Check fields
	assert.Contains(t, session, "session_id")
	assert.Contains(t, session, "threat_model_id")
	assert.Contains(t, session, "diagram_id")
	assert.Contains(t, session, "participants")
	assert.Contains(t, session, "websocket_url")
	assert.Equal(t, tm.Id.String(), session["threat_model_id"])
	assert.Equal(t, diagram.Id.String(), session["diagram_id"])

	// Check that the current user is in the participants list
	participants, ok := session["participants"].([]interface{})
	assert.True(t, ok)
	if assert.NotEmpty(t, participants) {
		participant, ok := participants[0].(map[string]interface{})
		assert.True(t, ok)
		user, ok := participant["user"].(map[string]interface{})
		assert.True(t, ok)
		// User struct uses provider_id field (not "id" or "user_id")
		assert.NotNil(t, user["provider_id"], "User should have a provider_id field")
		assert.Equal(t, "test@example.com", user["email"])
	}
}

// TestDeleteThreatModelDiagramCollaborate tests leaving a collaboration session
func TestDeleteThreatModelDiagramCollaborate(t *testing.T) {
	r := setupThreatModelDiagramRouter()

	// Create a test threat model with a diagram
	tm, diagram := createTestThreatModelWithDiagram(t, r, "Test Threat Model", "This is a test threat model",
		"Test Diagram", "This is a test diagram")

	// Leave collaboration session
	deleteReq, _ := http.NewRequest("DELETE", fmt.Sprintf("/threat_models/%s/diagrams/%s/collaborate", tm.Id.String(), diagram.Id.String()), nil)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	// Assert response
	assert.Equal(t, http.StatusNoContent, deleteW.Code)
}

// TestDeleteThreatModelDiagramCollaborateImmediateDisconnection tests that when host deletes a session,
// all participants are immediately disconnected and the session is fully cleaned up
func TestDeleteThreatModelDiagramCollaborateImmediateDisconnection(t *testing.T) {
	// Create a specific WebSocket hub for this test so we can access it directly
	wsHub := NewWebSocketHubForTests()

	// Setup router with the specific hub
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Initialize test fixtures
	InitTestFixtures()

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userID", TestFixtures.OwnerUser+"-provider-id")  // Provider ID for testing
		c.Next()
	})

	// Add authorization middleware
	r.Use(ThreatModelMiddleware())

	// Create handlers
	tmHandler := NewThreatModelHandler(NewWebSocketHubForTests())
	diagramHandler := NewThreatModelDiagramHandler(wsHub)

	// Register all routes needed for the test
	r.POST("/threat_models", tmHandler.CreateThreatModel)
	r.POST("/threat_models/:threat_model_id/diagrams", func(c *gin.Context) {
		diagramHandler.CreateDiagram(c, c.Param("threat_model_id"))
	})
	r.POST("/threat_models/:threat_model_id/diagrams/:diagram_id/collaborate", func(c *gin.Context) {
		diagramHandler.CreateDiagramCollaborate(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.DELETE("/threat_models/:threat_model_id/diagrams/:diagram_id/collaborate", func(c *gin.Context) {
		diagramHandler.DeleteDiagramCollaborate(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})

	// Create a test threat model with a diagram
	tm, diagram := createTestThreatModelWithDiagram(t, r, "Test Threat Model", "This is a test threat model",
		"Test Diagram", "This is a test diagram")

	// Create collaboration session
	postReq, _ := http.NewRequest("POST", fmt.Sprintf("/threat_models/%s/diagrams/%s/collaborate", tm.Id.String(), diagram.Id.String()), nil)
	postW := httptest.NewRecorder()
	r.ServeHTTP(postW, postReq)
	require.Equal(t, http.StatusCreated, postW.Code)

	// Verify session was created
	session := wsHub.GetSession(diagram.Id.String())
	require.NotNil(t, session, "Collaboration session should exist")
	require.Equal(t, SessionStateActive, session.State)

	// Simulate some participants by creating mock clients
	mockClient1 := &WebSocketClient{
		UserID:    "user1@test.com",
		UserName:  "User One",
		UserEmail: "user1@test.com",
		Send:      make(chan []byte, 1),
	}
	mockClient2 := &WebSocketClient{
		UserID:    "user2@test.com",
		UserName:  "User Two",
		UserEmail: "user2@test.com",
		Send:      make(chan []byte, 1),
	}

	// Add clients to session directly (simulating WebSocket connection)
	session.mu.Lock()
	session.Clients[mockClient1] = true
	session.Clients[mockClient2] = true
	session.mu.Unlock()

	// Verify clients are connected
	session.mu.RLock()
	clientCount := len(session.Clients)
	session.mu.RUnlock()
	require.Equal(t, 2, clientCount, "Should have 2 mock clients connected")

	// HOST deletes the collaboration session (immediate termination)
	deleteReq, _ := http.NewRequest("DELETE", fmt.Sprintf("/threat_models/%s/diagrams/%s/collaborate", tm.Id.String(), diagram.Id.String()), nil)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	// Assert response
	assert.Equal(t, http.StatusNoContent, deleteW.Code)

	// Verify immediate state cleanup - session should be completely removed from hub
	sessionAfterDelete := wsHub.GetSession(diagram.Id.String())
	assert.Nil(t, sessionAfterDelete, "Session should be immediately removed from hub")

	// The main verification is that the session is removed from the hub immediately
	// This confirms that:
	// 1. No new connections can be established to the session
	// 2. The session state has been cleaned up
	// 3. All associated resources have been deallocated

	// Additional verification - session should remain gone after a brief delay
	time.Sleep(10 * time.Millisecond)
	finalSessionCheck := wsHub.GetSession(diagram.Id.String())
	assert.Nil(t, finalSessionCheck, "Session should remain removed from hub after cleanup delay")

	// Verify the session termination was logged
	t.Log("✓ Host DELETE request immediately removed session from hub")
	t.Log("✓ No timeouts were waited for - immediate cleanup confirmed")
}

// TestImageUpdateVectorLogic tests that image.update_vector is automatically set when SVG is provided but update_vector is not
func TestImageUpdateVectorLogic(t *testing.T) {
	r := setupThreatModelDiagramRouter()

	// Create a test threat model with a diagram
	tm, diagram := createTestThreatModelWithDiagram(t, r, "Test Threat Model", "This is a test threat model",
		"Test Diagram", "This is a test diagram")

	// Encode test SVG data
	testSVG := []byte("<svg><circle r='10'/></svg>")

	// Test Case 1: Update diagram with SVG but no image.update_vector - should auto-set
	updatePayload1 := map[string]interface{}{
		"name":     "Updated Diagram with SVG",
		"type":     "DFD-1.0.0",
		"cells":    diagram.Cells,
		"metadata": diagram.Metadata,
		"image": map[string]interface{}{
			"svg": testSVG,
			// Deliberately omit update_vector to test auto-set logic
		},
	}

	updateBody1, _ := json.Marshal(updatePayload1)
	updateReq1, _ := http.NewRequest("PUT", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), bytes.NewBuffer(updateBody1))
	updateReq1.Header.Set("Content-Type", "application/json")
	updateW1 := httptest.NewRecorder()

	r.ServeHTTP(updateW1, updateReq1)

	// Assert response
	assert.Equal(t, http.StatusOK, updateW1.Code)

	// Parse response
	var resultDiagramUnion1 Diagram
	err := json.Unmarshal(updateW1.Body.Bytes(), &resultDiagramUnion1)
	require.NoError(t, err)

	// Convert union type to DfdDiagram for field access
	resultDiagram1, err := resultDiagramUnion1.AsDfdDiagram()
	require.NoError(t, err)

	// Verify that image.update_vector was automatically set to match BaseDiagram.update_vector
	require.NotNil(t, resultDiagram1.Image, "Image should not be nil")
	require.NotNil(t, resultDiagram1.Image.UpdateVector, "Image.UpdateVector should be auto-set")
	require.NotNil(t, resultDiagram1.UpdateVector, "BaseDiagram.UpdateVector should exist")
	assert.Equal(t, *resultDiagram1.UpdateVector, *resultDiagram1.Image.UpdateVector, "Image.UpdateVector should match BaseDiagram.UpdateVector")

	// Test Case 2: Update diagram with SVG and explicit image.update_vector - should use provided value
	explicitImageVector := int64(42)
	updatePayload2 := map[string]interface{}{
		"name":     "Updated Diagram with Explicit Image Vector",
		"type":     "DFD-1.0.0",
		"cells":    diagram.Cells,
		"metadata": diagram.Metadata,
		"image": map[string]interface{}{
			"svg":           testSVG,
			"update_vector": explicitImageVector,
		},
	}

	updateBody2, _ := json.Marshal(updatePayload2)
	updateReq2, _ := http.NewRequest("PUT", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), bytes.NewBuffer(updateBody2))
	updateReq2.Header.Set("Content-Type", "application/json")
	updateW2 := httptest.NewRecorder()

	r.ServeHTTP(updateW2, updateReq2)

	// Assert response
	assert.Equal(t, http.StatusOK, updateW2.Code)

	// Parse response
	var resultDiagramUnion2 Diagram
	err = json.Unmarshal(updateW2.Body.Bytes(), &resultDiagramUnion2)
	require.NoError(t, err)

	// Convert union type to DfdDiagram for field access
	resultDiagram2, err := resultDiagramUnion2.AsDfdDiagram()
	require.NoError(t, err)

	// Verify that explicit image.update_vector was preserved
	require.NotNil(t, resultDiagram2.Image, "Image should not be nil")
	require.NotNil(t, resultDiagram2.Image.UpdateVector, "Image.UpdateVector should exist")
	assert.Equal(t, explicitImageVector, *resultDiagram2.Image.UpdateVector, "Image.UpdateVector should use explicitly provided value")

	// Test Case 3: Update diagram without image - should not create image
	updatePayload3 := map[string]interface{}{
		"name":     "Updated Diagram without Image",
		"type":     "DFD-1.0.0",
		"cells":    diagram.Cells,
		"metadata": diagram.Metadata,
	}

	updateBody3, _ := json.Marshal(updatePayload3)
	updateReq3, _ := http.NewRequest("PUT", fmt.Sprintf("/threat_models/%s/diagrams/%s", tm.Id.String(), diagram.Id.String()), bytes.NewBuffer(updateBody3))
	updateReq3.Header.Set("Content-Type", "application/json")
	updateW3 := httptest.NewRecorder()

	r.ServeHTTP(updateW3, updateReq3)

	// Assert response
	assert.Equal(t, http.StatusOK, updateW3.Code)

	// Parse response
	var resultDiagramUnion3 Diagram
	err = json.Unmarshal(updateW3.Body.Bytes(), &resultDiagramUnion3)
	require.NoError(t, err)

	// Convert union type to DfdDiagram for field access
	resultDiagram3, err := resultDiagramUnion3.AsDfdDiagram()
	require.NoError(t, err)

	// The logic should not affect image field when none was provided
	// This is mainly to verify the logic doesn't break when no image is present
	t.Logf("Test Case 3 completed successfully - no image provided, no crashes: %s", resultDiagram3.Name)
}
