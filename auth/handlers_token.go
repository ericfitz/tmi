package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// Exchange exchanges an authorization code for tokens (legacy endpoint, delegates to handleAuthorizationCodeGrant)
func (h *Handlers) Exchange(c *gin.Context) {
	var req struct {
		GrantType    string `json:"grant_type" form:"grant_type" binding:"required"`
		Code         string `json:"code" form:"code" binding:"required"`
		CodeVerifier string `json:"code_verifier" form:"code_verifier" binding:"required"`
		RedirectURI  string `json:"redirect_uri" form:"redirect_uri" binding:"required"`
	}

	// Support both JSON and form-urlencoded content types
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "Missing required fields for authorization_code grant",
		})
		return
	}

	// Validate grant_type is "authorization_code"
	if req.GrantType != "authorization_code" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_grant",
			"error_description": "grant_type must be 'authorization_code'",
		})
		return
	}

	// Delegate to the shared implementation
	h.handleAuthorizationCodeGrant(c, req.Code, req.CodeVerifier, req.RedirectURI)
}

// handleAuthorizationCodeGrant handles the authorization code grant flow with PKCE
// This is called by both Token (for /oauth2/token) and Exchange (for backward compatibility)
func (h *Handlers) handleAuthorizationCodeGrant(c *gin.Context, code, codeVerifier, _ string) {
	// Get provider ID from query parameter
	providerID := c.Query("idp")
	if providerID == "" {
		// In non-production builds, default to "tmi" provider for convenience
		if defaultProviderID := getDefaultProviderID(); defaultProviderID != "" {
			slogging.Get().WithContext(c).Debug("No idp parameter provided, defaulting to provider: %s", defaultProviderID)
			providerID = defaultProviderID
		} else {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Missing required parameter: idp",
			})
			return
		}
	}

	// Get the provider
	provider, err := h.getProvider(providerID)
	if err != nil {
		// Return 404 for unavailable providers (like test provider in production)
		if strings.Contains(err.Error(), "not available in production") {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Provider not available",
			})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("Invalid provider: %s", providerID),
			})
		}
		return
	}

	ctx := c.Request.Context()

	// Validate code_verifier format
	if err := ValidateCodeVerifierFormat(codeVerifier); err != nil {
		body, msg := codeVerifierFormatError(err)
		slogging.Get().WithContext(c).Error("%s", msg)
		c.JSON(http.StatusBadRequest, body)
		return
	}

	// Retrieve PKCE challenge stored with authorization code
	// The challenge was bound to the code during the authorization callback
	codeKey := fmt.Sprintf("pkce:%s", code)
	pkceDataJSON, err := h.service.dbManager.Redis().Get(ctx, codeKey)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to retrieve PKCE challenge for code (provider: %s): %v", providerID, err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_grant",
			"error_description": "Authorization code is invalid or expired",
		})
		return
	}

	// Parse PKCE data
	var pkceData map[string]string
	if err := json.Unmarshal([]byte(pkceDataJSON), &pkceData); err != nil {
		slogging.Get().WithContext(c).Error("Failed to parse PKCE data for code: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to validate PKCE challenge",
		})
		return
	}

	codeChallenge := pkceData["code_challenge"]
	codeChallengeMethod := pkceData["code_challenge_method"]

	// Validate PKCE challenge
	if err := ValidateCodeChallenge(codeVerifier, codeChallenge, codeChallengeMethod); err != nil {
		slogging.Get().WithContext(c).Error("PKCE validation failed for code (provider: %s): %v", providerID, err)
		// Delete the PKCE data to prevent retry attacks
		_ = h.service.dbManager.Redis().Del(ctx, codeKey)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_grant",
			"error_description": "PKCE verification failed",
		})
		return
	}

	// Delete the PKCE challenge (one-time use)
	_ = h.service.dbManager.Redis().Del(ctx, codeKey)
	slogging.Get().WithContext(c).Debug("PKCE validation successful for code")

	// Exchange authorization code for tokens
	// Note: login_hint is now encoded directly in the authorization code for test provider
	tokenResponse, err := provider.ExchangeCode(ctx, code)
	if err != nil {
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "not supported"):
			// Production-mode restriction (not a server error)
			c.JSON(http.StatusForbidden, gin.H{
				"error":             "unsupported_grant_type",
				"error_description": errMsg,
			})
		case strings.Contains(errMsg, "invalid authorization code"),
			strings.Contains(errMsg, "authorization code is required"):
			// Client error: bad or missing authorization code
			c.JSON(http.StatusBadRequest, gin.H{
				"error": errMsg,
			})
		default:
			body, msg := codeExchangeError(providerID, code, err)
			slogging.Get().WithContext(c).Error("%s", msg)
			c.JSON(http.StatusInternalServerError, body)
		}
		return
	}

	// Validate ID token if present
	var claims *IDTokenClaims
	if tokenResponse.IDToken != "" {
		claims, err = provider.ValidateIDToken(ctx, tokenResponse.IDToken)
		if err != nil {
			// Log error but continue - we can get user info from userinfo endpoint
			logger := slogging.Get().WithContext(c)
			logger.Error("Failed to validate ID token: %v", err)
		}
	}

	// Get user info from provider
	userInfo, err := provider.GetUserInfo(ctx, tokenResponse.AccessToken)
	if err != nil {
		body, msg := userInfoFetchError(providerID, err)
		slogging.Get().WithContext(c).Error("%s", msg)
		c.JSON(http.StatusInternalServerError, body)
		return
	}

	// Runtime backstop (#288): if claim extraction completed without a
	// subject, the provider config is broken — startup validation should
	// have caught it, but we may be running a hot-deployed config or hit a
	// transient provider response anomaly. Return 502 with a non-leaky
	// message; the operator gets full diagnostics in the server log.
	if userInfo.ID == "" {
		body, msg := emptySubjectError(providerID, userInfo.Email)
		slogging.Get().WithContext(c).Error("%s", msg)
		c.JSON(http.StatusBadGateway, body)
		return
	}

	// Extract email from userInfo or claims with fallback
	email, err := h.extractEmailWithFallback(c, providerID, userInfo, claims)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user email or ID from provider"})
		return
	}

	// Extract name
	name := userInfo.Name
	if name == "" && claims != nil {
		name = claims.Name
	}
	if name == "" {
		name = email
	}

	// Determine email_verified status from userInfo or claims
	emailVerified := userInfo.EmailVerified
	if !emailVerified && claims != nil {
		emailVerified = claims.EmailVerified
	}

	// Find or create user using tiered matching strategy
	user, matchType, err := h.findOrCreateUser(ctx, c, providerID, userInfo.ID, email, name, emailVerified)
	if err != nil {
		// Account-takeover defense (#290): cross-provider email match must be
		// rejected with a non-disclosing 409 instead of returning a token for
		// the existing user. Unverified-email sparse-record binds are rejected
		// with 403 to prevent forged-email account claims.
		switch {
		case errors.Is(err, errCrossProviderConflict):
			c.JSON(http.StatusConflict, gin.H{
				"error":             "account_conflict",
				"error_description": "This email is already linked to a different sign-in method.",
			})
			return
		case errors.Is(err, errUnverifiedEmailMatch):
			c.JSON(http.StatusForbidden, gin.H{
				"error":             "email_not_verified",
				"error_description": "Email address must be verified by your sign-in provider.",
			})
			return
		}
		body, msg := userPersistError(providerID, err)
		slogging.Get().WithContext(c).Error("%s", msg)
		c.JSON(http.StatusInternalServerError, body)
		return
	}

	// Update user profile if this was an existing user match
	if matchType != userMatchNone {
		// Don't fail login on update error - user can still authenticate with stale data
		_ = h.updateUserOnLogin(ctx, c, &user, matchType, providerID, userInfo.ID, email, name, emailVerified)
	}

	// Generate TMI JWT tokens (the provider ID will be used as subject in the JWT)
	tokenPair, err := h.service.GenerateTokensWithUserInfo(ctx, user, userInfo)
	if err != nil {
		body, msg := tokenIssuanceError(user.Email, err)
		slogging.Get().WithContext(c).Error("%s", msg)
		c.JSON(http.StatusInternalServerError, body)
		return
	}

	// Set HttpOnly session cookies (browser SPA can use these instead of localStorage)
	if h.cookieOpts.Enabled {
		SetTokenCookies(c, tokenPair, h.cookieOpts)
	}

	// Return TMI tokens
	c.JSON(http.StatusOK, tokenPair)
}

