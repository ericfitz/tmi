package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Context key type for context values
type contextKey string

const (
	// userHintContextKey is the key for login_hint in context
	userHintContextKey contextKey = "login_hint"
	// UserContextKey is the key for the user in the Gin context
	UserContextKey contextKey = "user"
)

// wwwAuthenticateRealm identifies the protection space for Bearer token authentication.
const wwwAuthenticateRealm = "tmi"

// setWWWAuthenticateHeader sets a RFC 6750 compliant WWW-Authenticate header.
// This is a package-local helper to avoid circular dependencies with the api package.
func setWWWAuthenticateHeader(c *gin.Context, errType, description string) {
	header := fmt.Sprintf(`Bearer realm="%s"`, wwwAuthenticateRealm)
	if errType != "" {
		header += fmt.Sprintf(`, error="%s"`, errType)
		if description != "" {
			escapedDesc := strings.ReplaceAll(description, `"`, `\"`)
			header += fmt.Sprintf(`, error_description="%s"`, escapedDesc)
		}
	}
	c.Header("WWW-Authenticate", header)
}

// AdminChecker is an interface for checking if a user is an administrator
type AdminChecker interface {
	IsAdmin(ctx context.Context, userInternalUUID *string, provider string, groupUUIDs []string) (bool, error)
	GetGroupUUIDsByNames(ctx context.Context, provider string, groupNames []string) ([]string, error)
}

// Handlers provides HTTP handlers for authentication
type Handlers struct {
	service      *Service
	config       Config
	adminChecker AdminChecker
}

// NewHandlers creates new authentication handlers
func NewHandlers(service *Service, config Config) *Handlers {
	return &Handlers{
		service: service,
		config:  config,
	}
}

// SetAdminChecker sets the admin checker for the handlers
func (h *Handlers) SetAdminChecker(checker AdminChecker) {
	h.adminChecker = checker
}

// Service returns the auth service (getter for unexported field)
func (h *Handlers) Service() *Service {
	return h.service
}

// Config returns the auth config (getter for unexported field)
func (h *Handlers) Config() Config {
	return h.config
}

// Note: Route registration has been removed. All routes are now registered via OpenAPI
// specification in api/api.go. The auth handlers are called through the Server's
// AuthService adapter.

// ProviderInfo contains information about an OAuth provider
type ProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	AuthURL     string `json:"auth_url"`
	TokenURL    string `json:"token_url"`
	RedirectURI string `json:"redirect_uri"`
	ClientID    string `json:"client_id"`
}

// SAMLProviderInfo contains public information about a SAML provider
type SAMLProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	AuthURL     string `json:"auth_url"`
	MetadataURL string `json:"metadata_url"`
	EntityID    string `json:"entity_id"`
	ACSURL      string `json:"acs_url"`
	SLOURL      string `json:"slo_url,omitempty"`
}

// GetProviders returns the available OAuth providers
func (h *Handlers) GetProviders(c *gin.Context) {
	providers := make([]ProviderInfo, 0, len(h.config.OAuth.Providers))

	for id, providerConfig := range h.config.OAuth.Providers {
		if !providerConfig.Enabled {
			continue
		}

		// Use configured name or fallback to ID
		name := providerConfig.Name
		if name == "" {
			name = id
		}

		// Use configured icon or fallback to ID
		icon := providerConfig.Icon
		if icon == "" {
			icon = id
		}

		// Build the authorization URL for this provider (using query parameter format)
		authURL := fmt.Sprintf("%s/oauth2/authorize?idp=%s", getBaseURL(c), id)

		// Build the token URL for this provider
		tokenURL := fmt.Sprintf("%s/oauth2/token?idp=%s", getBaseURL(c), id)

		providers = append(providers, ProviderInfo{
			ID:          id,
			Name:        name,
			Icon:        icon,
			AuthURL:     authURL,
			TokenURL:    tokenURL,
			RedirectURI: h.config.OAuth.CallbackURL,
			ClientID:    providerConfig.ClientID,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"providers": providers,
	})
}

// GetSAMLProviders returns the available SAML providers
func (h *Handlers) GetSAMLProviders(c *gin.Context) {
	// Return empty array if SAML disabled
	if !h.config.SAML.Enabled {
		c.JSON(http.StatusOK, gin.H{"providers": []SAMLProviderInfo{}})
		return
	}

	providers := make([]SAMLProviderInfo, 0, len(h.config.SAML.Providers))
	baseURL := getBaseURL(c)

	for id, providerConfig := range h.config.SAML.Providers {
		// Only include enabled providers
		if !providerConfig.Enabled {
			continue
		}

		// Use provider name or ID as fallback
		name := providerConfig.Name
		if name == "" {
			name = id
		}

		// Use provider icon or default SAML icon
		icon := providerConfig.Icon
		if icon == "" {
			icon = "fa-solid fa-key"
		}

		// Build public URLs (using path parameters)
		authURL := fmt.Sprintf("%s/saml/%s/login", baseURL, id)
		metadataURL := fmt.Sprintf("%s/saml/%s/metadata", baseURL, id)

		providers = append(providers, SAMLProviderInfo{
			ID:          id,
			Name:        name,
			Icon:        icon,
			AuthURL:     authURL,
			MetadataURL: metadataURL,
			EntityID:    providerConfig.EntityID,
			ACSURL:      providerConfig.ACSURL,
			SLOURL:      providerConfig.SLOURL,
		})
	}

	// Cache for 1 hour
	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, gin.H{"providers": providers})
}

