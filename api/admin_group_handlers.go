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

// ListAdminGroups handles GET /admin/groups
func (s *Server) ListAdminGroups(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Parse query parameters
	provider := c.Query("provider")
	groupName := c.Query("name")
	usedInAuthStr := c.Query("used_in_authorizations")
	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")
	sortBy := c.DefaultQuery("sort_by", "last_used")
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
	filter := GroupFilter{
		Provider:  provider,
		GroupName: groupName,
		Limit:     limit,
		Offset:    offset,
		SortBy:    sortBy,
		SortOrder: sortOrder,
	}

	// Parse optional used_in_authorizations filter
	if usedInAuthStr != "" {
		usedInAuth, err := strconv.ParseBool(usedInAuthStr)
		if err != nil {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_used_in_authorizations",
				Message: "used_in_authorizations must be true or false",
			})
			return
		}
		filter.UsedInAuthorizations = &usedInAuth
	}

	// Get groups from store
	groups, err := GlobalGroupStore.List(c.Request.Context(), filter)
	if err != nil {
		logger.Error("Failed to list groups: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to list groups",
		})
		return
	}

	// Get total count
	total, err := GlobalGroupStore.Count(c.Request.Context(), filter)
	if err != nil {
		logger.Warn("Failed to count groups: %v", err)
		total = len(groups) // Fallback to current page count
	}

	// Enrich with related data
	enriched, err := GlobalGroupStore.EnrichGroups(c.Request.Context(), groups)
	if err != nil {
		logger.Warn("Failed to enrich groups: %v", err)
		enriched = groups // Continue with non-enriched data
	}

	// Return response
	c.JSON(http.StatusOK, gin.H{
		"groups": enriched,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetAdminGroup handles GET /admin/groups/{internal_uuid}
func (s *Server) GetAdminGroup(c *gin.Context) {
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

	// Get group from store
	group, err := GlobalGroupStore.Get(c.Request.Context(), internalUUID)
	if err != nil {
		if err.Error() == "group not found" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "Group not found",
			})
		} else {
			logger.Error("Failed to get group: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to get group",
			})
		}
		return
	}

	// Enrich with related data
	enriched, err := GlobalGroupStore.EnrichGroups(c.Request.Context(), []Group{*group})
	if err != nil {
		logger.Warn("Failed to enrich group: %v", err)
		// Return non-enriched group
		c.JSON(http.StatusOK, group)
		return
	}

	if len(enriched) > 0 {
		c.JSON(http.StatusOK, enriched[0])
	} else {
		c.JSON(http.StatusOK, group)
	}
}

// CreateAdminGroupRequest represents the request body for creating a group
type CreateAdminGroupRequest struct {
	Provider    string `json:"provider" binding:"required"`
	GroupName   string `json:"group_name" binding:"required"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// CreateAdminGroup handles POST /admin/groups
func (s *Server) CreateAdminGroup(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Parse request body
	var req CreateAdminGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: fmt.Sprintf("Invalid request body: %v", err),
		})
		return
	}

	// Validate group_name
	if len(req.GroupName) == 0 || len(req.GroupName) > 256 {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_group_name",
			Message: "group_name must be between 1 and 256 characters",
		})
		return
	}

	// Get actor information for audit logging
	actorUserID := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")

	// Create group
	group := Group{
		InternalUUID: uuid.New(),
		Provider:     req.Provider,
		GroupName:    req.GroupName,
		Name:         req.Name,
		Description:  req.Description,
		FirstUsed:    time.Now().UTC(),
		LastUsed:     time.Now().UTC(),
		UsageCount:   1,
	}

	// Create in database
	err := GlobalGroupStore.Create(c.Request.Context(), group)
	if err != nil {
		if err.Error() == "group already exists for provider" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusConflict,
				Code:    "duplicate_group",
				Message: "Group already exists for this provider",
			})
		} else {
			logger.Error("Failed to create group: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to create group",
			})
		}
		return
	}

	// AUDIT LOG: Log creation with actor details
	logger.Info("[AUDIT] Group created: internal_uuid=%s, provider=%s, group_name=%s, created_by=%s (email=%s)",
		group.InternalUUID, group.Provider, group.GroupName, actorUserID, actorEmail)

	c.JSON(http.StatusCreated, group)
}

// UpdateAdminGroupRequest represents the request body for updating a group
type UpdateAdminGroupRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// UpdateAdminGroup handles PATCH /admin/groups/{internal_uuid}
func (s *Server) UpdateAdminGroup(c *gin.Context) {
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
	var req UpdateAdminGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: fmt.Sprintf("Invalid request body: %v", err),
		})
		return
	}

	// Get current group data
	group, err := GlobalGroupStore.Get(c.Request.Context(), internalUUID)
	if err != nil {
		if err.Error() == "group not found" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "Group not found",
			})
		} else {
			logger.Error("Failed to get group: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to get group",
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
	if req.Name != nil && *req.Name != group.Name {
		changes = append(changes, fmt.Sprintf("name: %s -> %s", group.Name, *req.Name))
		group.Name = *req.Name
	}

	if req.Description != nil && *req.Description != group.Description {
		changes = append(changes, fmt.Sprintf("description: %s -> %s", group.Description, *req.Description))
		group.Description = *req.Description
	}

	// If no changes, return current group
	if len(changes) == 0 {
		c.JSON(http.StatusOK, group)
		return
	}

	// Update in database
	err = GlobalGroupStore.Update(c.Request.Context(), *group)
	if err != nil {
		if err.Error() == "group not found" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "Group not found",
			})
		} else {
			logger.Error("Failed to update group: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to update group",
			})
		}
		return
	}

	// AUDIT LOG: Log update with actor details and changes
	logger.Info("[AUDIT] Group updated: internal_uuid=%s, provider=%s, group_name=%s, updated_by=%s (email=%s), changes=[%v]",
		group.InternalUUID, group.Provider, group.GroupName, actorUserID, actorEmail, changes)

	// Return updated group
	c.JSON(http.StatusOK, group)
}

// DeleteAdminGroup handles DELETE /admin/groups?provider={provider}&group_name={group_name}
func (s *Server) DeleteAdminGroup(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Parse query parameters
	provider := c.Query("provider")
	groupName := c.Query("group_name")

	if provider == "" || groupName == "" {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "missing_parameters",
			Message: "Both provider and group_name query parameters are required",
		})
		return
	}

	// Get actor information for audit logging (for future implementation)
	actorUserID := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")

	// Log the attempted deletion
	logger.Info("[AUDIT] Group deletion attempted (not implemented): provider=%s, group_name=%s, requested_by=%s (email=%s)",
		provider, groupName, actorUserID, actorEmail)

	// Return 501 Not Implemented (placeholder)
	HandleRequestError(c, &RequestError{
		Status:  http.StatusNotImplemented,
		Code:    "not_implemented",
		Message: "Group deletion is not yet implemented",
	})
}
