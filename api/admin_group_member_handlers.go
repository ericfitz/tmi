package api

import (
	"errors"
	"fmt"
	"net/http"

	repository "github.com/ericfitz/tmi/auth/repository"
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
	_, err = GlobalGroupRepository.Get(c.Request.Context(), groupUUID)
	if err != nil {
		HandleRequestError(c, StoreErrorToRequestError(err, "Group not found", "Failed to get group"))
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
	members, err := GlobalGroupMemberRepository.ListMembers(c.Request.Context(), filter)
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
	total, err := GlobalGroupMemberRepository.CountMembers(c.Request.Context(), groupUUID)
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

	// Get actor information for audit logging
	actorUserIDStr := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")

	var actorUserUUID *uuid.UUID
	if actorUserIDStr != "" {
		if actorUUID, err := uuid.Parse(actorUserIDStr); err == nil {
			actorUserUUID = &actorUUID
		}
	}

	var notes *string
	if req.Notes != nil {
		notes = req.Notes
	}

	// Determine subject type (default to "user" for backward compatibility)
	subjectType := string(AddGroupMemberRequestSubjectTypeUser)
	if req.SubjectType != nil {
		subjectType = string(*req.SubjectType)
	}

	var member *GroupMember

	if subjectType == string(AddGroupMemberRequestSubjectTypeGroup) {
		// Adding a group as a member
		if req.MemberGroupInternalUuid == nil {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_request",
				Message: "member_group_internal_uuid is required when subject_type is group",
			})
			return
		}
		memberGroupUUID, err := uuid.Parse(req.MemberGroupInternalUuid.String())
		if err != nil {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_uuid",
				Message: "member_group_internal_uuid must be a valid UUID",
			})
			return
		}

		member, err = GlobalGroupMemberRepository.AddGroupMember(c.Request.Context(), groupUUID, memberGroupUUID, actorUserUUID, notes)
		if err != nil {
			s.handleGroupMemberError(c, logger, err)
			return
		}

		logger.Info("[AUDIT] Group member added: group_uuid=%s, member_group_uuid=%s, added_by=%s (email=%s)",
			groupUUID, memberGroupUUID, actorUserIDStr, actorEmail)
	} else {
		// Adding a user as a member
		if req.UserInternalUuid == nil {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_request",
				Message: "user_internal_uuid is required when subject_type is user",
			})
			return
		}
		userUUID, err := uuid.Parse(req.UserInternalUuid.String())
		if err != nil {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_uuid",
				Message: "user_internal_uuid must be a valid UUID",
			})
			return
		}

		member, err = GlobalGroupMemberRepository.AddMember(c.Request.Context(), groupUUID, userUUID, actorUserUUID, notes)
		if err != nil {
			s.handleGroupMemberError(c, logger, err)
			return
		}

		logger.Info("[AUDIT] Group member added: group_uuid=%s, user_uuid=%s, added_by=%s (email=%s)",
			groupUUID, userUUID, actorUserIDStr, actorEmail)
	}

	c.JSON(http.StatusCreated, member)
}

// RemoveGroupMember handles DELETE /admin/groups/{internal_uuid}/members/{member_uuid}
func (s *Server) RemoveGroupMember(c *gin.Context, internalUuid openapi_types.UUID, memberUuid openapi_types.UUID, params RemoveGroupMemberParams) {
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

	memberUUID, err := uuid.Parse(memberUuid.String())
	if err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_uuid",
			Message: "member_uuid must be a valid UUID",
		})
		return
	}

	// Get actor information for audit logging
	actorUserID := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")

	// Determine subject type (default to "user" for backward compatibility)
	subjectType := string(AddGroupMemberRequestSubjectTypeUser)
	if params.SubjectType != nil {
		subjectType = string(*params.SubjectType)
	}

	// Remove member from group based on subject type
	if subjectType == string(AddGroupMemberRequestSubjectTypeGroup) {
		err = GlobalGroupMemberRepository.RemoveGroupMember(c.Request.Context(), groupUUID, memberUUID)
	} else {
		err = GlobalGroupMemberRepository.RemoveMember(c.Request.Context(), groupUUID, memberUUID)
	}

	if err != nil {
		switch {
		case errors.Is(err, ErrGroupMemberNotFound):
			HandleRequestError(c, NotFoundError("Membership not found"))
		case errors.Is(err, ErrEveryoneGroup):
			HandleRequestError(c, &RequestError{
				Status:  http.StatusForbidden,
				Code:    "forbidden",
				Message: "Cannot remove members from the 'everyone' pseudo-group",
			})
		default:
			logger.Error("Failed to remove group member: %v", err)
			HandleRequestError(c, ServerError("Failed to remove group member"))
		}
		return
	}

	// AUDIT LOG: Log removal with actor details
	logger.Info("[AUDIT] Group member removed: group_uuid=%s, member_uuid=%s, subject_type=%s, removed_by=%s (email=%s)",
		groupUUID, memberUUID, subjectType, actorUserID, actorEmail)

	// Return 204 No Content for successful deletion
	c.Status(http.StatusNoContent)
}

// handleGroupMemberError maps group member repository errors to HTTP responses
func (s *Server) handleGroupMemberError(c *gin.Context, logger *slogging.ContextLogger, err error) {
	switch {
	case errors.Is(err, ErrGroupNotFound):
		HandleRequestError(c, NotFoundError("Group not found"))
	case errors.Is(err, repository.ErrUserNotFound):
		HandleRequestError(c, NotFoundError("User not found"))
	case errors.Is(err, ErrGroupMemberDuplicate):
		HandleRequestError(c, &RequestError{
			Status:  http.StatusConflict,
			Code:    "duplicate_membership",
			Message: "Already a member of this group",
		})
	case errors.Is(err, ErrEveryoneGroup):
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "Cannot add members to the 'everyone' pseudo-group",
		})
	case errors.Is(err, ErrSelfMembership):
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "A group cannot be a member of itself",
		})
	default:
		logger.Error("Failed to add group member: %v", err)
		HandleRequestError(c, ServerError("Failed to add group member"))
	}
}
