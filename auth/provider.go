package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/ericfitz/tmi/internal/slogging"
	"golang.org/x/oauth2"
)

// Provider is the interface for OAuth providers
type Provider interface {
	// GetOAuth2Config returns the OAuth2 configuration
	GetOAuth2Config() *oauth2.Config

	// GetAuthorizationURL returns the authorization URL with the given state
	GetAuthorizationURL(state string) string

	// ExchangeCode exchanges an authorization code for tokens
	ExchangeCode(ctx context.Context, code string) (*TokenResponse, error)

	// GetUserInfo gets user information from the provider
	GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error)

	// ValidateIDToken validates an ID token
	ValidateIDToken(ctx context.Context, idToken string) (*IDTokenClaims, error)
}

// TokenResponse contains the response from the token endpoint
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token,omitempty"`
}

// UserInfo contains user information from the provider
type UserInfo struct {
	ID            string   `json:"id,omitempty"`
	Email         string   `json:"email,omitempty"`
	EmailVerified bool     `json:"email_verified,omitempty"`
	Name          string   `json:"name,omitempty"`
	GivenName     string   `json:"given_name,omitempty"`
	FamilyName    string   `json:"family_name,omitempty"`
	Picture       string   `json:"picture,omitempty"`
	Locale        string   `json:"locale,omitempty"`
	IdP           string   `json:"idp,omitempty"`    // Identity provider ID
	Groups        []string `json:"groups,omitempty"` // Groups from identity provider
}

// IDTokenClaims contains the claims from an ID token
type IDTokenClaims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified,omitempty"`
	Name          string `json:"name,omitempty"`
	GivenName     string `json:"given_name,omitempty"`
	FamilyName    string `json:"family_name,omitempty"`
	Picture       string `json:"picture,omitempty"`
	Locale        string `json:"locale,omitempty"`
	Issuer        string `json:"iss"`
	Audience      string `json:"aud"`
	ExpiresAt     int64  `json:"exp"`
	IssuedAt      int64  `json:"iat"`
}

// NewProvider creates a new OAuth provider based on the provider ID
func NewProvider(config OAuthProviderConfig, callbackURL string) (Provider, error) {
	logger := slogging.Get()
	logger.Debug("Creating OAuth provider: %s (%s)", config.ID, config.Name)

	switch config.ID {
	case "test", "tmi":
		// TMI internal OAuth provider (formerly "test" provider)
		// In dev/test builds: Supports Authorization Code flow with ephemeral user creation
		// In production builds: Only supports Client Credentials Grant
		logger.Debug("Creating TMI provider provider_id=%v", config.ID)
		provider := newTestProvider(config, callbackURL)
		if provider == nil {
			logger.Error("TMI provider creation failed provider_id=%v", config.ID)
			return nil, fmt.Errorf("TMI provider creation failed")
		}
		logger.Info("TMI provider created successfully provider_id=%v", config.ID)
		return provider, nil
	default:
		if config.Issuer != "" && config.JWKSURL != "" {
			// Use OIDC provider for providers with ID token validation
			logger.Debug("Creating OIDC provider provider_id=%v issuer=%v", config.ID, config.Issuer)
			return NewGenericOIDCProvider(config, callbackURL)
		}
		// Use base provider for pure OAuth2
		logger.Debug("Creating base OAuth2 provider provider_id=%v", config.ID)
		return NewBaseProvider(config, callbackURL)
	}
}

// BaseProvider provides common functionality for all providers
type BaseProvider struct {
	config       OAuthProviderConfig
	oauth2Config *oauth2.Config
	httpClient   *http.Client
}

// NewBaseProvider creates a new base OAuth provider
func NewBaseProvider(config OAuthProviderConfig, callbackURL string) (*BaseProvider, error) {
	logger := slogging.Get()
	logger.Info("Initializing base OAuth provider provider_id=%v provider_name=%v", config.ID, config.Name)

	oauth2Config := &oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		RedirectURL:  callbackURL,
		Endpoint: oauth2.Endpoint{
			AuthURL:  config.AuthorizationURL,
			TokenURL: config.TokenURL,
		},
		Scopes: config.Scopes,
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}

	logger.Info("Base OAuth provider initialized successfully provider_id=%v scopes=%v", config.ID, config.Scopes)
	return &BaseProvider{
		config:       config,
		oauth2Config: oauth2Config,
		httpClient:   httpClient,
	}, nil
}

