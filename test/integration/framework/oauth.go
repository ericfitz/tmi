package framework

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// OAuthStubURL is the default OAuth callback stub URL
	OAuthStubURL = "http://localhost:8079"
)

// OAuthTokens represents OAuth token response
type OAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// OAuthFlowInitRequest represents the request to initialize OAuth flow
type OAuthFlowInitRequest struct {
	UserID string `json:"userid,omitempty"`
	IDP    string `json:"idp,omitempty"`
	Scopes string `json:"scopes,omitempty"`
}

// OAuthFlowInitResponse represents the response from OAuth init
type OAuthFlowInitResponse struct {
	State              string `json:"state"`
	CodeVerifier       string `json:"code_verifier"`
	CodeChallenge      string `json:"code_challenge"`
	AuthorizationURL   string `json:"authorization_url"`
}

// OAuthFlowStartRequest represents automated flow start request
type OAuthFlowStartRequest struct {
	UserID string `json:"userid,omitempty"`
	IDP    string `json:"idp,omitempty"`
	Scopes string `json:"scopes,omitempty"`
}

// OAuthFlowStartResponse represents automated flow start response
type OAuthFlowStartResponse struct {
	FlowID  string `json:"flow_id"`
	Status  string `json:"status"`
	PollURL string `json:"poll_url"`
}

// OAuthFlowStatusResponse represents flow status polling response
type OAuthFlowStatusResponse struct {
	FlowID      string       `json:"flow_id"`
	Status      string       `json:"status"`
	Tokens      *OAuthTokens `json:"tokens,omitempty"`
	TokensReady bool         `json:"tokens_ready"`
	Error       string       `json:"error,omitempty"`
}

// OAuthCredentialsResponse represents stored credentials response
type OAuthCredentialsResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    string `json:"expires_in"`
	State        string `json:"state"`
}

// AuthenticateUser performs OAuth authentication for a test user
// This is the recommended method - uses automated end-to-end flow
func AuthenticateUser(userID string) (*OAuthTokens, error) {
	return AuthenticateUserWithStub(userID, OAuthStubURL)
}

// AuthenticateUserWithStub performs OAuth authentication using specified stub URL
func AuthenticateUserWithStub(userID, stubURL string) (*OAuthTokens, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	// Start automated OAuth flow
	startReq := OAuthFlowStartRequest{
		UserID: userID,
		IDP:    "test",
		Scopes: "openid profile email",
	}

	reqBody, err := json.Marshal(startReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal flow start request: %w", err)
	}

	resp, err := client.Post(stubURL+"/flows/start", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to start OAuth flow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OAuth flow start failed with status %d: %s", resp.StatusCode, string(body))
	}

	var startResp OAuthFlowStartResponse
	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		return nil, fmt.Errorf("failed to decode flow start response: %w", err)
	}

	// Poll for completion (with timeout)
	maxAttempts := 30 // 30 seconds max
	for i := 0; i < maxAttempts; i++ {
		time.Sleep(1 * time.Second)

		pollResp, err := client.Get(stubURL + startResp.PollURL)
		if err != nil {
			return nil, fmt.Errorf("failed to poll flow status: %w", err)
		}

		var statusResp OAuthFlowStatusResponse
		if err := json.NewDecoder(pollResp.Body).Decode(&statusResp); err != nil {
			pollResp.Body.Close()
			return nil, fmt.Errorf("failed to decode flow status: %w", err)
		}
		pollResp.Body.Close()

		if statusResp.Status == "completed" && statusResp.TokensReady {
			return statusResp.Tokens, nil
		}

		if statusResp.Status == "failed" {
			return nil, fmt.Errorf("OAuth flow failed: %s", statusResp.Error)
		}
	}

	return nil, fmt.Errorf("OAuth flow timed out after %d seconds", maxAttempts)
}

// GetStoredCredentials retrieves previously stored credentials for a user
// Useful when you need to reuse credentials across multiple tests
func GetStoredCredentials(userID string) (*OAuthTokens, error) {
	return GetStoredCredentialsFromStub(userID, OAuthStubURL)
}

// GetStoredCredentialsFromStub retrieves credentials from specified stub URL
func GetStoredCredentialsFromStub(userID, stubURL string) (*OAuthTokens, error) {
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(fmt.Sprintf("%s/creds?userid=%s", stubURL, userID))
	if err != nil {
		return nil, fmt.Errorf("failed to get stored credentials: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to retrieve credentials (status %d): %s", resp.StatusCode, string(body))
	}

	var credsResp OAuthCredentialsResponse
	if err := json.NewDecoder(resp.Body).Decode(&credsResp); err != nil {
		return nil, fmt.Errorf("failed to decode credentials: %w", err)
	}

	return &OAuthTokens{
		AccessToken:  credsResp.AccessToken,
		RefreshToken: credsResp.RefreshToken,
		TokenType:    credsResp.TokenType,
		ExpiresIn:    0, // Not provided in stored creds
	}, nil
}

// RefreshToken refreshes an access token using a refresh token
func RefreshToken(refreshToken, userID string) (*OAuthTokens, error) {
	return RefreshTokenWithStub(refreshToken, userID, OAuthStubURL)
}

// RefreshTokenWithStub refreshes a token using specified stub URL
func RefreshTokenWithStub(refreshToken, userID, stubURL string) (*OAuthTokens, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	refreshReq := map[string]string{
		"refresh_token": refreshToken,
		"userid":        userID,
		"idp":           "test",
	}

	reqBody, err := json.Marshal(refreshReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal refresh request: %w", err)
	}

	resp, err := client.Post(stubURL+"/refresh", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokens OAuthTokens
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("failed to decode refresh response: %w", err)
	}

	return &tokens, nil
}

// EnsureOAuthStubRunning checks if OAuth stub is running, returns error if not
func EnsureOAuthStubRunning() error {
	return EnsureOAuthStubRunningAt(OAuthStubURL)
}

// EnsureOAuthStubRunningAt checks if OAuth stub is running at specified URL
func EnsureOAuthStubRunningAt(stubURL string) error {
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get(stubURL + "/latest")
	if err != nil {
		return fmt.Errorf("OAuth stub not running at %s: %w (did you run 'make start-oauth-stub'?)", stubURL, err)
	}
	defer resp.Body.Close()

	return nil
}
