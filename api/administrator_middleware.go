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

		// Get user ID if available (may not always be set)
		var userID *uuid.UUID
		if userIDInterface, exists := c.Get("userID"); exists {
			if userIDStr, ok := userIDInterface.(string); ok {
				if parsedID, err := uuid.Parse(userIDStr); err == nil {
					userID = &parsedID
				}
			} else if userUUID, ok := userIDInterface.(uuid.UUID); ok {
				userID = &userUUID
			}
		}

		// Get user groups from JWT claims (may be empty)
		var groups []string
		if groupsInterface, exists := c.Get("userGroups"); exists {
			if groupSlice, ok := groupsInterface.([]string); ok {
				groups = groupSlice
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

		isAdmin, err := GlobalAdministratorStore.IsAdmin(c.Request.Context(), userID, email, groups)
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
			logger.Warn("Administrator middleware: access denied for non-admin user: email=%s, groups=%v",
				email, groups)
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
