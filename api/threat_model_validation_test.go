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
		c.Set("userName", "test@example.com")
		c.Set("userID", "test-user-id")
		c.Next()
	})

	// Add middleware
	router.Use(ThreatModelMiddleware())

	// Register handlers
	handler := NewThreatModelHandler()
	router.POST("/threat_models", handler.CreateThreatModel)
	router.PUT("/threat_models/:id", handler.UpdateThreatModel)
	router.PATCH("/threat_models/:id", handler.PatchThreatModel)

	return router
}

func TestCreateThreatModelRejectsCalculatedFields(t *testing.T) {
	InitTestFixtures()
	router := setupThreatModelValidationRouter()

	testCases := []struct {
		name          string
		requestBody   map[string]interface{}
		expectedField string
		expectedError string
	}{
		{
			name: "reject document_count",
			requestBody: map[string]interface{}{
				"name":           "Test Threat Model",
				"document_count": 5,
			},
			expectedField: "document_count",
			expectedError: "Count fields are calculated automatically and cannot be set directly.",
		},
		{
			name: "reject source_count",
			requestBody: map[string]interface{}{
				"name":         "Test Threat Model",
				"source_count": 3,
			},
			expectedField: "source_count",
			expectedError: "Count fields are calculated automatically and cannot be set directly.",
		},
		{
			name: "reject diagram_count",
			requestBody: map[string]interface{}{
				"name":          "Test Threat Model",
				"diagram_count": 2,
			},
			expectedField: "diagram_count",
			expectedError: "Count fields are calculated automatically and cannot be set directly.",
		},
		{
			name: "reject threat_count",
			requestBody: map[string]interface{}{
				"name":         "Test Threat Model",
				"threat_count": 10,
			},
			expectedField: "threat_count",
			expectedError: "Count fields are calculated automatically and cannot be set directly.",
		},
		{
			name: "reject id",
			requestBody: map[string]interface{}{
				"name": "Test Threat Model",
				"id":   "123e4567-e89b-12d3-a456-426614174000",
			},
			expectedField: "id",
			expectedError: "The ID is read-only and set by the server.",
		},
		{
			name: "reject created_at",
			requestBody: map[string]interface{}{
				"name":       "Test Threat Model",
				"created_at": "2025-01-01T00:00:00Z",
			},
			expectedField: "created_at",
			expectedError: "Creation timestamp is read-only and set by the server.",
		},
		{
			name: "reject modified_at",
			requestBody: map[string]interface{}{
				"name":        "Test Threat Model",
				"modified_at": "2025-01-01T00:00:00Z",
			},
			expectedField: "modified_at",
			expectedError: "Modification timestamp is managed automatically by the server.",
		},
		{
			name: "reject created_by",
			requestBody: map[string]interface{}{
				"name":       "Test Threat Model",
				"created_by": "someone@example.com",
			},
			expectedField: "created_by",
			expectedError: "The creator field is read-only and set during creation.",
		},
		{
			name: "reject owner",
			requestBody: map[string]interface{}{
				"name":  "Test Threat Model",
				"owner": "someone@example.com",
			},
			expectedField: "owner",
			expectedError: "The owner field is set automatically to the authenticated user during creation.",
		},
		{
			name: "reject diagrams",
			requestBody: map[string]interface{}{
				"name":     "Test Threat Model",
				"diagrams": []interface{}{},
			},
			expectedField: "diagrams",
			expectedError: "Diagrams must be managed via the /threat_models/:id/diagrams sub-entity endpoints.",
		},
		{
			name: "reject documents",
			requestBody: map[string]interface{}{
				"name":      "Test Threat Model",
				"documents": []interface{}{},
			},
			expectedField: "documents",
			expectedError: "Documents must be managed via the /threat_models/:id/documents sub-entity endpoints.",
		},
		{
			name: "reject threats",
			requestBody: map[string]interface{}{
				"name":    "Test Threat Model",
				"threats": []interface{}{},
			},
			expectedField: "threats",
			expectedError: "Threats must be managed via the /threat_models/:id/threats sub-entity endpoints.",
		},
		{
			name: "reject sourceCode",
			requestBody: map[string]interface{}{
				"name":       "Test Threat Model",
				"sourceCode": []interface{}{},
			},
			expectedField: "sourceCode",
			expectedError: "Source code entries must be managed via the /threat_models/:id/sources sub-entity endpoints.",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tc.requestBody)
			req := httptest.NewRequest("POST", "/threat_models", bytes.NewBuffer(jsonBody))
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

func TestUpdateThreatModelRejectsCalculatedFields(t *testing.T) {
	InitTestFixtures()
	router := setupThreatModelValidationRouter()

	// Use existing test fixture threat model ID
	threatModelID := TestFixtures.ThreatModelID

	testCases := []struct {
		name          string
		requestBody   map[string]interface{}
		expectedField string
		expectedError string
	}{
		{
			name: "reject document_count in PUT",
			requestBody: map[string]interface{}{
				"name":                   "Updated Threat Model",
				"owner":                  "test@example.com",
				"threat_model_framework": "STRIDE",
				"authorization": []map[string]interface{}{
					{"subject": "test@example.com", "role": "owner"},
				},
				"document_count": 5,
			},
			expectedField: "document_count",
			expectedError: "Count fields are calculated automatically and cannot be set directly.",
		},
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
			expectedError: "Diagrams must be managed via the /threat_models/:id/diagrams sub-entity endpoints.",
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

	// Use existing test fixture threat model ID
	threatModelID := TestFixtures.ThreatModelID

	testCases := []struct {
		name          string
		operations    []map[string]interface{}
		expectedField string
		expectedError string
	}{
		{
			name: "reject document_count in PATCH",
			operations: []map[string]interface{}{
				{"op": "replace", "path": "/document_count", "value": 5},
			},
			expectedField: "document_count",
			expectedError: "Count fields are calculated automatically and cannot be set directly.",
		},
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
			expectedError: "Diagrams must be managed via the /threat_models/:id/diagrams sub-entity endpoints.",
		},
		{
			name: "reject multiple prohibited fields",
			operations: []map[string]interface{}{
				{"op": "replace", "path": "/name", "value": "Valid Name"},
				{"op": "replace", "path": "/threat_count", "value": 10},
			},
			expectedField: "threat_count",
			expectedError: "Count fields are calculated automatically and cannot be set directly.",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tc.operations)
			req := httptest.NewRequest("PATCH", "/threat_models/"+threatModelID, bytes.NewBuffer(jsonBody))
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

func TestValidThreatModelRequests(t *testing.T) {
	InitTestFixtures()
	router := setupThreatModelValidationRouter()

	t.Run("valid POST request", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"name":        "Valid Threat Model",
			"description": "This is a valid threat model",
			"authorization": []map[string]interface{}{
				{"subject": "reader@example.com", "role": "reader"},
			},
		}

		jsonBody, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should succeed
		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("valid PUT request", func(t *testing.T) {
		threatModelID := TestFixtures.ThreatModelID

		requestBody := map[string]interface{}{
			"name":                   "Updated Valid Threat Model",
			"description":            "Updated description",
			"owner":                  "test@example.com",
			"threat_model_framework": "STRIDE",
			"authorization": []map[string]interface{}{
				{"subject": "test@example.com", "role": "owner"},
			},
		}

		jsonBody, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should succeed
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
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should succeed
		assert.Equal(t, http.StatusOK, w.Code)
	})
}
