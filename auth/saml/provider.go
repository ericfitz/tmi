package saml

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"errors"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	dsig "github.com/russellhaering/goxmldsig"
	"golang.org/x/oauth2"

	"github.com/ericfitz/tmi/internal/safehttp"
	"github.com/ericfitz/tmi/internal/slogging"
)

// TokenResponse represents the tokens returned by SAML (stub for interface compatibility)
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: stub token response struct for SAML interface compatibility (pure)
type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresIn    int
}

// IDTokenClaims represents claims from an ID token (stub for interface compatibility)
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: stub ID token claims struct for SAML interface compatibility (pure)
type IDTokenClaims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// SAMLProvider implements the Provider interface for SAML authentication
// SEM@0dcfe60d024e5cd95a40b61fc489253e670af6ce: SAML service provider holding config, SP descriptor, and IdP metadata (pure)
type SAMLProvider struct {
	config          *SAMLConfig
	serviceProvider *saml.ServiceProvider
	idpMetadata     *saml.EntityDescriptor
}

// NewSAMLProvider creates a new SAML provider
// SEM@d90c40114705e5560d91377af5101409dd78f88e: build and configure a SAMLProvider from SP key/cert and fetched IdP metadata
func NewSAMLProvider(config *SAMLConfig) (*SAMLProvider, error) {
	if config == nil {
		return nil, fmt.Errorf("SAML config is nil")
	}

	// Load SP private key and certificate
	privateKey, certificate, err := loadKeyAndCert(config)
	if err != nil {
		return nil, fmt.Errorf("failed to load SP key and certificate: %w", err)
	}

	// Parse IdP metadata
	idpMetadata, err := fetchIDPMetadata(config)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch IdP metadata: %w", err)
	}

	// Parse ACS URL
	acsURL, err := url.Parse(config.ACSURL)
	if err != nil {
		return nil, fmt.Errorf("invalid ACS URL: %w", err)
	}

	// Parse metadata URL - always derive from ACS URL
	// EntityID is just an identifier and doesn't need to be an HTTP URL
	metadataURLStr := acsURL.Scheme + "://" + acsURL.Host + "/saml/metadata"
	metadataURL, err := url.Parse(metadataURLStr)
	if err != nil {
		return nil, fmt.Errorf("invalid metadata URL: %w", err)
	}

	// Parse SLO URL if provided
	var sloURL url.URL
	if config.SLOURL != "" {
		parsedSloURL, err := url.Parse(config.SLOURL)
		if err != nil {
			return nil, fmt.Errorf("invalid SLO URL: %w", err)
		}
		sloURL = *parsedSloURL
	}

	// Create service provider configuration
	sp := &saml.ServiceProvider{
		EntityID:          config.EntityID,
		Key:               privateKey,
		Certificate:       certificate,
		IDPMetadata:       idpMetadata,
		AcsURL:            *acsURL,
		MetadataURL:       *metadataURL,
		SloURL:            sloURL,
		AllowIDPInitiated: config.AllowIDPInitiated,
		ForceAuthn:        &config.ForceAuthn,
	}

	return &SAMLProvider{
		config:          config,
		serviceProvider: sp,
		idpMetadata:     idpMetadata,
	}, nil
}

// GetOAuth2Config returns nil as SAML doesn't use OAuth2
// SEM@0dcfe60d024e5cd95a40b61fc489253e670af6ce: return nil to satisfy the provider interface; SAML does not use OAuth2 (pure)
func (p *SAMLProvider) GetOAuth2Config() *oauth2.Config {
	return nil
}

// GetAuthorizationURL generates a SAML authentication request URL
// SEM@85ed60a219cd0aba38e90907408068f8235d4cc1: build a SAML HTTP-redirect SSO URL with relay state for the configured IdP (pure)
func (p *SAMLProvider) GetAuthorizationURL(state string) (string, error) {
	// Create authentication request
	req, err := p.serviceProvider.MakeAuthenticationRequest(
		p.serviceProvider.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding,
		saml.HTTPPostBinding,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create authentication request: %w", err)
	}

	// Generate redirect URL with relay state
	redirectURL, err := req.Redirect(state, p.serviceProvider)
	if err != nil {
		return "", fmt.Errorf("failed to create redirect URL: %w", err)
	}

	return redirectURL.String(), nil
}

