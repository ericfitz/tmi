package saml

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/crewjam/saml/xmlenc"
	dsig "github.com/russellhaering/goxmldsig"
)

const (
	testIdPEntityID = "https://idp.example.com/metadata"
	testSPEntityID  = "https://sp.example.com/metadata"
	testACSURL      = "https://sp.example.com/saml/acs"
	testSLOURL      = "https://sp.example.com/saml/slo"
)

// spoofFixture is a test IdP (signing key + cert) and an SP-side SAMLProvider
// configured to trust it, with an SP key/cert so EncryptedAssertion works.
type spoofFixture struct {
	idpKey   *rsa.PrivateKey
	idpDER   []byte
	spKey    *rsa.PrivateKey
	spCert   *x509.Certificate
	provider *SAMLProvider
}

func newSelfSigned(t *testing.T, cn string) (*rsa.PrivateKey, []byte, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return key, der, cert
}

func newSpoofFixture(t *testing.T) *spoofFixture {
	t.Helper()
	idpKey, idpDER, _ := newSelfSigned(t, "test-idp")
	spKey, _, spCert := newSelfSigned(t, "test-sp")

	base := newTestLogoutProvider(t, idpDER)
	acs, err := url.Parse(testACSURL)
	if err != nil {
		t.Fatal(err)
	}
	metaURL, _ := url.Parse("https://sp.example.com/saml/metadata")
	base.serviceProvider = &saml.ServiceProvider{
		EntityID:          testSPEntityID,
		Key:               spKey,
		Certificate:       spCert,
		IDPMetadata:       base.idpMetadata,
		AcsURL:            *acs,
		MetadataURL:       *metaURL,
		AllowIDPInitiated: true,
	}
	return &spoofFixture{idpKey: idpKey, idpDER: idpDER, spKey: spKey, spCert: spCert, provider: base}
}

// sign returns el with an enveloped signature by the test IdP key.
func (f *spoofFixture) sign(t *testing.T, el *etree.Element) *etree.Element {
	t.Helper()
	ks := dsig.TLSCertKeyStore(tls.Certificate{Certificate: [][]byte{f.idpDER}, PrivateKey: f.idpKey})
	sctx := dsig.NewDefaultSigningContext(ks)
	// Exclusive c14n so the signature survives being embedded in a Response
	// that declares additional namespaces (as real IdPs do).
	sctx.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	signed, err := sctx.SignEnveloped(el)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	// Round-trip through bytes so the element is detached and canonical, as
	// it would be after crossing the wire.
	doc := etree.NewDocument()
	doc.SetRoot(signed)
	out, err := doc.WriteToBytes()
	if err != nil {
		t.Fatalf("serialize signed element: %v", err)
	}
	return parseEl(t, string(out))
}

func parseEl(t *testing.T, s string) *etree.Element {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromString(s); err != nil {
		t.Fatalf("parse xml: %v", err)
	}
	return doc.Root()
}

type assertionOpts struct {
	omitIssuer     bool
	omitSubject    bool
	omitConditions bool
	// subjectNameIDs is the raw XML of the NameID children placed in Subject.
	subjectNameIDs string
}

const goodNameID = `<saml:NameID Format="urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress">alice@example.com</saml:NameID>`
const foreignNameID = `<NameID xmlns="urn:evil:ns">victim@example.com</NameID>`

func assertionXML(o assertionOpts) string {
	now := time.Now().UTC()
	ts := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	var b strings.Builder
	fmt.Fprintf(&b, `<saml:Assertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_a1" Version="2.0" IssueInstant="%s">`, ts(0))
	if !o.omitIssuer {
		fmt.Fprintf(&b, `<saml:Issuer>%s</saml:Issuer>`, testIdPEntityID)
	}
	if !o.omitSubject {
		fmt.Fprintf(&b, `<saml:Subject>%s<saml:SubjectConfirmation Method="urn:oasis:names:tc:SAML:2.0:cm:bearer"><saml:SubjectConfirmationData NotOnOrAfter="%s" Recipient="%s"/></saml:SubjectConfirmation></saml:Subject>`,
			o.subjectNameIDs, ts(5*time.Minute), testACSURL)
	}
	if !o.omitConditions {
		fmt.Fprintf(&b, `<saml:Conditions NotBefore="%s" NotOnOrAfter="%s"><saml:AudienceRestriction><saml:Audience>%s</saml:Audience></saml:AudienceRestriction></saml:Conditions>`,
			ts(-time.Minute), ts(5*time.Minute), testSPEntityID)
	}
	b.WriteString(`<saml:AuthnStatement AuthnInstant="` + ts(0) + `"><saml:AuthnContext><saml:AuthnContextClassRef>urn:oasis:names:tc:SAML:2.0:ac:classes:Password</saml:AuthnContextClassRef></saml:AuthnContext></saml:AuthnStatement>`)
	b.WriteString(`</saml:Assertion>`)
	return b.String()
}

