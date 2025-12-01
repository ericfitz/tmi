package api

import (
	"net/http"
	"strconv"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// ListSAMLUsers handles GET /saml/providers/{idp}/users
func (s *Server) ListSAMLUsers(c *gin.Context, idp string) {
	logger := slogging.Get().WithContext(c)

	// Parse query parameters
	email := c.Query("email")
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")

	// Parse limit and offset
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 0 || limit > 500 {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_limit",
			Message: "limit must be between 0 and 500",
		})
		return
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_offset",
			Message: "offset must be a non-negative integer",
		})
		return
	}

	// Build filter - only return users from the specified SAML provider
	filter := UserFilter{
		Provider:  idp,
		Email:     email,
		Limit:     limit,
		Offset:    offset,
		SortBy:    "last_login",
		SortOrder: "desc",
	}

	// Get users from store (only active users - no special filtering needed as deleted users are gone)
	users, err := GlobalUserStore.List(c.Request.Context(), filter)
	if err != nil {
		logger.Error("Failed to list SAML users: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to list users",
		})
		return
	}

	// Get total count
	total, err := GlobalUserStore.Count(c.Request.Context(), filter)
	if err != nil {
		logger.Warn("Failed to count SAML users: %v", err)
		total = len(users) // Fallback to current page count
	}

	// Build lightweight response for UI autocomplete
	type SAMLUser struct {
		InternalUUID string  `json:"internal_uuid"`
		Email        string  `json:"email"`
		Name         string  `json:"name"`
		LastLogin    *string `json:"last_login,omitempty"`
	}

	samlUsers := make([]SAMLUser, 0, len(users))
	for _, user := range users {
		samlUser := SAMLUser{
			InternalUUID: user.InternalUUID.String(),
			Email:        user.Email,
			Name:         user.Name,
		}
		if user.LastLogin != nil {
			lastLogin := user.LastLogin.Format("2006-01-02T15:04:05Z07:00")
			samlUser.LastLogin = &lastLogin
		}
		samlUsers = append(samlUsers, samlUser)
	}

	// Return response
	c.JSON(http.StatusOK, gin.H{
		"idp":   idp,
		"users": samlUsers,
		"total": total,
	})
}
