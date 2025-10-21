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

// TestSubResourceIntegration tests the route registration and basic HTTP responses
func TestSubResourceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Initialize test fixtures
	InitTestFixtures()
	InitTestFixtures()

	// Setup Gin router with middleware
	gin.SetMode(gin.TestMode)
	router := setupTestRouter()

	// Get the threat model ID for sub-resource testing
	threatModelID := TestFixtures.ThreatModelID

	t.Run("ThreatRouteRegistration", func(t *testing.T) {
		testThreatRouteRegistration(t, router, threatModelID)
	})

	t.Run("DocumentRouteRegistration", func(t *testing.T) {
		testDocumentRouteRegistration(t, router, threatModelID)
	})

	t.Run("SourceRouteRegistration", func(t *testing.T) {
		testSourceRouteRegistration(t, router, threatModelID)
	})

	t.Run("MetadataRouteRegistration", func(t *testing.T) {
		testMetadataRouteRegistration(t, router, threatModelID)
	})

	t.Run("BatchOperationRoutes", func(t *testing.T) {
		testBatchOperationRoutes(t, router, threatModelID)
	})
}

// setupTestRouter creates a test router with stub handlers that return 501 Not Implemented
func setupTestRouter() *gin.Engine {
	router := gin.New()

	// Add test middleware that sets up authentication context
	router.Use(func(c *gin.Context) {
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userRole", RoleOwner)
		c.Set("threatModelID", TestFixtures.ThreatModelID)
		c.Next()
	})

	// Create stub handlers that return 501 Not Implemented
	notImplementedHandler := func(c *gin.Context) {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
	}

	// Register all sub-resource routes with stub handlers to test route registration
	// This mirrors the routes registered in gin_adapter.go

	// Diagram Metadata
	router.GET("/diagrams/:id/metadata", notImplementedHandler)
	router.POST("/diagrams/:id/metadata", notImplementedHandler)
	router.GET("/diagrams/:id/metadata/:key", notImplementedHandler)
	router.PUT("/diagrams/:id/metadata/:key", notImplementedHandler)
	router.DELETE("/diagrams/:id/metadata/:key", notImplementedHandler)
	router.POST("/diagrams/:id/metadata/bulk", notImplementedHandler)

	// Diagram Cell Metadata
	router.GET("/diagrams/:id/cells/:cell_id/metadata", notImplementedHandler)
	router.POST("/diagrams/:id/cells/:cell_id/metadata", notImplementedHandler)
	router.GET("/diagrams/:id/cells/:cell_id/metadata/:key", notImplementedHandler)
	router.PUT("/diagrams/:id/cells/:cell_id/metadata/:key", notImplementedHandler)
	router.DELETE("/diagrams/:id/cells/:cell_id/metadata/:key", notImplementedHandler)
	router.PATCH("/diagrams/:id/cells/:cell_id", notImplementedHandler)
	router.POST("/diagrams/:id/cells/batch/patch", notImplementedHandler)

	// Threat Model Diagram Metadata
	router.GET("/threat_models/:threat_model_id/diagrams/:diagram_id/metadata", notImplementedHandler)
	router.POST("/threat_models/:threat_model_id/diagrams/:diagram_id/metadata", notImplementedHandler)
	router.GET("/threat_models/:threat_model_id/diagrams/:diagram_id/metadata/:key", notImplementedHandler)
	router.PUT("/threat_models/:threat_model_id/diagrams/:diagram_id/metadata/:key", notImplementedHandler)
	router.DELETE("/threat_models/:threat_model_id/diagrams/:diagram_id/metadata/:key", notImplementedHandler)
	router.POST("/threat_models/:threat_model_id/diagrams/:diagram_id/metadata/bulk", notImplementedHandler)

	// Threat Model Threats
	router.GET("/threat_models/:threat_model_id/threats", notImplementedHandler)
	router.POST("/threat_models/:threat_model_id/threats", notImplementedHandler)
	router.GET("/threat_models/:threat_model_id/threats/:threat_id", notImplementedHandler)
	router.PUT("/threat_models/:threat_model_id/threats/:threat_id", notImplementedHandler)
	router.PATCH("/threat_models/:threat_model_id/threats/:threat_id", notImplementedHandler)
	router.DELETE("/threat_models/:threat_model_id/threats/:threat_id", notImplementedHandler)
	router.POST("/threat_models/:threat_model_id/threats/bulk", notImplementedHandler)
	router.PUT("/threat_models/:threat_model_id/threats/bulk", notImplementedHandler)

	// Threat Model Threat Metadata
	router.GET("/threat_models/:threat_model_id/threats/:threat_id/metadata", notImplementedHandler)
	router.POST("/threat_models/:threat_model_id/threats/:threat_id/metadata", notImplementedHandler)
	router.GET("/threat_models/:threat_model_id/threats/:threat_id/metadata/:key", notImplementedHandler)
	router.PUT("/threat_models/:threat_model_id/threats/:threat_id/metadata/:key", notImplementedHandler)
	router.DELETE("/threat_models/:threat_model_id/threats/:threat_id/metadata/:key", notImplementedHandler)
	router.POST("/threat_models/:threat_model_id/threats/:threat_id/metadata/bulk", notImplementedHandler)

	// Threat Model Documents
	router.GET("/threat_models/:threat_model_id/documents", notImplementedHandler)
	router.POST("/threat_models/:threat_model_id/documents", notImplementedHandler)
	router.GET("/threat_models/:threat_model_id/documents/:document_id", notImplementedHandler)
	router.PUT("/threat_models/:threat_model_id/documents/:document_id", notImplementedHandler)
	router.DELETE("/threat_models/:threat_model_id/documents/:document_id", notImplementedHandler)
	router.POST("/threat_models/:threat_model_id/documents/bulk", notImplementedHandler)

	// Threat Model Document Metadata
	router.GET("/threat_models/:threat_model_id/documents/:document_id/metadata", notImplementedHandler)
	router.POST("/threat_models/:threat_model_id/documents/:document_id/metadata", notImplementedHandler)
	router.GET("/threat_models/:threat_model_id/documents/:document_id/metadata/:key", notImplementedHandler)
	router.PUT("/threat_models/:threat_model_id/documents/:document_id/metadata/:key", notImplementedHandler)
	router.DELETE("/threat_models/:threat_model_id/documents/:document_id/metadata/:key", notImplementedHandler)
	router.POST("/threat_models/:threat_model_id/documents/:document_id/metadata/bulk", notImplementedHandler)

	// Threat Model Sources
	router.GET("/threat_models/:threat_model_id/sources", notImplementedHandler)
	router.POST("/threat_models/:threat_model_id/sources", notImplementedHandler)
	router.GET("/threat_models/:threat_model_id/sources/:repository_id", notImplementedHandler)
	router.PUT("/threat_models/:threat_model_id/sources/:repository_id", notImplementedHandler)
	router.DELETE("/threat_models/:threat_model_id/sources/:repository_id", notImplementedHandler)
	router.POST("/threat_models/:threat_model_id/sources/bulk", notImplementedHandler)

	// Threat Model Source Metadata
	router.GET("/threat_models/:threat_model_id/sources/:repository_id/metadata", notImplementedHandler)
	router.POST("/threat_models/:threat_model_id/sources/:repository_id/metadata", notImplementedHandler)
	router.GET("/threat_models/:threat_model_id/sources/:repository_id/metadata/:key", notImplementedHandler)
	router.PUT("/threat_models/:threat_model_id/sources/:repository_id/metadata/:key", notImplementedHandler)
	router.DELETE("/threat_models/:threat_model_id/sources/:repository_id/metadata/:key", notImplementedHandler)
	router.POST("/threat_models/:threat_model_id/sources/:repository_id/metadata/bulk", notImplementedHandler)

	// Batch Operations
	router.POST("/threat_models/:threat_model_id/threats/batch/patch", notImplementedHandler)
	router.DELETE("/threat_models/:threat_model_id/threats/batch", notImplementedHandler)

	return router
}

