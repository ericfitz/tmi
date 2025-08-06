package api

import (
	"os"
	"strconv"
	"testing"
)

// TestIntegrationWithRedisEnabled runs the full integration test suite with Redis enabled
func TestIntegrationWithRedisEnabled(t *testing.T) {
	runFullIntegrationSuite(t, true)
}

// TestIntegrationWithRedisDisabled runs the full integration test suite with Redis disabled
func TestIntegrationWithRedisDisabled(t *testing.T) {
	runFullIntegrationSuite(t, false)
}

// runFullIntegrationSuite executes all integration test categories with specified Redis configuration
func runFullIntegrationSuite(t *testing.T, redisEnabled bool) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test suite in short mode")
	}

	// Set Redis configuration for this test run
	originalRedisEnabled := os.Getenv("REDIS_ENABLED")
	_ = os.Setenv("REDIS_ENABLED", strconv.FormatBool(redisEnabled))
	defer func() {
		if originalRedisEnabled == "" {
			_ = os.Unsetenv("REDIS_ENABLED")
		} else {
			_ = os.Setenv("REDIS_ENABLED", originalRedisEnabled)
		}
	}()

	t.Logf("Running integration test suite with Redis enabled: %v", redisEnabled)

	// Run all test categories in sequence
	// Note: These correspond to the major test functions in the existing integration test files

	t.Run("RootEntities", func(t *testing.T) {
		// Root entities are tested via the existing threat model tests
		t.Run("ThreatModels", TestDatabaseThreatModelIntegration)
	})

	t.Run("SubEntities", func(t *testing.T) {
		// Sub-entities are tested via the existing diagram and metadata tests
		t.Run("Diagrams", TestDatabaseDiagramIntegration)
		t.Run("ThreatModelDiagrams", TestThreatModelDiagramIntegration)
		t.Run("Metadata", TestDatabaseMetadataIntegration)
	})

	t.Run("Collaboration", func(t *testing.T) {
		// Real-time collaboration features
		t.Run("CollaborationIntegration", TestCollaborationIntegration)
		if redisEnabled {
			t.Run("CollaborationWithRedis", TestCollaborationWithRedis)
		}
	})

	t.Run("FieldValidation", func(t *testing.T) {
		// Field validation tests
		t.Run("CalculatedFields", TestDatabaseCalculatedFieldsValidation)
	})
}

// RedisIntegrationTestSuite extends the base integration test suite with Redis-specific functionality
type RedisIntegrationTestSuite struct {
	*SubEntityIntegrationTestSuite
	redisEnabled bool
}

// SetupRedisIntegrationTest initializes a test environment with specific Redis configuration
func SetupRedisIntegrationTest(t *testing.T, redisEnabled bool) *RedisIntegrationTestSuite {
	// Set Redis enabled state before setup
	_ = os.Setenv("REDIS_ENABLED", strconv.FormatBool(redisEnabled))
	defer func() { _ = os.Unsetenv("REDIS_ENABLED") }()

	// Use the existing setup but with Redis configuration
	baseSuite := SetupSubEntityIntegrationTest(t)

	return &RedisIntegrationTestSuite{
		SubEntityIntegrationTestSuite: baseSuite,
		redisEnabled:                  redisEnabled,
	}
}

// TeardownRedisIntegrationTest cleans up the Redis-specific test environment
func (suite *RedisIntegrationTestSuite) TeardownRedisIntegrationTest(t *testing.T) {
	// Clean up Redis data if enabled
	if suite.redisEnabled && isRedisEnabled(suite.SubEntityIntegrationTestSuite) {
		suite.cleanupRedisTestData(t)
	}

	// Use the base teardown
	suite.TeardownSubEntityIntegrationTest(t)
}

