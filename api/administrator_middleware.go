package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AdministratorMiddleware creates a middleware that requires the user to be an administrator
func AdministratorMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.Get()

		// Get authenticated user information from JWT claims (set by auth middleware)
		userEmail, exists := c.Get("userEmail")
		if !exists {
			logger.Warn("Administrator middleware: no userEmail in context")
			HandleRequestError(c, &RequestError{
				Status:  http.StatusUnauthorized,
				Code:    "unauthorized",
				Message: "Authentication required",
			})
			c.Abort()
			return
		}

		email, ok := userEmail.(string)
		if !ok || email == "" {
			logger.Warn("Administrator middleware: invalid userEmail in context")
			HandleRequestError(c, &RequestError{
				Status:  http.StatusUnauthorized,
				Code:    "unauthorized",
				Message: "Invalid authentication token",
			})
			c.Abort()
			return
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
			logger.Warn("Administrator middleware: no provider in context")
			HandleRequestError(c, &RequestError{
				Status:  http.StatusUnauthorized,
				Code:    "unauthorized",
				Message: "Invalid authentication token",
			})
			c.Abort()
			return
		}

		// Get user groups from JWT claims (may be empty)
		var groupNames []string
		if groupsInterface, exists := c.Get("userGroups"); exists {
			if groupSlice, ok := groupsInterface.([]string); ok {
				groupNames = groupSlice
			}
		}

		// Check if user is an administrator
		if GlobalAdministratorStore == nil {
			logger.Error("Administrator middleware: GlobalAdministratorStore is nil")
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Administrator store not initialized",
			})
			c.Abort()
			return
		}

		// Convert group names to group UUIDs
		var groupUUIDs []uuid.UUID
		if dbStore, ok := GlobalAdministratorStore.(*AdministratorDatabaseStore); ok && len(groupNames) > 0 {
			var err error
			groupUUIDs, err = dbStore.GetGroupUUIDsByNames(c.Request.Context(), provider, groupNames)
			if err != nil {
				logger.Error("Administrator middleware: failed to lookup group UUIDs: %v", err)
				HandleRequestError(c, &RequestError{
					Status:  http.StatusInternalServerError,
					Code:    "server_error",
					Message: "Failed to verify administrator status",
				})
				c.Abort()
				return
			}
		}

		isAdmin, err := GlobalAdministratorStore.IsAdmin(c.Request.Context(), userInternalUUID, provider, groupUUIDs)
		if err != nil {
			logger.Error("Administrator middleware: failed to check admin status for email=%s: %v", email, err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to verify administrator status",
			})
			c.Abort()
			return
		}

		if !isAdmin {
			logger.Warn("Administrator middleware: access denied for non-admin user: email=%s, provider=%s, groups=%v",
				email, provider, groupNames)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusForbidden,
				Code:    "forbidden",
				Message: "Administrator access required",
			})
			c.Abort()
			return
		}

		logger.Debug("Administrator middleware: access granted for admin user: email=%s", email)

		// User is an administrator, proceed
		c.Next()
	}
}
