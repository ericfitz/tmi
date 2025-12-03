package api

import (
	"fmt"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ListGroupMembers handles GET /admin/groups/{internal_uuid}/members
func (s *Server) ListGroupMembers(c *gin.Context, internalUuid openapi_types.UUID, params ListGroupMembersParams) {
	logger := slogging.Get().WithContext(c)

	// Convert openapi_types.UUID to google/uuid
	groupUUID, err := uuid.Parse(internalUuid.String())
	if err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_uuid",
			Message: "internal_uuid must be a valid UUID",
		})
		return
	}

	// Verify group exists
	_, err = GlobalGroupStore.Get(c.Request.Context(), groupUUID)
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

	// Extract pagination parameters
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

	// Build filter
	filter := GroupMemberFilter{
		GroupInternalUUID: groupUUID,
		Limit:             limit,
		Offset:            offset,
	}

	// Get members from store
	members, err := GlobalGroupMemberStore.ListMembers(c.Request.Context(), filter)
	if err != nil {
		logger.Error("Failed to list group members: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to list group members",
		})
		return
	}

	// Get total count
	total, err := GlobalGroupMemberStore.CountMembers(c.Request.Context(), groupUUID)
	if err != nil {
		logger.Warn("Failed to count group members: %v", err)
		total = len(members) // Fallback to current page count
	}

	// Return response
	c.JSON(http.StatusOK, gin.H{
		"members": members,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// AddGroupMember handles POST /admin/groups/{internal_uuid}/members
func (s *Server) AddGroupMember(c *gin.Context, internalUuid openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	// Convert openapi_types.UUID to google/uuid
	groupUUID, err := uuid.Parse(internalUuid.String())
	if err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_uuid",
			Message: "internal_uuid must be a valid UUID",
		})
		return
	}

	// Parse request body
	var req AddGroupMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: fmt.Sprintf("Invalid request body: %v", err),
		})
		return
	}

	// Convert user UUID
	userUUID, err := uuid.Parse(req.UserInternalUuid.String())
	if err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_uuid",
			Message: "user_internal_uuid must be a valid UUID",
		})
		return
	}

	// Get actor information for audit logging
	actorUserIDStr := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")

	var actorUserUUID *uuid.UUID
	if actorUserIDStr != "" {
		if actorUUID, err := uuid.Parse(actorUserIDStr); err == nil {
			actorUserUUID = &actorUUID
		}
	}

	// Add member to group
	var notes *string
	if req.Notes != nil {
		notes = req.Notes
	}

	member, err := GlobalGroupMemberStore.AddMember(c.Request.Context(), groupUUID, userUUID, actorUserUUID, notes)
	if err != nil {
		if err.Error() == "group not found" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "Group not found",
			})
		} else if err.Error() == "user not found" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "User not found",
			})
		} else if err.Error() == "user is already a member of this group" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusConflict,
				Code:    "duplicate_membership",
				Message: "User is already a member of this group",
			})
		} else if err.Error() == "cannot add members to the 'everyone' pseudo-group" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusForbidden,
				Code:    "forbidden",
				Message: "Cannot add members to the 'everyone' pseudo-group",
			})
		} else {
			logger.Error("Failed to add group member: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to add group member",
			})
		}
		return
	}

	// AUDIT LOG: Log addition with actor details
	logger.Info("[AUDIT] Group member added: group_uuid=%s, user_uuid=%s, user_email=%s, added_by=%s (email=%s)",
		groupUUID, userUUID, member.UserEmail, actorUserIDStr, actorEmail)

	c.JSON(http.StatusCreated, member)
}

// RemoveGroupMember handles DELETE /admin/groups/{internal_uuid}/members/{user_uuid}
func (s *Server) RemoveGroupMember(c *gin.Context, internalUuid openapi_types.UUID, userUuid openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	// Convert openapi_types.UUID to google/uuid
	groupUUID, err := uuid.Parse(internalUuid.String())
	if err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_uuid",
			Message: "internal_uuid must be a valid UUID",
		})
		return
	}

	userUUID, err := uuid.Parse(userUuid.String())
	if err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_uuid",
			Message: "user_uuid must be a valid UUID",
		})
		return
	}

	// Get actor information for audit logging
	actorUserID := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")

	// Remove member from group
	err = GlobalGroupMemberStore.RemoveMember(c.Request.Context(), groupUUID, userUUID)
	if err != nil {
		if err.Error() == "membership not found" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "Membership not found",
			})
		} else if err.Error() == "cannot remove members from the 'everyone' pseudo-group" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusForbidden,
				Code:    "forbidden",
				Message: "Cannot remove members from the 'everyone' pseudo-group",
			})
		} else {
			logger.Error("Failed to remove group member: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to remove group member",
			})
		}
		return
	}

	// AUDIT LOG: Log removal with actor details
	logger.Info("[AUDIT] Group member removed: group_uuid=%s, user_uuid=%s, removed_by=%s (email=%s)",
		groupUUID, userUUID, actorUserID, actorEmail)

	// Return 204 No Content for successful deletion
	c.Status(http.StatusNoContent)
}
