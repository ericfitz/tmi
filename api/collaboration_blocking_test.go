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

// TestDiagramUpdateBlockedDuringCollaboration tests that PUT /diagrams/{id} returns 409 during active session
func TestDiagramUpdateBlockedDuringCollaboration(t *testing.T) {
	// Initialize mock stores
	InitializeMockStores()

	// Setup
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "alice@example.com")
		c.Set("userID", "alice-provider-id")
		c.Set("userName", "Alice")
		c.Set("userId", "alice@example.com")
		c.Next()
	})
	r.Use(ThreatModelMiddleware())

	wsHub := NewWebSocketHubForTests()
	tmHandler := NewThreatModelHandler(wsHub)
	diagramHandler := NewThreatModelDiagramHandler(wsHub)

	r.POST("/threat_models", tmHandler.CreateThreatModel)
	r.POST("/threat_models/:threat_model_id/diagrams", func(c *gin.Context) {
		diagramHandler.CreateDiagram(c, c.Param("threat_model_id"))
	})
	r.GET("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		diagramHandler.GetDiagramByID(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.PUT("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		diagramHandler.UpdateDiagram(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.GET("/threat_models/:threat_model_id/diagrams/:diagram_id/collaborate", func(c *gin.Context) {
		diagramHandler.GetDiagramCollaborate(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.POST("/threat_models/:threat_model_id/diagrams/:diagram_id/collaborate", func(c *gin.Context) {
		diagramHandler.CreateDiagramCollaborate(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})

	// Create threat model
	tmPayload := map[string]interface{}{
		"name":                   "Test TM",
		"threat_model_framework": "STRIDE",
	}
	tmBody, _ := json.Marshal(tmPayload)
	tmReq := httptest.NewRequest("POST", "/threat_models", bytes.NewBuffer(tmBody))
	tmReq.Header.Set("Content-Type", "application/json")
	tmW := httptest.NewRecorder()
	r.ServeHTTP(tmW, tmReq)
	require.Equal(t, http.StatusCreated, tmW.Code)

	var tm ThreatModel
	err := json.Unmarshal(tmW.Body.Bytes(), &tm)
	require.NoError(t, err)
	tmID := tm.Id.String()

	// Create diagram
	diagPayload := map[string]interface{}{
		"name": "Test Diagram",
		"type": "DFD-1.0.0",
	}
	diagBody, _ := json.Marshal(diagPayload)
	diagReq := httptest.NewRequest("POST", "/threat_models/"+tmID+"/diagrams", bytes.NewBuffer(diagBody))
	diagReq.Header.Set("Content-Type", "application/json")
	diagW := httptest.NewRecorder()
	r.ServeHTTP(diagW, diagReq)
	require.Equal(t, http.StatusCreated, diagW.Code)

	var diagram DfdDiagram
	err = json.Unmarshal(diagW.Body.Bytes(), &diagram)
	require.NoError(t, err)
	diagramID := diagram.Id.String()

	// Start collaboration session
	collabReq := httptest.NewRequest("POST", "/threat_models/"+tmID+"/diagrams/"+diagramID+"/collaborate", nil)
	collabW := httptest.NewRecorder()
	r.ServeHTTP(collabW, collabReq)
	require.Equal(t, http.StatusCreated, collabW.Code)

	// Verify session is active
	assert.True(t, wsHub.HasActiveSession(diagramID))

	// Try to update diagram - should fail with 409
	updatePayload := DfdDiagram{
		Name: "Updated Diagram",
		Type: "DFD-1.0.0",
	}
	updateBody, _ := json.Marshal(updatePayload)
	updateReq := httptest.NewRequest("PUT", "/threat_models/"+tmID+"/diagrams/"+diagramID, bytes.NewBuffer(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)

	assert.Equal(t, http.StatusConflict, updateW.Code)

	var errorResp map[string]interface{}
	err = json.Unmarshal(updateW.Body.Bytes(), &errorResp)
	require.NoError(t, err)
	assert.Equal(t, "conflict", errorResp["error"])
	assert.Contains(t, errorResp["error_description"], "collaboration session is active")
}

// TestDiagramPatchBlockedDuringCollaboration tests that PATCH /diagrams/{id} returns 409 during active session
func TestDiagramPatchBlockedDuringCollaboration(t *testing.T) {
	// Initialize mock stores
	InitializeMockStores()

	// Setup
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "bob@example.com")
		c.Set("userID", "bob-provider-id")
		c.Set("userName", "Bob")
		c.Set("userId", "bob@example.com")
		c.Next()
	})
	r.Use(ThreatModelMiddleware())

	wsHub := NewWebSocketHubForTests()
	tmHandler := NewThreatModelHandler(wsHub)
	diagramHandler := NewThreatModelDiagramHandler(wsHub)

	r.POST("/threat_models", tmHandler.CreateThreatModel)
	r.POST("/threat_models/:threat_model_id/diagrams", func(c *gin.Context) {
		diagramHandler.CreateDiagram(c, c.Param("threat_model_id"))
	})
	r.PATCH("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		diagramHandler.PatchDiagram(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.POST("/threat_models/:threat_model_id/diagrams/:diagram_id/collaborate", func(c *gin.Context) {
		diagramHandler.CreateDiagramCollaborate(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})

	// Create threat model
	tmPayload := map[string]interface{}{
		"name":                   "Test TM",
		"threat_model_framework": "STRIDE",
	}
	tmBody, _ := json.Marshal(tmPayload)
	tmReq := httptest.NewRequest("POST", "/threat_models", bytes.NewBuffer(tmBody))
	tmReq.Header.Set("Content-Type", "application/json")
	tmW := httptest.NewRecorder()
	r.ServeHTTP(tmW, tmReq)
	require.Equal(t, http.StatusCreated, tmW.Code)

	var tm ThreatModel
	err := json.Unmarshal(tmW.Body.Bytes(), &tm)
	require.NoError(t, err)
	tmID := tm.Id.String()

	// Create diagram
	diagPayload := map[string]interface{}{
		"name": "Test Diagram",
		"type": "DFD-1.0.0",
	}
	diagBody, _ := json.Marshal(diagPayload)
	diagReq := httptest.NewRequest("POST", "/threat_models/"+tmID+"/diagrams", bytes.NewBuffer(diagBody))
	diagReq.Header.Set("Content-Type", "application/json")
	diagW := httptest.NewRecorder()
	r.ServeHTTP(diagW, diagReq)
	require.Equal(t, http.StatusCreated, diagW.Code)

	var diagram DfdDiagram
	err = json.Unmarshal(diagW.Body.Bytes(), &diagram)
	require.NoError(t, err)
	diagramID := diagram.Id.String()

	// Start collaboration session
	collabReq := httptest.NewRequest("POST", "/threat_models/"+tmID+"/diagrams/"+diagramID+"/collaborate", nil)
	collabW := httptest.NewRecorder()
	r.ServeHTTP(collabW, collabReq)
	require.Equal(t, http.StatusCreated, collabW.Code)

	// Try to patch diagram - should fail with 409
	patchPayload := []map[string]interface{}{
		{
			"op":    "replace",
			"path":  "/name",
			"value": "Patched Diagram",
		},
	}
	patchBody, _ := json.Marshal(patchPayload)
	patchReq := httptest.NewRequest("PATCH", "/threat_models/"+tmID+"/diagrams/"+diagramID, bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	r.ServeHTTP(patchW, patchReq)

	assert.Equal(t, http.StatusConflict, patchW.Code)

	var errorResp map[string]interface{}
	err = json.Unmarshal(patchW.Body.Bytes(), &errorResp)
	require.NoError(t, err)
	assert.Equal(t, "conflict", errorResp["error"])
	assert.Contains(t, errorResp["error_description"], "collaboration session is active")
}

// TestDiagramDeleteBlockedDuringCollaboration tests that DELETE /diagrams/{id} returns 409 during active session
func TestDiagramDeleteBlockedDuringCollaboration(t *testing.T) {
	// Initialize mock stores
	InitializeMockStores()

	// Setup
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "charlie@example.com")
		c.Set("userID", "charlie-provider-id")
		c.Set("userName", "Charlie")
		c.Set("userId", "charlie@example.com")
		c.Next()
	})
	r.Use(ThreatModelMiddleware())

	wsHub := NewWebSocketHubForTests()
	tmHandler := NewThreatModelHandler(wsHub)
	diagramHandler := NewThreatModelDiagramHandler(wsHub)

	r.POST("/threat_models", tmHandler.CreateThreatModel)
	r.POST("/threat_models/:threat_model_id/diagrams", func(c *gin.Context) {
		diagramHandler.CreateDiagram(c, c.Param("threat_model_id"))
	})
	r.DELETE("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		diagramHandler.DeleteDiagram(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.POST("/threat_models/:threat_model_id/diagrams/:diagram_id/collaborate", func(c *gin.Context) {
		diagramHandler.CreateDiagramCollaborate(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})

	// Create threat model
	tmPayload := map[string]interface{}{
		"name":                   "Test TM",
		"threat_model_framework": "STRIDE",
	}
	tmBody, _ := json.Marshal(tmPayload)
	tmReq := httptest.NewRequest("POST", "/threat_models", bytes.NewBuffer(tmBody))
	tmReq.Header.Set("Content-Type", "application/json")
	tmW := httptest.NewRecorder()
	r.ServeHTTP(tmW, tmReq)
	require.Equal(t, http.StatusCreated, tmW.Code)

	var tm ThreatModel
	err := json.Unmarshal(tmW.Body.Bytes(), &tm)
	require.NoError(t, err)
	tmID := tm.Id.String()

	// Create diagram
	diagPayload := map[string]interface{}{
		"name": "Test Diagram",
		"type": "DFD-1.0.0",
	}
	diagBody, _ := json.Marshal(diagPayload)
	diagReq := httptest.NewRequest("POST", "/threat_models/"+tmID+"/diagrams", bytes.NewBuffer(diagBody))
	diagReq.Header.Set("Content-Type", "application/json")
	diagW := httptest.NewRecorder()
	r.ServeHTTP(diagW, diagReq)
	require.Equal(t, http.StatusCreated, diagW.Code)

	var diagram DfdDiagram
	err = json.Unmarshal(diagW.Body.Bytes(), &diagram)
	require.NoError(t, err)
	diagramID := diagram.Id.String()

	// Start collaboration session
	collabReq := httptest.NewRequest("POST", "/threat_models/"+tmID+"/diagrams/"+diagramID+"/collaborate", nil)
	collabW := httptest.NewRecorder()
	r.ServeHTTP(collabW, collabReq)
	require.Equal(t, http.StatusCreated, collabW.Code)

	// Try to delete diagram - should fail with 409
	deleteReq := httptest.NewRequest("DELETE", "/threat_models/"+tmID+"/diagrams/"+diagramID, nil)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	assert.Equal(t, http.StatusConflict, deleteW.Code)

	var errorResp map[string]interface{}
	err = json.Unmarshal(deleteW.Body.Bytes(), &errorResp)
	require.NoError(t, err)
	assert.Equal(t, "conflict", errorResp["error"])
	assert.Contains(t, errorResp["error_description"], "collaboration session is active")
}

// TestThreatModelDeleteBlockedDuringCollaboration tests that DELETE /threat_models/{id} returns 409 when diagram has active session
func TestThreatModelDeleteBlockedDuringCollaboration(t *testing.T) {
	// Initialize mock stores
	InitializeMockStores()

	// Setup
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "dave@example.com")
		c.Set("userID", "dave-provider-id")
		c.Set("userName", "Dave")
		c.Set("userId", "dave@example.com")
		c.Next()
	})
	r.Use(ThreatModelMiddleware())

	wsHub := NewWebSocketHubForTests()
	tmHandler := NewThreatModelHandler(wsHub)
	diagramHandler := NewThreatModelDiagramHandler(wsHub)

	r.POST("/threat_models", tmHandler.CreateThreatModel)
	r.DELETE("/threat_models/:threat_model_id", tmHandler.DeleteThreatModel)
	r.POST("/threat_models/:threat_model_id/diagrams", func(c *gin.Context) {
		diagramHandler.CreateDiagram(c, c.Param("threat_model_id"))
	})
	r.POST("/threat_models/:threat_model_id/diagrams/:diagram_id/collaborate", func(c *gin.Context) {
		diagramHandler.CreateDiagramCollaborate(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})

	// Create threat model
	tmPayload := map[string]interface{}{
		"name":                   "Test TM",
		"threat_model_framework": "STRIDE",
	}
	tmBody, _ := json.Marshal(tmPayload)
	tmReq := httptest.NewRequest("POST", "/threat_models", bytes.NewBuffer(tmBody))
	tmReq.Header.Set("Content-Type", "application/json")
	tmW := httptest.NewRecorder()
	r.ServeHTTP(tmW, tmReq)
	require.Equal(t, http.StatusCreated, tmW.Code)

	var tm ThreatModel
	err := json.Unmarshal(tmW.Body.Bytes(), &tm)
	require.NoError(t, err)
	tmID := tm.Id.String()

	// Create diagram
	diagPayload := map[string]interface{}{
		"name": "Test Diagram",
		"type": "DFD-1.0.0",
	}
	diagBody, _ := json.Marshal(diagPayload)
	diagReq := httptest.NewRequest("POST", "/threat_models/"+tmID+"/diagrams", bytes.NewBuffer(diagBody))
	diagReq.Header.Set("Content-Type", "application/json")
	diagW := httptest.NewRecorder()
	r.ServeHTTP(diagW, diagReq)
	require.Equal(t, http.StatusCreated, diagW.Code)

	var diagram DfdDiagram
	err = json.Unmarshal(diagW.Body.Bytes(), &diagram)
	require.NoError(t, err)
	diagramID := diagram.Id.String()

	// Start collaboration session
	collabReq := httptest.NewRequest("POST", "/threat_models/"+tmID+"/diagrams/"+diagramID+"/collaborate", nil)
	collabW := httptest.NewRecorder()
	r.ServeHTTP(collabW, collabReq)
	require.Equal(t, http.StatusCreated, collabW.Code)

	// Try to delete threat model - should fail with 409
	deleteReq := httptest.NewRequest("DELETE", "/threat_models/"+tmID, nil)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	assert.Equal(t, http.StatusConflict, deleteW.Code)

	var errorResp map[string]interface{}
	err = json.Unmarshal(deleteW.Body.Bytes(), &errorResp)
	require.NoError(t, err)
	assert.Equal(t, "conflict", errorResp["error"])
	assert.Contains(t, errorResp["error_description"], "collaboration session")
}

