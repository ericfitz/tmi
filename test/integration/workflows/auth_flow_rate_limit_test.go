package workflows

import (
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestAuthFlowRateLimiting_MultiScope verifies multi-scope rate limiting
// on OAuth/SAML auth flow endpoints (Tier 2).
// Requires: running TMI server + Redis (via make start-dev) AND
// rate limiting enabled at the server. The dev server is commonly started
// with disable_rate_limiting: true, in which case this test skips —
// auth_flow_rate_limiter unit tests in api/ cover the limiter itself.
func TestAuthFlowRateLimiting_MultiScope(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if !framework.IsRateLimitingActive(serverURL) {
		t.Skip("Rate limiting is not active on the server (disable_rate_limiting or build_mode=test); skipping rate-limit assertions")
	}

	t.Run("session scope blocks after 5 requests with same state", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		sessionState := "test-session-state-" + framework.UniqueUserID()

		// Session limit is 5 requests/minute — send 5, all should succeed
		for i := 0; i < 5; i++ {
			resp, err := client.Do(framework.Request{
				Method:      "GET",
				Path:        "/oauth2/authorize",
				QueryParams: map[string]string{"state": sessionState, "idp": "tmi", "scope": "openid"},
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i+1))
			if resp.StatusCode == 429 {
				t.Fatalf("Request %d was rate limited unexpectedly", i+1)
			}
		}

		// 6th request with same state should be blocked by session scope
		resp, err := client.Do(framework.Request{
			Method:      "GET",
			Path:        "/oauth2/authorize",
			QueryParams: map[string]string{"state": sessionState, "idp": "tmi", "scope": "openid"},
		})
		framework.AssertNoError(t, err, "6th request failed")

		if resp.StatusCode != 429 {
			t.Errorf("Expected 429 on 6th request with same state, got %d", resp.StatusCode)
		}

		// Verify X-RateLimit-Scope header indicates session scope
		scope := resp.Headers.Get("X-RateLimit-Scope")
		if scope != "session" {
			t.Errorf("Expected X-RateLimit-Scope=session, got %q", scope)
		}
		if resp.Headers.Get("Retry-After") == "" {
			t.Error("Missing Retry-After header on 429 response")
		}
	})

	t.Run("different sessions are independent", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Exhaust session limit for session A
		sessionA := "session-a-" + framework.UniqueUserID()
		for i := 0; i < 6; i++ {
			resp, err := client.Do(framework.Request{
				Method:      "GET",
				Path:        "/oauth2/authorize",
				QueryParams: map[string]string{"state": sessionA, "idp": "tmi", "scope": "openid"},
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Session A request %d failed", i+1))
			_ = resp
		}

		// Session B should still work
		sessionB := "session-b-" + framework.UniqueUserID()
		resp, err := client.Do(framework.Request{
			Method:      "GET",
			Path:        "/oauth2/authorize",
			QueryParams: map[string]string{"state": sessionB, "idp": "tmi", "scope": "openid"},
		})
		framework.AssertNoError(t, err, "Session B request failed")
		if resp.StatusCode == 429 {
			t.Error("Session B was rate limited — sessions should be independent")
		}
	})

	t.Run("user identifier scope blocks after 10 attempts with same login_hint", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		loginHint := "ratelimit-test-" + framework.UniqueUserID()

		// User identifier limit is 10 attempts/hour — each with a different state to avoid session limit
		for i := 0; i < 10; i++ {
			state := fmt.Sprintf("unique-state-%s-%d", framework.UniqueUserID(), i)
			resp, err := client.Do(framework.Request{
				Method:      "GET",
				Path:        "/oauth2/authorize",
				QueryParams: map[string]string{"state": state, "login_hint": loginHint, "idp": "tmi", "scope": "openid"},
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i+1))
			if resp.StatusCode == 429 {
				t.Fatalf("Request %d was rate limited unexpectedly", i+1)
			}
		}

		// 11th request should be blocked by user scope
		resp, err := client.Do(framework.Request{
			Method:      "GET",
			Path:        "/oauth2/authorize",
			QueryParams: map[string]string{"state": "final-state-" + framework.UniqueUserID(), "login_hint": loginHint, "idp": "tmi", "scope": "openid"},
		})
		framework.AssertNoError(t, err, "11th request failed")

		if resp.StatusCode != 429 {
			t.Errorf("Expected 429 on 11th request with same login_hint, got %d", resp.StatusCode)
		}

		scope := resp.Headers.Get("X-RateLimit-Scope")
		if scope != "user" {
			t.Errorf("Expected X-RateLimit-Scope=user, got %q", scope)
		}
	})

	t.Run("token endpoint uses stricter IP limit of 20/min", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Token endpoint IP limit is 20/min — send 20 with unique sessions/codes
		for i := 0; i < 20; i++ {
			code := fmt.Sprintf("auth-code-%s-%d", framework.UniqueUserID(), i)
			resp, err := client.Do(framework.Request{
				Method: "POST",
				Path:   "/oauth2/token",
				FormBody: map[string]string{
					"grant_type":   "authorization_code",
					"code":         code,
					"redirect_uri": "http://localhost:8079/callback",
				},
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i+1))
			// Expect 400 (invalid code) but NOT 429
			if resp.StatusCode == 429 {
				t.Fatalf("Request %d was rate limited unexpectedly at token endpoint", i+1)
			}
		}

		// 21st request should be blocked by IP scope at the stricter limit
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/oauth2/token",
			FormBody: map[string]string{
				"grant_type":   "authorization_code",
				"code":         "final-code-" + framework.UniqueUserID(),
				"redirect_uri": "http://localhost:8079/callback",
			},
		})
		framework.AssertNoError(t, err, "21st request failed")

		if resp.StatusCode != 429 {
			t.Errorf("Expected 429 on 21st token request from same IP, got %d", resp.StatusCode)
		}

		scope := resp.Headers.Get("X-RateLimit-Scope")
		if scope != "ip" {
			t.Errorf("Expected X-RateLimit-Scope=ip, got %q", scope)
		}
	})

	t.Run("rate limit headers present on allowed auth flow requests", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method:      "GET",
			Path:        "/oauth2/authorize",
			QueryParams: map[string]string{"state": "header-check-" + framework.UniqueUserID(), "idp": "tmi", "scope": "openid"},
		})
		framework.AssertNoError(t, err, "Request failed")

		// Even on allowed requests, rate limit headers should be present
		if resp.Headers.Get("X-RateLimit-Limit") == "" {
			t.Error("Missing X-RateLimit-Limit header on auth flow response")
		}
		if resp.Headers.Get("X-RateLimit-Remaining") == "" {
			t.Error("Missing X-RateLimit-Remaining header on auth flow response")
		}
		if resp.Headers.Get("X-RateLimit-Reset") == "" {
			t.Error("Missing X-RateLimit-Reset header on auth flow response")
		}
		// X-RateLimit-Scope should NOT be present on allowed requests
		if resp.Headers.Get("X-RateLimit-Scope") != "" {
			t.Errorf("X-RateLimit-Scope should not be present on allowed requests, got %q", resp.Headers.Get("X-RateLimit-Scope"))
		}
	})

	t.Run("corporate NAT scenario: many users same IP not blocked", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Simulate corporate NAT: many different users behind the same IP,
		// each making a few requests. The IP limit is 100/min for non-token endpoints,
		// so 20 users x 3 requests = 60 should all succeed.
		for i := 0; i < 20; i++ {
			loginHint := fmt.Sprintf("corp-user-%d-%s", i, framework.UniqueUserID())
			for j := 0; j < 3; j++ {
				state := fmt.Sprintf("corp-state-%d-%d-%s", i, j, framework.UniqueUserID())
				resp, err := client.Do(framework.Request{
					Method:      "GET",
					Path:        "/oauth2/authorize",
					QueryParams: map[string]string{"state": state, "login_hint": loginHint, "idp": "tmi", "scope": "openid"},
				})
				framework.AssertNoError(t, err, fmt.Sprintf("User %d request %d failed", i, j))
				if resp.StatusCode == 429 {
					t.Fatalf("Corporate NAT user %d request %d was rate limited — legitimate multi-user traffic should not be blocked", i, j)
				}
			}
		}
	})

	t.Run("credential stuffing pattern: many login_hints from same IP get user-scoped blocks", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Simulate credential stuffing: attacker uses many different login_hints
		// Each login_hint is unique, so user scope won't trigger.
		// With 100/min IP limit, the IP scope should eventually block after 100 requests.
		blockedCount := 0
		for i := 0; i < 105; i++ {
			loginHint := fmt.Sprintf("stuffing-victim-%d-%s", i, framework.UniqueUserID())
			state := fmt.Sprintf("stuff-state-%d-%s", i, framework.UniqueUserID())
			resp, err := client.Do(framework.Request{
				Method:      "GET",
				Path:        "/oauth2/authorize",
				QueryParams: map[string]string{"state": state, "login_hint": loginHint, "idp": "tmi", "scope": "openid"},
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i+1))
			if resp.StatusCode == 429 {
				blockedCount++
				// Verify it's the IP scope that blocked
				scope := resp.Headers.Get("X-RateLimit-Scope")
				if scope != "ip" {
					t.Errorf("Expected IP scope block for credential stuffing, got %q", scope)
				}
			}
		}

		// At least some requests should have been blocked by IP scope
		if blockedCount == 0 {
			t.Error("Expected IP-based rate limiting to block credential stuffing pattern after 100 requests, but none were blocked")
		}
	})

	t.Run("non-auth endpoints not affected by auth flow rate limiter", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Public discovery endpoint should not be affected by auth flow limiter
		// (it has its own IP rate limiter at Tier 1, tested separately)
		for i := 0; i < 8; i++ {
			resp, err := client.Do(framework.Request{
				Method: "GET",
				Path:   "/.well-known/openid-configuration",
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i+1))
			// Should get 200 (within IP rate limit of 10/min)
			if resp.StatusCode == 429 {
				t.Fatalf("Public endpoint request %d was unexpectedly rate limited by auth flow limiter", i+1)
			}
		}
	})
}