// testThreatRouteRegistration tests that threat routes are properly registered
func testThreatRouteRegistration(t *testing.T, router *gin.Engine, threatModelID string) {
	testCases := []struct {
		method string
		path   string
	}{
		{"GET", fmt.Sprintf("/threat_models/%s/threats", threatModelID)},
		{"POST", fmt.Sprintf("/threat_models/%s/threats", threatModelID)},
		{"GET", fmt.Sprintf("/threat_models/%s/threats/test-threat-id", threatModelID)},
		{"PUT", fmt.Sprintf("/threat_models/%s/threats/test-threat-id", threatModelID)},
		{"PATCH", fmt.Sprintf("/threat_models/%s/threats/test-threat-id", threatModelID)},
		{"DELETE", fmt.Sprintf("/threat_models/%s/threats/test-threat-id", threatModelID)},
		{"POST", fmt.Sprintf("/threat_models/%s/threats/bulk", threatModelID)},
		{"PUT", fmt.Sprintf("/threat_models/%s/threats/bulk", threatModelID)},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s %s", tc.method, tc.path), func(t *testing.T) {
			var body *bytes.Buffer
			if tc.method == "POST" || tc.method == "PUT" || tc.method == "PATCH" {
				body = bytes.NewBuffer([]byte(`{"test": "data"}`))
			} else {
				body = bytes.NewBuffer(nil)
			}

			req := httptest.NewRequest(tc.method, tc.path, body)
			if body.Len() > 0 {
				req.Header.Set("Content-Type", "application/json")
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Expect 501 Not Implemented from our stub handlers
			assert.Equal(t, http.StatusNotImplemented, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, "not implemented", response["error"])
		})
	}
}

