package api

import (
	"fmt"
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
	}

	// Fallback to getting from full user object
	user, err := GetUserFromContext(c)
	if err != nil {
		return "", err
	}

	if user.InternalUUID == "" {
		return "", &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "User internal UUID not available",
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
		return "", &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to retrieve user email from context",
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
		return "", &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to retrieve user provider from context",
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
		return "", &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to retrieve user ID from context",
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

// ValidateUserAuthentication is a comprehensive validation that checks
// all user context values are properly set
// Returns user email, internal UUID, provider, and provider user ID
// This is useful for handlers that need all user identification fields
func ValidateUserAuthentication(c *gin.Context) (email, internalUUID, provider, providerUserID string, err error) {
	email, err = GetUserEmail(c)
	if err != nil {
		return "", "", "", "", err
	}

	internalUUID, err = GetUserInternalUUID(c)
	if err != nil {
		return "", "", "", "", err
	}

	provider, err = GetUserProvider(c)
	if err != nil {
		return "", "", "", "", err
	}

	providerUserID, err = GetUserProviderID(c)
	if err != nil {
		return "", "", "", "", err
	}

	return email, internalUUID, provider, providerUserID, nil
}

// GetUserContext is a convenience function that returns a structured UserContext
// containing all user identification information from the Gin context
func GetUserContext(c *gin.Context) (*UserContext, error) {
	email, internalUUID, provider, providerUserID, err := ValidateUserAuthentication(c)
	if err != nil {
		return nil, err
	}

	return &UserContext{
		Email:          email,
		InternalUUID:   internalUUID,
		Provider:       provider,
		ProviderUserID: providerUserID,
		DisplayName:    GetUserDisplayName(c),
		Groups:         GetUserGroups(c),
	}, nil
}

// UserContext represents the authenticated user's context information
// This is a convenience structure for passing user info between handlers
type UserContext struct {
	Email          string   `json:"email"`
	InternalUUID   string   `json:"internal_uuid"`    // System-generated UUID (never in JWT)
	Provider       string   `json:"provider"`         // OAuth provider name
	ProviderUserID string   `json:"provider_user_id"` // Provider's user ID (from JWT sub)
	DisplayName    string   `json:"display_name,omitempty"`
	Groups         []string `json:"groups,omitempty"`
}

// String returns a string representation of the UserContext for logging
func (uc *UserContext) String() string {
	return fmt.Sprintf("UserContext{email=%s, internal_uuid=%s, provider=%s, provider_user_id=%s}",
		uc.Email, uc.InternalUUID, uc.Provider, uc.ProviderUserID)
}
