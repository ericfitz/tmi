package api

import (
	"fmt"
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// groupMemberToAdministrator converts a models.GroupMember (with preloaded relationships)
// to the Administrator API type for backwards-compatible API responses.
func groupMemberToAdministrator(m models.GroupMember) Administrator {
	admin := Administrator{
		Id:        uuid.MustParse(m.ID),
		CreatedAt: m.AddedAt,
	}

	if m.SubjectType == "user" && m.UserInternalUUID != nil {
		userUUID := uuid.MustParse(*m.UserInternalUUID)
		admin.UserId = &userUUID
		if m.User != nil {
			admin.UserEmail = &m.User.Email
			admin.UserName = &m.User.Name
			admin.Provider = m.User.Provider
		}
	} else if m.SubjectType == "group" && m.MemberGroupInternalUUID != nil {
		groupUUID := uuid.MustParse(*m.MemberGroupInternalUUID)
		admin.GroupId = &groupUUID
		if m.MemberGroup != nil {
			if m.MemberGroup.Name != nil {
				admin.GroupName = m.MemberGroup.Name
			} else {
				admin.GroupName = &m.MemberGroup.GroupName
			}
			admin.Provider = m.MemberGroup.Provider
		}
	}

	return admin
}

// ListAdministrators handles GET /admin/administrators
// Wraps listing of members in the Administrators built-in group.
func (s *Server) ListAdministrators(c *gin.Context, params ListAdministratorsParams) {
	logger := slogging.Get().WithContext(c)

	// Validate pagination parameters
	if err := ValidatePaginationParams(params.Limit, params.Offset); err != nil {
		HandleRequestError(c, err)
		return
	}

	limit := 50
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}

	if adminDB == nil {
		logger.Error("adminDB not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Database not initialized",
		})
		return
	}

	// Query group members of the Administrators group with relationship preloading
	query := adminDB.Where("group_members.group_internal_uuid = ?", AdministratorsGroupUUID).
		Preload("User").Preload("MemberGroup")

	// Apply optional filters
	if params.UserId != nil {
		query = query.Where("group_members.subject_type = ? AND group_members.user_internal_uuid = ?", "user", params.UserId.String())
	}
	if params.GroupId != nil {
		query = query.Where("group_members.subject_type = ? AND group_members.member_group_internal_uuid = ?", "group", params.GroupId.String())
	}
	if params.Provider != nil {
		query = query.Where(
			"(group_members.subject_type = 'user' AND group_members.user_internal_uuid IN (SELECT internal_uuid FROM users WHERE provider = ?)) OR "+
				"(group_members.subject_type = 'group' AND group_members.member_group_internal_uuid IN (SELECT internal_uuid FROM groups WHERE provider = ?))",
			*params.Provider, *params.Provider)
	}

	// Count total before pagination
	var total int64
	if err := query.Session(&gorm.Session{}).Model(&models.GroupMember{}).Count(&total).Error; err != nil {
		logger.Warn("Failed to count administrators: %v", err)
		total = 0
	}

	// Apply pagination and fetch
	var members []models.GroupMember
	if err := query.Order("group_members.added_at DESC").Limit(limit).Offset(offset).Find(&members).Error; err != nil {
		logger.Error("Failed to list administrators: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to list administrators",
		})
		return
	}

	// Convert to Administrator API type
	apiAdmins := make([]Administrator, 0, len(members))
	for _, m := range members {
		apiAdmins = append(apiAdmins, groupMemberToAdministrator(m))
	}

	c.JSON(http.StatusOK, ListAdministratorsResponse{
		Administrators: apiAdmins,
		Total:          int(total),
		Limit:          limit,
		Offset:         offset,
	})
}