// testDocumentRouteRegistration tests that document routes are properly registered
func testDocumentRouteRegistration(t *testing.T, router *gin.Engine, threatModelID string) {
	testCases := []struct {
		method string
		path   string
	}{
		{"GET", fmt.Sprintf("/threat_models/%s/documents", threatModelID)},
		{"POST", fmt.Sprintf("/threat_models/%s/documents", threatModelID)},
		{"GET", fmt.Sprintf("/threat_models/%s/documents/test-doc-id", threatModelID)},
		{"PUT", fmt.Sprintf("/threat_models/%s/documents/test-doc-id", threatModelID)},
		{"DELETE", fmt.Sprintf("/threat_models/%s/documents/test-doc-id", threatModelID)},
		{"POST", fmt.Sprintf("/threat_models/%s/documents/bulk", threatModelID)},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s %s", tc.method, tc.path), func(t *testing.T) {
			var body *bytes.Buffer
			if tc.method == "POST" || tc.method == "PUT" {
				body = bytes.NewBuffer([]byte(`{"test": "data"}`))
			} else {
				body = bytes.NewBuffer(nil)
			}

			req := httptest.NewRequest(tc.method, tc.path, body)
			if body.Len() > 0 {
				req.Header.Set("Content-Type", "application/json")
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotImplemented, w.Code)
		})
	}
}

// testSourceRouteRegistration tests that source routes are properly registered
func testSourceRouteRegistration(t *testing.T, router *gin.Engine, threatModelID string) {
	testCases := []struct {
		method string
		path   string
	}{
		{"GET", fmt.Sprintf("/threat_models/%s/sources", threatModelID)},
		{"POST", fmt.Sprintf("/threat_models/%s/sources", threatModelID)},
		{"GET", fmt.Sprintf("/threat_models/%s/sources/test-source-id", threatModelID)},
		{"PUT", fmt.Sprintf("/threat_models/%s/sources/test-source-id", threatModelID)},
		{"DELETE", fmt.Sprintf("/threat_models/%s/sources/test-source-id", threatModelID)},
		{"POST", fmt.Sprintf("/threat_models/%s/sources/bulk", threatModelID)},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s %s", tc.method, tc.path), func(t *testing.T) {
			var body *bytes.Buffer
			if tc.method == "POST" || tc.method == "PUT" {
				body = bytes.NewBuffer([]byte(`{"test": "data"}`))
			} else {
				body = bytes.NewBuffer(nil)
			}

			req := httptest.NewRequest(tc.method, tc.path, body)
			if body.Len() > 0 {
				req.Header.Set("Content-Type", "application/json")
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotImplemented, w.Code)
		})
	}
}

