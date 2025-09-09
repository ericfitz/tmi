package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/auth"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRFC9728EndpointIntegration tests the OAuth 2.0 Protected Resource Metadata endpoint (RFC 9728)
func TestRFC9728EndpointIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Create a simple auth config for testing
	config := auth.Config{
		OAuth: auth.OAuthConfig{
			CallbackURL: "http://localhost:8080/oauth2/callback",
		},
	}

	// Create auth handlers
	handlers := auth.NewHandlers(nil, config) // No service needed for this endpoint

	// Create router and register the endpoint
	router := gin.New()
	handlers.RegisterRoutes(router)

	t.Run("RFC9728_HTTP", func(t *testing.T) {
		// Test HTTP endpoint
		req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource", nil)
		req.Host = "localhost:8080"
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Assert response
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
		assert.Equal(t, "public, max-age=3600", w.Header().Get("Cache-Control"))

		// Parse response
		var metadata auth.OAuthProtectedResourceMetadata
		err := json.Unmarshal(w.Body.Bytes(), &metadata)
		require.NoError(t, err)

		// Validate RFC 9728 compliance
		assert.Equal(t, "http://localhost:8080", metadata.Resource) // Required field
		assert.Equal(t, []string{"openid", "profile", "email"}, metadata.ScopesSupported)
		assert.Equal(t, []string{"http://localhost:8080"}, metadata.AuthorizationServers)
		assert.Equal(t, "http://localhost:8080/.well-known/jwks.json", metadata.JWKSURI)
		assert.Equal(t, []string{"header"}, metadata.BearerMethodsSupported)
		assert.Equal(t, "TMI (Threat Modeling Improved) API", metadata.ResourceName)
		assert.Equal(t, "https://github.com/ericfitz/tmi", metadata.ResourceDocumentation)
		assert.False(t, metadata.TLSClientCertificateBoundAccessTokens)
	})

	t.Run("RFC9728_HTTPS", func(t *testing.T) {
		// Test HTTPS endpoint with proxy headers
		req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource", nil)
		req.Host = "api.example.com"
		req.Header.Set("X-Forwarded-Proto", "https")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Assert response
		assert.Equal(t, http.StatusOK, w.Code)

		// Parse response
		var metadata auth.OAuthProtectedResourceMetadata
		err := json.Unmarshal(w.Body.Bytes(), &metadata)
		require.NoError(t, err)

		// Validate HTTPS URLs
		assert.Equal(t, "https://api.example.com", metadata.Resource)
		assert.Equal(t, []string{"https://api.example.com"}, metadata.AuthorizationServers)
		assert.Equal(t, "https://api.example.com/.well-known/jwks.json", metadata.JWKSURI)
	})

	t.Run("RFC9728_JSONStructure", func(t *testing.T) {
		// Test that the JSON structure matches RFC 9728 exactly
		req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource", nil)
		req.Host = "localhost:8080"
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Assert response
		assert.Equal(t, http.StatusOK, w.Code)

		// Parse as generic map to check structure
		var jsonResponse map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &jsonResponse)
		require.NoError(t, err)

		// Check that all RFC 9728 fields are present (whether required or optional)
		expectedFields := []string{
			"resource",                 // required
			"scopes_supported",         // recommended
			"authorization_servers",    // optional
			"jwks_uri",                 // optional
			"bearer_methods_supported", // optional
			"resource_name",            // optional
			"resource_documentation",   // optional
			"tls_client_certificate_bound_access_tokens", // optional
		}

		for _, field := range expectedFields {
			assert.Contains(t, jsonResponse, field, "RFC 9728 field %s should be present", field)
		}

		// Validate the required field
		assert.NotEmpty(t, jsonResponse["resource"], "resource field is required by RFC 9728")

		// Validate that arrays are properly formatted
		if scopes, ok := jsonResponse["scopes_supported"]; ok {
			scopesArray, ok := scopes.([]interface{})
			assert.True(t, ok, "scopes_supported should be an array")
			assert.Greater(t, len(scopesArray), 0, "scopes_supported should not be empty")
		}
	})

	t.Run("RFC9728_HTTPHeaders", func(t *testing.T) {
		// Test HTTP headers compliance
		req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource", nil)
		req.Host = "localhost:8080"
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Check caching headers as per discovery endpoint best practices
		assert.Equal(t, "public, max-age=3600", w.Header().Get("Cache-Control"))
		assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	})
}

// TestRFC9728WithAPIServer tests the endpoint in the context of the OpenAPI-generated server
func TestRFC9728WithAPIServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Create minimal server setup (without database dependencies)
	server := NewServerForTests()

	// Create simple auth config and handlers
	config := auth.Config{
		OAuth: auth.OAuthConfig{
			CallbackURL: "http://localhost:8080/oauth2/callback",
		},
	}
	handlers := auth.NewHandlers(nil, config)
	authAdapter := NewAuthServiceAdapter(handlers)
	server.SetAuthService(authAdapter)

	// Create router and register only OpenAPI routes (this will include the RFC 9728 endpoint)
	router := gin.New()
	RegisterHandlers(router, server)

	t.Run("ViaOpenAPI", func(t *testing.T) {
		// Test the endpoint via the OpenAPI-generated route
		req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource", nil)
		req.Host = "localhost:8080"
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Verify the response
		assert.Equal(t, http.StatusOK, w.Code)

		// Parse response to ensure it's valid JSON
		var metadata auth.OAuthProtectedResourceMetadata
		err := json.Unmarshal(w.Body.Bytes(), &metadata)
		require.NoError(t, err)

		// Validate core RFC 9728 compliance
		assert.Equal(t, "http://localhost:8080", metadata.Resource)
		assert.NotEmpty(t, metadata.ScopesSupported)
	})
}