// CreateAdministrator handles POST /admin/administrators
// Wraps adding a user or group as a member of the Administrators built-in group.
func (s *Server) CreateAdministrator(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Parse request body
	var req CreateAdministratorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: fmt.Sprintf("Invalid request body: %v", err),
		})
		return
	}

	// Extract the identification field from the oneOf union
	var email *string
	var providerUserID *string
	var groupName *string

	if emailVariant, err := req.AsCreateAdministratorRequest0(); err == nil && emailVariant.Email != "" {
		emailStr := string(emailVariant.Email)
		email = &emailStr
	}
	if providerIDVariant, err := req.AsCreateAdministratorRequest1(); err == nil && providerIDVariant.ProviderUserId != "" {
		providerUserID = &providerIDVariant.ProviderUserId
	}
	if groupVariant, err := req.AsCreateAdministratorRequest2(); err == nil && groupVariant.GroupName != "" {
		groupName = &groupVariant.GroupName
	}

	// Count how many identification fields are set
	fieldCount := 0
	if email != nil {
		fieldCount++
	}
	if providerUserID != nil {
		fieldCount++
	}
	if groupName != nil {
		fieldCount++
	}

	if fieldCount == 0 {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "one of email, provider_user_id, or group_name must be specified",
		})
		return
	}
	if fieldCount > 1 {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "only one of email, provider_user_id, or group_name can be specified",
		})
		return
	}

	// Get actor information
	actorUserID := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")
	var addedByUUID *uuid.UUID
	if actorUserID != "" {
		parsed, err := uuid.Parse(actorUserID)
		if err == nil {
			addedByUUID = &parsed
		}
	}

	adminsGroupUUID := uuid.MustParse(AdministratorsGroupUUID)

	// Get auth service for user/group lookup
	if GlobalAuthServiceForEvents == nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Authentication service not available",
		})
		return
	}

	adapter, ok := GlobalAuthServiceForEvents.(*AuthServiceAdapter)
	if !ok {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Authentication service adapter error",
		})
		return
	}

	authService := adapter.GetService()
	if authService == nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Authentication service not initialized",
		})
		return
	}

	// Resolve user or group and add to Administrators group
	var responseAdmin Administrator

	if email != nil {
		user, err := authService.GetUserByEmail(c.Request.Context(), *email)
		if err != nil {
			logger.Warn("User not found: provider=%s, email=%s, error=%v", req.Provider, *email, err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "user_not_found",
				Message: fmt.Sprintf("User not found with email %s and provider %s", *email, req.Provider),
			})
			return
		}
		if user.Provider != req.Provider {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "provider_mismatch",
				Message: fmt.Sprintf("User %s belongs to provider %s, not %s", *email, user.Provider, req.Provider),
			})
			return
		}
		userUUID := uuid.MustParse(user.InternalUUID)
		member, err := GlobalGroupMemberStore.AddMember(c.Request.Context(), adminsGroupUUID, userUUID, addedByUUID, nil)
		if err != nil {
			logger.Error("Failed to add user to Administrators group: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to create administrator grant",
			})
			return
		}
		responseAdmin = Administrator{
			Id:        member.Id,
			Provider:  user.Provider,
			CreatedAt: member.AddedAt,
			UserId:    &userUUID,
			UserEmail: &user.Email,
			UserName:  &user.Name,
		}
	} else if providerUserID != nil {
		user, err := authService.GetUserByProviderID(c.Request.Context(), req.Provider, *providerUserID)
		if err != nil {
			logger.Warn("User not found: provider=%s, provider_user_id=%s, error=%v", req.Provider, *providerUserID, err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "user_not_found",
				Message: fmt.Sprintf("User not found with provider_user_id %s and provider %s", *providerUserID, req.Provider),
			})
			return
		}
		userUUID := uuid.MustParse(user.InternalUUID)
		member, err := GlobalGroupMemberStore.AddMember(c.Request.Context(), adminsGroupUUID, userUUID, addedByUUID, nil)
		if err != nil {
			logger.Error("Failed to add user to Administrators group: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to create administrator grant",
			})
			return
		}
		responseAdmin = Administrator{
			Id:        member.Id,
			Provider:  user.Provider,
			CreatedAt: member.AddedAt,
			UserId:    &userUUID,
			UserEmail: &user.Email,
			UserName:  &user.Name,
		}
	} else {
		// Group-based grant
		group, err := GlobalGroupStore.GetByProviderAndName(c.Request.Context(), req.Provider, *groupName)
		if err != nil {
			logger.Warn("Group not found: provider=%s, group_name=%s, error=%v", req.Provider, *groupName, err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "group_not_found",
				Message: fmt.Sprintf("Group not found with name %s and provider %s", *groupName, req.Provider),
			})
			return
		}
		member, err := GlobalGroupMemberStore.AddGroupMember(c.Request.Context(), adminsGroupUUID, group.InternalUUID, addedByUUID, nil)
		if err != nil {
			logger.Error("Failed to add group to Administrators group: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to create administrator grant",
			})
			return
		}
		groupUUID := group.InternalUUID
		displayName := group.Name
		if displayName == "" {
			displayName = group.GroupName
		}
		responseAdmin = Administrator{
			Id:        member.Id,
			Provider:  group.Provider,
			CreatedAt: member.AddedAt,
			GroupId:   &groupUUID,
			GroupName: &displayName,
		}
	}

	// AUDIT LOG
	auditLogger := NewAuditLogger()
	auditCtx := &AuditContext{
		ActorUserID: actorUserID,
		ActorEmail:  actorEmail,
	}
	auditLogger.LogAdministratorGrantCreated(auditCtx, responseAdmin.Id.String(), responseAdmin.UserId, responseAdmin.GroupId, responseAdmin.Provider)

	c.JSON(http.StatusCreated, responseAdmin)
}

