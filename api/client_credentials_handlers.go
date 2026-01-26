package api

import (
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// CreateCurrentUserClientCredential handles POST /me/client_credentials
// Creates a new OAuth 2.0 client credential for machine-to-machine authentication
func (s *Server) CreateCurrentUserClientCredential(c *gin.Context) {
	logger := slogging.Get().WithContext(c)
	userUUID := c.GetString("userInternalUUID")

	// Parse request body with strict binding (rejects unknown fields to prevent mass assignment)
	var req CreateCurrentUserClientCredentialJSONBody
	if errMsg := StrictJSONBind(c, &req); errMsg != "" {
		logger.Warn("Invalid request body: %s", errMsg)
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: errMsg,
		})
		return
	}

	// Validate name field for security issues
	if errMsg := validateClientCredentialName(req.Name); errMsg != "" {
		logger.Warn("Invalid name in client credential request: %s (name=%s)",
			errMsg, sanitizeForLogging(req.Name))
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: "Invalid name: " + errMsg,
		})
		return
	}

	// Validate description field if provided
	description := StrFromPtr(req.Description)
	if errMsg := validateClientCredentialDescription(description); errMsg != "" {
		logger.Warn("Invalid description in client credential request: %s", errMsg)
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: "Invalid description: " + errMsg,
		})
		return
	}

	// Validate expires_at if provided (must be in the future)
	if req.ExpiresAt != nil && req.ExpiresAt.Before(time.Now()) {
		logger.Warn("Client credential expiration date is in the past")
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: "expires_at must be a future date",
		})
		return
	}

	// Parse user UUID
	ownerUUID, err := uuid.Parse(userUUID)
	if err != nil {
		logger.Error("Invalid user UUID format in authentication context: %v", err)
		// Invalid UUID in auth context indicates corrupted authentication state
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Invalid authentication state - please re-authenticate")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "Invalid authentication state - please re-authenticate",
		})
		return
	}

	// Check quota BEFORE creation
	if GlobalClientCredentialQuotaStore != nil {
		if err := GlobalClientCredentialQuotaStore.CheckClientCredentialQuota(c.Request.Context(), ownerUUID); err != nil {
			logger.Warn("Client credential quota exceeded for user %s: %v", userUUID, err)
			c.JSON(http.StatusForbidden, Error{
				Error:            "quota_exceeded",
				ErrorDescription: err.Error(),
			})
			return
		}
	}

	// Get underlying auth service from adapter
	authServiceAdapter, ok := s.authService.(*AuthServiceAdapter)
	if !ok || authServiceAdapter == nil {
		logger.Error("Failed to get auth service adapter")
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, Error{
			Error:            "service_unavailable",
			ErrorDescription: "Authentication service temporarily unavailable - please retry",
		})
		return
	}

	// Create client credential
	service := NewClientCredentialService(authServiceAdapter.GetService())
	resp, err := service.Create(c.Request.Context(), ownerUUID, CreateClientCredentialRequest{
		Name:        req.Name,
		Description: description,
		ExpiresAt:   TimeFromPtr(req.ExpiresAt),
	})
	if err != nil {
		// Check if it's a validation-related error vs a true server error
		errStr := err.Error()
		if strings.Contains(errStr, "constraint") ||
			strings.Contains(errStr, "duplicate") ||
			strings.Contains(errStr, "invalid") {
			logger.Warn("Client credential creation failed due to validation: %v", err)
			c.JSON(http.StatusBadRequest, Error{
				Error:            "invalid_request",
				ErrorDescription: "Failed to create client credential: validation error",
			})
			return
		}
		logger.Error("Failed to create client credential: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to create client credential",
		})
		return
	}

	logger.Info("Client credential created: client_id=%s, name=%s, owner=%s",
		resp.ClientID, sanitizeForLogging(resp.Name), userUUID)

	// Convert to OpenAPI response type
	apiResp := ClientCredentialResponse{
		Id:           resp.ID,
		ClientId:     resp.ClientID,
		ClientSecret: resp.ClientSecret,
		Name:         resp.Name,
		Description:  StrPtr(resp.Description),
		CreatedAt:    resp.CreatedAt,
		ExpiresAt:    TimePtr(resp.ExpiresAt),
	}

	c.JSON(http.StatusCreated, apiResp)
}

