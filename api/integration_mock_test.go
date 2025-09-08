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

// TestEndpointIntegrationMock tests the API endpoints with mock authentication
// This test doesn't require a real database and can run in CI/CD environments
func TestEndpointIntegrationMock(t *testing.T) {
	// Initialize test fixtures
	InitTestFixtures()

	// Setup Gin router in test mode
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Mock authentication middleware
	router.Use(func(c *gin.Context) {
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userID", "test-user-id")
		c.Next()
	})

	// Add middleware
	router.Use(ThreatModelMiddleware())
	router.Use(DiagramMiddleware())

	// Register handlers
	threatModelHandler := NewThreatModelHandler()
	diagramHandler := NewThreatModelDiagramHandler(NewWebSocketHubForTests())

	// Threat Model routes
	router.GET("/threat_models", threatModelHandler.GetThreatModels)
	router.POST("/threat_models", threatModelHandler.CreateThreatModel)
	router.GET("/threat_models/:threat_model_id", threatModelHandler.GetThreatModelByID)
	router.PUT("/threat_models/:threat_model_id", threatModelHandler.UpdateThreatModel)
	router.PATCH("/threat_models/:threat_model_id", threatModelHandler.PatchThreatModel)
	router.DELETE("/threat_models/:threat_model_id", threatModelHandler.DeleteThreatModel)

	// Threat model diagram sub-entity routes only
	router.POST("/threat_models/:threat_model_id/diagrams", func(c *gin.Context) {
		diagramHandler.CreateDiagram(c, c.Param("threat_model_id"))
	})
	router.GET("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		threatModelID := c.Param("threat_model_id")
		diagramID := c.Param("diagram_id")
		diagramHandler.GetDiagramByID(c, threatModelID, diagramID)
	})
	router.PUT("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		threatModelID := c.Param("threat_model_id")
		diagramID := c.Param("diagram_id")
		diagramHandler.UpdateDiagram(c, threatModelID, diagramID)
	})
	router.PATCH("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		threatModelID := c.Param("threat_model_id")
		diagramID := c.Param("diagram_id")
		diagramHandler.PatchDiagram(c, threatModelID, diagramID)
	})
	router.DELETE("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		threatModelID := c.Param("threat_model_id")
		diagramID := c.Param("diagram_id")
		diagramHandler.DeleteDiagram(c, threatModelID, diagramID)
	})

	// Run sub-tests
	t.Run("ThreatModelEndpoints", func(t *testing.T) {
		testThreatModelEndpointsMock(t, router)
	})

	t.Run("DiagramEndpoints", func(t *testing.T) {
		testDiagramEndpointsMock(t, router)
	})
}

func testThreatModelEndpointsMock(t *testing.T, router *gin.Engine) {
	// Test POST /threat_models
	t.Run("POST", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"name":        "Mock Integration Test Threat Model",
			"description": "A threat model created during mock integration testing",
		}

		jsonBody, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Logf("Expected 201, got %d. Response body: %s", w.Code, w.Body.String())
		}
		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.NotEmpty(t, response["id"])
		assert.Equal(t, requestBody["name"], response["name"])
		assert.Equal(t, requestBody["description"], response["description"])

		// Check owner field: if not provided or nil, should default to authenticated user
		expectedOwner := "test@example.com" // Default to authenticated user
		if requestBody["owner"] != nil {
			expectedOwner = requestBody["owner"].(string)
		}
		assert.Equal(t, expectedOwner, response["owner"])
	})

	// Test GET /threat_models (list)
	t.Run("GET_List", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/threat_models", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should have at least the fixture data
		assert.GreaterOrEqual(t, len(response), 1)
	})

	// Test GET /threat_models/:threat_model_id
	t.Run("GET_ByID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/threat_models/"+TestFixtures.ThreatModelID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, TestFixtures.ThreatModelID, response["id"])
		assert.Equal(t, TestFixtures.ThreatModel.Name, response["name"])
	})

	// Test PUT /threat_models/:threat_model_id
	t.Run("PUT", func(t *testing.T) {
		updateBody := map[string]interface{}{
			"name":                   "Updated Mock Test Threat Model",
			"description":            "Updated description via PUT",
			"owner":                  TestFixtures.OwnerUser,
			"threat_model_framework": "STRIDE",
			"authorization":          []interface{}{},
		}

		jsonBody, _ := json.Marshal(updateBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+TestFixtures.ThreatModelID, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, TestFixtures.ThreatModelID, response["id"])
		assert.Equal(t, updateBody["name"], response["name"])
		assert.Equal(t, updateBody["description"], response["description"])
	})
}

func testDiagramEndpointsMock(t *testing.T, router *gin.Engine) {
	// First create a threat model for the sub-entity tests
	var threatModelID string
	t.Run("CreateThreatModelForDiagrams", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"name": "Mock Test Threat Model for Diagrams",
		}

		jsonBody, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		threatModelID = response["id"].(string)
	})

	// Test POST /threat_models/:threat_model_id/diagrams
	t.Run("POST", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"name": "Mock Integration Test Diagram",
			"type": "DFD-1.0.0",
		}

		jsonBody, _ := json.Marshal(requestBody)
		diagramURL := fmt.Sprintf("/threat_models/%s/diagrams", threatModelID)
		req := httptest.NewRequest("POST", diagramURL, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Logf("Expected 201, got %d. Response body: %s", w.Code, w.Body.String())
		}
		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.NotEmpty(t, response["id"])
		assert.Equal(t, requestBody["name"], response["name"])
		assert.Equal(t, requestBody["type"], response["type"])
	})

	// Note: GET list would be /threat_models/:threat_model_id/diagrams - skipping for now

	// Test GET /threat_models/:threat_model_id/diagrams/:diagram_id
	t.Run("GET_ByID", func(t *testing.T) {
		getDiagramURL := fmt.Sprintf("/threat_models/%s/diagrams/%s", TestFixtures.ThreatModelID, TestFixtures.DiagramID)
		req := httptest.NewRequest("GET", getDiagramURL, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, TestFixtures.DiagramID, response["id"])
		assert.Equal(t, TestFixtures.Diagram.Name, response["name"])
	})

	// Test PUT /threat_models/:threat_model_id/diagrams/:diagram_id
	t.Run("PUT", func(t *testing.T) {
		updateBody := map[string]interface{}{
			"name": "Updated Mock Test Diagram",
		}

		jsonBody, _ := json.Marshal(updateBody)
		updateDiagramURL := fmt.Sprintf("/threat_models/%s/diagrams/%s", TestFixtures.ThreatModelID, TestFixtures.DiagramID)
		req := httptest.NewRequest("PUT", updateDiagramURL, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, TestFixtures.DiagramID, response["id"])
		assert.Equal(t, updateBody["name"], response["name"])
	})
}