// getProvider returns a Provider instance for the given provider ID
func (h *Handlers) getProvider(providerID string) (Provider, error) {
	providerConfig, exists := h.config.OAuth.Providers[providerID]
	if !exists {
		return nil, fmt.Errorf("provider %s not found", providerID)
	}

	return NewProvider(providerConfig, h.config.OAuth.CallbackURL)
}

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
		codeChallengeMethod = "S256" // Default to S256 if not specified
	}

	if codeChallengeMethod != "S256" {
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
	if providerID == "tmi" && clientCallback != "" {
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

// setUserHintContext adds login_hint to context for TMI provider
func (h *Handlers) setUserHintContext(c *gin.Context, ctx context.Context, stateData *callbackStateData) context.Context {
	if stateData.UserHint != "" && stateData.ProviderID == "tmi" {
		slogging.Get().WithContext(c).Debug("Setting login_hint in context for TMI provider: %s", stateData.UserHint)
		return context.WithValue(ctx, userHintContextKey, stateData.UserHint)
	} else if stateData.ProviderID == "tmi" {
		slogging.Get().WithContext(c).Debug("No login_hint provided for TMI provider: provider=%s userHint=%s",
			stateData.ProviderID, stateData.UserHint)
	}
	return ctx
}

// exchangeCodeAndGetUser exchanges OAuth code for tokens and gets user info
func (h *Handlers) exchangeCodeAndGetUser(c *gin.Context, ctx context.Context, provider Provider, code string, callbackURL string) (*TokenResponse, *UserInfo, *IDTokenClaims, error) {
	logger := slogging.Get().WithContext(c)
	logger.Debug("About to call ExchangeCode: code=%s has_login_hint_in_context=%v",
		code, ctx.Value(userHintContextKey) != nil)

	tokenResponse, err := provider.ExchangeCode(ctx, code)
	if err != nil {
		if strings.Contains(err.Error(), "invalid authorization code") ||
			strings.Contains(err.Error(), "authorization code is required") {
			h.redirectWithErrorOAuth(c, callbackURL, http.StatusBadRequest, err.Error())
		} else {
			logger.Error("Failed to exchange OAuth authorization code for tokens (code prefix: %.10s...): %v", code, err)
			h.redirectWithErrorOAuth(c, callbackURL, http.StatusInternalServerError, fmt.Sprintf("Failed to exchange code for tokens: %v", err))
		}
		return nil, nil, nil, err
	}

	// Validate ID token if present
	var claims *IDTokenClaims
	if tokenResponse.IDToken != "" {
		claims, err = provider.ValidateIDToken(ctx, tokenResponse.IDToken)
		if err != nil {
			logger.Error("Failed to validate ID token: %v", err)
		}
	}

	// Get user info
	logger.Debug("About to call GetUserInfo: access_token=%s", tokenResponse.AccessToken)
	userInfo, err := provider.GetUserInfo(ctx, tokenResponse.AccessToken)
	if err != nil {
		logger.Error("Failed to get user info from OAuth provider using access token: %v", err)
		h.redirectWithErrorOAuth(c, callbackURL, http.StatusInternalServerError, fmt.Sprintf("Failed to get user info: %v", err))
		return nil, nil, nil, err
	}

	logger.Debug("GetUserInfo returned: user_id=%s email=%s name=%s",
		userInfo.ID, userInfo.Email, userInfo.Name)

	return tokenResponse, userInfo, claims, nil
}

// extractEmailWithFallback extracts email from userInfo/claims with fallback to synthetic email
func (h *Handlers) extractEmailWithFallback(c *gin.Context, providerID string, userInfo *UserInfo, claims *IDTokenClaims) (string, error) {
	email := userInfo.Email
	if email == "" && claims != nil {
		email = claims.Email
	}

	if email == "" {
		// Enhanced logging for email retrieval failure
		claimsEmail := "<no_claims>"
		if claims != nil {
			claimsEmail = claims.Email
		}
		slogging.Get().WithContext(c).Warn("OAuth provider returned empty email - using fallback (provider: %s, user_id: %s, name: %s, userInfo.Email: %s, claims.Email: %s, email_verified: %v)",
			providerID, userInfo.ID, userInfo.Name, userInfo.Email, claimsEmail, userInfo.EmailVerified)

		// Fallback: use provider user ID as email identifier
		// This handles cases where:
		// - GitHub user has private email or unverified email
		// - Provider doesn't return email in userinfo or ID token claims
		if userInfo.ID == "" {
			slogging.Get().WithContext(c).Error("OAuth provider returned no email and no user ID (provider: %s, name: %s)", providerID, userInfo.Name)
			return "", fmt.Errorf("no email or user ID found")
		}

		// Create synthetic email from provider ID and user ID
		// Format: <provider>-<user_id>@<provider>.oauth.tmi
		email = fmt.Sprintf("%s-%s@%s.oauth.tmi", providerID, userInfo.ID, providerID)
		slogging.Get().WithContext(c).Info("Using fallback email for OAuth user (provider: %s, user_id: %s, fallback_email: %s)",
			providerID, userInfo.ID, email)
	}

	return email, nil
}

// userMatchType indicates how a user was matched during login
type userMatchType int

const (
	userMatchNone          userMatchType = iota // No match found, need to create new user
	userMatchProviderID                         // Matched by provider + provider_user_id (strongest)
	userMatchProviderEmail                      // Matched by provider + email
	userMatchEmailOnly                          // Matched by email only (sparse record)
)

// findOrCreateUser implements tiered user matching strategy:
// 1. Provider + Provider ID (strongest) - can update email and name
// 2. Provider + Email - can update name
// 3. Email only (sparse record) - can update provider, provider_id, and name
// Returns the user, match type, and any error
func (h *Handlers) findOrCreateUser(ctx context.Context, c *gin.Context, providerID, providerUserID, email, name string, emailVerified bool) (User, userMatchType, error) {
	logger := slogging.Get().WithContext(c)

	// Tier 1: Try to match by provider + provider_user_id (strongest match)
	user, err := h.service.GetUserByProviderID(ctx, providerID, providerUserID)
	if err == nil {
		logger.Debug("User matched by provider+provider_id: provider=%s, provider_id=%s, email=%s",
			providerID, providerUserID, user.Email)
		return user, userMatchProviderID, nil
	}

	// Tier 2: Try to match by provider + email
	user, err = h.service.GetUserByProviderAndEmail(ctx, providerID, email)
	if err == nil {
		logger.Debug("User matched by provider+email: provider=%s, email=%s, existing_provider_id=%s",
			providerID, email, user.ProviderUserID)
		return user, userMatchProviderEmail, nil
	}

	// Tier 3: Try to match by email only (sparse record or different provider)
	user, err = h.service.GetUserByEmail(ctx, email)
	if err == nil {
		// Check if this is a sparse record (no provider set) or a different provider
		if user.Provider == "" {
			logger.Debug("User matched by email only (sparse record): email=%s", email)
			return user, userMatchEmailOnly, nil
		}
		// User exists with a different provider - this is a conflict
		// For now, we'll treat it as a sparse record match to allow completing it
		// In a multi-provider setup, you might want to link accounts instead
		logger.Info("User matched by email with different provider: email=%s, existing_provider=%s, new_provider=%s",
			email, user.Provider, providerID)
		return user, userMatchEmailOnly, nil
	}

	// No match found - need to create new user
	logger.Debug("No existing user found, will create new: provider=%s, provider_id=%s, email=%s",
		providerID, providerUserID, email)

	nowTime := time.Now()
	newUser := User{
		Provider:       providerID,
		ProviderUserID: providerUserID,
		Email:          email,
		Name:           name,
		EmailVerified:  emailVerified,
		CreatedAt:      nowTime,
		ModifiedAt:     nowTime,
		LastLogin:      &nowTime,
	}

	createdUser, err := h.service.CreateUser(ctx, newUser)
	if err != nil {
		logger.Error("Failed to create new user: email=%s, name=%s, error=%v", email, name, err)
		return User{}, userMatchNone, fmt.Errorf("failed to create user: %w", err)
	}

	return createdUser, userMatchNone, nil
}

// updateUserOnLogin updates user fields based on match type and OAuth data
func (h *Handlers) updateUserOnLogin(ctx context.Context, c *gin.Context, user *User, matchType userMatchType, providerID, providerUserID, email, name string, emailVerified bool) error {
	logger := slogging.Get().WithContext(c)
	updateNeeded := false

	now := time.Now()
	user.LastLogin = &now
	user.ModifiedAt = now

	switch matchType {
	case userMatchProviderID:
		// Strongest match - can update email and name if changed
		if email != "" && user.Email != email {
			logger.Info("Updating user email on login: old=%s, new=%s (matched by provider_id)", user.Email, email)
			user.Email = email
			updateNeeded = true
		}
		if name != "" && user.Name != name {
			logger.Info("Updating user name on login: old=%s, new=%s (matched by provider_id)", user.Name, name)
			user.Name = name
			updateNeeded = true
		}

	case userMatchProviderEmail:
		// Medium match - can update name and provider_user_id if empty
		if user.ProviderUserID == "" && providerUserID != "" {
			logger.Info("Completing user record with provider_id: user=%s, provider_id=%s", user.Email, providerUserID)
			user.ProviderUserID = providerUserID
			updateNeeded = true
		}
		if name != "" && user.Name != name {
			logger.Info("Updating user name on login: old=%s, new=%s (matched by provider+email)", user.Name, name)
			user.Name = name
			updateNeeded = true
		}

	case userMatchEmailOnly:
		// Sparse record match - update provider, provider_id, and name
		if user.Provider == "" && providerID != "" {
			logger.Info("Completing sparse user record with provider: user=%s, provider=%s", user.Email, providerID)
			user.Provider = providerID
			updateNeeded = true
		}
		if user.ProviderUserID == "" && providerUserID != "" {
			logger.Info("Completing sparse user record with provider_id: user=%s, provider_id=%s", user.Email, providerUserID)
			user.ProviderUserID = providerUserID
			updateNeeded = true
		}
		if name != "" && user.Name != name {
			logger.Info("Updating user name on login: old=%s, new=%s (matched by email only)", user.Name, name)
			user.Name = name
			updateNeeded = true
		}
	}

	// Always update email_verified status (one-way: false -> true)
	if emailVerified && !user.EmailVerified {
		user.EmailVerified = true
		updateNeeded = true
	}

	if updateNeeded {
		if err := h.service.UpdateUser(ctx, *user); err != nil {
			logger.Error("Failed to update user profile during login: %v (user_id: %s)", err, user.InternalUUID)
			return err
		}
	}

	return nil
}

// createOrGetUser creates a new user or gets existing user
func (h *Handlers) createOrGetUser(c *gin.Context, ctx context.Context, providerID string, userInfo *UserInfo, claims *IDTokenClaims, callbackURL string) (User, error) {
	email, err := h.extractEmailWithFallback(c, providerID, userInfo, claims)
	if err != nil {
		h.redirectWithErrorOAuth(c, callbackURL, http.StatusInternalServerError, "Failed to get user email or ID from provider")
		return User{}, err
	}

	name := userInfo.Name
	if name == "" && claims != nil {
		name = claims.Name
	}
	if name == "" {
		name = email
	}

	user, err := h.service.GetUserByEmail(ctx, email)
	if err != nil {
		// Create a new user with provider data
		// Note: GivenName, FamilyName, Picture, Locale are ignored per schema requirements
		nowTime := time.Now()
		user = User{
			Provider:       providerID,
			ProviderUserID: userInfo.ID,
			Email:          email,
			Name:           name,
			EmailVerified:  userInfo.EmailVerified,
			CreatedAt:      nowTime,
			ModifiedAt:     nowTime,
			LastLogin:      &nowTime,
		}

		user, err = h.service.CreateUser(ctx, user)
		if err != nil {
			h.redirectWithErrorOAuth(c, callbackURL, http.StatusInternalServerError, fmt.Sprintf("Failed to create user: %v", err))
			return User{}, err
		}
	} else {
		// User exists - update profile with fresh OAuth data on each login
		updateNeeded := false

		if name != "" && user.Name != name {
			user.Name = name
			updateNeeded = true
		}
		// Note: GivenName, FamilyName, Picture, Locale are ignored per schema requirements

		// Always update last login and email verification status
		now := time.Now()
		user.LastLogin = &now
		user.ModifiedAt = time.Now()
		if userInfo.EmailVerified {
			user.EmailVerified = true
			updateNeeded = true
		}

		if updateNeeded {
			err = h.service.UpdateUser(ctx, user)
			if err != nil {
				// Log error but don't fail the login - user can still authenticate with stale data
				slogging.Get().WithContext(c).Error("Failed to update user profile during login: %v", err)
			}
		}
	}

	return user, nil
}

// generateAndReturnTokens generates JWT tokens and redirects to client callback
func (h *Handlers) generateAndReturnTokens(c *gin.Context, ctx context.Context, user User, userInfo *UserInfo, stateData *callbackStateData) error {
	tokenPair, err := h.service.GenerateTokensWithUserInfo(ctx, user, userInfo)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to generate JWT tokens for user %s: %v", user.Email, err)
		h.redirectWithErrorOAuth(c, stateData.ClientCallback, http.StatusInternalServerError, fmt.Sprintf("Failed to generate tokens: %v", err))
		return err
	}

	// Require client callback URL
	if stateData.ClientCallback == "" {
		h.redirectWithErrorOAuth(c, stateData.ClientCallback, http.StatusBadRequest, "client_callback URL is required")
		return fmt.Errorf("missing client_callback")
	}

	redirectURL, err := buildClientRedirectURL(stateData.ClientCallback, tokenPair, c.Query("state"))
	if err != nil {
		h.redirectWithErrorOAuth(c, stateData.ClientCallback, http.StatusInternalServerError, fmt.Sprintf("Failed to build redirect URL: %v", err))
		return err
	}

	c.Redirect(http.StatusFound, redirectURL)
	return nil
}

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
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": fmt.Sprintf("Invalid code_verifier format: %v", err),
		})
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
		// Check if it's an invalid code error (client error) vs server error
		if strings.Contains(err.Error(), "invalid authorization code") ||
			strings.Contains(err.Error(), "authorization code is required") {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
		} else {
			slogging.Get().WithContext(c).Error("Failed to exchange authorization code for tokens in callback (provider: %s, code prefix: %.10s...): %v", providerID, code, err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Failed to exchange authorization code: %v", err),
			})
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
		slogging.Get().WithContext(c).Error("Failed to get user info from OAuth provider in exchange (provider: %s): %v", providerID, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get user info: %v", err),
		})
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
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to find or create user: %v", err),
		})
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
		slogging.Get().WithContext(c).Error("Failed to generate JWT tokens for user %s: %v", user.Email, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to generate tokens: %v", err),
		})
		return
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
		RefreshToken string `json:"refresh_token" form:"refresh_token"`
		RedirectURI  string `json:"redirect_uri" form:"redirect_uri"`
		ClientID     string `json:"client_id" form:"client_id"`
		ClientSecret string `json:"client_secret" form:"client_secret"`
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
		if req.RefreshToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Missing refresh_token parameter",
			})
			return
		}

		// Refresh the token
		tokenPair, err := h.service.RefreshToken(c.Request.Context(), req.RefreshToken)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("Failed to refresh token: %v", err),
			})
			return
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
		RefreshToken string `json:"refresh_token" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request",
		})
		return
	}

	// Refresh the token
	tokenPair, err := h.service.RefreshToken(c.Request.Context(), req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Failed to refresh token: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, tokenPair)
}

// revokeTokenInternal handles the actual token revocation logic
// This is shared between RevokeToken (RFC 7009) and MeLogout endpoints
func (h *Handlers) revokeTokenInternal(ctx context.Context, tokenString string, tokenTypeHint string) error {
	logger := slogging.Get()

	// Check if blacklist service is available
	if h.service == nil || h.service.dbManager == nil || h.service.dbManager.Redis() == nil {
		logger.Error("Token blacklist service not available")
		return fmt.Errorf("blacklist service unavailable")
	}

	// Try to determine token type if hint not provided or is access_token
	if tokenTypeHint == "" || tokenTypeHint == "access_token" {
		// Try to parse as JWT to check if it's a valid access token
		claims := jwt.MapClaims{}
		token, err := h.service.GetKeyManager().VerifyToken(tokenString, claims)
		if err == nil && token.Valid {
			// It's a valid access token - blacklist it
			blacklist := NewTokenBlacklist(h.service.dbManager.Redis().GetClient(), h.service.GetKeyManager())
			if err := blacklist.BlacklistToken(ctx, tokenString); err != nil {
				logger.Error("Failed to blacklist access token: %v", err)
				return err
			}
			logger.Debug("Access token blacklisted successfully")
			return nil
		}
		// Not a valid access token, fall through to try as refresh token if no hint
		if tokenTypeHint == "access_token" {
			// Hint was explicitly access_token but it's not valid - still return success per RFC 7009
			logger.Debug("Token provided with access_token hint is not a valid access token")
			return nil
		}
	}

	// Try as refresh token
	if tokenTypeHint == "" || tokenTypeHint == "refresh_token" {
		if err := h.service.RevokeToken(ctx, tokenString); err != nil {
			logger.Debug("Failed to revoke as refresh token (may not exist): %v", err)
			// Per RFC 7009, we still return success even if token doesn't exist
		} else {
			logger.Debug("Refresh token revoked successfully")
		}
	}

	return nil
}

// containsZeroWidthCharsAuth checks for zero-width Unicode characters that can be used for spoofing
func containsZeroWidthCharsAuth(s string) bool {
	zeroWidthChars := []rune{
		'\u200B', '\u200C', '\u200D', '\u200E', '\u200F',
		'\u202A', '\u202B', '\u202C', '\u202D', '\u202E',
		'\uFEFF',
	}
	for _, r := range s {
		for _, zw := range zeroWidthChars {
			if r == zw {
				return true
			}
		}
	}
	return false
}

// containsControlCharsAuth checks for control characters (except common whitespace)
func containsControlCharsAuth(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' && r != ' ' {
			return true
		}
	}
	return false
}

// validateTokenRevocationField validates a field value for the token revocation endpoint
func validateTokenRevocationField(value, fieldName string) string {
	if value == "" {
		return ""
	}

	// Check for zero-width characters
	if containsZeroWidthCharsAuth(value) {
		return fmt.Sprintf("%s contains invalid zero-width characters", fieldName)
	}

	// Check for control characters
	if containsControlCharsAuth(value) {
		return fmt.Sprintf("%s contains invalid control characters", fieldName)
	}

	return ""
}

// validateTokenTypeHint validates the token_type_hint parameter
func validateTokenTypeHint(hint string) string {
	if hint == "" {
		return "" // Optional field
	}

	// Per RFC 7009, valid values are "access_token" and "refresh_token"
	validHints := map[string]bool{
		"access_token":  true,
		"refresh_token": true,
	}

	if !validHints[hint] {
		return "token_type_hint must be 'access_token' or 'refresh_token'"
	}

	return ""
}

// RevokeToken revokes a token per RFC 7009 OAuth 2.0 Token Revocation
// The token to revoke is passed in the request body, not the Authorization header.
// Authentication: Bearer token OR client credentials (client_id/client_secret)
func (h *Handlers) RevokeToken(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Bind request body (supports both JSON and form-urlencoded)
	var req struct {
		Token         string `json:"token" form:"token" binding:"required"`
		TokenTypeHint string `json:"token_type_hint" form:"token_type_hint"`
		ClientID      string `json:"client_id" form:"client_id"`
		ClientSecret  string `json:"client_secret" form:"client_secret"`
	}

	if err := c.ShouldBind(&req); err != nil {
		// Per RFC 7009 Section 2.2.1: Return 400 for missing token parameter
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "Missing required 'token' parameter",
		})
		return
	}

	// Validate token field for malicious content
	if errMsg := validateTokenRevocationField(req.Token, "token"); errMsg != "" {
		logger.Warn("Invalid token in revocation request: %s", errMsg)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": errMsg,
		})
		return
	}

	// Validate token_type_hint if provided
	if errMsg := validateTokenTypeHint(req.TokenTypeHint); errMsg != "" {
		logger.Warn("Invalid token_type_hint in revocation request: %s", errMsg)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": errMsg,
		})
		return
	}

	// Validate client_id if provided
	if errMsg := validateTokenRevocationField(req.ClientID, "client_id"); errMsg != "" {
		logger.Warn("Invalid client_id in revocation request: %s", errMsg)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": errMsg,
		})
		return
	}

	// Validate client_secret if provided
	if errMsg := validateTokenRevocationField(req.ClientSecret, "client_secret"); errMsg != "" {
		logger.Warn("Invalid client_secret in revocation request: %s", errMsg)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": errMsg,
		})
		return
	}

	// Authenticate the request (one of: Bearer token OR client credentials)
	isAuthenticated := false

	// Method 1: Check for Bearer token in Authorization header
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		bearerToken := strings.TrimPrefix(authHeader, "Bearer ")
		claims := jwt.MapClaims{}
		token, err := h.service.GetKeyManager().VerifyToken(bearerToken, claims)
		if err == nil && token.Valid {
			isAuthenticated = true
			logger.Debug("Revocation request authenticated via Bearer token")
		}
	}

	// Method 2: Check for client credentials in request body
	if !isAuthenticated && req.ClientID != "" && req.ClientSecret != "" {
		// Validate client credentials using existing service method
		_, err := h.service.HandleClientCredentialsGrant(c.Request.Context(), req.ClientID, req.ClientSecret)
		if err == nil {
			isAuthenticated = true
			logger.Debug("Revocation request authenticated via client credentials")
		} else {
			logger.Debug("Client credentials validation failed: %v", err)
		}
	}

	if !isAuthenticated {
		// RFC 7009 Section 2.2.1: 401 for invalid client credentials
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "invalid_client",
			"error_description": "Client authentication failed",
		})
		return
	}

	// Attempt to revoke the token
	// Per RFC 7009 Section 2.2: Always return 200 OK (don't leak token validity)
	_ = h.revokeTokenInternal(c.Request.Context(), req.Token, req.TokenTypeHint)

	// RFC 7009 Section 2.2: "The authorization server responds with HTTP status code 200"
	c.JSON(http.StatusOK, gin.H{})
}

// MeLogout revokes the caller's own JWT token
// This is a convenience endpoint that doesn't require passing the token in the body
func (h *Handlers) MeLogout(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get JWT token from Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "unauthorized",
			"error_description": "Missing or invalid Authorization header",
		})
		return
	}

	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

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
				idp, groups, _ := h.service.GetCachedGroups(c.Request.Context(), userEmail)
				if len(groups) > 0 {
					user.Groups = groups
					if idp != "" {
						user.Provider = idp
					}
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
			response := convertUserToAPIResponse(user)
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

// Helper functions

// convertUserToAPIResponse converts auth.User to a map matching the OpenAPI UserWithAdminStatus schema
// This ensures field names match the API spec (provider_id instead of provider_user_id)
// Used by /me endpoint for TMI-specific user information
func convertUserToAPIResponse(user User) map[string]interface{} {
	return map[string]interface{}{
		"principal_type": "user",
		"provider":       user.Provider,
		"provider_id":    user.ProviderUserID, // Map ProviderUserID to provider_id
		"display_name":   user.Name,
		"email":          user.Email,
		"is_admin":       user.IsAdmin,
	}
}

// convertUserToOIDCResponse converts auth.User to OIDC-compliant userinfo response
// Per OIDC Core 1.0 Section 5.1, only "sub" is required; other claims are optional
// Used by /oauth2/userinfo endpoint for OIDC standard compliance
func convertUserToOIDCResponse(user User) map[string]interface{} {
	response := map[string]interface{}{
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
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}

	// Check for proxy headers that indicate HTTPS
	if forwardedProto := c.GetHeader("X-Forwarded-Proto"); forwardedProto == "https" {
		scheme = "https"
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
		return "", fmt.Errorf("invalid client callback URL: %v", err)
	}

	// Validate that this is a proper absolute URL for OAuth callbacks
	if parsedURL.Scheme == "" {
		return "", fmt.Errorf("invalid client callback URL: missing scheme")
	}
	if parsedURL.Host == "" {
		return "", fmt.Errorf("invalid client callback URL: missing host")
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
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
		return "", fmt.Errorf("invalid client callback URL: %v", err)
	}

	// Validate that this is a proper absolute URL for OAuth callbacks
	if parsedURL.Scheme == "" {
		return "", fmt.Errorf("invalid client callback URL: missing scheme")
	}
	if parsedURL.Host == "" {
		return "", fmt.Errorf("invalid client callback URL: missing host")
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
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
	hasOpenID := false
	for _, s := range scopes {
		if s == "openid" {
			hasOpenID = true
			break
		}
	}

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

// OpenIDConfiguration represents the OpenID Connect Discovery metadata
type OpenIDConfiguration struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	UserInfoEndpoint                  string   `json:"userinfo_endpoint"`
	JWKSURI                           string   `json:"jwks_uri"`
	ScopesSupported                   []string `json:"scopes_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	ResponseModesSupported            []string `json:"response_modes_supported,omitempty"`
	GrantTypesSupported               []string `json:"grant_types_supported,omitempty"`
	SubjectTypesSupported             []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported  []string `json:"id_token_signing_alg_values_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	ClaimsSupported                   []string `json:"claims_supported,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	IntrospectionEndpoint             string   `json:"introspection_endpoint,omitempty"`
	RevocationEndpoint                string   `json:"revocation_endpoint,omitempty"`
}

