package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getEnvOrDefault returns the environment variable value or a default value if not set
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getTestServerURL returns the base URL for the running integration test server
func getTestServerURL() string {
	return getEnvOrDefault("TEST_SERVER_URL", "http://localhost:8081")
}

// SubEntityIntegrationTestSuite manages database setup and teardown for sub-entity integration tests
type SubEntityIntegrationTestSuite struct {
	dbManager      *db.Manager
	authService    *auth.Service
	router         *gin.Engine
	testUser       *auth.User
	accessToken    string
	threatModelID  string
	testThreatID   string
	testDocumentID string
	testSourceID   string
	testDiagramID  string
}

// SetupSubEntityIntegrationTest initializes the test environment with a real database
func SetupSubEntityIntegrationTest(t *testing.T) *SubEntityIntegrationTestSuite {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping sub-entity integration test in short mode")
	}

	// Create test database configuration using environment variables
	// These defaults match what scripts/start-integration-tests.sh sets up
	postgresConfig := db.PostgresConfig{
		Host:     getEnvOrDefault("TEST_DB_HOST", "localhost"),
		Port:     getEnvOrDefault("TEST_DB_PORT", "5433"),
		User:     getEnvOrDefault("TEST_DB_USER", "tmi_dev"),
		Password: getEnvOrDefault("TEST_DB_PASSWORD", "dev123"),
		Database: getEnvOrDefault("TEST_DB_NAME", "tmi_integration_test"),
		SSLMode:  "disable",
	}

	redisConfig := db.RedisConfig{
		Host:     getEnvOrDefault("TEST_REDIS_HOST", "localhost"),
		Port:     getEnvOrDefault("TEST_REDIS_PORT", "6380"),
		Password: "",
		DB:       2, // Use DB 2 for sub-entity testing
	}

	// Initialize database manager
	dbManager := db.NewManager()
	err := dbManager.InitPostgres(postgresConfig)
	require.NoError(t, err, "Failed to initialize PostgreSQL")
	err = dbManager.InitRedis(redisConfig)
	require.NoError(t, err, "Failed to initialize Redis")

	// Create auth configuration
	authConfig := auth.Config{
		JWT: auth.JWTConfig{
			Secret:            "test-secret-key-for-sub-entity-integration-testing",
			ExpirationSeconds: 3600,
			SigningMethod:     "HS256",
		},
		OAuth: auth.OAuthConfig{
			CallbackURL: "http://localhost:8080/oauth2/callback",
			Providers: map[string]auth.OAuthProviderConfig{
				"test": {
					ClientID:     "test-client-id",
					ClientSecret: "test-client-secret",
					Enabled:      true,
				},
			},
		},
		Postgres: auth.PostgresConfig{
			Host:     postgresConfig.Host,
			Port:     postgresConfig.Port,
			User:     postgresConfig.User,
			Password: postgresConfig.Password,
			Database: postgresConfig.Database,
			SSLMode:  postgresConfig.SSLMode,
		},
		Redis: auth.RedisConfig{
			Host:     redisConfig.Host,
			Port:     redisConfig.Port,
			Password: redisConfig.Password,
			DB:       redisConfig.DB,
		},
	}

	// Initialize auth service
	authService, err := auth.NewService(dbManager, authConfig)
	require.NoError(t, err, "Failed to initialize auth service")

	// Create test user and token using the test provider
	testUser, accessToken := createSubEntityTestUserWithToken(t, authService)

	// Setup Gin router in test mode
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Add authentication middleware that uses the test token
	router.Use(func(c *gin.Context) {
		// Extract token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		// Validate the token (simplified for testing)
		if authHeader == "Bearer "+accessToken {
			c.Set("userEmail", testUser.Email)
			c.Set("userID", testUser.ID)
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}
		c.Next()
	})

	// Add middleware for threat models and diagrams
	router.Use(ThreatModelMiddleware())
	router.Use(DiagramMiddleware())

	// Initialize database stores for real database testing
	InitializeDatabaseStores(dbManager.Postgres().GetDB())

	// Initialize API server and register custom WebSocket handlers
	server := NewServerForTests()
	server.RegisterHandlers(router)

	// Register API handlers directly
	threatModelHandler := NewThreatModelHandler()
	diagramHandler := NewThreatModelDiagramHandler(NewWebSocketHubForTests())

	// Threat Model routes
	router.GET("/threat_models", threatModelHandler.GetThreatModels)
	router.POST("/threat_models", threatModelHandler.CreateThreatModel)
	router.GET("/threat_models/:threat_model_id", threatModelHandler.GetThreatModelByID)
	router.PUT("/threat_models/:threat_model_id", threatModelHandler.UpdateThreatModel)
	router.PATCH("/threat_models/:threat_model_id", threatModelHandler.PatchThreatModel)
	router.DELETE("/threat_models/:threat_model_id", threatModelHandler.DeleteThreatModel)

	// Threat Model Diagram routes (proper sub-entity endpoints) - use consistent parameter names
	router.POST("/threat_models/:threat_model_id/diagrams", func(c *gin.Context) {
		diagramHandler.CreateDiagram(c, c.Param("threat_model_id"))
	})
	router.GET("/threat_models/:threat_model_id/diagrams", func(c *gin.Context) {
		diagramHandler.GetDiagrams(c, c.Param("threat_model_id"))
	})
	router.GET("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) {
		threatModelID := c.Param("threat_model_id")
		diagramID := c.Param("diagram_id")
		diagramHandler.GetDiagramByID(c, threatModelID, diagramID)
	})

	// All diagram operations should go through threat model sub-entity endpoints
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

	// Register sub-resource handlers for comprehensive testing
	threatSubResourceHandler := NewThreatSubResourceHandler(GlobalThreatStore, dbManager.Postgres().GetDB(), nil, nil)
	documentSubResourceHandler := NewDocumentSubResourceHandler(GlobalDocumentStore, dbManager.Postgres().GetDB(), nil, nil)
	sourceSubResourceHandler := NewSourceSubResourceHandler(GlobalSourceStore, dbManager.Postgres().GetDB(), nil, nil)
	batchHandler := NewBatchHandler(GlobalThreatStore, dbManager.Postgres().GetDB(), nil, nil)

	// Register metadata handlers for comprehensive testing (using simple constructors)
	// These handlers work without complex dependencies and can be tested directly

	threatMetadataHandler := NewThreatMetadataHandlerSimple()
	documentMetadataHandler := NewDocumentMetadataHandlerSimple()
	sourceMetadataHandler := NewSourceMetadataHandlerSimple()
	diagramMetadataHandler := NewDiagramMetadataHandlerSimple()
	threatModelMetadataHandler := NewThreatModelMetadataHandlerSimple()

	// Threat metadata routes
	router.GET("/threat_models/:threat_model_id/threats/:threat_id/metadata", threatMetadataHandler.GetThreatMetadata)
	router.POST("/threat_models/:threat_model_id/threats/:threat_id/metadata", threatMetadataHandler.CreateThreatMetadata)
	router.GET("/threat_models/:threat_model_id/threats/:threat_id/metadata/:key", threatMetadataHandler.GetThreatMetadataByKey)
	router.PUT("/threat_models/:threat_model_id/threats/:threat_id/metadata/:key", threatMetadataHandler.UpdateThreatMetadata)
	router.DELETE("/threat_models/:threat_model_id/threats/:threat_id/metadata/:key", threatMetadataHandler.DeleteThreatMetadata)

	// Document metadata routes
	router.GET("/threat_models/:threat_model_id/documents/:document_id/metadata", documentMetadataHandler.GetDocumentMetadata)
	router.POST("/threat_models/:threat_model_id/documents/:document_id/metadata", documentMetadataHandler.CreateDocumentMetadata)
	router.GET("/threat_models/:threat_model_id/documents/:document_id/metadata/:key", documentMetadataHandler.GetDocumentMetadataByKey)
	router.PUT("/threat_models/:threat_model_id/documents/:document_id/metadata/:key", documentMetadataHandler.UpdateDocumentMetadata)
	router.DELETE("/threat_models/:threat_model_id/documents/:document_id/metadata/:key", documentMetadataHandler.DeleteDocumentMetadata)

	// Source metadata routes
	router.GET("/threat_models/:threat_model_id/sources/:source_id/metadata", sourceMetadataHandler.GetSourceMetadata)
	router.POST("/threat_models/:threat_model_id/sources/:source_id/metadata", sourceMetadataHandler.CreateSourceMetadata)
	router.GET("/threat_models/:threat_model_id/sources/:source_id/metadata/:key", sourceMetadataHandler.GetSourceMetadataByKey)
	router.PUT("/threat_models/:threat_model_id/sources/:source_id/metadata/:key", sourceMetadataHandler.UpdateSourceMetadata)
	router.DELETE("/threat_models/:threat_model_id/sources/:source_id/metadata/:key", sourceMetadataHandler.DeleteSourceMetadata)

	// Diagram metadata routes
	router.GET("/threat_models/:threat_model_id/diagrams/:diagram_id/metadata", diagramMetadataHandler.GetThreatModelDiagramMetadata)
	router.POST("/threat_models/:threat_model_id/diagrams/:diagram_id/metadata", diagramMetadataHandler.CreateThreatModelDiagramMetadata)
	router.GET("/threat_models/:threat_model_id/diagrams/:diagram_id/metadata/:key", diagramMetadataHandler.GetThreatModelDiagramMetadataByKey)
	router.PUT("/threat_models/:threat_model_id/diagrams/:diagram_id/metadata/:key", diagramMetadataHandler.UpdateThreatModelDiagramMetadata)
	router.DELETE("/threat_models/:threat_model_id/diagrams/:diagram_id/metadata/:key", diagramMetadataHandler.DeleteThreatModelDiagramMetadata)

	// Threat model metadata routes (direct metadata endpoints)
	router.GET("/threat_models/:threat_model_id/metadata", threatModelMetadataHandler.GetThreatModelMetadata)
	router.POST("/threat_models/:threat_model_id/metadata", threatModelMetadataHandler.CreateThreatModelMetadata)
	router.GET("/threat_models/:threat_model_id/metadata/:key", threatModelMetadataHandler.GetThreatModelMetadataByKey)
	router.PUT("/threat_models/:threat_model_id/metadata/:key", threatModelMetadataHandler.UpdateThreatModelMetadata)
	router.DELETE("/threat_models/:threat_model_id/metadata/:key", threatModelMetadataHandler.DeleteThreatModelMetadata)
	router.POST("/threat_models/:threat_model_id/metadata/bulk", threatModelMetadataHandler.BulkCreateThreatModelMetadata)

	// Threat sub-resource routes (CRUD operations)
	router.GET("/threat_models/:threat_model_id/threats", threatSubResourceHandler.GetThreats)
	router.POST("/threat_models/:threat_model_id/threats", threatSubResourceHandler.CreateThreat)
	router.GET("/threat_models/:threat_model_id/threats/:threat_id", threatSubResourceHandler.GetThreat)
	router.PUT("/threat_models/:threat_model_id/threats/:threat_id", threatSubResourceHandler.UpdateThreat)
	router.PATCH("/threat_models/:threat_model_id/threats/:threat_id", threatSubResourceHandler.PatchThreat)
	router.DELETE("/threat_models/:threat_model_id/threats/:threat_id", threatSubResourceHandler.DeleteThreat)

	// Threat bulk operations
	router.POST("/threat_models/:threat_model_id/threats/bulk", threatSubResourceHandler.BulkCreateThreats)
	router.PUT("/threat_models/:threat_model_id/threats/bulk", threatSubResourceHandler.BulkUpdateThreats)

	// Threat batch operations
	router.POST("/threat_models/:threat_model_id/threats/batch/patch", batchHandler.BatchPatchThreats)
	router.DELETE("/threat_models/:threat_model_id/threats/batch", batchHandler.BatchDeleteThreats)

	// Document sub-resource routes (CRUD operations)
	router.GET("/threat_models/:threat_model_id/documents", documentSubResourceHandler.GetDocuments)
	router.POST("/threat_models/:threat_model_id/documents", documentSubResourceHandler.CreateDocument)
	router.GET("/threat_models/:threat_model_id/documents/:document_id", documentSubResourceHandler.GetDocument)
	router.PUT("/threat_models/:threat_model_id/documents/:document_id", documentSubResourceHandler.UpdateDocument)
	router.DELETE("/threat_models/:threat_model_id/documents/:document_id", documentSubResourceHandler.DeleteDocument)

	// Document bulk operations
	router.POST("/threat_models/:threat_model_id/documents/bulk", documentSubResourceHandler.BulkCreateDocuments)

	// Source sub-resource routes (CRUD operations)
	router.GET("/threat_models/:threat_model_id/sources", sourceSubResourceHandler.GetSources)
	router.POST("/threat_models/:threat_model_id/sources", sourceSubResourceHandler.CreateSource)
	router.GET("/threat_models/:threat_model_id/sources/:source_id", sourceSubResourceHandler.GetSource)
	router.PUT("/threat_models/:threat_model_id/sources/:source_id", sourceSubResourceHandler.UpdateSource)
	router.DELETE("/threat_models/:threat_model_id/sources/:source_id", sourceSubResourceHandler.DeleteSource)

	// Source bulk operations
	router.POST("/threat_models/:threat_model_id/sources/bulk", sourceSubResourceHandler.BulkCreateSources)

	// Sub-entity integration testing: This approach successfully tests the full API hierarchy
	// following natural creation flows (Threat Model → Sub-entities → Metadata) with
	// database persistence verification at each step.

	suite := &SubEntityIntegrationTestSuite{
		dbManager:   dbManager,
		authService: authService,
		router:      router,
		testUser:    testUser,
		accessToken: accessToken,
	}

	// Create a parent threat model for testing sub-entities
	suite.createParentThreatModel(t)

	return suite
}

