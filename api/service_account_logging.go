package api

import (
	"github.com/gin-gonic/gin"
)

// GetUserIdentityForLogging returns a formatted user identity string for logging
// that distinguishes between regular users and service accounts.
//
// For regular users: returns "user={email}"
// For service accounts: returns "service_account=[Service Account] {name} (credential_id={id}, owner={email})"
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: format a loggable user identity string distinguishing service accounts from regular users (pure)
func GetUserIdentityForLogging(c *gin.Context) string {
	isServiceAccount, _ := c.Get("isServiceAccount")
	isServiceAccountBool, ok := isServiceAccount.(bool)

	if ok && isServiceAccountBool {
		// Service account request
		credentialID, _ := c.Get("serviceAccountCredentialID")
		credentialIDStr, _ := credentialID.(string)
		userEmail, _ := c.Get("userEmail")
		userEmailStr, _ := userEmail.(string)
		displayName, _ := c.Get("userDisplayName")
		displayNameStr, _ := displayName.(string)

		if credentialIDStr != "" && userEmailStr != "" && displayNameStr != "" {
			return "service_account=" + displayNameStr + " (credential_id=" + credentialIDStr + ", owner=" + userEmailStr + ")"
		}
		// Fallback if context incomplete
		return "service_account=" + displayNameStr
	}

	// Regular user request
	userEmail, _ := c.Get("userEmail")
	userEmailStr, _ := userEmail.(string)
	if userEmailStr != "" {
		return "user=" + userEmailStr
	}

	return UnknownUserIdentity
}

// IsServiceAccountRequest returns true if the current request is from a service account
// SEM@b88f8e119b1c65b1b76832e46f22d2ebdb88d0ca: check whether the current request was authenticated as a service account (pure)
func IsServiceAccountRequest(c *gin.Context) bool {
	isServiceAccount, exists := c.Get("isServiceAccount")
	if !exists {
		return false
	}
	isServiceAccountBool, ok := isServiceAccount.(bool)
	return ok && isServiceAccountBool
}
