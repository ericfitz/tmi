package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestBaseProviderRefusesInternalTokenURL verifies the hardened provider client
// blocks an admin-set internal token_url at dial time (residual 200-response
// SSRF from #470 — the redirect-only fix in #466 did not cover a direct request
// to an internal address). The test seam (ssrf_test_seam_test.go) allows only
// loopback, so these private/link-local/metadata literals stay blocked.
func TestBaseProviderRefusesInternalTokenURL(t *testing.T) {
	cases := map[string]string{
		"http://10.0.0.1/token":        "private",
		"http://192.168.1.1/token":     "private",
		"http://169.254.169.254/token": "cloud metadata",
		"http://169.254.1.1/token":     "link-local",
	}
	for tokenURL, want := range cases {
		cfg := OAuthProviderConfig{
			ID:           "evil",
			Name:         "Evil",
			ClientID:     "id",
			ClientSecret: "secret",
			TokenURL:     tokenURL,
			AcceptHeader: "application/json", // route ExchangeCode -> customTokenExchange
		}
		p, err := NewBaseProvider(cfg, "http://example.com/callback")
		if err != nil {
			t.Fatalf("NewBaseProvider(%s) error = %v", tokenURL, err)
		}
		_, err = p.customTokenExchange(context.Background(), "code-123")
		if err == nil {
			t.Errorf("customTokenExchange(%s) = nil error, want %q block", tokenURL, want)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("customTokenExchange(%s) error = %q, want substring %q", tokenURL, err, want)
		}
	}
}

// TestBaseProviderRefusesInternalUserInfoURL verifies the userinfo fetch path
// is likewise pinned and refuses an internal endpoint at dial time.
func TestBaseProviderRefusesInternalUserInfoURL(t *testing.T) {
	cfg := OAuthProviderConfig{
		ID:       "evil",
		Name:     "Evil",
		ClientID: "id",
		UserInfo: []UserInfoEndpoint{{URL: "http://169.254.169.254/latest/meta-data/"}},
	}
	p, err := NewBaseProvider(cfg, "http://example.com/callback")
	if err != nil {
		t.Fatalf("NewBaseProvider error = %v", err)
	}
	_, err = p.GetUserInfo(context.Background(), "access-token")
	if err == nil || !strings.Contains(err.Error(), "cloud metadata") {
		t.Fatalf("GetUserInfo(metadata endpoint) = %v, want cloud-metadata block", err)
	}
}

// TestDiscoveryClientDoesNotReachInternalIssuer verifies the OIDC discovery
// client does not fetch from an internal issuer URL: the dial is blocked, so
// the issuer is classified "not OIDC" (nil doc) rather than fetched.
func TestDiscoveryClientDoesNotReachInternalIssuer(t *testing.T) {
	dc := NewDiscoveryClient(2*time.Second, time.Minute)
	doc, err := dc.Discover(context.Background(), "http://169.254.169.254")
	if err != nil {
		t.Fatalf("Discover() error = %v, want nil (treated as not-OIDC)", err)
	}
	if doc != nil {
		t.Fatalf("Discover() = %+v, want nil doc (internal issuer must not be fetched)", doc)
	}
}