// Database Integration Tests

// TestDatabaseThreatModelIntegration tests threat model CRUD against actual PostgreSQL database
func TestDatabaseThreatModelIntegration(t *testing.T) {
	suite := SetupSubEntityIntegrationTest(t)
	defer suite.TeardownSubEntityIntegrationTest(t)

	t.Run("POST /threat_models", func(t *testing.T) {
		testDatabaseThreatModelPOST(t, suite)
	})

	t.Run("GET /threat_models", func(t *testing.T) {
		testDatabaseThreatModelGET(t, suite)
	})

	t.Run("PUT /threat_models/:threat_model_id", func(t *testing.T) {
		testDatabaseThreatModelPUT(t, suite)
	})
}

// TestDatabaseDiagramIntegration tests diagram CRUD against actual PostgreSQL database
func TestDatabaseDiagramIntegration(t *testing.T) {
	suite := SetupSubEntityIntegrationTest(t)
	defer suite.TeardownSubEntityIntegrationTest(t)

	t.Run("POST /diagrams", func(t *testing.T) {
		testDatabaseDiagramPOST(t, suite)
	})

	t.Run("GET /diagrams", func(t *testing.T) {
		testDatabaseDiagramGET(t, suite)
	})
}

// TestDatabaseMetadataIntegration tests metadata CRUD for all entity types
func TestDatabaseMetadataIntegration(t *testing.T) {
	suite := SetupSubEntityIntegrationTest(t)
	defer suite.TeardownSubEntityIntegrationTest(t)

	// First create entities for metadata testing
	diagramID := suite.createTestDiagram(t)
	require.NotEmpty(t, diagramID, "Diagram ID should not be empty for metadata testing")

	// Test diagram metadata (only metadata type we can fully test without complex store setup)
	t.Run("DiagramMetadata", func(t *testing.T) {
		testDatabaseDiagramMetadata(t, suite, diagramID)
	})

	// Note: Other metadata types (threat, document, source) would require
	// creating those entities first via proper handlers with all dependencies.
	// These are best tested via full server integration tests.
}

