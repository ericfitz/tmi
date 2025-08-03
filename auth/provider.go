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
	ID            string `json:"id,omitempty"`
	Email         string `json:"email,omitempty"`
	VerifiedEmail bool   `json:"verified_email,omitempty"`
	Name          string `json:"name,omitempty"`
	GivenName     string `json:"given_name,omitempty"`
	FamilyName    string `json:"family_name,omitempty"`
	Picture       string `json:"picture,omitempty"`
	Locale        string `json:"locale,omitempty"`
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
	switch config.ID {
	case "google":
		return NewGoogleProvider(config, callbackURL)
	case "github":
		return NewGithubProvider(config, callbackURL)
	case "microsoft":
		return NewMicrosoftProvider(config, callbackURL)
	case "test":
		return newTestProvider(config, callbackURL), nil
	default:
		// Generic OIDC provider for any standard-compliant provider
		return NewGenericOIDCProvider(config, callbackURL)
	}
}

// BaseProvider provides common functionality for all providers
type BaseProvider struct {
	config       OAuthProviderConfig
	oauth2Config *oauth2.Config
	httpClient   *http.Client
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
	token, err := p.oauth2Config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	// Calculate expiration duration once to avoid time drift
	expiresIn := int(time.Until(token.Expiry).Seconds())

	return &TokenResponse{
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		RefreshToken: token.RefreshToken,
		ExpiresIn:    expiresIn,
		IDToken:      token.Extra("id_token").(string),
	}, nil
}

// GetUserInfo gets user information from the provider
func (p *BaseProvider) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.config.UserInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get user info: %s", body)
	}

	var userInfo UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %w", err)
	}

	return &userInfo, nil
}

// ValidateIDToken validates an ID token
func (p *BaseProvider) ValidateIDToken(ctx context.Context, idToken string) (*IDTokenClaims, error) {
	// This is a placeholder. Each provider should implement its own validation.
	return nil, fmt.Errorf("ID token validation not implemented for this provider")
}

// GoogleProvider is the OAuth provider for Google
type GoogleProvider struct {
	BaseProvider
	verifier *oidc.IDTokenVerifier
}

// NewGoogleProvider creates a new Google OAuth provider
func NewGoogleProvider(config OAuthProviderConfig, callbackURL string) (*GoogleProvider, error) {
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

	// Create an OIDC provider
	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	// Create an ID token verifier
	verifier := provider.Verifier(&oidc.Config{
		ClientID: config.ClientID,
	})

	return &GoogleProvider{
		BaseProvider: BaseProvider{
			config:       config,
			oauth2Config: oauth2Config,
			httpClient:   httpClient,
		},
		verifier: verifier,
	}, nil
}

// ValidateIDToken validates an ID token
func (p *GoogleProvider) ValidateIDToken(ctx context.Context, idToken string) (*IDTokenClaims, error) {
	token, err := p.verifier.Verify(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify ID token: %w", err)
	}

	var claims IDTokenClaims
	if err := token.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse ID token claims: %w", err)
	}

	return &claims, nil
}

// GithubProvider is the OAuth provider for GitHub
type GithubProvider struct {
	BaseProvider
}

// NewGithubProvider creates a new GitHub OAuth provider
func NewGithubProvider(config OAuthProviderConfig, callbackURL string) (*GithubProvider, error) {
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

	return &GithubProvider{
		BaseProvider: BaseProvider{
			config:       config,
			oauth2Config: oauth2Config,
			httpClient:   httpClient,
		},
	}, nil
}

