package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBatchIntegration tests the complete batch operations with real database persistence
func TestBatchIntegration(t *testing.T) {
	suite := SetupSubEntityIntegrationTest(t)
	defer suite.TeardownSubEntityIntegrationTest(t)

	t.Run("POST /threat_models/:id/threats/batch/patch", func(t *testing.T) {
		testBatchPatchThreats(t, suite)
	})

	t.Run("DELETE /threat_models/:id/threats/batch", func(t *testing.T) {
		testBatchDeleteThreats(t, suite)
	})
}

// TestBulkIntegration tests the complete bulk operations with real database persistence
func TestBulkIntegration(t *testing.T) {
	suite := SetupSubEntityIntegrationTest(t)
	defer suite.TeardownSubEntityIntegrationTest(t)

	t.Run("POST /threat_models/:id/threats/bulk", func(t *testing.T) {
		testBulkCreateThreats(t, suite)
	})

	t.Run("PUT /threat_models/:id/threats/bulk", func(t *testing.T) {
		testBulkUpdateThreats(t, suite)
	})

	t.Run("POST /threat_models/:id/documents/bulk", func(t *testing.T) {
		testBulkCreateDocuments(t, suite)
	})

	t.Run("POST /threat_models/:id/sources/bulk", func(t *testing.T) {
		testBulkCreateSources(t, suite)
	})
}

// testBatchPatchThreats tests batch patching multiple threats
func testBatchPatchThreats(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// First create some threats to patch
	threat1ID := suite.createTestThreat(t)
	threat2ID := suite.createTestThreat(t)
	threat3ID := suite.createTestThreat(t)

	// Test successful batch patch
	t.Run("SuccessfulBatchPatch", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"operations": []map[string]interface{}{
				{
					"threat_id": threat1ID,
					"operations": []map[string]interface{}{
						{
							"op":    "replace",
							"path":  "/name",
							"value": "Batch Updated Threat 1",
						},
						{
							"op":    "replace",
							"path":  "/severity",
							"value": "critical",
						},
					},
				},
				{
					"threat_id": threat2ID,
					"operations": []map[string]interface{}{
						{
							"op":    "replace",
							"path":  "/name",
							"value": "Batch Updated Threat 2",
						},
						{
							"op":    "replace",
							"path":  "/status",
							"value": "mitigated",
						},
					},
				},
			},
		}

		path := fmt.Sprintf("/threat_models/%s/threats/batch/patch", suite.threatModelID)
		req := suite.makeAuthenticatedRequest("POST", path, requestBody)
		w := suite.executeRequest(req)

		response := suite.assertJSONResponse(t, w, http.StatusOK)

		// Verify response structure
		assert.Contains(t, response, "results")
		assert.Contains(t, response, "summary")

		results := response["results"].([]interface{})
		summary := response["summary"].(map[string]interface{})

		assert.Len(t, results, 2, "Should have 2 results")
		assert.Equal(t, float64(2), summary["total"])
		assert.Equal(t, float64(2), summary["succeeded"])
		assert.Equal(t, float64(0), summary["failed"])

		// Verify each result
		result1 := results[0].(map[string]interface{})
		assert.Equal(t, threat1ID, result1["threat_id"])
		assert.Equal(t, true, result1["success"])
		assert.Contains(t, result1, "threat")

		result2 := results[1].(map[string]interface{})
		assert.Equal(t, threat2ID, result2["threat_id"])
		assert.Equal(t, true, result2["success"])
		assert.Contains(t, result2, "threat")

		// Verify database persistence by fetching the updated threats
		verifyThreatInDatabase(suite, t, threat1ID, suite.threatModelID, map[string]interface{}{
			"name":     "Batch Updated Threat 1",
			"severity": "critical",
		})

		verifyThreatInDatabase(suite, t, threat2ID, suite.threatModelID, map[string]interface{}{
			"name":   "Batch Updated Threat 2",
			"status": "mitigated",
		})
	})

	// Test partial failure scenario
	t.Run("PartialFailureBatchPatch", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"operations": []map[string]interface{}{
				{
					"threat_id": threat3ID,
					"operations": []map[string]interface{}{
						{
							"op":    "replace",
							"path":  "/name",
							"value": "Successfully Updated Threat",
						},
					},
				},
				{
					"threat_id": "00000000-0000-0000-0000-999999999999", // Non-existent threat
					"operations": []map[string]interface{}{
						{
							"op":    "replace",
							"path":  "/name",
							"value": "This Will Fail",
						},
					},
				},
			},
		}

		path := fmt.Sprintf("/threat_models/%s/threats/batch/patch", suite.threatModelID)
		req := suite.makeAuthenticatedRequest("POST", path, requestBody)
		w := suite.executeRequest(req)

		response := suite.assertJSONResponse(t, w, http.StatusMultiStatus)

		results := response["results"].([]interface{})
		summary := response["summary"].(map[string]interface{})

		assert.Len(t, results, 2, "Should have 2 results")
		assert.Equal(t, float64(2), summary["total"])
		assert.Equal(t, float64(1), summary["succeeded"])
		assert.Equal(t, float64(1), summary["failed"])

		// First result should succeed
		result1 := results[0].(map[string]interface{})
		assert.Equal(t, threat3ID, result1["threat_id"])
		assert.Equal(t, true, result1["success"])

		// Second result should fail
		result2 := results[1].(map[string]interface{})
		assert.Equal(t, "00000000-0000-0000-0000-999999999999", result2["threat_id"])
		assert.Equal(t, false, result2["success"])
		assert.Contains(t, result2, "error")
	})

	// Test validation errors
	t.Run("ValidationErrors", func(t *testing.T) {
		// Test empty operations
		requestBody := map[string]interface{}{
			"operations": []map[string]interface{}{},
		}

		path := fmt.Sprintf("/threat_models/%s/threats/batch/patch", suite.threatModelID)
		req := suite.makeAuthenticatedRequest("POST", path, requestBody)
		w := suite.executeRequest(req)

		suite.assertJSONResponse(t, w, http.StatusBadRequest)

		// Test too many operations (over 20 limit)
		operations := make([]map[string]interface{}, 21)
		for i := 0; i < 21; i++ {
			operations[i] = map[string]interface{}{
				"threat_id": threat1ID,
				"operations": []map[string]interface{}{
					{
						"op":    "replace",
						"path":  "/name",
						"value": fmt.Sprintf("Test %d", i),
					},
				},
			}
		}

		requestBody = map[string]interface{}{
			"operations": operations,
		}

		req = suite.makeAuthenticatedRequest("POST", path, requestBody)
		w = suite.executeRequest(req)

		suite.assertJSONResponse(t, w, http.StatusBadRequest)
	})
}