// OAuthAuthorizationServerMetadata represents OAuth 2.0 Authorization Server Metadata
type OAuthAuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	JWKSURI                           string   `json:"jwks_uri,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	ResponseModesSupported            []string `json:"response_modes_supported,omitempty"`
	GrantTypesSupported               []string `json:"grant_types_supported,omitempty"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	IntrospectionEndpoint             string   `json:"introspection_endpoint,omitempty"`
	RevocationEndpoint                string   `json:"revocation_endpoint,omitempty"`
}

// OAuthProtectedResourceMetadata represents OAuth 2.0 protected resource metadata as defined in RFC 9728
type OAuthProtectedResourceMetadata struct {
	Resource                              string   `json:"resource"`
	ScopesSupported                       []string `json:"scopes_supported,omitempty"`
	AuthorizationServers                  []string `json:"authorization_servers,omitempty"`
	JWKSURI                               string   `json:"jwks_uri,omitempty"`
	BearerMethodsSupported                []string `json:"bearer_methods_supported,omitempty"`
	ResourceName                          string   `json:"resource_name,omitempty"`
	ResourceDocumentation                 string   `json:"resource_documentation,omitempty"`
	TLSClientCertificateBoundAccessTokens bool     `json:"tls_client_certificate_bound_access_tokens"`
}

