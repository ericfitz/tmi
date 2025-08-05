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
	postgresConfig := db.PostgresConfig{
		Host:     getEnvOrDefault("TEST_DB_HOST", "localhost"),
		Port:     getEnvOrDefault("TEST_DB_PORT", "5432"),
		User:     getEnvOrDefault("TEST_DB_USER", "tmi_test"),
		Password: getEnvOrDefault("TEST_DB_PASSWORD", "test123"),
		Database: getEnvOrDefault("TEST_DB_NAME", "tmi_test"),
		SSLMode:  "disable",
	}

	redisConfig := db.RedisConfig{
		Host:     getEnvOrDefault("TEST_REDIS_HOST", "localhost"),
		Port:     getEnvOrDefault("TEST_REDIS_PORT", "6379"),
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
			CallbackURL: "http://localhost:8080/auth/callback",
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
			c.Set("userName", testUser.Email)
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
	server := NewServer()
	server.RegisterHandlers(router)

	// Register API handlers directly
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

	// Sub-entity routes would be registered here
	// For now, we'll test the basic threat model and diagram functionality

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

	t.Run("PUT /threat_models/:id", func(t *testing.T) {
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

// testDatabaseThreatModelPOST tests creating threat models via POST with database persistence
func testDatabaseThreatModelPOST(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test data
	requestBody := map[string]interface{}{
		"name":                   "Database Integration Test Threat Model",
		"description":            "A threat model created during database integration testing",
		"owner":                  suite.testUser.Email,
		"created_by":             suite.testUser.Email,
		"threat_model_framework": "STRIDE",
		"document_count":         0,
		"source_count":           0,
		"diagram_count":          0,
		"threat_count":           0,
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
	assert.Equal(t, requestBody["owner"], response["owner"])
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
	assert.Equal(t, requestBody["owner"], getResponse["owner"])
}

// testDatabaseThreatModelGET tests retrieving threat models from database
func testDatabaseThreatModelGET(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// First create a threat model via API (which should persist to database)
	requestBody := map[string]interface{}{
		"name":                   "GET Test Database Threat Model",
		"owner":                  suite.testUser.Email,
		"created_by":             suite.testUser.Email,
		"threat_model_framework": "STRIDE",
		"document_count":         0,
		"source_count":           0,
		"diagram_count":          0,
		"threat_count":           0,
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
	assert.Equal(t, requestBody["owner"], response["owner"])

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
		"name":                   "PUT Test Database Threat Model",
		"owner":                  suite.testUser.Email,
		"created_by":             suite.testUser.Email,
		"threat_model_framework": "STRIDE",
		"document_count":         0,
		"source_count":           0,
		"diagram_count":          0,
		"threat_count":           0,
	}

	req := suite.makeAuthenticatedRequest("POST", "/threat_models", requestBody)
	w := suite.executeRequest(req)
	createResponse := suite.assertJSONResponse(t, w, http.StatusCreated)
	threatModelID := createResponse["id"].(string)

	// Update the threat model
	updateBody := map[string]interface{}{
		"id":                     threatModelID,
		"name":                   "Updated Database Threat Model Name",
		"description":            "Updated description in database",
		"owner":                  suite.testUser.Email,
		"created_by":             suite.testUser.Email,
		"threat_model_framework": "STRIDE",
		"document_count":         0,
		"source_count":           0,
		"diagram_count":          0,
		"threat_count":           0,
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
	// Create diagram
	requestBody := map[string]interface{}{
		"name":            "Database Integration Test Diagram",
		"description":     "A diagram created during database integration testing",
		"threat_model_id": suite.threatModelID,
	}

	req := suite.makeAuthenticatedRequest("POST", "/diagrams", requestBody)
	w := suite.executeRequest(req)
	response := suite.assertJSONResponse(t, w, http.StatusCreated)

	// Verify response
	assert.NotEmpty(t, response["id"], "Response should contain ID")
	assert.Equal(t, requestBody["name"], response["name"])
	assert.Equal(t, requestBody["description"], response["description"])
	assert.Equal(t, requestBody["threat_model_id"], response["threat_model_id"])
	assert.NotEmpty(t, response["created_at"], "Response should contain created_at")
	assert.NotEmpty(t, response["modified_at"], "Response should contain modified_at")

	// Verify database persistence by retrieving the diagram
	diagramID := response["id"].(string)
	getReq := suite.makeAuthenticatedRequest("GET", "/diagrams/"+diagramID, nil)
	getW := suite.executeRequest(getReq)
	getResponse := suite.assertJSONResponse(t, getW, http.StatusOK)

	// Verify the persisted data matches what we created
	assert.Equal(t, diagramID, getResponse["id"])
	assert.Equal(t, requestBody["name"], getResponse["name"])
	assert.Equal(t, requestBody["description"], getResponse["description"])
	assert.Equal(t, requestBody["threat_model_id"], getResponse["threat_model_id"])
}

// testDatabaseDiagramGET tests retrieving diagrams from database
func testDatabaseDiagramGET(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// First create a diagram
	requestBody := map[string]interface{}{
		"name":            "GET Test Database Diagram",
		"threat_model_id": suite.threatModelID,
	}

	req := suite.makeAuthenticatedRequest("POST", "/diagrams", requestBody)
	w := suite.executeRequest(req)
	createResponse := suite.assertJSONResponse(t, w, http.StatusCreated)
	diagramID := createResponse["id"].(string)

	// Test GET by ID - should retrieve from database
	req = suite.makeAuthenticatedRequest("GET", "/diagrams/"+diagramID, nil)
	w = suite.executeRequest(req)
	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify response matches database data
	assert.Equal(t, diagramID, response["id"])
	assert.Equal(t, requestBody["name"], response["name"])
	assert.Equal(t, requestBody["threat_model_id"], response["threat_model_id"])

	// Test GET all diagrams - should retrieve from database
	req = suite.makeAuthenticatedRequest("GET", "/diagrams", nil)
	w = suite.executeRequest(req)
	assert.Equal(t, http.StatusOK, w.Code)

	var listResponse []interface{}
	err := json.Unmarshal(w.Body.Bytes(), &listResponse)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listResponse), 1, "Should return at least one diagram from database")
}

// createParentThreatModel creates a parent threat model for testing sub-entities
func (suite *SubEntityIntegrationTestSuite) createParentThreatModel(t *testing.T) {
	requestBody := map[string]interface{}{
		"name":                   "Integration Test Parent Threat Model",
		"description":            "A threat model created for sub-entity integration testing",
		"owner":                  suite.testUser.Email,
		"created_by":             suite.testUser.Email,
		"threat_model_framework": "STRIDE",
		"document_count":         0,
		"source_count":           0,
		"diagram_count":          0,
		"threat_count":           0,
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

	// Create test user data
	userEmail := fmt.Sprintf("sub-entity-test-user-%d@test.tmi", time.Now().Unix())
	userID := uuid.New().String()

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
		"severity":    "high",
		"status":      "identified",
		"threat_type": "spoofing",
		"priority":    "high",
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
		"name":        "Test Integration Diagram",
		"description": "A diagram created during integration testing",
	}

	path := fmt.Sprintf("/threat_models/%s/diagrams", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusCreated)
	diagramID := response["id"].(string)
	suite.testDiagramID = diagramID
	return diagramID
}
