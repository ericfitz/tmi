package saml

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/slogging"
	"golang.org/x/oauth2"
)

// SAMLProvider implements the Provider interface for SAML authentication
type SAMLProvider struct {
	config         *SAMLConfig
	serviceProvider *saml.ServiceProvider
	idpMetadata    *saml.EntityDescriptor
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

	// Create service provider configuration
	sp := &saml.ServiceProvider{
		EntityID:          config.EntityID,
		Key:               privateKey,
		Certificate:       certificate,
		IDPMetadata:       idpMetadata,
		AcsURL:            url.URL{Scheme: "https", Host: config.ACSURL},
		MetadataURL:       url.URL{Scheme: "https", Host: config.EntityID + "/saml/metadata"},
		SloURL:            url.URL{Scheme: "https", Host: config.SLOURL},
		AllowIDPInitiated: config.AllowIDPInitiated,
		ForceAuthn:        config.ForceAuthn,
		SignRequest:       config.SignRequests,
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

	// Set relay state (similar to OAuth state parameter)
	req.RelayState = state

	// Generate redirect URL
	redirectURL, err := req.Redirect("")
	if err != nil {
		return "", fmt.Errorf("failed to create redirect URL: %w", err)
	}

	return redirectURL.String(), nil
}

// ExchangeCode is not applicable for SAML
func (p *SAMLProvider) ExchangeCode(ctx context.Context, code string) (*auth.TokenResponse, error) {
	return nil, fmt.Errorf("SAML provider does not support code exchange")
}

// GetUserInfo is not applicable for SAML (user info comes from assertion)
func (p *SAMLProvider) GetUserInfo(ctx context.Context, accessToken string) (*auth.UserInfo, error) {
	return nil, fmt.Errorf("SAML provider does not support GetUserInfo - user info comes from assertion")
}

// ValidateIDToken is not applicable for SAML
func (p *SAMLProvider) ValidateIDToken(ctx context.Context, idToken string) (*auth.IDTokenClaims, error) {
	return nil, fmt.Errorf("SAML provider does not support ID tokens")
}

// ParseResponse parses and validates a SAML response
func (p *SAMLProvider) ParseResponse(samlResponse string) (*saml.Assertion, error) {
	// Decode the SAML response
	response := &saml.Response{}
	if err := xml.Unmarshal([]byte(samlResponse), response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal SAML response: %w", err)
	}

	// Validate the response
	assertion, err := p.serviceProvider.ValidateResponse(response)
	if err != nil {
		return nil, fmt.Errorf("failed to validate SAML response: %w", err)
	}

	return assertion, nil
}

// ExtractUserInfoFromAssertion extracts user info from a SAML assertion
func (p *SAMLProvider) ExtractUserInfoFromAssertion(assertion *saml.Assertion) (*auth.UserInfo, error) {
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

// fetchIDPMetadata fetches and parses IdP metadata
func fetchIDPMetadata(config *SAMLConfig) (*saml.EntityDescriptor, error) {
	var metadataXML []byte
	var err error

	if config.IDPMetadataXML != "" {
		// Use static metadata
		metadataXML = []byte(config.IDPMetadataXML)
	} else if config.IDPMetadataURL != "" {
		// Fetch metadata from URL
		metadataXML, err = fetchMetadataFromURL(config.IDPMetadataURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch metadata from URL: %w", err)
		}
	} else {
		return nil, fmt.Errorf("no IdP metadata configured")
	}

	// Parse metadata
	metadata := &saml.EntityDescriptor{}
	if err := xml.Unmarshal(metadataXML, metadata); err != nil {
		return nil, fmt.Errorf("failed to parse IdP metadata: %w", err)
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
				InsecureSkipVerify: false, // Set to true only for development
			},
		},
	}

	resp, err := client.Get(metadataURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}
	defer resp.Body.Close()

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
	opts.SignRequest = p.config.SignRequests

	middleware, err := samlsp.New(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create SAML middleware: %w", err)
	}

	return middleware, nil
}