// ListCurrentUserClientCredentials handles GET /me/client_credentials
// Retrieves all client credentials owned by the authenticated user (without secrets)
func (s *Server) ListCurrentUserClientCredentials(c *gin.Context) {
	logger := slogging.Get().WithContext(c)
	userUUID := c.GetString("userInternalUUID")

	// Parse user UUID
	ownerUUID, err := uuid.Parse(userUUID)
	if err != nil {
		logger.Error("Invalid user UUID format in authentication context: %v", err)
		// Invalid UUID in auth context indicates corrupted authentication state
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Invalid authentication state - please re-authenticate")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "Invalid authentication state - please re-authenticate",
		})
		return
	}

	// Get underlying auth service from adapter
	authServiceAdapter, ok := s.authService.(*AuthServiceAdapter)
	if !ok || authServiceAdapter == nil {
		logger.Error("Failed to get auth service adapter")
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, Error{
			Error:            "service_unavailable",
			ErrorDescription: "Authentication service temporarily unavailable - please retry",
		})
		return
	}

	// List credentials
	service := NewClientCredentialService(authServiceAdapter.GetService())
	creds, err := service.List(c.Request.Context(), ownerUUID)
	if err != nil {
		logger.Error("Failed to list client credentials: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to list client credentials",
		})
		return
	}

	// Convert to OpenAPI response type
	// Initialize as empty slice (not nil) to ensure JSON serializes to [] instead of null
	apiCreds := make([]ClientCredentialInfo, 0)
	for _, cred := range creds {
		// Convert internal service type to OpenAPI type
		// Service has Description as string, OpenAPI expects *string
		apiCreds = append(apiCreds, ClientCredentialInfo{
			Id:          cred.ID,
			ClientId:    cred.ClientID,
			Name:        cred.Name,
			Description: StrPtr(cred.Description),
			IsActive:    cred.IsActive,
			LastUsedAt:  TimePtr(cred.LastUsedAt),
			CreatedAt:   cred.CreatedAt,
			ModifiedAt:  cred.ModifiedAt,
			ExpiresAt:   TimePtr(cred.ExpiresAt),
		})
	}

	logger.Debug("Listed %d client credentials for user %s", len(apiCreds), userUUID)

	c.JSON(http.StatusOK, apiCreds)
}

// DeleteCurrentUserClientCredential handles DELETE /me/client_credentials/{id}
// Permanently deletes a client credential
func (s *Server) DeleteCurrentUserClientCredential(c *gin.Context, id openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)
	userUUID := c.GetString("userInternalUUID")

	// Parse user UUID
	ownerUUID, err := uuid.Parse(userUUID)
	if err != nil {
		logger.Error("Invalid user UUID format in authentication context: %v", err)
		// Invalid UUID in auth context indicates corrupted authentication state
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Invalid authentication state - please re-authenticate")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "Invalid authentication state - please re-authenticate",
		})
		return
	}

	// Get underlying auth service from adapter
	authServiceAdapter, ok := s.authService.(*AuthServiceAdapter)
	if !ok || authServiceAdapter == nil {
		logger.Error("Failed to get auth service adapter")
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, Error{
			Error:            "service_unavailable",
			ErrorDescription: "Authentication service temporarily unavailable - please retry",
		})
		return
	}

	// Delete credential
	service := NewClientCredentialService(authServiceAdapter.GetService())
	if err := service.Delete(c.Request.Context(), id, ownerUUID); err != nil {
		logger.Error("Failed to delete client credential: %v", err)
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Client credential not found or not owned by user",
		})
		return
	}

	logger.Info("Client credential deleted: id=%s, owner=%s", id, userUUID)

	c.Status(http.StatusNoContent)
}

// Input validation for client credentials

