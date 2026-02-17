package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock Quota Store for Client Credential Handler Tests
// =============================================================================

// mockClientCredentialQuotaStore implements ClientCredentialQuotaStore for testing
type mockClientCredentialQuotaStore struct {
	checkErr error
}

func (m *mockClientCredentialQuotaStore) GetClientCredentialQuota(_ context.Context, _ uuid.UUID) (int, error) {
	return 10, nil
}

func (m *mockClientCredentialQuotaStore) GetClientCredentialCount(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}

func (m *mockClientCredentialQuotaStore) CheckClientCredentialQuota(_ context.Context, _ uuid.UUID) error {
	return m.checkErr
}

// =============================================================================
// Helper Functions
// =============================================================================

// newTestServerWithNilAuth creates a minimal Server with no auth service (nil).
// This causes the authService type assertion to fail, triggering the 503 path.
func newTestServerWithNilAuth() *Server {
	return &Server{
		authService: nil,
	}
}

// =============================================================================
// TestCreateCurrentUserClientCredential
// =============================================================================

func TestCreateCurrentUserClientCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)

	validUserUUID := uuid.New().String()

	t.Run("MissingRequestBody", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		// Empty body
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte{})
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		server.CreateCurrentUserClientCredential(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "invalid_request", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "Request body is required")
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(`{invalid`))
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		server.CreateCurrentUserClientCredential(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "invalid_request", errResp.Error)
	})

	t.Run("EmptyName", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		body := `{"name": ""}`
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		server.CreateCurrentUserClientCredential(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "invalid_request", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "name cannot be empty")
	})

	t.Run("WhitespaceOnlyName", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		body := `{"name": "   "}`
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		server.CreateCurrentUserClientCredential(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "invalid_request", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "name cannot be empty")
	})

	t.Run("NameTooLong", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		longName := strings.Repeat("a", 101)
		body := fmt.Sprintf(`{"name": "%s"}`, longName)
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		server.CreateCurrentUserClientCredential(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "invalid_request", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "Invalid name")
	})

	t.Run("DescriptionTooLong", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		longDesc := strings.Repeat("b", 501)
		body := fmt.Sprintf(`{"name": "valid-name", "description": "%s"}`, longDesc)
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		server.CreateCurrentUserClientCredential(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "invalid_request", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "Invalid description")
	})

	t.Run("ExpiredExpiresAt", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		pastTime := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
		body := fmt.Sprintf(`{"name": "test-cred", "expires_at": "%s"}`, pastTime)
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		server.CreateCurrentUserClientCredential(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "invalid_request", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "expires_at must be a future date")
	})

	t.Run("InvalidUserUUID", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		body := `{"name": "test-cred"}`
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		// Set an invalid UUID for the user
		SetFullUserContext(c, "user@example.com", "provider-id", "not-a-uuid", "tmi", nil)

		server.CreateCurrentUserClientCredential(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "unauthorized", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "Invalid authentication state")
		// Should set WWW-Authenticate header
		assert.NotEmpty(t, w.Header().Get("WWW-Authenticate"))
	})

	t.Run("EmptyUserUUID", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		body := `{"name": "test-cred"}`
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		// Set empty string for internal UUID - c.GetString returns "" if not set
		SetFullUserContext(c, "user@example.com", "provider-id", "", "tmi", nil)

		server.CreateCurrentUserClientCredential(c)

		// Empty string fails uuid.Parse, so we get 401
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("QuotaExceeded", func(t *testing.T) {
		// Save and restore the global quota store
		originalQuotaStore := GlobalClientCredentialQuotaStore
		defer func() { GlobalClientCredentialQuotaStore = originalQuotaStore }()

		quotaErr := fmt.Errorf("client credential quota exceeded: 10/10 credentials used")
		GlobalClientCredentialQuotaStore = &mockClientCredentialQuotaStore{
			checkErr: quotaErr,
		}

		server := newTestServerWithNilAuth()
		body := `{"name": "test-cred"}`
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		server.CreateCurrentUserClientCredential(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "quota_exceeded", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "quota exceeded")
	})

	t.Run("AuthServiceUnavailable", func(t *testing.T) {
		// Ensure no quota store interferes
		originalQuotaStore := GlobalClientCredentialQuotaStore
		defer func() { GlobalClientCredentialQuotaStore = originalQuotaStore }()
		GlobalClientCredentialQuotaStore = nil

		server := newTestServerWithNilAuth()
		body := `{"name": "test-cred"}`
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		server.CreateCurrentUserClientCredential(c)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "service_unavailable", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "Authentication service temporarily unavailable")
		// Should set Retry-After header
		assert.Equal(t, "30", w.Header().Get("Retry-After"))
	})

	t.Run("UnknownFieldRejected", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		body := `{"name": "test-cred", "unknown_field": "value"}`
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		server.CreateCurrentUserClientCredential(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "invalid_request", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "unknown field")
	})

	t.Run("ValidRequestWithFutureExpiry", func(t *testing.T) {
		// This test validates that all validation passes but reaches the auth service check.
		// Since authService is nil, we expect 503 (which proves all prior validation passed).
		originalQuotaStore := GlobalClientCredentialQuotaStore
		defer func() { GlobalClientCredentialQuotaStore = originalQuotaStore }()
		GlobalClientCredentialQuotaStore = nil

		server := newTestServerWithNilAuth()
		futureTime := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
		body := fmt.Sprintf(`{"name": "test-cred", "description": "A test credential", "expires_at": "%s"}`, futureTime)
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		server.CreateCurrentUserClientCredential(c)

		// Should pass all validation and reach the auth service check, which returns 503
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("QuotaPassesThenAuthFails", func(t *testing.T) {
		// Quota check passes (nil error) but auth service is unavailable
		originalQuotaStore := GlobalClientCredentialQuotaStore
		defer func() { GlobalClientCredentialQuotaStore = originalQuotaStore }()
		GlobalClientCredentialQuotaStore = &mockClientCredentialQuotaStore{
			checkErr: nil, // quota check passes
		}

		server := newTestServerWithNilAuth()
		body := `{"name": "test-cred"}`
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		server.CreateCurrentUserClientCredential(c)

		// Quota passes, but auth service is nil -> 503
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

// =============================================================================
// TestListCurrentUserClientCredentials
// =============================================================================

func TestListCurrentUserClientCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)

	validUserUUID := uuid.New().String()

	t.Run("InvalidUserUUID", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		c, w := CreateTestGinContext("GET", "/me/client_credentials")
		SetFullUserContext(c, "user@example.com", "provider-id", "not-a-uuid", "tmi", nil)

		params := ListCurrentUserClientCredentialsParams{}
		server.ListCurrentUserClientCredentials(c, params)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "unauthorized", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "Invalid authentication state")
		assert.NotEmpty(t, w.Header().Get("WWW-Authenticate"))
	})

	t.Run("EmptyUserUUID", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		c, w := CreateTestGinContext("GET", "/me/client_credentials")
		SetFullUserContext(c, "user@example.com", "provider-id", "", "tmi", nil)

		params := ListCurrentUserClientCredentialsParams{}
		server.ListCurrentUserClientCredentials(c, params)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("AuthServiceUnavailable", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		c, w := CreateTestGinContext("GET", "/me/client_credentials")
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		params := ListCurrentUserClientCredentialsParams{}
		server.ListCurrentUserClientCredentials(c, params)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "service_unavailable", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "Authentication service temporarily unavailable")
		assert.Equal(t, "30", w.Header().Get("Retry-After"))
	})

	t.Run("WithPaginationParams", func(t *testing.T) {
		// Test that pagination params are accepted and we still reach the auth service check
		server := newTestServerWithNilAuth()
		c, w := CreateTestGinContext("GET", "/me/client_credentials?limit=50&offset=10")
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		limit := 50
		offset := 10
		params := ListCurrentUserClientCredentialsParams{
			Limit:  &limit,
			Offset: &offset,
		}
		server.ListCurrentUserClientCredentials(c, params)

		// Should pass pagination parsing and UUID validation, then fail at auth service
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("LimitCappedAt100", func(t *testing.T) {
		// Even with limit > 100, pagination parsing should succeed (it caps at 100)
		// and we should reach the auth service check
		server := newTestServerWithNilAuth()
		c, w := CreateTestGinContext("GET", "/me/client_credentials?limit=200")
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		limit := 200
		params := ListCurrentUserClientCredentialsParams{
			Limit: &limit,
		}
		server.ListCurrentUserClientCredentials(c, params)

		// Should reach auth service check (503) - limit is capped internally, not rejected
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

// =============================================================================
// TestDeleteCurrentUserClientCredential
// =============================================================================

func TestDeleteCurrentUserClientCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)

	validUserUUID := uuid.New().String()
	credentialID := uuid.New()

	t.Run("InvalidUserUUID", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		c, w := CreateTestGinContext("DELETE", "/me/client_credentials/"+credentialID.String())
		SetFullUserContext(c, "user@example.com", "provider-id", "not-a-uuid", "tmi", nil)

		server.DeleteCurrentUserClientCredential(c, credentialID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "unauthorized", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "Invalid authentication state")
		assert.NotEmpty(t, w.Header().Get("WWW-Authenticate"))
	})

	t.Run("EmptyUserUUID", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		c, w := CreateTestGinContext("DELETE", "/me/client_credentials/"+credentialID.String())
		SetFullUserContext(c, "user@example.com", "provider-id", "", "tmi", nil)

		server.DeleteCurrentUserClientCredential(c, credentialID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("AuthServiceUnavailable", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		c, w := CreateTestGinContext("DELETE", "/me/client_credentials/"+credentialID.String())
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)

		server.DeleteCurrentUserClientCredential(c, credentialID)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "service_unavailable", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "Authentication service temporarily unavailable")
		assert.Equal(t, "30", w.Header().Get("Retry-After"))
	})
}

