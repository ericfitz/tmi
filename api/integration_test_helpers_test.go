package api

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: SubEntityIntegrationTestSuite is defined in sub_entities_integration_test.go
// These helper functions work with that suite type

// Database verification methods for each entity type

// verifyThreatModelInDatabase verifies a threat model exists in the database with expected data
func verifyThreatModelInDatabase(suite *SubEntityIntegrationTestSuite, t *testing.T, id string, expectedData map[string]interface{}) {
	ctx := context.Background()
	db := suite.dbManager.Postgres().GetDB()

	query := `SELECT id, name, description, owner_email, threat_model_framework, created_at, modified_at 
			  FROM threat_models WHERE id = $1`

	var dbID, name, description, owner, framework sql.NullString
	var createdAt, modifiedAt sql.NullTime

	err := db.QueryRowContext(ctx, query, id).Scan(
		&dbID, &name, &description, &owner, &framework,
		&createdAt, &modifiedAt)
	require.NoError(t, err, "Threat model should exist in database")

	// Verify basic fields
	assert.Equal(t, id, dbID.String)
	if expectedName, exists := expectedData["name"]; exists {
		assert.Equal(t, expectedName, name.String)
	}
	if expectedDesc, exists := expectedData["description"]; exists {
		assert.Equal(t, expectedDesc, description.String)
	}
	if expectedOwner, exists := expectedData["owner"]; exists {
		assert.Equal(t, expectedOwner, owner.String)
	}
	if expectedFramework, exists := expectedData["threat_model_framework"]; exists {
		assert.Equal(t, expectedFramework, framework.String)
	}

	// Verify timestamps exist
	assert.True(t, createdAt.Valid, "created_at should be set")
	assert.True(t, modifiedAt.Valid, "modified_at should be set")
}

// verifyThreatInDatabase verifies a threat exists in the database with expected data
func verifyThreatInDatabase(suite *SubEntityIntegrationTestSuite, t *testing.T, id, threatModelID string, expectedData map[string]interface{}) {
	ctx := context.Background()
	db := suite.dbManager.Postgres().GetDB()

	query := `SELECT id, threat_model_id, name, description, severity, status, threat_type, priority, mitigated, created_at, modified_at 
			  FROM threats WHERE id = $1 AND threat_model_id = $2`

	var dbID, dbThreatModelID, name, description, severity, status, threatType, priority sql.NullString
	var mitigated sql.NullBool
	var createdAt, modifiedAt sql.NullTime

	err := db.QueryRowContext(ctx, query, id, threatModelID).Scan(
		&dbID, &dbThreatModelID, &name, &description, &severity, &status,
		&threatType, &priority, &mitigated, &createdAt, &modifiedAt)
	require.NoError(t, err, "Threat should exist in database")

	// Verify basic fields
	assert.Equal(t, id, dbID.String)
	assert.Equal(t, threatModelID, dbThreatModelID.String)

	assertFieldsMatch(t, map[string]interface{}{
		"name":        name.String,
		"description": description.String,
		"severity":    severity.String,
		"status":      status.String,
		"threat_type": threatType.String,
		"priority":    priority.String,
		"mitigated":   mitigated.Bool,
	}, expectedData)

	// Verify timestamps exist
	assert.True(t, createdAt.Valid, "created_at should be set")
	assert.True(t, modifiedAt.Valid, "modified_at should be set")
}

// verifyDocumentInDatabase verifies a document exists in the database with expected data
func verifyDocumentInDatabase(suite *SubEntityIntegrationTestSuite, t *testing.T, id, threatModelID string, expectedData map[string]interface{}) {
	ctx := context.Background()
	db := suite.dbManager.Postgres().GetDB()

	query := `SELECT id, threat_model_id, name, description, url, created_at, modified_at 
			  FROM documents WHERE id = $1 AND threat_model_id = $2`

	var dbID, dbThreatModelID, name, description, url sql.NullString
	var createdAt, modifiedAt sql.NullTime

	err := db.QueryRowContext(ctx, query, id, threatModelID).Scan(
		&dbID, &dbThreatModelID, &name, &description, &url, &createdAt, &modifiedAt)
	require.NoError(t, err, "Document should exist in database")

	// Verify basic fields
	assert.Equal(t, id, dbID.String)
	assert.Equal(t, threatModelID, dbThreatModelID.String)

	assertFieldsMatch(t, map[string]interface{}{
		"name":        name.String,
		"description": description.String,
		"url":         url.String,
	}, expectedData)

	// Verify timestamps exist
	assert.True(t, createdAt.Valid, "created_at should be set")
	assert.True(t, modifiedAt.Valid, "modified_at should be set")
}

