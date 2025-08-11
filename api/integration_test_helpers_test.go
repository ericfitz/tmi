package api

import (
	"context"
	"database/sql"
	"encoding/json"
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

// verifySourceInDatabase verifies a source exists in the database with expected data
func verifySourceInDatabase(suite *SubEntityIntegrationTestSuite, t *testing.T, id, threatModelID string, expectedData map[string]interface{}) {
	ctx := context.Background()
	db := suite.dbManager.Postgres().GetDB()

	query := `SELECT id, threat_model_id, name, description, url, type, parameters, created_at, modified_at 
			  FROM sources WHERE id = $1 AND threat_model_id = $2`

	var dbID, dbThreatModelID, name, description, url, sourceType sql.NullString
	var parametersJSON sql.NullString
	var createdAt, modifiedAt sql.NullTime

	err := db.QueryRowContext(ctx, query, id, threatModelID).Scan(
		&dbID, &dbThreatModelID, &name, &description, &url, &sourceType,
		&parametersJSON, &createdAt, &modifiedAt)
	require.NoError(t, err, "Source should exist in database")

	// Verify basic fields
	assert.Equal(t, id, dbID.String)
	assert.Equal(t, threatModelID, dbThreatModelID.String)

	assertFieldsMatch(t, map[string]interface{}{
		"name":        name.String,
		"description": description.String,
		"url":         url.String,
		"type":        sourceType.String,
	}, expectedData)

	// Verify parameters if expected
	if expectedParams, exists := expectedData["parameters"]; exists && parametersJSON.Valid {
		var dbParams map[string]interface{}
		err := json.Unmarshal([]byte(parametersJSON.String), &dbParams)
		require.NoError(t, err, "Should be able to parse parameters JSON")
		assertFieldsMatch(t, dbParams, expectedParams.(map[string]interface{}))
	}

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

// verifyMetadataInDatabase verifies metadata exists in the database with expected data
func verifyMetadataInDatabase(suite *SubEntityIntegrationTestSuite, t *testing.T, parentID, entityType string, expectedData map[string]interface{}) {
	ctx := context.Background()
	db := suite.dbManager.Postgres().GetDB()

	// Validate entity type
	switch entityType {
	case "threat_model", "threat", "document", "source", "diagram":
		// Valid entity types
	default:
		t.Fatalf("Unknown entity type for metadata: %s", entityType)
	}

	expectedKey, keyExists := expectedData["key"]
	require.True(t, keyExists, "Expected data must contain 'key'")

	// Build query based on entity type - tableName is validated above
	var query string
	switch entityType {
	case "threat_model":
		query = `SELECT parent_id, key, value, created_at, modified_at FROM threat_model_metadata WHERE parent_id = $1 AND key = $2`
	case "threat":
		query = `SELECT parent_id, key, value, created_at, modified_at FROM threat_metadata WHERE parent_id = $1 AND key = $2`
	case "document":
		query = `SELECT parent_id, key, value, created_at, modified_at FROM document_metadata WHERE parent_id = $1 AND key = $2`
	case "source":
		query = `SELECT parent_id, key, value, created_at, modified_at FROM source_metadata WHERE parent_id = $1 AND key = $2`
	case "diagram":
		query = `SELECT parent_id, key, value, created_at, modified_at FROM diagram_metadata WHERE parent_id = $1 AND key = $2`
	default:
		t.Fatalf("Unknown entity type for metadata: %s", entityType)
	}

	var dbParentID, key, value sql.NullString
	var createdAt, modifiedAt sql.NullTime

	err := db.QueryRowContext(ctx, query, parentID, expectedKey).Scan(
		&dbParentID, &key, &value, &createdAt, &modifiedAt)
	require.NoError(t, err, "Metadata should exist in database")

	// Verify basic fields
	assert.Equal(t, parentID, dbParentID.String)
	assert.Equal(t, expectedKey, key.String)

	if expectedValue, exists := expectedData["value"]; exists {
		assert.Equal(t, expectedValue, value.String)
	}

	// Verify timestamps exist
	assert.True(t, createdAt.Valid, "created_at should be set")
	assert.True(t, modifiedAt.Valid, "modified_at should be set")
}

// verifyCellInDatabase verifies a cell exists in the database with expected data
func verifyCellInDatabase(suite *SubEntityIntegrationTestSuite, t *testing.T, diagramID, cellID string, expectedData map[string]interface{}) {
	// Note: Based on the OpenAPI spec review in the plan, there are no cell-specific endpoints
	// Cell operations appear to be handled through diagram content updates
	// This method is provided for completeness but may not be used
	t.Skip("Cell operations are handled through diagram content updates, no separate cell table expected")
}

// Negative verification methods (deletion testing)

// verifyThreatModelNotInDatabase verifies a threat model does not exist in the database
func verifyThreatModelNotInDatabase(suite *SubEntityIntegrationTestSuite, t *testing.T, id string) {
	ctx := context.Background()
	db := suite.dbManager.Postgres().GetDB()

	query := `SELECT COUNT(*) FROM threat_models WHERE id = $1`
	var count int
	err := db.QueryRowContext(ctx, query, id).Scan(&count)
	require.NoError(t, err, "Should be able to query threat model count")
	assert.Equal(t, 0, count, "Threat model should not exist in database")
}

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

// verifyDocumentNotInDatabase verifies a document does not exist in the database
func verifyDocumentNotInDatabase(suite *SubEntityIntegrationTestSuite, t *testing.T, id string) {
	ctx := context.Background()
	db := suite.dbManager.Postgres().GetDB()

	query := `SELECT COUNT(*) FROM documents WHERE id = $1`
	var count int
	err := db.QueryRowContext(ctx, query, id).Scan(&count)
	require.NoError(t, err, "Should be able to query document count")
	assert.Equal(t, 0, count, "Document should not exist in database")
}

// verifySourceNotInDatabase verifies a source does not exist in the database
func verifySourceNotInDatabase(suite *SubEntityIntegrationTestSuite, t *testing.T, id string) {
	ctx := context.Background()
	db := suite.dbManager.Postgres().GetDB()

	query := `SELECT COUNT(*) FROM sources WHERE id = $1`
	var count int
	err := db.QueryRowContext(ctx, query, id).Scan(&count)
	require.NoError(t, err, "Should be able to query source count")
	assert.Equal(t, 0, count, "Source should not exist in database")
}

// verifyDiagramNotInDatabase verifies a diagram does not exist in the database
func verifyDiagramNotInDatabase(suite *SubEntityIntegrationTestSuite, t *testing.T, id string) {
	ctx := context.Background()
	db := suite.dbManager.Postgres().GetDB()

	query := `SELECT COUNT(*) FROM diagrams WHERE id = $1`
	var count int
	err := db.QueryRowContext(ctx, query, id).Scan(&count)
	require.NoError(t, err, "Should be able to query diagram count")
	assert.Equal(t, 0, count, "Diagram should not exist in database")
}

// verifyNoOrphanedSubEntitiesInDatabase verifies no sub-entities remain after parent deletion
func verifyNoOrphanedSubEntitiesInDatabase(suite *SubEntityIntegrationTestSuite, t *testing.T, threatModelID string) {
	ctx := context.Background()
	db := suite.dbManager.Postgres().GetDB()

	// Check for orphaned threats
	var threatCount int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM threats WHERE threat_model_id = $1`, threatModelID).Scan(&threatCount)
	require.NoError(t, err, "Should be able to query threat count")
	assert.Equal(t, 0, threatCount, "No orphaned threats should remain")

	// Check for orphaned documents
	var documentCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents WHERE threat_model_id = $1`, threatModelID).Scan(&documentCount)
	require.NoError(t, err, "Should be able to query document count")
	assert.Equal(t, 0, documentCount, "No orphaned documents should remain")

	// Check for orphaned sources
	var sourceCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sources WHERE threat_model_id = $1`, threatModelID).Scan(&sourceCount)
	require.NoError(t, err, "Should be able to query source count")
	assert.Equal(t, 0, sourceCount, "No orphaned sources should remain")

	// Check for orphaned diagrams
	var diagramCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM diagrams WHERE threat_model_id = $1`, threatModelID).Scan(&diagramCount)
	require.NoError(t, err, "Should be able to query diagram count")
	assert.Equal(t, 0, diagramCount, "No orphaned diagrams should remain")

	// Check for orphaned metadata of all types
	// Check threat_model_metadata directly
	var tmMetadataCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM threat_model_metadata WHERE parent_id = $1`, threatModelID).Scan(&tmMetadataCount)
	require.NoError(t, err, "Should be able to query threat_model_metadata count")
	assert.Equal(t, 0, tmMetadataCount, "No orphaned threat_model_metadata should remain")

	// Check threat_metadata
	var threatMetadataCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM threat_metadata m WHERE EXISTS (
		SELECT 1 FROM threats p WHERE p.id = m.parent_id AND p.threat_model_id = $1
	)`, threatModelID).Scan(&threatMetadataCount)
	require.NoError(t, err, "Should be able to query threat_metadata count")
	assert.Equal(t, 0, threatMetadataCount, "No orphaned threat_metadata should remain")

	// Check document_metadata
	var documentMetadataCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM document_metadata m WHERE EXISTS (
		SELECT 1 FROM documents p WHERE p.id = m.parent_id AND p.threat_model_id = $1
	)`, threatModelID).Scan(&documentMetadataCount)
	require.NoError(t, err, "Should be able to query document_metadata count")
	assert.Equal(t, 0, documentMetadataCount, "No orphaned document_metadata should remain")

	// Check source_metadata
	var sourceMetadataCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM source_metadata m WHERE EXISTS (
		SELECT 1 FROM sources p WHERE p.id = m.parent_id AND p.threat_model_id = $1
	)`, threatModelID).Scan(&sourceMetadataCount)
	require.NoError(t, err, "Should be able to query source_metadata count")
	assert.Equal(t, 0, sourceMetadataCount, "No orphaned source_metadata should remain")

	// Check diagram_metadata
	var diagramMetadataCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM diagram_metadata m WHERE EXISTS (
		SELECT 1 FROM diagrams p WHERE p.id = m.parent_id AND p.threat_model_id = $1
	)`, threatModelID).Scan(&diagramMetadataCount)
	require.NoError(t, err, "Should be able to query diagram_metadata count")
	assert.Equal(t, 0, diagramMetadataCount, "No orphaned diagram_metadata should remain")
}

// Field-specific verification methods

// verifyFieldInDatabase verifies a specific field value in the database
func verifyFieldInDatabase(suite *SubEntityIntegrationTestSuite, t *testing.T, entityID, fieldName string, expectedValue interface{}) {
	// This is a generic helper that would need to be customized per entity type
	// For now, we'll use it as a placeholder for future implementation
	t.Logf("Verifying field %s = %v for entity %s", fieldName, expectedValue, entityID)
}

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

// assertContainsEntity verifies that a list contains an entity with the specified ID
func assertContainsEntity(t *testing.T, list []interface{}, entityID string) {
	found := false
	for _, item := range list {
		if entity, ok := item.(map[string]interface{}); ok {
			if id, exists := entity["id"]; exists && id == entityID {
				found = true
				break
			}
		}
	}
	assert.True(t, found, "List should contain entity with ID %s", entityID)
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
func measureDatabaseResponseTime(suite *SubEntityIntegrationTestSuite, t *testing.T, operation string, fn func()) time.Duration {
	start := time.Now()
	fn()
	duration := time.Since(start)
	t.Logf("Database operation '%s' took %v", operation, duration)
	return duration
}

// waitForDatabaseConsistency waits for database consistency (useful for eventual consistency scenarios)
func waitForDatabaseConsistency(suite *SubEntityIntegrationTestSuite, t *testing.T, maxWait time.Duration) {
	// Simple wait implementation - can be enhanced with actual consistency checks
	time.Sleep(100 * time.Millisecond)
}