// DeleteAdministrator handles DELETE /admin/administrators/{id}
// Wraps removing a user or group from the Administrators built-in group.
func (s *Server) DeleteAdministrator(c *gin.Context, id openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	if adminDB == nil {
		logger.Error("adminDB not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Database not initialized",
		})
		return
	}

	// Look up the group member record by ID in the Administrators group
	var member models.GroupMember
	if err := adminDB.Where("id = ? AND group_internal_uuid = ?", id.String(), AdministratorsGroupUUID).
		Preload("User").Preload("MemberGroup").
		First(&member).Error; err != nil {
		logger.Warn("Administrator grant not found: id=%s", id)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Administrator grant not found",
		})
		return
	}

	// Get current user information
	currentUserID := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")

	// Prevent self-revocation (user-based grants)
	if member.SubjectType == "user" && member.UserInternalUUID != nil && currentUserID != "" {
		if *member.UserInternalUUID == currentUserID {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusForbidden,
				Code:    "self_revocation",
				Message: "cannot revoke your own administrator privileges",
			})
			return
		}
	}

	// Prevent self-revocation (group-based grants)
	if member.SubjectType == "group" && member.MemberGroup != nil {
		userGroups, exists := c.Get("userGroups")
		if exists {
			if groupSlice, ok := userGroups.([]string); ok {
				for _, group := range groupSlice {
					if group == member.MemberGroup.GroupName {
						HandleRequestError(c, &RequestError{
							Status:  http.StatusForbidden,
							Code:    "self_revocation",
							Message: "cannot revoke administrator privileges for a group you belong to",
						})
						return
					}
				}
			}
		}
	}

	// AUDIT LOG: Log deletion before removing
	auditLogger := NewAuditLogger()
	auditCtx := &AuditContext{
		ActorUserID: currentUserID,
		ActorEmail:  actorEmail,
	}

	var userUUIDPtr *uuid.UUID
	var groupUUIDPtr *uuid.UUID
	provider := ""
	if member.SubjectType == "user" && member.UserInternalUUID != nil {
		parsed := uuid.MustParse(*member.UserInternalUUID)
		userUUIDPtr = &parsed
		if member.User != nil {
			provider = member.User.Provider
		}
	} else if member.SubjectType == "group" && member.MemberGroupInternalUUID != nil {
		parsed := uuid.MustParse(*member.MemberGroupInternalUUID)
		groupUUIDPtr = &parsed
		if member.MemberGroup != nil {
			provider = member.MemberGroup.Provider
		}
	}
	auditLogger.LogAdministratorGrantDeleted(auditCtx, id.String(), userUUIDPtr, groupUUIDPtr, provider)

	// Remove the member from the Administrators group
	adminsGroupUUID := uuid.MustParse(AdministratorsGroupUUID)
	if member.SubjectType == "user" && member.UserInternalUUID != nil {
		userUUID := uuid.MustParse(*member.UserInternalUUID)
		if err := GlobalGroupMemberStore.RemoveMember(c.Request.Context(), adminsGroupUUID, userUUID); err != nil {
			logger.Error("Failed to delete administrator grant: id=%s, error=%v", id, err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to delete administrator grant",
			})
			return
		}
	} else if member.SubjectType == "group" && member.MemberGroupInternalUUID != nil {
		memberGroupUUID := uuid.MustParse(*member.MemberGroupInternalUUID)
		if err := GlobalGroupMemberStore.RemoveGroupMember(c.Request.Context(), adminsGroupUUID, memberGroupUUID); err != nil {
			logger.Error("Failed to delete administrator grant: id=%s, error=%v", id, err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to delete administrator grant",
			})
			return
		}
	}

	c.Status(http.StatusNoContent)
}
