package auth

import (
	"net/url"
	"strings"
	"testing"
)

func TestClassifyStepUpStrength_KnownProviders(t *testing.T) {
	cases := []struct {
		providerID string
		issuer     string
		jwksURL    string
		want       StepUpStrength
	}{
		{"google", "https://accounts.google.com", "https://www.googleapis.com/oauth2/v3/certs", StepUpStrong},
		{"microsoft", "https://login.microsoftonline.com/common/v2.0", "https://login.microsoftonline.com/common/discovery/v2.0/keys", StepUpStrong},
		{"tmi", "", "", StepUpStrong}, // dev provider; controlled by us, treat as strong
		{"github", "", "", StepUpWeak},
		{"someenterprise-oidc", "https://idp.example.com", "https://idp.example.com/jwks", StepUpStrong},
		{"someenterprise-oauth", "", "", StepUpWeak},
	}
	for _, tc := range cases {
		t.Run(tc.providerID, func(t *testing.T) {
			cfg := OAuthProviderConfig{ID: tc.providerID, Issuer: tc.issuer, JWKSURL: tc.jwksURL}
			got := ClassifyStepUpStrength(cfg)
			if got != tc.want {
				t.Fatalf("ClassifyStepUpStrength(%q) = %v, want %v", tc.providerID, got, tc.want)
			}
		})
	}
}

func TestBuildStepUpAuthorizationURL_OAuthAppendsPromptAndMaxAge(t *testing.T) {
	cfg := OAuthProviderConfig{
		ID:               "google",
		ClientID:         "test-client",
		AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:         "https://oauth2.googleapis.com/token",
		Scopes:           []string{"openid", "email"},
	}
	bp, err := NewBaseProvider(cfg, "http://localhost:8080/oauth2/callback")
	if err != nil {
		t.Fatalf("NewBaseProvider: %v", err)
	}
	got, err := BuildStepUpAuthorizationURL(bp, cfg, "state-123", "")
	if err != nil {
		t.Fatalf("BuildStepUpAuthorizationURL: %v", err)
	}
	if !strings.Contains(got, "prompt=login") {
		t.Errorf("step-up URL missing prompt=login: %s", got)
	}
	if !strings.Contains(got, "max_age=0") {
		t.Errorf("step-up URL missing max_age=0: %s", got)
	}
	if !strings.Contains(got, "state=state-123") {
		t.Errorf("step-up URL missing state: %s", got)
	}
}

func TestStepUpStrength_String(t *testing.T) {
	cases := []struct {
		in   StepUpStrength
		want string
	}{
		{StepUpStrong, "strong"},
		{StepUpWeak, "weak"},
		{StepUpStrength(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("StepUpStrength(%d).String() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestBuildStepUpAuthorizationURL_ForwardsLoginHint pins #817: the hint that
// names the account to re-authenticate must reach the upstream authorize URL.
func TestBuildStepUpAuthorizationURL_ForwardsLoginHint(t *testing.T) {
	cfg := OAuthProviderConfig{
		ID:               "google",
		ClientID:         "test-client",
		AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:         "https://oauth2.googleapis.com/token",
		Scopes:           []string{"openid", "email"},
	}
	bp, err := NewBaseProvider(cfg, "http://localhost:8080/oauth2/callback")
	if err != nil {
		t.Fatalf("NewBaseProvider: %v", err)
	}
	got, err := BuildStepUpAuthorizationURL(bp, cfg, "state-123", "charlie@example.com")
	if err != nil {
		t.Fatalf("BuildStepUpAuthorizationURL: %v", err)
	}
	if !strings.Contains(got, "login_hint=charlie%40example.com") {
		t.Errorf("step-up URL missing login_hint: %s", got)
	}
}

// TestBuildStepUpAuthorizationURL_TestProviderEncodesHintInCode pins the
// tmi-provider half of #817: the identity travels in the fake authorization
// code (the only place ExchangeCode looks), and the address form of a test
// identity resolves to the same user as the bare username.
func TestBuildStepUpAuthorizationURL_TestProviderEncodesHintInCode(t *testing.T) {
	cfg := OAuthProviderConfig{ID: "tmi", ClientID: "tmi"}
	tp := NewTestProvider(cfg, "http://localhost:8080/oauth2/callback")
	for _, hint := range []string{"charlie", "charlie@tmi.local", "Charlie@TMI.local"} {
		got, err := BuildStepUpAuthorizationURL(tp, cfg, "state-123", hint)
		if err != nil {
			t.Fatalf("BuildStepUpAuthorizationURL(%q): %v", hint, err)
		}
		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("parse %q: %v", got, err)
		}
		if h := tp.extractUserHintFromAuthCode(u.Query().Get("code")); h != "charlie" {
			t.Errorf("hint %q: auth code carries %q, want charlie (url=%s)", hint, h, got)
		}
	}
}