// GetOpenIDConfiguration returns OpenID Connect Discovery metadata
func (h *Handlers) GetOpenIDConfiguration(c *gin.Context) {
	baseURL := getBaseURL(c)

	config := OpenIDConfiguration{
		Issuer:                            baseURL,
		AuthorizationEndpoint:             fmt.Sprintf("%s/oauth2/authorize", baseURL),
		TokenEndpoint:                     fmt.Sprintf("%s/oauth2/token", baseURL),
		UserInfoEndpoint:                  fmt.Sprintf("%s/oauth2/userinfo", baseURL),
		JWKSURI:                           fmt.Sprintf("%s/.well-known/jwks.json", baseURL),
		ScopesSupported:                   []string{"openid", "profile", "email"},
		ResponseTypesSupported:            []string{"code"},
		SubjectTypesSupported:             []string{"public"},
		IDTokenSigningAlgValuesSupported:  []string{"HS256"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post", "client_secret_basic"},
		ClaimsSupported: []string{
			"sub", "iss", "aud", "exp", "iat", "email", "email_verified",
			"name", "given_name", "family_name", "picture", "locale",
		},
		CodeChallengeMethodsSupported: []string{"S256"},
		GrantTypesSupported:           []string{"authorization_code", "refresh_token", "client_credentials"},
		RevocationEndpoint:            fmt.Sprintf("%s/oauth2/revoke", baseURL),
		IntrospectionEndpoint:         fmt.Sprintf("%s/oauth2/introspect", baseURL),
	}

	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, config)
}

