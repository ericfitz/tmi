package api

import (
	"context"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/periodic"
)

// QuotaCache provides in-memory caching for quota lookups with TTL
// SEM@f5e41f0bdd3e5075ef62036d28d486bd0ef0286b: in-memory TTL cache for user API and webhook quota lookups (mutates shared state)
type QuotaCache struct {
	userAPIQuotas map[string]*cachedUserAPIQuota
	webhookQuotas map[string]*cachedWebhookQuota
	mutex         sync.RWMutex
	ttl           time.Duration
	cleanupTicker *time.Ticker
	stopCleanup   chan bool
}

// SEM@f5e41f0bdd3e5075ef62036d28d486bd0ef0286b: internal cache entry pairing a user API quota with its expiry time
type cachedUserAPIQuota struct {
	quota     UserAPIQuota
	expiresAt time.Time
}

// SEM@b985c4183889477cf4e9dca2fc574b3cbaececec: internal cache entry pairing a webhook quota with its expiry time
type cachedWebhookQuota struct {
	quota     DBWebhookQuota
	expiresAt time.Time
}

// NewQuotaCache creates a new quota cache with the specified TTL
// SEM@f5e41f0bdd3e5075ef62036d28d486bd0ef0286b: build a quota cache with TTL and start its background cleanup goroutine (mutates shared state)
func NewQuotaCache(ttl time.Duration) *QuotaCache {
	cache := &QuotaCache{
		userAPIQuotas: make(map[string]*cachedUserAPIQuota),
		webhookQuotas: make(map[string]*cachedWebhookQuota),
		ttl:           ttl,
		stopCleanup:   make(chan bool),
	}

	// Start cleanup goroutine
	cache.cleanupTicker = time.NewTicker(ttl)
	go cache.cleanupExpired()

	return cache
}

// GetUserAPIQuota retrieves a user API quota from cache or store
// SEM@f02caa14cf5cd68c437a2bddba77d5f8f0d17f8c: fetch a user API quota from cache, falling back to the store on miss (reads DB)
func (c *QuotaCache) GetUserAPIQuota(ctx context.Context, userID string, store UserAPIQuotaStoreInterface) UserAPIQuota {
	c.mutex.RLock()
	cached, exists := c.userAPIQuotas[userID]
	c.mutex.RUnlock()

	// Check if cached and not expired
	if exists && time.Now().Before(cached.expiresAt) {
		return cached.quota
	}

	// Fetch from store
	quota := store.GetOrDefault(ctx, userID)

	// Cache it
	c.mutex.Lock()
	c.userAPIQuotas[userID] = &cachedUserAPIQuota{
		quota:     quota,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mutex.Unlock()

	return quota
}

// GetWebhookQuota retrieves a webhook quota from cache or store
// SEM@a3e8f5e791cb2d0db34a3485d770fb2aa7cdaaf5: fetch a webhook quota from cache, falling back to the store on miss (reads DB)
func (c *QuotaCache) GetWebhookQuota(ctx context.Context, userID string, store WebhookQuotaStoreInterface) DBWebhookQuota {
	c.mutex.RLock()
	cached, exists := c.webhookQuotas[userID]
	c.mutex.RUnlock()

	// Check if cached and not expired
	if exists && time.Now().Before(cached.expiresAt) {
		return cached.quota
	}

	// Fetch from store
	quota := store.GetOrDefault(ctx, userID)

	// Cache it
	c.mutex.Lock()
	c.webhookQuotas[userID] = &cachedWebhookQuota{
		quota:     quota,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mutex.Unlock()

	return quota
}

// InvalidateUserAPIQuota removes a user API quota from cache
// SEM@f5e41f0bdd3e5075ef62036d28d486bd0ef0286b: evict a user's API quota entry from cache (mutates shared state)
func (c *QuotaCache) InvalidateUserAPIQuota(userID string) {
	c.mutex.Lock()
	delete(c.userAPIQuotas, userID)
	c.mutex.Unlock()
}

// InvalidateWebhookQuota removes a webhook quota from cache
// SEM@f5e41f0bdd3e5075ef62036d28d486bd0ef0286b: evict a user's webhook quota entry from cache (mutates shared state)
func (c *QuotaCache) InvalidateWebhookQuota(userID string) {
	c.mutex.Lock()
	delete(c.webhookQuotas, userID)
	c.mutex.Unlock()
}

// InvalidateAll clears all cached quotas
// SEM@f5e41f0bdd3e5075ef62036d28d486bd0ef0286b: evict all quota entries from cache (mutates shared state)
func (c *QuotaCache) InvalidateAll() {
	c.mutex.Lock()
	c.userAPIQuotas = make(map[string]*cachedUserAPIQuota)
	c.webhookQuotas = make(map[string]*cachedWebhookQuota)
	c.mutex.Unlock()
}

// cleanupExpired removes expired entries from cache
// SEM@f5e41f0bdd3e5075ef62036d28d486bd0ef0286b: periodically evict expired quota entries from cache on a ticker (mutates shared state)
func (c *QuotaCache) cleanupExpired() {
	periodic.RunCleanup(c.cleanupTicker, c.stopCleanup, func() {
		now := time.Now()
		c.mutex.Lock()
		defer c.mutex.Unlock()

		// Clean up user API quotas
		for userID, cached := range c.userAPIQuotas {
			if now.After(cached.expiresAt) {
				delete(c.userAPIQuotas, userID)
			}
		}

		// Clean up webhook quotas
		for userID, cached := range c.webhookQuotas {
			if now.After(cached.expiresAt) {
				delete(c.webhookQuotas, userID)
			}
		}
	})
}

// Stop stops the cleanup goroutine
// SEM@f5e41f0bdd3e5075ef62036d28d486bd0ef0286b: stop the quota cache cleanup goroutine (mutates shared state)
func (c *QuotaCache) Stop() {
	c.cleanupTicker.Stop()
	close(c.stopCleanup)
}

// Global quota cache instance (60 second TTL for dynamic adjustment)
var GlobalQuotaCache *QuotaCache

// InitializeQuotaCache initializes the global quota cache
// SEM@f5e41f0bdd3e5075ef62036d28d486bd0ef0286b: initialize the global quota cache singleton with the given TTL (mutates shared state)
func InitializeQuotaCache(ttl time.Duration) {
	GlobalQuotaCache = NewQuotaCache(ttl)
}
