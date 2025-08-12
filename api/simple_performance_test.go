package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// BenchmarkSubResourceRoutePerformance benchmarks basic route performance for sub-resources
func BenchmarkSubResourceRoutePerformance(b *testing.B) {
	// Initialize test fixtures
	InitTestFixtures()
	ResetStores()
	InitTestFixtures()

	// Setup Gin router in test mode
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Register basic routes for performance testing
	router.GET("/threat_models/:threat_model_id", func(c *gin.Context) {
		// Mock response for performance testing
		c.JSON(200, gin.H{
			"id":          c.Param("threat_model_id"),
			"name":        "Test Threat Model",
			"description": "Performance test threat model",
		})
	})

	router.GET("/threat_models/:threat_model_id/basic", func(c *gin.Context) {
		// Simple response for overhead measurement
		c.JSON(200, gin.H{"status": "ok", "id": c.Param("threat_model_id")})
	})

	threatModelID := TestFixtures.ThreatModelID

	b.ResetTimer()

	b.Run("BasicRoute", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", fmt.Sprintf("/threat_models/%s/basic", threatModelID), nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
		}
	})

	b.Run("ThreatModelRoute", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", fmt.Sprintf("/threat_models/%s", threatModelID), nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
		}
	})
}

// BenchmarkJSONSerialization benchmarks JSON serialization performance for sub-resources
func BenchmarkJSONSerialization(b *testing.B) {
	// Sample threat data for serialization benchmarking
	threat := map[string]interface{}{
		"id":              "123e4567-e89b-12d3-a456-426614174000",
		"name":            "Sample Threat",
		"description":     "A sample threat for performance testing",
		"mitigation":      "Standard mitigation procedures",
		"threat_model_id": "550e8400-e29b-41d4-a716-446655440000",
		"created_at":      "2024-01-01T00:00:00Z",
		"modified_at":     "2024-01-01T00:00:00Z",
	}

	// Sample document data
	document := map[string]interface{}{
		"id":          "123e4567-e89b-12d3-a456-426614174001",
		"name":        "Sample Document",
		"description": "A sample document for performance testing",
		"url":         "https://example.com/document",
	}

	// Sample source data
	source := map[string]interface{}{
		"id":          "123e4567-e89b-12d3-a456-426614174002",
		"name":        "Sample Source",
		"description": "A sample source for performance testing",
		"url":         "https://github.com/example/repo",
	}

	b.ResetTimer()

	b.Run("ThreatSerialization", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = json.Marshal(threat)
		}
	})

	b.Run("DocumentSerialization", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = json.Marshal(document)
		}
	})

	b.Run("SourceSerialization", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = json.Marshal(source)
		}
	})

	b.Run("ThreatDeserialization", func(b *testing.B) {
		data, _ := json.Marshal(threat)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var result map[string]interface{}
			_ = json.Unmarshal(data, &result)
		}
	})
}

// BenchmarkBulkOperations benchmarks bulk operation performance
func BenchmarkBulkOperations(b *testing.B) {
	b.Run("BulkJSONGeneration", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Generate 100 threats for bulk operations
			threats := make([]map[string]interface{}, 100)
			for j := 0; j < 100; j++ {
				threats[j] = map[string]interface{}{
					"id":          fmt.Sprintf("threat-%d-%d", i, j),
					"name":        fmt.Sprintf("Bulk Threat %d-%d", i, j),
					"description": "Generated for bulk performance testing",
				}
			}
			_, _ = json.Marshal(threats)
		}
	})

	b.Run("BulkJSONParsing", func(b *testing.B) {
		// Pre-generate bulk data
		threats := make([]map[string]interface{}, 100)
		for j := 0; j < 100; j++ {
			threats[j] = map[string]interface{}{
				"id":          fmt.Sprintf("parse-threat-%d", j),
				"name":        fmt.Sprintf("Parse Threat %d", j),
				"description": "Generated for bulk parsing testing",
			}
		}
		data, _ := json.Marshal(threats)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var result []map[string]interface{}
			_ = json.Unmarshal(data, &result)
		}
	})
}

// BenchmarkPaginationLogic benchmarks pagination calculation overhead
func BenchmarkPaginationLogic(b *testing.B) {
	b.Run("PaginationCalculation", func(b *testing.B) {
		totalItems := 10000
		pageSize := 20

		for i := 0; i < b.N; i++ {
			offset := (i % 500) * pageSize // Simulate different page requests

			// Simulate pagination logic
			startIdx := offset
			endIdx := offset + pageSize
			if endIdx > totalItems {
				endIdx = totalItems
			}

			// Calculate page info
			currentPage := offset/pageSize + 1
			totalPages := (totalItems + pageSize - 1) / pageSize
			hasNext := currentPage < totalPages
			hasPrev := currentPage > 1

			// Use calculated values to prevent optimization
			_ = startIdx + endIdx + currentPage + totalPages
			_ = hasNext && hasPrev
		}
	})
}

// BenchmarkURLPatternMatching benchmarks URL pattern matching for sub-resources
func BenchmarkURLPatternMatching(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Register complex nested routes similar to our sub-resource patterns
	router.GET("/threat_models/:threat_model_id/threats", func(c *gin.Context) {
		c.JSON(200, gin.H{"threats": []interface{}{}})
	})
	router.GET("/threat_models/:threat_model_id/threats/:threat_id", func(c *gin.Context) {
		c.JSON(200, gin.H{"threat": gin.H{"id": c.Param("threat_id")}})
	})
	router.GET("/threat_models/:threat_model_id/threats/:threat_id/metadata", func(c *gin.Context) {
		c.JSON(200, gin.H{"metadata": gin.H{}})
	})
	router.GET("/threat_models/:threat_model_id/documents", func(c *gin.Context) {
		c.JSON(200, gin.H{"documents": []interface{}{}})
	})
	router.GET("/threat_models/:threat_model_id/sources", func(c *gin.Context) {
		c.JSON(200, gin.H{"sources": []interface{}{}})
	})

	threatModelID := "550e8400-e29b-41d4-a716-446655440000"
	threatID := "123e4567-e89b-12d3-a456-426614174000"

	b.ResetTimer()

	b.Run("SimpleRoute", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", fmt.Sprintf("/threat_models/%s/threats", threatModelID), nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
		}
	})

	b.Run("NestedRoute", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", fmt.Sprintf("/threat_models/%s/threats/%s", threatModelID, threatID), nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
		}
	})

	b.Run("DeepNestedRoute", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", fmt.Sprintf("/threat_models/%s/threats/%s/metadata", threatModelID, threatID), nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
		}
	})
}
