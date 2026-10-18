package workflows

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestUserAPIRateLimiting_Integration verifies the per-user API rate limiter
// on authenticated REST routes (#1103): a fresh user's quota is lowered by an
// admin, then the user's requests are counted and rejected with 429 once over
// the limit.
// Requires: running TMI server + Redis AND rate limiting enabled at the server
// (TMI_TEST_ENABLE_RATE_LIMITING=true); skips otherwise.
func TestUserAPIRateLimiting_Integration(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if !framework.IsRateLimitingActive(serverURL) {
		t.Skip("Rate limiting is not active on the server; skipping per-user rate-limit assertions")
	}

	const quota = 5
	_ = framework.ClearRateLimits()

	userID := framework.UniqueUserID()
	userTokens, err := framework.AuthenticateUser(userID)
	framework.AssertNoError(t, err, "AuthenticateUser failed")
	userClient, err := framework.NewClient(serverURL, userTokens, framework.WithValidation(false))
	framework.AssertNoError(t, err, "Failed to create user client")

	adminTokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "AuthenticateAdmin failed")
	adminClient, err := framework.NewClient(serverURL, adminTokens, framework.WithValidation(false))
	framework.AssertNoError(t, err, "Failed to create admin client")

	db, err := framework.NewDevDatabase()
	framework.AssertNoError(t, err, "Failed to open database")
	defer db.Close()
	userUUID, err := db.QueryString(fmt.Sprintf(
		"SELECT internal_uuid FROM users WHERE provider_user_id = '%s' AND provider = 'tmi' FETCH FIRST 1 ROWS ONLY", userID))
	if err != nil || userUUID == "" {
		t.Fatalf("Could not look up internal UUID for %s (err=%v)", userID, err)
	}

	quotaPath := "/admin/quotas/users/" + userUUID
	resp, err := adminClient.Do(framework.Request{
		Method: "PUT",
		Path:   quotaPath,
		Body:   map[string]interface{}{"max_requests_per_minute": quota},
	})
	framework.AssertNoError(t, err, "PUT user quota failed")
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		t.Fatalf("Expected 200/201 setting quota, got %d; body: %s", resp.StatusCode, string(resp.Body))
	}
	t.Cleanup(func() {
		_, _ = adminClient.Do(framework.Request{Method: "DELETE", Path: quotaPath})
		_ = framework.ClearRateLimits()
	})
	_ = framework.ClearRateLimits()

	prevRemaining := -1
	var limited bool
	for i := 1; i <= quota+1; i++ {
		resp, err := userClient.Do(framework.Request{Method: "GET", Path: "/threat_models"})
		framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i))

		if got := resp.Headers.Get("X-RateLimit-Limit"); got != strconv.Itoa(quota) {
			t.Fatalf("Request %d: X-RateLimit-Limit = %q, want %d", i, got, quota)
		}
		remaining, convErr := strconv.Atoi(resp.Headers.Get("X-RateLimit-Remaining"))
		framework.AssertNoError(t, convErr, "X-RateLimit-Remaining not an integer")
		if resp.StatusCode == 429 {
			limited = true
			// The exhausted window reports 0 remaining, the same as the last allowed request.
			if remaining != 0 {
				t.Errorf("Request %d: X-RateLimit-Remaining on 429 = %d, want 0", i, remaining)
			}
			if i <= quota {
				t.Errorf("Request %d was rate limited before the quota of %d was used", i, quota)
			}
			if resp.Headers.Get("Retry-After") == "" {
				t.Error("Missing Retry-After header on 429 response")
			}
			var body struct {
				Error string `json:"error"`
			}
			framework.AssertNoError(t, json.Unmarshal(resp.Body, &body), "decode 429 body")
			if body.Error != "rate_limit_exceeded" {
				t.Errorf("429 error = %q, want rate_limit_exceeded", body.Error)
			}
			break
		}
		if resp.StatusCode != 200 {
			t.Fatalf("Request %d: expected 200, got %d; body: %s", i, resp.StatusCode, string(resp.Body))
		}
		if prevRemaining >= 0 && remaining >= prevRemaining {
			t.Errorf("Request %d: X-RateLimit-Remaining %d did not decrease from %d", i, remaining, prevRemaining)
		}
		prevRemaining = remaining
	}
	if !limited {
		t.Fatalf("No 429 after %d requests with a quota of %d", quota+1, quota)
	}
}
