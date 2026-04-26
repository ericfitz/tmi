package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func startDiscoveryServer(t *testing.T, userinfoEndpoint string) *httptest.Server {
	t.Helper()
	body := fmt.Sprintf(`{"issuer":"%%s","authorization_endpoint":"a","token_endpoint":"t","jwks_uri":"j","userinfo_endpoint":%q,"subject_types_supported":["public"],"response_types_supported":["code"]}`, userinfoEndpoint)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, body, "https://issuer.example")
	}))
	return srv
}

func TestClassifyProvider_OIDCCompliant(t *testing.T) {
	srv := startDiscoveryServer(t, "https://issuer.example/userinfo")
	defer srv.Close()

	client := NewDiscoveryClient(2*time.Second, 1*time.Hour)
	cfg := OAuthProviderConfig{
		Issuer:   srv.URL,
		UserInfo: []UserInfoEndpoint{{URL: "https://issuer.example/userinfo"}},
	}
	got := ClassifyProvider(context.Background(), client, "google", cfg)
	if got.Classification != ClassificationOIDCCompliant {
		t.Errorf("classification = %v, want OIDCCompliant", got.Classification)
	}
}

func TestClassifyProvider_OIDCCustomUserinfo(t *testing.T) {
	srv := startDiscoveryServer(t, "https://issuer.example/userinfo")
	defer srv.Close()

	client := NewDiscoveryClient(2*time.Second, 1*time.Hour)
	cfg := OAuthProviderConfig{
		Issuer:   srv.URL,
		UserInfo: []UserInfoEndpoint{{URL: "https://graph.microsoft.com/v1.0/me"}},
	}
	got := ClassifyProvider(context.Background(), client, "microsoft", cfg)
	if got.Classification != ClassificationOIDCCustomUserinfo {
		t.Errorf("classification = %v, want OIDCCustomUserinfo", got.Classification)
	}
}

func TestClassifyProvider_NonOIDC_NoIssuer(t *testing.T) {
	client := NewDiscoveryClient(2*time.Second, 1*time.Hour)
	cfg := OAuthProviderConfig{
		Issuer:   "",
		UserInfo: []UserInfoEndpoint{{URL: "https://api.github.com/user"}},
	}
	got := ClassifyProvider(context.Background(), client, "github", cfg)
	if got.Classification != ClassificationNonOIDC {
		t.Errorf("classification = %v, want NonOIDC", got.Classification)
	}
}

func TestClassifyProvider_OIDCCompliant_TrailingSlash(t *testing.T) {
	// The discovery doc returns a URL without a trailing slash, but the config
	// was written with one. Raw equality would make this OIDCCustomUserinfo;
	// canonicalization should make it OIDCCompliant.
	srv := startDiscoveryServer(t, "https://issuer.example/userinfo")
	defer srv.Close()

	client := NewDiscoveryClient(2*time.Second, 1*time.Hour)
	cfg := OAuthProviderConfig{
		Issuer:   srv.URL,
		UserInfo: []UserInfoEndpoint{{URL: "https://issuer.example/userinfo/"}},
	}
	got := ClassifyProvider(context.Background(), client, "testprovider", cfg)
	if got.Classification != ClassificationOIDCCompliant {
		t.Errorf("classification = %v, want OIDCCompliant (canonicalization should match trailing-slash URL)", got.Classification)
	}
}

func TestClassifyProvider_NonOIDC_DiscoveryFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewDiscoveryClient(2*time.Second, 1*time.Hour)
	cfg := OAuthProviderConfig{
		Issuer:   srv.URL,
		UserInfo: []UserInfoEndpoint{{URL: "https://example.com/user"}},
	}
	got := ClassifyProvider(context.Background(), client, "weird", cfg)
	if got.Classification != ClassificationNonOIDC {
		t.Errorf("classification = %v, want NonOIDC", got.Classification)
	}
}