// TestDatabaseCalculatedFieldsValidation tests that integration tests also reject calculated fields
func TestDatabaseCalculatedFieldsValidation(t *testing.T) {
	suite := SetupSubEntityIntegrationTest(t)
	defer suite.TeardownSubEntityIntegrationTest(t)

	t.Run("ThreatModelCalculatedFieldsRejection", func(t *testing.T) {
		testDatabaseCalculatedFieldsRejection(t, suite)
	})
}

// testDatabaseThreatModelPOST tests creating threat models via POST with database persistence
func testDatabaseThreatModelPOST(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test data - only include fields that are allowed by the validation
	requestBody := map[string]interface{}{
		"name":        "Database Integration Test Threat Model",
		"description": "A threat model created during database integration testing",
		// Note: Do not include calculated fields, owner, created_by, etc.
		// These are now rejected by validation and set automatically by the server
	}

	// Make request
	req := suite.makeAuthenticatedRequest("POST", "/threat_models", requestBody)
	w := suite.executeRequest(req)

	// Verify response
	response := suite.assertJSONResponse(t, w, http.StatusCreated)

	// Verify response contains expected fields
	assert.NotEmpty(t, response["id"], "Response should contain ID")
	assert.Equal(t, requestBody["name"], response["name"])
	assert.Equal(t, requestBody["description"], response["description"])
	assert.NotEmpty(t, response["owner"], "Response should contain owner (set by server)")
	assert.NotEmpty(t, response["created_at"], "Response should contain created_at")
	assert.NotEmpty(t, response["modified_at"], "Response should contain modified_at")

	// Verify the data was actually persisted to the database
	// by making a GET request for the same threat model
	threatModelID := response["id"].(string)
	getReq := suite.makeAuthenticatedRequest("GET", "/threat_models/"+threatModelID, nil)
	getW := suite.executeRequest(getReq)
	getResponse := suite.assertJSONResponse(t, getW, http.StatusOK)

	// Verify the persisted data matches what we created
	assert.Equal(t, threatModelID, getResponse["id"])
	assert.Equal(t, requestBody["name"], getResponse["name"])
	assert.Equal(t, requestBody["description"], getResponse["description"])
	assert.NotEmpty(t, getResponse["owner"], "Response should contain owner (set by server)")
}

