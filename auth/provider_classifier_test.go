package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