// testBatchDeleteThreats tests batch deleting multiple threats
func testBatchDeleteThreats(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// First create some threats to delete
	threat1ID := suite.createTestThreat(t)
	threat2ID := suite.createTestThreat(t)
	threat3ID := suite.createTestThreat(t)

	// Test successful batch delete
	t.Run("SuccessfulBatchDelete", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"threat_ids": []string{threat1ID, threat2ID},
		}

		path := fmt.Sprintf("/threat_models/%s/threats/batch", suite.threatModelID)
		req := suite.makeAuthenticatedRequest("DELETE", path, requestBody)
		w := suite.executeRequest(req)

		response := suite.assertJSONResponse(t, w, http.StatusOK)

		// Verify response structure
		assert.Contains(t, response, "results")
		assert.Contains(t, response, "summary")

		results := response["results"].([]interface{})
		summary := response["summary"].(map[string]interface{})

		assert.Len(t, results, 2, "Should have 2 results")
		assert.Equal(t, float64(2), summary["total"])
		assert.Equal(t, float64(2), summary["succeeded"])
		assert.Equal(t, float64(0), summary["failed"])

		// Verify each result
		result1 := results[0].(map[string]interface{})
		assert.Equal(t, threat1ID, result1["threat_id"])
		assert.Equal(t, true, result1["success"])

		result2 := results[1].(map[string]interface{})
		assert.Equal(t, threat2ID, result2["threat_id"])
		assert.Equal(t, true, result2["success"])

		// Verify database persistence - threats should be deleted
		verifyThreatNotInDatabase(suite, t, threat1ID)
		verifyThreatNotInDatabase(suite, t, threat2ID)

		// Verify threat3 still exists (wasn't deleted)
		verifyThreatInDatabase(suite, t, threat3ID, suite.threatModelID, map[string]interface{}{})
	})

	// Test partial failure scenario
	t.Run("PartialFailureBatchDelete", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"threat_ids": []string{
				threat3ID,                              // Exists
				"00000000-0000-0000-0000-999999999999", // Non-existent
			},
		}

		path := fmt.Sprintf("/threat_models/%s/threats/batch", suite.threatModelID)
		req := suite.makeAuthenticatedRequest("DELETE", path, requestBody)
		w := suite.executeRequest(req)

		response := suite.assertJSONResponse(t, w, http.StatusMultiStatus)

		results := response["results"].([]interface{})
		summary := response["summary"].(map[string]interface{})

		assert.Len(t, results, 2, "Should have 2 results")
		assert.Equal(t, float64(2), summary["total"])
		assert.Equal(t, float64(1), summary["succeeded"])
		assert.Equal(t, float64(1), summary["failed"])

		// First result should succeed
		result1 := results[0].(map[string]interface{})
		assert.Equal(t, threat3ID, result1["threat_id"])
		assert.Equal(t, true, result1["success"])

		// Second result should fail
		result2 := results[1].(map[string]interface{})
		assert.Equal(t, "00000000-0000-0000-0000-999999999999", result2["threat_id"])
		assert.Equal(t, false, result2["success"])
		assert.Contains(t, result2, "error")

		// Verify threat3 was actually deleted
		verifyThreatNotInDatabase(suite, t, threat3ID)
	})

	// Test validation errors
	t.Run("ValidationErrors", func(t *testing.T) {
		// Test empty threat_ids array
		requestBody := map[string]interface{}{
			"threat_ids": []string{},
		}

		path := fmt.Sprintf("/threat_models/%s/threats/batch", suite.threatModelID)
		req := suite.makeAuthenticatedRequest("DELETE", path, requestBody)
		w := suite.executeRequest(req)

		suite.assertJSONResponse(t, w, http.StatusBadRequest)

		// Test too many threat IDs (over 50 limit)
		threatIDs := make([]string, 51)
		for i := 0; i < 51; i++ {
			threatIDs[i] = fmt.Sprintf("00000000-0000-0000-0000-%012d", i)
		}

		requestBody = map[string]interface{}{
			"threat_ids": threatIDs,
		}

		req = suite.makeAuthenticatedRequest("DELETE", path, requestBody)
		w = suite.executeRequest(req)

		suite.assertJSONResponse(t, w, http.StatusBadRequest)

		// Test invalid UUID format
		requestBody = map[string]interface{}{
			"threat_ids": []string{"invalid-uuid"},
		}

		req = suite.makeAuthenticatedRequest("DELETE", path, requestBody)
		w = suite.executeRequest(req)

		response := suite.assertJSONResponse(t, w, http.StatusBadRequest)

		results := response["results"].([]interface{})
		summary := response["summary"].(map[string]interface{})

		assert.Equal(t, float64(1), summary["total"])
		assert.Equal(t, float64(0), summary["succeeded"])
		assert.Equal(t, float64(1), summary["failed"])

		result1 := results[0].(map[string]interface{})
		assert.Equal(t, "invalid-uuid", result1["threat_id"])
		assert.Equal(t, false, result1["success"])
		assert.Contains(t, result1["error"], "Invalid threat ID format")
	})
}