// verifyDiagramInDatabase verifies a diagram exists in the database with expected data
func verifyDiagramInDatabase(suite *SubEntityIntegrationTestSuite, t *testing.T, id, threatModelID string, expectedData map[string]interface{}) {
	ctx := context.Background()
	db := suite.dbManager.Postgres().GetDB()

	query := `SELECT id, threat_model_id, name, created_at, modified_at 
			  FROM diagrams WHERE id = $1 AND threat_model_id = $2`

	var dbID, dbThreatModelID, name sql.NullString
	var createdAt, modifiedAt sql.NullTime

	err := db.QueryRowContext(ctx, query, id, threatModelID).Scan(
		&dbID, &dbThreatModelID, &name, &createdAt, &modifiedAt)
	require.NoError(t, err, "Diagram should exist in database")

	// Verify basic fields
	assert.Equal(t, id, dbID.String)
	assert.Equal(t, threatModelID, dbThreatModelID.String)

	assertFieldsMatch(t, map[string]interface{}{
		"name": name.String,
	}, expectedData)

	// Note: Content field is no longer used in diagrams schema, replaced by cells

	// Verify timestamps exist
	assert.True(t, createdAt.Valid, "created_at should be set")
	assert.True(t, modifiedAt.Valid, "modified_at should be set")
}

// Negative verification methods (deletion testing)

// verifyThreatNotInDatabase verifies a threat does not exist in the database
func verifyThreatNotInDatabase(suite *SubEntityIntegrationTestSuite, t *testing.T, id string) {
	ctx := context.Background()
	db := suite.dbManager.Postgres().GetDB()

	query := `SELECT COUNT(*) FROM threats WHERE id = $1`
	var count int
	err := db.QueryRowContext(ctx, query, id).Scan(&count)
	require.NoError(t, err, "Should be able to query threat count")
	assert.Equal(t, 0, count, "Threat should not exist in database")
}

// Field-specific verification methods

// assertFieldsMatch compares expected fields with actual database fields
func assertFieldsMatch(t *testing.T, actual map[string]interface{}, expected map[string]interface{}) {
	for key, expectedValue := range expected {
		if actualValue, exists := actual[key]; exists {
			assert.Equal(t, expectedValue, actualValue, "Field %s should match expected value", key)
		} else {
			t.Errorf("Expected field %s not found in actual data", key)
		}
	}
}

// Redis verification helpers

// verifyRedisConsistency verifies that Redis cache is consistent with database
func verifyRedisConsistency(suite *SubEntityIntegrationTestSuite, t *testing.T, entityType, entityID string) {
	// Only verify if Redis is enabled
	if !isRedisEnabled(suite) {
		t.Skip("Redis not enabled, skipping consistency check")
		return
	}

	// Implementation depends on specific caching strategy
	// This is a placeholder for future Redis consistency checks
	t.Logf("Verifying Redis consistency for %s:%s", entityType, entityID)
}

// isRedisEnabled checks if Redis is enabled for the current test
func isRedisEnabled(suite *SubEntityIntegrationTestSuite) bool {
	// Check if Redis client is available and connected
	if suite.dbManager == nil {
		return false
	}

	redis := suite.dbManager.Redis()
	if redis == nil {
		return false
	}

	// Test Redis connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := redis.Ping(ctx)
	return err == nil
}

// Performance and timing helpers

// measureDatabaseResponseTime measures how long a database operation takes
func measureDatabaseResponseTime(_ *SubEntityIntegrationTestSuite, t *testing.T, operation string, fn func()) time.Duration {
	start := time.Now()
	fn()
	duration := time.Since(start)
	t.Logf("Database operation '%s' took %v", operation, duration)
	return duration
}
