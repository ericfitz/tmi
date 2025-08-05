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
		c.Set("userName", TestFixtures.OwnerUser)
		c.Set("userID", "test-user-id")
		c.Next()
	})

	// Add middleware
	router.Use(ThreatModelMiddleware())
	router.Use(DiagramMiddleware())

	// Register handlers
	threatModelHandler := NewThreatModelHandler()
	diagramHandler := NewDiagramHandler()

	// Threat Model routes
	router.GET("/threat_models", threatModelHandler.GetThreatModels)
	router.POST("/threat_models", threatModelHandler.CreateThreatModel)
	router.GET("/threat_models/:id", threatModelHandler.GetThreatModelByID)
	router.PUT("/threat_models/:id", threatModelHandler.UpdateThreatModel)
	router.PATCH("/threat_models/:id", threatModelHandler.PatchThreatModel)
	router.DELETE("/threat_models/:id", threatModelHandler.DeleteThreatModel)

	// Diagram routes
	router.GET("/diagrams", diagramHandler.GetDiagrams)
	router.POST("/diagrams", diagramHandler.CreateDiagram)
	router.GET("/diagrams/:id", diagramHandler.GetDiagramByID)
	router.PUT("/diagrams/:id", diagramHandler.UpdateDiagram)
	router.PATCH("/diagrams/:id", diagramHandler.PatchDiagram)
	router.DELETE("/diagrams/:id", diagramHandler.DeleteDiagram)

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
			"owner":       TestFixtures.OwnerUser,
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
		assert.Equal(t, requestBody["owner"], response["owner"])
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

	// Test GET /threat_models/:id
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

	// Test PUT /threat_models/:id
	t.Run("PUT", func(t *testing.T) {
		updateBody := map[string]interface{}{
			"id":          TestFixtures.ThreatModelID,
			"name":        "Updated Mock Test Threat Model",
			"description": "Updated description via PUT",
			"owner":       TestFixtures.OwnerUser,
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
	// Test POST /diagrams
	t.Run("POST", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"name":        "Mock Integration Test Diagram",
			"description": "A diagram created during mock integration testing",
		}

		jsonBody, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/diagrams", bytes.NewBuffer(jsonBody))
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
	})

	// Test GET /diagrams (list)
	t.Run("GET_List", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/diagrams", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should have at least the fixture data
		assert.GreaterOrEqual(t, len(response), 1)
	})

	// Test GET /diagrams/:id
	t.Run("GET_ByID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/diagrams/"+TestFixtures.DiagramID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, TestFixtures.DiagramID, response["id"])
		assert.Equal(t, TestFixtures.Diagram.Name, response["name"])
	})

	// Test PUT /diagrams/:id
	t.Run("PUT", func(t *testing.T) {
		updateBody := map[string]interface{}{
			"id":          TestFixtures.DiagramID,
			"name":        "Updated Mock Test Diagram",
			"description": "Updated description via PUT",
		}

		jsonBody, _ := json.Marshal(updateBody)
		req := httptest.NewRequest("PUT", "/diagrams/"+TestFixtures.DiagramID, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, TestFixtures.DiagramID, response["id"])
		assert.Equal(t, updateBody["name"], response["name"])
		assert.Equal(t, updateBody["description"], response["description"])
	})
}
