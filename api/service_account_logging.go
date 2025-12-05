package api

import (
	"github.com/gin-gonic/gin"
)

// GetUserIdentityForLogging returns a formatted user identity string for logging
// that distinguishes between regular users and service accounts.
//
// For regular users: returns "user={email}"
// For service accounts: returns "service_account=[Service Account] {name} (credential_id={id}, owner={email})"
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

	return "user=<unknown>"
}

// IsServiceAccountRequest returns true if the current request is from a service account
func IsServiceAccountRequest(c *gin.Context) bool {
	isServiceAccount, exists := c.Get("isServiceAccount")
	if !exists {
		return false
	}
	isServiceAccountBool, ok := isServiceAccount.(bool)
	return ok && isServiceAccountBool
}