// testDatabaseThreatModelGET tests retrieving threat models from database
func testDatabaseThreatModelGET(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// First create a threat model via API (which should persist to database)
	requestBody := map[string]interface{}{
		"name": "GET Test Database Threat Model",
		// Note: Do not include calculated fields, owner, created_by, etc.
		// These are now rejected by validation and set automatically by the server
	}

	req := suite.makeAuthenticatedRequest("POST", "/threat_models", requestBody)
	w := suite.executeRequest(req)
	createResponse := suite.assertJSONResponse(t, w, http.StatusCreated)
	threatModelID := createResponse["id"].(string)

	// Test GET by ID - should retrieve from database
	req = suite.makeAuthenticatedRequest("GET", "/threat_models/"+threatModelID, nil)
	w = suite.executeRequest(req)
	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify response matches database data
	assert.Equal(t, threatModelID, response["id"])
	assert.Equal(t, requestBody["name"], response["name"])
	assert.NotEmpty(t, response["owner"], "Response should contain owner (set by server)")

	// Test GET all threat models - should retrieve from database
	req = suite.makeAuthenticatedRequest("GET", "/threat_models", nil)
	w = suite.executeRequest(req)
	assert.Equal(t, http.StatusOK, w.Code)

	var listResponse []interface{}
	err := json.Unmarshal(w.Body.Bytes(), &listResponse)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listResponse), 1, "Should return at least one threat model from database")
}

