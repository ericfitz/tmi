package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// startRedirectTarget returns a server that records whether it was ever
// reached. It stands in for the internal service an attacker would try to
// pivot to via a redirecting provider endpoint.
func startRedirectTarget(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// startRedirector returns a server that answers every request with a 307
// redirect to target. 307 preserves method and body, so a client that
// follows it re-sends the client_secret-bearing POST.
func startRedirector(t *testing.T, target string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func redirectTestConfig(redirectorURL string) OAuthProviderConfig {
	return OAuthProviderConfig{
		ID:               "redirect-test",
		Name:             "Redirect Test",
		ClientID:         "client-id",
		ClientSecret:     "client-secret",
		AuthorizationURL: redirectorURL + "/authorize",
		TokenURL:         redirectorURL + "/token",
		UserInfo:         []UserInfoEndpoint{{URL: redirectorURL + "/userinfo"}},
	}
}

// TestBaseProviderClientRefusesRedirects verifies the provider HTTP client
// refuses redirects rather than forwarding requests to the redirect target
// (f004: SSRF via 307/308 from a hostile or compromised provider endpoint).
func TestBaseProviderClientRefusesRedirects(t *testing.T) {
	target, hits := startRedirectTarget(t)
	redirector := startRedirector(t, target.URL)

	p, err := NewBaseProvider(redirectTestConfig(redirector.URL), "http://localhost/oauth2/callback")
	if err != nil {
		t.Fatalf("NewBaseProvider() error = %v", err)
	}

	resp, err := p.httpClient.Get(redirector.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("client followed redirect (status %d), want refusal", resp.StatusCode)
	}
	if !strings.Contains(err.Error(), "refusing to follow redirect") {
		t.Errorf("error = %q, want redirect-refusal error", err)
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("redirect target reached %d times, want 0", got)
	}
}

// TestStandardExchangeDoesNotUseDefaultClient verifies the standard OAuth2
// exchange path is pinned to the hardened client via oauth2.HTTPClient.
// Before the fix it used http.DefaultClient, which has no timeout and
// follows up to 10 redirects.
func TestStandardExchangeDoesNotUseDefaultClient(t *testing.T) {
	target, hits := startRedirectTarget(t)
	redirector := startRedirector(t, target.URL)

	p, err := NewBaseProvider(redirectTestConfig(redirector.URL), "http://localhost/oauth2/callback")
	if err != nil {
		t.Fatalf("NewBaseProvider() error = %v", err)
	}

	if _, err := p.ExchangeCode(context.Background(), "test-code"); err == nil {
		t.Fatal("ExchangeCode() succeeded via redirecting token endpoint, want error")
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("token POST re-sent to redirect target %d times, want 0", got)
	}
}

// TestCustomTokenExchangeRefusesRedirects covers the AcceptHeader
// (GitHub-style) token path, which POSTs the client_secret using
// BaseProvider.httpClient directly.
func TestCustomTokenExchangeRefusesRedirects(t *testing.T) {
	target, hits := startRedirectTarget(t)
	redirector := startRedirector(t, target.URL)

	cfg := redirectTestConfig(redirector.URL)
	cfg.AcceptHeader = "application/json" // forces customTokenExchange
	p, err := NewBaseProvider(cfg, "http://localhost/oauth2/callback")
	if err != nil {
		t.Fatalf("NewBaseProvider() error = %v", err)
	}

	if _, err := p.ExchangeCode(context.Background(), "test-code"); err == nil {
		t.Fatal("ExchangeCode() succeeded via redirecting token endpoint, want error")
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("client_secret POST re-sent to redirect target %d times, want 0", got)
	}
}

// TestGetUserInfoRefusesRedirects covers the userinfo fetch path.
func TestGetUserInfoRefusesRedirects(t *testing.T) {
	target, hits := startRedirectTarget(t)
	redirector := startRedirector(t, target.URL)

	p, err := NewBaseProvider(redirectTestConfig(redirector.URL), "http://localhost/oauth2/callback")
	if err != nil {
		t.Fatalf("NewBaseProvider() error = %v", err)
	}

	if _, err := p.GetUserInfo(context.Background(), "access-token"); err == nil {
		t.Fatal("GetUserInfo() succeeded via redirecting userinfo endpoint, want error")
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("userinfo request followed redirect %d times, want 0", got)
	}
}

// TestTestProviderClientRefusesRedirects covers the TMI internal provider,
// whose http.Client is constructed separately from BaseProvider's.
func TestTestProviderClientRefusesRedirects(t *testing.T) {
	target, hits := startRedirectTarget(t)
	redirector := startRedirector(t, target.URL)

	p := NewTestProvider(redirectTestConfig(redirector.URL), "http://localhost/oauth2/callback")
	resp, err := p.httpClient.Get(redirector.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("client followed redirect (status %d), want refusal", resp.StatusCode)
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("redirect target reached %d times, want 0", got)
	}
}

// TestDiscoveryClientRefusesRedirects: a redirecting issuer is classified as
// not-OIDC (nil doc, nil error) instead of being followed.
func TestDiscoveryClientRefusesRedirects(t *testing.T) {
	target, hits := startRedirectTarget(t)
	redirector := startRedirector(t, target.URL)

	c := NewDiscoveryClient(2*time.Second, time.Hour)
	doc, err := c.Discover(context.Background(), redirector.URL)
	if err != nil {
		t.Fatalf("Discover() error = %v, want nil (redirect is a not-OIDC condition)", err)
	}
	if doc != nil {
		t.Errorf("Discover() doc = %+v, want nil for redirecting issuer", doc)
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("discovery followed redirect %d times, want 0", got)
	}
}
