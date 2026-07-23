package config

import (
	"reflect"
	"testing"
)

// TestOverrideOAuthProviders_ParsesClaims is the regression test for #558:
// env-var provider config must populate userinfo claim mappings (and the
// secondary endpoint, auth header, and accept header), or providers classified
// NonOIDC / OIDCCustomUserinfo fail startup validation for a missing
// subject_claim.
func TestOverrideOAuthProviders_ParsesClaims(t *testing.T) {
	p := "OAUTH_PROVIDERS_ENVCLAIMTEST_"
	t.Setenv(p+"ENABLED", "true")
	t.Setenv(p+"NAME", "EnvClaimTest")
	t.Setenv(p+"CLIENT_ID", "cid")
	t.Setenv(p+"CLIENT_SECRET", "secret")
	t.Setenv(p+"USERINFO_URL", "https://api.example.com/user")
	t.Setenv(p+"USERINFO_CLAIMS_SUBJECT_CLAIM", "id")
	t.Setenv(p+"USERINFO_CLAIMS_NAME_CLAIM", "name")
	t.Setenv(p+"USERINFO_SECONDARY_URL", "https://api.example.com/user/emails")
	t.Setenv(p+"USERINFO_SECONDARY_CLAIMS_EMAIL_CLAIM", "[0].email")
	t.Setenv(p+"AUTH_HEADER_FORMAT", "token %s")
	t.Setenv(p+"ACCEPT_HEADER", "application/vnd.github+json")

	providers := map[string]OAuthProviderConfig{}
	if err := overrideOAuthProviders(reflect.ValueOf(providers)); err != nil {
		t.Fatalf("overrideOAuthProviders: %v", err)
	}

	got, ok := providers["envclaimtest"]
	if !ok {
		t.Fatalf("provider not loaded; got keys %v", reflect.ValueOf(providers).MapKeys())
	}
	if len(got.UserInfo) != 2 {
		t.Fatalf("want 2 userinfo endpoints (primary+secondary), got %d", len(got.UserInfo))
	}
	// Primary endpoint carries subject + name claims.
	if got.UserInfo[0].Claims["subject_claim"] != "id" {
		t.Errorf("primary subject_claim = %q, want %q", got.UserInfo[0].Claims["subject_claim"], "id")
	}
	if got.UserInfo[0].Claims["name_claim"] != "name" {
		t.Errorf("primary name_claim = %q, want %q", got.UserInfo[0].Claims["name_claim"], "name")
	}
	// Secondary endpoint (GitHub-style email endpoint) carries its own claim.
	if got.UserInfo[1].URL != "https://api.example.com/user/emails" {
		t.Errorf("secondary URL = %q", got.UserInfo[1].URL)
	}
	if got.UserInfo[1].Claims["email_claim"] != "[0].email" {
		t.Errorf("secondary email_claim = %q, want %q", got.UserInfo[1].Claims["email_claim"], "[0].email")
	}
	if got.AuthHeaderFormat != "token %s" {
		t.Errorf("AuthHeaderFormat = %q", got.AuthHeaderFormat)
	}
	if got.AcceptHeader != "application/vnd.github+json" {
		t.Errorf("AcceptHeader = %q", got.AcceptHeader)
	}
}