// testBulkCreateThreats tests bulk creating multiple threats
func testBulkCreateThreats(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test data for bulk create
	requestBody := map[string]interface{}{
		"threats": []map[string]interface{}{
			{
				"name":        "Bulk Test Threat 1",
				"description": "First threat in bulk create test",
				"severity":    "high",
				"status":      "identified",
				"threat_type": "spoofing",
				"priority":    "high",
				"mitigated":   false,
			},
			{
				"name":        "Bulk Test Threat 2",
				"description": "Second threat in bulk create test",
				"severity":    "medium",
				"status":      "identified",
				"threat_type": "tampering",
				"priority":    "medium",
				"mitigated":   false,
			},
			{
				"name":        "Bulk Test Threat 3",
				"description": "Third threat in bulk create test",
				"severity":    "low",
				"status":      "identified",
				"threat_type": "repudiation",
				"priority":    "low",
				"mitigated":   false,
			},
		},
	}

	// Make request
	path := fmt.Sprintf("/threat_models/%s/threats/bulk", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusCreated)

	// Verify response
	createdThreats, ok := response["threats"].([]interface{})
	assert.True(t, ok, "Response should contain threats array")
	assert.Len(t, createdThreats, 3, "Should create 3 threats")

	// Verify each created threat
	for i, threatInterface := range createdThreats {
		threat := threatInterface.(map[string]interface{})
		originalThreat := requestBody["threats"].([]map[string]interface{})[i]

		assert.NotEmpty(t, threat["id"], "Each threat should have an ID")
		assert.Equal(t, originalThreat["name"], threat["name"])
		assert.Equal(t, originalThreat["description"], threat["description"])
		assert.Equal(t, originalThreat["severity"], threat["severity"])
		assert.Equal(t, originalThreat["status"], threat["status"])
		assert.Equal(t, originalThreat["threat_type"], threat["threat_type"])
		assert.Equal(t, originalThreat["priority"], threat["priority"])
		assert.Equal(t, originalThreat["mitigated"], threat["mitigated"])
		assert.Equal(t, suite.threatModelID, threat["threat_model_id"])
		assert.NotEmpty(t, threat["created_at"])
		assert.NotEmpty(t, threat["modified_at"])

		// Verify database persistence
		threatID := threat["id"].(string)
		verifyThreatInDatabase(suite, t, threatID, suite.threatModelID, map[string]interface{}{
			"name":        originalThreat["name"],
			"description": originalThreat["description"],
			"severity":    originalThreat["severity"],
			"status":      originalThreat["status"],
			"threat_type": originalThreat["threat_type"],
			"priority":    originalThreat["priority"],
			"mitigated":   originalThreat["mitigated"],
		})
	}
}

