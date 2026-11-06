package saml

import (
	"crypto"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestRecoverToError_ConvertsPanicToError(t *testing.T) {
	got, err := recoverToError("test", func() (string, error) {
		if true {
			panic("secret-detail")
		}
		return "unreachable", nil
	})
	if err == nil {
		t.Fatal("expected an error from a panicking function")
	}
	if got != "" {
		t.Fatalf("expected zero result after panic, got %q", got)
	}
	if strings.Contains(err.Error(), "secret-detail") {
		t.Fatalf("error must not leak panic detail: %v", err)
	}
}

func TestRecoverToError_PassesThroughResultAndError(t *testing.T) {
	v, err := recoverToError("test", func() (int, error) { return 7, nil })
	if err != nil || v != 7 {
		t.Fatalf("unexpected: %v %v", v, err)
	}
	sentinel := errors.New("boom")
	if _, err := recoverToError("test", func() (int, error) { return 0, sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestPrecheckSAMLResponse_RejectsMalformedAndEmptyInput(t *testing.T) {
	for name, in := range map[string]string{
		"not xml":   "this is not xml",
		"truncated": `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"><saml:Assertion`,
		"empty":     "",
	} {
		t.Run(name, func(t *testing.T) {
			if err := precheckSAMLResponse([]byte(in), nil); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestPrecheckSAMLResponse_ResolvesNamespacesNotPrefixes(t *testing.T) {
	f := newSpoofFixture(t)
	// A prefix named "saml" bound to a foreign namespace must not pass for the
	// SAML assertion namespace, and a SAML-bound alias prefix must.
	spoof := `<r xmlns:saml="urn:evil"><a:Assertion xmlns:a="urn:oasis:names:tc:SAML:2.0:assertion">` +
		`<a:Issuer>i</a:Issuer><a:Subject><saml:NameID>victim</saml:NameID></a:Subject><a:Conditions/></a:Assertion></r>`
	if err := precheckSAMLResponse([]byte(spoof), f.spKey); err == nil {
		t.Fatal("NameID with prefix bound to a foreign namespace accepted")
	}
	alias := strings.Replace(spoof, `<saml:NameID>`, `<a:NameID>`, 1)
	alias = strings.Replace(alias, `</saml:NameID>`, `</a:NameID>`, 1)
	if err := precheckSAMLResponse([]byte(alias), f.spKey); err != nil {
		t.Fatalf("SAML-bound alias prefix rejected: %v", err)
	}
}

func TestPrecheckSAMLResponse_IssuerMustBeSAMLNamespace(t *testing.T) {
	doc := `<r><Assertion xmlns="urn:oasis:names:tc:SAML:2.0:assertion"><Issuer xmlns="urn:evil">i</Issuer><Subject/><Conditions/></Assertion></r>`
	if err := precheckSAMLResponse([]byte(doc), nil); err == nil {
		t.Fatal("foreign-namespace Issuer accepted")
	}
}

func TestPrecheckSAMLResponse_IgnoresNonDirectAndForeignAssertions(t *testing.T) {
	// Mirrors the library: nested Assertion elements and foreign-namespace
	// Assertion elements are not examined (the library does not use them).
	doc := `<r><x><Assertion xmlns="urn:oasis:names:tc:SAML:2.0:assertion"/></x><Assertion xmlns="urn:evil"/></r>`
	if err := precheckSAMLResponse([]byte(doc), nil); err != nil {
		t.Fatalf("unexpected rejection: %v", err)
	}
}

func TestPrecheckSAMLResponse_EncryptedAssertion(t *testing.T) {
	f := newSpoofFixture(t)
	decode := func(b64 string) []byte {
		b, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	good := decode(f.encryptedResponse(t, assertionOpts{subjectNameIDs: goodNameID}))
	if err := precheckSAMLResponse(good, f.spKey); err != nil {
		t.Fatalf("valid encrypted assertion rejected: %v", err)
	}
	// No SP key: skipped here (the library rejects it).
	if err := precheckSAMLResponse(good, nil); err != nil {
		t.Fatalf("expected skip with nil key, got %v", err)
	}
	// Wrong key: decryption fails, so the pre-check fails closed.
	otherKey, _, _ := newSelfSigned(t, "other")
	if err := precheckSAMLResponse(good, crypto.Signer(otherKey)); err == nil {
		t.Fatal("decryption failure must fail closed")
	}
	// Foreign NameID inside the encrypted assertion.
	bad := decode(f.encryptedResponse(t, assertionOpts{subjectNameIDs: foreignNameID}))
	if err := precheckSAMLResponse(bad, f.spKey); err == nil {
		t.Fatal("foreign NameID inside encrypted assertion accepted")
	}
}

func TestPrecheckAssertion_ValidAccepted(t *testing.T) {
	doc := assertionXML(assertionOpts{subjectNameIDs: goodNameID})
	if err := precheckAssertion(parseEl(t, doc)); err != nil {
		t.Fatalf("valid assertion rejected: %v", err)
	}
}

func TestPrecheckLogoutRequestNameID(t *testing.T) {
	ok := `<samlp:LogoutRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"><saml:NameID>a</saml:NameID></samlp:LogoutRequest>`
	if err := precheckLogoutRequestNameID([]byte(ok)); err != nil {
		t.Fatalf("valid rejected: %v", err)
	}
	bad := `<samlp:LogoutRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"><NameID xmlns="urn:evil">a</NameID></samlp:LogoutRequest>`
	if err := precheckLogoutRequestNameID([]byte(bad)); err == nil {
		t.Fatal("foreign NameID accepted")
	}
	if err := precheckLogoutRequestNameID([]byte("<<<")); err == nil {
		t.Fatal("malformed XML accepted")
	}
}
