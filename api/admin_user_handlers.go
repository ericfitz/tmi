package api

import (
	"fmt"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ListAdminUsers handles GET /admin/users
func (s *Server) ListAdminUsers(c *gin.Context, params ListAdminUsersParams) {
	logger := slogging.Get().WithContext(c)

	// Extract parameters with defaults
	limit := 50
	if params.Limit != nil {
		limit = *params.Limit
		if limit < 0 || limit > 200 {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_limit",
				Message: "limit must be between 0 and 200",
			})
			return
		}
	}

	offset := 0
	if params.Offset != nil {
		offset = *params.Offset
		if offset < 0 {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_offset",
				Message: "offset must be a non-negative integer",
			})
			return
		}
	}

	sortBy := "created_at"
	if params.SortBy != nil {
		sortBy = string(*params.SortBy)
	}

	sortOrder := "desc"
	if params.SortOrder != nil {
		sortOrder = string(*params.SortOrder)
	}

	provider := ""
	if params.Provider != nil {
		provider = *params.Provider
	}

	email := ""
	if params.Email != nil {
		email = *params.Email
	}

	// Build filter
	filter := UserFilter{
		Provider:        provider,
		Email:           email,
		CreatedAfter:    params.CreatedAfter,
		CreatedBefore:   params.CreatedBefore,
		LastLoginAfter:  params.LastLoginAfter,
		LastLoginBefore: params.LastLoginBefore,
		Limit:           limit,
		Offset:          offset,
		SortBy:          sortBy,
		SortOrder:       sortOrder,
	}

	// Get users from store
	users, err := GlobalUserStore.List(c.Request.Context(), filter)
	if err != nil {
		logger.Error("Failed to list users: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to list users",
		})
		return
	}

	// Get total count
	total, err := GlobalUserStore.Count(c.Request.Context(), filter)
	if err != nil {
		logger.Warn("Failed to count users: %v", err)
		total = len(users) // Fallback to current page count
	}

	// Enrich with related data
	enriched, err := GlobalUserStore.EnrichUsers(c.Request.Context(), users)
	if err != nil {
		logger.Warn("Failed to enrich users: %v", err)
		enriched = users // Continue with non-enriched data
	}

	// Return response
	c.JSON(http.StatusOK, gin.H{
		"users":  enriched,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetAdminUser handles GET /admin/users/{internal_uuid}
func (s *Server) GetAdminUser(c *gin.Context, internalUuid openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	// Convert openapi_types.UUID to google/uuid
	internalUUID, err := uuid.Parse(internalUuid.String())
	if err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_uuid",
			Message: "internal_uuid must be a valid UUID",
		})
		return
	}

	// Get user from store
	user, err := GlobalUserStore.Get(c.Request.Context(), internalUUID)
	if err != nil {
		if err.Error() == "user not found" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "User not found",
			})
		} else {
			logger.Error("Failed to get user: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to get user",
			})
		}
		return
	}

	// Enrich with related data
	enriched, err := GlobalUserStore.EnrichUsers(c.Request.Context(), []AdminUser{*user})
	if err != nil {
		logger.Warn("Failed to enrich user: %v", err)
		// Return non-enriched user
		c.JSON(http.StatusOK, user)
		return
	}

	if len(enriched) > 0 {
		c.JSON(http.StatusOK, enriched[0])
	} else {
		c.JSON(http.StatusOK, user)
	}
}

// Note: UpdateAdminUserRequest is now generated from OpenAPI spec in api.go

// UpdateAdminUser handles PATCH /admin/users/{internal_uuid}
func (s *Server) UpdateAdminUser(c *gin.Context, internalUuid openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	// Convert openapi_types.UUID to google/uuid
	internalUUID, err := uuid.Parse(internalUuid.String())
	if err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_uuid",
			Message: "internal_uuid must be a valid UUID",
		})
		return
	}

	// Parse request body
	var req UpdateAdminUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: fmt.Sprintf("Invalid request body: %v", err),
		})
		return
	}

	// Get current user data
	user, err := GlobalUserStore.Get(c.Request.Context(), internalUUID)
	if err != nil {
		if err.Error() == "user not found" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "User not found",
			})
		} else {
			logger.Error("Failed to get user: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to get user",
			})
		}
		return
	}

	// Get actor information for audit logging
	actorUserID := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")

	// Track what changed for audit log
	changes := []string{}

	// Apply updates
	if req.Email != nil && string(*req.Email) != string(user.Email) {
		changes = append(changes, fmt.Sprintf("email: %s -> %s", user.Email, *req.Email))
		user.Email = *req.Email
	}

	if req.Name != nil && *req.Name != user.Name {
		changes = append(changes, fmt.Sprintf("name: %s -> %s", user.Name, *req.Name))
		user.Name = *req.Name
	}

	if req.EmailVerified != nil && *req.EmailVerified != user.EmailVerified {
		changes = append(changes, fmt.Sprintf("email_verified: %v -> %v", user.EmailVerified, *req.EmailVerified))
		user.EmailVerified = *req.EmailVerified
	}

	// If no changes, return current user
	if len(changes) == 0 {
		c.JSON(http.StatusOK, user)
		return
	}

	// Update in database
	err = GlobalUserStore.Update(c.Request.Context(), *user)
	if err != nil {
		if err.Error() == "user not found" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "User not found",
			})
		} else {
			logger.Error("Failed to update user: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to update user",
			})
		}
		return
	}

	// AUDIT LOG: Log update with actor details and changes
	logger.Info("[AUDIT] User updated: internal_uuid=%s, provider=%s, provider_user_id=%s, email=%s, updated_by=%s (email=%s), changes=[%v]",
		user.InternalUuid, user.Provider, user.ProviderUserId, user.Email, actorUserID, actorEmail, changes)

	// Return updated user
	c.JSON(http.StatusOK, user)
}

// DeleteAdminUser handles DELETE /admin/users/{internal_uuid}
func (s *Server) DeleteAdminUser(c *gin.Context, internalUuid openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	// Get actor information for audit logging
	actorUserID := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")

	// Lookup user by internal UUID to get provider and provider_user_id
	user, err := GlobalUserStore.Get(c.Request.Context(), internalUuid)
	if err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "User not found",
		})
		return
	}

	// Delete user (delegates to auth service)
	stats, err := GlobalUserStore.Delete(c.Request.Context(), user.Provider, user.ProviderUserId)
	if err != nil {
		if err.Error() == "failed to find user: user not found" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "User not found",
			})
		} else {
			logger.Error("Failed to delete user: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to delete user",
			})
		}
		return
	}

	// AUDIT LOG: Log deletion with actor details and statistics
	logger.Info("[AUDIT] Admin user deletion: internal_uuid=%s, provider=%s, provider_id=%s, email=%s, deleted_by=%s (email=%s), transferred=%d, deleted=%d",
		internalUuid, user.Provider, user.ProviderUserId, stats.UserEmail, actorUserID, actorEmail, stats.ThreatModelsTransferred, stats.ThreatModelsDeleted)

	// Return 204 No Content for successful deletion
	c.Status(http.StatusNoContent)
}
