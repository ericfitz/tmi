//go:build integration
// +build integration

package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func TestOpenTelemetryIntegration(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration tests")
	}

	// Create test configuration
	config := &Config{
		ServiceName:       "tmi-test",
		ServiceVersion:    "1.0.0-test",
		Environment:       "test",
		TracingEnabled:    true,
		TracingSampleRate: 1.0,
		MetricsEnabled:    true,
		MetricsInterval:   1 * time.Second,
		ConsoleExporter:   true,
		IsDevelopment:     true,
	}

	// Initialize telemetry
	err := Initialize(config)
	require.NoError(t, err)

	// Ensure cleanup
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		Shutdown(ctx)
	}()

	// Verify service is initialized
	service := GetService()
	require.NotNil(t, service)

	// Test tracer
	tracer := GetTracer()
	require.NotNil(t, tracer)

	// Test meter
	meter := GetMeter()
	require.NotNil(t, meter)

	// Test creating a span
	ctx, span := tracer.Start(context.Background(), "test.operation")
	span.End()

	// Verify span context
	spanContext := span.SpanContext()
	assert.True(t, spanContext.TraceID().IsValid())
	assert.True(t, spanContext.SpanID().IsValid())

	// Test HTTP middleware integration
	gin.SetMode(gin.TestMode)
	router := gin.New()

	httpTracing, err := NewHTTPTracing(tracer, meter)
	require.NoError(t, err)

	router.Use(httpTracing.TracingLoggerMiddleware())
	router.GET("/test", func(c *gin.Context) {
		// Verify trace context is available
		span := SpanFromContext(c.Request.Context())
		assert.True(t, span.SpanContext().IsValid())

		// Add some attributes
		AddSpanAttributes(c.Request.Context(),
			SpanAttribute("test.key", "test.value"),
		)

		c.JSON(200, gin.H{"message": "test"})
	})

	// Make test request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	// Test service health
	health := service.Health()
	assert.True(t, health.Healthy)
	assert.Equal(t, "tmi-test", health.Details["service_name"])
}

func TestDatabaseTracingIntegration(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration tests")
	}

	// This test would require a database connection
	// For now, just test the interface creation
	service := GetService()
	if service == nil {
		t.Skip("Telemetry service not initialized")
	}

	dbTracing, err := NewDatabaseTracing(service.GetTracer(), service.GetMeter())
	require.NoError(t, err)
	assert.NotNil(t, dbTracing)
}

func TestRedisTracingIntegration(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration tests")
	}

	service := GetService()
	if service == nil {
		t.Skip("Telemetry service not initialized")
	}

	redisTracing, err := NewRedisTracing(service.GetTracer(), service.GetMeter())
	require.NoError(t, err)
	assert.NotNil(t, redisTracing)
}

// Helper function for span attributes (compatibility)
func SpanAttribute(key, value string) interface{} {
	// This would be replaced with actual attribute creation
	return struct {
		Key   string
		Value string
	}{Key: key, Value: value}
}

// IntegrationTestSuite provides comprehensive end-to-end testing for OpenTelemetry integration
type IntegrationTestSuite struct {
	service    *Service
	httpServer *httptest.Server
	mockDB     *sql.DB
	mockRedis  *redis.Client
	testConfig *Config
	cleanup    []func() error
	mu         sync.RWMutex
}

// NewIntegrationTestSuite creates a new integration test suite
func NewIntegrationTestSuite(t *testing.T) *IntegrationTestSuite {
	suite := &IntegrationTestSuite{
		testConfig: &Config{
			ServiceName:       "tmi-api-test",
			ServiceVersion:    "test-1.0.0",
			Environment:       "test",
			TracingEnabled:    true,
			TracingSampleRate: 1.0,
			MetricsEnabled:    true,
			MetricsInterval:   time.Second,
			ConsoleExporter:   true,
			IsDevelopment:     true,
		},
		cleanup: make([]func() error, 0),
	}

	// Initialize test environment
	if err := suite.setup(t); err != nil {
		t.Fatalf("Failed to setup integration test suite: %v", err)
	}

	return suite
}

