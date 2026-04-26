package auth

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

// emptySubjectError returns the gin.H body to send when claim extraction
// yields an empty user_id, and the matching log message for operators.
// Returned separately from the handler so the diagnostic format can be
// unit-tested without building a full handler stack.
//
// The log message reports the *currently configured* default subject-claim
// path (from DefaultClaimMappings) rather than hardcoding "sub" (#294). If
// a future change introduces per-classification defaults or alters the OIDC
// default, the log stays accurate without a follow-up edit.
func emptySubjectError(providerID, email string) (gin.H, string) {
	body := gin.H{
		"error":             "provider_response_invalid",
		"error_description": "Authentication provider returned incomplete profile data. Please contact the administrator.",
	}
	defaultSubjectPath := DefaultClaimMappings["subject_claim"]
	msg := fmt.Sprintf(
		"Runtime backstop triggered: claim extraction produced empty user_id (provider_id=%s, user_email=%s, subject_claim_path_default=%s). Likely cause: missing OAUTH_PROVIDERS_%s_USERINFO_CLAIMS_SUBJECT_CLAIM mapping. See issue #288.",
		providerID, email, defaultSubjectPath, providerIDToEnvKey(providerID),
	)
	return body, msg
}

// The helpers below all return a (body, log-message) pair for OAuth error
// paths in the token handler. The pattern, established by emptySubjectError
// and required by issue #295: never interpolate raw err into the response
// body — the upstream message can leak URLs, hostnames, partial stack traces,
// or library internals. The detailed err goes to the server log; the client
// gets a generic OAuth-style code (RFC 6749 §5.2; extension codes are fine).

// codeExchangeError covers the default-path failure when exchanging an
// authorization code for tokens (after the caller has handled known
// client-error patterns separately).
func codeExchangeError(providerID, codePrefix string, err error) (gin.H, string) {
	body := gin.H{
		"error":             "server_error",
		"error_description": "Could not complete authorization code exchange. Please retry or contact the administrator.",
	}
	msg := fmt.Sprintf("Failed to exchange authorization code for tokens in callback (provider: %s, code prefix: %.10s...): %v", providerID, codePrefix, err)
	return body, msg
}

// userInfoFetchError covers the case where calling the provider's userinfo
// endpoint itself returned an error (transport, 5xx, malformed body, etc.).
// This is the path called out in issue #295.
func userInfoFetchError(providerID string, err error) (gin.H, string) {
	body := gin.H{
		"error":             "provider_unreachable",
		"error_description": "Could not retrieve user information from the authentication provider. Please retry or contact the administrator.",
	}
	msg := fmt.Sprintf("Failed to get user info from OAuth provider (provider: %s): %v", providerID, err)
	return body, msg
}

// userPersistError covers find-or-create-user failures after the match-type
// branches (cross-provider conflict, unverified email) have already returned.
// What's left is a database/repository fault.
func userPersistError(providerID string, err error) (gin.H, string) {
	body := gin.H{
		"error":             "server_error",
		"error_description": "Could not complete user account setup. Please retry or contact the administrator.",
	}
	msg := fmt.Sprintf("Failed to find or create user (provider: %s): %v", providerID, err)
	return body, msg
}

// tokenIssuanceError covers JWT generation failures after the user record
// has been resolved.
func tokenIssuanceError(userEmail string, err error) (gin.H, string) {
	body := gin.H{
		"error":             "server_error",
		"error_description": "Could not issue authentication tokens. Please retry or contact the administrator.",
	}
	msg := fmt.Sprintf("Failed to generate JWT tokens for user %s: %v", userEmail, err)
	return body, msg
}

// codeVerifierFormatError covers PKCE code_verifier validation failures (4xx).
func codeVerifierFormatError(err error) (gin.H, string) {
	body := gin.H{
		"error":             "invalid_request",
		"error_description": "Code verifier format is invalid.",
	}
	msg := fmt.Sprintf("PKCE code_verifier format validation failed: %v", err)
	return body, msg
}

// refreshTokenError covers refresh-token failures (4xx). The detailed cause
// (expired, revoked, malformed) goes to the log; the client gets a single
// non-disclosing message.
func refreshTokenError(err error) (gin.H, string) {
	body := gin.H{
		"error":             "invalid_grant",
		"error_description": "The refresh token is invalid or expired.",
	}
	msg := fmt.Sprintf("Failed to refresh token: %v", err)
	return body, msg
}