// GetOAuthAuthorizationServerMetadata returns OAuth 2.0 Authorization Server metadata
func (h *Handlers) GetOAuthAuthorizationServerMetadata(c *gin.Context) {
	baseURL := getBaseURL(c)

	metadata := OAuthAuthorizationServerMetadata{
		Issuer:                            baseURL,
		AuthorizationEndpoint:             fmt.Sprintf("%s/oauth2/authorize", baseURL),
		TokenEndpoint:                     fmt.Sprintf("%s/oauth2/token", baseURL),
		JWKSURI:                           fmt.Sprintf("%s/.well-known/jwks.json", baseURL),
		ScopesSupported:                   []string{"openid", "profile", "email"},
		ResponseTypesSupported:            []string{"code"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token", "client_credentials"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post", "client_secret_basic"},
		RevocationEndpoint:                fmt.Sprintf("%s/oauth2/revoke", baseURL),
		IntrospectionEndpoint:             fmt.Sprintf("%s/oauth2/introspect", baseURL),
	}

	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, metadata)
}

// GetOAuthProtectedResourceMetadata returns OAuth 2.0 protected resource metadata as per RFC 9728
func (h *Handlers) GetOAuthProtectedResourceMetadata(c *gin.Context) {
	baseURL := getBaseURL(c)

	metadata := OAuthProtectedResourceMetadata{
		Resource:                              baseURL,
		ScopesSupported:                       []string{"openid", "profile", "email"},
		AuthorizationServers:                  []string{baseURL},
		JWKSURI:                               fmt.Sprintf("%s/.well-known/jwks.json", baseURL),
		BearerMethodsSupported:                []string{"header"},
		ResourceName:                          "TMI (Threat Modeling Improved) API",
		ResourceDocumentation:                 "https://github.com/ericfitz/tmi",
		TLSClientCertificateBoundAccessTokens: false,
	}

	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, metadata)
}

