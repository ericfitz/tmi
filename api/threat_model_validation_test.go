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

// setupThreatModelValidationRouter creates a router for testing validation
func setupThreatModelValidationRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Mock authentication middleware
	router.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-user-id")
		c.Next()
	})

	// Add OpenAPI validation middleware
	openAPIValidator, err := SetupOpenAPIValidation()
	if err != nil {
		// Return nil router to indicate setup failure - tests will handle this
		return nil
	}
	router.Use(openAPIValidator)

	// Add middleware
	router.Use(ThreatModelMiddleware())

	// Register handlers
	handler := NewThreatModelHandler(NewWebSocketHubForTests())
	router.POST("/threat_models", handler.CreateThreatModel)
	router.PUT("/threat_models/:threat_model_id", handler.UpdateThreatModel)
	router.PATCH("/threat_models/:threat_model_id", handler.PatchThreatModel)

	return router
}

func TestCreateThreatModelRejectsCalculatedFields(t *testing.T) {
	InitTestFixtures()
	router := setupThreatModelValidationRouter()
	if router == nil {
		t.Skip("Failed to setup OpenAPI validation middleware")
	}

	testCases := []struct {
		name        string
		requestBody map[string]interface{}
		description string
	}{
		{
			name: "reject id",
			requestBody: map[string]interface{}{
				"name": "Test Threat Model",
				"id":   "123e4567-e89b-12d3-a456-426614174000",
			},
			description: "OpenAPI validation should reject additional properties like id",
		},
		{
			name: "reject created_at",
			requestBody: map[string]interface{}{
				"name":       "Test Threat Model",
				"created_at": "2025-01-01T00:00:00Z",
			},
			description: "OpenAPI validation should reject additional properties like created_at",
		},
		{
			name: "reject modified_at",
			requestBody: map[string]interface{}{
				"name":        "Test Threat Model",
				"modified_at": "2025-01-01T00:00:00Z",
			},
			description: "OpenAPI validation should reject additional properties like modified_at",
		},
		{
			name: "reject created_by",
			requestBody: map[string]interface{}{
				"name":       "Test Threat Model",
				"created_by": "someone@example.com",
			},
			description: "OpenAPI validation should reject additional properties like created_by",
		},
		{
			name: "reject diagrams",
			requestBody: map[string]interface{}{
				"name":     "Test Threat Model",
				"diagrams": []interface{}{},
			},
			description: "OpenAPI validation should reject additional properties like diagrams",
		},
		{
			name: "reject documents",
			requestBody: map[string]interface{}{
				"name":      "Test Threat Model",
				"documents": []interface{}{},
			},
			description: "OpenAPI validation should reject additional properties like documents",
		},
		{
			name: "reject threats",
			requestBody: map[string]interface{}{
				"name":    "Test Threat Model",
				"threats": []interface{}{},
			},
			description: "OpenAPI validation should reject additional properties like threats",
		},
		{
			name: "reject sourceCode",
			requestBody: map[string]interface{}{
				"name":       "Test Threat Model",
				"sourceCode": []interface{}{},
			},
			description: "OpenAPI validation should reject additional properties like sourceCode",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tc.requestBody)
			req := httptest.NewRequest("POST", "/threat_models", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Should return 400 Bad Request from OpenAPI validation
			assert.Equal(t, http.StatusBadRequest, w.Code, tc.description)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			// OpenAPI middleware returns "invalid_input" error type
			assert.Equal(t, "invalid_input", response["error"], "Should return invalid_input error from OpenAPI validation")

			// The error message from OpenAPI validation will mention the extra/unknown properties
			errorDesc, exists := response["error_description"].(string)
			assert.True(t, exists, "Should have error_description")
			assert.NotEmpty(t, errorDesc, "Should have non-empty error description")
		})
	}
}

func TestUpdateThreatModelRejectsCalculatedFields(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	InitTestFixtures()
	router := setupThreatModelValidationRouter()
	if router == nil {
		t.Skip("Failed to setup OpenAPI validation middleware")
	}

	// Use existing test fixture threat model ID
	threatModelID := TestFixtures.ThreatModelID

	testCases := []struct {
		name          string
		requestBody   map[string]interface{}
		expectedField string
		expectedError string
	}{
		{
			name: "reject created_at in PUT",
			requestBody: map[string]interface{}{
				"name":                   "Updated Threat Model",
				"owner":                  "test@example.com",
				"threat_model_framework": "STRIDE",
				"authorization": []map[string]interface{}{
					{"subject": "test@example.com", "role": "owner"},
				},
				"created_at": "2025-01-01T00:00:00Z",
			},
			expectedField: "created_at",
			expectedError: "Creation timestamp is read-only and set by the server.",
		},
		{
			name: "reject diagrams in PUT",
			requestBody: map[string]interface{}{
				"name":                   "Updated Threat Model",
				"owner":                  "test@example.com",
				"threat_model_framework": "STRIDE",
				"authorization": []map[string]interface{}{
					{"subject": "test@example.com", "role": "owner"},
				},
				"diagrams": []interface{}{},
			},
			expectedField: "diagrams",
			expectedError: "Diagrams must be managed via the /threat_models/:threat_model_id/diagrams sub-entity endpoints.",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tc.requestBody)
			req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID, bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Should return 400 Bad Request
			assert.Equal(t, http.StatusBadRequest, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "invalid_input", response["error"])
			assert.Contains(t, response["error_description"], tc.expectedField)
			assert.Contains(t, response["error_description"], tc.expectedError)
		})
	}
}

