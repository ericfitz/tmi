package api

import (
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// CreateCurrentUserClientCredential handles POST /users/me/client_credentials
// Creates a new OAuth 2.0 client credential for machine-to-machine authentication
func (s *Server) CreateCurrentUserClientCredential(c *gin.Context) {
	logger := slogging.Get().WithContext(c)
	userUUID := c.GetString("userInternalUUID")

	// Parse request body
	var req CreateCurrentUserClientCredentialJSONBody
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("Invalid request body: %v", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: "Invalid request body: " + err.Error(),
		})
		return
	}

	// Parse user UUID
	ownerUUID, err := uuid.Parse(userUUID)
	if err != nil {
		logger.Error("Invalid user UUID: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to parse user UUID",
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
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Authentication service unavailable",
		})
		return
	}

	// Create client credential
	service := NewClientCredentialService(authServiceAdapter.GetService())
	resp, err := service.Create(c.Request.Context(), ownerUUID, CreateClientCredentialRequest{
		Name:        req.Name,
		Description: StrFromPtr(req.Description),
		ExpiresAt:   TimeFromPtr(req.ExpiresAt),
	})
	if err != nil {
		logger.Error("Failed to create client credential: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to create client credential",
		})
		return
	}

	logger.Info("Client credential created: client_id=%s, name=%s, owner=%s",
		resp.ClientID, resp.Name, userUUID)

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

// ListCurrentUserClientCredentials handles GET /users/me/client_credentials
// Retrieves all client credentials owned by the authenticated user (without secrets)
func (s *Server) ListCurrentUserClientCredentials(c *gin.Context) {
	logger := slogging.Get().WithContext(c)
	userUUID := c.GetString("userInternalUUID")

	// Parse user UUID
	ownerUUID, err := uuid.Parse(userUUID)
	if err != nil {
		logger.Error("Invalid user UUID: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to parse user UUID",
		})
		return
	}

	// Get underlying auth service from adapter
	authServiceAdapter, ok := s.authService.(*AuthServiceAdapter)
	if !ok || authServiceAdapter == nil {
		logger.Error("Failed to get auth service adapter")
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Authentication service unavailable",
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

// DeleteCurrentUserClientCredential handles DELETE /users/me/client_credentials/{id}
// Permanently deletes a client credential
func (s *Server) DeleteCurrentUserClientCredential(c *gin.Context, id openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)
	userUUID := c.GetString("userInternalUUID")

	// Parse user UUID
	ownerUUID, err := uuid.Parse(userUUID)
	if err != nil {
		logger.Error("Invalid user UUID: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to parse user UUID",
		})
		return
	}

	// Get underlying auth service from adapter
	authServiceAdapter, ok := s.authService.(*AuthServiceAdapter)
	if !ok || authServiceAdapter == nil {
		logger.Error("Failed to get auth service adapter")
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Authentication service unavailable",
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
