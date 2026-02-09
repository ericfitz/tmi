// Package secrets provides a unified interface for retrieving secrets from various providers.
// This supports environment variables, AWS Secrets Manager, OCI Vault, and future providers
// like HashiCorp Vault, Azure Key Vault, and GCP Secret Manager.
package secrets

import (
	"context"
	"errors"
	"fmt"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

// Common errors
var (
	ErrSecretNotFound     = errors.New("secret not found")
	ErrProviderNotEnabled = errors.New("secrets provider not enabled")
	ErrInvalidConfig      = errors.New("invalid secrets provider configuration")
)

// Provider defines the interface for secrets providers
type Provider interface {
	// GetSecret retrieves a secret value by its key.
	// Returns ErrSecretNotFound if the secret doesn't exist.
	GetSecret(ctx context.Context, key string) (string, error)

	// ListSecrets returns a list of available secret keys.
	// This may not be supported by all providers (returns empty list if unsupported).
	ListSecrets(ctx context.Context) ([]string, error)

	// Name returns the provider's identifier (e.g., "env", "aws", "oci")
	Name() string

	// Close releases any resources held by the provider
	Close() error
}

// ProviderType represents the type of secrets provider
type ProviderType string

// Provider type constants
const (
	ProviderTypeEnv   ProviderType = "env"
	ProviderTypeAWS   ProviderType = "aws"
	ProviderTypeOCI   ProviderType = "oci"
	ProviderTypeVault ProviderType = "vault" // Future: HashiCorp Vault
	ProviderTypeAzure ProviderType = "azure" // Future: Azure Key Vault
	ProviderTypeGCP   ProviderType = "gcp"   // Future: GCP Secret Manager
)

// NewProvider creates a new secrets provider based on configuration.
// If no provider is configured, it defaults to the environment variable provider.
func NewProvider(ctx context.Context, cfg *config.SecretsConfig) (Provider, error) {
	logger := slogging.Get()

	if cfg == nil || cfg.Provider == "" {
		logger.Info("No secrets provider configured, using environment variables")
		return NewEnvProvider(), nil
	}

	providerType := ProviderType(cfg.Provider)
	logger.Info("Initializing secrets provider: %s", providerType)

	switch providerType {
	case ProviderTypeEnv:
		return NewEnvProvider(), nil

	case ProviderTypeAWS:
		if cfg.AWSRegion == "" || cfg.AWSSecretName == "" {
			return nil, fmt.Errorf("%w: AWS secrets provider requires region and secret name", ErrInvalidConfig)
		}
		return NewAWSProvider(ctx, cfg.AWSRegion, cfg.AWSSecretName)

	case ProviderTypeOCI:
		if cfg.OCICompartmentID == "" || cfg.OCIVaultID == "" {
			return nil, fmt.Errorf("%w: OCI secrets provider requires compartment ID and vault ID", ErrInvalidConfig)
		}
		return NewOCIProvider(ctx, cfg.OCICompartmentID, cfg.OCIVaultID, cfg.OCISecretName)

	case ProviderTypeVault:
		return nil, fmt.Errorf("%w: HashiCorp Vault provider not yet implemented", ErrProviderNotEnabled)

	case ProviderTypeAzure:
		return nil, fmt.Errorf("%w: Azure Key Vault provider not yet implemented", ErrProviderNotEnabled)

	case ProviderTypeGCP:
		return nil, fmt.Errorf("%w: GCP Secret Manager provider not yet implemented", ErrProviderNotEnabled)

	default:
		return nil, fmt.Errorf("%w: unknown provider type: %s", ErrInvalidConfig, cfg.Provider)
	}
}

// SecretKeys contains the standard secret key names used by TMI
var SecretKeys = struct {
	JWTSecret        string
	DatabasePassword string
	RedisPassword    string
	OAuthGitHub      struct {
		ClientID     string
		ClientSecret string
	}
	OAuthGoogle struct {
		ClientID     string
		ClientSecret string
	}
	OAuthMicrosoft struct {
		ClientID     string
		ClientSecret string
	}
	SettingsEncryptionKey         string
	SettingsEncryptionPreviousKey string
	SettingsEncryptionContextID   string
}{
	JWTSecret:        "jwt_secret",
	DatabasePassword: "database_password",
	RedisPassword:    "redis_password",
	OAuthGitHub: struct {
		ClientID     string
		ClientSecret string
	}{
		ClientID:     "oauth_github_client_id",
		ClientSecret: "oauth_github_client_secret",
	},
	OAuthGoogle: struct {
		ClientID     string
		ClientSecret string
	}{
		ClientID:     "oauth_google_client_id",
		ClientSecret: "oauth_google_client_secret",
	},
	OAuthMicrosoft: struct {
		ClientID     string
		ClientSecret string
	}{
		ClientID:     "oauth_microsoft_client_id",
		ClientSecret: "oauth_microsoft_client_secret",
	},
	SettingsEncryptionKey:         "settings_encryption_key",
	SettingsEncryptionPreviousKey: "settings_encryption_previous_key",
	SettingsEncryptionContextID:   "settings_encryption_context_id",
}