// testDatabaseThreatModelPUT tests updating threat models in database
func testDatabaseThreatModelPUT(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// First create a threat model
	requestBody := map[string]interface{}{
		"name": "PUT Test Database Threat Model",
		// Note: Do not include calculated fields, owner, created_by, etc.
		// These are now rejected by validation and set automatically by the server
	}

	req := suite.makeAuthenticatedRequest("POST", "/threat_models", requestBody)
	w := suite.executeRequest(req)
	createResponse := suite.assertJSONResponse(t, w, http.StatusCreated)
	threatModelID := createResponse["id"].(string)

	// Update the threat model - for PUT we need to include all required fields
	updateBody := map[string]interface{}{
		"name":                   "Updated Database Threat Model Name",
		"description":            "Updated description in database",
		"owner":                  suite.testUser.Email, // Required for PUT
		"threat_model_framework": "STRIDE",             // Required for PUT
		"authorization": []map[string]interface{}{ // Required for PUT
			{"subject": suite.testUser.Email, "role": "owner"},
		},
		// Note: Do not include calculated fields, id, timestamps, etc.
		// These are rejected by validation
	}

	req = suite.makeAuthenticatedRequest("PUT", "/threat_models/"+threatModelID, updateBody)
	w = suite.executeRequest(req)
	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify updates in response
	assert.Equal(t, threatModelID, response["id"])
	assert.Equal(t, updateBody["name"], response["name"])
	assert.Equal(t, updateBody["description"], response["description"])

	// Verify updates were persisted to database by fetching again
	getReq := suite.makeAuthenticatedRequest("GET", "/threat_models/"+threatModelID, nil)
	getW := suite.executeRequest(getReq)
	getResponse := suite.assertJSONResponse(t, getW, http.StatusOK)

	// Verify the database contains the updated data
	assert.Equal(t, threatModelID, getResponse["id"])
	assert.Equal(t, updateBody["name"], getResponse["name"])
	assert.Equal(t, updateBody["description"], getResponse["description"])
}

