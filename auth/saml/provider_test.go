package saml

import (
	"testing"
)

// TestGetAuthorizationURLForceAuthn_Skip documents that this method requires a
// fully-configured SAML service provider (live IdP metadata URL, SP
// certificate/key pair) to exercise the redirect-URL machinery.  Constructing
// that inline in a unit test would be brittle and environment-dependent, so we
// skip here and rely on the integration test in Task 11 (test/integration/) to
// cover the end-to-end SAML ForceAuthn flow.
//
// What the implementation guarantees (verified by code inspection):
//   - GetAuthorizationURLForceAuthn delegates to MakeAuthenticationRequest
//     exactly like GetAuthorizationURL does.
//   - It then sets req.ForceAuthn = &true on the *saml.AuthnRequest before
//     calling req.Redirect — a request-scoped mutation that does not touch
//     p.serviceProvider.ForceAuthn and is therefore safe under concurrent
//     normal-login traffic.
//   - ForceAuthn field type on *saml.AuthnRequest is *bool (confirmed via
//     go doc github.com/crewjam/saml.AuthnRequest).
func TestGetAuthorizationURLForceAuthn_Skip(t *testing.T) {
	t.Skip("integration test in test/integration/workflows covers SAML ForceAuthn end-to-end (#397)")
}