// setup initializes the test environment
func (suite *IntegrationTestSuite) setup(t *testing.T) error {
	// Initialize telemetry service
	err := Initialize(suite.testConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize telemetry: %w", err)
	}

	suite.service = GetService()
	if suite.service == nil {
		return fmt.Errorf("telemetry service not initialized")
	}

	// Setup mock HTTP server
	if err := suite.setupHTTPServer(); err != nil {
		return fmt.Errorf("failed to setup HTTP server: %w", err)
	}

	// Setup mock database (using in-memory SQLite for testing)
	if err := suite.setupMockDatabase(); err != nil {
		return fmt.Errorf("failed to setup mock database: %w", err)
	}

	// Add cleanup for service shutdown
	suite.cleanup = append(suite.cleanup, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return Shutdown(ctx)
	})

	return nil
}

// setupHTTPServer creates a test HTTP server with instrumentation
func (suite *IntegrationTestSuite) setupHTTPServer() error {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Add HTTP tracing middleware
	httpTracing, err := NewHTTPTracing(suite.service.GetTracer(), suite.service.GetMeter())
	if err != nil {
		return fmt.Errorf("failed to create HTTP tracing: %w", err)
	}

	router.Use(httpTracing.TracingLoggerMiddleware())

	// Test endpoints
	router.GET("/", suite.healthHandler)
	router.GET("/api/v1/threat-models", suite.listThreatModelsHandler)
	router.POST("/api/v1/threat-models", suite.createThreatModelHandler)
	router.GET("/api/v1/threat-models/:id", suite.getThreatModelHandler)
	router.PUT("/api/v1/threat-models/:id", suite.updateThreatModelHandler)
	router.DELETE("/api/v1/threat-models/:id", suite.deleteThreatModelHandler)
	router.GET("/api/v1/diagrams/:id/cells", suite.getDiagramCellsHandler)
	router.POST("/error", suite.errorHandler)

	suite.httpServer = httptest.NewServer(router)
	suite.cleanup = append(suite.cleanup, func() error {
		suite.httpServer.Close()
		return nil
	})

	return nil
}