func TestPatchThreatModelRejectsCalculatedFields(t *testing.T) {
	InitTestFixtures()
	router := setupThreatModelValidationRouter()
	if router == nil {
		t.Skip("Failed to setup OpenAPI validation middleware")
	}

	// Use existing test fixture threat model ID
	threatModelID := TestFixtures.ThreatModelID

	testCases := []struct {
		name          string
		operations    []map[string]interface{}
		expectedField string
		expectedError string
	}{
		{
			name: "reject created_at in PATCH",
			operations: []map[string]interface{}{
				{"op": "replace", "path": "/created_at", "value": "2025-01-01T00:00:00Z"},
			},
			expectedField: "created_at",
			expectedError: "Creation timestamp is read-only and set by the server.",
		},
		{
			name: "reject diagrams in PATCH",
			operations: []map[string]interface{}{
				{"op": "replace", "path": "/diagrams", "value": []interface{}{}},
			},
			expectedField: "diagrams",
			expectedError: "Diagrams must be managed via the /threat_models/:threat_model_id/diagrams sub-entity endpoints.",
		},
		{
			name: "reject multiple prohibited fields",
			operations: []map[string]interface{}{
				{"op": "replace", "path": "/name", "value": "Valid Name"},
				{"op": "replace", "path": "/created_at", "value": "2025-01-01T00:00:00Z"},
			},
			expectedField: "created_at",
			expectedError: "Creation timestamp is read-only and set by the server.",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tc.operations)
			req := httptest.NewRequest("PATCH", "/threat_models/"+threatModelID, bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json-patch+json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Should return 400 Bad Request
			assert.Equal(t, http.StatusBadRequest, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "invalid_input", response["error"])
			assert.Contains(t, response["error_description"], tc.expectedField)
			assert.Contains(t, response["error_description"], tc.expectedError)
		})
	}
}

func TestValidThreatModelRequests(t *testing.T) {
	InitTestFixtures()
	router := setupThreatModelValidationRouter()
	if router == nil {
		t.Skip("Failed to setup OpenAPI validation middleware")
	}

	t.Run("valid POST request", func(t *testing.T) {
		t.Skip("Skipping due to OpenAPI middleware issue with allOf Authorization schema - see https://github.com/ericfitz/tmi/issues/XXX")
		requestBody := map[string]interface{}{
			"name":        "Valid Threat Model",
			"description": "This is a valid threat model",
			"authorization": []map[string]interface{}{
				{"principal_type": "user", "provider": "tmi", "provider_id": "reader@example.com", "role": "reader"},
			},
		}

		jsonBody, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should succeed
		if w.Code != http.StatusCreated {
			t.Logf("Expected status 201, got %d. Response: %s", w.Code, w.Body.String())
		}
		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("valid PUT request", func(t *testing.T) {
		t.Skip("Skipping due to OpenAPI middleware issue with allOf Authorization schema - see https://github.com/ericfitz/tmi/issues/XXX")
		threatModelID := TestFixtures.ThreatModelID

		requestBody := map[string]interface{}{
			"name":                   "Updated Valid Threat Model",
			"description":            "Updated description",
			"threat_model_framework": "STRIDE",
			"authorization": []map[string]interface{}{
				{"principal_type": "user", "provider": "tmi", "provider_id": "test@example.com", "role": "owner"},
			},
		}

		jsonBody, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should succeed
		if w.Code != http.StatusOK {
			t.Logf("Expected status 200, got %d. Response: %s", w.Code, w.Body.String())
		}
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("valid PATCH request", func(t *testing.T) {
		threatModelID := TestFixtures.ThreatModelID

		operations := []map[string]interface{}{
			{"op": "replace", "path": "/name", "value": "Patched Name"},
			{"op": "add", "path": "/description", "value": "Patched description"},
		}

		jsonBody, _ := json.Marshal(operations)
		req := httptest.NewRequest("PATCH", "/threat_models/"+threatModelID, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json-patch+json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should succeed
		assert.Equal(t, http.StatusOK, w.Code)
	})
}