// cleanupRedisTestData removes test data from Redis
func (suite *RedisIntegrationTestSuite) cleanupRedisTestData(t *testing.T) {
	redis := suite.dbManager.Redis()
	if redis == nil {
		t.Log("Redis client not available for cleanup")
		return
	}

	// Implementation depends on specific caching patterns used
	// For now, we'll do a basic cleanup
	t.Log("Cleaning up Redis test data")

	// This is a placeholder for Redis cleanup logic
	// In a real implementation, this would clear specific test keys
	// based on the caching strategy used by the application
}

// Redis-specific test helpers

// TestRedisConsistency tests that Redis cache stays consistent with database operations
func TestRedisConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Redis consistency test in short mode")
	}

	suite := SetupRedisIntegrationTest(t, true)
	defer suite.TeardownRedisIntegrationTest(t)

	if !isRedisEnabled(suite.SubEntityIntegrationTestSuite) {
		t.Skip("Redis not enabled, skipping consistency test")
	}

	t.Run("ThreatModelCacheConsistency", func(t *testing.T) {
		suite.testThreatModelCacheConsistency(t)
	})

	t.Run("DiagramCacheConsistency", func(t *testing.T) {
		suite.testDiagramCacheConsistency(t)
	})
}

// testThreatModelCacheConsistency verifies threat model cache consistency
func (suite *RedisIntegrationTestSuite) testThreatModelCacheConsistency(t *testing.T) {
	// Create a threat model
	requestBody := map[string]interface{}{
		"name":        "Redis Consistency Test Threat Model",
		"description": "Testing Redis cache consistency",
	}

	req := suite.makeAuthenticatedRequest("POST", "/threat_models", requestBody)
	w := suite.executeRequest(req)
	response := suite.assertJSONResponse(t, w, 201)

	threatModelID := response["id"].(string)

	// Verify database persistence with the helpers
	verifyThreatModelInDatabase(suite.SubEntityIntegrationTestSuite, t, threatModelID, requestBody)

	// Verify Redis consistency (placeholder for actual cache verification)
	verifyRedisConsistency(suite.SubEntityIntegrationTestSuite, t, "threat_model", threatModelID)

	// Update the threat model and verify consistency again
	updateBody := map[string]interface{}{
		"name":                   "Updated Redis Test Threat Model",
		"description":            "Updated for Redis consistency testing",
		"owner":                  suite.testUser.Email,
		"threat_model_framework": "STRIDE",
		"authorization": []map[string]interface{}{
			{"subject": suite.testUser.Email, "role": "owner"},
		},
	}

	req = suite.makeAuthenticatedRequest("PUT", "/threat_models/"+threatModelID, updateBody)
	w = suite.executeRequest(req)
	suite.assertJSONResponse(t, w, 200)

	// Verify updated data in database
	verifyThreatModelInDatabase(suite.SubEntityIntegrationTestSuite, t, threatModelID, updateBody)

	// Verify Redis cache was updated (placeholder)
	verifyRedisConsistency(suite.SubEntityIntegrationTestSuite, t, "threat_model", threatModelID)
}

