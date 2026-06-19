package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// MiddlewareTestHelper provides utilities for testing middleware functions
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: test fixture holding a Gin engine for middleware unit tests (pure)
type MiddlewareTestHelper struct {
	Router *gin.Engine
}

// NewMiddlewareTestHelper creates a new middleware test helper with a clean router
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: build a MiddlewareTestHelper with a fresh Gin engine in test mode (pure)
func NewMiddlewareTestHelper() *MiddlewareTestHelper {
	gin.SetMode(gin.TestMode)
	return &MiddlewareTestHelper{
		Router: gin.New(),
	}
}

// CreateTestGinContext creates a Gin context for testing with the given HTTP method and path
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: build a Gin context and response recorder for a given HTTP method and path (pure)
func CreateTestGinContext(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, nil)
	return c, w
}

// CreateTestGinContextWithBody creates a Gin context with a request body
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: build a Gin context with a request body and content type for testing (pure)
func CreateTestGinContextWithBody(method, path, contentType string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
	if contentType != "" {
		c.Request.Header.Set("Content-Type", contentType)
	}
	return c, w
}

// SetUserContext sets authentication context on a Gin context
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: store email, user ID, and role into a Gin context for testing (mutates shared state)
func SetUserContext(c *gin.Context, email, userID string, role Role) {
	c.Set("userEmail", email)
	c.Set("userID", userID)
	if role != "" {
		c.Set("userRole", role)
	}
}

// SetFullUserContext sets complete user authentication context including groups and IdP
// SEM@c85b80a7fe0b19a3e43a1c6f9dc121ba2ccd093c: store full user identity fields including IdP and groups into a Gin context for testing (mutates shared state)
func SetFullUserContext(c *gin.Context, email, userID, internalUUID, idp string, groups []string) {
	c.Set("userEmail", email)
	c.Set("userID", userID)
	if internalUUID != "" {
		c.Set("userInternalUUID", internalUUID)
	}
	if idp != "" {
		c.Set("userIdP", idp)
		c.Set("userProvider", idp) // GetAuthenticatedUser reads this key
	}
	if groups != nil {
		c.Set("userGroups", groups)
	}
}

// AssertSecurityHeaders verifies that all expected security headers are present
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: validate that a response includes all required security headers (pure)
func AssertSecurityHeaders(t *testing.T, headers http.Header) {
	t.Helper()

	// Required security headers
	assert.Equal(t, "nosniff", headers.Get("X-Content-Type-Options"),
		"X-Content-Type-Options should be 'nosniff'")
	assert.Equal(t, "DENY", headers.Get("X-Frame-Options"),
		"X-Frame-Options should be 'DENY'")
	assert.Equal(t, "0", headers.Get("X-XSS-Protection"),
		"X-XSS-Protection should be '0' (disabled)")
	assert.NotEmpty(t, headers.Get("Content-Security-Policy"),
		"Content-Security-Policy should be set")
	assert.Equal(t, "strict-origin-when-cross-origin", headers.Get("Referrer-Policy"),
		"Referrer-Policy should be 'strict-origin-when-cross-origin'")
	assert.Equal(t, "no-store, no-cache, must-revalidate", headers.Get("Cache-Control"),
		"Cache-Control should prevent caching")
	assert.NotEmpty(t, headers.Get("Permissions-Policy"),
		"Permissions-Policy should be set")
}

// AssertHSTSHeader verifies that HSTS header is present and correctly configured
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: validate HSTS response header presence and value in tests (pure)
func AssertHSTSHeader(t *testing.T, headers http.Header, expectPresent bool) {
	t.Helper()

	hstsValue := headers.Get("Strict-Transport-Security")
	if expectPresent {
		assert.Equal(t, "max-age=31536000; includeSubDomains", hstsValue,
			"HSTS header should have correct value")
	} else {
		assert.Empty(t, hstsValue, "HSTS header should not be present when TLS is disabled")
	}
}

// AssertCORSHeaders verifies CORS headers are present when an allowed origin is set.
// The origin parameter is the expected reflected origin (or empty if no CORS headers expected).
// SEM@314b7ae8fe586a75ecee2e8fa7103d3193f15f7c: validate CORS response headers reflect the expected origin and required fields (pure)
func AssertCORSHeaders(t *testing.T, headers http.Header, origin ...string) {
	t.Helper()

	expectedOrigin := ""
	if len(origin) > 0 {
		expectedOrigin = origin[0]
	}

	if expectedOrigin != "" {
		assert.Equal(t, expectedOrigin, headers.Get("Access-Control-Allow-Origin"),
			"Access-Control-Allow-Origin should reflect the requesting origin")
		assert.Equal(t, "true", headers.Get("Access-Control-Allow-Credentials"),
			"Access-Control-Allow-Credentials should be 'true'")
		assert.Equal(t, "Origin", headers.Get("Vary"),
			"Vary should include Origin")
	}
	assert.NotEmpty(t, headers.Get("Access-Control-Allow-Headers"),
		"Access-Control-Allow-Headers should be set")
	assert.NotEmpty(t, headers.Get("Access-Control-Allow-Methods"),
		"Access-Control-Allow-Methods should be set")
}

// AssertRateLimitHeaders verifies rate limit headers are present with expected values
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: validate rate limit response headers match expected limit and remaining counts (pure)
func AssertRateLimitHeaders(t *testing.T, headers http.Header, remaining, limit int) {
	t.Helper()

	if limit > 0 {
		assert.Equal(t, strconv.Itoa(limit), headers.Get("X-RateLimit-Limit"),
			"X-RateLimit-Limit should match expected value")
	}
	if remaining >= 0 {
		assert.Equal(t, strconv.Itoa(remaining), headers.Get("X-RateLimit-Remaining"),
			"X-RateLimit-Remaining should match expected value")
	}
	assert.NotEmpty(t, headers.Get("X-RateLimit-Reset"),
		"X-RateLimit-Reset should be set")
}