func TestDetectDiscoveryDocDrift(t *testing.T) {
	tests := []struct {
		name      string
		cfg       OAuthProviderConfig
		doc       *OIDCDiscoveryDoc
		wantWarns int
		wantText  []string // substrings each warning must contain
	}{
		{
			name:      "nil doc produces no warnings",
			cfg:       OAuthProviderConfig{Issuer: "https://issuer.example"},
			doc:       nil,
			wantWarns: 0,
		},
		{
			name: "matching issuer and jwks: no warnings",
			cfg: OAuthProviderConfig{
				Issuer:  "https://issuer.example",
				JWKSURL: "https://issuer.example/jwks",
			},
			doc: &OIDCDiscoveryDoc{
				Issuer:  "https://issuer.example",
				JWKSURI: "https://issuer.example/jwks",
			},
			wantWarns: 0,
		},
		{
			name: "issuer mismatch: 1 warning naming both values",
			cfg: OAuthProviderConfig{
				Issuer:  "https://issuer.example",
				JWKSURL: "https://issuer.example/jwks",
			},
			doc: &OIDCDiscoveryDoc{
				Issuer:  "https://other-tenant.example",
				JWKSURI: "https://issuer.example/jwks",
			},
			wantWarns: 1,
			wantText:  []string{"issuer mismatch", "https://issuer.example", "https://other-tenant.example", "§4.3"},
		},
		{
			name: "jwks_uri mismatch: 1 warning naming both values",
			cfg: OAuthProviderConfig{
				Issuer:  "https://issuer.example",
				JWKSURL: "https://issuer.example/keys/v1",
			},
			doc: &OIDCDiscoveryDoc{
				Issuer:  "https://issuer.example",
				JWKSURI: "https://issuer.example/jwks",
			},
			wantWarns: 1,
			wantText:  []string{"JWKS URL mismatch", "/keys/v1", "/jwks"},
		},
		{
			name: "both mismatch: 2 warnings",
			cfg: OAuthProviderConfig{
				Issuer:  "https://issuer.example",
				JWKSURL: "https://issuer.example/keys/v1",
			},
			doc: &OIDCDiscoveryDoc{
				Issuer:  "https://other-tenant.example",
				JWKSURI: "https://issuer.example/jwks",
			},
			wantWarns: 2,
		},
		{
			name: "trailing-slash on non-root path matches via canonicalization",
			cfg: OAuthProviderConfig{
				Issuer:  "https://issuer.example",
				JWKSURL: "https://issuer.example/jwks/",
			},
			doc: &OIDCDiscoveryDoc{
				Issuer:  "https://issuer.example",
				JWKSURI: "https://issuer.example/jwks",
			},
			// canonicalizeURL strips trailing slash on non-root paths; expect no drift warning.
			wantWarns: 0,
		},
		{
			name: "empty cfg.JWKSURL skips jwks check (operator hasn't pinned a value)",
			cfg:  OAuthProviderConfig{Issuer: "https://issuer.example"},
			doc: &OIDCDiscoveryDoc{
				Issuer:  "https://issuer.example",
				JWKSURI: "https://issuer.example/jwks",
			},
			wantWarns: 0,
		},
		{
			name: "empty doc.Issuer skips issuer check",
			cfg:  OAuthProviderConfig{Issuer: "https://issuer.example"},
			doc:  &OIDCDiscoveryDoc{Issuer: ""},
			// doc.Issuer empty -> nothing to compare; no warning.
			wantWarns: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			warns := detectDiscoveryDocDrift("testprov", tc.cfg, tc.doc)
			if len(warns) != tc.wantWarns {
				t.Fatalf("warns=%d (%v), want %d", len(warns), warns, tc.wantWarns)
			}
			for _, sub := range tc.wantText {
				found := false
				for _, w := range warns {
					if strings.Contains(w, sub) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected a warning containing %q; got %v", sub, warns)
				}
			}
		})
	}
}

func TestValidateClassifiedProvider(t *testing.T) {
	tests := []struct {
		name           string
		providerID     string
		classification ProviderClassification
		userinfo       []UserInfoEndpoint
		wantErrs       int
	}{
		{
			name:           "no userinfo endpoints (built-in provider) skips validation",
			providerID:     "tmi",
			classification: ClassificationNonOIDC,
			userinfo:       []UserInfoEndpoint{},
			wantErrs:       0,
		},
		{
			name:           "OIDCCompliant accepts no explicit mappings",
			classification: ClassificationOIDCCompliant,
			userinfo:       []UserInfoEndpoint{{URL: "https://example/userinfo"}},
			wantErrs:       0,
		},
		{
			name:           "OIDCCustomUserinfo without subject_claim fails",
			classification: ClassificationOIDCCustomUserinfo,
			userinfo:       []UserInfoEndpoint{{URL: "https://graph.microsoft.com/v1.0/me"}},
			wantErrs:       1,
		},
		{
			name:           "OIDCCustomUserinfo with subject_claim passes",
			classification: ClassificationOIDCCustomUserinfo,
			userinfo:       []UserInfoEndpoint{{URL: "https://graph.microsoft.com/v1.0/me", Claims: map[string]string{"subject_claim": "id"}}},
			wantErrs:       0,
		},
		{
			name:           "NonOIDC without subject_claim fails",
			classification: ClassificationNonOIDC,
			userinfo:       []UserInfoEndpoint{{URL: "https://api.github.com/user"}},
			wantErrs:       1,
		},
		{
			name:           "NonOIDC with subject_claim on primary passes",
			classification: ClassificationNonOIDC,
			userinfo:       []UserInfoEndpoint{{URL: "https://api.github.com/user", Claims: map[string]string{"subject_claim": "id"}}},
			wantErrs:       0,
		},
		{
			name:           "NonOIDC with subject_claim on secondary endpoint passes",
			classification: ClassificationNonOIDC,
			userinfo: []UserInfoEndpoint{
				{URL: "https://api.github.com/user/emails", Claims: map[string]string{"email_claim": "[0].email"}},
				{URL: "https://api.github.com/user", Claims: map[string]string{"subject_claim": "id"}},
			},
			wantErrs: 0,
		},
		{
			name:           "non-TMI provider with empty userinfo fails (would 500 at runtime)",
			classification: ClassificationNonOIDC,
			userinfo:       []UserInfoEndpoint{},
			wantErrs:       1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := tt.providerID
			if id == "" {
				id = "test"
			}
			cp := ClassifiedProvider{ProviderID: id, Classification: tt.classification}
			cfg := OAuthProviderConfig{UserInfo: tt.userinfo}
			errs := ValidateClassifiedProvider(cp, cfg)
			if len(errs) != tt.wantErrs {
				t.Errorf("got %d errs %v, want %d", len(errs), errs, tt.wantErrs)
			}
		})
	}
}
