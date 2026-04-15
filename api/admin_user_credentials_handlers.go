package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// credentialDeleter is an interface for deleting client credentials.
// It is satisfied by *ClientCredentialService and allows unit testing without a real database.
type credentialDeleter interface {
	Delete(ctx context.Context, credID uuid.UUID, ownerUUID uuid.UUID) error
}

// getAutomationUser looks up a user by UUID and verifies they are an automation account.
// Returns the AdminUser on success. On failure, writes the appropriate error response
// and returns nil.
func (s *Server) getAutomationUser(c *gin.Context, internalUuid openapi_types.UUID) *AdminUser {
	logger := slogging.Get().WithContext(c)

	user, err := GlobalUserStore.Get(c.Request.Context(), internalUuid)
	if err != nil {
		logger.Warn("Admin client credentials: user not found: %s", internalUuid)
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "User not found",
		})
		return nil
	}

	if user.Automation == nil || !*user.Automation {
		logger.Warn("Admin client credentials: user %s is not an automation account", internalUuid)
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Client credentials management via admin API is only available for automation accounts",
		})
		return nil
	}

	return user
}

// ListAdminUserClientCredentials handles GET /admin/users/{internal_uuid}/client_credentials
func (s *Server) ListAdminUserClientCredentials(c *gin.Context, internalUuid openapi_types.UUID, params ListAdminUserClientCredentialsParams) {
	logger := slogging.Get().WithContext(c)

	user := s.getAutomationUser(c, internalUuid)
	if user == nil {
		return
	}

	// Parse pagination parameters with defaults
	limit := 20
	offset := 0
	if params.Limit != nil {
		limit = min(*params.Limit, 100)
	}
	if params.Offset != nil {
		offset = *params.Offset
	}

	// Get auth service
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

	// List credentials for the target user
	ccService := NewClientCredentialService(authServiceAdapter.GetService())
	creds, err := ccService.List(c.Request.Context(), internalUuid)
	if err != nil {
		logger.Error("Failed to list client credentials for user %s: %v", internalUuid, err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to list client credentials",
		})
		return
	}

	// Paginate
	total := len(creds)
	start := min(offset, total)
	end := min(start+limit, total)
	paginatedCreds := creds[start:end]

	// Convert to API response type
	apiCreds := make([]ClientCredentialInfo, 0, len(paginatedCreds))
	for _, cred := range paginatedCreds {
		apiCreds = append(apiCreds, ClientCredentialInfo{
			Id:          cred.ID,
			ClientId:    cred.ClientID,
			Name:        cred.Name,
			Description: strPtr(cred.Description),
			IsActive:    cred.IsActive,
			LastUsedAt:  timePtr(cred.LastUsedAt),
			CreatedAt:   cred.CreatedAt,
			ModifiedAt:  cred.ModifiedAt,
			ExpiresAt:   timePtr(cred.ExpiresAt),
		})
	}

	logger.Debug("Listed %d client credentials for automation user %s (total: %d)", len(apiCreds), internalUuid, total)

	c.JSON(http.StatusOK, ListClientCredentialsResponse{
		Credentials: apiCreds,
		Total:       total,
		Limit:       limit,
		Offset:      offset,
	})
}

// CreateAdminUserClientCredential handles POST /admin/users/{internal_uuid}/client_credentials
func (s *Server) CreateAdminUserClientCredential(c *gin.Context, internalUuid openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	user := s.getAutomationUser(c, internalUuid)
	if user == nil {
		return
	}

	// Parse request body
	var req CreateAdminUserClientCredentialJSONRequestBody
	if errMsg := StrictJSONBind(c, &req); errMsg != "" {
		logger.Warn("Invalid request body: %s", errMsg)
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: errMsg,
		})
		return
	}

	// Validate name
	if errMsg := validateClientCredentialName(req.Name); errMsg != "" {
		logger.Warn("Invalid name in client credential request: %s", errMsg)
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: "Invalid name: " + errMsg,
		})
		return
	}

	// Validate description
	description := ""
	if req.Description != nil {
		description = *req.Description
	}
	if errMsg := validateClientCredentialDescription(description); errMsg != "" {
		logger.Warn("Invalid description in client credential request: %s", errMsg)
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: "Invalid description: " + errMsg,
		})
		return
	}

	// Validate expires_at
	if req.ExpiresAt != nil && req.ExpiresAt.Before(time.Now()) {
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: "expires_at must be a future date",
		})
		return
	}

	// Get auth service
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

	// Create credential (no quota check — admin operation)
	ccService := NewClientCredentialService(authServiceAdapter.GetService())
	resp, err := ccService.Create(c.Request.Context(), internalUuid, CreateClientCredentialRequest{
		Name:        req.Name,
		Description: description,
		ExpiresAt:   timeFromPtr(req.ExpiresAt),
	})
	if err != nil {
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
		logger.Error("Failed to create client credential for user %s: %v", internalUuid, err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to create client credential",
		})
		return
	}

	logger.Info("[AUDIT] Admin created client credential for automation user: user=%s, client_id=%s, name=%s",
		internalUuid, resp.ClientID, sanitizeForLogging(resp.Name))

	c.JSON(http.StatusCreated, ClientCredentialResponse{
		Id:           resp.ID,
		ClientId:     resp.ClientID,
		ClientSecret: resp.ClientSecret,
		Name:         resp.Name,
		Description:  strPtr(resp.Description),
		CreatedAt:    resp.CreatedAt,
		ExpiresAt:    timePtr(resp.ExpiresAt),
	})
}

// DeleteAdminUserClientCredential handles DELETE /admin/users/{internal_uuid}/client_credentials/{credential_id}
func (s *Server) DeleteAdminUserClientCredential(c *gin.Context, internalUuid openapi_types.UUID, credentialId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	user := s.getAutomationUser(c, internalUuid)
	if user == nil {
		return
	}

	// Resolve the credential deleter — use injected override (tests) or build from auth service.
	var deleter credentialDeleter
	if s.credentialDeleter != nil {
		deleter = s.credentialDeleter
	} else {
		// Get auth service adapter
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
		// Guard against nil underlying auth service to prevent nil pointer panic
		underlyingService := authServiceAdapter.GetService()
		if underlyingService == nil {
			logger.Error("Underlying auth service is nil for admin credential delete")
			c.Header("Retry-After", "30")
			c.JSON(http.StatusServiceUnavailable, Error{
				Error:            "service_unavailable",
				ErrorDescription: "Authentication service temporarily unavailable - please retry",
			})
			return
		}
		deleter = NewClientCredentialService(underlyingService)
	}

	// Delete credential (ownership enforced by ownerUUID)
	if err := deleter.Delete(c.Request.Context(), credentialId, internalUuid); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "unauthorized") {
			logger.Warn("Client credential not found or unauthorized: user=%s, credential=%s: %v", internalUuid, credentialId, err)
			c.JSON(http.StatusNotFound, Error{
				Error:            "not_found",
				ErrorDescription: "Client credential not found or not owned by this user",
			})
		} else {
			logger.Error("Failed to delete client credential %s for user %s: %v", credentialId, internalUuid, err)
			c.Header("Retry-After", "30")
			c.JSON(http.StatusServiceUnavailable, Error{
				Error:            "service_unavailable",
				ErrorDescription: "Failed to delete client credential - please retry",
			})
		}
		return
	}

	logger.Info("[AUDIT] Admin deleted client credential for automation user: user=%s, credential_id=%s",
		internalUuid, credentialId)

	c.Status(http.StatusNoContent)
}