// responseB64 wraps child (a signed Assertion or an EncryptedAssertion element)
// in an unsigned Response and returns it base64-encoded.
func responseB64(t *testing.T, child *etree.Element) string {
	t.Helper()
	resp := parseEl(t, fmt.Sprintf(`<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_r1" Version="2.0" IssueInstant="%s" Destination="%s"><saml:Issuer>%s</saml:Issuer><samlp:Status><samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></samlp:Status></samlp:Response>`,
		time.Now().UTC().Format(time.RFC3339), testACSURL, testIdPEntityID))
	resp.AddChild(child)
	doc := etree.NewDocument()
	doc.SetRoot(resp)
	out, err := doc.WriteToBytes()
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(out)
}

func (f *spoofFixture) signedResponse(t *testing.T, o assertionOpts) string {
	t.Helper()
	return responseB64(t, f.sign(t, parseEl(t, assertionXML(o))))
}

// encryptedResponse signs the assertion, encrypts it to the SP certificate,
// and wraps it in an EncryptedAssertion.
func (f *spoofFixture) encryptedResponse(t *testing.T, o assertionOpts) string {
	t.Helper()
	signed := f.sign(t, parseEl(t, assertionXML(o)))
	doc := etree.NewDocument()
	doc.SetRoot(signed)
	plain, err := doc.WriteToBytes()
	if err != nil {
		t.Fatal(err)
	}
	enc := xmlenc.OAEP()
	enc.BlockCipher = xmlenc.AES128CBC
	enc.DigestMethod = &xmlenc.SHA1
	encData, err := enc.Encrypt(f.spCert, plain, nil)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	wrapper := parseEl(t, `<saml:EncryptedAssertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"/>`)
	wrapper.AddChild(encData)
	return responseB64(t, wrapper)
}

func (f *spoofFixture) signedLogoutRequest(t *testing.T, nameIDXML string) string {
	t.Helper()
	el := parseEl(t, fmt.Sprintf(`<samlp:LogoutRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_l1" Version="2.0" IssueInstant="%s" Destination="%s"><saml:Issuer>%s</saml:Issuer>%s</samlp:LogoutRequest>`,
		time.Now().UTC().Format(time.RFC3339), testSLOURL, testIdPEntityID, nameIDXML))
	signed := f.sign(t, el)
	doc := etree.NewDocument()
	doc.SetRoot(signed)
	out, err := doc.WriteToBytes()
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(out)
}

// parseNoPanic calls ParseResponse and fails the test (instead of crashing the
// run) if it panics.
func parseNoPanic(t *testing.T, p *SAMLProvider, b64 string) (a *saml.Assertion, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ParseResponse panicked: %v", r)
		}
	}()
	return p.ParseResponse(b64)
}

func TestParseResponse_ValidSignedAssertionAccepted(t *testing.T) {
	f := newSpoofFixture(t)
	a, err := parseNoPanic(t, f.provider, f.signedResponse(t, assertionOpts{subjectNameIDs: goodNameID}))
	if err != nil {
		t.Fatalf("valid signed assertion rejected: %v", err)
	}
	if a.Subject == nil || a.Subject.NameID == nil || a.Subject.NameID.Value != "alice@example.com" {
		t.Fatalf("unexpected subject NameID in accepted assertion")
	}
}

