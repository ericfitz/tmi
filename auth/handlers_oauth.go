package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// Authorize redirects to the OAuth provider's authorization page
func (h *Handlers) Authorize(c *gin.Context) {
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
				"error": err.Error(),
			})
		}
		return
	}

	// Validate scope parameter according to OpenID Connect specification
	scope := c.Query("scope")
	if err := h.validateOAuthScope(scope); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_scope",
			"error_description": err.Error(),
		})
		return
	}

	// Validate response_type parameter according to OAuth 2.0/OIDC specification
	responseType := c.Query("response_type")
	// PKCE only supports authorization code flow
	if responseType == "" {
		responseType = "code" // Default to authorization code flow
	}
	if responseType != "code" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "unsupported_response_type",
			"error_description": "Only authorization code flow (response_type=code) is supported with PKCE",
		})
		return
	}

	// Get optional client callback URL from query parameter
	clientCallback := c.Query("client_callback")

	// Get optional login_hint for test provider automation
	userHint := c.Query("login_hint")

	// Extract PKCE parameters (required for PKCE flow)
	codeChallenge := c.Query("code_challenge")
	codeChallengeMethod := c.Query("code_challenge_method")

	slogging.Get().WithContext(c).Debug("OAuth Authorize handler - extracted query parameters: provider=%s, client_callback=%s, login_hint=%s, scope=%s, code_challenge_method=%s",
		providerID, clientCallback, userHint, scope, codeChallengeMethod)

	// Validate PKCE parameters
	if codeChallenge == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "code_challenge parameter is required for PKCE",
		})
		return
	}

	if codeChallengeMethod == "" {
		codeChallengeMethod = pkceMethodS256 // Default to S256 if not specified
	}

	if codeChallengeMethod != pkceMethodS256 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "Only S256 code_challenge_method is supported",
		})
		return
	}

	// Validate code_challenge format
	if err := ValidateCodeChallengeFormat(codeChallenge); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": fmt.Sprintf("Invalid code_challenge format: %v", err),
		})
		return
	}

	// Get state parameter from client or generate one if not provided
	state := c.Query("state")
	if state == "" {
		// Generate a state parameter to prevent CSRF if client didn't provide one
		var err error
		state, err = generateRandomState()
		if err != nil {
			slogging.Get().WithContext(c).Error("Failed to generate OAuth state parameter: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to generate state parameter",
			})
			return
		}
	}

	// Store the state and client callback in Redis with a 10-minute expiration
	stateKey := fmt.Sprintf("oauth_state:%s", state)
	ctx := c.Request.Context()

	// Store provider ID and optional client callback URL/login_hint
	stateData := map[string]string{
		"provider": providerID,
	}
	if clientCallback != "" {
		stateData["client_callback"] = clientCallback
	}
	if userHint != "" {
		stateData["login_hint"] = userHint
		slogging.Get().WithContext(c).Debug("Storing login_hint in state: %s", userHint)
	}

	stateJSON, err := json.Marshal(stateData)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to marshal OAuth state data for provider %s: %v", providerID, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to encode state data",
		})
		return
	}

	// Check if service is available (for testing purposes)
	if h.service == nil {
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "OAuth service temporarily unavailable",
		})
		return
	}

	err = h.service.dbManager.Redis().Set(ctx, stateKey, string(stateJSON), 10*time.Minute)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to store OAuth state in Redis (key: %s, provider: %s): %v", stateKey, providerID, err)
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "State storage temporarily unavailable - please retry",
		})
		return
	}

	// Store PKCE challenge with state
	err = h.service.stateStore.StorePKCEChallenge(ctx, state, codeChallenge, codeChallengeMethod, 10*time.Minute)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to store PKCE challenge for state %s: %v", state, err)
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "PKCE storage temporarily unavailable - please retry",
		})
		return
	}

	slogging.Get().WithContext(c).Debug("Stored PKCE challenge for state: %s (method: %s)", state, codeChallengeMethod)

	// For authorization code flow, handle client_callback if provided
	if providerID == tmiProviderID && clientCallback != "" {
		slogging.Get().WithContext(c).Debug("Authorization code flow with client_callback, redirecting directly to client")
		// Generate test authorization code with login_hint encoded if available
		authCode := fmt.Sprintf("test_auth_code_%d", time.Now().Unix())
		if userHint != "" {
			// Encode login_hint into the authorization code for later retrieval
			encodedHint := base64.URLEncoding.EncodeToString([]byte(userHint))
			authCode = fmt.Sprintf("test_auth_code_%d_hint_%s", time.Now().Unix(), encodedHint)
			slogging.Get().WithContext(c).Debug("Generated auth code with login_hint: %s", userHint)
		}

		// Bind PKCE challenge to authorization code
		// Retrieve PKCE challenge from state
		pkceChallenge, pkceMethod, err := h.service.stateStore.GetPKCEChallenge(ctx, state)
		if err != nil {
			slogging.Get().WithContext(c).Error("Failed to retrieve PKCE challenge for state %s: %v", state, err)
			c.Header("Retry-After", "30")
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "PKCE storage temporarily unavailable - please retry",
			})
			return
		}

		// Store PKCE challenge with authorization code for later validation
		codeKey := fmt.Sprintf("pkce:%s", authCode)
		pkceData := map[string]string{
			"code_challenge":        pkceChallenge,
			"code_challenge_method": pkceMethod,
		}
		pkceJSON, err := json.Marshal(pkceData)
		if err != nil {
			slogging.Get().WithContext(c).Error("Failed to marshal PKCE data for code: %v", err)
			// This is a true internal error - JSON marshaling of our own data failed
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Internal error processing PKCE data",
			})
			return
		}

		err = h.service.dbManager.Redis().Set(ctx, codeKey, string(pkceJSON), 10*time.Minute)
		if err != nil {
			slogging.Get().WithContext(c).Error("Failed to store PKCE challenge for code %s: %v", authCode, err)
			c.Header("Retry-After", "30")
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "PKCE storage temporarily unavailable - please retry",
			})
			return
		}

		// Delete PKCE challenge from state (moved to code)
		_ = h.service.stateStore.DeletePKCEChallenge(ctx, state)

		slogging.Get().WithContext(c).Debug("Bound PKCE challenge to authorization code")

		// Build redirect URL with code and state
		redirectURL := fmt.Sprintf("%s?code=%s&state=%s", clientCallback, authCode, url.QueryEscape(state))
		slogging.Get().WithContext(c).Debug("Redirecting to client callback: %s", redirectURL)
		c.Redirect(http.StatusFound, redirectURL)
	} else {
		// For normal authorization code flow, get the authorization URL and redirect
		authURL := provider.GetAuthorizationURL(state)
		c.Redirect(http.StatusFound, authURL)
	}
}

