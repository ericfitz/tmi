package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/unicodecheck"
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
func (s *Server) ListCurrentUserClientCredentials(c *gin.Context, params ListCurrentUserClientCredentialsParams) {
	logger := slogging.Get().WithContext(c)
	userUUID := c.GetString("userInternalUUID")

	// Parse pagination parameters with defaults
	limit := 20
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
		if limit > 100 {
			limit = 100
		}
	}
	if params.Offset != nil {
		offset = *params.Offset
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

	// Get total count before pagination
	total := len(creds)

	// Apply pagination
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	paginatedCreds := creds[start:end]

	// Convert to OpenAPI response type
	// Initialize as empty slice (not nil) to ensure JSON serializes to [] instead of null
	apiCreds := make([]ClientCredentialInfo, 0, len(paginatedCreds))
	for _, cred := range paginatedCreds {
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

	logger.Debug("Listed %d client credentials for user %s (total: %d)", len(apiCreds), userUUID, total)

	c.JSON(http.StatusOK, ListClientCredentialsResponse{
		Credentials: apiCreds,
		Total:       total,
		Limit:       limit,
		Offset:      offset,
	})
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
	if unicodecheck.ContainsControlChars(name) {
		return "name contains invalid control characters"
	}

	// Check for zero-width characters that could be used for spoofing
	if unicodecheck.ContainsZeroWidthChars(name) {
		return "name contains invalid zero-width characters"
	}

	// Check for dangerous Unicode categories (can cause display/storage issues)
	if unicodecheck.ContainsProblematicCategories(name) {
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
	if unicodecheck.ContainsControlChars(description) {
		return "description contains invalid control characters"
	}

	// Check for zero-width characters
	if unicodecheck.ContainsZeroWidthChars(description) {
		return "description contains invalid zero-width characters"
	}

	// Check for problematic Unicode
	if unicodecheck.ContainsProblematicCategories(description) {
		return "description contains invalid Unicode characters"
	}

	return ""
}

// sanitizeForLogging removes potentially dangerous characters from strings before logging.
// Delegates to the consolidated unicodecheck package.
func sanitizeForLogging(s string) string {
	return unicodecheck.SanitizeForLogging(s)
}

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