// setupMockDatabase creates an in-memory SQLite database for testing
func (suite *IntegrationTestSuite) setupMockDatabase() error {
	// Use in-memory SQLite for testing
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return fmt.Errorf("failed to open test database: %w", err)
	}

	// Create test tables
	schema := `
		CREATE TABLE threat_models (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			modified_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		
		CREATE TABLE diagrams (
			id TEXT PRIMARY KEY,
			threat_model_id TEXT,
			name TEXT NOT NULL,
			data TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			modified_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (threat_model_id) REFERENCES threat_models(id)
		);
		
		CREATE TABLE diagram_cells (
			id TEXT PRIMARY KEY,
			diagram_id TEXT,
			cell_type TEXT,
			position_x INTEGER,
			position_y INTEGER,
			data TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			modified_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (diagram_id) REFERENCES diagrams(id)
		);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create test schema: %w", err)
	}

	suite.mockDB = db
	suite.cleanup = append(suite.cleanup, func() error {
		return db.Close()
	})

	return nil
}

// Test HTTP request handlers with instrumentation

func (suite *IntegrationTestSuite) healthHandler(c *gin.Context) {
	ctx := c.Request.Context()

	// Create span for health check
	_, span := suite.service.GetTracer().Start(ctx, "health_check")
	defer span.End()

	span.SetAttributes(
		attribute.String("health.status", "ok"),
		attribute.String("service.name", "tmi-api"),
	)

	c.JSON(http.StatusOK, gin.H{"status": "ok", "timestamp": time.Now()})
}

func (suite *IntegrationTestSuite) listThreatModelsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	_, span := suite.service.GetTracer().Start(ctx, "list_threat_models")
	defer span.End()

	// Simulate database query
	suite.simulateDatabaseQuery(ctx, "SELECT", "threat_models", 100*time.Millisecond, 5, 0, nil)

	span.SetAttributes(
		attribute.Int("threat_models.count", 5),
		attribute.String("query.type", "list"),
	)

	c.JSON(http.StatusOK, gin.H{
		"threat_models": []gin.H{
			{"id": "tm1", "name": "Web App Security", "description": "Security model for web application"},
			{"id": "tm2", "name": "API Security", "description": "Security model for REST API"},
			{"id": "tm3", "name": "Database Security", "description": "Security model for database layer"},
		},
	})
}

func (suite *IntegrationTestSuite) createThreatModelHandler(c *gin.Context) {
	ctx := c.Request.Context()

	_, span := suite.service.GetTracer().Start(ctx, "create_threat_model")
	defer span.End()

	// Simulate database insertion
	suite.simulateDatabaseQuery(ctx, "INSERT", "threat_models", 50*time.Millisecond, 1, 0, nil)

	span.SetAttributes(
		attribute.String("threat_model.id", "tm_new_123"),
		attribute.String("operation.type", "create"),
	)

	c.JSON(http.StatusCreated, gin.H{
		"id":     "tm_new_123",
		"name":   "New Threat Model",
		"status": "created",
	})
}

func (suite *IntegrationTestSuite) getThreatModelHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	_, span := suite.service.GetTracer().Start(ctx, "get_threat_model")
	defer span.End()

	// Simulate database query
	suite.simulateDatabaseQuery(ctx, "SELECT", "threat_models", 75*time.Millisecond, 1, 0, nil)

	span.SetAttributes(
		attribute.String("threat_model.id", id),
	)

	c.JSON(http.StatusOK, gin.H{
		"id":          id,
		"name":        "Sample Threat Model",
		"description": "A sample threat model for testing",
	})
}

func (suite *IntegrationTestSuite) updateThreatModelHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	_, span := suite.service.GetTracer().Start(ctx, "update_threat_model")
	defer span.End()

	// Simulate database update
	suite.simulateDatabaseQuery(ctx, "UPDATE", "threat_models", 80*time.Millisecond, 1, 0, nil)

	span.SetAttributes(
		attribute.String("threat_model.id", id),
		attribute.String("operation.type", "update"),
	)

	c.JSON(http.StatusOK, gin.H{
		"id":     id,
		"status": "updated",
	})
}

func (suite *IntegrationTestSuite) deleteThreatModelHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	_, span := suite.service.GetTracer().Start(ctx, "delete_threat_model")
	defer span.End()

	// Simulate database deletion
	suite.simulateDatabaseQuery(ctx, "DELETE", "threat_models", 60*time.Millisecond, 1, 0, nil)

	span.SetAttributes(
		attribute.String("threat_model.id", id),
		attribute.String("operation.type", "delete"),
	)

	c.JSON(http.StatusOK, gin.H{
		"id":     id,
		"status": "deleted",
	})
}

func (suite *IntegrationTestSuite) getDiagramCellsHandler(c *gin.Context) {
	ctx := c.Request.Context()
	diagramID := c.Param("id")

	_, span := suite.service.GetTracer().Start(ctx, "get_diagram_cells")
	defer span.End()

	// Simulate complex query with joins
	suite.simulateDatabaseQuery(ctx, "SELECT", "diagram_cells", 150*time.Millisecond, 25, 0, nil)

	span.SetAttributes(
		attribute.String("diagram.id", diagramID),
		attribute.Int("cells.count", 25),
	)

	c.JSON(http.StatusOK, gin.H{
		"diagram_id": diagramID,
		"cells":      make([]gin.H, 25), // Empty cells for testing
		"count":      25,
	})
}

func (suite *IntegrationTestSuite) errorHandler(c *gin.Context) {
	ctx := c.Request.Context()

	_, span := suite.service.GetTracer().Start(ctx, "simulate_error")
	defer span.End()

	// Simulate an error condition
	err := fmt.Errorf("simulated error for testing")
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())

	c.JSON(http.StatusInternalServerError, gin.H{
		"error": "Simulated error for testing",
		"code":  "SIMULATION_ERROR",
	})
}

// Simulation helpers

func (suite *IntegrationTestSuite) simulateDatabaseQuery(ctx context.Context, operation, table string, duration time.Duration, rowsReturned, rowsAffected int64, err error) {
	dbMetrics := GetDatabaseMetrics()
	if dbMetrics == nil {
		return
	}

	queryMetrics := &QueryMetrics{
		StartTime:    time.Now().Add(-duration),
		Operation:    operation,
		Table:        table,
		Query:        fmt.Sprintf("%s FROM %s", operation, table),
		RowsReturned: rowsReturned,
		RowsAffected: rowsAffected,
		IsSlowQuery:  duration > 100*time.Millisecond,
	}

	dbMetrics.RecordQuery(ctx, queryMetrics, err)
}

// Test execution methods

// RunComprehensiveTest runs a comprehensive integration test covering all observability features
func (suite *IntegrationTestSuite) RunComprehensiveTest(t *testing.T) {
	// Test HTTP endpoints with various patterns
	suite.testHTTPEndpoints(t)

	// Test database operations
	suite.testDatabaseOperations(t, context.Background())

	// Test error scenarios
	suite.testErrorScenarios(t)

	// Test concurrent operations
	suite.testConcurrentOperations(t)

	// Test performance under load
	suite.testPerformanceUnderLoad(t)

	// Validate collected metrics and traces
	suite.validateObservabilityData(t)
}

func (suite *IntegrationTestSuite) testHTTPEndpoints(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	baseURL := suite.httpServer.URL

	// Test various HTTP operations
	testCases := []struct {
		method   string
		path     string
		expected int
	}{
		{"GET", "/", http.StatusOK},
		{"GET", "/api/v1/threat-models", http.StatusOK},
		{"POST", "/api/v1/threat-models", http.StatusCreated},
		{"GET", "/api/v1/threat-models/tm123", http.StatusOK},
		{"PUT", "/api/v1/threat-models/tm123", http.StatusOK},
		{"GET", "/api/v1/diagrams/diag456/cells", http.StatusOK},
		{"DELETE", "/api/v1/threat-models/tm123", http.StatusOK},
		{"POST", "/error", http.StatusInternalServerError},
	}

	for _, tc := range testCases {
		req, err := http.NewRequest(tc.method, baseURL+tc.path, nil)
		if err != nil {
			t.Errorf("Failed to create request: %v", err)
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("Request failed: %v", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode != tc.expected {
			t.Errorf("Expected status %d for %s %s, got %d", tc.expected, tc.method, tc.path, resp.StatusCode)
		}
	}
}

func (suite *IntegrationTestSuite) testDatabaseOperations(t *testing.T, ctx context.Context) {
	if suite.mockDB == nil {
		t.Skip("Database not available for testing")
		return
	}

	// Test various database operations
	operations := []struct {
		operation string
		query     string
	}{
		{"INSERT", "INSERT INTO threat_models (id, name) VALUES (?, ?)"},
		{"SELECT", "SELECT * FROM threat_models WHERE id = ?"},
		{"UPDATE", "UPDATE threat_models SET name = ? WHERE id = ?"},
		{"DELETE", "DELETE FROM threat_models WHERE id = ?"},
	}

	for _, op := range operations {
		start := time.Now()

		// Execute the operation (simplified for testing)
		switch op.operation {
		case "INSERT":
			_, err := suite.mockDB.Exec(op.query, "test_id", "Test Model")
			if err != nil {
				t.Errorf("Database INSERT failed: %v", err)
			}
		case "SELECT":
			rows, err := suite.mockDB.Query("SELECT * FROM threat_models")
			if err != nil {
				t.Errorf("Database SELECT failed: %v", err)
			} else {
				rows.Close()
			}
		case "UPDATE":
			_, err := suite.mockDB.Exec("UPDATE threat_models SET name = ? WHERE id = ?", "Updated Model", "test_id")
			if err != nil {
				t.Errorf("Database UPDATE failed: %v", err)
			}
		case "DELETE":
			_, err := suite.mockDB.Exec("DELETE FROM threat_models WHERE id = ?", "test_id")
			if err != nil {
				t.Errorf("Database DELETE failed: %v", err)
			}
		}

		// Record metrics
		suite.simulateDatabaseQuery(ctx, op.operation, "threat_models", time.Since(start), 1, 1, nil)
	}
}

func (suite *IntegrationTestSuite) testErrorScenarios(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Test error endpoint
	resp, err := client.Post(suite.httpServer.URL+"/error", "application/json", nil)
	if err != nil {
		t.Errorf("Error request failed: %v", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected error status 500, got %d", resp.StatusCode)
	}
}

func (suite *IntegrationTestSuite) testConcurrentOperations(t *testing.T) {
	const numConcurrent = 10
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 10 * time.Second}

	// Test concurrent HTTP requests
	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			resp, err := client.Get(fmt.Sprintf("%s/api/v1/threat-models/concurrent_%d", suite.httpServer.URL, id))
			if err != nil {
				t.Errorf("Concurrent request %d failed: %v", id, err)
				return
			}
			resp.Body.Close()
		}(i)
	}

	wg.Wait()
}

func (suite *IntegrationTestSuite) testPerformanceUnderLoad(t *testing.T) {
	const requestCount = 100
	const concurrency = 10

	client := &http.Client{Timeout: 10 * time.Second}
	semaphore := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			resp, err := client.Get(fmt.Sprintf("%s/api/v1/threat-models/%d", suite.httpServer.URL, id))
			if err != nil {
				t.Errorf("Load test request %d failed: %v", id, err)
				return
			}
			resp.Body.Close()
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	rps := float64(requestCount) / duration.Seconds()
	t.Logf("Load test completed: %d requests in %v (%.2f RPS)", requestCount, duration, rps)

	if rps < 50 {
		t.Errorf("Performance below threshold: %.2f RPS (expected > 50)", rps)
	}
}

func (suite *IntegrationTestSuite) validateObservabilityData(t *testing.T) {
	// Wait a moment for async operations to complete
	time.Sleep(2 * time.Second)

	// Validate that the service is healthy
	health := suite.service.Health()
	assert.True(t, health.Healthy, "Service should be healthy")
	assert.Equal(t, "tmi-api-test", health.Details["service_name"])

	// Validate tracers and meters are available
	tracer := suite.service.GetTracer()
	assert.NotNil(t, tracer, "Tracer should be available")

	meter := suite.service.GetMeter()
	assert.NotNil(t, meter, "Meter should be available")

	t.Log("Observability data validation completed")
}

// Cleanup cleans up test resources
func (suite *IntegrationTestSuite) Cleanup() error {
	var errors []error

	for _, cleanup := range suite.cleanup {
		if err := cleanup(); err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %v", errors)
	}

	return nil
}

// TestComprehensiveIntegration is the main test function that runs the complete integration test
func TestComprehensiveIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping comprehensive integration test in short mode")
	}

	// Skip if running in CI without proper setup
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Integration tests require INTEGRATION_TESTS=true")
	}

	suite := NewIntegrationTestSuite(t)
	defer func() {
		if err := suite.Cleanup(); err != nil {
			t.Errorf("Cleanup failed: %v", err)
		}
	}()

	suite.RunComprehensiveTest(t)
}

// Benchmark tests for performance validation

func BenchmarkHTTPRequestWithTelemetry(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	suite := NewIntegrationTestSuite(&testing.T{})
	defer suite.Cleanup()

	client := &http.Client{Timeout: 5 * time.Second}
	url := suite.httpServer.URL + "/"

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := client.Get(url)
			if err != nil {
				b.Errorf("Request failed: %v", err)
				continue
			}
			resp.Body.Close()
		}
	})
}
