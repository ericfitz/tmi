package workflows

// TestStepUpRoundTrip exercises the full step-up authentication round-trip:
//
//  1. Authenticate admin → verify fresh tokens can hit admin endpoint (GET /admin/groups).
//  2. Stale-path: write a legacy 2-field refresh token to Redis, exchange it for a
//     new JWT (which has auth_time = epoch zero ≡ stale), then call an admin write
//     (POST /admin/groups) and expect 401 + WWW-Authenticate challenge.
//  3. Fresh-path: re-authenticate via OAuth → call POST /admin/groups → expect 201.
//  4. Audit row: query system_audit_entries and confirm a row landed for the create.
//
// The stale path uses the legacy 2-field token trick introduced in Task 3
// (commit 2adc3731): when a 2-field value ("userInternalUUID|sessionCreatedAt")
// is refreshed, the server mints a new JWT with auth_time = time.Unix(0, 0),
// which StepUpMiddleware treats as perpetually stale.
//
// Requires:
//   - INTEGRATION_TESTS=true
//   - TMI server running (make start-dev)
//   - OAuth stub running (make start-oauth-stub)
//   - Redis reachable at localhost:6379

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/test/integration/framework"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// createAdminGroupRequest is the body for POST /admin/groups.
type createAdminGroupRequest struct {
	GroupName   string `json:"group_name"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func TestStepUpRoundTrip(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	// Verify OAuth stub is available.
	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nRun: make start-oauth-stub", err)
	}

	// Open a DB connection for audit row verification.
	db, err := framework.NewDevDatabase()
	if err != nil {
		t.Fatalf("Cannot connect to dev database: %v", err)
	}
	defer db.Close()

	// ----------------------------------------------------------------
	// Step 1: Authenticate admin and verify fresh tokens pass step-up.
	// ----------------------------------------------------------------
	t.Log("Step 1: Authenticating admin (fresh OAuth login)")
	freshTokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "AuthenticateAdmin failed")

	client, err := framework.NewClient(serverURL, freshTokens)
	framework.AssertNoError(t, err, "NewClient failed")

	resp, err := client.Do(framework.Request{
		Method: "GET",
		Path:   "/admin/groups",
	})
	framework.AssertNoError(t, err, "GET /admin/groups with fresh tokens failed")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for GET /admin/groups with fresh tokens, got %d; body: %s",
			resp.StatusCode, string(resp.Body))
	}
	t.Log("Step 1 passed: fresh tokens can reach admin endpoints")

	// ----------------------------------------------------------------
	// Step 2: Manufacture a stale JWT via the legacy 2-field refresh path.
	// ----------------------------------------------------------------

	// 2a. Look up the admin user's internal UUID from the DB.
	t.Log("Step 2a: Looking up admin user's internal UUID")
	adminInternalUUID, err := db.QueryString(
		"SELECT internal_uuid FROM users WHERE provider_user_id = 'test-admin' AND provider = 'tmi' LIMIT 1",
	)
	if err != nil || adminInternalUUID == "" {
		t.Fatalf("Could not find test-admin user in DB (err=%v uuid=%q); is the dev server running and test-admin previously authenticated?",
			err, adminInternalUUID)
	}
	t.Logf("Step 2a: admin internal_uuid = %s", adminInternalUUID)

	// 2b. Write a legacy 2-field refresh token directly to Redis.
	// Format: "userInternalUUID|sessionCreatedAtUnix"  (no third field)
	// The server will mint a new JWT with auth_time = epoch zero.
	t.Log("Step 2b: Writing legacy 2-field refresh token to Redis")
	legacyRefreshTokenID := "step-up-test-legacy-" + uuid.New().String()
	legacySessionCreatedAt := time.Now().Unix() // valid session, just no auth_time

	if err := writeRefreshTokenToRedis(t, legacyRefreshTokenID, adminInternalUUID, legacySessionCreatedAt); err != nil {
		t.Fatalf("Failed to write legacy refresh token to Redis: %v", err)
	}

	// 2c. Exchange the legacy refresh token for a new JWT via POST /oauth2/token.
	// This hits the server's RefreshToken path, which parses the 2-field value and
	// mints a JWT with auth_time = epoch zero (stale sentinel).
	t.Log("Step 2c: Exchanging legacy 2-field refresh token for stale JWT")
	staleTokens, err := refreshTokenDirectly(serverURL, legacyRefreshTokenID)
	if err != nil {
		t.Fatalf("Token refresh with legacy token failed: %v", err)
	}
	t.Log("Step 2c: got stale JWT from legacy 2-field refresh")

	// 2d. Use the stale JWT to call an admin write — expect 401 + WWW-Authenticate.
	t.Log("Step 2d: Calling POST /admin/groups with stale JWT — expect 401")
	staleGroupName := "step-up-stale-test-" + uuid.New().String()[:8]
	staleFail, err := doGroupCreate(serverURL, staleTokens.AccessToken, staleGroupName)
	framework.AssertNoError(t, err, "POST /admin/groups HTTP call (stale) failed unexpectedly")

	if staleFail.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Expected 401 for stale-auth-time admin write, got %d; body: %s",
			staleFail.StatusCode, string(staleFail.Body))
	}

	wwwAuth := staleFail.Headers.Get("WWW-Authenticate")
	if !strings.Contains(wwwAuth, "insufficient_user_authentication") {
		t.Errorf("WWW-Authenticate header missing or wrong: %q", wwwAuth)
	}
	if !strings.Contains(wwwAuth, "max_age=") {
		t.Errorf("WWW-Authenticate header missing max_age directive: %q", wwwAuth)
	}
	t.Logf("Step 2d passed: got 401 with WWW-Authenticate: %s", wwwAuth)

	// ----------------------------------------------------------------
	// Step 3: Re-authenticate to get fresh tokens, then write succeeds.
	// ----------------------------------------------------------------
	t.Log("Step 3: Re-authenticating admin to get fresh JWT")
	// Force a new OAuth flow — AuthenticateAdmin caches with sync.Once so we
	// call AuthenticateUser directly to get uncached fresh tokens.
	freshTokens2, err := framework.AuthenticateUser("test-admin")
	framework.AssertNoError(t, err, "Second AuthenticateUser failed")

	// Mark the time before the write so we can find the audit row.
	beforeCreate := time.Now().UTC()

	t.Log("Step 3: Calling POST /admin/groups with fresh JWT — expect 201")
	suffix := uuid.New().String()[:8]
	testGroupName := "step-up-test-grp-" + suffix
	testGroupHumanName := "Step-Up Integration Test Group " + suffix

	freshOK, err := doGroupCreate(serverURL, freshTokens2.AccessToken, testGroupName)
	framework.AssertNoError(t, err, "POST /admin/groups HTTP call (fresh) failed unexpectedly")

	if freshOK.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 for fresh-auth-time admin write, got %d; body: %s",
			freshOK.StatusCode, string(freshOK.Body))
	}
	t.Logf("Step 3 passed: POST /admin/groups returned 201")

	// Extract the created group UUID for cleanup.
	var createResp map[string]interface{}
	if err := json.Unmarshal(freshOK.Body, &createResp); err != nil {
		t.Fatalf("Failed to parse group create response: %v", err)
	}
	createdGroupID, _ := createResp["internal_uuid"].(string)

	// 3b. Cleanup: DELETE the created group (ignore errors — cleanup is best-effort).
	if createdGroupID != "" {
		t.Cleanup(func() {
			cleanupClient, cerr := framework.NewClient(serverURL, freshTokens2)
			if cerr != nil {
				t.Logf("Cleanup: failed to create client: %v", cerr)
				return
			}
			delResp, cerr := cleanupClient.Do(framework.Request{
				Method: "DELETE",
				Path:   "/admin/groups/" + createdGroupID,
			})
			if cerr != nil || (delResp.StatusCode != http.StatusNoContent && delResp.StatusCode != http.StatusNotFound) {
				t.Logf("Cleanup: DELETE /admin/groups/%s returned status=%d err=%v",
					createdGroupID, delResp.StatusCode, cerr)
			} else {
				t.Logf("Cleanup: deleted test group %s", createdGroupID)
			}
		})
	}

	// ----------------------------------------------------------------
	// Step 4: Verify the audit row landed in system_audit_entries.
	// ----------------------------------------------------------------
	t.Log("Step 4: Verifying audit row in system_audit_entries")

	// Look up the admin's email to match against the audit row.
	adminEmail, _ := db.QueryString(
		"SELECT email FROM users WHERE provider_user_id = 'test-admin' AND provider = 'tmi' LIMIT 1",
	)
	if adminEmail == "" {
		t.Log("Warning: could not determine test-admin email; audit row check will be less precise")
	}

	// Poll briefly for the audit row — the middleware writes it synchronously, but
	// give the DB a moment to make the committed row visible on this connection.
	var auditFieldPath, auditMethod string
	deadline := time.Now().Add(3 * time.Second)
	found := false
	for {
		var queryErr error
		if adminEmail != "" {
			auditFieldPath, queryErr = db.QueryString(fmt.Sprintf(
				"SELECT field_path FROM system_audit_entries "+
					"WHERE actor_email = '%s' AND field_path = 'groups.create' AND http_method = 'POST' "+
					"AND created_at >= '%s' ORDER BY created_at DESC LIMIT 1",
				adminEmail, beforeCreate.Format("2006-01-02T15:04:05"),
			))
			auditMethod = "POST"
		} else {
			// Fallback: any groups.create row written after beforeCreate.
			auditFieldPath, queryErr = db.QueryString(fmt.Sprintf(
				"SELECT field_path FROM system_audit_entries "+
					"WHERE field_path = 'groups.create' AND http_method = 'POST' "+
					"AND created_at >= '%s' ORDER BY created_at DESC LIMIT 1",
				beforeCreate.Format("2006-01-02T15:04:05"),
			))
			auditMethod = "POST"
		}
		if queryErr == nil && auditFieldPath != "" {
			found = true
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	if !found {
		t.Errorf("No system_audit_entries row found for groups.create by %s after %v",
			adminEmail, beforeCreate)
	} else {
		t.Logf("Step 4 passed: audit row found — field_path=%s http_method=%s group_name=%s",
			auditFieldPath, auditMethod, testGroupHumanName)
	}
}

// writeRefreshTokenToRedis writes a legacy 2-field refresh token to the dev Redis.
// Key:   "refresh_token:<id>"
// Value: "<userInternalUUID>|<sessionCreatedAtUnix>"
// This is the pre-#355 format that causes RefreshToken() to mint a JWT with
// auth_time = epoch zero (stale sentinel for StepUpMiddleware).
func writeRefreshTokenToRedis(t *testing.T, refreshTokenID, userInternalUUID string, sessionCreatedAt int64) error {
	t.Helper()

	host := getEnvOrDefault("TEST_REDIS_HOST", "localhost")
	port := getEnvOrDefault("TEST_REDIS_PORT", "6379")

	rdb := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", host, port),
		DB:   0,
	})
	defer rdb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Ping to give an early, clear error if Redis is unreachable.
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis not reachable at %s:%s: %w", host, port, err)
	}

	key := "refresh_token:" + refreshTokenID
	val := fmt.Sprintf("%s|%d", userInternalUUID, sessionCreatedAt)

	return rdb.Set(ctx, key, val, time.Hour).Err()
}

// refreshTokenDirectly calls POST /oauth2/token with grant_type=refresh_token
// directly against the TMI server (not via the OAuth stub). Returns the
// token pair or an error.
func refreshTokenDirectly(serverURL, refreshToken string) (*framework.OAuthTokens, error) {
	form := fmt.Sprintf("grant_type=refresh_token&refresh_token=%s", refreshToken)
	req, err := http.NewRequest(http.MethodPost, serverURL+"/oauth2/token",
		strings.NewReader(form))
	if err != nil {
		return nil, fmt.Errorf("creating refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST /oauth2/token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("POST /oauth2/token returned %d: %s", resp.StatusCode, string(body))
	}

	var tokens framework.OAuthTokens
	if err := json.Unmarshal(body, &tokens); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	if tokens.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token: %s", string(body))
	}
	return &tokens, nil
}

// doGroupCreate issues POST /admin/groups with the given bearer token and a
// generated group name. Returns the raw framework.Response for status/header inspection.
func doGroupCreate(serverURL, accessToken, groupName string) (*framework.Response, error) {
	reqBody := createAdminGroupRequest{
		GroupName:   groupName,
		Name:        "Step-Up Integration Test: " + groupName,
		Description: "Created by TestStepUpRoundTrip",
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling group create body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, serverURL+"/admin/groups",
		bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating group request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	httpClient := &http.Client{Timeout: 15 * time.Second}
	httpResp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST /admin/groups: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading group create response: %w", err)
	}

	return &framework.Response{
		StatusCode: httpResp.StatusCode,
		Headers:    httpResp.Header,
		Body:       respBody,
	}, nil
}

// getEnvOrDefault returns the value of the env var or the provided default.
// This mirrors the unexported helper in framework/database.go.
func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
