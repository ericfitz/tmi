package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMetadataIntegration tests the complete CRUD lifecycle for metadata on all entity types
func TestMetadataIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	suite := SetupSubEntityIntegrationTest(t)
	defer suite.TeardownSubEntityIntegrationTest(t)

	// Create sub-entities for metadata testing
	suite.createTestThreat(t)
	suite.createTestDocument(t)
	suite.createTestSource(t)
	suite.createTestDiagram(t)

	t.Run("ThreatMetadata", func(t *testing.T) {
		testThreatMetadata(t, suite)
	})

	t.Run("DocumentMetadata", func(t *testing.T) {
		testDocumentMetadata(t, suite)
	})

	t.Run("SourceMetadata", func(t *testing.T) {
		testSourceMetadata(t, suite)
	})

	t.Run("DiagramMetadata", func(t *testing.T) {
		testDiagramMetadata(t, suite)
	})

	t.Run("ThreatModelMetadata", func(t *testing.T) {
		testThreatModelMetadata(t, suite)
	})
}

// testThreatMetadata tests metadata operations for threats
func testThreatMetadata(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	basePath := fmt.Sprintf("/threat_models/%s/threats/%s/metadata", suite.threatModelID, suite.testThreatID)

	t.Run("POST_CreateMetadata", func(t *testing.T) {
		testCreateMetadata(t, suite, basePath, "threat")
	})

	t.Run("GET_ListMetadata", func(t *testing.T) {
		testListMetadata(t, suite, basePath, "threat")
	})

	t.Run("GET_MetadataByKey", func(t *testing.T) {
		testGetMetadataByKey(t, suite, basePath, "threat")
	})

	t.Run("PUT_UpdateMetadata", func(t *testing.T) {
		testUpdateMetadata(t, suite, basePath, "threat")
	})

	t.Run("DELETE_Metadata", func(t *testing.T) {
		testDeleteMetadata(t, suite, basePath, "threat")
	})

	t.Run("POST_BulkCreateMetadata", func(t *testing.T) {
		testBulkCreateMetadata(t, suite, basePath, "threat")
	})
}

// testDocumentMetadata tests metadata operations for documents
func testDocumentMetadata(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	basePath := fmt.Sprintf("/threat_models/%s/documents/%s/metadata", suite.threatModelID, suite.testDocumentID)

	t.Run("POST_CreateMetadata", func(t *testing.T) {
		testCreateMetadata(t, suite, basePath, "document")
	})

	t.Run("GET_ListMetadata", func(t *testing.T) {
		testListMetadata(t, suite, basePath, "document")
	})

	t.Run("GET_MetadataByKey", func(t *testing.T) {
		testGetMetadataByKey(t, suite, basePath, "document")
	})

	t.Run("PUT_UpdateMetadata", func(t *testing.T) {
		testUpdateMetadata(t, suite, basePath, "document")
	})

	t.Run("DELETE_Metadata", func(t *testing.T) {
		testDeleteMetadata(t, suite, basePath, "document")
	})

	t.Run("POST_BulkCreateMetadata", func(t *testing.T) {
		testBulkCreateMetadata(t, suite, basePath, "document")
	})
}

// testSourceMetadata tests metadata operations for sources
func testSourceMetadata(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	basePath := fmt.Sprintf("/threat_models/%s/sources/%s/metadata", suite.threatModelID, suite.testSourceID)

	t.Run("POST_CreateMetadata", func(t *testing.T) {
		testCreateMetadata(t, suite, basePath, "source")
	})

	t.Run("GET_ListMetadata", func(t *testing.T) {
		testListMetadata(t, suite, basePath, "source")
	})

	t.Run("GET_MetadataByKey", func(t *testing.T) {
		testGetMetadataByKey(t, suite, basePath, "source")
	})

	t.Run("PUT_UpdateMetadata", func(t *testing.T) {
		testUpdateMetadata(t, suite, basePath, "source")
	})

	t.Run("DELETE_Metadata", func(t *testing.T) {
		testDeleteMetadata(t, suite, basePath, "source")
	})

	t.Run("POST_BulkCreateMetadata", func(t *testing.T) {
		testBulkCreateMetadata(t, suite, basePath, "source")
	})
}

