package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ListAdminUsers handles GET /admin/users
func (s *Server) ListAdminUsers(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Parse query parameters
	provider := c.Query("provider")
	email := c.Query("email")
	createdAfterStr := c.Query("created_after")
	createdBeforeStr := c.Query("created_before")
	lastLoginAfterStr := c.Query("last_login_after")
	lastLoginBeforeStr := c.Query("last_login_before")
	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	// Parse limit and offset
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 0 || limit > 200 {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_limit",
			Message: "limit must be between 0 and 200",
		})
		return
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_offset",
			Message: "offset must be a non-negative integer",
		})
		return
	}

	// Build filter
	filter := UserFilter{
		Provider:  provider,
		Email:     email,
		Limit:     limit,
		Offset:    offset,
		SortBy:    sortBy,
		SortOrder: sortOrder,
	}

	// Parse optional date filters
	if createdAfterStr != "" {
		createdAfter, err := time.Parse(time.RFC3339, createdAfterStr)
		if err != nil {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_created_after",
				Message: "created_after must be in RFC3339 format",
			})
			return
		}
		filter.CreatedAfter = &createdAfter
	}

	if createdBeforeStr != "" {
		createdBefore, err := time.Parse(time.RFC3339, createdBeforeStr)
		if err != nil {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_created_before",
				Message: "created_before must be in RFC3339 format",
			})
			return
		}
		filter.CreatedBefore = &createdBefore
	}

	if lastLoginAfterStr != "" {
		lastLoginAfter, err := time.Parse(time.RFC3339, lastLoginAfterStr)
		if err != nil {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_last_login_after",
				Message: "last_login_after must be in RFC3339 format",
			})
			return
		}
		filter.LastLoginAfter = &lastLoginAfter
	}

	if lastLoginBeforeStr != "" {
		lastLoginBefore, err := time.Parse(time.RFC3339, lastLoginBeforeStr)
		if err != nil {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_last_login_before",
				Message: "last_login_before must be in RFC3339 format",
			})
			return
		}
		filter.LastLoginBefore = &lastLoginBefore
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
func (s *Server) GetAdminUser(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Parse internal_uuid from path parameter
	internalUUIDStr := c.Param("internal_uuid")
	internalUUID, err := uuid.Parse(internalUUIDStr)
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
	enriched, err := GlobalUserStore.EnrichUsers(c.Request.Context(), []User{*user})
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

// UpdateAdminUserRequest represents the request body for updating a user
type UpdateAdminUserRequest struct {
	Email         *string `json:"email,omitempty"`
	Name          *string `json:"name,omitempty"`
	EmailVerified *bool   `json:"email_verified,omitempty"`
}

// UpdateAdminUser handles PATCH /admin/users/{internal_uuid}
func (s *Server) UpdateAdminUser(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Parse internal_uuid from path parameter
	internalUUIDStr := c.Param("internal_uuid")
	internalUUID, err := uuid.Parse(internalUUIDStr)
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
	if req.Email != nil && *req.Email != user.Email {
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
		user.InternalUUID, user.Provider, user.ProviderUserID, user.Email, actorUserID, actorEmail, changes)

	// Return updated user
	c.JSON(http.StatusOK, user)
}

// DeleteAdminUser handles DELETE /admin/users?provider={provider}&provider_id={provider_id}
func (s *Server) DeleteAdminUser(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Parse query parameters
	provider := c.Query("provider")
	providerID := c.Query("provider_id")

	if provider == "" || providerID == "" {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "missing_parameters",
			Message: "Both provider and provider_id query parameters are required",
		})
		return
	}

	// Get actor information for audit logging
	actorUserID := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")

	// Delete user (delegates to auth service)
	stats, err := GlobalUserStore.Delete(c.Request.Context(), provider, providerID)
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
	logger.Info("[AUDIT] Admin user deletion: provider=%s, provider_id=%s, email=%s, deleted_by=%s (email=%s), transferred=%d, deleted=%d",
		provider, providerID, stats.UserEmail, actorUserID, actorEmail, stats.ThreatModelsTransferred, stats.ThreatModelsDeleted)

	// Return 204 No Content for successful deletion
	c.Status(http.StatusNoContent)
}
