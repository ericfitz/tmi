package main

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/secrets"
	"github.com/oracle/oci-go-sdk/v65/vault"
)

// VaultManager handles OCI Vault operations for certificate storage
type VaultManager struct {
	vaultClient   vault.VaultsClient
	secretsClient secrets.SecretsClient
	vaultID       string
	keyID         string
	compartmentID string
	namePrefix    string
}

// CertificateInfo contains information about a stored certificate
type CertificateInfo struct {
	Certificate   string
	PrivateKey    string
	Issuer        string
	NotAfter      time.Time
	DaysRemaining int
}

// NewVaultManager creates a new Vault manager using Resource Principal authentication
func NewVaultManager(ctx context.Context, vaultID, keyID, compartmentID, namePrefix string) (*VaultManager, error) {
	// Use Resource Principal authentication (for OCI Functions)
	provider, err := auth.ResourcePrincipalConfigurationProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create resource principal provider: %w", err)
	}

	vaultClient, err := vault.NewVaultsClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault client: %w", err)
	}

	secretsClient, err := secrets.NewSecretsClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("failed to create secrets client: %w", err)
	}

	return &VaultManager{
		vaultClient:   vaultClient,
		secretsClient: secretsClient,
		vaultID:       vaultID,
		keyID:         keyID,
		compartmentID: compartmentID,
		namePrefix:    namePrefix,
	}, nil
}

// GetAccountKey retrieves the ACME account key from the vault
func (m *VaultManager) GetAccountKey(ctx context.Context) (string, error) {
	secretName := m.namePrefix + "-acme-account-key"
	return m.getSecretValue(ctx, secretName)
}

// StoreAccountKey stores the ACME account key in the vault
func (m *VaultManager) StoreAccountKey(ctx context.Context, keyPEM string) error {
	secretName := m.namePrefix + "-acme-account-key"
	return m.updateSecretValue(ctx, secretName, keyPEM)
}

// GetCertificate retrieves the current certificate from the vault
func (m *VaultManager) GetCertificate(ctx context.Context) (*CertificateInfo, error) {
	certPEM, err := m.getSecretValue(ctx, m.namePrefix+"-certificate")
	if err != nil {
		return nil, err
	}

	if certPEM == "" {
		return nil, nil // No certificate stored yet
	}

	keyPEM, err := m.getSecretValue(ctx, m.namePrefix+"-private-key")
	if err != nil {
		return nil, err
	}

	// Parse certificate to get expiry
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	daysRemaining := int(time.Until(cert.NotAfter).Hours() / 24)

	return &CertificateInfo{
		Certificate:   certPEM,
		PrivateKey:    keyPEM,
		NotAfter:      cert.NotAfter,
		DaysRemaining: daysRemaining,
	}, nil
}

// StoreCertificate stores the certificate and private key in the vault
func (m *VaultManager) StoreCertificate(ctx context.Context, cert *Certificate) error {
	// Store certificate
	if err := m.updateSecretValue(ctx, m.namePrefix+"-certificate", cert.CertificatePEM); err != nil {
		return fmt.Errorf("failed to store certificate: %w", err)
	}

	// Store private key
	if err := m.updateSecretValue(ctx, m.namePrefix+"-private-key", cert.PrivateKeyPEM); err != nil {
		return fmt.Errorf("failed to store private key: %w", err)
	}

	return nil
}

// getSecretValue retrieves a secret value by name
func (m *VaultManager) getSecretValue(ctx context.Context, secretName string) (string, error) {
	// First, find the secret by name
	listReq := vault.ListSecretsRequest{
		CompartmentId: common.String(m.compartmentID),
		VaultId:       common.String(m.vaultID),
		Name:          common.String(secretName),
	}

	listResp, err := m.vaultClient.ListSecrets(ctx, listReq)
	if err != nil {
		return "", fmt.Errorf("failed to list secrets: %w", err)
	}

	if len(listResp.Items) == 0 {
		return "", nil // Secret doesn't exist
	}

	secretID := *listResp.Items[0].Id

	// Get the secret bundle (current version)
	getBundleReq := secrets.GetSecretBundleRequest{
		SecretId: common.String(secretID),
		Stage:    secrets.GetSecretBundleStageLatest,
	}

	getBundleResp, err := m.secretsClient.GetSecretBundle(ctx, getBundleReq)
	if err != nil {
		return "", fmt.Errorf("failed to get secret bundle: %w", err)
	}

	// Decode the base64 content
	content, ok := getBundleResp.SecretBundleContent.(secrets.Base64SecretBundleContentDetails)
	if !ok {
		return "", fmt.Errorf("unexpected secret content type")
	}

	decoded, err := base64.StdEncoding.DecodeString(*content.Content)
	if err != nil {
		return "", fmt.Errorf("failed to decode secret: %w", err)
	}

	return string(decoded), nil
}

// updateSecretValue updates a secret value by name
func (m *VaultManager) updateSecretValue(ctx context.Context, secretName, value string) error {
	// First, find the secret by name
	listReq := vault.ListSecretsRequest{
		CompartmentId: common.String(m.compartmentID),
		VaultId:       common.String(m.vaultID),
		Name:          common.String(secretName),
	}

	listResp, err := m.vaultClient.ListSecrets(ctx, listReq)
	if err != nil {
		return fmt.Errorf("failed to list secrets: %w", err)
	}

	if len(listResp.Items) == 0 {
		return fmt.Errorf("secret %s not found", secretName)
	}

	secretID := *listResp.Items[0].Id

	// Update the secret with a new version
	updateReq := vault.UpdateSecretRequest{
		SecretId: common.String(secretID),
		UpdateSecretDetails: vault.UpdateSecretDetails{
			SecretContent: vault.Base64SecretContentDetails{
				Content: common.String(base64.StdEncoding.EncodeToString([]byte(value))),
			},
		},
	}

	_, err = m.vaultClient.UpdateSecret(ctx, updateReq)
	if err != nil {
		return fmt.Errorf("failed to update secret: %w", err)
	}

	return nil
}
