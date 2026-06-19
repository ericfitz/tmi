package api

import (
	"sync"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

// ContentOAuthProviderRegistry is a thread-safe registry mapping provider id
// to a ContentOAuthProvider instance.
// SEM@95c7b93f8264174c0c95a133b5e0fc17b05d6594: thread-safe registry mapping provider ID to a ContentOAuthProvider (struct)
type ContentOAuthProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]ContentOAuthProvider
}

// NewContentOAuthProviderRegistry creates a new, empty ContentOAuthProviderRegistry.
// SEM@95c7b93f8264174c0c95a133b5e0fc17b05d6594: build an empty, thread-safe ContentOAuthProviderRegistry (pure)
func NewContentOAuthProviderRegistry() *ContentOAuthProviderRegistry {
	return &ContentOAuthProviderRegistry{
		providers: make(map[string]ContentOAuthProvider),
	}
}

// Register adds or replaces the provider in the registry.
// It is safe to call concurrently.
// SEM@95c7b93f8264174c0c95a133b5e0fc17b05d6594: register or replace a content OAuth provider by its ID (mutates shared state)
func (r *ContentOAuthProviderRegistry) Register(p ContentOAuthProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.ID()] = p
}

// Get returns the provider with the given id, and whether it was found.
// It is safe to call concurrently.
// SEM@95c7b93f8264174c0c95a133b5e0fc17b05d6594: fetch a registered content OAuth provider by ID; returns false when not found (pure)
func (r *ContentOAuthProviderRegistry) Get(id string) (ContentOAuthProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
}

// IDs returns a snapshot of the registered provider ids.
// The order of the returned slice is not guaranteed.
// SEM@95c7b93f8264174c0c95a133b5e0fc17b05d6594: list all registered provider IDs as an unordered snapshot (pure)
func (r *ContentOAuthProviderRegistry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	return ids
}

// LoadContentOAuthRegistryFromConfig builds a ContentOAuthProviderRegistry from
// the given config, registering an appropriate ContentOAuthProvider for each
// enabled entry. Providers with Enabled == false are skipped.
//
// Provider-specific implementations are selected by id when they need to
// override BaseContentOAuthProvider behavior (e.g. Confluence augments
// FetchAccountInfo with the matched accessible-resources site URL). All
// other ids fall back to BaseContentOAuthProvider.
//
// validator is the URIValidator used to gate outbound OAuth and
// userinfo/accessible-resources calls. It MUST be non-nil; in production it
// is built from the operator's content_oauth allowlist.
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: build and populate a ContentOAuthProviderRegistry from config, skipping disabled providers
func LoadContentOAuthRegistryFromConfig(cfg config.ContentOAuthConfig, validator *URIValidator) (*ContentOAuthProviderRegistry, error) {
	logger := slogging.Get()
	r := NewContentOAuthProviderRegistry()
	for id, p := range cfg.Providers {
		if !p.Enabled {
			continue
		}
		r.Register(buildContentOAuthProvider(id, p, validator))
		logger.Info("registered content OAuth provider id=%s", id)
	}
	return r, nil
}

// buildContentOAuthProvider returns the appropriate ContentOAuthProvider
// implementation for a given provider id. Confluence wraps the base provider
// to upgrade the account label using Atlassian's accessible-resources endpoint;
// Microsoft wraps the base provider as a stable extension point for future
// Graph-specific behavior; other providers use the base provider directly.
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: select and construct the correct ContentOAuthProvider implementation for a given provider ID (pure)
func buildContentOAuthProvider(id string, p config.ContentOAuthProviderConfig, validator *URIValidator) ContentOAuthProvider {
	base := NewBaseContentOAuthProvider(id, p, validator)
	switch id {
	case ProviderConfluence:
		return NewConfluenceContentOAuthProvider(base, validator)
	case ProviderMicrosoft:
		return NewMicrosoftContentOAuthProvider(base)
	}
	return base
}
