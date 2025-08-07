package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		expectWS    bool
		setupServer func() *Server
	}{
		{
			name:        "JSON response with WebSocket info",
			headers:     map[string]string{"Accept": "application/json"},
			expectHTML:  false,
			expectWS:    true,
			setupServer: func() *Server { return NewServer() },
		},
		{
			name:        "HTML response with WebSocket info",
			headers:     map[string]string{"Accept": "text/html"},
			expectHTML:  true,
			expectWS:    true,
			setupServer: func() *Server { return NewServer() },
		},
		{
			name:        "JSON response without server (no WebSocket info)",
			headers:     map[string]string{"Accept": "application/json"},
			expectHTML:  false,
			expectWS:    false,
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
				assert.Equal(t, "1.0.0", apiInfo.Api.Version)
				assert.NotEmpty(t, apiInfo.Api.Specification)

				// Check WebSocket info presence
				if tt.expectWS {
					assert.NotEmpty(t, apiInfo.Websocket.BaseUrl, "WebSocket base URL should be present")
					assert.Equal(t, "/ws/diagrams/{diagram_id}", apiInfo.Websocket.DiagramEndpoint)
					assert.Contains(t, apiInfo.Websocket.BaseUrl, "ws://")
				} else {
					assert.Empty(t, apiInfo.Websocket.BaseUrl, "WebSocket base URL should be empty when no server")
					assert.Empty(t, apiInfo.Websocket.DiagramEndpoint, "WebSocket diagram endpoint should be empty when no server")
				}
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

	server := NewServer()
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

	// Verify HTTPS WebSocket URL
	assert.Equal(t, "wss://api.example.com/ws", apiInfo.Websocket.BaseUrl)
	assert.Equal(t, "/ws/diagrams/{diagram_id}", apiInfo.Websocket.DiagramEndpoint)
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

	server := NewServer()
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

	// Verify WebSocket URL with custom port
	assert.Equal(t, "ws://localhost:8080/ws", apiInfo.Websocket.BaseUrl)
	assert.Equal(t, "/ws/diagrams/{diagram_id}", apiInfo.Websocket.DiagramEndpoint)
}