// callbackStateData holds parsed OAuth state information
type callbackStateData struct {
	ProviderID     string
	ClientCallback string
	UserHint       string
}

// Callback handles the OAuth callback
func (h *Handlers) Callback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")

	if code == "" || state == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Missing code or state parameter",
		})
		return
	}

	// Parse and validate state
	stateData, err := h.parseCallbackState(c, state)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid state parameter",
		})
		return
	}

	// Handle the OAuth flow
	err = h.processOAuthCallback(c, code, stateData)
	if err != nil {
		// Error already handled in processOAuthCallback
		return
	}
}

// parseCallbackState retrieves and parses OAuth state data
func (h *Handlers) parseCallbackState(c *gin.Context, state string) (*callbackStateData, error) {
	stateKey := fmt.Sprintf("oauth_state:%s", state)
	ctx := c.Request.Context()
	stateDataJSON, err := h.service.dbManager.Redis().Get(ctx, stateKey)
	if err != nil {
		return nil, err
	}

	// Delete the state from Redis
	_ = h.service.dbManager.Redis().Del(ctx, stateKey)

	// Parse the state data (structured JSON format)
	var stateMap map[string]string
	if err := json.Unmarshal([]byte(stateDataJSON), &stateMap); err != nil {
		return nil, fmt.Errorf("invalid state data format: %w", err)
	}

	result := &callbackStateData{
		ProviderID:     stateMap["provider"],
		ClientCallback: stateMap["client_callback"],
		UserHint:       stateMap["login_hint"],
	}

	slogging.Get().WithContext(c).Debug("Retrieved state data: provider=%s, client_callback=%s, login_hint=%s",
		result.ProviderID, result.ClientCallback, result.UserHint)

	return result, nil
}

// processOAuthCallback handles the core OAuth callback flow for PKCE
// Returns authorization code to client without exchanging it
func (h *Handlers) processOAuthCallback(c *gin.Context, code string, stateData *callbackStateData) error {
	// PKCE flow: Return authorization code to client for token exchange
	// Client will call /oauth2/token with code and code_verifier

	// Require client callback URL
	if stateData.ClientCallback == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "client_callback URL is required",
		})
		return fmt.Errorf("missing client_callback")
	}

	// Bind PKCE challenge to authorization code for later validation during token exchange
	// Retrieve PKCE challenge from state
	ctx := c.Request.Context()
	state := c.Query("state")
	pkceChallenge, pkceMethod, err := h.service.stateStore.GetPKCEChallenge(ctx, state)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to retrieve PKCE challenge for state %s: %v", state, err)
		h.redirectWithErrorOAuth(c, stateData.ClientCallback, http.StatusInternalServerError, "Failed to retrieve PKCE challenge")
		return fmt.Errorf("failed to retrieve PKCE challenge: %w", err)
	}

	// Store PKCE challenge with authorization code for later validation
	codeKey := fmt.Sprintf("pkce:%s", code)
	pkceData := map[string]string{
		"code_challenge":        pkceChallenge,
		"code_challenge_method": pkceMethod,
	}
	pkceJSON, err := json.Marshal(pkceData)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to marshal PKCE data for code: %v", err)
		h.redirectWithErrorOAuth(c, stateData.ClientCallback, http.StatusInternalServerError, "Failed to store PKCE challenge")
		return fmt.Errorf("failed to marshal PKCE data: %w", err)
	}

	err = h.service.dbManager.Redis().Set(ctx, codeKey, string(pkceJSON), 10*time.Minute)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to store PKCE challenge for code %s: %v", code, err)
		h.redirectWithErrorOAuth(c, stateData.ClientCallback, http.StatusInternalServerError, "Failed to store PKCE challenge")
		return fmt.Errorf("failed to store PKCE challenge: %w", err)
	}

	// Delete PKCE challenge from state (moved to code)
	_ = h.service.stateStore.DeletePKCEChallenge(ctx, state)

	slogging.Get().WithContext(c).Debug("Bound PKCE challenge to authorization code")

	// Build redirect URL with authorization code and state
	redirectURL, err := buildAuthCodeRedirectURL(stateData.ClientCallback, code, state)
	if err != nil {
		h.redirectWithErrorOAuth(c, stateData.ClientCallback, http.StatusInternalServerError, fmt.Sprintf("Failed to build redirect URL: %v", err))
		return err
	}

	slogging.Get().WithContext(c).Debug("Redirecting to client with authorization code: %s", redirectURL)
	c.Redirect(http.StatusFound, redirectURL)
	return nil
}
