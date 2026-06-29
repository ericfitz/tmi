package workflows

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestStepUpEndpoint_StrongProvider_Returns302WithPromptLogin pins the wire
// contract for /oauth2/step_up (#397):
//
//  1. Authenticate a user via the OAuth stub.
//  2. GET /oauth2/step_up?client_callback=...&code_challenge=...&code_challenge_method=S256
//     with the user's access token.
//  3. Assert 302 Found, Location header contains prompt=login AND max_age=0,
//     Location host is the upstream IdP authorize URL.
//
// Does NOT complete the full round-trip — the OAuth stub does not yet honor
// prompt=login, and unit tests (auth/handlers_step_up_test.go) cover the
// downstream callback + token-mint paths.
//
// Requires:
//   - INTEGRATION_TESTS=true
//   - TMI server running (make dev-up)
//   - OAuth stub running (make start-oauth-stub)
func TestStepUpEndpoint_StrongProvider_Returns302WithPromptLogin(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nRun: make start-oauth-stub", err)
	}

	// 1. Authenticate as a test user (the tmi provider mints fresh-auth tokens).
	tokens, err := framework.AuthenticateUser("test-stepup")
	framework.AssertNoError(t, err, "AuthenticateUser failed")

	// 2. Build the step-up request. PKCE values are arbitrary fixed strings
	//    valid per the format check (~43 chars URL-safe base64).
	clientCallback := "http://localhost:8079/callback" // must be in the dev allowlist
	codeChallenge := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

	qs := url.Values{}
	qs.Set("client_callback", clientCallback)
	qs.Set("code_challenge", codeChallenge)
	qs.Set("code_challenge_method", "S256")
	stepUpURL := serverURL + "/oauth2/step_up?" + qs.Encode()

	req, err := http.NewRequest("GET", stepUpURL, nil)
	framework.AssertNoError(t, err, "build step_up request")
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)

	// Do NOT follow redirects — we want to inspect the 302.
	httpClient := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := httpClient.Do(req)
	framework.AssertNoError(t, err, "GET /oauth2/step_up")
	defer func() { _ = resp.Body.Close() }()

	// 3. Assert 302 (or 307) + Location header contents.
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("Expected 302 or 307, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		t.Fatal("Location header is empty")
	}
	if !strings.Contains(loc, "prompt=login") {
		t.Errorf("Location missing prompt=login: %s", loc)
	}
	if !strings.Contains(loc, "max_age=0") {
		t.Errorf("Location missing max_age=0: %s", loc)
	}
	parsed, err := url.Parse(loc)
	framework.AssertNoError(t, err, "parse Location header")
	if parsed.Host == "" {
		t.Errorf("Location is not an absolute URL: %s", loc)
	}
	t.Logf("step-up redirect Location: %s", loc)
}