// Token exchanges an authorization code for tokens
func (h *Handlers) Token(c *gin.Context) {
	var req struct {
		GrantType    string `json:"grant_type" form:"grant_type"`
		Code         string `json:"code" form:"code"`
		CodeVerifier string `json:"code_verifier" form:"code_verifier"`
		RefreshToken string `json:"refresh_token" form:"refresh_token"` //nolint:gosec // G117 - OAuth token request field
		RedirectURI  string `json:"redirect_uri" form:"redirect_uri"`
		ClientID     string `json:"client_id" form:"client_id"`
		ClientSecret string `json:"client_secret" form:"client_secret"` //nolint:gosec // G117 - OAuth token request field
	}

	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request",
		})
		return
	}

	switch req.GrantType {
	case "authorization_code":
		// Handle authorization code grant with PKCE inline (don't delegate to Exchange to avoid double body read)
		if req.Code == "" || req.RedirectURI == "" || req.CodeVerifier == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_request",
				"error_description": "Missing required fields for authorization_code grant",
			})
			return
		}

		// Call handleAuthorizationCodeGrant which contains the Exchange logic
		h.handleAuthorizationCodeGrant(c, req.Code, req.CodeVerifier, req.RedirectURI)

	case "refresh_token":
		// Handle refresh token grant
		// If no refresh token in body, try cookie (browser SPA flow)
		if req.RefreshToken == "" && h.cookieOpts.Enabled {
			req.RefreshToken = ExtractRefreshTokenFromCookie(c)
		}
		if req.RefreshToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Missing refresh_token parameter",
			})
			return
		}

		// Refresh the token
		tokenPair, err := h.service.RefreshToken(c.Request.Context(), req.RefreshToken)
		if err != nil {
			// On refresh failure, clear cookies (token is invalid/expired)
			if h.cookieOpts.Enabled {
				ClearTokenCookies(c, h.cookieOpts)
			}
			body, msg := refreshTokenError(err)
			slogging.Get().WithContext(c).Error("%s", msg)
			c.JSON(http.StatusBadRequest, body)
			return
		}

		if h.cookieOpts.Enabled {
			SetTokenCookies(c, tokenPair, h.cookieOpts)
		}
		c.JSON(http.StatusOK, tokenPair)

	case "client_credentials":
		// Handle client credentials grant (RFC 6749 Section 4.4)
		if req.ClientID == "" || req.ClientSecret == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_request",
				"error_description": "Missing client_id or client_secret parameter",
			})
			return
		}

		// Exchange client credentials for access token
		tokenPair, err := h.service.HandleClientCredentialsGrant(c.Request.Context(), req.ClientID, req.ClientSecret)
		if err != nil {
			// Use standard OAuth error codes
			if err.Error() == "invalid_client" {
				c.JSON(http.StatusUnauthorized, gin.H{
					"error":             "invalid_client",
					"error_description": "Client authentication failed",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":             "server_error",
				"error_description": "Failed to process client credentials grant",
			})
			return
		}

		c.JSON(http.StatusOK, tokenPair)

	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Unsupported grant type: %s", req.GrantType),
		})
	}
}

// Refresh refreshes an access token
func (h *Handlers) Refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"` //nolint:gosec // G117 - OAuth refresh token request field
	}

	// Don't fail on missing body — cookie-based refresh sends empty body
	_ = c.ShouldBindJSON(&req)

	// If no refresh token in body, try cookie (browser SPA flow)
	if req.RefreshToken == "" && h.cookieOpts.Enabled {
		req.RefreshToken = ExtractRefreshTokenFromCookie(c)
	}

	if req.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Missing refresh_token",
		})
		return
	}

	// Refresh the token
	tokenPair, err := h.service.RefreshToken(c.Request.Context(), req.RefreshToken)
	if err != nil {
		// On refresh failure, clear cookies (token is invalid/expired)
		if h.cookieOpts.Enabled {
			ClearTokenCookies(c, h.cookieOpts)
		}
		body, msg := refreshTokenError(err)
		slogging.Get().WithContext(c).Error("%s", msg)
		c.JSON(http.StatusBadRequest, body)
		return
	}

	if h.cookieOpts.Enabled {
		SetTokenCookies(c, tokenPair, h.cookieOpts)
	}
	c.JSON(http.StatusOK, tokenPair)
}