// testDiagramMetadata tests metadata operations for diagrams
func testDiagramMetadata(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	basePath := fmt.Sprintf("/threat_models/%s/diagrams/%s/metadata", suite.threatModelID, suite.testDiagramID)

	t.Run("POST_CreateMetadata", func(t *testing.T) {
		testCreateMetadata(t, suite, basePath, "diagram")
	})

	t.Run("GET_ListMetadata", func(t *testing.T) {
		testListMetadata(t, suite, basePath, "diagram")
	})

	t.Run("GET_MetadataByKey", func(t *testing.T) {
		testGetMetadataByKey(t, suite, basePath, "diagram")
	})

	t.Run("PUT_UpdateMetadata", func(t *testing.T) {
		testUpdateMetadata(t, suite, basePath, "diagram")
	})

	t.Run("DELETE_Metadata", func(t *testing.T) {
		testDeleteMetadata(t, suite, basePath, "diagram")
	})

	t.Run("POST_BulkCreateMetadata", func(t *testing.T) {
		testBulkCreateMetadata(t, suite, basePath, "diagram")
	})
}

// testCreateMetadata tests creating metadata entries
func testCreateMetadata(t *testing.T, suite *SubEntityIntegrationTestSuite, basePath, entityType string) {
	// Test data
	requestBody := map[string]interface{}{
		"key":   fmt.Sprintf("%s_integration_test_key", entityType),
		"value": fmt.Sprintf("Integration test value for %s", entityType),
	}

	// Make request
	req := suite.makeAuthenticatedRequest("POST", basePath, requestBody)
	w := suite.executeRequest(req)

	// Verify response
	response := suite.assertJSONResponse(t, w, http.StatusCreated)

	// Verify response contains expected fields
	assert.Equal(t, requestBody["key"], response["key"])
	assert.Equal(t, requestBody["value"], response["value"])
}

// testListMetadata tests retrieving metadata list
func testListMetadata(t *testing.T, suite *SubEntityIntegrationTestSuite, basePath, entityType string) {
	// First create a metadata entry
	createBody := map[string]interface{}{
		"key":   fmt.Sprintf("%s_list_test_key", entityType),
		"value": fmt.Sprintf("List test value for %s", entityType),
	}
	req := suite.makeAuthenticatedRequest("POST", basePath, createBody)
	w := suite.executeRequest(req)
	suite.assertJSONResponse(t, w, http.StatusCreated)

	// Test GET list
	req = suite.makeAuthenticatedRequest("GET", basePath, nil)
	w = suite.executeRequest(req)

	response := suite.assertJSONArrayResponse(t, w, http.StatusOK)

	// Verify response
	assert.GreaterOrEqual(t, len(response), 1, "Should return at least one metadata entry")

	// Check the first metadata entry in the list
	if len(response) > 0 {
		metadata := response[0].(map[string]interface{})
		assert.NotEmpty(t, metadata["key"])
		assert.NotEmpty(t, metadata["value"])
	}
}

// testGetMetadataByKey tests retrieving specific metadata by key
func testGetMetadataByKey(t *testing.T, suite *SubEntityIntegrationTestSuite, basePath, entityType string) {
	// First create a metadata entry
	testKey := fmt.Sprintf("%s_get_test_key", entityType)
	testValue := fmt.Sprintf("Get test value for %s", entityType)
	createBody := map[string]interface{}{
		"key":   testKey,
		"value": testValue,
	}
	req := suite.makeAuthenticatedRequest("POST", basePath, createBody)
	w := suite.executeRequest(req)
	suite.assertJSONResponse(t, w, http.StatusCreated)

	// Test GET by key
	path := fmt.Sprintf("%s/%s", basePath, testKey)
	req = suite.makeAuthenticatedRequest("GET", path, nil)
	w = suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify response
	assert.Equal(t, testKey, response["key"])
	assert.Equal(t, testValue, response["value"])
}

// testUpdateMetadata tests updating metadata entries
func testUpdateMetadata(t *testing.T, suite *SubEntityIntegrationTestSuite, basePath, entityType string) {
	// First create a metadata entry
	testKey := fmt.Sprintf("%s_update_test_key", entityType)
	createBody := map[string]interface{}{
		"key":   testKey,
		"value": fmt.Sprintf("Original value for %s", entityType),
	}
	req := suite.makeAuthenticatedRequest("POST", basePath, createBody)
	w := suite.executeRequest(req)
	suite.assertJSONResponse(t, w, http.StatusCreated)

	// Update the metadata entry
	updateBody := map[string]interface{}{
		"key":   testKey,
		"value": fmt.Sprintf("Updated value for %s", entityType),
	}

	path := fmt.Sprintf("%s/%s", basePath, testKey)
	req = suite.makeAuthenticatedRequest("PUT", path, updateBody)
	w = suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify updates
	assert.Equal(t, testKey, response["key"])
	assert.Equal(t, updateBody["value"], response["value"])
}

