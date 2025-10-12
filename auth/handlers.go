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

// Handlers provides HTTP handlers for authentication
type Handlers struct {
	service *Service
	config  Config
}

// NewHandlers creates new authentication handlers
func NewHandlers(service *Service, config Config) *Handlers {
	return &Handlers{
		service: service,
		config:  config,
	}
}

// Service returns the auth service (getter for unexported field)
func (h *Handlers) Service() *Service {
	return h.service
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

// GetProviders returns the available OAuth providers
func (h *Handlers) GetProviders(c *gin.Context) {
	providers := make([]ProviderInfo, 0, len(h.config.OAuth.Providers))

	for id, providerConfig := range h.config.OAuth.Providers {
		if !providerConfig.Enabled {
			continue
		}

		var name, icon string
		switch id {
		case "google":
			name = "Google"
			icon = "fa-brands fa-google"
		case "github":
			name = "GitHub"
			icon = "fa-brands fa-github"
		case "microsoft":
			name = "Microsoft"
			icon = "fa-brands fa-microsoft"
		case "test":
			name = "Test Provider"
			icon = "fa-solid fa-flask-vial"
		default:
			name = providerConfig.Name
			if name == "" {
				name = id
			}
			icon = providerConfig.Icon
			if icon == "" {
				icon = id
			}
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
		// In non-production builds, default to "test" provider for convenience
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
	if responseType == "" {
		responseType = "code" // Default to authorization code flow
	}
	if err := h.validateResponseType(responseType); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "unsupported_response_type",
			"error_description": err.Error(),
		})
		return
	}

	// Get optional client callback URL from query parameter
	clientCallback := c.Query("client_callback")

	// Get optional login_hint for test provider automation
	userHint := c.Query("login_hint")
	slogging.Get().WithContext(c).Debug("OAuth Authorize handler - extracted query parameters: provider=%s, client_callback=%s, login_hint=%s, scope=%s",
		providerID, clientCallback, userHint, scope)

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

	// Store provider ID, response type, and optional client callback URL/login_hint
	stateData := map[string]string{
		"provider":      providerID,
		"response_type": responseType,
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
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "OAuth service not initialized",
		})
		return
	}

	err = h.service.dbManager.Redis().Set(ctx, stateKey, string(stateJSON), 10*time.Minute)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to store OAuth state in Redis (key: %s, provider: %s): %v", stateKey, providerID, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to store state parameter",
		})
		return
	}

	// Handle implicit and hybrid flows for test provider
	slogging.Get().WithContext(c).Debug("OAuth flow decision: provider=%s, responseType=%s, clientCallback=%s", providerID, responseType, clientCallback)
	if responseType != "code" && providerID == "test" {
		slogging.Get().WithContext(c).Debug("Triggering implicit/hybrid flow for test provider")
		err := h.handleImplicitOrHybridFlow(c, provider, responseType, state, stateData)
		if err != nil {
			slogging.Get().WithContext(c).Error("Failed to handle OAuth implicit/hybrid flow (provider: %s, response_type: %s): %v", providerID, responseType, err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Failed to handle implicit/hybrid flow: %v", err),
			})
		}
		return
	}

	// For authorization code flow, handle client_callback if provided
	if providerID == "test" && clientCallback != "" {
		slogging.Get().WithContext(c).Debug("Authorization code flow with client_callback, redirecting directly to client")
		// Generate test authorization code with login_hint encoded if available
		authCode := fmt.Sprintf("test_auth_code_%d", time.Now().Unix())
		if userHint != "" {
			// Encode login_hint into the authorization code for later retrieval
			encodedHint := base64.URLEncoding.EncodeToString([]byte(userHint))
			authCode = fmt.Sprintf("test_auth_code_%d_hint_%s", time.Now().Unix(), encodedHint)
			slogging.Get().WithContext(c).Debug("Generated auth code with login_hint: %s", userHint)
		}

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
	ResponseType   string
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

	// Parse the state data (handle both old and new formats)
	var stateMap map[string]string
	result := &callbackStateData{}

	if err := json.Unmarshal([]byte(stateDataJSON), &stateMap); err != nil {
		// Handle legacy format where stateData is just the provider ID
		result.ProviderID = stateDataJSON
	} else {
		// Handle new format with structured data
		result.ProviderID = stateMap["provider"]
		result.ResponseType = stateMap["response_type"]
		if result.ResponseType == "" {
			result.ResponseType = "code" // Default for backward compatibility
		}
		result.ClientCallback = stateMap["client_callback"]
		result.UserHint = stateMap["login_hint"]

		slogging.Get().WithContext(c).Debug("Retrieved state data: provider=%s, response_type=%s, client_callback=%s, login_hint=%s",
			result.ProviderID, result.ResponseType, result.ClientCallback, result.UserHint)
	}

	return result, nil
}