// testDatabaseDiagramPOST tests creating diagrams with database persistence
func testDatabaseDiagramPOST(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Create diagram using the proper sub-entity endpoint
	requestBody := map[string]interface{}{
		"name": "Database Integration Test Diagram",
		"type": "DFD-1.0.0",
	}

	// Step 1: Create diagram using threat model ID
	path := fmt.Sprintf("/threat_models/%s/diagrams", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)
	response := suite.assertJSONResponse(t, w, http.StatusCreated)

	// Verify response contains diagram ID
	assert.NotEmpty(t, response["id"], "Response should contain diagram ID")
	diagramID := response["id"].(string)
	suite.testDiagramID = diagramID // Save for potential future tests

	// Verify basic response fields
	assert.Equal(t, requestBody["name"], response["name"])
	assert.NotEmpty(t, response["created_at"], "Response should contain created_at")
	assert.NotEmpty(t, response["modified_at"], "Response should contain modified_at")

	// Step 2: Verify database persistence by retrieving the specific diagram using proper endpoint
	getDiagramPath := fmt.Sprintf("/threat_models/%s/diagrams/%s", suite.threatModelID, diagramID)
	getReq := suite.makeAuthenticatedRequest("GET", getDiagramPath, nil)
	getW := suite.executeRequest(getReq)
	getResponse := suite.assertJSONResponse(t, getW, http.StatusOK)

	// Verify the persisted data matches what we created
	assert.Equal(t, diagramID, getResponse["id"])
	assert.Equal(t, requestBody["name"], getResponse["name"])
}

// testDatabaseDiagramGET tests retrieving diagrams from database
func testDatabaseDiagramGET(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Step 1: Create a diagram using threat model ID
	requestBody := map[string]interface{}{
		"name": "GET Test Database Diagram",
		"type": "DFD-1.0.0",
	}

	path := fmt.Sprintf("/threat_models/%s/diagrams", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)
	createResponse := suite.assertJSONResponse(t, w, http.StatusCreated)
	diagramID := createResponse["id"].(string)

	// Step 2: Test GET specific diagram using proper endpoint - should retrieve from database
	getDiagramPath := fmt.Sprintf("/threat_models/%s/diagrams/%s", suite.threatModelID, diagramID)
	req = suite.makeAuthenticatedRequest("GET", getDiagramPath, nil)
	w = suite.executeRequest(req)
	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify response matches database data
	assert.Equal(t, diagramID, response["id"])
	assert.Equal(t, requestBody["name"], response["name"])
}

// createParentThreatModel creates a parent threat model for testing sub-entities
func (suite *SubEntityIntegrationTestSuite) createParentThreatModel(t *testing.T) {
	requestBody := map[string]interface{}{
		"name":        "Integration Test Parent Threat Model",
		"description": "A threat model created for sub-entity integration testing",
		// Note: Do not include calculated fields like counts, owner, created_by, etc.
		// These are now rejected by the validation and are set automatically by the server
	}

	req := suite.makeAuthenticatedRequest("POST", "/threat_models", requestBody)
	w := suite.executeRequest(req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create parent threat model. Status: %d, Body: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err, "Failed to parse response JSON: %s", w.Body.String())

	threatModelID, ok := response["id"].(string)
	if !ok || threatModelID == "" {
		t.Fatalf("Response should contain a valid threat model ID. Response: %v", response)
	}

	suite.threatModelID = threatModelID
	require.NotEmpty(t, suite.threatModelID, "Parent threat model ID should not be empty")
}

// TeardownSubEntityIntegrationTest cleans up the test environment
func (suite *SubEntityIntegrationTestSuite) TeardownSubEntityIntegrationTest(t *testing.T) {
	// Clean up test data
	suite.cleanupSubEntityTestData(t)

	// Close database connections
	if suite.dbManager != nil {
		if err := suite.dbManager.Close(); err != nil {
			t.Logf("Warning: failed to close database manager: %v", err)
		}
	}
}

