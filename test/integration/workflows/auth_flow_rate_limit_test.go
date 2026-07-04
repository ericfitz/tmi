package workflows

import (
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// authFlowLimit is the per-session and per-IP auth-flow rate limit (requests /
// 60s) enforced by api/auth_flow_rate_limiter.go. The token endpoint uses the
// same per-IP limit. Keep this in sync with the implementation
// (authFlowSessionLimit / authFlowDefaultIPLimit in checkRateLimitWithIPLimit).
const authFlowLimit = 100

// authFlowUserLimit is the per-user-identifier auth-flow rate limit (requests /
// 60s). It is deliberately lower than authFlowLimit so the user scope is
// independently enforceable for single-account attacks (issue #506). Keep this
// in sync with authFlowUserLimit in api/auth_flow_rate_limiter.go.
const authFlowUserLimit = 50

// uniqueAuthFlowIP returns a distinct non-loopback test IP for index i. The
// integration server runs with no trusted proxies, so it honors X-Forwarded-For
// (extractIPAddress's manual path) — letting a test vary the perceived client IP
// per request to isolate the IP scope from the session/user scopes, exactly as
// the unit tests in api/rate_limit_middleware_test.go do by passing a fresh IP.
// 198.51.100.0/24 is TEST-NET-2 (RFC 5737), reserved for documentation/tests.
func uniqueAuthFlowIP(i int) string {
	return fmt.Sprintf("198.51.100.%d", i%254+1)
}

// authFlowAuthorize issues a GET /oauth2/authorize with the given session state,
// optional login_hint, and a spoofed client IP via X-Forwarded-For.
func authFlowAuthorize(t *testing.T, c *framework.IntegrationClient, state, loginHint, xff string) *framework.Response {
	t.Helper()
	qp := map[string]string{"state": state, "idp": "tmi", "scope": "openid"}
	if loginHint != "" {
		qp["login_hint"] = loginHint
	}
	resp, err := c.Do(framework.Request{
		Method:      "GET",
		Path:        "/oauth2/authorize",
		QueryParams: qp,
		Headers:     map[string]string{"X-Forwarded-For": xff},
	})
	framework.AssertNoError(t, err, "authorize request failed")
	return resp
}

// authFlowToken issues a POST /oauth2/token (authorization_code grant) with a
// spoofed client IP. The code is bogus, so a non-rate-limited response is the
// 400 invalid_grant path; the test only distinguishes 429 from non-429.
func authFlowToken(t *testing.T, c *framework.IntegrationClient, code, xff string) *framework.Response {
	t.Helper()
	resp, err := c.Do(framework.Request{
		Method: "POST",
		Path:   "/oauth2/token",
		FormBody: map[string]string{
			"grant_type":   "authorization_code",
			"code":         code,
			"redirect_uri": "http://localhost:8079/callback",
		},
		Headers: map[string]string{"X-Forwarded-For": xff},
	})
	framework.AssertNoError(t, err, "token request failed")
	return resp
}

// TestAuthFlowRateLimiting_MultiScope verifies multi-scope rate limiting on
// OAuth/SAML auth flow endpoints (Tier 2): session, IP, and user scopes each
// returning 429 with the correct X-RateLimit-Scope, plus Retry-After.
//
// The auth-flow limiter no-ops in build_mode=test (auth_flow_rate_limiter.go),
// and the integration test server runs build_mode=test for the built-in tmi
// OAuth provider every other workflow test depends on. The harness therefore
// runs this only when rate-limit coverage is explicitly requested
// (TMI_TEST_ENABLE_RATE_LIMITING=true, which makes scripts/run-integration-tests.py
// set the server-side TMI_TEST_FORCE_AUTH_FLOW_RATE_LIMITING override). In any
// other configuration the limiter is disabled and this test SKIPS cleanly via
// IsAuthFlowRateLimitingActive (a present-but-zero X-RateLimit-Limit means "not
// enforcing"); the limiter's logic is also covered by unit tests in
// api/rate_limit_middleware_test.go.
//
// Each sub-test isolates one scope by varying the other two dimensions so only
// the targeted counter accumulates (the same technique the unit tests use):
//   - session: same state, unique IP per request, no login_hint
//   - user:    same login_hint, unique state + unique IP per request
//   - ip:      fixed IP, unique state + unique login_hint per request
func TestAuthFlowRateLimiting_MultiScope(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if !framework.IsAuthFlowRateLimitingActive(serverURL) {
		t.Skip("Auth-flow rate limiter is not enforcing (disabled via disable_rate_limiting or the build_mode=test no-op — enable with TMI_TEST_ENABLE_RATE_LIMITING=true); covered by unit tests in api/rate_limit_middleware_test.go")
	}

	t.Run("session scope blocks after the limit with the same state", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Same session state, a fresh IP per request so only the session scope
		// accumulates. The first authFlowLimit requests must all be allowed.
		state := "session-state-" + framework.UniqueUserID()
		for i := 0; i < authFlowLimit; i++ {
			resp := authFlowAuthorize(t, client, state, "", uniqueAuthFlowIP(i))
			if resp.StatusCode == 429 {
				t.Fatalf("Request %d was rate limited before the limit of %d", i+1, authFlowLimit)
			}
		}

		// One more request with the same state should trip the session scope.
		resp := authFlowAuthorize(t, client, state, "", uniqueAuthFlowIP(authFlowLimit))
		if resp.StatusCode != 429 {
			t.Errorf("Expected 429 on request %d with the same state, got %d", authFlowLimit+1, resp.StatusCode)
		}
		if scope := resp.Headers.Get("X-RateLimit-Scope"); scope != "session" {
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

		// Exhaust session A (unique IP per request keeps the IP scope out of it).
		sessionA := "session-a-" + framework.UniqueUserID()
		for i := 0; i <= authFlowLimit; i++ {
			_ = authFlowAuthorize(t, client, sessionA, "", uniqueAuthFlowIP(i))
		}

		// Session B (a different state, its own fresh IP) must still be allowed.
		sessionB := "session-b-" + framework.UniqueUserID()
		resp := authFlowAuthorize(t, client, sessionB, "", uniqueAuthFlowIP(authFlowLimit+10))
		if resp.StatusCode == 429 {
			t.Error("Session B was rate limited — sessions should be independent")
		}
	})

	t.Run("user identifier scope blocks after the limit with the same login_hint", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Same login_hint, but a unique state AND a unique IP per request so
		// neither the session nor the IP scope accumulates — only the user scope,
		// which enforces the lower authFlowUserLimit (issue #506).
		loginHint := "ratelimit-user-" + framework.UniqueUserID()
		for i := 0; i < authFlowUserLimit; i++ {
			state := fmt.Sprintf("user-state-%s-%d", framework.UniqueUserID(), i)
			resp := authFlowAuthorize(t, client, state, loginHint, uniqueAuthFlowIP(i))
			if resp.StatusCode == 429 {
				t.Fatalf("Request %d was rate limited before the limit of %d", i+1, authFlowUserLimit)
			}
		}

		resp := authFlowAuthorize(t, client, "user-final-"+framework.UniqueUserID(), loginHint, uniqueAuthFlowIP(authFlowUserLimit))
		if resp.StatusCode != 429 {
			t.Errorf("Expected 429 on request %d with the same login_hint, got %d", authFlowUserLimit+1, resp.StatusCode)
		}
		if scope := resp.Headers.Get("X-RateLimit-Scope"); scope != "user" {
			t.Errorf("Expected X-RateLimit-Scope=user, got %q", scope)
		}
		if resp.Headers.Get("Retry-After") == "" {
			t.Error("Missing Retry-After header on 429 response")
		}
	})

	t.Run("IP scope blocks after the limit from the same address", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Fixed IP, but a unique state AND unique login_hint per request so only
		// the IP scope accumulates.
		ip := "203.0.113.10"
		for i := 0; i < authFlowLimit; i++ {
			state := fmt.Sprintf("ip-state-%s-%d", framework.UniqueUserID(), i)
			hint := fmt.Sprintf("ip-user-%s-%d", framework.UniqueUserID(), i)
			resp := authFlowAuthorize(t, client, state, hint, ip)
			if resp.StatusCode == 429 {
				t.Fatalf("Request %d was rate limited before the limit of %d", i+1, authFlowLimit)
			}
		}

		resp := authFlowAuthorize(t, client, "ip-final-"+framework.UniqueUserID(), "ip-final-"+framework.UniqueUserID(), ip)
		if resp.StatusCode != 429 {
			t.Errorf("Expected 429 on request %d from the same IP, got %d", authFlowLimit+1, resp.StatusCode)
		}
		if scope := resp.Headers.Get("X-RateLimit-Scope"); scope != "ip" {
			t.Errorf("Expected X-RateLimit-Scope=ip, got %q", scope)
		}
		if resp.Headers.Get("Retry-After") == "" {
			t.Error("Missing Retry-After header on 429 response")
		}
	})

	t.Run("token endpoint enforces the per-IP limit", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Fixed IP, unique code per request (so the session scope, keyed on the
		// code, never accumulates) — only the IP scope should trip.
		ip := "203.0.113.20"
		for i := 0; i < authFlowLimit; i++ {
			code := fmt.Sprintf("auth-code-%s-%d", framework.UniqueUserID(), i)
			resp := authFlowToken(t, client, code, ip)
			if resp.StatusCode == 429 {
				t.Fatalf("Token request %d was rate limited before the limit of %d", i+1, authFlowLimit)
			}
		}

		resp := authFlowToken(t, client, "final-code-"+framework.UniqueUserID(), ip)
		if resp.StatusCode != 429 {
			t.Errorf("Expected 429 on token request %d from the same IP, got %d", authFlowLimit+1, resp.StatusCode)
		}
		if scope := resp.Headers.Get("X-RateLimit-Scope"); scope != "ip" {
			t.Errorf("Expected X-RateLimit-Scope=ip, got %q", scope)
		}
	})

	t.Run("rate limit headers present on allowed auth flow requests", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		resp := authFlowAuthorize(t, client, "header-check-"+framework.UniqueUserID(), "", "203.0.113.30")

		// Even on allowed requests, rate limit headers should be present and the
		// advertised limit should match the implemented per-scope limit.
		if got := resp.Headers.Get("X-RateLimit-Limit"); got != fmt.Sprintf("%d", authFlowLimit) {
			t.Errorf("Expected X-RateLimit-Limit=%d on allowed auth flow response, got %q", authFlowLimit, got)
		}
		if resp.Headers.Get("X-RateLimit-Remaining") == "" {
			t.Error("Missing X-RateLimit-Remaining header on auth flow response")
		}
		if resp.Headers.Get("X-RateLimit-Reset") == "" {
			t.Error("Missing X-RateLimit-Reset header on auth flow response")
		}
		// X-RateLimit-Scope should NOT be present on allowed requests.
		if scope := resp.Headers.Get("X-RateLimit-Scope"); scope != "" {
			t.Errorf("X-RateLimit-Scope should not be present on allowed requests, got %q", scope)
		}
	})

	t.Run("corporate NAT scenario: many users same IP under the limit not blocked", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Many distinct users behind a single IP, kept under the IP limit in
		// aggregate (20 users x 3 requests = 60 < 100) — all should succeed.
		ip := "203.0.113.40"
		for i := 0; i < 20; i++ {
			loginHint := fmt.Sprintf("corp-user-%d-%s", i, framework.UniqueUserID())
			for j := 0; j < 3; j++ {
				state := fmt.Sprintf("corp-state-%d-%d-%s", i, j, framework.UniqueUserID())
				resp := authFlowAuthorize(t, client, state, loginHint, ip)
				if resp.StatusCode == 429 {
					t.Fatalf("Corporate NAT user %d request %d was rate limited — legitimate multi-user traffic under the limit should not be blocked", i, j)
				}
			}
		}
	})

	t.Run("credential stuffing pattern: many login_hints from same IP get IP-scoped blocks", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Each login_hint and state is unique, so neither the user nor the
		// session scope triggers; the shared IP eventually trips the IP scope.
		ip := "203.0.113.50"
		blockedByIP := 0
		for i := 0; i < authFlowLimit+5; i++ {
			loginHint := fmt.Sprintf("stuffing-victim-%d-%s", i, framework.UniqueUserID())
			state := fmt.Sprintf("stuff-state-%d-%s", i, framework.UniqueUserID())
			resp := authFlowAuthorize(t, client, state, loginHint, ip)
			if resp.StatusCode == 429 {
				if scope := resp.Headers.Get("X-RateLimit-Scope"); scope != "ip" {
					t.Errorf("Expected IP scope block for credential stuffing, got %q", scope)
				}
				blockedByIP++
			}
		}
		if blockedByIP == 0 {
			t.Errorf("Expected IP-based rate limiting to block credential stuffing after %d requests, but none were blocked", authFlowLimit)
		}
	})

	t.Run("non-auth endpoints not affected by auth flow rate limiter", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// The discovery endpoint is not an auth-flow endpoint, so the auth-flow
		// limiter must not touch it. A handful of requests stays under the
		// endpoint's own (Tier 1) IP limit, tested separately.
		for i := 0; i < 5; i++ {
			resp, err := client.Do(framework.Request{
				Method:  "GET",
				Path:    "/.well-known/openid-configuration",
				Headers: map[string]string{"X-Forwarded-For": "203.0.113.60"},
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i+1))
			if resp.StatusCode == 429 {
				t.Fatalf("Public endpoint request %d was unexpectedly rate limited", i+1)
			}
		}
	})
}