// JWKSResponse represents a JSON Web Key Set response
type JWKSResponse struct {
	Keys []JWK `json:"keys"`
}

// JWK represents a JSON Web Key
type JWK struct {
	KeyType   string   `json:"kty"`
	Use       string   `json:"use,omitempty"`
	KeyOps    []string `json:"key_ops,omitempty"`
	KeyID     string   `json:"kid,omitempty"`
	Algorithm string   `json:"alg,omitempty"`
	// RSA parameters
	N string `json:"n,omitempty"` // RSA modulus
	E string `json:"e,omitempty"` // RSA exponent
	// ECDSA parameters
	Curve string `json:"crv,omitempty"` // Elliptic curve
	X     string `json:"x,omitempty"`   // X coordinate
	Y     string `json:"y,omitempty"`   // Y coordinate
}

// createJWKFromPublicKey creates a JWK from a public key
func (h *Handlers) createJWKFromPublicKey(publicKey interface{}, signingMethod string) (*JWK, error) {
	jwk := &JWK{
		Use:       "sig",
		KeyOps:    []string{"verify"},
		KeyID:     h.service.config.JWT.KeyID,
		Algorithm: signingMethod,
	}

	switch key := publicKey.(type) {
	case *rsa.PublicKey:
		jwk.KeyType = "RSA"
		// Encode RSA modulus and exponent in base64url format
		jwk.N = base64URLEncode(key.N.Bytes())
		jwk.E = base64URLEncode(intToBytes(key.E))

	case *ecdsa.PublicKey:
		jwk.KeyType = "EC"
		// Determine the curve name
		switch key.Curve.Params().Name {
		case "P-256":
			jwk.Curve = "P-256"
		case "P-384":
			jwk.Curve = "P-384"
		case "P-521":
			jwk.Curve = "P-521"
		default:
			return nil, fmt.Errorf("unsupported ECDSA curve: %s", key.Curve.Params().Name)
		}

		// Get coordinate byte length for the curve
		byteLen := (key.Curve.Params().BitSize + 7) / 8

		// Encode X and Y coordinates
		jwk.X = base64URLEncode(key.X.FillBytes(make([]byte, byteLen)))
		jwk.Y = base64URLEncode(key.Y.FillBytes(make([]byte, byteLen)))

	default:
		return nil, fmt.Errorf("unsupported public key type: %T", publicKey)
	}

	return jwk, nil
}