// processOAuthCallback handles the core OAuth callback flow
func (h *Handlers) processOAuthCallback(c *gin.Context, code string, stateData *callbackStateData) error {
	ctx := c.Request.Context()

	// Get the provider
	provider, err := h.getProvider(stateData.ProviderID)
	if err != nil {
		if strings.Contains(err.Error(), "not available in production") {
			c.JSON(http.StatusNotFound, gin.H{"error": "Provider not available"})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return err
	}

	// Set login_hint context for test provider
	ctx = h.setUserHintContext(c, ctx, stateData)

	// Exchange code for tokens and get user info
	_, userInfo, claims, err := h.exchangeCodeAndGetUser(c, ctx, provider, code)
	if err != nil {
		return err
	}

	// Create or get user
	user, err := h.createOrGetUser(c, ctx, userInfo, claims)
	if err != nil {
		return err
	}

	// Link provider to user
	h.linkProviderToUser(ctx, user.ID, stateData.ProviderID, userInfo, claims)

	// Refetch user with provider ID for token generation
	userWithProviderID, err := h.service.GetUserWithProviderID(ctx, user.Email)
	if err != nil {
		// Fallback to original user if fetch fails
		slogging.Get().WithContext(c).Error("Failed to get user with provider ID: %v", err)
		userWithProviderID = user
	}

	// Generate and return tokens
	return h.generateAndReturnTokens(c, ctx, userWithProviderID, userInfo, stateData)
}

// setUserHintContext adds login_hint to context for test provider
func (h *Handlers) setUserHintContext(c *gin.Context, ctx context.Context, stateData *callbackStateData) context.Context {
	if stateData.UserHint != "" && stateData.ProviderID == "test" {
		slogging.Get().WithContext(c).Debug("Setting login_hint in context for test provider: %s", stateData.UserHint)
		return context.WithValue(ctx, userHintContextKey, stateData.UserHint)
	} else if stateData.ProviderID == "test" {
		slogging.Get().WithContext(c).Debug("No login_hint provided for test provider: provider=%s userHint=%s",
			stateData.ProviderID, stateData.UserHint)
	}
	return ctx
}

// exchangeCodeAndGetUser exchanges OAuth code for tokens and gets user info
func (h *Handlers) exchangeCodeAndGetUser(c *gin.Context, ctx context.Context, provider Provider, code string) (*TokenResponse, *UserInfo, *IDTokenClaims, error) {
	slogging.Get().WithContext(c).Debug("About to call ExchangeCode: code=%s has_login_hint_in_context=%v",
		code, ctx.Value(userHintContextKey) != nil)

	tokenResponse, err := provider.ExchangeCode(ctx, code)
	if err != nil {
		if strings.Contains(err.Error(), "invalid authorization code") ||
			strings.Contains(err.Error(), "authorization code is required") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			slogging.Get().WithContext(c).Error("Failed to exchange OAuth authorization code for tokens (code prefix: %.10s...): %v", code, err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Failed to exchange code for tokens: %v", err),
			})
		}
		return nil, nil, nil, err
	}

	// Validate ID token if present
	var claims *IDTokenClaims
	if tokenResponse.IDToken != "" {
		claims, err = provider.ValidateIDToken(ctx, tokenResponse.IDToken)
		if err != nil {
			logger := slogging.Get().WithContext(c)
			logger.Error("Failed to validate ID token: %v", err)
		}
	}

	// Get user info
	slogging.Get().WithContext(c).Debug("About to call GetUserInfo: access_token=%s", tokenResponse.AccessToken)
	userInfo, err := provider.GetUserInfo(ctx, tokenResponse.AccessToken)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to get user info from OAuth provider using access token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get user info: %v", err),
		})
		return nil, nil, nil, err
	}

	slogging.Get().WithContext(c).Debug("GetUserInfo returned: user_id=%s email=%s name=%s",
		userInfo.ID, userInfo.Email, userInfo.Name)

	return tokenResponse, userInfo, claims, nil
}

