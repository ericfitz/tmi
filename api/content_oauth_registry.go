package api

import (
	"sync"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

// ContentOAuthProviderRegistry is a thread-safe registry mapping provider id
// to a ContentOAuthProvider instance.
type ContentOAuthProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]ContentOAuthProvider
}

// NewContentOAuthProviderRegistry creates a new, empty ContentOAuthProviderRegistry.
func NewContentOAuthProviderRegistry() *ContentOAuthProviderRegistry {
	return &ContentOAuthProviderRegistry{
		providers: make(map[string]ContentOAuthProvider),
	}
}

// Register adds or replaces the provider in the registry.
// It is safe to call concurrently.
func (r *ContentOAuthProviderRegistry) Register(p ContentOAuthProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.ID()] = p
}

// Get returns the provider with the given id, and whether it was found.
// It is safe to call concurrently.
func (r *ContentOAuthProviderRegistry) Get(id string) (ContentOAuthProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
}

// IDs returns a snapshot of the registered provider ids.
// The order of the returned slice is not guaranteed.
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
// the given config, registering a BaseContentOAuthProvider for each enabled
// entry. Providers with Enabled == false are skipped.
func LoadContentOAuthRegistryFromConfig(cfg config.ContentOAuthConfig) (*ContentOAuthProviderRegistry, error) {
	logger := slogging.Get()
	r := NewContentOAuthProviderRegistry()
	for id, p := range cfg.Providers {
		if !p.Enabled {
			continue
		}
		r.Register(NewBaseContentOAuthProvider(id, p))
		logger.Info("registered content OAuth provider id=%s", id)
	}
	return r, nil
}