// Helper functions for JWK creation
func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func intToBytes(i int) []byte {
	// Convert int to big-endian bytes
	if i == 0 {
		return []byte{0}
	}

	var bytes []byte
	for i > 0 {
		bytes = append([]byte{byte(i)}, bytes...)
		i >>= 8
	}
	return bytes
}

// GetJWKS returns the JSON Web Key Set for JWT signature verification
func (h *Handlers) GetJWKS(c *gin.Context) {
	jwks := JWKSResponse{
		Keys: []JWK{},
	}

	// Get public key from the key manager
	publicKey := h.service.keyManager.GetPublicKey()
	if publicKey != nil {
		jwk, err := h.createJWKFromPublicKey(publicKey, h.service.keyManager.GetSigningMethod())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to create JWK",
			})
			return
		}
		jwks.Keys = append(jwks.Keys, *jwk)
	}

	// Cache the response for 1 hour since keys don't change frequently
	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, jwks)
}

// TokenIntrospectionResponse represents the response from token introspection
type TokenIntrospectionResponse struct {
	Active    bool   `json:"active"`
	Sub       string `json:"sub,omitempty"`
	Email     string `json:"email,omitempty"`
	Name      string `json:"name,omitempty"`
	Iat       int64  `json:"iat,omitempty"`
	Exp       int64  `json:"exp,omitempty"`
	Aud       string `json:"aud,omitempty"`
	Iss       string `json:"iss,omitempty"`
	TokenType string `json:"token_type,omitempty"`
	Scope     string `json:"scope,omitempty"`
}

// IntrospectToken handles token introspection requests per RFC 7662
func (h *Handlers) IntrospectToken(c *gin.Context) {
	var req struct {
		Token string `json:"token" form:"token" binding:"required"`
	}

	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request: token parameter is required",
		})
		return
	}

	// Parse and validate the JWT token using centralized verification
	claims := jwt.MapClaims{}
	token, err := h.service.GetKeyManager().VerifyToken(req.Token, claims)

	// If token parsing failed or token is invalid, return inactive
	if err != nil || !token.Valid {
		c.JSON(http.StatusOK, TokenIntrospectionResponse{
			Active: false,
		})
		return
	}

	// Extract claims from the token
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusOK, TokenIntrospectionResponse{
			Active: false,
		})
		return
	}

	// Check if token is blacklisted (if blacklist service is available)
	if h.service.dbManager != nil && h.service.dbManager.Redis() != nil {
		blacklist := NewTokenBlacklist(h.service.dbManager.Redis().GetClient(), h.service.GetKeyManager())
		isBlacklisted, err := blacklist.IsTokenBlacklisted(c.Request.Context(), req.Token)
		if err == nil && isBlacklisted {
			c.JSON(http.StatusOK, TokenIntrospectionResponse{
				Active: false,
			})
			return
		}
	}

	// Extract standard claims
	baseURL := getBaseURL(c)
	response := TokenIntrospectionResponse{
		Active:    true,
		TokenType: "Bearer",
		Iss:       baseURL,
		Scope:     "openid profile email",
	}

	// Extract subject (user identifier)
	if sub, ok := claims["sub"].(string); ok {
		response.Sub = sub
	}

	// Extract email
	if email, ok := claims["email"].(string); ok {
		response.Email = email
	}

	// Extract name
	if name, ok := claims["name"].(string); ok {
		response.Name = name
	}

	// Extract issued at time
	if iat, ok := claims["iat"].(float64); ok {
		response.Iat = int64(iat)
	}

	// Extract expiration time
	if exp, ok := claims["exp"].(float64); ok {
		response.Exp = int64(exp)
	}

	// Extract audience
	if aud, ok := claims["aud"].(string); ok {
		response.Aud = aud
	}

	c.JSON(http.StatusOK, response)
}

// GetSAMLMetadata returns SAML service provider metadata
func (h *Handlers) GetSAMLMetadata(c *gin.Context, providerID string) {
	logger := slogging.Get()

	// Check if SAML is enabled
	if !h.config.SAML.Enabled {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "SAML authentication is not enabled",
		})
		return
	}

	// Get SAML manager
	samlManager := h.service.GetSAMLManager()
	if samlManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "SAML manager not initialized",
		})
		return
	}

	// Get provider
	provider, err := samlManager.GetProvider(providerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("SAML provider not found: %v", err),
		})
		return
	}

	// Generate metadata
	metadata, err := provider.GenerateMetadata()
	if err != nil {
		logger.Error("Failed to generate SAML metadata: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to generate metadata",
		})
		return
	}

	// Return metadata as XML
	c.Header("Content-Type", "application/samlmetadata+xml")
	c.Data(http.StatusOK, "application/samlmetadata+xml", []byte(metadata))
}

// InitiateSAMLLogin starts SAML authentication flow
func (h *Handlers) InitiateSAMLLogin(c *gin.Context, providerID string, clientCallback *string) {
	logger := slogging.Get()

	// Check if SAML is enabled
	if !h.config.SAML.Enabled {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "SAML authentication is not enabled",
		})
		return
	}

	// Get SAML manager
	samlManager := h.service.GetSAMLManager()
	if samlManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "SAML manager not initialized",
		})
		return
	}

	// Get provider
	provider, err := samlManager.GetProvider(providerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("SAML provider not found: %v", err),
		})
		return
	}

	// Initiate SAML authentication
	authURL, relayState, err := provider.InitiateLogin(clientCallback)
	if err != nil {
		logger.Error("Failed to initiate SAML login: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to initiate SAML authentication",
		})
		return
	}

	// Store state for CSRF protection
	if err := h.service.stateStore.StoreState(c.Request.Context(), relayState, providerID, 10*time.Minute); err != nil {
		logger.Error("Failed to store SAML relay state: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to initiate SAML authentication",
		})
		return
	}

	// Store client callback URL if provided
	if clientCallback != nil && *clientCallback != "" {
		if err := h.service.stateStore.StoreCallbackURL(c.Request.Context(), relayState, *clientCallback, 10*time.Minute); err != nil {
			logger.Error("Failed to store SAML callback URL: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to initiate SAML authentication",
			})
			return
		}
		logger.Info("Stored SAML callback URL for relay state: %s -> %s", relayState, *clientCallback)
	}

	// Redirect to IdP
	c.Redirect(http.StatusFound, authURL)
}

