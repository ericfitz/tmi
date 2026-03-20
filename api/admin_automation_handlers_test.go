package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Tests: normalizeAutomationName
// =============================================================================

func TestNormalizeAutomationName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"webhook-analyzer", "webhook-analyzer"},
		{"Webhook Analyzer", "webhook-analyzer"},
		{"my_bot", "my-bot"},
		{"My.Bot.Name", "my-bot-name"},
		{"test@user", "test-user"},
		{"UPPER", "upper"},
		{"a--b", "a-b"},
		{"a---b", "a-b"},
		{"-hello-", "hello"},
		{"ab", "ab"},
		{"a1", "a1"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := normalizeAutomationName(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// Tests: automationNamePattern
// =============================================================================

func TestAutomationNamePattern(t *testing.T) {
	valid := []string{
		"ab",
		"webhook-analyzer",
		"My Bot",
		"test_bot",
		"a1",
		"Hello World 2",
		"bot.name",
		"user@domain",
	}
	invalid := []string{
		"a",         // too short for pattern (single char matches but len check catches it)
		"1abc",      // starts with digit
		"-abc",      // starts with hyphen
		"abc-",      // ends with hyphen
		"abc ",      // ends with space
		"abc\ttab",  // contains tab
		"abc\nnewl", // contains newline
	}

	for _, name := range valid {
		t.Run("valid_"+name, func(t *testing.T) {
			assert.True(t, automationNamePattern.MatchString(name), "expected %q to be valid", name)
		})
	}
	for _, name := range invalid {
		t.Run("invalid_"+name, func(t *testing.T) {
			assert.False(t, automationNamePattern.MatchString(name), "expected %q to be invalid", name)
		})
	}
}

// =============================================================================
// Tests: CreateAutomationAccount handler - validation
// =============================================================================

func setupAutomationRouter() (*gin.Engine, *Server) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	server := &Server{}

	// Add fake auth middleware that sets admin context
	adminUUID := uuid.New().String()
	r.Use(func(c *gin.Context) {
		SetFullUserContext(c, "admin@test.com", "provider-id", adminUUID, "tmi", []string{"administrators"})
		c.Set("isAdmin", true)
		c.Next()
	})

	r.POST("/admin/users/automation", func(c *gin.Context) {
		server.CreateAutomationAccount(c)
	})

	return r, server
}

func TestCreateAutomationAccount_ValidationErrors(t *testing.T) {
	router, _ := setupAutomationRouter()

	// Use a mock user store that always returns "not found"
	store := newMockUserStore()
	GlobalUserStore = store

	t.Run("missing name field", func(t *testing.T) {
		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/admin/users/automation", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("name too short", func(t *testing.T) {
		body := `{"name": "a"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/users/automation", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("name starts with digit", func(t *testing.T) {
		body := `{"name": "1bot"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/users/automation", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("name ends with hyphen", func(t *testing.T) {
		body := `{"name": "bot-"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/users/automation", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("duplicate provider_user_id returns 409", func(t *testing.T) {
		// Add a user that will match the provider_user_id
		existingUser := makeTestAdminUser("TMI Automation: mybot", "tmi-automation-mybot@tmi.local", "tmi")
		existingUser.ProviderUserId = "tmi-automation-mybot"
		store.addUser(existingUser)

		// Need a mock that supports GetByProviderAndID
		body := `{"name": "mybot"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/users/automation", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)

		var resp Error
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "conflict", resp.Error)
	})

	t.Run("no auth service returns 503", func(t *testing.T) {
		// Remove the existing user to pass duplicate check
		freshStore := newMockUserStore()
		GlobalUserStore = freshStore

		body := `{"name": "newbot"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/users/automation", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Server has nil authService, so should return 503
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}
