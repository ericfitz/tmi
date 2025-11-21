package api

import (
	"sync"
	"time"
)

// QuotaCache provides in-memory caching for quota lookups with TTL
type QuotaCache struct {
	userAPIQuotas map[string]*cachedUserAPIQuota
	webhookQuotas map[string]*cachedWebhookQuota
	mutex         sync.RWMutex
	ttl           time.Duration
	cleanupTicker *time.Ticker
	stopCleanup   chan bool
}

type cachedUserAPIQuota struct {
	quota     UserAPIQuota
	expiresAt time.Time
}

type cachedWebhookQuota struct {
	quota     WebhookQuota
	expiresAt time.Time
}

// NewQuotaCache creates a new quota cache with the specified TTL
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
func (c *QuotaCache) GetUserAPIQuota(userID string, store UserAPIQuotaStoreInterface) UserAPIQuota {
	c.mutex.RLock()
	cached, exists := c.userAPIQuotas[userID]
	c.mutex.RUnlock()

	// Check if cached and not expired
	if exists && time.Now().Before(cached.expiresAt) {
		return cached.quota
	}

	// Fetch from store
	quota := store.GetOrDefault(userID)

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
func (c *QuotaCache) GetWebhookQuota(userID string, store WebhookQuotaStoreInterface) WebhookQuota {
	c.mutex.RLock()
	cached, exists := c.webhookQuotas[userID]
	c.mutex.RUnlock()

	// Check if cached and not expired
	if exists && time.Now().Before(cached.expiresAt) {
		return cached.quota
	}

	// Fetch from store
	quota := store.GetOrDefault(userID)

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
func (c *QuotaCache) InvalidateUserAPIQuota(userID string) {
	c.mutex.Lock()
	delete(c.userAPIQuotas, userID)
	c.mutex.Unlock()
}

// InvalidateWebhookQuota removes a webhook quota from cache
func (c *QuotaCache) InvalidateWebhookQuota(userID string) {
	c.mutex.Lock()
	delete(c.webhookQuotas, userID)
	c.mutex.Unlock()
}

// InvalidateAll clears all cached quotas
func (c *QuotaCache) InvalidateAll() {
	c.mutex.Lock()
	c.userAPIQuotas = make(map[string]*cachedUserAPIQuota)
	c.webhookQuotas = make(map[string]*cachedWebhookQuota)
	c.mutex.Unlock()
}

// cleanupExpired removes expired entries from cache
func (c *QuotaCache) cleanupExpired() {
	for {
		select {
		case <-c.cleanupTicker.C:
			now := time.Now()
			c.mutex.Lock()

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

			c.mutex.Unlock()

		case <-c.stopCleanup:
			return
		}
	}
}

// Stop stops the cleanup goroutine
func (c *QuotaCache) Stop() {
	c.cleanupTicker.Stop()
	close(c.stopCleanup)
}

// Global quota cache instance (60 second TTL for dynamic adjustment)
var GlobalQuotaCache *QuotaCache

// InitializeQuotaCache initializes the global quota cache
func InitializeQuotaCache(ttl time.Duration) {
	GlobalQuotaCache = NewQuotaCache(ttl)
}
