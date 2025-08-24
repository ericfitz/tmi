package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Context key type for login_hint
type contextKey string

const userHintContextKey contextKey = "login_hint"

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

// RegisterRoutes registers the authentication routes
func (h *Handlers) RegisterRoutes(router *gin.Engine) {
	logger := logging.Get()
	logger.Info("[AUTH_MODULE] Starting route registration")

	auth := router.Group("/oauth2")
	{
		// Note: OAuth2 authorize and token endpoints are now handled by OpenAPI-generated routes
		// with query parameters instead of path parameters. These routes are registered by the
		// api.RegisterHandlers() call in the main server setup.

		logger.Info("[AUTH_MODULE] Registering route: GET /oauth2/providers")
		auth.GET("/providers", h.GetProviders)
		logger.Info("[AUTH_MODULE] Registering route: GET /oauth2/callback")
		auth.GET("/callback", h.Callback)
		logger.Info("[AUTH_MODULE] Registering route: POST /oauth2/refresh")
		auth.POST("/refresh", h.Refresh)
		logger.Info("[AUTH_MODULE] Registering route: POST /oauth2/revoke")
		auth.POST("/revoke", h.Logout)
		logger.Info("[AUTH_MODULE] Registering route: GET /oauth2/userinfo (with auth middleware)")
		auth.GET("/userinfo", h.AuthMiddleware().AuthRequired(), h.Me)
	}

	logger.Info("[AUTH_MODULE] Registering test provider routes")
	// Register test provider routes (only in dev/test builds)
	h.registerTestProviderRoutes(router)
	logger.Info("[AUTH_MODULE] Route registration completed")
}

// AuthMiddleware returns the authentication middleware
func (h *Handlers) AuthMiddleware() *Middleware {
	return NewMiddleware(h.service)
}

// ProviderInfo contains information about an OAuth provider
type ProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	AuthURL     string `json:"auth_url"`
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

		providers = append(providers, ProviderInfo{
			ID:          id,
			Name:        name,
			Icon:        icon,
			AuthURL:     authURL,
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
			logging.Get().WithContext(c).Debug("No idp parameter provided, defaulting to provider: %s", defaultProviderID)
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

	// Get optional client callback URL from query parameter
	clientCallback := c.Query("client_callback")

	// Get optional login_hint for test provider automation
	userHint := c.Query("login_hint")
	logging.Get().WithContext(c).Debug("OAuth Authorize handler - extracted query parameters: provider=%s, client_callback=%s, login_hint=%s, scope=%s",
		providerID, clientCallback, userHint, scope)

	// Get state parameter from client or generate one if not provided
	state := c.Query("state")
	if state == "" {
		// Generate a state parameter to prevent CSRF if client didn't provide one
		var err error
		state, err = generateRandomState()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to generate state parameter",
			})
			return
		}
	}

	// Store the state and client callback in Redis with a 10-minute expiration
	stateKey := fmt.Sprintf("oauth_state:%s", state)
	ctx := c.Request.Context()

	// Store both provider ID and client callback URL (if provided)
	stateData := map[string]string{
		"provider": providerID,
	}
	if clientCallback != "" {
		stateData["client_callback"] = clientCallback
	}
	if userHint != "" {
		stateData["login_hint"] = userHint
		logging.Get().WithContext(c).Debug("Storing login_hint in state: %s", userHint)
	}

	stateJSON, err := json.Marshal(stateData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to encode state data",
		})
		return
	}

	err = h.service.dbManager.Redis().Set(ctx, stateKey, string(stateJSON), 10*time.Minute)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to store state parameter",
		})
		return
	}

	// Get the authorization URL
	authURL := provider.GetAuthorizationURL(state)

	// Redirect to the authorization URL
	c.Redirect(http.StatusFound, authURL)
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

	// Parse the state data (handle both old and new formats)
	var stateMap map[string]string
	result := &callbackStateData{}

	if err := json.Unmarshal([]byte(stateDataJSON), &stateMap); err != nil {
		// Handle legacy format where stateData is just the provider ID
		result.ProviderID = stateDataJSON
	} else {
		// Handle new format with structured data
		result.ProviderID = stateMap["provider"]
		result.ClientCallback = stateMap["client_callback"]
		result.UserHint = stateMap["login_hint"]

		logging.Get().WithContext(c).Debug("Retrieved state data: provider=%s, client_callback=%s, login_hint=%s",
			result.ProviderID, result.ClientCallback, result.UserHint)
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

	// Generate and return tokens
	return h.generateAndReturnTokens(c, ctx, user, stateData)
}

