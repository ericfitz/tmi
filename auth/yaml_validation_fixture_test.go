package auth

// yaml_validation_fixture_test.go tests that each dev/test YAML config file
// passes ValidateAllOAuthProviders without errors. This is a regression guard
// for the class of bug described in C1 (Issue #288): a stale or wrong userinfo
// URL in a config file causing the validator to classify an OIDC-compliant
// provider as OIDCCustomUserinfo, requiring an explicit subject_claim that
// isn't present, and refusing to start.
//
// The test loads each YAML file directly (pure struct unmarshaling, no DB
// or Redis connections) and runs the validator against a stubbed discovery
// client whose HTTP transport returns canned discovery documents for the known
// issuer URLs used in our dev/test configs.
//
// Files covered:
//   - config-development.yml
//   - config-development-mysql.yml
//   - config-development-oci.yml
//   - config-development-sqlite.yml
//   - config-development-sqlserver.yml
//   - config-test.yml
//   - config-test-integration-pg.yml
//   - config-test-integration-oci.yml
//   - config-example.yml
//
// config-production.yml is intentionally excluded: it contains many disabled
// providers with incomplete configs that are fine to disable but would fail if
// we tried to validate them here.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/config"
	"gopkg.in/yaml.v3"
)

// cannedDiscoveryDocs maps issuer URL prefixes to their canned OIDC discovery documents.
// Only the fields required by OIDCDiscoveryDoc.IsValid() plus userinfo_endpoint are set.
// Issuers not in this map return 404 (treated as NonOIDC).
var cannedDiscoveryDocs = map[string]OIDCDiscoveryDoc{
	// Google's canonical OIDC issuer
	"https://accounts.google.com": {
		Issuer:                 "https://accounts.google.com",
		AuthorizationEndpoint:  "https://accounts.google.com/o/oauth2/v2/auth",
		TokenEndpoint:          "https://oauth2.googleapis.com/token",
		UserinfoEndpoint:       "https://openidconnect.googleapis.com/v1/userinfo",
		JWKSURI:                "https://www.googleapis.com/oauth2/v3/certs",
		SubjectTypesSupported:  []string{"public"},
		ResponseTypesSupported: []string{"code"},
	},
	// Microsoft consumer tenant
	"https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/v2.0": {
		Issuer:                 "https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/v2.0",
		AuthorizationEndpoint:  "https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/oauth2/v2.0/authorize",
		TokenEndpoint:          "https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/oauth2/v2.0/token",
		UserinfoEndpoint:       "https://graph.microsoft.com/oidc/userinfo",
		JWKSURI:                "https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/discovery/v2.0/keys",
		SubjectTypesSupported:  []string{"pairwise"},
		ResponseTypesSupported: []string{"code"},
	},
	// Microsoft "consumers" tenant (used in config-test.yml)
	"https://login.microsoftonline.com/consumers/v2.0": {
		Issuer:                 "https://login.microsoftonline.com/consumers/v2.0",
		AuthorizationEndpoint:  "https://login.microsoftonline.com/consumers/oauth2/v2.0/authorize",
		TokenEndpoint:          "https://login.microsoftonline.com/consumers/oauth2/v2.0/token",
		UserinfoEndpoint:       "https://graph.microsoft.com/oidc/userinfo",
		JWKSURI:                "https://login.microsoftonline.com/consumers/discovery/v2.0/keys",
		SubjectTypesSupported:  []string{"pairwise"},
		ResponseTypesSupported: []string{"code"},
	},
}

// cannedRoundTripper is an http.RoundTripper that serves canned OIDC discovery
// documents for known issuers and returns 404 for everything else.
type cannedRoundTripper struct{}

func (cannedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// The DiscoveryClient requests <issuer>/.well-known/openid-configuration.
	// Reconstruct the issuer from the request URL by stripping the well-known path.
	reqURL := req.URL.String()
	issuer := strings.TrimSuffix(reqURL, wellKnownPath)
	issuer = strings.TrimSuffix(issuer, "/")

	// Try exact match first, then prefix match for parameterized issuers
	doc, ok := cannedDiscoveryDocs[issuer]
	if !ok {
		// Check by prefix — handles cases where the URL might have trailing slash variations
		for k, v := range cannedDiscoveryDocs {
			if strings.EqualFold(k, issuer) {
				doc = v
				ok = true
				break
			}
		}
	}

	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       http.NoBody,
			Request:    req,
		}, nil
	}

	body, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       newReadCloser(body),
		Request:    req,
	}, nil
}

