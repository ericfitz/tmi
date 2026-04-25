package auth

import (
	"encoding/json"
	"testing"
)

func TestOIDCDiscoveryDoc_Parse(t *testing.T) {
	body := []byte(`{
		"issuer": "https://accounts.google.com",
		"authorization_endpoint": "https://accounts.google.com/o/oauth2/v2/auth",
		"token_endpoint": "https://oauth2.googleapis.com/token",
		"userinfo_endpoint": "https://openidconnect.googleapis.com/v1/userinfo",
		"jwks_uri": "https://www.googleapis.com/oauth2/v3/certs",
		"subject_types_supported": ["public"],
		"response_types_supported": ["code", "token", "id_token"]
	}`)

	var doc OIDCDiscoveryDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.Issuer != "https://accounts.google.com" {
		t.Errorf("issuer = %q", doc.Issuer)
	}
	if doc.UserinfoEndpoint != "https://openidconnect.googleapis.com/v1/userinfo" {
		t.Errorf("userinfo_endpoint = %q", doc.UserinfoEndpoint)
	}
}

func TestOIDCDiscoveryDoc_IsValid(t *testing.T) {
	tests := []struct {
		name string
		doc  OIDCDiscoveryDoc
		want bool
	}{
		{
			name: "complete",
			doc: OIDCDiscoveryDoc{
				Issuer: "https://example.com", AuthorizationEndpoint: "a",
				TokenEndpoint: "t", JWKSURI: "j",
				SubjectTypesSupported:  []string{"public"},
				ResponseTypesSupported: []string{"code"},
			},
			want: true,
		},
		{
			name: "missing issuer",
			doc: OIDCDiscoveryDoc{
				AuthorizationEndpoint: "a", TokenEndpoint: "t", JWKSURI: "j",
				SubjectTypesSupported: []string{"public"}, ResponseTypesSupported: []string{"code"},
			},
			want: false,
		},
		{
			name: "missing subject_types",
			doc: OIDCDiscoveryDoc{
				Issuer: "i", AuthorizationEndpoint: "a", TokenEndpoint: "t", JWKSURI: "j",
				ResponseTypesSupported: []string{"code"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.doc.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}
