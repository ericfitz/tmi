package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// IntegrationTestSuite manages database setup and teardown for integration tests
type IntegrationTestSuite struct {
	dbManager   *db.Manager
	authService *auth.Service
	server      *Server
	router      *gin.Engine
	testUser    *auth.User
	accessToken string
}

// SetupIntegrationTest initializes the test environment with a real database
func SetupIntegrationTest(t *testing.T) *IntegrationTestSuite {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
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
		DB:       1, // Use DB 1 for testing
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
			Secret:            "test-secret-key-for-integration-testing",
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
	testUser, accessToken := createTestUserWithToken(t, authService)

	// Initialize API server
	server := NewServer()

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

	// Register API handlers
	threatModelHandler := NewThreatModelHandler()
	diagramHandler := NewThreatModelDiagramHandler(NewWebSocketHub())

	// Threat Model routes
	router.GET("/threat_models", threatModelHandler.GetThreatModels)
	router.POST("/threat_models", threatModelHandler.CreateThreatModel)
	router.GET("/threat_models/:id", threatModelHandler.GetThreatModelByID)
	router.PUT("/threat_models/:id", threatModelHandler.UpdateThreatModel)
	router.PATCH("/threat_models/:id", threatModelHandler.PatchThreatModel)
	router.DELETE("/threat_models/:id", threatModelHandler.DeleteThreatModel)

	// Threat model diagram sub-entity routes only
	router.POST("/threat_models/:id/diagrams", func(c *gin.Context) {
		diagramHandler.CreateDiagram(c, c.Param("id"))
	})
	router.GET("/threat_models/:id/diagrams/:diagram_id", func(c *gin.Context) {
		threatModelID := c.Param("id")
		diagramID := c.Param("diagram_id")
		diagramHandler.GetDiagramByID(c, threatModelID, diagramID)
	})
	router.PUT("/threat_models/:id/diagrams/:diagram_id", func(c *gin.Context) {
		threatModelID := c.Param("id")
		diagramID := c.Param("diagram_id")
		diagramHandler.UpdateDiagram(c, threatModelID, diagramID)
	})
	router.PATCH("/threat_models/:id/diagrams/:diagram_id", func(c *gin.Context) {
		threatModelID := c.Param("id")
		diagramID := c.Param("diagram_id")
		diagramHandler.PatchDiagram(c, threatModelID, diagramID)
	})
	router.DELETE("/threat_models/:id/diagrams/:diagram_id", func(c *gin.Context) {
		threatModelID := c.Param("id")
		diagramID := c.Param("diagram_id")
		diagramHandler.DeleteDiagram(c, threatModelID, diagramID)
	})

	// Register server handlers
	server.RegisterHandlers(router)

	return &IntegrationTestSuite{
		dbManager:   dbManager,
		authService: authService,
		server:      server,
		router:      router,
		testUser:    testUser,
		accessToken: accessToken,
	}
}

// TeardownIntegrationTest cleans up the test environment
func (suite *IntegrationTestSuite) TeardownIntegrationTest(t *testing.T) {
	// Clean up test data
	suite.cleanupTestData(t)

	// Close database connections
	if suite.dbManager != nil {
		if err := suite.dbManager.Close(); err != nil {
			t.Logf("Warning: failed to close database manager: %v", err)
		}
	}
}

// createTestUserWithToken creates a test user and authentication token using the test provider
func createTestUserWithToken(t *testing.T, authService *auth.Service) (*auth.User, string) {
	ctx := context.Background()

	// Create test user data with unique timestamp and proper UUID
	timestamp := time.Now().UnixNano() // Use nanoseconds for better uniqueness
	userID := uuid.New().String()
	userEmail := fmt.Sprintf("test-user-%d-%s@test.tmi", timestamp, userID[:8])

	// Create test user struct
	testUser := auth.User{
		ID:    userID,
		Email: userEmail,
		Name:  "Test User",
	}

	// Create user in the database
	user, err := authService.CreateUser(ctx, testUser)
	require.NoError(t, err, "Failed to create test user")

	// Generate a test access token
	tokens, err := authService.GenerateTokens(ctx, user)
	require.NoError(t, err, "Failed to generate test tokens")

	return &user, tokens.AccessToken
}

// cleanupTestData removes test data from the database
func (suite *IntegrationTestSuite) cleanupTestData(t *testing.T) {
	// Reset the stores to clean up test data
	ResetStores()

	// Additional cleanup can be added here if needed
	// For example, cleaning up users, sessions, etc.
}

// makeAuthenticatedRequest creates an HTTP request with authentication headers
func (suite *IntegrationTestSuite) makeAuthenticatedRequest(method, path string, body interface{}) *http.Request {
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
func (suite *IntegrationTestSuite) executeRequest(req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)
	return w
}

// assertJSONResponse verifies that the response is valid JSON and returns the parsed data
func (suite *IntegrationTestSuite) assertJSONResponse(t *testing.T, w *httptest.ResponseRecorder, expectedStatus int) map[string]interface{} {
	assert.Equal(t, expectedStatus, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err, "Response should be valid JSON")

	return response
}

