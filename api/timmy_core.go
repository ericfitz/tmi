package api

import (
	"context"
	"sync"

	"github.com/ericfitz/tmi/internal/config"
)

// TimmyRuntime bundles the rebuildable Timmy objects served to a request. It is
// immutable once built; TimmyCore swaps the whole struct on rebuild.
type TimmyRuntime struct {
	SessionManager *TimmySessionManager
	LLMService     *TimmyLLMService
	VectorManager  *VectorIndexManager
}

// TimmyRuntimeBuilder builds a TimmyRuntime from a resolved config. Injected so
// tests can substitute a fake builder instead of constructing real LangChainGo
// clients.
type TimmyRuntimeBuilder func(ctx context.Context, cfg config.TimmyConfig) (*TimmyRuntime, error)

// TimmyCore owns the live TimmyRuntime and rebuilds it lazily when the wiring
// hash changes. Safe for concurrent use.
type TimmyCore struct {
	provider *TimmyConfigProvider
	build    TimmyRuntimeBuilder

	mu      sync.RWMutex
	current *TimmyRuntime
	hash    string
}

// NewTimmyCore constructs a core over the given provider and builder.
func NewTimmyCore(provider *TimmyConfigProvider, build TimmyRuntimeBuilder) *TimmyCore {
	return &TimmyCore{provider: provider, build: build}
}

// Get returns the live TimmyRuntime, rebuilding it if the wiring config has
// changed since the last build. A build error is returned to the caller and
// does NOT poison the cache: the next Get retries. Callers hold the returned
// pointer for the duration of their request; a concurrent rebuild swaps the
// holder's pointer but never mutates an in-use runtime.
func (c *TimmyCore) Get(ctx context.Context) (*TimmyRuntime, error) {
	cfg := c.provider.Current(ctx)
	want := c.provider.WiringHash(cfg)

	c.mu.RLock()
	if c.current != nil && c.hash == want {
		rt := c.current
		c.mu.RUnlock()
		return rt, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check under the write lock: another goroutine may have rebuilt.
	if c.current != nil && c.hash == want {
		return c.current, nil
	}
	rt, err := c.build(ctx, cfg)
	if err != nil {
		return nil, err
	}
	c.current = rt
	c.hash = want
	return rt, nil
}
