package saml

import (
	"crypto"
	"errors"
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

// errPrecheckRejected is the only error precheckSAMLResponse returns. It is
// deliberately generic: the pre-check decrypts unauthenticated input, and
// distinguishable failure messages (padding vs. parse vs. structure) would give
// a client a decryption oracle. Detail is logged server-side only.
var errPrecheckRejected = errors.New("SAML response rejected")

// SEM@74c94fa9dd5167632559edfe2f08920768f67aca: reject SAML responses with malformed or namespace-spoofed assertions, returning one generic error
func precheckSAMLResponse(responseXML []byte, spKey crypto.Signer) error {
	if err := inspectSAMLResponse(responseXML, spKey); err != nil {
		// The internal error text is built from fixed strings only (never
		// XML, values, or underlying library errors).
		slogging.Get().Warn("SAML response rejected by pre-check: %v", err)
		return errPrecheckRejected
	}
	return nil
}

// SEM@74c94fa9dd5167632559edfe2f08920768f67aca: inspect SAML response assertions and return a fixed-text reason on rejection (pure)
func inspectSAMLResponse(responseXML []byte, spKey crypto.Signer) error {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(responseXML); err != nil {
		return errors.New("malformed XML")
	}
	root := doc.Root()
	if root == nil {
		return errors.New("no root element")
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
				return fmt.Errorf("cannot inspect encrypted assertion: %w", err)
			}
			if err := precheckAssertion(plain); err != nil {
				return err
			}
		}
	}
	return nil
}

// SEM@74c94fa9dd5167632559edfe2f08920768f67aca: decrypt an EncryptedAssertion element to its plaintext Assertion element, as the library does
func decryptAssertionForPrecheck(encryptedEl *etree.Element, spKey crypto.Signer) (*etree.Element, error) {
	var dataEls []*etree.Element
	for _, c := range encryptedEl.ChildElements() {
		if c.Tag == "EncryptedData" && c.NamespaceURI() == xmlEncNS {
			dataEls = append(dataEls, c)
		}
	}
	if len(dataEls) != 1 {
		return nil, errors.New("EncryptedData element count is not one")
	}

	var key interface{} = spKey
	if keyEl := encryptedEl.FindElement("./EncryptedKey"); keyEl != nil {
		k, err := xmlenc.Decrypt(spKey, keyEl)
		if err != nil {
			return nil, errors.New("key decryption failed")
		}
		key = k
	}
	plaintext, err := xmlenc.Decrypt(key, dataEls[0])
	if err != nil {
		return nil, errors.New("data decryption failed")
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(plaintext); err != nil {
		return nil, errors.New("decrypted assertion is not well-formed XML")
	}
	if doc.Root() == nil {
		return nil, errors.New("decrypted assertion has no root element")
	}
	return doc.Root(), nil
}

// SEM@74c94fa9dd5167632559edfe2f08920768f67aca: validate an Assertion element has required children and only SAML-namespace NameIDs (pure)
func precheckAssertion(assertionEl *etree.Element) error {
	if assertionEl.Tag != "Assertion" || assertionEl.NamespaceURI() != samlAssertionNS {
		return errors.New("unexpected assertion element")
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
				return errors.New("subject outside the SAML assertion namespace")
			}
			subjects++
			if err := requireSAMLNameIDs(c, "Subject"); err != nil {
				return err
			}
			// The library dereferences SubjectConfirmationData unconditionally.
			for _, sc := range c.ChildElements() {
				if sc.Tag != "SubjectConfirmation" {
					continue
				}
				hasData := false
				for _, d := range sc.ChildElements() {
					if d.Tag == "SubjectConfirmationData" {
						hasData = true
					}
				}
				if !hasData {
					return errors.New("subject confirmation has no SubjectConfirmationData")
				}
			}
		case "Conditions":
			conditions++
		}
	}
	if issuers == 0 {
		return errors.New("assertion has no Issuer")
	}
	// The library dereferences Subject and Conditions unconditionally.
	if subjects != 1 {
		return errors.New("assertion must have exactly one Subject")
	}
	if conditions == 0 {
		return errors.New("assertion has no Conditions")
	}
	return nil
}

// SEM@74c94fa9dd5167632559edfe2f08920768f67aca: reject any direct NameID child of an element that is outside the SAML assertion namespace (pure)
func requireSAMLNameIDs(parent *etree.Element, parentName string) error {
	for _, c := range parent.ChildElements() {
		if c.Tag == "NameID" && c.NamespaceURI() != samlAssertionNS {
			return fmt.Errorf("%s contains a NameID outside the SAML assertion namespace", parentName)
		}
	}
	return nil
}

// SEM@74c94fa9dd5167632559edfe2f08920768f67aca: run fn, converting any panic into an error and logging the recovery
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

// SEM@74c94fa9dd5167632559edfe2f08920768f67aca: reject a signature-verified LogoutRequest whose NameID is outside the SAML namespace (pure)
func precheckLogoutRequestNameID(verifiedXML []byte) error {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(verifiedXML); err != nil {
		return errors.New("SAML logout request pre-check: malformed XML")
	}
	if doc.Root() == nil {
		return errors.New("SAML logout request pre-check: no root element")
	}
	return requireSAMLNameIDs(doc.Root(), "LogoutRequest")
}
