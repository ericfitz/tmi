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
type EnvProvider struct {
	prefix string
}

// NewEnvProvider creates a new environment variable secrets provider
func NewEnvProvider() *EnvProvider {
	return &EnvProvider{
		prefix: "TMI_SECRET_",
	}
}

// GetSecret retrieves a secret from environment variables.
// It looks for an environment variable named TMI_SECRET_<KEY> (uppercase).
// For example, key "jwt_secret" maps to TMI_SECRET_JWT_SECRET.
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
func (p *EnvProvider) Name() string {
	return string(ProviderTypeEnv)
}

// Close is a no-op for the environment provider
func (p *EnvProvider) Close() error {
	return nil
}

// envKey converts a secret key to an environment variable name
func (p *EnvProvider) envKey(key string) string {
	// Convert to uppercase and replace non-alphanumeric with underscores
	upper := strings.ToUpper(key)
	return p.prefix + upper
}
