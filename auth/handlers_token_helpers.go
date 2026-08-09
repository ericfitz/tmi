package auth

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
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
// SEM@022025c0087d1b5a5a082c0b361153b6d80265a6: build a sanitized OAuth error response and operator log message for an empty subject claim (pure)
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
// SEM@a050fb6e0fd9dcae1492b381c23e55964a2b9506: build a sanitized server_error response and operator log for a failed authorization code exchange (pure)
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
// SEM@a050fb6e0fd9dcae1492b381c23e55964a2b9506: build a sanitized provider_unreachable response and operator log for a user-info fetch failure (pure)
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
// SEM@a050fb6e0fd9dcae1492b381c23e55964a2b9506: build a sanitized server_error response and operator log for a user find-or-create failure (pure)
func userPersistError(providerID string, err error) (gin.H, string) {
	body := gin.H{
		"error":             "server_error",
		"error_description": "Could not complete user account setup. Please retry or contact the administrator.",
	}
	msg := fmt.Sprintf("Failed to find or create user (provider: %s): %v", providerID, err)
	return body, msg
}

// userPersistUnavailableError covers find-or-create-user failures whose root
// cause is a transient database condition (connection kill, deadlock,
// serialization failure). Mapped to 503 + Retry-After so clients retry (#721);
// the OpenAPI ServiceUnavailable component already documents this response.
// SEM@0000000: build a service_unavailable response and operator log for a transient user-persist failure (pure)
func userPersistUnavailableError(providerID string, err error) (gin.H, string) {
	body := gin.H{
		"error":             "service_unavailable",
		"error_description": "Storage service temporarily unavailable - please retry",
	}
	msg := fmt.Sprintf("Transient database error during user find-or-create (provider: %s): %v", providerID, err)
	return body, msg
}

// respondUserPersistError writes the HTTP response for a findOrCreateUser
// failure: 409 cross-provider conflict, 403 unverified email (#290),
// 503 + Retry-After for transient DB faults (#721), 500 otherwise.
// SEM@0000000: handle a user find-or-create failure with status mapped to its cause
func respondUserPersistError(c *gin.Context, providerID string, err error) {
	switch {
	case errors.Is(err, errCrossProviderConflict):
		c.JSON(http.StatusConflict, gin.H{
			"error":             "account_conflict",
			"error_description": "This email is already linked to a different sign-in method.",
		})
	case errors.Is(err, errUnverifiedEmailMatch):
		c.JSON(http.StatusForbidden, gin.H{
			"error":             "email_not_verified",
			"error_description": "Email address must be verified by your sign-in provider.",
		})
	case errors.Is(dberrors.Classify(err), dberrors.ErrTransient):
		body, msg := userPersistUnavailableError(providerID, err)
		slogging.Get().WithContext(c).Error("%s", msg)
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, body)
	default:
		body, msg := userPersistError(providerID, err)
		slogging.Get().WithContext(c).Error("%s", msg)
		c.JSON(http.StatusInternalServerError, body)
	}
}

// tokenIssuanceError covers JWT generation failures after the user record
// has been resolved.
// SEM@a050fb6e0fd9dcae1492b381c23e55964a2b9506: build a sanitized server_error response and operator log for a JWT generation failure (pure)
func tokenIssuanceError(userEmail string, err error) (gin.H, string) {
	body := gin.H{
		"error":             "server_error",
		"error_description": "Could not issue authentication tokens. Please retry or contact the administrator.",
	}
	msg := fmt.Sprintf("Failed to generate JWT tokens for user %s: %v", userEmail, err)
	return body, msg
}

// codeVerifierFormatError covers PKCE code_verifier validation failures (4xx).
// SEM@a050fb6e0fd9dcae1492b381c23e55964a2b9506: build an invalid_request response and operator log for a malformed PKCE code verifier (pure)
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
// SEM@a050fb6e0fd9dcae1492b381c23e55964a2b9506: build an invalid_grant response and operator log for a failed refresh token exchange (pure)
func refreshTokenError(err error) (gin.H, string) {
	body := gin.H{
		"error":             "invalid_grant",
		"error_description": "The refresh token is invalid or expired.",
	}
	msg := fmt.Sprintf("Failed to refresh token: %v", err)
	return body, msg
}