// setUserHintContext adds login_hint to context for test provider
func (h *Handlers) setUserHintContext(c *gin.Context, ctx context.Context, stateData *callbackStateData) context.Context {
	if stateData.UserHint != "" && stateData.ProviderID == "test" {
		logging.Get().WithContext(c).Debug("Setting login_hint in context for test provider: %s", stateData.UserHint)
		return context.WithValue(ctx, userHintContextKey, stateData.UserHint)
	} else if stateData.ProviderID == "test" {
		logging.Get().WithContext(c).Debug("No login_hint provided for test provider: provider=%s userHint=%s",
			stateData.ProviderID, stateData.UserHint)
	}
	return ctx
}

// exchangeCodeAndGetUser exchanges OAuth code for tokens and gets user info
func (h *Handlers) exchangeCodeAndGetUser(c *gin.Context, ctx context.Context, provider Provider, code string) (*TokenResponse, *UserInfo, *IDTokenClaims, error) {
	logging.Get().WithContext(c).Debug("About to call ExchangeCode: code=%s has_login_hint_in_context=%v",
		code, ctx.Value(userHintContextKey) != nil)

	tokenResponse, err := provider.ExchangeCode(ctx, code)
	if err != nil {
		if strings.Contains(err.Error(), "invalid authorization code") ||
			strings.Contains(err.Error(), "authorization code is required") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
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
			fmt.Printf("Failed to validate ID token: %v\n", err)
		}
	}

	// Get user info
	logging.Get().WithContext(c).Debug("About to call GetUserInfo: access_token=%s", tokenResponse.AccessToken)
	userInfo, err := provider.GetUserInfo(ctx, tokenResponse.AccessToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get user info: %v", err),
		})
		return nil, nil, nil, err
	}

	logging.Get().WithContext(c).Debug("GetUserInfo returned: user_id=%s email=%s name=%s",
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
			fmt.Printf("Failed to link provider: %v\n", err)
		}
	}
}

// generateAndReturnTokens generates JWT tokens and returns them
func (h *Handlers) generateAndReturnTokens(c *gin.Context, ctx context.Context, user User, stateData *callbackStateData) error {
	tokenPair, err := h.service.GenerateTokens(ctx, user)
	if err != nil {
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
			logging.Get().WithContext(c).Debug("No idp parameter provided, defaulting to provider: %s", defaultProviderID)
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
	tokenResponse, err := provider.ExchangeCode(ctx, req.Code)
	if err != nil {
		// Check if it's an invalid code error (client error) vs server error
		if strings.Contains(err.Error(), "invalid authorization code") ||
			strings.Contains(err.Error(), "authorization code is required") {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
		} else {
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
			fmt.Printf("Failed to validate ID token: %v\n", err)
		}
	}

	// Get user info from provider
	userInfo, err := provider.GetUserInfo(ctx, tokenResponse.AccessToken)
	if err != nil {
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
			fmt.Printf("Failed to update user last login: %v\n", err)
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
			fmt.Printf("Failed to link user provider: %v\n", err)
		}
	}

	// Generate TMI JWT tokens
	tokenPair, err := h.service.GenerateTokens(ctx, user)
	if err != nil {
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

			// Validate token format
			if _, _, err := new(jwt.Parser).ParseUnverified(tokenStr, jwt.MapClaims{}); err == nil {
				// Try to blacklist the JWT token if blacklist service is available
				// We'll use the database manager to access Redis for token blacklisting
				if h.service != nil && h.service.dbManager != nil && h.service.dbManager.Redis() != nil {
					blacklist := NewTokenBlacklist(h.service.dbManager.Redis().GetClient())
					if err := blacklist.BlacklistToken(c.Request.Context(), tokenStr); err != nil {
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
	// First try to get the full user object from context (for auth middleware)
	user, err := GetUserFromContext(c.Request.Context())
	if err == nil {
		c.JSON(http.StatusOK, user)
		return
	}

	// If full user not available, try to get userName from JWT middleware context
	userNameInterface, exists := c.Get("userName")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "User not authenticated",
		})
		return
	}

	userName, ok := userNameInterface.(string)
	if !ok || userName == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid user context",
		})
		return
	}

	// Return a minimal user object with the available information from JWT
	c.JSON(http.StatusOK, gin.H{
		"email":         userName,
		"name":          userName, // We don't have the full name from JWT, so use email
		"id":            "",       // We don't have user ID from JWT
		"authenticated": true,
		"source":        "jwt",
	})
}

// Helper functions

// getBaseURL constructs the base URL for the current request
func getBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
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

// getUserInfo gets the user info from the provider
func getUserInfo(ctx context.Context, provider OAuthProviderConfig, accessToken string) (map[string]interface{}, error) {
	// Prepare the request
	req, err := http.NewRequestWithContext(ctx, "GET", provider.UserInfoURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
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
		return nil, fmt.Errorf("failed to get user info: %s", body)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
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

	logging.Get().Debug("OAuth scope validation: requested=%s, validated=%v", scope, validScopes)
	return nil
}