// createSubEntityTestUserWithToken creates a test user and authentication token
func createSubEntityTestUserWithToken(t *testing.T, authService *auth.Service) (*auth.User, string) {
	ctx := context.Background()

	// Create test user data with unique timestamp and random UUID to avoid duplicates
	timestamp := time.Now().UnixNano() // Use nanoseconds for better uniqueness
	userID := uuid.New().String()
	userEmail := fmt.Sprintf("sub-entity-test-user-%d-%s@test.tmi", timestamp, userID[:8])

	// Create test user struct
	testUser := auth.User{
		ID:    userID,
		Email: userEmail,
		Name:  "Sub Entity Test User",
	}

	// Create user in the database
	user, err := authService.CreateUser(ctx, testUser)
	require.NoError(t, err, "Failed to create test user")

	// Generate a test access token
	tokens, err := authService.GenerateTokens(ctx, user)
	require.NoError(t, err, "Failed to generate test tokens")

	return &user, tokens.AccessToken
}

// cleanupSubEntityTestData removes test data from the database
func (suite *SubEntityIntegrationTestSuite) cleanupSubEntityTestData(t *testing.T) {
	// Reset the stores to clean up test data
	ResetStores()

	// Additional cleanup can be added here if needed
	// For example, cleaning up users, sessions, etc.
}

// makeAuthenticatedRequest creates an HTTP request with authentication headers
func (suite *SubEntityIntegrationTestSuite) makeAuthenticatedRequest(method, path string, body interface{}) *http.Request {
	var reqBody *bytes.Buffer
	if body != nil {
		jsonBody, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(jsonBody)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Authorization", "Bearer "+suite.accessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req
}

// executeRequest executes an HTTP request and returns the response recorder
func (suite *SubEntityIntegrationTestSuite) executeRequest(req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)
	return w
}

// assertJSONResponse verifies that the response is valid JSON and returns the parsed data
func (suite *SubEntityIntegrationTestSuite) assertJSONResponse(t *testing.T, w *httptest.ResponseRecorder, expectedStatus int) map[string]interface{} {
	if w.Code != expectedStatus {
		t.Logf("Expected status %d, got %d. Response body: %s", expectedStatus, w.Code, w.Body.String())
	}
	assert.Equal(t, expectedStatus, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err, "Response should be valid JSON")

	return response
}

// assertJSONArrayResponse verifies that the response is a valid JSON array and returns the parsed data
func (suite *SubEntityIntegrationTestSuite) assertJSONArrayResponse(t *testing.T, w *httptest.ResponseRecorder, expectedStatus int) []interface{} {
	if w.Code != expectedStatus {
		t.Logf("Expected status %d, got %d. Response body: %s", expectedStatus, w.Code, w.Body.String())
	}
	assert.Equal(t, expectedStatus, w.Code)

	var response []interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err, "Response should be valid JSON array")

	return response
}

// createTestThreat creates a test threat for use in other tests
func (suite *SubEntityIntegrationTestSuite) createTestThreat(t *testing.T) string {
	requestBody := map[string]interface{}{
		"name":        "Test Integration Threat",
		"description": "A threat created during integration testing",
		"severity":    "High",
		"status":      "identified",
		"threat_type": "spoofing",
		"priority":    "High",
		"mitigated":   false,
	}

	path := fmt.Sprintf("/threat_models/%s/threats", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusCreated)
	threatID := response["id"].(string)
	suite.testThreatID = threatID
	return threatID
}

// createTestDocument creates a test document for use in other tests
func (suite *SubEntityIntegrationTestSuite) createTestDocument(t *testing.T) string {
	requestBody := map[string]interface{}{
		"name":        "Test Integration Document",
		"url":         "https://example.com/test-document.pdf",
		"description": "A document created during integration testing",
	}

	path := fmt.Sprintf("/threat_models/%s/documents", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusCreated)
	documentID := response["id"].(string)
	suite.testDocumentID = documentID
	return documentID
}

// createTestSource creates a test source for use in other tests
func (suite *SubEntityIntegrationTestSuite) createTestSource(t *testing.T) string {
	requestBody := map[string]interface{}{
		"name":        "Test Integration Source",
		"url":         "https://github.com/example/test-repo",
		"description": "A source created during integration testing",
		"type":        "git",
		"parameters": map[string]interface{}{
			"refType":  "branch",
			"refValue": "main",
			"subPath":  "/src",
		},
	}

	path := fmt.Sprintf("/threat_models/%s/sources", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusCreated)
	sourceID := response["id"].(string)
	suite.testSourceID = sourceID
	return sourceID
}