// createOrGetUser creates a new user or gets existing user
func (h *Handlers) createOrGetUser(c *gin.Context, ctx context.Context, userInfo *UserInfo, claims *IDTokenClaims) (User, error) {
	email := userInfo.Email
	if email == "" && claims != nil {
		email = claims.Email
	}

	if email == "" {
		slogging.Get().WithContext(c).Error("OAuth provider returned empty email for user (name: %s, id: %s, userInfo.Email: %s)", userInfo.Name, userInfo.ID, userInfo.Email)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user email"})
		return User{}, fmt.Errorf("no email found")
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
		user = User{
			Email:         email,
			Name:          name,
			EmailVerified: userInfo.EmailVerified,
			GivenName:     userInfo.GivenName,
			FamilyName:    userInfo.FamilyName,
			Picture:       userInfo.Picture,
			Locale:        userInfo.Locale,
			CreatedAt:     time.Now(),
			ModifiedAt:    time.Now(),
			LastLogin:     time.Now(),
		}

		// Set default locale if not provided
		if user.Locale == "" {
			user.Locale = "en-US"
		}

		user, err = h.service.CreateUser(ctx, user)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Failed to create user: %v", err),
			})
			return User{}, err
		}
	}

	return user, nil
}

// linkProviderToUser links the OAuth provider to the user
func (h *Handlers) linkProviderToUser(ctx context.Context, userID, providerID string, userInfo *UserInfo, claims *IDTokenClaims) {
	providerUserID := userInfo.ID
	if providerUserID == "" && claims != nil {
		providerUserID = claims.Subject
	}

	if providerUserID != "" {
		err := h.service.LinkUserProvider(ctx, userID, providerID, providerUserID, userInfo.Email)
		if err != nil {
			logger := slogging.Get()
			logger.Error("Failed to link provider: %v (provider: %s, user_id: %s)", err, providerID, userID)
		}
	}
}

// generateAndReturnTokens generates JWT tokens and returns them
func (h *Handlers) generateAndReturnTokens(c *gin.Context, ctx context.Context, user User, userInfo *UserInfo, stateData *callbackStateData) error {
	tokenPair, err := h.service.GenerateTokensWithUserInfo(ctx, user, userInfo)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to generate JWT tokens for user %s: %v", user.Email, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to generate tokens: %v", err),
		})
		return err
	}

	// If client callback URL is provided, redirect there with tokens
	if stateData.ClientCallback != "" {
		redirectURL, err := buildClientRedirectURL(stateData.ClientCallback, tokenPair, c.Query("state"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Failed to build redirect URL: %v", err),
			})
			return err
		}
		c.Redirect(http.StatusFound, redirectURL)
		return nil
	}

	// Fallback: Return tokens as JSON (legacy behavior)
	c.JSON(http.StatusOK, tokenPair)
	return nil
}

