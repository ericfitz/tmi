package api

import (
	"fmt"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ListAdministrators handles GET /admin/administrators
func (s *Server) ListAdministrators(c *gin.Context, params ListAdministratorsParams) {
	logger := slogging.Get().WithContext(c)

	// Validate pagination parameters
	if err := ValidatePaginationParams(params.Limit, params.Offset); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Set defaults
	limit := 50
	offset := 0

	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}

	// Build filter
	filter := AdminFilter{
		Limit:  limit,
		Offset: offset,
	}

	// Set optional provider filter
	if params.Provider != nil {
		filter.Provider = *params.Provider
	}

	// Set optional user_internal_uuid filter
	if params.UserId != nil {
		filter.UserID = params.UserId
	}

	// Set optional group_internal_uuid filter
	if params.GroupId != nil {
		filter.GroupID = params.GroupId
	}

	// Get administrators from store
	if dbStore, ok := GlobalAdministratorStore.(*GormAdministratorStore); ok {
		admins, err := dbStore.ListFiltered(c.Request.Context(), filter)
		if err != nil {
			logger.Error("Failed to list administrators: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to list administrators",
			})
			return
		}

		// Enrich with user emails and group names
		enriched, err := dbStore.EnrichAdministrators(c.Request.Context(), admins)
		if err != nil {
			logger.Warn("Failed to enrich administrators: %v", err)
			// Continue with non-enriched data
			enriched = admins
		}

		// Convert to API type
		apiAdmins := make([]Administrator, 0, len(enriched))
		for i := range enriched {
			apiAdmins = append(apiAdmins, enriched[i].ToAPI())
		}

		// Return response
		c.JSON(http.StatusOK, ListAdministratorsResponse{
			Administrators: apiAdmins,
			Total:          len(apiAdmins),
		})
	} else {
		logger.Error("GlobalAdministratorStore is not a database store")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Administrator store not properly initialized",
		})
	}
}

// CreateAdministrator handles POST /admin/administrators
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

	// Count how many identification fields are set
	fieldCount := 0
	if req.Email != nil {
		fieldCount++
	}
	if req.ProviderUserId != nil {
		fieldCount++
	}
	if req.GroupName != nil {
		fieldCount++
	}

	// Validate: exactly one identification field must be set
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

	// Get actor information for audit logging
	actorUserID := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")

	// Create administrator grant (using internal DBAdministrator type)
	admin := DBAdministrator{
		ID:        uuid.New(),
		Provider:  req.Provider,
		GrantedAt: time.Now().UTC(),
	}

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

	// Lookup user or group by the provided identifier
	if req.Email != nil {
		// Lookup user by email
		user, err := authService.GetUserByEmail(c.Request.Context(), string(*req.Email))
		if err != nil {
			logger.Warn("User not found: provider=%s, email=%s, error=%v", req.Provider, *req.Email, err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "user_not_found",
				Message: fmt.Sprintf("User not found with email %s and provider %s", *req.Email, req.Provider),
			})
			return
		}
		// Verify provider matches
		if user.Provider != req.Provider {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "provider_mismatch",
				Message: fmt.Sprintf("User %s belongs to provider %s, not %s", *req.Email, user.Provider, req.Provider),
			})
			return
		}
		userUUID := uuid.MustParse(user.InternalUUID)
		admin.UserInternalUUID = &userUUID
		admin.SubjectType = "user"
	} else if req.ProviderUserId != nil {
		// Lookup user by provider_user_id
		user, err := authService.GetUserByProviderID(c.Request.Context(), req.Provider, *req.ProviderUserId)
		if err != nil {
			logger.Warn("User not found: provider=%s, provider_user_id=%s, error=%v", req.Provider, *req.ProviderUserId, err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "user_not_found",
				Message: fmt.Sprintf("User not found with provider_user_id %s and provider %s", *req.ProviderUserId, req.Provider),
			})
			return
		}
		userUUID := uuid.MustParse(user.InternalUUID)
		admin.UserInternalUUID = &userUUID
		admin.SubjectType = "user"
	} else {
		// Lookup group by group_name using GlobalGroupStore
		group, err := GlobalGroupStore.GetByProviderAndName(c.Request.Context(), req.Provider, *req.GroupName)
		if err != nil {
			logger.Warn("Group not found: provider=%s, group_name=%s, error=%v", req.Provider, *req.GroupName, err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "group_not_found",
				Message: fmt.Sprintf("Group not found with name %s and provider %s", *req.GroupName, req.Provider),
			})
			return
		}
		admin.GroupInternalUUID = &group.InternalUUID
		admin.SubjectType = "group"
	}

	// Set granted_by to current user
	if actorUserID != "" {
		actorUUID, err := uuid.Parse(actorUserID)
		if err == nil {
			admin.GrantedBy = &actorUUID
		}
	}

	// Create in database
	err := GlobalAdministratorStore.Create(c.Request.Context(), admin)
	if err != nil {
		logger.Error("Failed to create administrator grant: %v", err)
		// Check if it's a duplicate (conflict)
		if err.Error() == "administrator grant already exists" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusConflict,
				Code:    "duplicate_grant",
				Message: "Administrator grant already exists for this user/group and provider",
			})
		} else {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to create administrator grant",
			})
		}
		return
	}

	// Enrich response with email/group name
	if dbStore, ok := GlobalAdministratorStore.(*GormAdministratorStore); ok {
		enriched, err := dbStore.EnrichAdministrators(c.Request.Context(), []DBAdministrator{admin})
		if err == nil && len(enriched) > 0 {
			admin = enriched[0]
		}
	}

	// AUDIT LOG: Log creation with actor details
	auditLogger := NewAuditLogger()
	auditCtx := &AuditContext{
		ActorUserID: actorUserID,
		ActorEmail:  actorEmail,
	}
	auditLogger.LogAdministratorGrantCreated(auditCtx, admin.ID.String(), admin.UserInternalUUID, admin.GroupInternalUUID, admin.Provider)

	// Convert to API type for response
	c.JSON(http.StatusCreated, admin.ToAPI())
}