// AssertJSONErrorResponse verifies the response is a JSON error with expected status
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: validate HTTP error response has expected status, JSON content type, and error text (pure)
func AssertJSONErrorResponse(t *testing.T, w *httptest.ResponseRecorder, expectedStatus int, expectedError string) {
	t.Helper()

	assert.Equal(t, expectedStatus, w.Code, "HTTP status should match")
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json",
		"Content-Type should be JSON")
	if expectedError != "" {
		assert.Contains(t, w.Body.String(), expectedError,
			"Response body should contain expected error")
	}
}

// RunMiddlewareTest executes middleware and returns the response
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: dispatch a single middleware handler against a synthetic request and return the response (pure)
func (h *MiddlewareTestHelper) RunMiddlewareTest(middleware gin.HandlerFunc, method, path string, setupContext func(*gin.Context)) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, nil)

	if setupContext != nil {
		setupContext(c)
	}

	middleware(c)
	return w
}

// RunMiddlewareChain executes a chain of middleware functions
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: dispatch an ordered chain of middleware handlers, stopping on abort (pure)
func (h *MiddlewareTestHelper) RunMiddlewareChain(middlewares []gin.HandlerFunc, method, path string, setupContext func(*gin.Context)) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, nil)

	if setupContext != nil {
		setupContext(c)
	}

	for _, mw := range middlewares {
		if c.IsAborted() {
			break
		}
		mw(c)
	}
	return w
}

// MiddlewareTestCase represents a test case for middleware testing
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: data container describing a single middleware test scenario and its expected outcomes (pure)
type MiddlewareTestCase struct {
	Name           string
	Method         string
	Path           string
	Body           []byte
	ContentType    string
	SetupContext   func(*gin.Context)
	SetupHeaders   func(*http.Request)
	ExpectedStatus int
	ExpectedError  string
	CheckResponse  func(*testing.T, *httptest.ResponseRecorder)
	CheckContext   func(*testing.T, *gin.Context)
}

// RunMiddlewareTestCases executes a slice of test cases against a middleware
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: execute a batch of middleware test cases, asserting status, error text, and custom checks (pure)
func RunMiddlewareTestCases(t *testing.T, middleware gin.HandlerFunc, testCases []MiddlewareTestCase) {
	t.Helper()

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			var c *gin.Context
			var w *httptest.ResponseRecorder

			if tc.Body != nil {
				c, w = CreateTestGinContextWithBody(tc.Method, tc.Path, tc.ContentType, tc.Body)
			} else {
				c, w = CreateTestGinContext(tc.Method, tc.Path)
			}

			if tc.SetupContext != nil {
				tc.SetupContext(c)
			}
			if tc.SetupHeaders != nil {
				tc.SetupHeaders(c.Request)
			}

			// Track if Next() was called
			nextCalled := false
			c.Set("_test_next_called", &nextCalled)

			// Wrap middleware to track Next() call
			wrappedMiddleware := func(ctx *gin.Context) {
				middleware(ctx)
				if !ctx.IsAborted() {
					nextCalled = true
				}
			}
			wrappedMiddleware(c)

			if tc.ExpectedStatus != 0 {
				assert.Equal(t, tc.ExpectedStatus, w.Code,
					"Expected status %d but got %d", tc.ExpectedStatus, w.Code)
			}

			if tc.ExpectedError != "" {
				assert.Contains(t, w.Body.String(), tc.ExpectedError,
					"Response should contain expected error")
			}

			if tc.CheckResponse != nil {
				tc.CheckResponse(t, w)
			}

			if tc.CheckContext != nil {
				tc.CheckContext(t, c)
			}
		})
	}
}

// TestUsers provides standard test user identities
var TestUsers = struct {
	Owner    TestUserIdentity
	Writer   TestUserIdentity
	Reader   TestUserIdentity
	External TestUserIdentity
}{
	Owner: TestUserIdentity{
		Email:        "owner@example.com",
		ProviderID:   "owner-provider-id",
		InternalUUID: "owner-internal-uuid",
		IdP:          "tmi",
		Groups:       []string{},
	},
	Writer: TestUserIdentity{
		Email:        "writer@example.com",
		ProviderID:   "writer-provider-id",
		InternalUUID: "writer-internal-uuid",
		IdP:          "tmi",
		Groups:       []string{},
	},
	Reader: TestUserIdentity{
		Email:        "reader@example.com",
		ProviderID:   "reader-provider-id",
		InternalUUID: "reader-internal-uuid",
		IdP:          "tmi",
		Groups:       []string{},
	},
	External: TestUserIdentity{
		Email:        "external@example.com",
		ProviderID:   "external-provider-id",
		InternalUUID: "external-internal-uuid",
		IdP:          "tmi",
		Groups:       []string{},
	},
}

// TestUserIdentity represents a test user with all identity attributes
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: data container holding all identity attributes for a test user principal (pure)
type TestUserIdentity struct {
	Email        string
	ProviderID   string
	InternalUUID string
	IdP          string
	Groups       []string
}

// SetContext sets the user identity in a Gin context
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: store the test user's identity into a Gin request context (mutates shared state)
func (u TestUserIdentity) SetContext(c *gin.Context) {
	SetFullUserContext(c, u.Email, u.ProviderID, u.InternalUUID, u.IdP, u.Groups)
}
