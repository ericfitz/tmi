package secrets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/secrets"
	"github.com/oracle/oci-go-sdk/v65/vault"
)

// OCIProvider retrieves secrets from Oracle Cloud Infrastructure (OCI) Vault.
// It can operate in two modes:
// 1. Single secret mode: secretName points to a JSON secret containing key-value pairs
// 2. Multi-secret mode: secretName is empty, and each key maps to a separate secret in the vault
type OCIProvider struct {
	secretsClient secrets.SecretsClient
	vaultClient   vault.VaultsClient
	compartmentID string
	vaultID       string
	secretName    string // If set, uses single JSON secret; otherwise uses individual secrets

	// Cache for parsed secret values
	cache    map[string]string
	cacheMu  sync.RWMutex
	cacheSet bool
}

// NewOCIProvider creates a new OCI Vault secrets provider
func NewOCIProvider(ctx context.Context, compartmentID, vaultID, secretName string) (*OCIProvider, error) {
	logger := slogging.Get()

	// Create OCI config provider (uses ~/.oci/config or instance principal)
	configProvider := common.DefaultConfigProvider()

	// Create secrets client (for reading secret values)
	secretsClient, err := secrets.NewSecretsClientWithConfigurationProvider(configProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create OCI secrets client: %w", err)
	}

	// Create vaults client (for listing secrets)
	vaultClient, err := vault.NewVaultsClientWithConfigurationProvider(configProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create OCI vault client: %w", err)
	}

	logger.Info("OCI Vault secrets provider initialized for vault: %s in compartment: %s", vaultID, compartmentID)

	return &OCIProvider{
		secretsClient: secretsClient,
		vaultClient:   vaultClient,
		compartmentID: compartmentID,
		vaultID:       vaultID,
		secretName:    secretName,
		cache:         make(map[string]string),
	}, nil
}

// GetSecret retrieves a secret value by key
func (p *OCIProvider) GetSecret(ctx context.Context, key string) (string, error) {
	// If using single JSON secret mode
	if p.secretName != "" {
		return p.getFromJSONSecret(ctx, key)
	}

	// Multi-secret mode: fetch individual secret by key
	return p.getIndividualSecret(ctx, key)
}

// getFromJSONSecret retrieves a key from a JSON-formatted secret
func (p *OCIProvider) getFromJSONSecret(ctx context.Context, key string) (string, error) {
	logger := slogging.Get()

	// Check cache first
	p.cacheMu.RLock()
	if p.cacheSet {
		if value, ok := p.cache[key]; ok {
			p.cacheMu.RUnlock()
			logger.Debug("OCI Vault cache hit for key: %s", key)
			return value, nil
		}
		p.cacheMu.RUnlock()
		logger.Debug("OCI Vault cache miss for key: %s", key)
		return "", ErrSecretNotFound
	}
	p.cacheMu.RUnlock()

	// Load secrets from OCI
	if err := p.loadJSONSecret(ctx); err != nil {
		return "", err
	}

	// Check cache again after loading
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()

	if value, ok := p.cache[key]; ok {
		logger.Debug("OCI Vault retrieved key: %s", key)
		return value, nil
	}

	return "", ErrSecretNotFound
}

// getIndividualSecret retrieves a secret by its name from the vault
func (p *OCIProvider) getIndividualSecret(ctx context.Context, key string) (string, error) {
	logger := slogging.Get()

	// First, find the secret OCID by name
	secretSummary, err := p.findSecretByName(ctx, key)
	if err != nil {
		return "", err
	}

	// Get the secret bundle (actual value)
	request := secrets.GetSecretBundleByNameRequest{
		SecretName: new(key),
		VaultId:    new(p.vaultID),
	}

	response, err := p.secretsClient.GetSecretBundleByName(ctx, request)
	if err != nil {
		return "", fmt.Errorf("failed to get secret bundle for %s: %w", key, err)
	}

	// Extract the secret value
	content, ok := response.SecretBundleContent.(secrets.Base64SecretBundleContentDetails)
	if !ok {
		return "", fmt.Errorf("unexpected secret content type for %s", key)
	}

	// Decode base64 content
	decoded, err := base64.StdEncoding.DecodeString(*content.Content)
	if err != nil {
		return "", fmt.Errorf("failed to decode secret content for %s: %w", key, err)
	}

	logger.Debug("OCI Vault retrieved secret: %s (OCID: %s)", key, *secretSummary.Id)
	return string(decoded), nil
}

