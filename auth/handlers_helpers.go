package auth

import (
	"crypto/rand"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"time"

	"encoding/base64"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// Helper functions

// convertUserToAPIResponse converts auth.User to a map matching the OpenAPI UserWithAdminStatus schema
// This ensures field names match the API spec (provider_id instead of provider_user_id)
// Used by /me endpoint for TMI-specific user information
func convertUserToAPIResponse(user User, groups []UserGroupInfo) map[string]any {
	response := map[string]any{
		"principal_type":       "user",
		"provider":             user.Provider,
		"provider_id":          user.ProviderUserID, // Map ProviderUserID to provider_id
		"display_name":         user.Name,
		"email":                user.Email,
		"is_admin":             user.IsAdmin,
		"is_security_reviewer": user.IsSecurityReviewer,
	}
	if groups != nil {
		response["groups"] = groups
	} else {
		response["groups"] = []UserGroupInfo{}
	}
	return response
}

// convertUserToOIDCResponse converts auth.User to OIDC-compliant userinfo response
// Per OIDC Core 1.0 Section 5.1, only "sub" is required; other claims are optional
// Used by /oauth2/userinfo endpoint for OIDC standard compliance
func convertUserToOIDCResponse(user User) map[string]any {
	response := map[string]any{
		"sub":   user.ProviderUserID, // OIDC: subject identifier (required)
		"email": user.Email,          // OIDC: email claim
		"name":  user.Name,           // OIDC: full name claim
	}

	// Add idp if provider is set
	if user.Provider != "" {
		response["idp"] = user.Provider
	}

	// Add groups if present
	if len(user.Groups) > 0 {
		response["groups"] = user.Groups
	}

	return response
}

// getBaseURL constructs the base URL for the current request
func getBaseURL(c *gin.Context) string {
	scheme := schemeHTTP
	if c.Request.TLS != nil {
		scheme = schemeHTTPS
	}

	// Check for proxy headers that indicate HTTPS
	if forwardedProto := c.GetHeader("X-Forwarded-Proto"); forwardedProto == schemeHTTPS {
		scheme = schemeHTTPS
	}

	return fmt.Sprintf("%s://%s", scheme, c.Request.Host)
}

// generateState generates a random state parameter (method on Handlers)
func (h *Handlers) generateState() string {
	state, err := generateRandomState()
	if err != nil {
		// If we can't generate a secure random state, we should return an error
		// rather than falling back to a weak random number
		return fmt.Sprintf("state_%d", time.Now().UnixNano())
	}
	return state
}

// generateRandomState generates a random state parameter
func generateRandomState() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// buildClientRedirectURL builds the redirect URL for the client with tokens
// Tokens are returned in the URL fragment per OAuth 2.0 implicit flow specification
// to prevent them from being logged in server access logs or browser history
// buildAuthCodeRedirectURL builds redirect URL with authorization code (PKCE flow)
func buildAuthCodeRedirectURL(clientCallback string, code string, state string) (string, error) {
	// Parse the client callback URL
	parsedURL, err := url.Parse(clientCallback)
	if err != nil {
		return "", fmt.Errorf("invalid client callback URL: %w", err)
	}

	// Validate that this is a proper absolute URL for OAuth callbacks
	if parsedURL.Scheme == "" {
		return "", fmt.Errorf("invalid client callback URL: missing scheme")
	}
	if parsedURL.Host == "" {
		return "", fmt.Errorf("invalid client callback URL: missing host")
	}
	if parsedURL.Scheme != schemeHTTP && parsedURL.Scheme != schemeHTTPS {
		return "", fmt.Errorf("invalid client callback URL: scheme must be http or https")
	}

	// Build query parameters with authorization code (PKCE flow)
	query := parsedURL.Query()
	query.Set("code", code)
	if state != "" {
		query.Set("state", state)
	}
	parsedURL.RawQuery = query.Encode()

	return parsedURL.String(), nil
}

func buildClientRedirectURL(clientCallback string, tokenPair TokenPair, state string) (string, error) {
	// Parse the client callback URL
	parsedURL, err := url.Parse(clientCallback)
	if err != nil {
		return "", fmt.Errorf("invalid client callback URL: %w", err)
	}

	// Validate that this is a proper absolute URL for OAuth callbacks
	if parsedURL.Scheme == "" {
		return "", fmt.Errorf("invalid client callback URL: missing scheme")
	}
	if parsedURL.Host == "" {
		return "", fmt.Errorf("invalid client callback URL: missing host")
	}
	if parsedURL.Scheme != schemeHTTP && parsedURL.Scheme != schemeHTTPS {
		return "", fmt.Errorf("invalid client callback URL: scheme must be http or https")
	}

	// Build fragment parameters with tokens (per OAuth 2.0 implicit flow spec)
	fragment := url.Values{}
	fragment.Set("access_token", tokenPair.AccessToken)
	fragment.Set("refresh_token", tokenPair.RefreshToken)
	fragment.Set("expires_in", fmt.Sprintf("%d", tokenPair.ExpiresIn))
	fragment.Set("token_type", tokenPair.TokenType)

	// Include the original state parameter in fragment
	if state != "" {
		fragment.Set("state", state)
	}

	// Set the fragment (tokens are now in URL fragment, not query string)
	parsedURL.Fragment = fragment.Encode()

	return parsedURL.String(), nil
}

// validateOAuthScope validates the scope parameter according to OpenID Connect specification
// Requires at least "openid" scope, supports "profile" and "email", ignores other scopes
func (h *Handlers) validateOAuthScope(scope string) error {
	if scope == "" {
		return fmt.Errorf("scope parameter is required")
	}

	// Split scope parameter by spaces (OAuth 2.0 spec uses space-separated values)
	scopes := strings.Fields(scope)
	if len(scopes) == 0 {
		return fmt.Errorf("scope parameter cannot be empty")
	}

	// Check for required "openid" scope according to OpenID Connect specification
	hasOpenID := slices.Contains(scopes, "openid")

	if !hasOpenID {
		return fmt.Errorf("OpenID Connect requires 'openid' scope")
	}

	// Validate each scope - we support openid, profile, email and silently ignore others
	supportedScopes := map[string]bool{
		"openid":  true,
		"profile": true,
		"email":   true,
	}

	validScopes := make([]string, 0)
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s == "" {
			continue // Skip empty scopes
		}
		// We only validate that openid is present; other scopes are ignored (per spec)
		if supportedScopes[s] {
			validScopes = append(validScopes, s)
		}
		// Silently ignore unsupported scopes as per OAuth 2.0/OIDC spec
	}

	slogging.Get().Debug("OAuth scope validation: requested=%s, validated=%v", scope, validScopes)
	return nil
}