// ExchangeCode exchanges an authorization code for tokens
func (p *GithubProvider) ExchangeCode(ctx context.Context, code string) (*TokenResponse, error) {
	// GitHub's OAuth implementation requires Accept: application/json header
	data := url.Values{}
	data.Set("client_id", p.config.ClientID)
	data.Set("client_secret", p.config.ClientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", p.oauth2Config.RedirectURL)

	req, err := http.NewRequestWithContext(ctx, "POST", p.config.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to exchange code: %s", body)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &TokenResponse{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresIn:   0, // GitHub doesn't provide an expiration time
	}, nil
}

// GetUserInfo gets user information from GitHub
func (p *GithubProvider) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	// First, get the user profile
	req, err := http.NewRequestWithContext(ctx, "GET", p.config.UserInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", accessToken))
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get user info: %s", body)
	}

	var githubUser struct {
		ID        int    `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&githubUser); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %w", err)
	}

	// Then, get the user's email
	req, err = http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user/emails", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", accessToken))
	req.Header.Set("Accept", "application/json")

	resp, err = p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user emails: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get user emails: %s", body)
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return nil, fmt.Errorf("failed to decode user emails: %w", err)
	}

	// Find the primary email
	var primaryEmail string
	var verified bool
	for _, email := range emails {
		if email.Primary {
			primaryEmail = email.Email
			verified = email.Verified
			break
		}
	}

	// If no primary email is found, use the first one
	if primaryEmail == "" && len(emails) > 0 {
		primaryEmail = emails[0].Email
		verified = emails[0].Verified
	}

	return &UserInfo{
		ID:            fmt.Sprintf("%d", githubUser.ID),
		Email:         primaryEmail,
		VerifiedEmail: verified,
		Name:          githubUser.Name,
		Picture:       githubUser.AvatarURL,
	}, nil
}

// MicrosoftProvider is the OAuth provider for Microsoft
type MicrosoftProvider struct {
	BaseProvider
	verifier *oidc.IDTokenVerifier
}

// NewMicrosoftProvider creates a new Microsoft OAuth provider
func NewMicrosoftProvider(config OAuthProviderConfig, callbackURL string) (*MicrosoftProvider, error) {
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

	// Microsoft's /common endpoint creates issuer validation issues
	// We'll create the provider without OIDC discovery for Microsoft
	ctx := context.Background()
	var provider *oidc.Provider
	var err error
	
	// Try to create OIDC provider, but if it fails due to issuer mismatch, we'll skip OIDC verification
	// This is a common issue with Microsoft's multi-tenant setup
	provider, err = oidc.NewProvider(ctx, "https://login.microsoftonline.com/common/v2.0")
	if err != nil && strings.Contains(err.Error(), "issuer did not match") {
		// For Microsoft, we'll skip OIDC provider creation and handle token validation manually
		provider = nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	// Create an ID token verifier if we have an OIDC provider
	var verifier *oidc.IDTokenVerifier
	if provider != nil {
		verifier = provider.Verifier(&oidc.Config{
			ClientID:        config.ClientID,
			SkipIssuerCheck: true, // Skip issuer validation for Microsoft due to tenant-specific issuers
		})
	}

	return &MicrosoftProvider{
		BaseProvider: BaseProvider{
			config:       config,
			oauth2Config: oauth2Config,
			httpClient:   httpClient,
		},
		verifier: verifier,
	}, nil
}

// ValidateIDToken validates an ID token
func (p *MicrosoftProvider) ValidateIDToken(ctx context.Context, idToken string) (*IDTokenClaims, error) {
	if p.verifier == nil {
		// If we don't have a verifier due to Microsoft's issuer issues, skip ID token validation
		// In production, you might want to implement manual JWT validation here
		return nil, fmt.Errorf("ID token validation not available for Microsoft provider due to issuer validation issues")
	}

	token, err := p.verifier.Verify(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify ID token: %w", err)
	}

	var claims IDTokenClaims
	if err := token.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse ID token claims: %w", err)
	}

	return &claims, nil
}

// GenericOIDCProvider is a generic OIDC provider
type GenericOIDCProvider struct {
	BaseProvider
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
}

// NewGenericOIDCProvider creates a new generic OIDC provider
func NewGenericOIDCProvider(config OAuthProviderConfig, callbackURL string) (*GenericOIDCProvider, error) {
	// Create an OIDC provider
	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, config.Issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	// Get the OAuth2 endpoints
	endpoint := provider.Endpoint()

	oauth2Config := &oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		RedirectURL:  callbackURL,
		Endpoint:     endpoint,
		Scopes:       config.Scopes,
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}

	// Create an ID token verifier
	verifier := provider.Verifier(&oidc.Config{
		ClientID: config.ClientID,
	})

	return &GenericOIDCProvider{
		BaseProvider: BaseProvider{
			config:       config,
			oauth2Config: oauth2Config,
			httpClient:   httpClient,
		},
		provider: provider,
		verifier: verifier,
	}, nil
}

// ValidateIDToken validates an ID token
func (p *GenericOIDCProvider) ValidateIDToken(ctx context.Context, idToken string) (*IDTokenClaims, error) {
	token, err := p.verifier.Verify(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify ID token: %w", err)
	}

	var claims IDTokenClaims
	if err := token.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse ID token claims: %w", err)
	}

	return &claims, nil
}