// redirectWithError attempts to redirect to client callback URL with error, or returns JSON error if no callback
// For SAML: uses relayState to retrieve callback URL from state store
func (h *Handlers) redirectWithError(c *gin.Context, ctx context.Context, relayState string, statusCode int, errorMsg string) {
	logger := slogging.Get()

	// Try to get callback URL - even if state validation failed, we might have stored it
	callbackURL, _ := h.service.stateStore.GetCallbackURL(ctx, relayState)

	if callbackURL != "" {
		// Redirect to client with error in fragment
		redirectURL, err := url.Parse(callbackURL)
		if err != nil {
			logger.Error("Invalid callback URL during error redirect: %v", err)
			c.JSON(statusCode, gin.H{
				"error": errorMsg,
			})
			return
		}

		// Add error to fragment using OAuth 2.0 error format
		fragment := fmt.Sprintf("error=saml_error&error_description=%s", url.QueryEscape(errorMsg))
		redirectURL.Fragment = fragment

		c.Redirect(http.StatusFound, redirectURL.String())
		return
	}

	// No callback URL, return JSON error
	c.JSON(statusCode, gin.H{
		"error": errorMsg,
	})
}

// redirectWithErrorOAuth redirects to client callback URL with error for OAuth flows
func (h *Handlers) redirectWithErrorOAuth(c *gin.Context, callbackURL string, statusCode int, errorMsg string) {
	logger := slogging.Get()

	if callbackURL == "" {
		// No callback URL, return JSON error
		c.JSON(statusCode, gin.H{
			"error": errorMsg,
		})
		return
	}

	// Redirect to client with error in fragment
	redirectURL, err := url.Parse(callbackURL)
	if err != nil {
		logger.Error("Invalid callback URL during error redirect: %v", err)
		c.JSON(statusCode, gin.H{
			"error": errorMsg,
		})
		return
	}

	// Add error to fragment using OAuth 2.0 error format
	fragment := fmt.Sprintf("error=oauth_error&error_description=%s", url.QueryEscape(errorMsg))
	redirectURL.Fragment = fragment

	c.Redirect(http.StatusFound, redirectURL.String())
}

// ProcessSAMLResponse handles SAML assertion consumer service
func (h *Handlers) ProcessSAMLResponse(c *gin.Context, providerID string, samlResponse string, relayState string) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Check if SAML is enabled
	if !h.config.SAML.Enabled {
		h.redirectWithError(c, ctx, relayState, http.StatusNotFound, "SAML authentication is not enabled")
		return
	}

	// Get SAML manager
	samlManager := h.service.GetSAMLManager()
	if samlManager == nil {
		h.redirectWithError(c, ctx, relayState, http.StatusInternalServerError, "SAML manager not initialized")
		return
	}

	// Verify state for CSRF protection
	if relayState != "" {
		storedProviderID, err := h.service.stateStore.ValidateState(ctx, relayState)
		if err != nil {
			logger.Error("Invalid SAML relay state: %v", err)
			h.redirectWithError(c, ctx, relayState, http.StatusBadRequest, "Invalid or expired state")
			return
		}
		// Use the provider ID from the state if not specified
		if providerID == "" || providerID == "default" {
			providerID = storedProviderID
		}
	}

	// Process SAML response
	_, tokenPair, err := samlManager.ProcessSAMLResponse(ctx, providerID, samlResponse, relayState)
	if err != nil {
		logger.Error("Failed to process SAML response: %v", err)
		h.redirectWithError(c, ctx, relayState, http.StatusUnauthorized, fmt.Sprintf("Authentication failed: %v", err))
		return
	}

	// Check if there's a client callback URL
	callbackURL, _ := h.service.stateStore.GetCallbackURL(ctx, relayState)
	if callbackURL != "" {
		// Redirect to client with tokens in fragment (implicit flow style)
		redirectURL, err := url.Parse(callbackURL)
		if err != nil {
			logger.Error("Invalid callback URL: %v", err)
			h.redirectWithError(c, ctx, relayState, http.StatusInternalServerError, "Invalid callback URL")
			return
		}

		// Add tokens to fragment
		fragment := fmt.Sprintf("access_token=%s&refresh_token=%s&token_type=%s&expires_in=%d",
			tokenPair.AccessToken,
			tokenPair.RefreshToken,
			tokenPair.TokenType,
			tokenPair.ExpiresIn,
		)
		redirectURL.Fragment = fragment

		c.Redirect(http.StatusFound, redirectURL.String())
		return
	}

	// Return tokens as JSON
	c.JSON(http.StatusOK, tokenPair)
}

// ProcessSAMLLogout handles SAML single logout
func (h *Handlers) ProcessSAMLLogout(c *gin.Context, providerID string, samlRequest string) {
	logger := slogging.Get()

	// Check if SAML is enabled
	if !h.config.SAML.Enabled {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "SAML authentication is not enabled",
		})
		return
	}

	// Get SAML manager
	samlManager := h.service.GetSAMLManager()
	if samlManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "SAML manager not initialized",
		})
		return
	}

	// Get provider
	provider, err := samlManager.GetProvider(providerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("SAML provider not found: %v", err),
		})
		return
	}

	// Process and validate logout request (includes signature verification)
	logoutReq, err := provider.ProcessLogoutRequest(samlRequest)
	if err != nil {
		logger.Error("Failed to process SAML logout: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid logout request",
		})
		return
	}

	// Invalidate user sessions based on the NameID from the logout request
	ctx := c.Request.Context()
	if logoutReq.NameID != nil {
		nameID := logoutReq.NameID.Value
		logger.Info("Processing SAML logout for NameID: %s", nameID)

		// Try to find the user by email (assuming NameID is email)
		user, err := h.service.GetUserByEmail(ctx, nameID)
		if err == nil {
			// Invalidate all sessions for this user using InternalUUID
			if err := h.service.InvalidateUserSessions(ctx, user.InternalUUID); err != nil {
				logger.Warn("Failed to invalidate sessions during SAML logout: %v", err)
				// Log but don't fail the logout
			}
		} else {
			logger.Warn("User not found for SAML logout NameID: %s", nameID)
		}
	}

	// Create logout response
	logoutResponse, err := provider.MakeLogoutResponse(logoutReq.ID, "urn:oasis:names:tc:SAML:2.0:status:Success")
	if err != nil {
		logger.Error("Failed to create SAML logout response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create logout response",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":         "Logout successful",
		"logout_response": logoutResponse,
	})
}
