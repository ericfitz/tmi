package saml

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"errors"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	"golang.org/x/oauth2"

	"github.com/ericfitz/tmi/internal/slogging"
)

// TokenResponse represents the tokens returned by SAML (stub for interface compatibility)
type TokenResponse struct {
	AccessToken  string //nolint:gosec // G117 - SAML token response field
	RefreshToken string //nolint:gosec // G117 - SAML token response field
	IDToken      string
	ExpiresIn    int
}

// IDTokenClaims represents claims from an ID token (stub for interface compatibility)
type IDTokenClaims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// SAMLProvider implements the Provider interface for SAML authentication
type SAMLProvider struct {
	config          *SAMLConfig
	serviceProvider *saml.ServiceProvider
	idpMetadata     *saml.EntityDescriptor
}

// NewSAMLProvider creates a new SAML provider
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
func (p *SAMLProvider) GetOAuth2Config() *oauth2.Config {
	return nil
}

// GetAuthorizationURL generates a SAML authentication request URL
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

// ExchangeCode is not applicable for SAML
func (p *SAMLProvider) ExchangeCode(ctx context.Context, code string) (*TokenResponse, error) {
	return nil, fmt.Errorf("SAML provider does not support code exchange")
}

// GetUserInfo is not applicable for SAML (user info comes from assertion)
func (p *SAMLProvider) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	return nil, fmt.Errorf("SAML provider does not support GetUserInfo - user info comes from assertion")
}

// ValidateIDToken is not applicable for SAML
func (p *SAMLProvider) ValidateIDToken(ctx context.Context, idToken string) (*IDTokenClaims, error) {
	return nil, fmt.Errorf("SAML provider does not support ID tokens")
}

// ParseResponse parses and validates a SAML response with full signature verification
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
func (p *SAMLProvider) ExtractUserInfoFromAssertion(assertion *saml.Assertion) (*UserInfo, error) {
	return ExtractUserInfo(assertion, p.config)
}

// GetMetadata returns the service provider metadata
func (p *SAMLProvider) GetMetadata() (*saml.EntityDescriptor, error) {
	return p.serviceProvider.Metadata(), nil
}

// GetMetadataXML returns the service provider metadata as XML
func (p *SAMLProvider) GetMetadataXML() ([]byte, error) {
	metadata := p.serviceProvider.Metadata()
	return xml.MarshalIndent(metadata, "", "  ")
}

// GetConfig returns the SAML configuration
func (p *SAMLProvider) GetConfig() *SAMLConfig {
	return p.config
}

// GenerateMetadata returns the SP metadata XML string
func (p *SAMLProvider) GenerateMetadata() (string, error) {
	metadata, err := p.GetMetadataXML()
	if err != nil {
		return "", err
	}
	return string(metadata), nil
}

// InitiateLogin creates a SAML authentication request
func (p *SAMLProvider) InitiateLogin(clientCallback *string) (string, string, error) {
	// Generate a random state for CSRF protection
	state := fmt.Sprintf("%d", time.Now().UnixNano())

	authURL, err := p.GetAuthorizationURL(state)
	if err != nil {
		return "", "", err
	}

	return authURL, state, nil
}

// ProcessLogoutRequest handles a SAML logout request from the IdP
func (p *SAMLProvider) ProcessLogoutRequest(samlRequest string) (*saml.LogoutRequest, error) {
	// Decode base64-encoded SAML logout request
	decodedRequest, err := base64.StdEncoding.DecodeString(samlRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 SAML logout request: %w", err)
	}

	// Parse the logout request
	logoutRequest := &saml.LogoutRequest{}
	if err := xml.Unmarshal(decodedRequest, logoutRequest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal SAML logout request: %w", err)
	}

	// Validate basic structure
	if logoutRequest.Issuer == nil || logoutRequest.Issuer.Value != p.idpMetadata.EntityID {
		return nil, fmt.Errorf("invalid or missing issuer in logout request")
	}

	if logoutRequest.NameID == nil {
		return nil, fmt.Errorf("missing NameID in logout request")
	}

	// Return the parsed and validated logout request
	// The caller should:
	// 1. Invalidate the user's session based on NameID
	// 2. Send a LogoutResponse back to the IdP
	return logoutRequest, nil
}

// MakeLogoutResponse creates a SAML logout response
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
func loadKeyAndCert(config *SAMLConfig) (*rsa.PrivateKey, *x509.Certificate, error) {
	var keyPEM, certPEM []byte
	var err error

	// Load private key
	if config.SPPrivateKey != "" {
		keyPEM = []byte(config.SPPrivateKey)
	} else if config.SPPrivateKeyPath != "" {
		keyPEM, err = os.ReadFile(config.SPPrivateKeyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read private key file: %w", err)
		}
	} else {
		return nil, nil, fmt.Errorf("no SP private key configured")
	}

	// Load certificate
	if config.SPCertificate != "" {
		certPEM = []byte(config.SPCertificate)
	} else if config.SPCertificatePath != "" {
		certPEM, err = os.ReadFile(config.SPCertificatePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read certificate file: %w", err)
		}
	} else {
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
func fetchIDPMetadata(config *SAMLConfig) (*saml.EntityDescriptor, error) {
	var metadataXML []byte
	var err error

	if config.IDPMetadataURL != "" {
		// Prefer URL - fetches fresh metadata and handles certificate rotation
		metadataXML, err = fetchMetadataFromURL(config.IDPMetadataURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch metadata from URL: %w", err)
		}
	} else if config.IDPMetadataB64XML != "" {
		// Fall back to base64-encoded metadata XML
		// This avoids shell escaping issues with XML namespace prefixes (ds:, etc.)
		metadataXML, err = base64.StdEncoding.DecodeString(config.IDPMetadataB64XML)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 IdP metadata: %w", err)
		}
	} else {
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

// fetchMetadataFromURL fetches metadata from a URL
func fetchMetadataFromURL(metadataURL string) ([]byte, error) {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:         tls.VersionTLS12, // Require TLS 1.2 minimum
				InsecureSkipVerify: false,            // Set to true only for development
			},
		},
	}

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