// findSecretByName finds a secret summary by name in the vault
func (p *OCIProvider) findSecretByName(ctx context.Context, name string) (*vault.SecretSummary, error) {
	request := vault.ListSecretsRequest{
		CompartmentId: new(p.compartmentID),
		VaultId:       new(p.vaultID),
		Name:          new(name),
	}

	response, err := p.vaultClient.ListSecrets(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	if len(response.Items) == 0 {
		return nil, ErrSecretNotFound
	}

	return &response.Items[0], nil
}

// loadJSONSecret fetches and parses the main JSON secret
func (p *OCIProvider) loadJSONSecret(ctx context.Context) error {
	logger := slogging.Get()

	request := secrets.GetSecretBundleByNameRequest{
		SecretName: new(p.secretName),
		VaultId:    new(p.vaultID),
	}

	response, err := p.secretsClient.GetSecretBundleByName(ctx, request)
	if err != nil {
		return fmt.Errorf("failed to get OCI secret bundle: %w", err)
	}

	// Extract the secret value
	content, ok := response.SecretBundleContent.(secrets.Base64SecretBundleContentDetails)
	if !ok {
		return fmt.Errorf("unexpected secret content type")
	}

	// Decode base64 content
	decoded, err := base64.StdEncoding.DecodeString(*content.Content)
	if err != nil {
		return fmt.Errorf("failed to decode secret content: %w", err)
	}

	// Parse JSON
	var secretData map[string]string
	if err := json.Unmarshal(decoded, &secretData); err != nil {
		return fmt.Errorf("failed to parse OCI secret as JSON: %w", err)
	}

	// Update cache
	p.cacheMu.Lock()
	p.cache = secretData
	p.cacheSet = true
	p.cacheMu.Unlock()

	logger.Info("Loaded %d secrets from OCI Vault", len(secretData))
	return nil
}

// ListSecrets returns all secret keys available in the vault
func (p *OCIProvider) ListSecrets(ctx context.Context) ([]string, error) {
	// If using single JSON secret mode
	if p.secretName != "" {
		// Ensure cache is loaded
		if !p.cacheSet {
			if err := p.loadJSONSecret(ctx); err != nil {
				return nil, err
			}
		}

		p.cacheMu.RLock()
		defer p.cacheMu.RUnlock()

		keys := make([]string, 0, len(p.cache))
		for key := range p.cache {
			keys = append(keys, key)
		}
		return keys, nil
	}

	// Multi-secret mode: list all secrets in vault
	request := vault.ListSecretsRequest{
		CompartmentId: new(p.compartmentID),
		VaultId:       new(p.vaultID),
	}

	var keys []string
	for {
		response, err := p.vaultClient.ListSecrets(ctx, request)
		if err != nil {
			return nil, fmt.Errorf("failed to list OCI secrets: %w", err)
		}

		for _, secret := range response.Items {
			if secret.SecretName != nil {
				keys = append(keys, *secret.SecretName)
			}
		}

		if response.OpcNextPage == nil {
			break
		}
		request.Page = response.OpcNextPage
	}

	return keys, nil
}

// Name returns the provider name
func (p *OCIProvider) Name() string {
	return string(ProviderTypeOCI)
}

// Close releases resources
func (p *OCIProvider) Close() error {
	// OCI SDK clients don't have explicit close methods
	return nil
}

// InvalidateCache clears the cached secrets
func (p *OCIProvider) InvalidateCache() {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	p.cache = make(map[string]string)
	p.cacheSet = false
}
