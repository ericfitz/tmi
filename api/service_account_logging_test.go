package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetUserIdentityForLogging(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("regular user identity", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/test", nil)

		// Set regular user context
		c.Set("isServiceAccount", false)
		c.Set("userEmail", "alice@example.com")

		identity := GetUserIdentityForLogging(c)
		expected := "user=alice@example.com"

		if identity != expected {
			t.Errorf("Expected '%s', got '%s'", expected, identity)
		}
	})

	t.Run("service account identity with full context", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/test", nil)

		// Set service account context
		c.Set("isServiceAccount", true)
		c.Set("serviceAccountCredentialID", "123e4567-e89b")
		c.Set("userEmail", "alice@example.com")
		c.Set("userDisplayName", "[Service Account] AWS Lambda Scanner")

		identity := GetUserIdentityForLogging(c)
		expected := "service_account=[Service Account] AWS Lambda Scanner (credential_id=123e4567-e89b, owner=alice@example.com)"

		if identity != expected {
			t.Errorf("Expected '%s', got '%s'", expected, identity)
		}
	})

	t.Run("service account identity with incomplete context", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/test", nil)

		// Set partial service account context (missing credential ID)
		c.Set("isServiceAccount", true)
		c.Set("userDisplayName", "[Service Account] Test")

		identity := GetUserIdentityForLogging(c)
		expected := "service_account=[Service Account] Test"

		if identity != expected {
			t.Errorf("Expected '%s', got '%s'", expected, identity)
		}
	})

	t.Run("fallback for missing context", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/test", nil)

		// No context set
		identity := GetUserIdentityForLogging(c)
		expected := "user=<unknown>"

		if identity != expected {
			t.Errorf("Expected '%s', got '%s'", expected, identity)
		}
	})

	t.Run("regular user without email", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/test", nil)

		c.Set("isServiceAccount", false)
		// No email set

		identity := GetUserIdentityForLogging(c)
		expected := "user=<unknown>"

		if identity != expected {
			t.Errorf("Expected '%s', got '%s'", expected, identity)
		}
	})
}

func TestIsServiceAccountRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("returns true for service account request", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/test", nil)

		c.Set("isServiceAccount", true)

		if !IsServiceAccountRequest(c) {
			t.Error("Expected true for service account request")
		}
	})

	t.Run("returns false for regular user request", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/test", nil)

		c.Set("isServiceAccount", false)

		if IsServiceAccountRequest(c) {
			t.Error("Expected false for regular user request")
		}
	})

	t.Run("returns false when context not set", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/test", nil)

		// No context set
		if IsServiceAccountRequest(c) {
			t.Error("Expected false when context not set")
		}
	})

	t.Run("returns false when context has wrong type", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/test", nil)

		c.Set("isServiceAccount", "true") // String instead of bool

		if IsServiceAccountRequest(c) {
			t.Error("Expected false when context has wrong type")
		}
	})
}
