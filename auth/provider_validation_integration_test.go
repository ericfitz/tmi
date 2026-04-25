// auth/provider_validation_integration_test.go
package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestValidateAllOAuthProviders_NonOIDCMissingSubjectClaim(t *testing.T) {
	providers := map[string]OAuthProviderConfig{
		"badgithub": {
			ID:       "badgithub",
			Enabled:  true,
			Issuer:   "", // forces NonOIDC classification
			UserInfo: []UserInfoEndpoint{{URL: "https://api.github.com/user"}},
		},
	}
	client := NewDiscoveryClient(500*time.Millisecond, 1*time.Hour)
	errs := ValidateAllOAuthProviders(context.Background(), client, providers)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for non-OIDC provider missing subject_claim")
	}
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "badgithub") || !strings.Contains(joined, "subject_claim") {
		t.Errorf("error message did not include provider id and subject_claim guidance: %s", joined)
	}
}

func TestValidateAllOAuthProviders_DisabledProvidersSkipped(t *testing.T) {
	providers := map[string]OAuthProviderConfig{
		"badgithub": {
			ID:       "badgithub",
			Enabled:  false, // disabled — should not be validated
			UserInfo: []UserInfoEndpoint{{URL: "https://api.github.com/user"}},
		},
	}
	client := NewDiscoveryClient(500*time.Millisecond, 1*time.Hour)
	errs := ValidateAllOAuthProviders(context.Background(), client, providers)
	if len(errs) != 0 {
		t.Errorf("disabled provider should not produce validation errors, got %v", errs)
	}
}