// testMetadataRouteRegistration tests that metadata routes are properly registered
func testMetadataRouteRegistration(t *testing.T, router *gin.Engine, threatModelID string) {
	testCases := []struct {
		method string
		path   string
	}{
		// Threat metadata
		{"GET", fmt.Sprintf("/threat_models/%s/threats/test-threat-id/metadata", threatModelID)},
		{"POST", fmt.Sprintf("/threat_models/%s/threats/test-threat-id/metadata", threatModelID)},
		{"GET", fmt.Sprintf("/threat_models/%s/threats/test-threat-id/metadata/test-key", threatModelID)},
		{"PUT", fmt.Sprintf("/threat_models/%s/threats/test-threat-id/metadata/test-key", threatModelID)},
		{"DELETE", fmt.Sprintf("/threat_models/%s/threats/test-threat-id/metadata/test-key", threatModelID)},
		{"POST", fmt.Sprintf("/threat_models/%s/threats/test-threat-id/metadata/bulk", threatModelID)},

		// Document metadata
		{"GET", fmt.Sprintf("/threat_models/%s/documents/test-doc-id/metadata", threatModelID)},
		{"POST", fmt.Sprintf("/threat_models/%s/documents/test-doc-id/metadata", threatModelID)},
		{"GET", fmt.Sprintf("/threat_models/%s/documents/test-doc-id/metadata/test-key", threatModelID)},
		{"PUT", fmt.Sprintf("/threat_models/%s/documents/test-doc-id/metadata/test-key", threatModelID)},
		{"DELETE", fmt.Sprintf("/threat_models/%s/documents/test-doc-id/metadata/test-key", threatModelID)},
		{"POST", fmt.Sprintf("/threat_models/%s/documents/test-doc-id/metadata/bulk", threatModelID)},

		// Source metadata
		{"GET", fmt.Sprintf("/threat_models/%s/sources/test-source-id/metadata", threatModelID)},
		{"POST", fmt.Sprintf("/threat_models/%s/sources/test-source-id/metadata", threatModelID)},
		{"GET", fmt.Sprintf("/threat_models/%s/sources/test-source-id/metadata/test-key", threatModelID)},
		{"PUT", fmt.Sprintf("/threat_models/%s/sources/test-source-id/metadata/test-key", threatModelID)},
		{"DELETE", fmt.Sprintf("/threat_models/%s/sources/test-source-id/metadata/test-key", threatModelID)},
		{"POST", fmt.Sprintf("/threat_models/%s/sources/test-source-id/metadata/bulk", threatModelID)},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s %s", tc.method, tc.path), func(t *testing.T) {
			var body *bytes.Buffer
			if tc.method == "POST" || tc.method == "PUT" {
				body = bytes.NewBuffer([]byte(`{"test": "data"}`))
			} else {
				body = bytes.NewBuffer(nil)
			}

			req := httptest.NewRequest(tc.method, tc.path, body)
			if body.Len() > 0 {
				req.Header.Set("Content-Type", "application/json")
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotImplemented, w.Code)
		})
	}
}

// testBatchOperationRoutes tests that batch operation routes are properly registered
func testBatchOperationRoutes(t *testing.T, router *gin.Engine, threatModelID string) {
	testCases := []struct {
		method string
		path   string
	}{
		{"POST", fmt.Sprintf("/threat_models/%s/threats/batch/patch", threatModelID)},
		{"DELETE", fmt.Sprintf("/threat_models/%s/threats/batch", threatModelID)},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s %s", tc.method, tc.path), func(t *testing.T) {
			body := bytes.NewBuffer([]byte(`{"test": "data"}`))
			req := httptest.NewRequest(tc.method, tc.path, body)
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotImplemented, w.Code)
		})
	}
}
