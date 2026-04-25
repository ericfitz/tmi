package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiscoveryClient_Discover_Success(t *testing.T) {
	body := `{"issuer":"%s","authorization_endpoint":"a","token_endpoint":"t","jwks_uri":"j","userinfo_endpoint":"u","subject_types_supported":["public"],"response_types_supported":["code"]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, body, "https://issuer.example")
	}))
	defer srv.Close()

	c := NewDiscoveryClient(2*time.Second, 1*time.Hour)
	doc, err := c.Discover(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil doc")
	}
	if doc.Issuer != "https://issuer.example" {
		t.Errorf("issuer = %q", doc.Issuer)
	}
}

func TestDiscoveryClient_Discover_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewDiscoveryClient(2*time.Second, 1*time.Hour)
	doc, err := c.Discover(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("expected nil err on 404, got %v", err)
	}
	if doc != nil {
		t.Errorf("expected nil doc on 404, got %+v", doc)
	}
}

func TestDiscoveryClient_Discover_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "not json")
	}))
	defer srv.Close()

	c := NewDiscoveryClient(2*time.Second, 1*time.Hour)
	doc, err := c.Discover(context.Background(), srv.URL)
	if err != nil || doc != nil {
		t.Errorf("invalid JSON: doc=%v err=%v; want both nil", doc, err)
	}
}

func TestDiscoveryClient_Discover_MissingRequiredFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"issuer":"x"}`)
	}))
	defer srv.Close()

	c := NewDiscoveryClient(2*time.Second, 1*time.Hour)
	doc, err := c.Discover(context.Background(), srv.URL)
	if err != nil || doc != nil {
		t.Errorf("missing fields: doc=%v err=%v; want both nil", doc, err)
	}
}

func TestDiscoveryClient_Discover_CacheHit(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = fmt.Fprint(w, `{"issuer":"i","authorization_endpoint":"a","token_endpoint":"t","jwks_uri":"j","subject_types_supported":["public"],"response_types_supported":["code"]}`)
	}))
	defer srv.Close()

	c := NewDiscoveryClient(2*time.Second, 1*time.Hour)
	for i := 0; i < 3; i++ {
		if _, err := c.Discover(context.Background(), srv.URL); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected 1 upstream call (cached after first), got %d", got)
	}
}

func TestDiscoveryClient_Discover_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	c := NewDiscoveryClient(50*time.Millisecond, 1*time.Hour)
	doc, err := c.Discover(context.Background(), srv.URL)
	if err != nil || doc != nil {
		t.Errorf("timeout: doc=%v err=%v; want both nil", doc, err)
	}
}

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
	if !doc.IsValid() {
		t.Errorf("expected parsed Google doc to be valid")
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
			name: "missing authorization_endpoint",
			doc: OIDCDiscoveryDoc{
				Issuer: "i", TokenEndpoint: "t", JWKSURI: "j",
				SubjectTypesSupported: []string{"public"}, ResponseTypesSupported: []string{"code"},
			},
			want: false,
		},
		{
			name: "missing token_endpoint",
			doc: OIDCDiscoveryDoc{
				Issuer: "i", AuthorizationEndpoint: "a", JWKSURI: "j",
				SubjectTypesSupported: []string{"public"}, ResponseTypesSupported: []string{"code"},
			},
			want: false,
		},
		{
			name: "missing jwks_uri",
			doc: OIDCDiscoveryDoc{
				Issuer: "i", AuthorizationEndpoint: "a", TokenEndpoint: "t",
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
		{
			name: "missing response_types",
			doc: OIDCDiscoveryDoc{
				Issuer: "i", AuthorizationEndpoint: "a", TokenEndpoint: "t", JWKSURI: "j",
				SubjectTypesSupported: []string{"public"},
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