// GetAuthorizationURLForceAuthn generates a SAML authentication request URL
// with ForceAuthn=true set on the AuthnRequest, overriding the configured
// p.config.ForceAuthn default. Used by /oauth2/step_up (#397) to require a
// fresh interactive re-authentication at the IdP.
// SEM@e55d63794c48585aafab36880122df63ab8ab1be: build a SAML SSO URL with ForceAuthn to require fresh IdP authentication (pure)
func (p *SAMLProvider) GetAuthorizationURLForceAuthn(state string) (string, error) {
	req, err := p.serviceProvider.MakeAuthenticationRequest(
		p.serviceProvider.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding,
		saml.HTTPPostBinding,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create authentication request: %w", err)
	}

	// Force a fresh IdP prompt regardless of the configured default.
	forceAuthn := true
	req.ForceAuthn = &forceAuthn

	redirectURL, err := req.Redirect(state, p.serviceProvider)
	if err != nil {
		return "", fmt.Errorf("failed to create redirect URL: %w", err)
	}

	return redirectURL.String(), nil
}

// ExchangeCode is not applicable for SAML
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: return an error; SAML does not support authorization code exchange (pure)
func (p *SAMLProvider) ExchangeCode(ctx context.Context, code string) (*TokenResponse, error) {
	return nil, fmt.Errorf("SAML provider does not support code exchange")
}

// GetUserInfo is not applicable for SAML (user info comes from assertion)
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: return an error; SAML user info is extracted from the assertion, not via token (pure)
func (p *SAMLProvider) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	return nil, fmt.Errorf("SAML provider does not support GetUserInfo - user info comes from assertion")
}

// ValidateIDToken is not applicable for SAML
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: return an error; SAML does not issue ID tokens (pure)
func (p *SAMLProvider) ValidateIDToken(ctx context.Context, idToken string) (*IDTokenClaims, error) {
	return nil, fmt.Errorf("SAML provider does not support ID tokens")
}

// ParseResponse parses and validates a SAML response with full signature verification
// SEM@3cfa5ca8e2e34a45e99f3137a0eb102176a82bc4: decode and validate a base64 SAML response, returning a verified assertion (pure)
func (p *SAMLProvider) ParseResponse(samlResponse string) (*saml.Assertion, error) {
	// Decode base64-encoded SAML response
	decodedResponse, err := base64.StdEncoding.DecodeString(samlResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 SAML response: %w", err)
	}

	// Use the service provider's ParseXMLResponse which:
	// 1. Validates the XML structure
	// 2. Verifies the digital signature (response or assertion)
	// 3. Validates conditions (NotBefore, NotOnOrAfter)
	// 4. Validates audience restrictions
	// 5. Validates issuer matches expected IdP
	// 6. Validates destination URL
	// 7. Handles encrypted assertions (decrypts if SP key provided)
	//
	// We use an empty URL since we don't have access to the original request URL here.
	// The library will fall back to validating against the ACS URL configured in the SP.
	emptyURL := url.URL{}
	assertion, err := p.serviceProvider.ParseXMLResponse(decodedResponse, []string{}, emptyURL)
	if err != nil {
		// crewjam/saml wraps validation errors in InvalidResponseError which returns
		// a generic "Authentication failed" from Error(). Extract PrivateErr for diagnostics.
		logger := slogging.Get()
		var ire *saml.InvalidResponseError
		if errors.As(err, &ire) {
			logger.Error("SAML response validation failed - PrivateErr: %v, Response (first 500 chars): %.500s", ire.PrivateErr, ire.Response)
		}
		return nil, fmt.Errorf("failed to validate SAML response: %w", err)
	}

	return assertion, nil
}