// testDeleteMetadata tests deleting metadata entries
func testDeleteMetadata(t *testing.T, suite *SubEntityIntegrationTestSuite, basePath, entityType string) {
	// First create a metadata entry
	testKey := fmt.Sprintf("%s_delete_test_key", entityType)
	createBody := map[string]interface{}{
		"key":   testKey,
		"value": fmt.Sprintf("Delete test value for %s", entityType),
	}
	req := suite.makeAuthenticatedRequest("POST", basePath, createBody)
	w := suite.executeRequest(req)
	suite.assertJSONResponse(t, w, http.StatusCreated)

	// Delete the metadata entry
	path := fmt.Sprintf("%s/%s", basePath, testKey)
	req = suite.makeAuthenticatedRequest("DELETE", path, nil)
	w = suite.executeRequest(req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify the metadata entry no longer exists
	req = suite.makeAuthenticatedRequest("GET", path, nil)
	w = suite.executeRequest(req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// testBulkCreateMetadata tests bulk creating metadata entries
func testBulkCreateMetadata(t *testing.T, suite *SubEntityIntegrationTestSuite, basePath, entityType string) {
	// Test data for bulk create (direct array, no wrapper)
	requestBody := []map[string]interface{}{
		{
			"key":   fmt.Sprintf("%s_bulk_key_1", entityType),
			"value": fmt.Sprintf("Bulk value 1 for %s", entityType),
		},
		{
			"key":   fmt.Sprintf("%s_bulk_key_2", entityType),
			"value": fmt.Sprintf("Bulk value 2 for %s", entityType),
		},
		{
			"key":   fmt.Sprintf("%s_bulk_key_3", entityType),
			"value": fmt.Sprintf("Bulk value 3 for %s", entityType),
		},
	}

	// Make request
	path := fmt.Sprintf("%s/bulk", basePath)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	metadata := suite.assertJSONArrayResponse(t, w, http.StatusCreated)
	assert.Len(t, metadata, 3, "Should create 3 metadata entries")

	// Verify each created metadata entry
	for i, metadataInterface := range metadata {
		metadataEntry := metadataInterface.(map[string]interface{})
		originalEntry := requestBody[i]

		assert.Equal(t, originalEntry["key"], metadataEntry["key"])
		assert.Equal(t, originalEntry["value"], metadataEntry["value"])
	}

	// Verify the metadata entries were created by trying to retrieve them
	for _, metadataEntry := range requestBody {
		testKey := metadataEntry["key"].(string)

		path := fmt.Sprintf("%s/%s", basePath, testKey)
		req := suite.makeAuthenticatedRequest("GET", path, nil)
		w := suite.executeRequest(req)

		if w.Code == http.StatusOK {
			retrievedResponse := suite.assertJSONResponse(t, w, http.StatusOK)
			assert.Equal(t, testKey, retrievedResponse["key"])
		}
	}
}

// testThreatModelMetadata tests metadata operations for threat models
func testThreatModelMetadata(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	basePath := fmt.Sprintf("/threat_models/%s/metadata", suite.threatModelID)

	t.Run("POST_CreateMetadata", func(t *testing.T) {
		testCreateMetadata(t, suite, basePath, "threat_model")
	})

	t.Run("GET_ListMetadata", func(t *testing.T) {
		testListMetadata(t, suite, basePath, "threat_model")
	})

	t.Run("GET_MetadataByKey", func(t *testing.T) {
		testGetMetadataByKey(t, suite, basePath, "threat_model")
	})

	t.Run("PUT_UpdateMetadata", func(t *testing.T) {
		testUpdateMetadata(t, suite, basePath, "threat_model")
	})

	t.Run("DELETE_Metadata", func(t *testing.T) {
		testDeleteMetadata(t, suite, basePath, "threat_model")
	})

	t.Run("POST_BulkCreateMetadata", func(t *testing.T) {
		testBulkCreateMetadata(t, suite, basePath, "threat_model")
	})
}
