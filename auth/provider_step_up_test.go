package auth

import (
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
	got, err := BuildStepUpAuthorizationURL(bp, cfg, "state-123")
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