// ExtractUserInfoFromAssertion extracts user info from a SAML assertion
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: parse user identity attributes from a verified SAML assertion (pure)
func (p *SAMLProvider) ExtractUserInfoFromAssertion(assertion *saml.Assertion) (*UserInfo, error) {
	return ExtractUserInfo(assertion, p.config)
}

// GetMetadata returns the service provider metadata
// SEM@0dcfe60d024e5cd95a40b61fc489253e670af6ce: return the SP EntityDescriptor metadata struct (pure)
func (p *SAMLProvider) GetMetadata() (*saml.EntityDescriptor, error) {
	return p.serviceProvider.Metadata(), nil
}

// GetMetadataXML returns the service provider metadata as XML
// SEM@0dcfe60d024e5cd95a40b61fc489253e670af6ce: serialize the SP metadata to indented XML bytes (pure)
func (p *SAMLProvider) GetMetadataXML() ([]byte, error) {
	metadata := p.serviceProvider.Metadata()
	return xml.MarshalIndent(metadata, "", "  ")
}

// GetConfig returns the SAML configuration
// SEM@0dcfe60d024e5cd95a40b61fc489253e670af6ce: return the SAML provider configuration (pure)
func (p *SAMLProvider) GetConfig() *SAMLConfig {
	return p.config
}

// GenerateMetadata returns the SP metadata XML string
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: return the SP metadata as an XML string (pure)
func (p *SAMLProvider) GenerateMetadata() (string, error) {
	metadata, err := p.GetMetadataXML()
	if err != nil {
		return "", err
	}
	return string(metadata), nil
}

// InitiateLogin creates a SAML authentication request
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: generate a SAML authentication URL and relay state for a new login flow (pure)
func (p *SAMLProvider) InitiateLogin(clientCallback *string) (string, string, error) {
	// Generate a random state for CSRF protection
	state := fmt.Sprintf("%d", time.Now().UnixNano())

	authURL, err := p.GetAuthorizationURL(state)
	if err != nil {
		return "", "", err
	}

	return authURL, state, nil
}

// Freshness bounds for an inbound LogoutRequest's IssueInstant. Together
// these bound the replay window for a captured (validly signed) logout
// request to roughly +/-5 minutes.
const (
	maxLogoutRequestAge       = 5 * time.Minute
	maxLogoutRequestClockSkew = 5 * time.Minute
)

