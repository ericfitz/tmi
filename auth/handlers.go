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

	"github.com/gin-gonic/gin"
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

// RegisterRoutes registers the authentication routes
func (h *Handlers) RegisterRoutes(router *gin.Engine) {
	auth := router.Group("/auth")
	{
		auth.GET("/providers", h.GetProviders)
		auth.GET("/authorize/:provider", h.Authorize)
		auth.GET("/callback", h.Callback)
		auth.POST("/token", h.Token)
		auth.POST("/refresh", h.Refresh)
		auth.POST("/logout", h.Logout)
		auth.GET("/me", h.AuthMiddleware().AuthRequired(), h.Me)
	}
}

// AuthMiddleware returns the authentication middleware
func (h *Handlers) AuthMiddleware() *Middleware {
	return NewMiddleware(h.service)
}

// ProviderInfo contains information about an OAuth provider
type ProviderInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

// GetProviders returns the available OAuth providers
func (h *Handlers) GetProviders(c *gin.Context) {
	providers := make([]ProviderInfo, 0, len(h.config.OAuth.Providers))

	for id := range h.config.OAuth.Providers {
		var name, icon string
		switch id {
		case "google":
			name = "Google"
			icon = "google"
		case "github":
			name = "GitHub"
			icon = "github"
		case "microsoft":
			name = "Microsoft"
			icon = "microsoft"
		default:
			name = id
			icon = id
		}

		providers = append(providers, ProviderInfo{
			ID:   id,
			Name: name,
			Icon: icon,
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
	providerID := c.Param("provider")

	// Get the provider
	provider, err := h.getProvider(providerID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// Generate a state parameter to prevent CSRF
	state, err := generateRandomState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to generate state parameter",
		})
		return
	}

	// Store the state in Redis with a 10-minute expiration
	stateKey := fmt.Sprintf("oauth_state:%s", state)
	ctx := c.Request.Context()
	err = h.service.dbManager.Redis().Set(ctx, stateKey, providerID, 10*time.Minute)
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

// Callback handles the OAuth callback
func (h *Handlers) Callback(c *gin.Context) {
	// Get the authorization code and state
	code := c.Query("code")
	state := c.Query("state")

	if code == "" || state == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Missing code or state parameter",
		})
		return
	}

	// Verify the state parameter
	stateKey := fmt.Sprintf("oauth_state:%s", state)
	ctx := c.Request.Context()
	providerID, err := h.service.dbManager.Redis().Get(ctx, stateKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid state parameter",
		})
		return
	}

	// Delete the state from Redis
	_ = h.service.dbManager.Redis().Del(ctx, stateKey)

	// Get the provider
	provider, err := h.getProvider(providerID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// Exchange the authorization code for tokens
	tokenResponse, err := provider.ExchangeCode(ctx, code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to exchange code for tokens: %v", err),
		})
		return
	}

	// Validate the ID token if present
	var claims *IDTokenClaims
	if tokenResponse.IDToken != "" {
		claims, err = provider.ValidateIDToken(ctx, tokenResponse.IDToken)
		if err != nil {
			// Log the error but continue, as we can still get user info from the userinfo endpoint
			fmt.Printf("Failed to validate ID token: %v\n", err)
		}
	}

	// Get the user info from the provider
	userInfo, err := provider.GetUserInfo(ctx, tokenResponse.AccessToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get user info: %v", err),
		})
		return
	}

	// Get or create the user
	email := userInfo.Email
	if email == "" && claims != nil {
		email = claims.Email
	}

	if email == "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get user email",
		})
		return
	}

	name := userInfo.Name
	if name == "" && claims != nil {
		name = claims.Name
	}
	if name == "" {
		name = email
	}

	// Check if the user exists
	user, err := h.service.GetUserByEmail(ctx, email)
	if err != nil {
		// Create a new user
		user = User{
			Email:     email,
			Name:      name,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			LastLogin: time.Now(),
		}

		user, err = h.service.CreateUser(ctx, user)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Failed to create user: %v", err),
			})
			return
		}
	}

	// Link the provider to the user if not already linked
	providerUserID := userInfo.ID
	if providerUserID == "" && claims != nil {
		providerUserID = claims.Subject
	}

	if providerUserID != "" {
		err = h.service.LinkUserProvider(ctx, user.ID, providerID, providerUserID, email)
		if err != nil {
			// Log the error but continue
			fmt.Printf("Failed to link provider: %v\n", err)
		}
	}

	// Generate JWT tokens
	tokenPair, err := h.service.GenerateTokens(ctx, user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to generate tokens: %v", err),
		})
		return
	}

	// Return the tokens
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
			"error": "Use the /auth/callback endpoint for authorization code grant",
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

// Logout revokes a refresh token
func (h *Handlers) Logout(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request",
		})
		return
	}

	// Revoke the token
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
	user, err := GetUserFromContext(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "User not found in context",
		})
		return
	}

	c.JSON(http.StatusOK, user)
}

// Helper functions

// generateRandomState generates a random state parameter
func generateRandomState() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
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