// newReadCloser wraps a byte slice in an io.ReadCloser
func newReadCloser(data []byte) *nopCloser {
	return &nopCloser{r: strings.NewReader(string(data))}
}

type nopCloser struct {
	r *strings.Reader
}

func (n *nopCloser) Read(p []byte) (int, error) { return n.r.Read(p) }
func (n *nopCloser) Close() error               { return nil }

// loadYAMLOAuthProviders loads OAuth provider configs from a YAML file without
// initializing any infrastructure (DB, Redis). Returns only the OAuth provider
// map extracted from the config YAML. Returns (nil, false) when the file does
// not exist (callers should skip the test for missing gitignored files).
func loadYAMLOAuthProviders(t *testing.T, yamlPath string) (map[string]OAuthProviderConfig, bool) {
	t.Helper()

	data, err := os.ReadFile(yamlPath) // #nosec G304 -- test fixture path only
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}
		t.Fatalf("loadYAMLOAuthProviders: read %s: %v", yamlPath, err)
	}

	// Use a minimal struct that only unmarshals the OAuth section we care about.
	// Reuse internal/config types to stay aligned with production parsing.
	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("loadYAMLOAuthProviders: unmarshal %s: %v", yamlPath, err)
	}

	// Convert from internal/config types to auth types (same as ConfigFromUnified)
	providers := make(map[string]OAuthProviderConfig)
	for id, p := range cfg.Auth.OAuth.Providers {
		var userInfo []UserInfoEndpoint
		for _, ep := range p.UserInfo {
			userInfo = append(userInfo, UserInfoEndpoint{
				URL:    ep.URL,
				Claims: ep.Claims,
			})
		}
		providers[id] = OAuthProviderConfig{
			ID:       p.ID,
			Enabled:  p.Enabled,
			Issuer:   p.Issuer,
			UserInfo: userInfo,
		}
	}
	return providers, true
}

// newCannedDiscoveryClient returns a DiscoveryClient whose HTTP requests are
// served by cannedRoundTripper (no real network calls).
func newCannedDiscoveryClient() *DiscoveryClient {
	c := NewDiscoveryClient(2*time.Second, 1*time.Hour)
	c.httpClient = &http.Client{
		Transport: cannedRoundTripper{},
		Timeout:   2 * time.Second,
	}
	return c
}

// projectRoot returns the repository root by walking up from the test's working
// directory until we find a directory containing go.mod. Tests in auth/ have
// their working directory set to auth/ by the Go test runner.
func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root (go.mod not found)")
		}
		dir = parent
	}
}

// TestYAMLConfigsPassOAuthValidation loads each dev/test YAML file and asserts
// that ValidateAllOAuthProviders returns no errors when using canned discovery
// responses for known issuers.
func TestYAMLConfigsPassOAuthValidation(t *testing.T) {
	root := projectRoot(t)

	// Files to validate. config-production.yml is excluded (many intentionally
	// incomplete disabled providers; validates as part of a different workflow).
	files := []string{
		"config-development.yml",
		"config-development-mysql.yml",
		"config-development-oci.yml",
		"config-development-sqlite.yml",
		"config-development-sqlserver.yml",
		"config-test.yml",
		"config-test-integration-pg.yml",
		"config-test-integration-oci.yml",
		"config-example.yml",
	}

	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			absPath := filepath.Join(root, f)
			providers, ok := loadYAMLOAuthProviders(t, absPath)
			if !ok {
				t.Logf("skipping %s (file not present — gitignored local config)", f)
				return
			}
			if len(providers) == 0 {
				t.Logf("no oauth providers found in %s (skipping)", f)
				return
			}

			client := newCannedDiscoveryClient()
			errs := ValidateAllOAuthProviders(context.Background(), client, providers)
			if len(errs) > 0 {
				t.Errorf("ValidateAllOAuthProviders(%s) returned %d error(s):\n%s",
					f, len(errs), strings.Join(errs, "\n"))
			}
		})
	}
}
