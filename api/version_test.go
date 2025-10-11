package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApiInfoHandler_GetApiInfo(t *testing.T) {
	tests := []struct {
		name        string
		headers     map[string]string
		expectHTML  bool
		setupServer func() *Server
	}{
		{
			name:        "JSON response with server",
			headers:     map[string]string{"Accept": "application/json"},
			expectHTML:  false,
			setupServer: func() *Server { return NewServerForTests() },
		},
		{
			name:        "HTML response with server",
			headers:     map[string]string{"Accept": "text/html"},
			expectHTML:  true,
			setupServer: func() *Server { return NewServerForTests() },
		},
		{
			name:        "JSON response without server",
			headers:     map[string]string{"Accept": "application/json"},
			expectHTML:  false,
			setupServer: func() *Server { return nil },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			gin.SetMode(gin.TestMode)
			router := gin.New()

			// Create handler with or without server
			handler := NewApiInfoHandler(tt.setupServer())
			router.GET("/", handler.GetApiInfo)

			// Create request with specified headers
			req, err := http.NewRequest("GET", "/", nil)
			require.NoError(t, err)

			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			// Execute request
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Verify response
			assert.Equal(t, http.StatusOK, w.Code)

			if tt.expectHTML {
				// Check HTML response
				assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
				assert.Contains(t, w.Body.String(), "<title>TMI API Server</title>")
				assert.Contains(t, w.Body.String(), "api-info")
			} else {
				// Parse JSON response
				var apiInfo ApiInfo
				err = json.Unmarshal(w.Body.Bytes(), &apiInfo)
				require.NoError(t, err, "Response should be valid JSON")

				// Verify basic structure
				assert.Equal(t, "OK", string(apiInfo.Status.Code))
				assert.Equal(t, "TMI", apiInfo.Service.Name)
				assert.NotEmpty(t, apiInfo.Service.Build)

				// Verify API version follows semantic versioning format (e.g., 0.99.1, 1.0.0, 1.2.3-beta)
				semverPattern := regexp.MustCompile(`^\d+\.\d+\.\d+(-[a-zA-Z0-9.-]+)?$`)
				assert.Regexp(t, semverPattern, apiInfo.Api.Version, "API version should follow semantic versioning format")
				assert.NotEmpty(t, apiInfo.Api.Specification)

				// Note: WebSocket information is now documented separately in AsyncAPI spec
				// REST API info no longer includes WebSocket details as they use different protocols
			}
		})
	}
}

func TestApiInfoHandler_GetApiInfo_WithTLS(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Add middleware to simulate TLS context
	router.Use(func(c *gin.Context) {
		c.Set("tlsEnabled", true)
		c.Set("tlsSubjectName", "api.example.com")
		c.Set("serverPort", "443")
		c.Next()
	})

	server := NewServerForTests()
	handler := NewApiInfoHandler(server)
	router.GET("/", handler.GetApiInfo)

	// Create request
	req, err := http.NewRequest("GET", "/", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")
	req.Host = "api.example.com"

	// Execute request
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var apiInfo ApiInfo
	err = json.Unmarshal(w.Body.Bytes(), &apiInfo)
	require.NoError(t, err)

	// Note: WebSocket URLs are now documented in AsyncAPI specification
}

func TestApiInfoHandler_GetApiInfo_WithCustomPort(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Add middleware to simulate custom port
	router.Use(func(c *gin.Context) {
		c.Set("tlsEnabled", false)
		c.Set("serverPort", "8080")
		c.Next()
	})

	server := NewServerForTests()
	handler := NewApiInfoHandler(server)
	router.GET("/", handler.GetApiInfo)

	// Create request
	req, err := http.NewRequest("GET", "/", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")
	req.Host = "localhost:8080"

	// Execute request
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var apiInfo ApiInfo
	err = json.Unmarshal(w.Body.Bytes(), &apiInfo)
	require.NoError(t, err)

	// Note: WebSocket URLs are now documented in AsyncAPI specification
}