// TestOperationsSucceedWithoutActiveSession verifies that operations work normally when no session is active
func TestOperationsSucceedWithoutActiveSession(t *testing.T) {
	// Initialize mock stores
	InitializeMockStores()

	// Setup
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "eve@example.com")
		c.Set("userID", "eve-provider-id")
		c.Set("userName", "Eve")
		c.Set("userId", "eve@example.com")
		c.Next()
	})
	r.Use(ThreatModelMiddleware())

	wsHub := NewWebSocketHubForTests()
	tmHandler := NewThreatModelHandler(wsHub)
	diagramHandler := NewThreatModelDiagramHandler(wsHub)

	r.POST("/threat_models", tmHandler.CreateThreatModel)
	r.DELETE("/threat_models/:threat_model_id", tmHandler.DeleteThreatModel)
	r.POST("/threat_models/:threat_model_id/diagrams", func(c *gin.Context) {
		diagramHandler.CreateDiagram(c, c.Param("threat_model_id"))
	})
	r.PUT("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		diagramHandler.UpdateDiagram(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.PATCH("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		diagramHandler.PatchDiagram(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})
	r.DELETE("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		diagramHandler.DeleteDiagram(c, c.Param("threat_model_id"), c.Param("diagram_id"))
	})

	// Create threat model
	tmPayload := map[string]interface{}{
		"name":                   "Test TM",
		"threat_model_framework": "STRIDE",
	}
	tmBody, _ := json.Marshal(tmPayload)
	tmReq := httptest.NewRequest("POST", "/threat_models", bytes.NewBuffer(tmBody))
	tmReq.Header.Set("Content-Type", "application/json")
	tmW := httptest.NewRecorder()
	r.ServeHTTP(tmW, tmReq)
	require.Equal(t, http.StatusCreated, tmW.Code)

	var tm ThreatModel
	err := json.Unmarshal(tmW.Body.Bytes(), &tm)
	require.NoError(t, err)
	tmID := tm.Id.String()

	// Create diagram
	diagPayload := map[string]interface{}{
		"name": "Test Diagram",
		"type": "DFD-1.0.0",
	}
	diagBody, _ := json.Marshal(diagPayload)
	diagReq := httptest.NewRequest("POST", "/threat_models/"+tmID+"/diagrams", bytes.NewBuffer(diagBody))
	diagReq.Header.Set("Content-Type", "application/json")
	diagW := httptest.NewRecorder()
	r.ServeHTTP(diagW, diagReq)
	require.Equal(t, http.StatusCreated, diagW.Code)

	var diagram DfdDiagram
	err = json.Unmarshal(diagW.Body.Bytes(), &diagram)
	require.NoError(t, err)
	diagramID := diagram.Id.String()

	// No collaboration session active - verify no session exists
	assert.False(t, wsHub.HasActiveSession(diagramID))

	// Test PUT - should succeed
	updatePayload := map[string]interface{}{
		"name": "Updated Diagram",
		"type": "DFD-1.0.0",
	}
	updateBody, _ := json.Marshal(updatePayload)
	updateReq := httptest.NewRequest("PUT", "/threat_models/"+tmID+"/diagrams/"+diagramID, bytes.NewBuffer(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)
	assert.Equal(t, http.StatusOK, updateW.Code)

	// Test PATCH - should succeed
	patchPayload := []map[string]interface{}{
		{
			"op":    "replace",
			"path":  "/name",
			"value": "Patched Diagram",
		},
	}
	patchBody, _ := json.Marshal(patchPayload)
	patchReq := httptest.NewRequest("PATCH", "/threat_models/"+tmID+"/diagrams/"+diagramID, bytes.NewBuffer(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	r.ServeHTTP(patchW, patchReq)
	assert.Equal(t, http.StatusOK, patchW.Code)

	// Test DELETE diagram - should succeed
	deleteReq := httptest.NewRequest("DELETE", "/threat_models/"+tmID+"/diagrams/"+diagramID, nil)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)
	assert.Equal(t, http.StatusNoContent, deleteW.Code)

	// Test DELETE threat model - should succeed
	deleteTMReq := httptest.NewRequest("DELETE", "/threat_models/"+tmID, nil)
	deleteTMW := httptest.NewRecorder()
	r.ServeHTTP(deleteTMW, deleteTMReq)
	assert.Equal(t, http.StatusNoContent, deleteTMW.Code)
}
