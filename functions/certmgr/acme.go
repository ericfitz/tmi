package main

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"time"

	"golang.org/x/crypto/acme"
)

// ACMEClient wraps the ACME client for Let's Encrypt operations
type ACMEClient struct {
	client    *acme.Client
	directory string
	email     string
}

// Certificate represents a TLS certificate with its private key
type Certificate struct {
	CertificatePEM string
	PrivateKeyPEM  string
	IssuerPEM      string
	NotAfter       time.Time
}

// NewACMEClient creates a new ACME client
func NewACMEClient(directory, email string, accountKey crypto.Signer) *ACMEClient {
	client := &acme.Client{
		Key:          accountKey,
		DirectoryURL: directory,
	}

	return &ACMEClient{
		client:    client,
		directory: directory,
		email:     email,
	}
}

// GenerateAccountKey generates a new ECDSA private key for the ACME account
func GenerateAccountKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// EncodeAccountKey encodes an ECDSA private key to PEM format
func EncodeAccountKey(key *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", fmt.Errorf("failed to marshal account key: %w", err)
	}

	block := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: der,
	}

	return string(pem.EncodeToMemory(block)), nil
}

// DecodeAccountKey decodes a PEM-encoded ECDSA private key
func DecodeAccountKey(pemData string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	return x509.ParseECPrivateKey(block.Bytes)
}

// RegisterAccount registers or retrieves an existing ACME account
func (c *ACMEClient) RegisterAccount(ctx context.Context) error {
	account := &acme.Account{
		Contact: []string{"mailto:" + c.email},
	}

	_, err := c.client.Register(ctx, account, acme.AcceptTOS)
	if err != nil && err != acme.ErrAccountAlreadyExists {
		return fmt.Errorf("failed to register ACME account: %w", err)
	}

	return nil
}

// RequestCertificate requests a new certificate using DNS-01 challenge
// It returns the DNS challenge token that must be set as a TXT record
func (c *ACMEClient) RequestCertificate(ctx context.Context, domain string) (*acme.Authorization, *acme.Challenge, error) {
	// Create a new order
	order, err := c.client.AuthorizeOrder(ctx, acme.DomainIDs(domain))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create order: %w", err)
	}

	// Get the authorization
	if len(order.AuthzURLs) == 0 {
		return nil, nil, fmt.Errorf("no authorization URLs in order")
	}

	auth, err := c.client.GetAuthorization(ctx, order.AuthzURLs[0])
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get authorization: %w", err)
	}

	// Find the DNS-01 challenge
	var dnsChallenge *acme.Challenge
	for _, ch := range auth.Challenges {
		if ch.Type == "dns-01" {
			dnsChallenge = ch
			break
		}
	}

	if dnsChallenge == nil {
		return nil, nil, fmt.Errorf("no DNS-01 challenge found")
	}

	return auth, dnsChallenge, nil
}

// GetDNSChallengeRecord returns the TXT record value for the DNS-01 challenge
func (c *ACMEClient) GetDNSChallengeRecord(challenge *acme.Challenge) (string, error) {
	return c.client.DNS01ChallengeRecord(challenge.Token)
}

// AcceptChallenge notifies the ACME server that the challenge is ready
func (c *ACMEClient) AcceptChallenge(ctx context.Context, challenge *acme.Challenge) error {
	_, err := c.client.Accept(ctx, challenge)
	return err
}

// WaitForAuthorization waits for the authorization to be valid
func (c *ACMEClient) WaitForAuthorization(ctx context.Context, auth *acme.Authorization) error {
	_, err := c.client.WaitAuthorization(ctx, auth.URI)
	return err
}

// FinalizeCertificate finalizes the order and retrieves the certificate
func (c *ACMEClient) FinalizeCertificate(ctx context.Context, domain string) (*Certificate, error) {
	// Generate a new private key for the certificate
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate certificate key: %w", err)
	}

	// Create a CSR
	csr := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: domain,
		},
		DNSNames: []string{domain},
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csr, certKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CSR: %w", err)
	}

	// Create a new order and finalize it
	order, err := c.client.AuthorizeOrder(ctx, acme.DomainIDs(domain))
	if err != nil {
		return nil, fmt.Errorf("failed to create order for finalization: %w", err)
	}

	// Wait for order to be ready
	order, err = c.client.WaitOrder(ctx, order.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for order: %w", err)
	}

	// Finalize the order
	certChain, _, err := c.client.CreateOrderCert(ctx, order.FinalizeURL, csrDER, true)
	if err != nil {
		return nil, fmt.Errorf("failed to finalize order: %w", err)
	}

	// Parse the certificate chain
	if len(certChain) == 0 {
		return nil, fmt.Errorf("empty certificate chain")
	}

	// Parse the first certificate to get expiry
	cert, err := x509.ParseCertificate(certChain[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Encode certificate chain to PEM
	var certPEM, issuerPEM string
	for i, certDER := range certChain {
		block := &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: certDER,
		}
		if i == 0 {
			certPEM = string(pem.EncodeToMemory(block))
		} else {
			issuerPEM += string(pem.EncodeToMemory(block))
		}
	}

	// Encode private key to PEM
	keyDER, err := x509.MarshalECPrivateKey(certKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	keyBlock := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	}
	keyPEM := string(pem.EncodeToMemory(keyBlock))

	return &Certificate{
		CertificatePEM: certPEM,
		PrivateKeyPEM:  keyPEM,
		IssuerPEM:      issuerPEM,
		NotAfter:       cert.NotAfter,
	}, nil
}