// Integration Tests

// TestThreatModelIntegration tests the complete CRUD lifecycle for threat models
func TestThreatModelIntegration(t *testing.T) {
	suite := SetupIntegrationTest(t)
	defer suite.TeardownIntegrationTest(t)

	t.Run("POST /threat_models", func(t *testing.T) {
		testThreatModelPOST(t, suite)
	})

	t.Run("GET /threat_models", func(t *testing.T) {
		testThreatModelGET(t, suite)
	})

	t.Run("PUT /threat_models/:id", func(t *testing.T) {
		testThreatModelPUT(t, suite)
	})
}

// TestDiagramIntegration tests the complete CRUD lifecycle for diagrams
func TestDiagramIntegration(t *testing.T) {
	suite := SetupIntegrationTest(t)
	defer suite.TeardownIntegrationTest(t)

	t.Run("POST /threat_models/:id/diagrams", func(t *testing.T) {
		testDiagramPOST(t, suite)
	})

	t.Run("GET /threat_models/:id/diagrams/:diagram_id", func(t *testing.T) {
		testDiagramGET(t, suite)
	})

	t.Run("PUT /threat_models/:id/diagrams/:diagram_id", func(t *testing.T) {
		testDiagramPUT(t, suite)
	})
}

// testThreatModelPOST tests creating threat models via POST
func testThreatModelPOST(t *testing.T, suite *IntegrationTestSuite) {
	// Test data
	requestBody := map[string]interface{}{
		"name":        "Integration Test Threat Model",
		"description": "A threat model created during integration testing",
		"owner":       suite.testUser.Email,
		"authorization": []map[string]interface{}{
			{"subject": suite.testUser.Email, "role": "owner"},
		},
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

	// Verify authorization array
	auth, ok := response["authorization"].([]interface{})
	require.True(t, ok, "Authorization should be an array")
	assert.Len(t, auth, 1)

	authEntry := auth[0].(map[string]interface{})
	assert.Equal(t, suite.testUser.Email, authEntry["subject"])
	assert.Equal(t, "owner", authEntry["role"])
}

// testThreatModelGET tests retrieving threat models via GET
func testThreatModelGET(t *testing.T, suite *IntegrationTestSuite) {
	// First create a threat model
	requestBody := map[string]interface{}{
		"name":  "GET Test Threat Model",
		"owner": suite.testUser.Email,
		"authorization": []map[string]interface{}{
			{"subject": suite.testUser.Email, "role": "owner"},
		},
	}

	req := suite.makeAuthenticatedRequest("POST", "/threat_models", requestBody)
	w := suite.executeRequest(req)
	createResponse := suite.assertJSONResponse(t, w, http.StatusCreated)

	threatModelID := createResponse["id"].(string)

	// Test GET by ID
	req = suite.makeAuthenticatedRequest("GET", "/threat_models/"+threatModelID, nil)
	w = suite.executeRequest(req)
	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify response
	assert.Equal(t, threatModelID, response["id"])
	assert.Equal(t, requestBody["name"], response["name"])
	assert.Equal(t, requestBody["owner"], response["owner"])

	// Test GET all threat models
	req = suite.makeAuthenticatedRequest("GET", "/threat_models", nil)
	w = suite.executeRequest(req)
	assert.Equal(t, http.StatusOK, w.Code)

	var listResponse []interface{}
	err := json.Unmarshal(w.Body.Bytes(), &listResponse)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listResponse), 1, "Should return at least one threat model")
}

// testThreatModelPUT tests updating threat models via PUT
func testThreatModelPUT(t *testing.T, suite *IntegrationTestSuite) {
	// First create a threat model
	requestBody := map[string]interface{}{
		"name":  "PUT Test Threat Model",
		"owner": suite.testUser.Email,
		"authorization": []map[string]interface{}{
			{"subject": suite.testUser.Email, "role": "owner"},
		},
	}

	req := suite.makeAuthenticatedRequest("POST", "/threat_models", requestBody)
	w := suite.executeRequest(req)
	createResponse := suite.assertJSONResponse(t, w, http.StatusCreated)

	threatModelID := createResponse["id"].(string)

	// Update the threat model
	updateBody := map[string]interface{}{
		"id":          threatModelID,
		"name":        "Updated Threat Model Name",
		"description": "Updated description",
		"owner":       suite.testUser.Email,
		"authorization": []map[string]interface{}{
			{"subject": suite.testUser.Email, "role": "owner"},
		},
	}

	req = suite.makeAuthenticatedRequest("PUT", "/threat_models/"+threatModelID, updateBody)
	w = suite.executeRequest(req)
	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify updates
	assert.Equal(t, threatModelID, response["id"])
	assert.Equal(t, updateBody["name"], response["name"])
	assert.Equal(t, updateBody["description"], response["description"])
}

