package main

import "testing"

// TestIsPublicSAMLPath guards the fix for #650: the provider-scoped SAML routes
// must stay public while GET /saml/providers/{idp}/users must not, because its
// handler needs the JWT context the auth middleware only populates for
// non-public paths.
func TestIsPublicSAMLPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		// Public, provider-scoped (x-public-endpoint in the OpenAPI spec)
		{"/saml/okta/login", true},
		{"/saml/okta/metadata", true},
		{"/saml/tmi/login", true},
		{"/saml/entra-id/metadata", true},

		// The regression this function exists to prevent: authenticated, and
		// specifically NOT covered by the old blanket "/saml/" prefix.
		{"/saml/providers/tmi/users", false},
		{"/saml/providers/okta/users", false},

		// Exact-match public paths are handled by publicPaths, not here.
		{"/saml/acs", false},
		{"/saml/slo", false},
		{"/saml/providers", false},

		// Anything else under /saml/ stays private until added deliberately.
		{"/saml/okta/login/extra", false},
		{"/saml/okta/logout", false},
		{"/saml//login", false},
		{"/saml/", false},
		{"/saml", false},

		// Non-SAML paths must not be affected.
		{"/me", false},
		{"/threat_models", false},
		{"/samlish/okta/login", false},
	}

	for _, tt := range tests {
		if got := isPublicSAMLPath(tt.path); got != tt.want {
			t.Errorf("isPublicSAMLPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// TestSAMLUsersNotInPublicPaths asserts the authenticated SAML endpoint is not
// reachable through either of the other two public-path mechanisms.
func TestSAMLUsersNotInPublicPaths(t *testing.T) {
	const authenticated = "/saml/providers/tmi/users"

	if publicPaths[authenticated] {
		t.Errorf("%s must not be in publicPaths", authenticated)
	}
	for _, prefix := range publicPathPrefixes {
		if len(authenticated) >= len(prefix) && authenticated[:len(prefix)] == prefix {
			t.Errorf("%s must not be covered by publicPathPrefixes entry %q",
				authenticated, prefix)
		}
	}

	// The public SAML paths that do use exact matching must still be listed.
	for _, p := range []string{"/saml/acs", "/saml/slo", "/saml/providers"} {
		if !publicPaths[p] {
			t.Errorf("%s should be in publicPaths", p)
		}
	}
}
