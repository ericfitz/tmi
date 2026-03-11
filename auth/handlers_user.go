package auth

import (
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// MeLogout revokes the caller's own JWT token
// This is a convenience endpoint that doesn't require passing the token in the body
func (h *Handlers) MeLogout(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get JWT token from Authorization header or cookie
	var tokenStr string
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
		tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
	} else if h.cookieOpts.Enabled {
		tokenStr = ExtractAccessTokenFromCookie(c)
	}

	if tokenStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "unauthorized",
			"error_description": "Missing or invalid authentication",
		})
		return
	}

	// Validate token before revoking (this endpoint requires a valid token)
	claims := jwt.MapClaims{}
	token, err := h.service.GetKeyManager().VerifyToken(tokenStr, claims)
	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "unauthorized",
			"error_description": "Invalid token",
		})
		return
	}

	// Revoke the token (as access_token)
	if err := h.revokeTokenInternal(c.Request.Context(), tokenStr, "access_token"); err != nil {
		logger.Error("Failed to revoke token during logout: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":             "server_error",
			"error_description": "Failed to revoke token",
		})
		return
	}

	// Clear session cookies
	if h.cookieOpts.Enabled {
		ClearTokenCookies(c, h.cookieOpts)
	}

	logger.Info("User logged out successfully")

	// Return 204 No Content
	c.Status(http.StatusNoContent)
}

// Logout is deprecated - use RevokeToken for RFC 7009 compliance or MeLogout for self-logout
// Kept for backward compatibility, delegates to MeLogout
func (h *Handlers) Logout(c *gin.Context) {
	h.MeLogout(c)
}

// Me returns the current user
func (h *Handlers) Me(c *gin.Context) {
	// Get the full user object from Gin context (set by JWT middleware)
	userInterface, exists := c.Get(string(UserContextKey))
	if exists {
		if user, ok := userInterface.(User); ok {
			// Try to get groups from JWT claims or cache
			userEmail := c.GetString("userEmail")
			if userEmail != "" {
				// Try to get groups from cache
				_, groups, _ := h.service.GetCachedGroups(c.Request.Context(), userEmail)
				if len(groups) > 0 {
					user.Groups = groups
					// Note: do NOT overwrite user.Provider from the group cache.
					// The provider in the user object (set from JWT/DB) is authoritative
					// for the current session. The cached IdP may be from a different
					// authentication method (e.g., SAML vs OAuth) for the same user.
				}
			}

			// Check if we should add admin status
			if addAdminStatus, exists := c.Get("add_admin_status"); exists && addAdminStatus == true {
				if h.adminChecker != nil {
					// Get user's internal UUID from the user object (not from context)
					userInternalUUID := &user.InternalUUID

					// Get provider from user object (prefer it over context)
					provider := user.Provider
					if provider == "" {
						// Fallback to context if not in user object
						provider = c.GetString("userProvider")
					}

					// Get groups from context
					var groupNames []string
					if groupsInterface, exists := c.Get("userGroups"); exists {
						if groupSlice, ok := groupsInterface.([]string); ok {
							groupNames = groupSlice
						}
					}

					// Convert group names to UUIDs for admin check
					var groupUUIDs []string
					if len(groupNames) > 0 {
						if uuids, err := h.adminChecker.GetGroupUUIDsByNames(c.Request.Context(), provider, groupNames); err == nil {
							groupUUIDs = uuids
						}
					}

					// Check admin status
					if isAdmin, err := h.adminChecker.IsAdmin(c.Request.Context(), userInternalUUID, provider, groupUUIDs); err == nil {
						user.IsAdmin = isAdmin
					}

					// Check security reviewer status
					if isSecReviewer, err := h.adminChecker.IsSecurityReviewer(c.Request.Context(), userInternalUUID, provider, groupUUIDs); err == nil {
						user.IsSecurityReviewer = isSecReviewer
					}
				}

				// Fetch TMI-managed groups the user belongs to
				if h.userGroupsFetcher != nil {
					if userGroups, err := h.userGroupsFetcher.GetUserGroups(c.Request.Context(), user.InternalUUID); err == nil {
						c.Set("tmiUserGroups", userGroups)
					} else {
						slogging.Get().Warn("Failed to fetch user groups for /me: %v", err)
					}
				}
			}

			// Check if OIDC response format is requested (for /oauth2/userinfo)
			if oidcFormat, exists := c.Get("oidc_response_format"); exists && oidcFormat == true {
				// Return OIDC-compliant userinfo response
				response := convertUserToOIDCResponse(user)
				c.JSON(http.StatusOK, response)
				return
			}

			// Convert auth.User to OpenAPI UserWithAdminStatus type
			// This ensures field names match the API spec (provider_id instead of provider_user_id)
			var userGroups []UserGroupInfo
			if groups, exists := c.Get("tmiUserGroups"); exists {
				if g, ok := groups.([]UserGroupInfo); ok {
					userGroups = g
				}
			}
			response := convertUserToAPIResponse(user, userGroups)
			c.JSON(http.StatusOK, response)
			return
		}
	}

	// User not found in context - not authenticated
	setWWWAuthenticateHeader(c, "invalid_token", "User not authenticated")
	c.JSON(http.StatusUnauthorized, gin.H{
		"error": "User not authenticated",
	})
}