// testBulkUpdateThreats tests bulk updating multiple threats
func testBulkUpdateThreats(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// First create some threats to update
	threat1ID := suite.createTestThreat(t)
	threat2ID := suite.createTestThreat(t)

	// Test data for bulk update
	requestBody := map[string]interface{}{
		"threats": []map[string]interface{}{
			{
				"id":          threat1ID,
				"name":        "Bulk Updated Threat 1",
				"description": "First threat updated in bulk",
				"severity":    "critical",
				"status":      "mitigated",
				"threat_type": "spoofing",
				"priority":    "critical",
				"mitigated":   true,
				"mitigation":  "Updated mitigation for threat 1",
			},
			{
				"id":          threat2ID,
				"name":        "Bulk Updated Threat 2",
				"description": "Second threat updated in bulk",
				"severity":    "high",
				"status":      "in_progress",
				"threat_type": "tampering",
				"priority":    "high",
				"mitigated":   false,
				"mitigation":  "Updated mitigation for threat 2",
			},
		},
	}

	// Make request
	path := fmt.Sprintf("/threat_models/%s/threats/bulk", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("PUT", path, requestBody)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify response
	updatedThreats, ok := response["threats"].([]interface{})
	assert.True(t, ok, "Response should contain threats array")
	assert.Len(t, updatedThreats, 2, "Should update 2 threats")

	// Verify each updated threat
	for i, threatInterface := range updatedThreats {
		threat := threatInterface.(map[string]interface{})
		originalThreat := requestBody["threats"].([]map[string]interface{})[i]

		assert.Equal(t, originalThreat["id"], threat["id"])
		assert.Equal(t, originalThreat["name"], threat["name"])
		assert.Equal(t, originalThreat["description"], threat["description"])
		assert.Equal(t, originalThreat["severity"], threat["severity"])
		assert.Equal(t, originalThreat["status"], threat["status"])
		assert.Equal(t, originalThreat["mitigated"], threat["mitigated"])
		assert.Equal(t, originalThreat["mitigation"], threat["mitigation"])

		// Verify database persistence
		threatID := threat["id"].(string)
		verifyThreatInDatabase(suite, t, threatID, suite.threatModelID, map[string]interface{}{
			"name":        originalThreat["name"],
			"description": originalThreat["description"],
			"severity":    originalThreat["severity"],
			"status":      originalThreat["status"],
			"threat_type": originalThreat["threat_type"],
			"priority":    originalThreat["priority"],
			"mitigated":   originalThreat["mitigated"],
		})
	}
}

// testBulkCreateDocuments tests bulk creating multiple documents
func testBulkCreateDocuments(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test data for bulk document creation
	requestBody := map[string]interface{}{
		"documents": []map[string]interface{}{
			{
				"name":        "Bulk Test Document 1",
				"description": "First document in bulk create test",
				"url":         "https://example.com/doc1.pdf",
			},
			{
				"name":        "Bulk Test Document 2",
				"description": "Second document in bulk create test",
				"url":         "https://example.com/doc2.pdf",
			},
		},
	}

	// Make request
	path := fmt.Sprintf("/threat_models/%s/documents/bulk", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusCreated)

	// Verify response
	createdDocuments, ok := response["documents"].([]interface{})
	assert.True(t, ok, "Response should contain documents array")
	assert.Len(t, createdDocuments, 2, "Should create 2 documents")

	// Verify each created document
	for i, documentInterface := range createdDocuments {
		document := documentInterface.(map[string]interface{})
		originalDocument := requestBody["documents"].([]map[string]interface{})[i]

		assert.NotEmpty(t, document["id"], "Each document should have an ID")
		assert.Equal(t, originalDocument["name"], document["name"])
		assert.Equal(t, originalDocument["description"], document["description"])
		assert.Equal(t, originalDocument["url"], document["url"])
		assert.Equal(t, suite.threatModelID, document["threat_model_id"])
		assert.NotEmpty(t, document["created_at"])
		assert.NotEmpty(t, document["modified_at"])

		// Verify database persistence
		documentID := document["id"].(string)
		verifyDocumentInDatabase(suite, t, documentID, suite.threatModelID, map[string]interface{}{
			"name":        originalDocument["name"],
			"description": originalDocument["description"],
			"url":         originalDocument["url"],
		})
	}
}

// testBulkCreateSources tests bulk creating multiple sources
func testBulkCreateSources(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test data for bulk source creation
	requestBody := map[string]interface{}{
		"sources": []map[string]interface{}{
			{
				"name":        "Bulk Test Source 1",
				"description": "First source in bulk create test",
				"url":         "https://github.com/example/repo1",
				"type":        "git",
				"parameters": map[string]interface{}{
					"branch": "main",
					"path":   "/src",
				},
			},
			{
				"name":        "Bulk Test Source 2",
				"description": "Second source in bulk create test",
				"url":         "https://github.com/example/repo2",
				"type":        "git",
				"parameters": map[string]interface{}{
					"branch": "develop",
					"path":   "/api",
				},
			},
		},
	}

	// Make request
	path := fmt.Sprintf("/threat_models/%s/sources/bulk", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusCreated)

	// Verify response
	createdSources, ok := response["sources"].([]interface{})
	assert.True(t, ok, "Response should contain sources array")
	assert.Len(t, createdSources, 2, "Should create 2 sources")

	// Verify each created source
	for i, sourceInterface := range createdSources {
		source := sourceInterface.(map[string]interface{})
		originalSource := requestBody["sources"].([]map[string]interface{})[i]

		assert.NotEmpty(t, source["id"], "Each source should have an ID")
		assert.Equal(t, originalSource["name"], source["name"])
		assert.Equal(t, originalSource["description"], source["description"])
		assert.Equal(t, originalSource["url"], source["url"])
		assert.Equal(t, originalSource["type"], source["type"])
		assert.Equal(t, suite.threatModelID, source["threat_model_id"])
		assert.NotEmpty(t, source["created_at"])
		assert.NotEmpty(t, source["modified_at"])

		// Verify parameters if present
		if originalParams, exists := originalSource["parameters"]; exists {
			assert.Equal(t, originalParams, source["parameters"])
		}

		// Verify database persistence
		sourceID := source["id"].(string)
		verifySourceInDatabase(suite, t, sourceID, suite.threatModelID, map[string]interface{}{
			"name":        originalSource["name"],
			"description": originalSource["description"],
			"url":         originalSource["url"],
			"type":        originalSource["type"],
			"parameters":  originalSource["parameters"],
		})
	}
}