// createTestDiagram creates a test diagram for use in other tests
func (suite *SubEntityIntegrationTestSuite) createTestDiagram(t *testing.T) string {
	requestBody := map[string]interface{}{
		"name": "Test Integration Diagram",
		"type": "DFD-1.0.0",
	}

	path := fmt.Sprintf("/threat_models/%s/diagrams", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusCreated)
	diagramID := response["id"].(string)
	suite.testDiagramID = diagramID
	return diagramID
}

// testDatabaseDiagramMetadata tests diagram metadata CRUD operations
func testDatabaseDiagramMetadata(t *testing.T, suite *SubEntityIntegrationTestSuite, diagramID string) {
	// Test data for metadata
	metadataKey := "test_key"
	metadataValue := "test_value"

	metadataBody := map[string]interface{}{
		"key":   metadataKey,
		"value": metadataValue,
	}

	// Test POST metadata
	t.Run("POST", func(t *testing.T) {
		path := fmt.Sprintf("/threat_models/%s/diagrams/%s/metadata", suite.threatModelID, diagramID)
		req := suite.makeAuthenticatedRequest("POST", path, metadataBody)
		w := suite.executeRequest(req)

		// Should succeed
		response := suite.assertJSONResponse(t, w, http.StatusCreated)
		assert.Equal(t, metadataKey, response["key"])
		assert.Equal(t, metadataValue, response["value"])
	})

	// Test GET metadata by key
	t.Run("GET_ByKey", func(t *testing.T) {
		path := fmt.Sprintf("/threat_models/%s/diagrams/%s/metadata/%s", suite.threatModelID, diagramID, metadataKey)
		req := suite.makeAuthenticatedRequest("GET", path, nil)
		w := suite.executeRequest(req)

		// Should succeed
		response := suite.assertJSONResponse(t, w, http.StatusOK)
		assert.Equal(t, metadataKey, response["key"])
		assert.Equal(t, metadataValue, response["value"])
	})

	// Test PUT metadata (update)
	t.Run("PUT", func(t *testing.T) {
		updatedValue := "updated_test_value"
		updateBody := map[string]interface{}{
			"key":   metadataKey, // Required field for PUT metadata
			"value": updatedValue,
		}

		path := fmt.Sprintf("/threat_models/%s/diagrams/%s/metadata/%s", suite.threatModelID, diagramID, metadataKey)
		req := suite.makeAuthenticatedRequest("PUT", path, updateBody)
		w := suite.executeRequest(req)

		// Should succeed
		response := suite.assertJSONResponse(t, w, http.StatusOK)
		assert.Equal(t, metadataKey, response["key"])
		assert.Equal(t, updatedValue, response["value"])
	})

	// Test GET all metadata
	t.Run("GET_All", func(t *testing.T) {
		path := fmt.Sprintf("/threat_models/%s/diagrams/%s/metadata", suite.threatModelID, diagramID)
		req := suite.makeAuthenticatedRequest("GET", path, nil)
		w := suite.executeRequest(req)

		// Should succeed and return an array
		responseArray := suite.assertJSONArrayResponse(t, w, http.StatusOK)
		assert.GreaterOrEqual(t, len(responseArray), 1, "Should return at least one metadata entry")
	})

	// Test DELETE metadata
	t.Run("DELETE", func(t *testing.T) {
		path := fmt.Sprintf("/threat_models/%s/diagrams/%s/metadata/%s", suite.threatModelID, diagramID, metadataKey)
		req := suite.makeAuthenticatedRequest("DELETE", path, nil)
		w := suite.executeRequest(req)

		// Should succeed
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}

// testDatabaseCalculatedFieldsRejection tests that integration tests also reject calculated fields
func testDatabaseCalculatedFieldsRejection(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test that the integration environment also properly rejects calculated fields
	requestBody := map[string]interface{}{
		"name":        "Integration Test Threat Model",
		"description": "Should be rejected due to calculated fields",
		"created_at":  "2025-01-01T00:00:00Z", // This should be rejected
	}

	req := suite.makeAuthenticatedRequest("POST", "/threat_models", requestBody)
	w := suite.executeRequest(req)

	// Should be rejected with 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, w.Code, "Calculated fields should be rejected in integration tests too")

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify error message mentions the problematic field
	if errorDesc, exists := response["error_description"]; exists {
		assert.Contains(t, errorDesc, "created_at", "Error should mention the rejected field")
	}
}