func TestParseResponse_MissingIssuerRejectedWithoutPanic(t *testing.T) {
	f := newSpoofFixture(t)
	_, err := parseNoPanic(t, f.provider, f.signedResponse(t, assertionOpts{omitIssuer: true, subjectNameIDs: goodNameID}))
	if err == nil {
		t.Fatal("assertion without Issuer was accepted")
	}
}

func TestParseResponse_MissingSubjectOrConditionsRejectedWithoutPanic(t *testing.T) {
	f := newSpoofFixture(t)
	for name, o := range map[string]assertionOpts{
		"no subject":    {omitSubject: true},
		"no conditions": {omitConditions: true, subjectNameIDs: goodNameID},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseNoPanic(t, f.provider, f.signedResponse(t, o)); err == nil {
				t.Fatal("assertion accepted; expected rejection")
			}
		})
	}
}

func TestParseResponse_ForeignNamespaceNameIDRejected(t *testing.T) {
	f := newSpoofFixture(t)
	a, err := parseNoPanic(t, f.provider, f.signedResponse(t, assertionOpts{subjectNameIDs: foreignNameID}))
	if err == nil {
		got := ""
		if a != nil && a.Subject != nil && a.Subject.NameID != nil {
			got = a.Subject.NameID.Value
		}
		t.Fatalf("assertion with foreign-namespace NameID accepted; spoofed NameID value reached caller: %q", got)
	}
}

func TestParseResponse_ForeignPlusRealNameIDRejected(t *testing.T) {
	f := newSpoofFixture(t)
	for name, ids := range map[string]string{
		"real first":    goodNameID + foreignNameID,
		"foreign first": foreignNameID + goodNameID,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseNoPanic(t, f.provider, f.signedResponse(t, assertionOpts{subjectNameIDs: ids})); err == nil {
				t.Fatal("assertion with foreign NameID alongside real NameID accepted")
			}
		})
	}
}

func TestParseResponse_EncryptedValidAccepted(t *testing.T) {
	f := newSpoofFixture(t)
	if _, err := parseNoPanic(t, f.provider, f.encryptedResponse(t, assertionOpts{subjectNameIDs: goodNameID})); err != nil {
		t.Fatalf("valid encrypted assertion rejected: %v", err)
	}
}

func TestParseResponse_EncryptedForeignNameIDRejected(t *testing.T) {
	f := newSpoofFixture(t)
	a, err := parseNoPanic(t, f.provider, f.encryptedResponse(t, assertionOpts{subjectNameIDs: foreignNameID}))
	if err == nil {
		got := ""
		if a != nil && a.Subject != nil && a.Subject.NameID != nil {
			got = a.Subject.NameID.Value
		}
		t.Fatalf("encrypted assertion with foreign NameID accepted; spoofed value reached caller: %q", got)
	}
}

func TestParseResponse_EncryptedMissingSubjectRejectedWithoutPanic(t *testing.T) {
	f := newSpoofFixture(t)
	if _, err := parseNoPanic(t, f.provider, f.encryptedResponse(t, assertionOpts{omitSubject: true})); err == nil {
		t.Fatal("encrypted assertion without Subject accepted")
	}
}

func TestProcessLogoutRequest_ValidSignedAccepted(t *testing.T) {
	f := newSpoofFixture(t)
	req, err := f.provider.ProcessLogoutRequest(f.signedLogoutRequest(t, goodNameID))
	if err != nil {
		t.Fatalf("valid signed logout request rejected: %v", err)
	}
	if req.NameID == nil || req.NameID.Value != "alice@example.com" {
		t.Fatal("unexpected NameID")
	}
}

func TestProcessLogoutRequest_ForeignNamespaceNameIDRejected(t *testing.T) {
	f := newSpoofFixture(t)
	for name, ids := range map[string]string{
		"foreign only":      foreignNameID,
		"real then foreign": goodNameID + foreignNameID,
		"foreign then real": foreignNameID + goodNameID,
	} {
		t.Run(name, func(t *testing.T) {
			req, err := f.provider.ProcessLogoutRequest(f.signedLogoutRequest(t, ids))
			if err == nil {
				got := ""
				if req != nil && req.NameID != nil {
					got = req.NameID.Value
				}
				t.Fatalf("logout request with foreign NameID accepted; session selector would be %q", got)
			}
		})
	}
}
