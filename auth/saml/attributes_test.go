package saml

import (
	"testing"

	"github.com/crewjam/saml"
)

// makeAssertion is a small helper to construct a *saml.Assertion with a NameID
// and a list of (name, value) attributes for the AttributeStatement.
func makeAssertion(nameID string, attrs map[string]string) *saml.Assertion {
	a := &saml.Assertion{
		Subject: &saml.Subject{
			NameID: &saml.NameID{Value: nameID},
		},
	}
	if len(attrs) == 0 {
		return a
	}

	stmt := saml.AttributeStatement{}
	for name, value := range attrs {
		stmt.Attributes = append(stmt.Attributes, saml.Attribute{
			Name:   name,
			Values: []saml.AttributeValue{{Value: value}},
		})
	}
	a.AttributeStatements = []saml.AttributeStatement{stmt}
	return a
}

// TestExtractUserInfo_EntraDisplayNameFallback reproduces issue #303.
//
// Microsoft Entra ID emits the user's display name in
// http://schemas.microsoft.com/identity/claims/displayname and the email in
// http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress, but the
// operator did not configure NAME_ATTRIBUTE / EMAIL_ATTRIBUTE for this
// provider. Without well-known attribute fallbacks, the synthetic-fallback
// path produces display_name = NameID and email = NameID@<provider>.saml.tmi.
func TestExtractUserInfo_EntraDisplayNameFallback(t *testing.T) {
	nameID := "KV6PoGFDaWI0OI3XYwwyEH79CoAwIRas/CTuYuLyytE="
	assertion := makeAssertion(nameID, map[string]string{
		"http://schemas.microsoft.com/identity/claims/displayname":           "Alice Example",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress": "alice@example.com",
	})

	// Operator forgot to configure attribute mappings for this provider.
	cfg := &SAMLConfig{ID: "entra-tmidev-saml"}

	got, err := ExtractUserInfo(assertion, cfg)
	if err != nil {
		t.Fatalf("ExtractUserInfo returned error: %v", err)
	}

	if got.Email != "alice@example.com" {
		t.Errorf("Email: want %q, got %q", "alice@example.com", got.Email)
	}
	if got.Name != "Alice Example" {
		t.Errorf("Name: want %q, got %q", "Alice Example", got.Name)
	}
	if got.ID != nameID {
		t.Errorf("ID: want %q, got %q", nameID, got.ID)
	}
}

// TestExtractUserInfo_OperatorConfigWins ensures that explicit operator
// configuration takes priority over well-known fallbacks: if the operator
// chose a specific attribute name, we use it.
func TestExtractUserInfo_OperatorConfigWins(t *testing.T) {
	assertion := makeAssertion("nameid-123", map[string]string{
		// Operator-configured attributes:
		"custom_email_attr": "configured@example.com",
		"custom_name_attr":  "Configured Name",
		// Well-known fallback attributes that should be ignored when explicit
		// config is present:
		"http://schemas.microsoft.com/identity/claims/displayname":           "Wrong Name",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress": "wrong@example.com",
	})

	cfg := &SAMLConfig{
		ID: "custom-idp",
		AttributeMapping: map[string]string{
			"email": "custom_email_attr",
			"name":  "custom_name_attr",
		},
	}

	got, err := ExtractUserInfo(assertion, cfg)
	if err != nil {
		t.Fatalf("ExtractUserInfo returned error: %v", err)
	}
	if got.Email != "configured@example.com" {
		t.Errorf("Email: want %q (operator config), got %q", "configured@example.com", got.Email)
	}
	if got.Name != "Configured Name" {
		t.Errorf("Name: want %q (operator config), got %q", "Configured Name", got.Name)
	}
}

// TestExtractUserInfo_ConfiguredAttrAbsent_FallsBackToWellKnown verifies
// that when the operator configured an attribute name but the IdP did NOT
// emit that attribute, we fall through to well-known names rather than
// silently producing synthetic values.
func TestExtractUserInfo_ConfiguredAttrAbsent_FallsBackToWellKnown(t *testing.T) {
	assertion := makeAssertion("nameid-456", map[string]string{
		// Operator configured "custom_email_attr" but it is not in the
		// assertion. Entra-style claims ARE present.
		"http://schemas.microsoft.com/identity/claims/displayname":           "Bob Example",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress": "bob@example.com",
	})

	cfg := &SAMLConfig{
		ID: "entra-tmidev-saml",
		AttributeMapping: map[string]string{
			"email": "custom_email_attr_not_emitted",
			"name":  "custom_name_attr_not_emitted",
		},
	}

	got, err := ExtractUserInfo(assertion, cfg)
	if err != nil {
		t.Fatalf("ExtractUserInfo returned error: %v", err)
	}
	if got.Email != "bob@example.com" {
		t.Errorf("Email: want %q (well-known fallback), got %q", "bob@example.com", got.Email)
	}
	if got.Name != "Bob Example" {
		t.Errorf("Name: want %q (well-known fallback), got %q", "Bob Example", got.Name)
	}
}

// TestExtractUserInfo_FriendlyNameFallback verifies that well-known
// FriendlyNames (e.g., "displayname", "emailaddress") are also tried.
// crewjam/saml stores both Name and FriendlyName as separate map entries
// in our buildAttributeMap, so a friendly-name-only attribute should also
// work as a fallback.
func TestExtractUserInfo_FriendlyNameFallback(t *testing.T) {
	a := &saml.Assertion{
		Subject: &saml.Subject{NameID: &saml.NameID{Value: "nameid-789"}},
		AttributeStatements: []saml.AttributeStatement{{
			Attributes: []saml.Attribute{
				{
					Name:         "urn:oid:2.16.840.1.113730.3.1.241",
					FriendlyName: "displayName",
					Values:       []saml.AttributeValue{{Value: "Carol Example"}},
				},
				{
					Name:         "urn:oid:0.9.2342.19200300.100.1.3",
					FriendlyName: "mail",
					Values:       []saml.AttributeValue{{Value: "carol@example.com"}},
				},
			},
		}},
	}

	cfg := &SAMLConfig{ID: "ldap-saml-idp"}

	got, err := ExtractUserInfo(a, cfg)
	if err != nil {
		t.Fatalf("ExtractUserInfo returned error: %v", err)
	}
	if got.Email != "carol@example.com" {
		t.Errorf("Email: want %q (friendly-name fallback), got %q", "carol@example.com", got.Email)
	}
	if got.Name != "Carol Example" {
		t.Errorf("Name: want %q (friendly-name fallback), got %q", "Carol Example", got.Name)
	}
}

// TestExtractUserInfo_SyntheticFallbackOnlyWhenNothingAvailable ensures the
// existing synthetic-email behavior is preserved as a true last resort when
// the assertion contains NO recognizable email or name attributes.
func TestExtractUserInfo_SyntheticFallbackOnlyWhenNothingAvailable(t *testing.T) {
	assertion := makeAssertion("opaque-id", map[string]string{
		"some.unrecognized.attribute": "irrelevant",
	})
	cfg := &SAMLConfig{ID: "minimal-idp"}

	got, err := ExtractUserInfo(assertion, cfg)
	if err != nil {
		t.Fatalf("ExtractUserInfo returned error: %v", err)
	}
	wantEmail := "opaque-id@minimal-idp.saml.tmi"
	if got.Email != wantEmail {
		t.Errorf("Email: want %q (synthetic), got %q", wantEmail, got.Email)
	}
	if got.Name != "opaque-id" {
		t.Errorf("Name: want %q (derived from synthetic email), got %q", "opaque-id", got.Name)
	}
}
