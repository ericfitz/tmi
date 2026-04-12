package api

import (
	"net/http"

	"github.com/ericfitz/tmi/auth"
	"github.com/gin-gonic/gin"
)

// GetUserFromContext retrieves the full user object from the Gin context
// The user object is set by the JWT middleware after authentication
// Returns RequestError if user is not found or not authenticated
func GetUserFromContext(c *gin.Context) (*auth.User, error) {
	userInterface, exists := c.Get(string(auth.UserContextKey))
	if !exists {
		return nil, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required",
		}
	}

	user, ok := userInterface.(auth.User)
	if !ok {
		return nil, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to retrieve user from context",
		}
	}

	return &user, nil
}

// GetUserInternalUUID retrieves the user's internal UUID from the context
// This is the system-generated UUID for internal tracking (never exposed in JWT)
// Returns error if user is not authenticated or UUID is not available
func GetUserInternalUUID(c *gin.Context) (string, error) {
	// Try to get from explicit context key first (set by JWT middleware)
	if internalUUID, exists := c.Get("userInternalUUID"); exists {
		if uuid, ok := internalUUID.(string); ok && uuid != "" {
			return uuid, nil
		}
		// Context key exists but value is invalid - authentication is corrupted
		return "", &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Invalid authentication state - please re-authenticate",
		}
	}

	// Fallback to getting from full user object
	user, err := GetUserFromContext(c)
	if err != nil {
		return "", err
	}

	if user.InternalUUID == "" {
		// User object exists but UUID is empty - authentication is incomplete
		return "", &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication incomplete - please re-authenticate",
		}
	}

	return user.InternalUUID, nil
}

// GetUserEmail retrieves the user's email from the context
// This is set by the JWT middleware from the email claim
// Returns error if user is not authenticated or email is not available
func GetUserEmail(c *gin.Context) (string, error) {
	emailInterface, exists := c.Get("userEmail")
	if !exists {
		return "", &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required",
		}
	}

	email, ok := emailInterface.(string)
	if !ok || email == "" {
		// Context key exists but value is invalid - authentication is corrupted
		return "", &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Invalid authentication state - please re-authenticate",
		}
	}

	return email, nil
}

// GetUserProvider retrieves the user's OAuth provider from the context
// Returns the provider name (e.g., "tmi", "google", "github", "microsoft", "azure")
// Returns error if user is not authenticated or provider is not available
func GetUserProvider(c *gin.Context) (string, error) {
	providerInterface, exists := c.Get("userProvider")
	if !exists {
		// Try fallback to userIdP for backward compatibility
		providerInterface, exists = c.Get("userIdP")
		if !exists {
			return "", &RequestError{
				Status:  http.StatusUnauthorized,
				Code:    "unauthorized",
				Message: "Authentication required",
			}
		}
	}

	provider, ok := providerInterface.(string)
	if !ok || provider == "" {
		// Context key exists but value is invalid - authentication is corrupted
		return "", &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Invalid authentication state - please re-authenticate",
		}
	}

	return provider, nil
}

// GetUserProviderID retrieves the user's provider user ID from the context
// This is the OAuth provider's user ID (from JWT sub claim)
// Returns error if user is not authenticated or provider user ID is not available
func GetUserProviderID(c *gin.Context) (string, error) {
	userIDInterface, exists := c.Get("userID")
	if !exists {
		return "", &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required",
		}
	}

	userID, ok := userIDInterface.(string)
	if !ok || userID == "" {
		// Context key exists but value is invalid - authentication is corrupted
		return "", &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Invalid authentication state - please re-authenticate",
		}
	}

	return userID, nil
}

// GetUserGroups retrieves the user's groups from the context
// Returns the groups array from the identity provider
// Returns empty array if no groups are present (not an error)
func GetUserGroups(c *gin.Context) []string {
	groupsInterface, exists := c.Get("userGroups")
	if !exists {
		return []string{}
	}

	groups, ok := groupsInterface.([]string)
	if !ok {
		return []string{}
	}

	return groups
}

// GetUserDisplayName retrieves the user's display name from the context
// Returns the display name from JWT claims
// Returns empty string if not available (not an error)
func GetUserDisplayName(c *gin.Context) string {
	nameInterface, exists := c.Get("userDisplayName")
	if !exists {
		return ""
	}

	name, ok := nameInterface.(string)
	if !ok {
		return ""
	}

	return name
}

// GetUserAuthFieldsForAccessCheck extracts user identity fields from the Gin context
// using soft-failure semantics (empty strings on missing values). This is used by
// authorization middleware where missing fields don't prevent the access check -
// the authorization logic itself will deny access based on empty values.
func GetUserAuthFieldsForAccessCheck(c *gin.Context) (providerUserID, internalUUID, provider string, groups []string) {
	if id, exists := c.Get("userID"); exists {
		providerUserID, _ = id.(string)
	}
	if iuuid, exists := c.Get("userInternalUUID"); exists {
		internalUUID, _ = iuuid.(string)
	}
	if idp, exists := c.Get("userIdP"); exists {
		provider, _ = idp.(string)
	}
	if g, exists := c.Get("userGroups"); exists {
		groups, _ = g.([]string)
	}
	return
}