// Exchange handles authorization code exchange for any provider
func (h *Handlers) Exchange(c *gin.Context) {
	providerID := c.Query("idp")
	if providerID == "" {
		// In non-production builds, default to "test" provider for convenience
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

	var req struct {
		Code        string `json:"code" binding:"required"`
		State       string `json:"state"`
		RedirectURI string `json:"redirect_uri" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request: missing required fields",
		})
		return
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

	// Optional: Verify state parameter if using state validation
	if req.State != "" {
		stateKey := fmt.Sprintf("oauth_state:%s", req.State)
		storedProvider, err := h.service.dbManager.Redis().Get(ctx, stateKey)
		if err != nil || storedProvider != providerID {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Invalid state parameter",
			})
			return
		}
		// Clean up state
		_ = h.service.dbManager.Redis().Del(ctx, stateKey)
	}

	// Exchange authorization code for tokens
	// Note: login_hint is now encoded directly in the authorization code for test provider
	tokenResponse, err := provider.ExchangeCode(ctx, req.Code)
	if err != nil {
		// Check if it's an invalid code error (client error) vs server error
		if strings.Contains(err.Error(), "invalid authorization code") ||
			strings.Contains(err.Error(), "authorization code is required") {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
		} else {
			slogging.Get().WithContext(c).Error("Failed to exchange authorization code for tokens in callback (provider: %s, code prefix: %.10s...): %v", providerID, req.Code, err)
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

	// Extract email from userInfo or claims
	email := userInfo.Email
	if email == "" && claims != nil {
		email = claims.Email
	}
	if email == "" {
		slogging.Get().WithContext(c).Error("OAuth provider returned empty email in callback (userInfo.Email: %s, claims present: %v)", userInfo.Email, claims != nil)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get user email from provider",
		})
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

	// Get or create user
	user, err := h.service.GetUserByEmail(ctx, email)
	if err != nil {
		// Create new user
		user = User{
			Email:      email,
			Name:       name,
			CreatedAt:  time.Now(),
			ModifiedAt: time.Now(),
			LastLogin:  time.Now(),
		}

		user, err = h.service.CreateUser(ctx, user)
		if err != nil {
			slogging.Get().WithContext(c).Error("Failed to create new user in database during callback (email: %s, name: %s): %v", user.Email, user.Name, err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Failed to create user: %v", err),
			})
			return
		}
	} else {
		// Update last login
		user.LastLogin = time.Now()
		err = h.service.UpdateUser(ctx, user)
		if err != nil {
			// Log error but continue
			logger := slogging.Get().WithContext(c)
			logger.Error("Failed to update user last login: %v (user_id: %s)", err, user.ID)
		}
	}

	// Link provider to user
	providerUserID := userInfo.ID
	if providerUserID == "" && claims != nil {
		providerUserID = claims.Subject
	}
	if providerUserID != "" {
		err = h.service.LinkUserProvider(ctx, user.ID, providerID, providerUserID, email)
		if err != nil {
			// Log error but continue
			logger := slogging.Get().WithContext(c)
			logger.Error("Failed to link user provider: %v (user_id: %s, provider: %s)", err, user.ID, providerID)
		}
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
		RefreshToken string `json:"refresh_token" form:"refresh_token"`
		RedirectURI  string `json:"redirect_uri" form:"redirect_uri"`
		ClientID     string `json:"client_id" form:"client_id"`
	}

	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request",
		})
		return
	}

	switch req.GrantType {
	case "authorization_code":
		// Handle authorization code grant
		if req.Code == "" || req.RedirectURI == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Missing code or redirect_uri parameter",
			})
			return
		}

		// This is handled by the Callback handler
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Use the /oauth2/callback endpoint for authorization code grant",
		})

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

// Logout revokes a refresh token or blacklists a JWT token
func (h *Handlers) Logout(c *gin.Context) {
	// Try to get JWT token from Authorization header first
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		// Handle JWT-based logout (new method)
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			tokenStr := parts[1]

			// Validate token format and signature using centralized JWT verification
			claims := jwt.MapClaims{}
			token, err := h.service.GetKeyManager().VerifyToken(tokenStr, claims)
			if err == nil && token.Valid {
				// Try to blacklist the JWT token if blacklist service is available
				// We'll use the database manager to access Redis for token blacklisting
				if h.service != nil && h.service.dbManager != nil && h.service.dbManager.Redis() != nil {
					blacklist := NewTokenBlacklist(h.service.dbManager.Redis().GetClient(), h.service.GetKeyManager())
					if err := blacklist.BlacklistToken(c.Request.Context(), tokenStr); err != nil {
						slogging.Get().WithContext(c).Error("Failed to blacklist JWT token during logout (token prefix: %.10s...): %v", tokenStr, err)
						c.JSON(http.StatusInternalServerError, gin.H{
							"error": fmt.Sprintf("Failed to blacklist token: %v", err),
						})
						return
					}
				}

				c.JSON(http.StatusOK, gin.H{
					"message": "Logged out successfully",
				})
				return
			}
		}
	}

	// Fall back to refresh token-based logout (original method)
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}

	if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request: missing refresh_token in body or Authorization header",
		})
		return
	}

	// Revoke the refresh token
	err := h.service.RevokeToken(c.Request.Context(), req.RefreshToken)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to revoke refresh token during logout (token prefix: %.10s...): %v", req.RefreshToken, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to revoke token: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Logged out successfully",
	})
}

// Me returns the current user
func (h *Handlers) Me(c *gin.Context) {
	// Get the full user object from Gin context (set by JWT middleware)
	userInterface, exists := c.Get(string(UserContextKey))
	if exists {
		if user, ok := userInterface.(User); ok {
			c.JSON(http.StatusOK, user)
			return
		}
	}

	// User not found in context - not authenticated
	c.Header("WWW-Authenticate", "Bearer")
	c.JSON(http.StatusUnauthorized, gin.H{
		"error": "User not authenticated",
	})
}

// Helper functions

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

	// Build query parameters with tokens
	params := url.Values{}
	params.Set("access_token", tokenPair.AccessToken)
	params.Set("refresh_token", tokenPair.RefreshToken)
	params.Set("expires_in", fmt.Sprintf("%d", tokenPair.ExpiresIn))
	params.Set("token_type", tokenPair.TokenType)

	// Include the original state parameter
	if state != "" {
		params.Set("state", state)
	}

	// Preserve any existing query parameters from client callback URL
	existingParams := parsedURL.Query()
	for key, values := range existingParams {
		for _, value := range values {
			params.Add(key, value)
		}
	}

	// Set the combined query parameters
	parsedURL.RawQuery = params.Encode()

	return parsedURL.String(), nil
}

// exchangeCodeForTokens exchanges an authorization code for tokens
// TODO: Currently unused - reserved for future OAuth Authorization Code flow implementation
/*
func exchangeCodeForTokens(ctx context.Context, provider OAuthProviderConfig, code, redirectURI string) (map[string]string, error) {
	// Prepare the request
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("client_id", provider.ClientID)
	data.Set("client_secret", provider.ClientSecret)

	// Send the request
	req, err := http.NewRequestWithContext(ctx, "POST", provider.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp.Body)

	// Parse the response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to exchange code: %s", body)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}
*/

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
		ResponseTypesSupported:            []string{"code", "token", "id_token", "code token", "code id_token", "code id_token token"},
		SubjectTypesSupported:             []string{"public"},
		IDTokenSigningAlgValuesSupported:  []string{"HS256"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post", "client_secret_basic"},
		ClaimsSupported: []string{
			"sub", "iss", "aud", "exp", "iat", "email", "email_verified",
			"name", "given_name", "family_name", "picture", "locale",
		},
		GrantTypesSupported:   []string{"authorization_code", "refresh_token"},
		RevocationEndpoint:    fmt.Sprintf("%s/oauth2/revoke", baseURL),
		IntrospectionEndpoint: fmt.Sprintf("%s/oauth2/introspect", baseURL),
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
		ResponseTypesSupported:            []string{"code", "token", "id_token", "code token", "code id_token", "code id_token token"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
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

// validateResponseType validates the response_type parameter according to OAuth 2.0/OIDC specification
func (h *Handlers) validateResponseType(responseType string) error {
	supportedResponseTypes := map[string]bool{
		"code":                true, // Authorization Code Flow
		"token":               true, // Implicit Flow (Access Token only)
		"id_token":            true, // Implicit Flow (ID Token only)
		"code token":          true, // Hybrid Flow
		"code id_token":       true, // Hybrid Flow
		"code id_token token": true, // Hybrid Flow
	}

	if !supportedResponseTypes[responseType] {
		return fmt.Errorf("unsupported response_type: %s. Supported types are: code, token, id_token, and hybrid combinations", responseType)
	}

	return nil
}

// handleImplicitOrHybridFlow handles implicit and hybrid flows for test provider
func (h *Handlers) handleImplicitOrHybridFlow(c *gin.Context, provider Provider, responseType, state string, stateData map[string]string) error {
	ctx := c.Request.Context()

	// Set login_hint context for test provider if provided
	if userHint, exists := stateData["login_hint"]; exists && userHint != "" {
		ctx = context.WithValue(ctx, userHintContextKey, userHint)
	}

	// For implicit flow with test provider, directly get user info without code exchange
	// We'll generate a mock access token for the test provider
	mockTokenResponse := &TokenResponse{
		AccessToken: "test_implicit_access_token",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	}

	// Get user info using the mock token
	userInfo, err := provider.GetUserInfo(ctx, mockTokenResponse.AccessToken)
	if err != nil {
		return fmt.Errorf("failed to get user info: %v", err)
	}

	// Create or get user
	email := userInfo.Email
	if email == "" {
		return fmt.Errorf("no email found for user")
	}

	name := userInfo.Name
	if name == "" {
		name = email
	}

	user, err := h.service.GetUserByEmail(ctx, email)
	if err != nil {
		// Create a new user
		user = User{
			Email:      email,
			Name:       name,
			CreatedAt:  time.Now(),
			ModifiedAt: time.Now(),
			LastLogin:  time.Now(),
		}

		user, err = h.service.CreateUser(ctx, user)
		if err != nil {
			return fmt.Errorf("failed to create user: %v", err)
		}

		// Link provider to user after creation
		providerUserID := userInfo.ID
		if providerUserID != "" && stateData["provider"] != "" {
			err = h.service.LinkUserProvider(ctx, user.ID, stateData["provider"], providerUserID, email)
			if err != nil {
				// Log error but continue
				slogging.Get().Error("Failed to link provider in implicit flow: %v", err)
			}
		}
	}

	// Generate TMI JWT tokens (the provider ID will be used as subject in the JWT)
	tokenPair, err := h.service.GenerateTokensWithUserInfo(ctx, user, userInfo)
	if err != nil {
		return fmt.Errorf("failed to generate tokens: %v", err)
	}

	// For implicit and hybrid flows, return tokens and/or code in the redirect
	redirectURI := stateData["client_callback"]
	if redirectURI == "" {
		// If no client callback, return JSON (fallback)
		c.JSON(http.StatusOK, tokenPair)
		return nil
	}

	var authCode string
	// For hybrid flows containing "code", generate an authorization code
	if strings.Contains(responseType, "code") {
		// Generate a mock authorization code for test provider
		authCode = fmt.Sprintf("test_hybrid_code_%d", time.Now().UnixNano())

		// Store the code in Redis for later exchange (similar to regular auth code flow)
		codeKey := fmt.Sprintf("oauth_code:%s", authCode)
		codeData := map[string]string{
			"provider":   stateData["provider"],
			"email":      user.Email,
			"name":       user.Name,
			"user_id":    user.ID,
			"expires_at": fmt.Sprintf("%d", time.Now().Add(10*time.Minute).Unix()),
		}

		codeJSON, err := json.Marshal(codeData)
		if err == nil {
			_ = h.service.dbManager.Redis().Set(ctx, codeKey, string(codeJSON), 10*time.Minute)
		}
	}

	// Build the redirect URL for implicit/hybrid flow
	redirectURL, err := h.buildImplicitOrHybridFlowRedirect(redirectURI, tokenPair, responseType, state, authCode)
	if err != nil {
		return fmt.Errorf("failed to build redirect URL: %v", err)
	}

	c.Redirect(http.StatusFound, redirectURL)
	return nil
}

// buildImplicitOrHybridFlowRedirect builds the redirect URL for implicit/hybrid flows
func (h *Handlers) buildImplicitOrHybridFlowRedirect(redirectURI string, tokenPair TokenPair, responseType, state, authCode string) (string, error) {
	parsedURL, err := url.Parse(redirectURI)
	if err != nil {
		return "", fmt.Errorf("invalid redirect URI: %v", err)
	}

	// Handle query parameters for hybrid flows (authorization code)
	query := parsedURL.Query()
	if authCode != "" && strings.Contains(responseType, "code") {
		query.Set("code", authCode)
		if state != "" {
			query.Set("state", state)
		}
	}
	parsedURL.RawQuery = query.Encode()

	// Build fragment parameters for tokens (implicit/hybrid flows)
	fragment := url.Values{}

	if strings.Contains(responseType, "token") {
		fragment.Set("access_token", tokenPair.AccessToken)
		fragment.Set("token_type", tokenPair.TokenType)
		fragment.Set("expires_in", fmt.Sprintf("%d", tokenPair.ExpiresIn))
	}

	if strings.Contains(responseType, "id_token") {
		// For this implementation, we'll use the access token as a mock ID token
		// In a full implementation, you'd generate a proper ID token with different claims
		fragment.Set("id_token", tokenPair.AccessToken)
	}

	// For pure implicit flows (no code), include state in fragment
	if authCode == "" && state != "" {
		fragment.Set("state", state)
	}

	// Set the fragment if there are any fragment parameters
	if len(fragment) > 0 {
		parsedURL.Fragment = fragment.Encode()
	}

	return parsedURL.String(), nil
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
