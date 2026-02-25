package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ListMyGroups handles GET /me/groups
// Returns the TMI-managed groups that the authenticated user belongs to.
func (s *Server) ListMyGroups(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	internalUUIDStr, err := GetUserInternalUUID(c)
	if err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required",
		})
		return
	}

	userUUID, err := uuid.Parse(internalUUIDStr)
	if err != nil {
		logger.Error("ListMyGroups: invalid user UUID format: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to resolve user identity",
		})
		return
	}

	groups, err := GlobalGroupMemberStore.GetGroupsForUser(c.Request.Context(), userUUID)
	if err != nil {
		logger.Error("ListMyGroups: failed to get groups for user: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to list groups",
		})
		return
	}

	// Map store Group objects to UserGroupMembership API type
	memberships := make([]UserGroupMembership, len(groups))
	for i, g := range groups {
		memberships[i] = UserGroupMembership{
			InternalUuid: g.InternalUUID,
			GroupName:    g.GroupName,
		}
		if g.Name != "" {
			name := g.Name
			memberships[i].Name = &name
		}
	}

	c.JSON(http.StatusOK, MyGroupListResponse{
		Groups: memberships,
		Total:  len(memberships),
	})
}

// ListMyGroupMembers handles GET /me/groups/{internal_uuid}/members
// Returns a paginated list of members for a group the authenticated user belongs to.
// Admin audit fields (added_by, notes) are redacted from the response.
func (s *Server) ListMyGroupMembers(c *gin.Context, internalUuid openapi_types.UUID, params ListMyGroupMembersParams) {
	logger := slogging.Get().WithContext(c)

	// Parse and validate group UUID
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
		if err.Error() == ErrMsgGroupNotFound {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "Group not found",
			})
		} else {
			logger.Error("ListMyGroupMembers: failed to get group: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to get group",
			})
		}
		return
	}

	// Check if this is the "everyone" pseudo-group (all authenticated users are implicit members)
	if groupUUID.String() != EveryonePseudoGroupUUID {
		// Resolve the authenticated user's membership context
		mc, err := ResolveMembershipContext(c)
		if err != nil {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusUnauthorized,
				Code:    "unauthorized",
				Message: "Authentication required",
			})
			return
		}

		// Check effective membership (direct or via nested group)
		isMember, err := GlobalGroupMemberStore.IsEffectiveMember(
			c.Request.Context(), groupUUID, mc.UserUUID, mc.GroupUUIDs,
		)
		if err != nil {
			logger.Error("ListMyGroupMembers: failed to check group membership: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to verify group membership",
			})
			return
		}
		if !isMember {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusForbidden,
				Code:    "forbidden",
				Message: "Not a member of this group",
			})
			return
		}
	}

	// Parse pagination parameters
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

	// Query members
	filter := GroupMemberFilter{
		GroupInternalUUID: groupUUID,
		Limit:             limit,
		Offset:            offset,
	}
	members, err := GlobalGroupMemberStore.ListMembers(c.Request.Context(), filter)
	if err != nil {
		logger.Error("ListMyGroupMembers: failed to list group members: %v", err)
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
		logger.Warn("ListMyGroupMembers: failed to count group members: %v", err)
		total = len(members)
	}

	// Redact admin audit fields
	for i := range members {
		members[i].AddedByInternalUuid = nil
		members[i].AddedByEmail = nil
		members[i].Notes = nil
	}

	c.JSON(http.StatusOK, gin.H{
		"members": members,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}
