package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApiInfoHandler_GetApiInfo(t *testing.T) {
	tests := []struct {
		name         string
		headers      map[string]string
		expectHTML   bool
		expectStatus ApiInfoStatusCode
		setupServer  func() *Server
	}{
		{
			name:         "JSON response with server",
			headers:      map[string]string{"Accept": "application/json"},
			expectHTML:   false,
			expectStatus: DEGRADED, // No database manager in test = DEGRADED status
			setupServer:  func() *Server { return NewServerForTests() },
		},
		{
			name:         "HTML response with server",
			headers:      map[string]string{"Accept": "text/html"},
			expectHTML:   true,
			expectStatus: DEGRADED, // Not checked for HTML, but included for consistency
			setupServer:  func() *Server { return NewServerForTests() },
		},
		{
			name:         "JSON response without server",
			headers:      map[string]string{"Accept": "application/json"},
			expectHTML:   false,
			expectStatus: DEGRADED, // No database manager in test = DEGRADED status
			setupServer:  func() *Server { return nil },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Ensure no global manager is set for these tests
			db.SetGlobalManager(nil)

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

				// Verify status code reflects health check result
				// Without a database manager, status should be DEGRADED
				assert.Equal(t, tt.expectStatus, apiInfo.Status.Code,
					"Status should be %s when database manager is not configured", tt.expectStatus)

				// When degraded, health details should be included
				if tt.expectStatus == DEGRADED {
					assert.NotNil(t, apiInfo.Health, "Health details should be included when status is DEGRADED")
				}

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

func TestGetVersion_PreRelease(t *testing.T) {
	// Save and restore original value
	original := VersionPreRelease
	defer func() { VersionPreRelease = original }()

	t.Run("with pre-release label", func(t *testing.T) {
		VersionPreRelease = "rc.0"
		v := GetVersion()
		assert.Equal(t, "rc.0", v.PreRelease)
	})

	t.Run("without pre-release label", func(t *testing.T) {
		VersionPreRelease = ""
		v := GetVersion()
		assert.Empty(t, v.PreRelease)
	})
}

func TestGetVersionString_WithPreRelease(t *testing.T) {
	// Save and restore original values
	origMajor, origMinor, origPatch, origPreRelease := VersionMajor, VersionMinor, VersionPatch, VersionPreRelease
	origCommit, origDate := GitCommit, BuildDate
	defer func() {
		VersionMajor, VersionMinor, VersionPatch, VersionPreRelease = origMajor, origMinor, origPatch, origPreRelease
		GitCommit, BuildDate = origCommit, origDate
	}()

	VersionMajor = "1"
	VersionMinor = "2"
	VersionPatch = "0"
	GitCommit = "abc1234"
	BuildDate = "2026-01-01T00:00:00Z"

	t.Run("with pre-release", func(t *testing.T) {
		VersionPreRelease = "rc.0"
		result := GetVersionString()
		assert.Equal(t, "tmi 1.2.0-rc.0 (abc1234 - built 2026-01-01T00:00:00Z)", result)
	})

	t.Run("without pre-release", func(t *testing.T) {
		VersionPreRelease = ""
		result := GetVersionString()
		assert.Equal(t, "tmi 1.2.0 (abc1234 - built 2026-01-01T00:00:00Z)", result)
	})
}

func TestGetApiInfo_BuildString_PreRelease(t *testing.T) {
	// Save and restore original values
	origPreRelease := VersionPreRelease
	defer func() { VersionPreRelease = origPreRelease }()

	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		preRelease   string
		buildPattern string
	}{
		{
			name:         "pre-release build string uses semver format",
			preRelease:   "rc.0",
			buildPattern: `^\d+\.\d+\.\d+-rc\.0\+.+$`,
		},
		{
			name:         "stable build string uses legacy format",
			preRelease:   "",
			buildPattern: `^\d+\.\d+\.\d+-.+$`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			VersionPreRelease = tt.preRelease

			router := gin.New()
			handler := NewApiInfoHandler(NewServerForTests())
			router.GET("/", handler.GetApiInfo)

			req, err := http.NewRequest("GET", "/", nil)
			require.NoError(t, err)
			req.Header.Set("Accept", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var apiInfo ApiInfo
			err = json.Unmarshal(w.Body.Bytes(), &apiInfo)
			require.NoError(t, err)

			pattern := regexp.MustCompile(tt.buildPattern)
			assert.Regexp(t, pattern, apiInfo.Service.Build,
				"Build string %q should match pattern %s", apiInfo.Service.Build, tt.buildPattern)
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
