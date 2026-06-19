// Package configsecrets bridges internal/config and internal/secrets.
//
// internal/config defines the config.SecretResolver interface but cannot
// implement the vault leg itself: internal/secrets imports internal/config
// (for config.SecretsConfig), so internal/config importing internal/secrets
// would create an import cycle. This package sits above both and supplies the
// concrete adapter that delegates config.SecretResolver.ResolveVault to a
// secrets.Provider.
package configsecrets

import (
	"context"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/secrets"
)

// ProviderResolver adapts a secrets.Provider to the config.SecretResolver
// interface so that config.ResolveSecretValue can dereference vault:// secret
// references at startup.
// SEM@b583a71af02ca00e2c408d9d52e1e41f514df3ff: adapter that bridges a secrets.Provider to the config.SecretResolver interface (pure)
type ProviderResolver struct {
	provider secrets.Provider
}

// NewProviderResolver wraps a secrets.Provider as a config.SecretResolver.
// SEM@b583a71af02ca00e2c408d9d52e1e41f514df3ff: build a ProviderResolver wrapping the given secrets provider (pure)
func NewProviderResolver(provider secrets.Provider) *ProviderResolver {
	return &ProviderResolver{provider: provider}
}

// ResolveVault dereferences a vault:// secret locator path through the
// underlying secrets provider's GetSecret. It satisfies config.SecretResolver.
// SEM@b583a71af02ca00e2c408d9d52e1e41f514df3ff: fetch a secret value by vault path through the underlying secrets provider
func (r *ProviderResolver) ResolveVault(ctx context.Context, path string) (string, error) {
	return r.provider.GetSecret(ctx, path)
}

// compile-time assurance that ProviderResolver satisfies config.SecretResolver.
var _ config.SecretResolver = (*ProviderResolver)(nil)