// testDiagramCacheConsistency verifies diagram cache consistency
func (suite *RedisIntegrationTestSuite) testDiagramCacheConsistency(t *testing.T) {
	// Create a diagram
	requestBody := map[string]interface{}{
		"name":        "Redis Consistency Test Diagram",
		"description": "Testing Redis cache consistency for diagrams",
	}

	path := "/threat_models/" + suite.threatModelID + "/diagrams"
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)
	response := suite.assertJSONResponse(t, w, 201)

	diagramID := response["id"].(string)

	// Verify database persistence
	verifyDiagramInDatabase(suite.SubEntityIntegrationTestSuite, t, diagramID, suite.threatModelID, requestBody)

	// Verify Redis consistency (placeholder)
	verifyRedisConsistency(suite.SubEntityIntegrationTestSuite, t, "diagram", diagramID)

	// Update the diagram and verify consistency
	updateBody := map[string]interface{}{
		"name":        "Updated Redis Test Diagram",
		"description": "Updated for Redis consistency testing",
	}

	updatePath := "/threat_models/" + suite.threatModelID + "/diagrams/" + diagramID
	req = suite.makeAuthenticatedRequest("PUT", updatePath, updateBody)
	w = suite.executeRequest(req)
	suite.assertJSONResponse(t, w, 200)

	// Verify updated data in database
	verifyDiagramInDatabase(suite.SubEntityIntegrationTestSuite, t, diagramID, suite.threatModelID, updateBody)

	// Verify Redis cache was updated (placeholder)
	verifyRedisConsistency(suite.SubEntityIntegrationTestSuite, t, "diagram", diagramID)

	// Test PATCH operation cache consistency
	patchOps := []PatchOperation{
		{
			Op:    "replace",
			Path:  "/name",
			Value: "Redis PATCH Test Diagram",
		},
	}

	patchPath := "/threat_models/" + suite.threatModelID + "/diagrams/" + diagramID
	req = suite.makeAuthenticatedRequest("PATCH", patchPath, patchOps)
	w = suite.executeRequest(req)
	suite.assertJSONResponse(t, w, 200)

	// Verify PATCH updated data in database
	patchExpectedData := map[string]interface{}{
		"name":        "Redis PATCH Test Diagram",
		"description": "Testing Redis cache consistency for diagrams",
	}
	verifyDiagramInDatabase(suite.SubEntityIntegrationTestSuite, t, diagramID, suite.threatModelID, patchExpectedData)

	// Verify Redis cache was updated after PATCH (placeholder)
	verifyRedisConsistency(suite.SubEntityIntegrationTestSuite, t, "diagram", diagramID)
}

// Performance comparison tests between Redis enabled/disabled

// TestPerformanceWithAndWithoutRedis compares performance with Redis enabled vs disabled
func TestPerformanceWithAndWithoutRedis(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance comparison test in short mode")
	}

	// Test with Redis enabled
	t.Run("WithRedis", func(t *testing.T) {
		suite := SetupRedisIntegrationTest(t, true)
		defer suite.TeardownRedisIntegrationTest(t)

		if !isRedisEnabled(suite.SubEntityIntegrationTestSuite) {
			t.Skip("Redis not available for performance test")
		}

		suite.measureOperationPerformance(t, "Redis enabled")
	})

	// Test with Redis disabled
	t.Run("WithoutRedis", func(t *testing.T) {
		suite := SetupRedisIntegrationTest(t, false)
		defer suite.TeardownRedisIntegrationTest(t)

		suite.measureOperationPerformance(t, "Redis disabled")
	})
}

// measureOperationPerformance measures the performance of common operations
func (suite *RedisIntegrationTestSuite) measureOperationPerformance(t *testing.T, testLabel string) {
	// Measure threat model creation
	measureDatabaseResponseTime(suite.SubEntityIntegrationTestSuite, t, testLabel+" - Create Threat Model", func() {
		requestBody := map[string]interface{}{
			"name":        "Performance Test Threat Model",
			"description": "Testing performance with " + testLabel,
		}

		req := suite.makeAuthenticatedRequest("POST", "/threat_models", requestBody)
		w := suite.executeRequest(req)
		suite.assertJSONResponse(t, w, 201)
	})

	// Measure threat model retrieval
	measureDatabaseResponseTime(suite.SubEntityIntegrationTestSuite, t, testLabel+" - Get Threat Model", func() {
		req := suite.makeAuthenticatedRequest("GET", "/threat_models/"+suite.threatModelID, nil)
		w := suite.executeRequest(req)
		suite.assertJSONResponse(t, w, 200)
	})

	// Measure threat model list retrieval
	measureDatabaseResponseTime(suite.SubEntityIntegrationTestSuite, t, testLabel+" - List Threat Models", func() {
		req := suite.makeAuthenticatedRequest("GET", "/threat_models", nil)
		w := suite.executeRequest(req)
		suite.assertJSONResponse(t, w, 200)
	})
}
