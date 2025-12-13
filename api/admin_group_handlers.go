package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ListAdminGroups handles GET /admin/groups
func (s *Server) ListAdminGroups(c *gin.Context, params ListAdminGroupsParams) {
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

	sortBy := "group_name"
	if params.SortBy != nil {
		sortBy = string(*params.SortBy)
	}

	sortOrder := "asc"
	if params.SortOrder != nil {
		sortOrder = string(*params.SortOrder)
	}

	provider := ""
	if params.Provider != nil {
		provider = *params.Provider
	}

	groupName := ""
	if params.GroupName != nil {
		groupName = *params.GroupName
	}

	// Build filter
	filter := GroupFilter{
		Provider:             provider,
		GroupName:            groupName,
		UsedInAuthorizations: params.UsedInAuthorizations,
		Limit:                limit,
		Offset:               offset,
		SortBy:               sortBy,
		SortOrder:            sortOrder,
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
func (s *Server) GetAdminGroup(c *gin.Context, internalUuid openapi_types.UUID) {
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

// Note: CreateAdminGroupRequest is now generated from OpenAPI spec in api.go

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

	// Create group (provider-independent groups use "*")
	description := ""
	if req.Description != nil {
		description = *req.Description
	}

	group := Group{
		InternalUUID: uuid.New(),
		Provider:     "*", // Provider-independent group
		GroupName:    req.GroupName,
		Name:         req.Name,
		Description:  description,
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
	auditLogger := NewAuditLogger()
	auditCtx := &AuditContext{
		ActorUserID: actorUserID,
		ActorEmail:  actorEmail,
	}
	auditLogger.LogCreate(auditCtx, "Group", group.InternalUUID.String(), map[string]interface{}{
		"provider":   group.Provider,
		"group_name": group.GroupName,
	})

	c.JSON(http.StatusCreated, group)
}

// Note: UpdateAdminGroupRequest is now generated from OpenAPI spec in api.go

// UpdateAdminGroup handles PATCH /admin/groups/{internal_uuid}
func (s *Server) UpdateAdminGroup(c *gin.Context, internalUuid openapi_types.UUID) {
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
	auditLogger := NewAuditLogger()
	auditCtx := &AuditContext{
		ActorUserID: actorUserID,
		ActorEmail:  actorEmail,
	}
	auditLogger.LogUpdate(auditCtx, "Group", group.InternalUUID.String(), changes)

	// Return updated group
	c.JSON(http.StatusOK, group)
}

// DeleteAdminGroup handles DELETE /admin/groups/{internal_uuid}
func (s *Server) DeleteAdminGroup(c *gin.Context, internalUuid openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	// Get actor information for audit logging
	actorUserID := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")

	// Lookup group by internal UUID to get group_name
	group, err := GlobalGroupStore.Get(c.Request.Context(), internalUuid)
	if err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Group not found",
		})
		return
	}

	// Validate not deleting "everyone"
	if group.GroupName == ProtectedGroupEveryone {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "protected_group",
			Message: "Cannot delete protected group: everyone",
		})
		return
	}

	// Delete group (delegates to auth service)
	stats, err := GlobalGroupStore.Delete(c.Request.Context(), group.GroupName)
	if err != nil {
		if err.Error() == "group not found: "+group.GroupName {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "Group not found",
			})
		} else if err.Error() == "cannot delete protected group: everyone" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusForbidden,
				Code:    "protected_group",
				Message: "Cannot delete protected group: everyone",
			})
		} else {
			logger.Error("Failed to delete group: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to delete group",
			})
		}
		return
	}

	// AUDIT LOG: Log deletion with actor details and statistics
	logger.Info("[AUDIT] Admin group deletion: internal_uuid=%s, group_name=%s, deleted_by=%s (email=%s), threat_models_deleted=%d, threat_models_retained=%d",
		internalUuid, group.GroupName, actorUserID, actorEmail, stats.ThreatModelsDeleted, stats.ThreatModelsRetained)

	// Return 204 No Content for successful deletion
	c.Status(http.StatusNoContent)
}