// GetOAuth2Config returns the OAuth2 configuration
func (p *BaseProvider) GetOAuth2Config() *oauth2.Config {
	return p.oauth2Config
}

// GetAuthorizationURL returns the authorization URL with the given state
func (p *BaseProvider) GetAuthorizationURL(state string) string {
	return p.oauth2Config.AuthCodeURL(state)
}

// ExchangeCode exchanges an authorization code for tokens
func (p *BaseProvider) ExchangeCode(ctx context.Context, code string) (*TokenResponse, error) {
	logger := slogging.Get()
	logger.Debug("Exchanging authorization code for tokens provider_id=%v", p.config.ID)

	// Some providers (like GitHub) require Accept header
	if p.config.AcceptHeader != "" {
		// Custom token exchange for providers that need special headers
		logger.Debug("Using custom token exchange provider_id=%v", p.config.ID)
		return p.customTokenExchange(ctx, code)
	}

	// Standard OAuth2 token exchange
	logger.Debug("Using standard OAuth2 token exchange provider_id=%v", p.config.ID)
	token, err := p.oauth2Config.Exchange(ctx, code)
	if err != nil {
		logger.Error("Failed to exchange authorization code provider_id=%v error=%v", p.config.ID, err)
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	// Calculate expiration duration
	expiresIn := int(time.Until(token.Expiry).Seconds())

	response := &TokenResponse{
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		RefreshToken: token.RefreshToken,
		ExpiresIn:    expiresIn,
	}

	// Extract ID token if present
	hasIDToken := false
	if idToken := token.Extra("id_token"); idToken != nil {
		if idTokenStr, ok := idToken.(string); ok {
			response.IDToken = idTokenStr
			hasIDToken = true
		}
	}

	logger.Info("Token exchange successful provider_id=%v token_type=%v has_id_token=%v expires_in=%v", p.config.ID, token.TokenType, hasIDToken, expiresIn)
	return response, nil
}

// customTokenExchange handles token exchange for providers that need special headers
func (p *BaseProvider) customTokenExchange(ctx context.Context, code string) (*TokenResponse, error) {
	logger := slogging.Get()
	logger.Debug("Performing custom token exchange provider_id=%v", p.config.ID)

	data := url.Values{}
	data.Set("client_id", p.config.ClientID)
	data.Set("client_secret", p.config.ClientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", p.oauth2Config.RedirectURL)
	data.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, "POST", p.config.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		logger.Error("Failed to create token exchange request provider_id=%v error=%v", p.config.ID, err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if p.config.AcceptHeader != "" {
		req.Header.Set("Accept", p.config.AcceptHeader)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		logger.Error("Custom token exchange request failed provider_id=%v error=%v", p.config.ID, err)
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		logger.Error("Token exchange returned error status provider_id=%v status_code=%v response_body=%v", p.config.ID, resp.StatusCode, string(body))
		return nil, fmt.Errorf("failed to exchange code: %s", body)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		logger.Error("Failed to decode token response provider_id=%v error=%v", p.config.ID, err)
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	// GitHub doesn't provide expires_in, set a default
	if tokenResp.ExpiresIn == 0 {
		tokenResp.ExpiresIn = 3600 // 1 hour default
		logger.Debug("Set default token expiration provider_id=%v expires_in=%v", p.config.ID, 3600)
	}

	logger.Info("Custom token exchange successful provider_id=%v token_type=%v expires_in=%v", p.config.ID, tokenResp.TokenType, tokenResp.ExpiresIn)
	return &tokenResp, nil
}

// GetUserInfo gets user information from the provider
func (p *BaseProvider) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	logger := slogging.Get()
	logger.Debug("Fetching user information provider_id=%v endpoints_count=%v", p.config.ID, len(p.config.UserInfo))

	if len(p.config.UserInfo) == 0 {
		logger.Error("No userinfo endpoints configured provider_id=%v", p.config.ID)
		return nil, fmt.Errorf("no userinfo endpoints configured")
	}

	userInfo := &UserInfo{
		IdP: p.config.ID, // Set the identity provider ID
	}

	// Determine auth header format
	authHeaderFormat := p.config.AuthHeaderFormat
	if authHeaderFormat == "" {
		authHeaderFormat = "Bearer %s"
	}

	// Determine accept header
	acceptHeader := p.config.AcceptHeader
	if acceptHeader == "" {
		acceptHeader = "application/json"
	}

	// Process each userinfo endpoint
	for i, endpoint := range p.config.UserInfo {
		logger.Debug("Processing userinfo endpoint provider_id=%v endpoint_index=%v endpoint_url=%v", p.config.ID, i, endpoint.URL)
		// Fetch data from endpoint
		jsonData, err := p.fetchEndpoint(ctx, endpoint.URL, accessToken, authHeaderFormat, acceptHeader)
		if err != nil {
			logger.Error("Failed to fetch userinfo provider_id=%v endpoint_url=%v error=%v", p.config.ID, endpoint.URL, err)
			return nil, fmt.Errorf("failed to fetch userinfo from %s: %w", endpoint.URL, err)
		}

		// Make a copy of the claims map
		claims := make(map[string]string)
		for k, v := range endpoint.Claims {
			claims[k] = v
		}

		// For the first endpoint, apply defaults for unmapped essential claims
		if i == 0 {
			applyDefaultMappings(claims, jsonData)
		}

		// Extract claims using the claim extractor
		if err := extractClaims(jsonData, claims, userInfo); err != nil {
			logger.Error("Failed to extract claims provider_id=%v endpoint_url=%v error=%v", p.config.ID, endpoint.URL, err)
			return nil, fmt.Errorf("failed to extract claims: %w", err)
		}
		logger.Debug("Claims extracted successfully provider_id=%v endpoint_index=%v", p.config.ID, i)
	}

	logger.Info("User information retrieved successfully provider_id=%v user_id=%v user_email=%v groups_count=%v", p.config.ID, userInfo.ID, userInfo.Email, len(userInfo.Groups))
	return userInfo, nil
}

// fetchEndpoint fetches JSON data from an endpoint
func (p *BaseProvider) fetchEndpoint(ctx context.Context, url, accessToken, authHeaderFormat, acceptHeader string) (map[string]interface{}, error) {
	logger := slogging.Get()
	logger.Debug("Fetching endpoint data provider_id=%v url=%v", p.config.ID, url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		logger.Error("Failed to create endpoint request provider_id=%v url=%v error=%v", p.config.ID, url, err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf(authHeaderFormat, accessToken))
	req.Header.Set("Accept", acceptHeader)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		logger.Error("Endpoint request failed provider_id=%v url=%v error=%v", p.config.ID, url, err)
		return nil, fmt.Errorf("failed to fetch endpoint: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		logger.Error("Endpoint returned error status provider_id=%v url=%v status_code=%v response_body=%v", p.config.ID, url, resp.StatusCode, string(body))
		return nil, fmt.Errorf("failed to fetch endpoint (status %d): %s", resp.StatusCode, body)
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		// Try to decode as array
		_ = resp.Body.Close()
		logger.Debug("Retrying endpoint as array provider_id=%v url=%v", p.config.ID, url)
		req2, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
		req2.Header = req.Header
		resp2, err := p.httpClient.Do(req2)
		if err != nil {
			logger.Error("Failed to re-fetch endpoint provider_id=%v url=%v error=%v", p.config.ID, url, err)
			return nil, fmt.Errorf("failed to re-fetch endpoint: %w", err)
		}
		defer closeBody(resp2.Body)

		var arrData []interface{}
		if err := json.NewDecoder(resp2.Body).Decode(&arrData); err != nil {
			logger.Error("Failed to decode array response provider_id=%v url=%v error=%v", p.config.ID, url, err)
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		// Wrap array response so it can be accessed with [0] syntax
		data = map[string]interface{}{
			"": arrData,
		}
		logger.Debug("Successfully decoded array response provider_id=%v url=%v array_length=%v", p.config.ID, url, len(arrData))
	}

	logger.Debug("Endpoint data fetched successfully provider_id=%v url=%v", p.config.ID, url)
	return data, nil
}

// ValidateIDToken validates an ID token
func (p *BaseProvider) ValidateIDToken(ctx context.Context, idToken string) (*IDTokenClaims, error) {
	// Base provider doesn't support ID token validation
	return nil, fmt.Errorf("ID token validation not implemented for this provider")
}

// GenericOIDCProvider is a generic OIDC provider
type GenericOIDCProvider struct {
	BaseProvider
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
}

// NewGenericOIDCProvider creates a new generic OIDC provider
func NewGenericOIDCProvider(config OAuthProviderConfig, callbackURL string) (*GenericOIDCProvider, error) {
	logger := slogging.Get()
	logger.Info("Initializing generic OIDC provider provider_id=%v issuer=%v", config.ID, config.Issuer)

	// Create base provider first
	baseProvider, err := NewBaseProvider(config, callbackURL)
	if err != nil {
		logger.Error("Failed to create base provider for OIDC provider_id=%v error=%v", config.ID, err)
		return nil, err
	}

	// Create an OIDC provider
	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, config.Issuer)
	if err != nil {
		// For providers like Microsoft with issuer validation issues, fall back to base provider
		if strings.Contains(err.Error(), "issuer did not match") {
			logger.Warn("OIDC issuer validation failed, falling back to base provider provider_id=%v issuer=%v error=%v", config.ID, config.Issuer, err)
			return &GenericOIDCProvider{
				BaseProvider: *baseProvider,
				provider:     nil,
				verifier:     nil,
			}, nil
		}
		logger.Error("Failed to create OIDC provider provider_id=%v issuer=%v error=%v", config.ID, config.Issuer, err)
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	// Get the OAuth2 endpoints from OIDC discovery
	endpoint := provider.Endpoint()
	baseProvider.oauth2Config.Endpoint = endpoint

	// Create an ID token verifier
	verifierConfig := &oidc.Config{
		ClientID: config.ClientID,
	}

	// Skip issuer check for providers with known issues
	if strings.Contains(config.Issuer, "microsoft") {
		logger.Debug("Skipping issuer check for Microsoft provider provider_id=%v", config.ID)
		verifierConfig.SkipIssuerCheck = true
	}

	verifier := provider.Verifier(verifierConfig)

	logger.Info("Generic OIDC provider initialized successfully provider_id=%v issuer=%v", config.ID, config.Issuer)
	return &GenericOIDCProvider{
		BaseProvider: *baseProvider,
		provider:     provider,
		verifier:     verifier,
	}, nil
}

// ValidateIDToken validates an ID token
func (p *GenericOIDCProvider) ValidateIDToken(ctx context.Context, idToken string) (*IDTokenClaims, error) {
	logger := slogging.Get()
	logger.Debug("Validating ID token provider_id=%v", p.config.ID)

	if p.verifier == nil {
		logger.Error("ID token validation not available provider_id=%v", p.config.ID)
		return nil, fmt.Errorf("ID token validation not available for this provider")
	}

	token, err := p.verifier.Verify(ctx, idToken)
	if err != nil {
		logger.Error("ID token verification failed provider_id=%v error=%v", p.config.ID, err)
		return nil, fmt.Errorf("failed to verify ID token: %w", err)
	}

	var claims IDTokenClaims
	if err := token.Claims(&claims); err != nil {
		logger.Error("Failed to parse ID token claims provider_id=%v error=%v", p.config.ID, err)
		return nil, fmt.Errorf("failed to parse ID token claims: %w", err)
	}

	logger.Info("ID token validated successfully provider_id=%v subject=%v issuer=%v", p.config.ID, claims.Subject, claims.Issuer)
	return &claims, nil
}