// =============================================================================
// TestValidateClientCredentialName
// =============================================================================

func TestValidateClientCredentialName(t *testing.T) {
	t.Run("EmptyString", func(t *testing.T) {
		result := validateClientCredentialName("")
		assert.NotEmpty(t, result, "Empty name should return an error")
		assert.Contains(t, result, "name cannot be empty")
	})

	t.Run("WhitespaceOnly", func(t *testing.T) {
		result := validateClientCredentialName("   ")
		assert.NotEmpty(t, result, "Whitespace-only name should return an error")
		assert.Contains(t, result, "name cannot be empty")
	})

	t.Run("TabsAndNewlines", func(t *testing.T) {
		result := validateClientCredentialName("\t\n")
		assert.NotEmpty(t, result, "Tabs/newlines should return an error")
	})

	t.Run("ValidName", func(t *testing.T) {
		result := validateClientCredentialName("my-api-key")
		assert.Empty(t, result, "Valid name should not return an error")
	})

	t.Run("ValidNameWithSpaces", func(t *testing.T) {
		result := validateClientCredentialName("My API Key")
		assert.Empty(t, result, "Valid name with spaces should not return an error")
	})

	t.Run("ExactlyMaxLength", func(t *testing.T) {
		name := strings.Repeat("x", 100)
		result := validateClientCredentialName(name)
		assert.Empty(t, result, "Name at exactly max length should not return an error")
	})

	t.Run("ExceedsMaxLength", func(t *testing.T) {
		name := strings.Repeat("x", 101)
		result := validateClientCredentialName(name)
		assert.NotEmpty(t, result, "Name exceeding max length should return an error")
		assert.Contains(t, result, "exceeds maximum length")
	})

	t.Run("SingleCharacter", func(t *testing.T) {
		result := validateClientCredentialName("a")
		assert.Empty(t, result, "Single character name should be valid")
	})
}

// =============================================================================
// TestValidateClientCredentialDescription
// =============================================================================

func TestValidateClientCredentialDescription(t *testing.T) {
	t.Run("EmptyString", func(t *testing.T) {
		result := validateClientCredentialDescription("")
		assert.Empty(t, result, "Empty description should be valid (description is optional)")
	})

	t.Run("ValidDescription", func(t *testing.T) {
		result := validateClientCredentialDescription("This is a valid description for testing purposes")
		assert.Empty(t, result, "Valid description should not return an error")
	})

	t.Run("ExactlyMaxLength", func(t *testing.T) {
		desc := strings.Repeat("d", 500)
		result := validateClientCredentialDescription(desc)
		assert.Empty(t, result, "Description at exactly max length should not return an error")
	})

	t.Run("ExceedsMaxLength", func(t *testing.T) {
		desc := strings.Repeat("d", 501)
		result := validateClientCredentialDescription(desc)
		assert.NotEmpty(t, result, "Description exceeding max length should return an error")
		assert.Contains(t, result, "exceeds maximum length")
	})

	t.Run("ShortDescription", func(t *testing.T) {
		result := validateClientCredentialDescription("ok")
		assert.Empty(t, result, "Short description should be valid")
	})
}
