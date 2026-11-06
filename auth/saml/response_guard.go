package saml

import (
	"crypto"
	"fmt"

	"github.com/beevik/etree"
	"github.com/crewjam/saml/xmlenc"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Mitigation for GO-2026-6909 (crewjam/saml v0.5.1, no fixed release; upstream
// PR crewjam/saml#674), tracked in #1138. The library:
//   - unmarshals Subject.NameID / LogoutRequest.NameID with a namespace-blind
//     tag, so a <NameID> in a foreign namespace is accepted as the subject, and
//   - dereferences optional assertion children (Subject, Conditions) without nil
//     checks in validateAssertion, so a signed but incomplete assertion panics.
//
// The checks here are reject-only pre-checks that run before the library sees
// the document. Remove them once a fixed crewjam/saml release is adopted.

const (
	samlAssertionNS = "urn:oasis:names:tc:SAML:2.0:assertion"
	xmlEncNS        = "http://www.w3.org/2001/04/xmlenc#"
)

// SEM@b50a40200e4338e13e0818ad44b5899a41ffff7b: reject SAML responses with malformed or namespace-spoofed assertions before library parsing (pure)
func precheckSAMLResponse(responseXML []byte, spKey crypto.Signer) error {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(responseXML); err != nil {
		return fmt.Errorf("SAML response pre-check: malformed XML: %w", err)
	}
	root := doc.Root()
	if root == nil {
		return fmt.Errorf("SAML response pre-check: no root element")
	}

	// Mirror the library: only direct children of the root, matched by
	// resolved namespace and local name.
	for _, child := range root.ChildElements() {
		if child.NamespaceURI() != samlAssertionNS {
			continue
		}
		switch child.Tag {
		case "Assertion":
			if err := precheckAssertion(child); err != nil {
				return err
			}
		case "EncryptedAssertion":
			if spKey == nil {
				// The library rejects EncryptedAssertion without an SP key.
				continue
			}
			plain, err := decryptAssertionForPrecheck(child, spKey)
			if err != nil {
				return fmt.Errorf("SAML response pre-check: cannot inspect encrypted assertion: %w", err)
			}
			if err := precheckAssertion(plain); err != nil {
				return err
			}
		}
	}
	return nil
}

// SEM@b50a40200e4338e13e0818ad44b5899a41ffff7b: decrypt an EncryptedAssertion element to its plaintext Assertion element, as the library does
func decryptAssertionForPrecheck(encryptedEl *etree.Element, spKey crypto.Signer) (*etree.Element, error) {
	var dataEls []*etree.Element
	for _, c := range encryptedEl.ChildElements() {
		if c.Tag == "EncryptedData" && c.NamespaceURI() == xmlEncNS {
			dataEls = append(dataEls, c)
		}
	}
	if len(dataEls) != 1 {
		return nil, fmt.Errorf("expected exactly one EncryptedData element, found %d", len(dataEls))
	}

	var key interface{} = spKey
	if keyEl := encryptedEl.FindElement("./EncryptedKey"); keyEl != nil {
		k, err := xmlenc.Decrypt(spKey, keyEl)
		if err != nil {
			return nil, fmt.Errorf("decrypt key: %w", err)
		}
		key = k
	}
	plaintext, err := xmlenc.Decrypt(key, dataEls[0])
	if err != nil {
		return nil, fmt.Errorf("decrypt data: %w", err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(plaintext); err != nil {
		return nil, fmt.Errorf("parse decrypted assertion: %w", err)
	}
	if doc.Root() == nil {
		return nil, fmt.Errorf("decrypted assertion has no root element")
	}
	return doc.Root(), nil
}

// SEM@b50a40200e4338e13e0818ad44b5899a41ffff7b: validate an Assertion element has required children and only SAML-namespace NameIDs (pure)
func precheckAssertion(assertionEl *etree.Element) error {
	if assertionEl.Tag != "Assertion" || assertionEl.NamespaceURI() != samlAssertionNS {
		return fmt.Errorf("SAML response pre-check: unexpected assertion element")
	}
	var issuers, subjects, conditions int
	for _, c := range assertionEl.ChildElements() {
		switch c.Tag {
		case "Issuer":
			if c.NamespaceURI() == samlAssertionNS {
				issuers++
			}
		case "Subject":
			// The library's Subject tag is namespace-blind; any Subject-named
			// child outside the SAML namespace is an attack.
			if c.NamespaceURI() != samlAssertionNS {
				return fmt.Errorf("SAML response pre-check: Subject outside the SAML assertion namespace")
			}
			subjects++
			if err := requireSAMLNameIDs(c, "Subject"); err != nil {
				return err
			}
		case "Conditions":
			conditions++
		}
	}
	if issuers == 0 {
		return fmt.Errorf("SAML response pre-check: assertion has no Issuer")
	}
	// The library dereferences Subject and Conditions unconditionally.
	if subjects != 1 {
		return fmt.Errorf("SAML response pre-check: assertion must have exactly one Subject")
	}
	if conditions == 0 {
		return fmt.Errorf("SAML response pre-check: assertion has no Conditions")
	}
	return nil
}

// SEM@b50a40200e4338e13e0818ad44b5899a41ffff7b: reject any direct NameID child of an element that is outside the SAML assertion namespace (pure)
func requireSAMLNameIDs(parent *etree.Element, parentName string) error {
	for _, c := range parent.ChildElements() {
		if c.Tag == "NameID" && c.NamespaceURI() != samlAssertionNS {
			return fmt.Errorf("SAML pre-check: %s contains a NameID outside the SAML assertion namespace", parentName)
		}
	}
	return nil
}

// SEM@b50a40200e4338e13e0818ad44b5899a41ffff7b: run fn, converting any panic into an error and logging the recovery
func recoverToError[T any](what string, fn func() (T, error)) (result T, err error) {
	defer func() {
		if r := recover(); r != nil {
			// Identifiers only: never log XML or assertion content.
			slogging.Get().Error("Recovered panic while processing %s (panic type %T)", what, r)
			var zero T
			result = zero
			err = fmt.Errorf("internal error while processing %s", what)
		}
	}()
	return fn()
}

// SEM@b50a40200e4338e13e0818ad44b5899a41ffff7b: reject a signature-verified LogoutRequest whose NameID is outside the SAML namespace (pure)
func precheckLogoutRequestNameID(verifiedXML []byte) error {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(verifiedXML); err != nil {
		return fmt.Errorf("SAML logout request pre-check: malformed XML: %w", err)
	}
	if doc.Root() == nil {
		return fmt.Errorf("SAML logout request pre-check: no root element")
	}
	return requireSAMLNameIDs(doc.Root(), "LogoutRequest")
}