// testDiagramPOST tests creating diagrams via POST
func testDiagramPOST(t *testing.T, suite *IntegrationTestSuite) {
	// First create a threat model
	tmRequestBody := map[string]interface{}{
		"name":  "Parent Threat Model",
		"owner": suite.testUser.Email,
		"authorization": []map[string]interface{}{
			{"subject": suite.testUser.Email, "role": "owner"},
		},
	}

	req := suite.makeAuthenticatedRequest("POST", "/threat_models", tmRequestBody)
	w := suite.executeRequest(req)
	tmResponse := suite.assertJSONResponse(t, w, http.StatusCreated)
	threatModelID := tmResponse["id"].(string)

	// Create diagram through sub-entity endpoint
	requestBody := map[string]interface{}{
		"name":        "Integration Test Diagram",
		"description": "A diagram created during integration testing",
	}

	diagramURL := fmt.Sprintf("/threat_models/%s/diagrams", threatModelID)
	req = suite.makeAuthenticatedRequest("POST", diagramURL, requestBody)
	w = suite.executeRequest(req)
	response := suite.assertJSONResponse(t, w, http.StatusCreated)

	// Verify response
	assert.NotEmpty(t, response["id"], "Response should contain ID")
	assert.Equal(t, requestBody["name"], response["name"])
	assert.Equal(t, requestBody["description"], response["description"])
	assert.NotEmpty(t, response["created_at"], "Response should contain created_at")
	assert.NotEmpty(t, response["modified_at"], "Response should contain modified_at")
}

// testDiagramGET tests retrieving diagrams via GET
func testDiagramGET(t *testing.T, suite *IntegrationTestSuite) {
	// First create a threat model and diagram
	tmRequestBody := map[string]interface{}{
		"name":  "Parent Threat Model for GET",
		"owner": suite.testUser.Email,
		"authorization": []map[string]interface{}{
			{"subject": suite.testUser.Email, "role": "owner"},
		},
	}

	req := suite.makeAuthenticatedRequest("POST", "/threat_models", tmRequestBody)
	w := suite.executeRequest(req)
	tmResponse := suite.assertJSONResponse(t, w, http.StatusCreated)
	threatModelID := tmResponse["id"].(string)

	requestBody := map[string]interface{}{
		"name": "GET Test Diagram",
	}

	diagramURL := fmt.Sprintf("/threat_models/%s/diagrams", threatModelID)
	req = suite.makeAuthenticatedRequest("POST", diagramURL, requestBody)
	w = suite.executeRequest(req)
	createResponse := suite.assertJSONResponse(t, w, http.StatusCreated)
	diagramID := createResponse["id"].(string)

	// Test GET by ID using sub-entity endpoint
	getDiagramURL := fmt.Sprintf("/threat_models/%s/diagrams/%s", threatModelID, diagramID)
	req = suite.makeAuthenticatedRequest("GET", getDiagramURL, nil)
	w = suite.executeRequest(req)
	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify response
	assert.Equal(t, diagramID, response["id"])
	assert.Equal(t, requestBody["name"], response["name"])

	// Note: List all diagrams would need to be implemented as GET /threat_models/:id/diagrams
	// For now, we skip the list test as it requires knowing the threat model ID
}

// testDiagramPUT tests updating diagrams via PUT
func testDiagramPUT(t *testing.T, suite *IntegrationTestSuite) {
	// First create a threat model and diagram
	tmRequestBody := map[string]interface{}{
		"name":  "Parent Threat Model for PUT",
		"owner": suite.testUser.Email,
		"authorization": []map[string]interface{}{
			{"subject": suite.testUser.Email, "role": "owner"},
		},
	}

	req := suite.makeAuthenticatedRequest("POST", "/threat_models", tmRequestBody)
	w := suite.executeRequest(req)
	tmResponse := suite.assertJSONResponse(t, w, http.StatusCreated)
	threatModelID := tmResponse["id"].(string)

	requestBody := map[string]interface{}{
		"name": "PUT Test Diagram",
	}

	diagramURL := fmt.Sprintf("/threat_models/%s/diagrams", threatModelID)
	req = suite.makeAuthenticatedRequest("POST", diagramURL, requestBody)
	w = suite.executeRequest(req)
	createResponse := suite.assertJSONResponse(t, w, http.StatusCreated)
	diagramID := createResponse["id"].(string)

	// Update the diagram using sub-entity endpoint
	updateBody := map[string]interface{}{
		"id":          diagramID,
		"name":        "Updated Diagram Name",
		"description": "Updated description",
	}

	updateDiagramURL := fmt.Sprintf("/threat_models/%s/diagrams/%s", threatModelID, diagramID)
	req = suite.makeAuthenticatedRequest("PUT", updateDiagramURL, updateBody)
	w = suite.executeRequest(req)
	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify updates
	assert.Equal(t, diagramID, response["id"])
	assert.Equal(t, updateBody["name"], response["name"])
	assert.Equal(t, updateBody["description"], response["description"])
}