// DeleteAdministrator handles DELETE /admin/administrators/{id}
func (s *Server) DeleteAdministrator(c *gin.Context, id openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	grantID := id

	// Get the administrator grant to check self-revocation
	var adminGrant *DBAdministrator
	var err error
	if dbStore, ok := GlobalAdministratorStore.(*GormAdministratorStore); ok {
		adminGrant, err = dbStore.Get(c.Request.Context(), grantID)
		if err != nil {
			logger.Warn("Administrator grant not found: id=%s", grantID)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "Administrator grant not found",
			})
			return
		}
	} else {
		logger.Error("GlobalAdministratorStore is not a database store")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Administrator store not properly initialized",
		})
		return
	}

	// Get current user information
	currentUserID := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")

	// Prevent self-revocation (user-based grants)
	if adminGrant.UserInternalUUID != nil && currentUserID != "" {
		if adminGrant.UserInternalUUID.String() == currentUserID {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusForbidden,
				Code:    "self_revocation",
				Message: "cannot revoke your own administrator privileges",
			})
			return
		}
	}

	// Prevent self-revocation (group-based grants)
	if adminGrant.GroupInternalUUID != nil {
		userGroups, exists := c.Get("userGroups")
		if exists {
			if groupSlice, ok := userGroups.([]string); ok && adminGrant.GroupName != "" {
				for _, group := range groupSlice {
					if group == adminGrant.GroupName {
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

	// AUDIT LOG: Log deletion with actor details and affected principal BEFORE deleting
	auditLogger := NewAuditLogger()
	auditCtx := &AuditContext{
		ActorUserID: currentUserID,
		ActorEmail:  actorEmail,
	}
	auditLogger.LogAdministratorGrantDeleted(auditCtx, grantID.String(), adminGrant.UserInternalUUID, adminGrant.GroupInternalUUID, adminGrant.Provider)

	// Delete the grant
	err = GlobalAdministratorStore.Delete(c.Request.Context(), grantID)
	if err != nil {
		logger.Error("Failed to delete administrator grant: id=%s, error=%v", grantID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to delete administrator grant",
		})
		return
	}

	c.Status(http.StatusNoContent)
}