// ProcessLogoutRequest handles a SAML logout request from the IdP.
//
// Binding note: this accepts the HTTP-POST binding form of the request --
// base64-encoded LogoutRequest XML carrying an enveloped XML signature. The
// HTTP-Redirect binding carries a DEFLATE-compressed payload whose signature
// lives in the SigAlg/Signature query parameters rather than inside the XML;
// that form is not supported by this method and is rejected (it fails XML
// parsing, or signature validation if inflated XML without an embedded
// signature is supplied).
//
// SECURITY: the request is only trusted after its enveloped XML signature has
// been verified against the IdP signing certificate(s) from metadata.
// Unsigned requests are rejected outright.
// SEM@bbf626f42894b3914617abadecf11fccd86a73b1: validate signature and fields of a base64 SAML logout request from the IdP (pure)
func (p *SAMLProvider) ProcessLogoutRequest(samlRequest string) (*saml.LogoutRequest, error) {
	// Decode base64-encoded SAML logout request
	decodedRequest, err := base64.StdEncoding.DecodeString(samlRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 SAML logout request: %w", err)
	}

	// SECURITY: bound input size before XML parsing (mirrors fetchIDPMetadata)
	const maxLogoutRequestSize = 102400 // 100KB
	if len(decodedRequest) > maxLogoutRequestSize {
		return nil, fmt.Errorf("logout request exceeds maximum size of %d bytes", maxLogoutRequestSize)
	}

	// Parse into an etree document so the XML signature can be verified
	// against the exact element that was signed.
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(decodedRequest); err != nil {
		return nil, fmt.Errorf("failed to parse SAML logout request XML: %w", err)
	}
	root := doc.Root()
	if root == nil || root.Tag != "LogoutRequest" {
		return nil, fmt.Errorf("unexpected root element in SAML logout request")
	}

	// SECURITY: verify the enveloped XML signature against the IdP signing
	// certificates from metadata before trusting any field of the request.
	// goxmldsig's Validate requires the Signature's Reference URI to resolve
	// to the element being validated and returns only the signed subtree,
	// which rejects both signature stripping (no signature -> error) and XML
	// signature wrapping (signature over a different element -> error).
	certs, err := p.idpSigningCerts()
	if err != nil {
		return nil, fmt.Errorf("cannot validate logout request signature: %w", err)
	}
	validationContext := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{
		Roots: certs,
	})
	validationContext.IdAttribute = "ID"
	signedRoot, err := validationContext.Validate(root)
	if err != nil {
		return nil, fmt.Errorf("SAML logout request signature validation failed (unsigned requests are rejected): %w", err)
	}

	// Unmarshal only the signature-verified subtree.
	signedDoc := etree.NewDocument()
	signedDoc.SetRoot(signedRoot)
	verifiedXML, err := signedDoc.WriteToBytes()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize verified logout request: %w", err)
	}
	logoutRequest := &saml.LogoutRequest{}
	if err := xml.Unmarshal(verifiedXML, logoutRequest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal SAML logout request: %w", err)
	}

	// Validate the issuer is the IdP we trust
	if logoutRequest.Issuer == nil || logoutRequest.Issuer.Value != p.idpMetadata.EntityID {
		return nil, fmt.Errorf("invalid or missing issuer in logout request")
	}

	if logoutRequest.NameID == nil {
		return nil, fmt.Errorf("missing NameID in logout request")
	}

	// SECURITY: IssueInstant freshness bounds the replay window for a
	// captured signed request.
	now := time.Now()
	if logoutRequest.IssueInstant.IsZero() {
		return nil, fmt.Errorf("missing IssueInstant in logout request")
	}
	if logoutRequest.IssueInstant.After(now.Add(maxLogoutRequestClockSkew)) {
		return nil, fmt.Errorf("logout request IssueInstant is in the future")
	}
	if now.Sub(logoutRequest.IssueInstant) > maxLogoutRequestAge+maxLogoutRequestClockSkew {
		return nil, fmt.Errorf("logout request IssueInstant is too old")
	}

	// SECURITY: a validly signed LogoutRequest destined for a different SP
	// must not be accepted here.
	if logoutRequest.Destination != "" && p.config.SLOURL != "" && logoutRequest.Destination != p.config.SLOURL {
		return nil, fmt.Errorf("logout request Destination does not match configured SLO URL")
	}

	// Return the parsed and signature-verified logout request
	// The caller should:
	// 1. Invalidate the user's session based on NameID
	// 2. Send a LogoutResponse back to the IdP
	return logoutRequest, nil
}

