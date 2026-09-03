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

// TestExtractUserInfo_GivenFamilyNameMappable verifies that an operator can
// map given_name/family_name to arbitrary SAML attribute names via
// AttributeMapping (#648), and that mapUserAttributes honors those names.
func TestExtractUserInfo_GivenFamilyNameMappable(t *testing.T) {
	assertion := makeAssertion("nameid-gf", map[string]string{
		"custom_given_attr":  "Dana",
		"custom_family_attr": "Example",
	})

	cfg := &SAMLConfig{
		ID: "custom-idp",
		AttributeMapping: map[string]string{
			"given_name":  "custom_given_attr",
			"family_name": "custom_family_attr",
		},
	}

	got, err := ExtractUserInfo(assertion, cfg)
	if err != nil {
		t.Fatalf("ExtractUserInfo returned error: %v", err)
	}
	if got.GivenName != "Dana" {
		t.Errorf("GivenName: want %q, got %q", "Dana", got.GivenName)
	}
	if got.FamilyName != "Example" {
		t.Errorf("FamilyName: want %q, got %q", "Example", got.FamilyName)
	}
}

// TestExtractUserInfo_SyntheticFallbackSetsFlags proves that when neither
// email nor name attributes are present, the synthesized values are flagged
// via EmailSynthesized/NameSynthesized so callers (e.g. updateSAMLUserOnLogin)
// know not to persist them over real stored values (#648).
func TestExtractUserInfo_SyntheticFallbackSetsFlags(t *testing.T) {
	assertion := makeAssertion("opaque-id-2", map[string]string{
		"some.unrecognized.attribute": "irrelevant",
	})
	cfg := &SAMLConfig{ID: "minimal-idp"}

	got, err := ExtractUserInfo(assertion, cfg)
	if err != nil {
		t.Fatalf("ExtractUserInfo returned error: %v", err)
	}
	if !got.EmailSynthesized {
		t.Errorf("EmailSynthesized: want true, got false")
	}
	if !got.NameSynthesized {
		t.Errorf("NameSynthesized: want true, got false")
	}
}

// TestExtractUserInfo_RealAttributesDoNotSetFlags is the inverse of
// TestExtractUserInfo_SyntheticFallbackSetsFlags: when the IdP asserts real
// email and name attributes, neither synthesized flag should be set.
func TestExtractUserInfo_RealAttributesDoNotSetFlags(t *testing.T) {
	assertion := makeAssertion("nameid-real", map[string]string{
		"http://schemas.microsoft.com/identity/claims/displayname":           "Real Name",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress": "real@example.com",
	})
	cfg := &SAMLConfig{ID: "entra-tmidev-saml"}

	got, err := ExtractUserInfo(assertion, cfg)
	if err != nil {
		t.Fatalf("ExtractUserInfo returned error: %v", err)
	}
	if got.EmailSynthesized {
		t.Errorf("EmailSynthesized: want false, got true")
	}
	if got.NameSynthesized {
		t.Errorf("NameSynthesized: want false, got true")
	}
}

// TestExtractUserInfo_EntraDefaultClaims_ComposesGivenSurname reproduces
// issue #799: with Entra's default enterprise-app claim set, claims/name
// carries the userPrincipalName, not a display name, and no displayname claim
// is emitted. The composed givenname + surname must win over the UPN.
func TestExtractUserInfo_EntraDefaultClaims_ComposesGivenSurname(t *testing.T) {
	assertion := makeAssertion("nameid-abc", map[string]string{
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name":         "eric_efitz.net#EXT#@ericfitztest.onmicrosoft.com",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname":    "Eric",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname":      "Fitzgerald",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress": "eric@efitz.net",
	})
	got, err := ExtractUserInfo(assertion, &SAMLConfig{ID: "entra-tmidev-saml"})
	if err != nil {
		t.Fatalf("ExtractUserInfo returned error: %v", err)
	}
	if got.Name != "Eric Fitzgerald" {
		t.Errorf("Name: want %q, got %q", "Eric Fitzgerald", got.Name)
	}
	if got.NameSynthesized {
		t.Errorf("NameSynthesized: want false for a composed real name")
	}
}

// TestExtractUserInfo_UPNInNameClaim_IsNotADisplayName covers the other half
// of #799: when claims/name is a UPN and no given/family name is asserted,
// the UPN must not be stored as the display name. The email local-part
// fallback applies instead and is flagged as synthesized.
func TestExtractUserInfo_UPNInNameClaim_IsNotADisplayName(t *testing.T) {
	assertion := makeAssertion("nameid-abc", map[string]string{
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name":         "eric_efitz.net#EXT#@ericfitztest.onmicrosoft.com",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress": "eric@efitz.net",
	})
	got, err := ExtractUserInfo(assertion, &SAMLConfig{ID: "entra-tmidev-saml"})
	if err != nil {
		t.Fatalf("ExtractUserInfo returned error: %v", err)
	}
	if got.Name != "eric" {
		t.Errorf("Name: want %q, got %q", "eric", got.Name)
	}
	if !got.NameSynthesized {
		t.Errorf("NameSynthesized: want true when the name is derived from the email")
	}
}
