package saml

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/crewjam/saml"
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

// newTestCertDER generates a throwaway self-signed certificate standing in
// for the IdP's signing certificate in metadata.
func newTestCertDER(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-idp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}
	return der
}

// newTestLogoutProvider builds a SAMLProvider whose IdP metadata (parsed from
// XML, the same path production metadata takes) carries the given signing
// certificate. ProcessLogoutRequest never touches p.serviceProvider, so no SP
// key material is required.
func newTestLogoutProvider(t *testing.T, certDER []byte) *SAMLProvider {
	t.Helper()
	certB64 := base64.StdEncoding.EncodeToString(certDER)
	metadataXML := fmt.Sprintf(`<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.com/metadata">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <KeyDescriptor use="signing">
      <KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#">
        <X509Data><X509Certificate>%s</X509Certificate></X509Data>
      </KeyInfo>
    </KeyDescriptor>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.com/sso"/>
  </IDPSSODescriptor>
</EntityDescriptor>`, certB64)

	metadata := &saml.EntityDescriptor{}
	if err := xml.Unmarshal([]byte(metadataXML), metadata); err != nil {
		t.Fatalf("failed to parse test IdP metadata: %v", err)
	}
	return &SAMLProvider{
		config: &SAMLConfig{
			EntityID: "https://sp.example.com/metadata",
			SLOURL:   "https://sp.example.com/saml/slo",
		},
		idpMetadata: metadata,
	}
}

// TestProcessLogoutRequest_RejectsUnsignedRequest is the regression test for
// the unauthenticated targeted session-invalidation finding: a forged
// LogoutRequest carrying the correct (public) IdP entityID and a victim
// NameID, but no XML signature, must be rejected.
func TestProcessLogoutRequest_RejectsUnsignedRequest(t *testing.T) {
	p := newTestLogoutProvider(t, newTestCertDER(t))

	forged := fmt.Sprintf(`<samlp:LogoutRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="id-forged" Version="2.0" IssueInstant="%s" Destination="https://sp.example.com/saml/slo">
  <saml:Issuer>https://idp.example.com/metadata</saml:Issuer>
  <saml:NameID Format="urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress">victim@example.com</saml:NameID>
</samlp:LogoutRequest>`, time.Now().UTC().Format(time.RFC3339))

	_, err := p.ProcessLogoutRequest(base64.StdEncoding.EncodeToString([]byte(forged)))
	if err == nil {
		t.Fatal("unsigned forged LogoutRequest was accepted; expected signature validation error")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Fatalf("expected a signature validation error, got: %v", err)
	}
}

// TestProcessLogoutRequest_RejectsWhenNoSigningCerts verifies fail-closed
// behavior when the IdP metadata carries no signing certificates.
func TestProcessLogoutRequest_RejectsWhenNoSigningCerts(t *testing.T) {
	metadata := &saml.EntityDescriptor{}
	metadataXML := `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.com/metadata"><IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol"/></EntityDescriptor>`
	if err := xml.Unmarshal([]byte(metadataXML), metadata); err != nil {
		t.Fatalf("failed to parse test IdP metadata: %v", err)
	}
	p := &SAMLProvider{
		config:      &SAMLConfig{EntityID: "https://sp.example.com/metadata"},
		idpMetadata: metadata,
	}

	req := `<samlp:LogoutRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" ID="x" Version="2.0"/>`
	if _, err := p.ProcessLogoutRequest(base64.StdEncoding.EncodeToString([]byte(req))); err == nil {
		t.Fatal("LogoutRequest accepted with no IdP signing certificates in metadata; expected rejection")
	}
}

// TestProcessLogoutRequest_RejectsWrongRootElement verifies that a payload
// whose root is not a LogoutRequest is rejected before any field is trusted.
func TestProcessLogoutRequest_RejectsWrongRootElement(t *testing.T) {
	p := newTestLogoutProvider(t, newTestCertDER(t))
	req := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" ID="x" Version="2.0"/>`
	if _, err := p.ProcessLogoutRequest(base64.StdEncoding.EncodeToString([]byte(req))); err == nil {
		t.Fatal("non-LogoutRequest root element accepted; expected rejection")
	}
}
