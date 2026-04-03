package framework

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
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
	UserID    string `json:"userid,omitempty"`
	IDP       string `json:"idp,omitempty"`
	Scopes    string `json:"scopes,omitempty"`
	TMIServer string `json:"tmi_server,omitempty"`
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
	// Best-effort: clear rate limit keys so OAuth flow is not throttled
	_ = ClearRateLimits()

	client := &http.Client{Timeout: 30 * time.Second}

	// Start automated OAuth flow
	startReq := OAuthFlowStartRequest{
		UserID:    userID,
		IDP:       "tmi",
		Scopes:    "openid profile email",
		TMIServer: os.Getenv("TMI_SERVER_URL"),
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
	for range maxAttempts {
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

		if statusResp.TokensReady && statusResp.Tokens != nil {
			return statusResp.Tokens, nil
		}

		if statusResp.Status == "failed" || statusResp.Status == "error" {
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
		"idp":           "tmi",
		"tmi_server":    os.Getenv("TMI_SERVER_URL"),
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

var (
	adminTokens     *OAuthTokens
	adminTokensOnce sync.Once
	adminTokensErr  error
)

// AuthenticateAdmin authenticates a user with admin privileges.
// Uses "test-admin" login hint. After first authentication, promotes the user
// to admin via direct DB insert into the Administrators group, then re-authenticates
// to get a JWT that reflects the new membership.
// Results are cached with sync.Once for efficiency.
func AuthenticateAdmin() (*OAuthTokens, error) {
	adminTokensOnce.Do(func() {
		// Step 1: Authenticate to create the user
		_, adminTokensErr = AuthenticateUser("test-admin")
		if adminTokensErr != nil {
			return
		}

		// Step 2: Promote via DB (the dev server's DB, not the test DB)
		promoteToAdmin()

		// Step 3: Re-authenticate to get JWT with admin membership
		adminTokens, adminTokensErr = AuthenticateUser("test-admin")
	})
	if adminTokensErr != nil {
		return AuthenticateUser("test-admin")
	}
	return adminTokens, nil
}

// promoteToAdmin adds test-admin to the Administrators group via the development
// database (the same DB the running server uses). The workflow tests run against
// the dev server (not a test server), so we must modify the dev DB directly.
func promoteToAdmin() {
	// Connect to the development database (default port 5432, same as dev server)
	db, err := NewDevDatabase()
	if err != nil {
		return
	}
	defer db.Close()

	const adminsGroupUUID = "00000000-0000-0000-0000-000000000002"

	// Wait for user to appear in DB (OAuth flow may not have committed yet)
	var userUUID string
	for range 10 {
		userUUID, _ = db.QueryString(
			"SELECT internal_uuid FROM users WHERE provider_user_id = 'test-admin' AND provider = 'tmi' LIMIT 1",
		)
		if userUUID != "" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if userUUID == "" {
		return
	}

	// Idempotent insert into group_members
	id := fmt.Sprintf("test-admin-%s", adminsGroupUUID[:8])
	_ = db.ExecSQL(fmt.Sprintf(
		"INSERT INTO group_members (id, group_internal_uuid, user_internal_uuid, subject_type, added_at, notes) "+
			"VALUES ('%s', '%s', '%s', 'user', NOW(), 'Integration test admin') "+
			"ON CONFLICT DO NOTHING",
		id, adminsGroupUUID, userUUID,
	))
}

// AllowLocalhostWebhooks removes the loopback address entries from the webhook URL deny list
// in the dev database, allowing integration tests to use localhost webhook receivers.
// This is idempotent and safe — the deny list entries are re-seeded on server restart.
func AllowLocalhostWebhooks() {
	db, err := NewDevDatabase()
	if err != nil {
		return
	}
	defer db.Close()

	// Remove entries that block localhost/127.* so test receivers work
	_ = db.ExecSQL("DELETE FROM webhook_url_deny_list WHERE pattern IN ('localhost', '127.*', '::1')")
}

// NewDevDatabase creates a connection to the development database (the one the dev server uses).
// Uses TEST_DEV_DB_PORT env var if set, otherwise defaults to 5432.
func NewDevDatabase() (*TestDatabase, error) {
	host := getEnvOrDefault("TEST_DB_HOST", "127.0.0.1")
	port := getEnvOrDefault("TEST_DEV_DB_PORT", "5432")
	user := getEnvOrDefault("TEST_DB_USER", "tmi_dev")
	password := getEnvOrDefault("TEST_DB_PASSWORD", "dev123")
	dbname := getEnvOrDefault("TEST_DB_NAME", "tmi_dev")

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	sqlDB, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to dev database: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping dev database: %w", err)
	}
	return &TestDatabase{db: sqlDB}, nil
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
