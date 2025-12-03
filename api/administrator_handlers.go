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
	if dbStore, ok := GlobalAdministratorStore.(*AdministratorDatabaseStore); ok {
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

	// Validate: exactly one of user_internal_uuid or group_internal_uuid must be set
	if req.UserId == nil && req.GroupId == nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "either user_id or group_id must be specified",
		})
		return
	}

	if req.UserId != nil && req.GroupId != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "cannot specify both user_id and group_id",
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

	if req.UserId != nil {
		admin.UserInternalUUID = req.UserId
		admin.SubjectType = "user"
	} else {
		admin.GroupInternalUUID = req.GroupId
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
	if dbStore, ok := GlobalAdministratorStore.(*AdministratorDatabaseStore); ok {
		enriched, err := dbStore.EnrichAdministrators(c.Request.Context(), []DBAdministrator{admin})
		if err == nil && len(enriched) > 0 {
			admin = enriched[0]
		}
	}

	// AUDIT LOG: Log creation with actor details
	logger.Info("[AUDIT] Administrator grant created: grant_id=%s, user_id=%v, group_id=%v, provider=%s, created_by=%s (email=%s)",
		admin.ID, admin.UserInternalUUID, admin.GroupInternalUUID, admin.Provider, actorUserID, actorEmail)

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
	if dbStore, ok := GlobalAdministratorStore.(*AdministratorDatabaseStore); ok {
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
	logger.Info("[AUDIT] Administrator grant deleted: grant_id=%s, user_id=%v, group_id=%v, provider=%s, deleted_by=%s (email=%s)",
		grantID, adminGrant.UserInternalUUID, adminGrant.GroupInternalUUID, adminGrant.Provider, currentUserID, actorEmail)

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
