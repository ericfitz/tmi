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

	// Resolve user identity and group memberships
	mc, err := ResolveMembershipContext(c)
	if err != nil {
		logger.Warn("Admin check: failed to resolve membership context: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required",
		})
		return nil, &RequestError{Status: http.StatusUnauthorized}
	}

	// Check effective membership in the Administrators group
	isAdmin, err := IsGroupMember(c.Request.Context(), mc, GroupAdministrators)
	if err != nil {
		logger.Error("Admin check: failed to check admin status for email=%s: %v", mc.Email, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to verify administrator status",
		})
		return nil, &RequestError{Status: http.StatusInternalServerError}
	}

	if !isAdmin {
		logger.Warn("Admin check: access denied for non-admin user: email=%s, provider=%s, groups=%v", mc.Email, mc.Provider, mc.GroupNames)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "Administrator access required",
		})
		return nil, &RequestError{Status: http.StatusForbidden}
	}

	logger.Debug("Admin check: access granted for admin user: email=%s", mc.Email)

	return &AdminContext{
		Email:        mc.Email,
		InternalUUID: &mc.UserUUID,
		Provider:     mc.Provider,
		GroupNames:   mc.GroupNames,
		GroupUUIDs:   mc.GroupUUIDs,
	}, nil
}