// idpSigningCerts extracts the IdP signing certificates from the IdP
// metadata (mirrors crewjam/saml's unexported getIDPSigningCerts).
// Certificates marked use="signing" are preferred; if none are marked,
// certificates with no use attribute are accepted, per the metadata spec.
// SEM@bbf626f42894b3914617abadecf11fccd86a73b1: extract and parse IdP signing certificates from metadata (pure)
func (p *SAMLProvider) idpSigningCerts() ([]*x509.Certificate, error) {
	var certStrs []string
	for _, idpSSODescriptor := range p.idpMetadata.IDPSSODescriptors {
		for _, keyDescriptor := range idpSSODescriptor.KeyDescriptors {
			if keyDescriptor.Use == "signing" {
				for _, cert := range keyDescriptor.KeyInfo.X509Data.X509Certificates {
					certStrs = append(certStrs, cert.Data)
				}
			}
		}
	}
	if len(certStrs) == 0 {
		for _, idpSSODescriptor := range p.idpMetadata.IDPSSODescriptors {
			for _, keyDescriptor := range idpSSODescriptor.KeyDescriptors {
				if keyDescriptor.Use == "" {
					for _, cert := range keyDescriptor.KeyInfo.X509Data.X509Certificates {
						certStrs = append(certStrs, cert.Data)
					}
				}
			}
		}
	}
	if len(certStrs) == 0 {
		return nil, fmt.Errorf("no IdP signing certificates found in metadata")
	}

	certs := make([]*x509.Certificate, 0, len(certStrs))
	for _, certStr := range certStrs {
		// Metadata certificates are base64 DER, frequently wrapped/indented.
		cleaned := strings.Map(func(r rune) rune {
			switch r {
			case ' ', '\t', '\n', '\r':
				return -1
			}
			return r
		}, certStr)
		certBytes, err := base64.StdEncoding.DecodeString(cleaned)
		if err != nil {
			return nil, fmt.Errorf("failed to decode IdP certificate from metadata: %w", err)
		}
		parsed, err := x509.ParseCertificate(certBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse IdP certificate from metadata: %w", err)
		}
		certs = append(certs, parsed)
	}
	return certs, nil
}

// MakeLogoutResponse creates a SAML logout response
// SEM@f4f52c4150b0113a1db8646f6327c385ab0ec2b1: build and base64-encode a SAML logout response for the IdP (pure)
func (p *SAMLProvider) MakeLogoutResponse(inResponseTo string, status string) (string, error) {
	// Create logout response
	logoutResponse := &saml.LogoutResponse{
		ID:           fmt.Sprintf("id-%d", time.Now().UnixNano()),
		InResponseTo: inResponseTo,
		Version:      "2.0",
		IssueInstant: time.Now(),
		Destination:  p.serviceProvider.GetSLOBindingLocation(saml.HTTPPostBinding),
		Issuer: &saml.Issuer{
			Value: p.serviceProvider.EntityID,
		},
		Status: saml.Status{
			StatusCode: saml.StatusCode{
				Value: status, // e.g., "urn:oasis:names:tc:SAML:2.0:status:Success"
			},
		},
	}

	// Marshal to XML
	responseXML, err := xml.Marshal(logoutResponse)
	if err != nil {
		return "", fmt.Errorf("failed to marshal logout response: %w", err)
	}

	// Base64 encode
	encodedResponse := base64.StdEncoding.EncodeToString(responseXML)
	return encodedResponse, nil
}

// loadKeyAndCert loads the SP private key and certificate
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: load and parse the SP RSA private key and X.509 certificate from config (pure)
func loadKeyAndCert(config *SAMLConfig) (*rsa.PrivateKey, *x509.Certificate, error) {
	var keyPEM, certPEM []byte
	var err error

	// Load private key
	switch {
	case config.SPPrivateKey != "":
		keyPEM = []byte(config.SPPrivateKey)
	case config.SPPrivateKeyPath != "":
		keyPEM, err = os.ReadFile(config.SPPrivateKeyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read private key file: %w", err)
		}
	default:
		return nil, nil, fmt.Errorf("no SP private key configured")
	}

	// Load certificate
	switch {
	case config.SPCertificate != "":
		certPEM = []byte(config.SPCertificate)
	case config.SPCertificatePath != "":
		certPEM, err = os.ReadFile(config.SPCertificatePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read certificate file: %w", err)
		}
	default:
		return nil, nil, fmt.Errorf("no SP certificate configured")
	}

	// Parse private key
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("failed to parse private key PEM")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		// Try PKCS8 format
		privateKeyInterface, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		var ok bool
		privateKey, ok = privateKeyInterface.(*rsa.PrivateKey)
		if !ok {
			return nil, nil, fmt.Errorf("private key is not RSA")
		}
	}

	// Parse certificate
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, nil, fmt.Errorf("failed to parse certificate PEM")
	}

	certificate, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return privateKey, certificate, nil
}

