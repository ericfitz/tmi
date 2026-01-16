package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AdminContext contains the authenticated administrator's information
type AdminContext struct {
	Email        string
	InternalUUID *uuid.UUID
	Provider     string
	GroupNames   []string
	GroupUUIDs   []uuid.UUID
}

// RequireAdministrator checks if the current user is an administrator
// Returns an AdminContext if authorized, or nil with error response sent
func RequireAdministrator(c *gin.Context) (*AdminContext, error) {
	logger := slogging.Get().WithContext(c)

	// Get authenticated user information from JWT claims
	userEmail, exists := c.Get("userEmail")
	if !exists {
		logger.Warn("Admin check: no userEmail in context")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required",
		})
		return nil, &RequestError{Status: http.StatusUnauthorized}
	}

	email, ok := userEmail.(string)
	if !ok || email == "" {
		logger.Warn("Admin check: invalid userEmail in context")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Invalid authentication token",
		})
		return nil, &RequestError{Status: http.StatusUnauthorized}
	}

	// Get user's internal UUID (NOT the provider's user ID)
	var userInternalUUID *uuid.UUID
	if internalUUIDInterface, exists := c.Get("userInternalUUID"); exists {
		if uuidVal, ok := internalUUIDInterface.(uuid.UUID); ok {
			userInternalUUID = &uuidVal
		} else if uuidStr, ok := internalUUIDInterface.(string); ok {
			if parsedID, err := uuid.Parse(uuidStr); err == nil {
				userInternalUUID = &parsedID
			}
		}
	}

	// Get provider from JWT claims
	provider := c.GetString("userProvider")
	if provider == "" {
		logger.Warn("Admin check: no provider in context")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Invalid authentication token",
		})
		return nil, &RequestError{Status: http.StatusUnauthorized}
	}

	// Get user groups from JWT claims
	var groupNames []string
	if groupsInterface, exists := c.Get("userGroups"); exists {
		if groupSlice, ok := groupsInterface.([]string); ok {
			groupNames = groupSlice
		}
	}

	// Check if user is an administrator
	if GlobalAdministratorStore == nil {
		logger.Error("Admin check: GlobalAdministratorStore is nil")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Administrator store not initialized",
		})
		return nil, &RequestError{Status: http.StatusInternalServerError}
	}

	// Convert group names to group UUIDs
	var groupUUIDs []uuid.UUID
	if dbStore, ok := GlobalAdministratorStore.(*GormAdministratorStore); ok && len(groupNames) > 0 {
		var err error
		groupUUIDs, err = dbStore.GetGroupUUIDsByNames(c.Request.Context(), provider, groupNames)
		if err != nil {
			logger.Error("Admin check: failed to lookup group UUIDs: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to verify administrator status",
			})
			return nil, &RequestError{Status: http.StatusInternalServerError}
		}
	}

	isAdmin, err := GlobalAdministratorStore.IsAdmin(c.Request.Context(), userInternalUUID, provider, groupUUIDs)
	if err != nil {
		logger.Error("Admin check: failed to check admin status for email=%s: %v", email, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to verify administrator status",
		})
		return nil, &RequestError{Status: http.StatusInternalServerError}
	}

	if !isAdmin {
		logger.Warn("Admin check: access denied for non-admin user: email=%s, provider=%s, groups=%v", email, provider, groupNames)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "Administrator access required",
		})
		return nil, &RequestError{Status: http.StatusForbidden}
	}

	logger.Debug("Admin check: access granted for admin user: email=%s", email)

	return &AdminContext{
		Email:        email,
		InternalUUID: userInternalUUID,
		Provider:     provider,
		GroupNames:   groupNames,
		GroupUUIDs:   groupUUIDs,
	}, nil
}