// validateClientCredentialName validates the name field for security issues
// Returns an error message if validation fails, empty string if valid
func validateClientCredentialName(name string) string {
	// Check for empty or whitespace-only names
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "name cannot be empty or whitespace only"
	}

	// Check length bounds (OpenAPI spec: minLength=1, maxLength=100)
	if len(name) > 100 {
		return "name exceeds maximum length of 100 characters"
	}

	// Check for control characters (except common whitespace)
	for _, r := range name {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return "name contains invalid control characters"
		}
	}

	// Check for zero-width characters that could be used for spoofing
	if containsZeroWidthChars(name) {
		return "name contains invalid zero-width characters"
	}

	// Check for dangerous Unicode categories (can cause display/storage issues)
	if containsProblematicUnicode(name) {
		return "name contains invalid Unicode characters"
	}

	return ""
}

// validateClientCredentialDescription validates the description field
// Returns an error message if validation fails, empty string if valid
func validateClientCredentialDescription(description string) string {
	// Empty description is allowed
	if description == "" {
		return ""
	}

	// Check length bounds (OpenAPI spec: maxLength=500)
	if len(description) > 500 {
		return "description exceeds maximum length of 500 characters"
	}

	// Check for control characters (except common whitespace)
	for _, r := range description {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return "description contains invalid control characters"
		}
	}

	// Check for zero-width characters
	if containsZeroWidthChars(description) {
		return "description contains invalid zero-width characters"
	}

	// Check for problematic Unicode
	if containsProblematicUnicode(description) {
		return "description contains invalid Unicode characters"
	}

	return ""
}

// containsZeroWidthChars checks for zero-width Unicode characters that can be used for spoofing
func containsZeroWidthChars(s string) bool {
	// Zero-width characters commonly used in attacks
	zeroWidthChars := []rune{
		'\u200B', // Zero Width Space
		'\u200C', // Zero Width Non-Joiner
		'\u200D', // Zero Width Joiner
		'\u200E', // Left-to-Right Mark
		'\u200F', // Right-to-Left Mark
		'\u202A', // Left-to-Right Embedding
		'\u202B', // Right-to-Left Embedding
		'\u202C', // Pop Directional Formatting
		'\u202D', // Left-to-Right Override
		'\u202E', // Right-to-Left Override
		'\uFEFF', // Byte Order Mark / Zero Width No-Break Space
	}

	for _, r := range s {
		for _, zw := range zeroWidthChars {
			if r == zw {
				return true
			}
		}
	}
	return false
}

// containsProblematicUnicode checks for Unicode characters that can cause issues
func containsProblematicUnicode(s string) bool {
	for _, r := range s {
		// Reject characters in problematic Unicode categories:
		// - Private Use Area
		// - Surrogates (shouldn't appear in valid UTF-8 anyway)
		// - Non-characters
		// - Tags (except for legitimate emoji sequences)
		if unicode.Is(unicode.Co, r) || // Private Use
			unicode.Is(unicode.Cs, r) || // Surrogate
			(r >= 0xFDD0 && r <= 0xFDEF) || // Non-characters
			(r&0xFFFF == 0xFFFE) || (r&0xFFFF == 0xFFFF) { // Non-characters at end of planes
			return true
		}

		// Hangul filler characters (used in fuzzing attacks)
		if r == '\u3164' || r == '\uFFA0' {
			return true
		}
	}
	return false
}

// sanitizeForLogging removes potentially dangerous characters from strings before logging
func sanitizeForLogging(s string) string {
	// Replace control characters and zero-width chars with placeholder
	var result strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			result.WriteString("[CTRL]")
		} else if containsZeroWidthChars(string(r)) {
			result.WriteString("[ZW]")
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// clientCredentialNamePattern is a compiled regex for additional name validation
var clientCredentialNamePattern = regexp.MustCompile(`^[\p{L}\p{N}\p{P}\p{S}\p{Z}]+$`)

// Helper functions for pointer conversions

func StrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func StrPtrOrEmpty(s string) *string {
	// Always return pointer, even for empty strings
	return &s
}

func StrFromPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func TimePtr(t *time.Time) *time.Time {
	return t
}

func TimeFromPtr(t *time.Time) *time.Time {
	return t
}
