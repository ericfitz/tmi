package secrets

import (
	"context"
	"os"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
)

// EnvProvider retrieves secrets from environment variables.
// This is the default provider when no external secrets manager is configured.
// It maps secret keys to environment variable names using a TMI_SECRET_ prefix.
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: secrets provider that reads values from TMI_SECRET_* environment variables
type EnvProvider struct {
	prefix string
}

// NewEnvProvider creates a new environment variable secrets provider
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: build an environment-variable secrets provider with the TMI_SECRET_ prefix (pure)
func NewEnvProvider() *EnvProvider {
	return &EnvProvider{
		prefix: "TMI_SECRET_",
	}
}

// GetSecret retrieves a secret from environment variables.
// It looks for an environment variable named TMI_SECRET_<KEY> (uppercase).
// For example, key "jwt_secret" maps to TMI_SECRET_JWT_SECRET.
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: fetch a secret value from the TMI_SECRET_<KEY> environment variable
func (p *EnvProvider) GetSecret(_ context.Context, key string) (string, error) {
	logger := slogging.Get()

	envKey := p.envKey(key)
	value := os.Getenv(envKey)
	if value == "" {
		logger.Debug("Secret not found in environment: %s", envKey)
		return "", ErrSecretNotFound
	}

	logger.Debug("Retrieved secret from environment: %s", envKey)
	return value, nil
}

// ListSecrets returns all TMI_SECRET_* environment variable keys
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: list all secret keys available in TMI_SECRET_* environment variables (pure)
func (p *EnvProvider) ListSecrets(_ context.Context) ([]string, error) {
	var keys []string

	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) >= 1 && strings.HasPrefix(parts[0], p.prefix) {
			// Convert back to secret key format
			key := strings.ToLower(strings.TrimPrefix(parts[0], p.prefix))
			keys = append(keys, key)
		}
	}

	return keys, nil
}

// Name returns the provider name
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: return the provider type name for the environment variable secrets provider (pure)
func (p *EnvProvider) Name() string {
	return string(ProviderTypeEnv)
}

// Close is a no-op for the environment provider
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: no-op close for the environment variable secrets provider (pure)
func (p *EnvProvider) Close() error {
	return nil
}

// envKey converts a secret key to an environment variable name
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: convert a secret key to its TMI_SECRET_<KEY> environment variable name (pure)
func (p *EnvProvider) envKey(key string) string {
	// Convert to uppercase and replace non-alphanumeric with underscores
	upper := strings.ToUpper(key)
	return p.prefix + upper
}