// fetchIDPMetadata fetches and parses IdP metadata with security validation
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: fetch and validate IdP metadata from URL or base64 XML in config
func fetchIDPMetadata(config *SAMLConfig) (*saml.EntityDescriptor, error) {
	var metadataXML []byte
	var err error

	switch {
	case config.IDPMetadataURL != "":
		// Prefer URL - fetches fresh metadata and handles certificate rotation
		metadataXML, err = fetchMetadataFromURL(config.IDPMetadataURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch metadata from URL: %w", err)
		}
	case config.IDPMetadataB64XML != "":
		// Fall back to base64-encoded metadata XML
		// This avoids shell escaping issues with XML namespace prefixes (ds:, etc.)
		metadataXML, err = base64.StdEncoding.DecodeString(config.IDPMetadataB64XML)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 IdP metadata: %w", err)
		}
	default:
		return nil, fmt.Errorf("no IdP metadata configured (set IDP_METADATA_URL or IDP_METADATA_B64XML)")
	}

	// SECURITY: Validate size before parsing (prevent XML bombs and DoS attacks)
	const maxMetadataSize = 102400 // 100KB limit
	if len(metadataXML) > maxMetadataSize {
		return nil, fmt.Errorf("metadata exceeds maximum size of %d bytes (got %d bytes)", maxMetadataSize, len(metadataXML))
	}

	// SECURITY: Validate well-formedness with strict decoder settings
	// This protects against:
	// - XML bombs (billion laughs attack)
	// - XXE attacks (external entities disabled by default in Go)
	// - Charset encoding attacks
	// - Malformed XML structures
	decoder := xml.NewDecoder(bytes.NewReader(metadataXML))
	decoder.Strict = true       // Enable strict XML parsing
	decoder.CharsetReader = nil // Disable charset conversion to prevent encoding attacks

	// Parse metadata with security settings
	metadata := &saml.EntityDescriptor{}
	if err := decoder.Decode(metadata); err != nil {
		return nil, fmt.Errorf("failed to parse IdP metadata: %w", err)
	}

	// SECURITY: Validate expected structure (ensure required fields present)
	if metadata.EntityID == "" {
		return nil, fmt.Errorf("invalid metadata: missing EntityID")
	}

	return metadata, nil
}

// fetchMetadataFromURL fetches metadata from a URL.
//
// IDP_METADATA_URL is an admin-set, runtime-mutable setting, so the fetch goes
// through the hardened client: it pins the dialed IP and refuses redirects,
// blocking an internal/private metadata URL (e.g. 169.254.169.254, RFC1918,
// loopback) at dial time (SSRF). TLS 1.2 minimum is enforced by the hardened
// client.
// SEM@e55d63794c48585aafab36880122df63ab8ab1be: fetch IdP metadata XML from a URL via a hardened SSRF-resistant HTTP client
func fetchMetadataFromURL(metadataURL string) ([]byte, error) {
	client := safehttp.NewHardenedClient(safehttp.HardenedClientOptions{
		Timeout: 30 * time.Second,
	})

	resp, err := client.Get(metadataURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata fetch returned status %d", resp.StatusCode)
	}

	metadataXML, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata response: %w", err)
	}

	return metadataXML, nil
}

// CreateMiddleware creates SAML middleware for HTTP handlers
// SEM@85ed60a219cd0aba38e90907408068f8235d4cc1: build a SAML SP middleware from provider config and IdP metadata
func (p *SAMLProvider) CreateMiddleware(opts samlsp.Options) (*samlsp.Middleware, error) {
	opts.IDPMetadata = p.idpMetadata
	opts.Key = p.serviceProvider.Key
	opts.Certificate = p.serviceProvider.Certificate
	opts.AllowIDPInitiated = p.config.AllowIDPInitiated
	opts.ForceAuthn = p.config.ForceAuthn
	// SignRequest option doesn't exist in samlsp.Options

	middleware, err := samlsp.New(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create SAML middleware: %w", err)
	}

	return middleware, nil
}
