package workflows

import (
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestIPRateLimiting_PublicEndpoints verifies IP-based rate limiting
// on public discovery endpoints (Tier 1).
// Requires: running TMI server + Redis (via make start-dev) AND rate
// limiting enabled at the server. The dev server is commonly started with
// disable_rate_limiting: true, and the IP limiter additionally bypasses
// loopback addresses (so localhost-originating requests never count). In
// either case this test skips — ip_rate_limiter unit tests in api/ cover
// the limiter itself.
func TestIPRateLimiting_PublicEndpoints(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if !framework.IsRateLimitingActive(serverURL) {
		t.Skip("Rate limiting is not active on the server (disable_rate_limiting, build_mode=test, or loopback bypass); skipping IP rate-limit assertions")
	}

	// Clear rate limit keys before tests to avoid pollution
	_ = framework.ClearRateLimits()

	t.Run("rate limits public discovery endpoints", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Default limit is 10 req/min — send 10 requests, all should succeed
		for i := 0; i < 10; i++ {
			resp, err := client.Do(framework.Request{
				Method: "GET",
				Path:   "/.well-known/openid-configuration",
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i+1))
			if resp.StatusCode == 429 {
				t.Fatalf("Request %d was rate limited unexpectedly", i+1)
			}
		}

		// 11th request should be rate limited
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/.well-known/openid-configuration",
		})
		framework.AssertNoError(t, err, "11th request failed")

		if resp.StatusCode != 429 {
			t.Errorf("Expected 429 on 11th request, got %d", resp.StatusCode)
		}

		// Verify rate limit headers
		if resp.Headers.Get("X-RateLimit-Limit") == "" {
			t.Error("Missing X-RateLimit-Limit header")
		}
		if resp.Headers.Get("X-RateLimit-Remaining") == "" {
			t.Error("Missing X-RateLimit-Remaining header")
		}
		if resp.Headers.Get("X-RateLimit-Reset") == "" {
			t.Error("Missing X-RateLimit-Reset header")
		}
		if resp.Headers.Get("Retry-After") == "" {
			t.Error("Missing Retry-After header on 429 response")
		}
	})

	t.Run("health check endpoint excluded from rate limiting", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Send 15 requests to GET / (more than the 10/min limit)
		for i := 0; i < 15; i++ {
			resp, err := client.Do(framework.Request{
				Method: "GET",
				Path:   "/",
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i+1))
			if resp.StatusCode == 429 {
				t.Fatalf("Health check request %d was rate limited — GET / should be excluded", i+1)
			}
		}
	})

	t.Run("rate limit headers present on successful responses", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/.well-known/openid-configuration",
		})
		framework.AssertNoError(t, err, "Request failed")

		if resp.StatusCode != 200 {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		if resp.Headers.Get("X-RateLimit-Limit") == "" {
			t.Error("Missing X-RateLimit-Limit header on 200 response")
		}
		if resp.Headers.Get("X-RateLimit-Remaining") == "" {
			t.Error("Missing X-RateLimit-Remaining header on 200 response")
		}
		if resp.Headers.Get("X-RateLimit-Reset") == "" {
			t.Error("Missing X-RateLimit-Reset header on 200 response")
		}
	})

	t.Run("different IPs are rate limited independently", func(t *testing.T) {
		_ = framework.ClearRateLimits()

		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Exhaust limit for IP "198.51.100.1"
		for i := 0; i < 11; i++ {
			resp, err := client.Do(framework.Request{
				Method:  "GET",
				Path:    "/.well-known/openid-configuration",
				Headers: map[string]string{"X-Forwarded-For": "198.51.100.1"},
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i+1))
			_ = resp
		}

		// Request from different IP "198.51.100.2" should still succeed
		resp, err := client.Do(framework.Request{
			Method:  "GET",
			Path:    "/.well-known/openid-configuration",
			Headers: map[string]string{"X-Forwarded-For": "198.51.100.2"},
		})
		framework.AssertNoError(t, err, "Request from different IP failed")
		if resp.StatusCode == 429 {
			t.Error("Request from different IP was rate limited — IPs should have independent counters")
		}
	})

	t.Run("non-public endpoints not affected by IP rate limiter", func(t *testing.T) {
		_ = framework.ClearRateLimits()

		if err := framework.EnsureOAuthStubRunning(); err != nil {
			t.Skipf("OAuth stub not running, skipping: %v", err)
		}
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Failed to authenticate user")

		client, err := framework.NewClient(serverURL, tokens, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create authenticated client")

		// Send 15 requests to an authenticated endpoint (exceeds IP limit of 10)
		for i := 0; i < 15; i++ {
			resp, err := client.Do(framework.Request{
				Method: "GET",
				Path:   "/threat_models",
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i+1))
			if resp.StatusCode == 429 {
				t.Fatalf("Authenticated endpoint request %d was IP rate limited — only public endpoints should be affected", i+1)
			}
		}
	})
}
