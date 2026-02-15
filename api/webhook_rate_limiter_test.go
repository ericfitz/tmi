package api

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookRateLimiter_CheckSubscriptionLimit(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	limiter := NewWebhookRateLimiter(client)
	ctx := context.Background()

	ownerID := uuid.New().String()

	// Initialize quota store with default values
	GlobalWebhookQuotaStore = &mockQuotaStore{
		quotas: map[string]DBWebhookQuota{
			ownerID: {
				OwnerId:          uuid.MustParse(ownerID),
				MaxSubscriptions: 2,
			},
		},
	}

	// Initialize subscription store
	GlobalWebhookSubscriptionStore = &mockSubscriptionStore{
		countByOwner: 0,
	}

	// Should allow when under limit
	err := limiter.CheckSubscriptionLimit(ctx, ownerID)
	assert.NoError(t, err)

	// Set count to 1 (still under limit of 2)
	GlobalWebhookSubscriptionStore.(*mockSubscriptionStore).countByOwner = 1
	err = limiter.CheckSubscriptionLimit(ctx, ownerID)
	assert.NoError(t, err)

	// Set count to 2 (at limit)
	GlobalWebhookSubscriptionStore.(*mockSubscriptionStore).countByOwner = 2
	err = limiter.CheckSubscriptionLimit(ctx, ownerID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "subscription limit exceeded")
}

func TestWebhookRateLimiter_SlidingWindow(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	limiter := NewWebhookRateLimiter(client)
	ctx := context.Background()

	key := "test:ratelimit:window"
	limit := 3
	windowSeconds := 60

	// First 3 requests should succeed
	for i := 0; i < limit; i++ {
		allowed, err := limiter.CheckSlidingWindowSimple(ctx, key, limit, windowSeconds)
		require.NoError(t, err)
		assert.True(t, allowed, "Request %d should be allowed", i+1)
	}

	// 4th request should be blocked
	allowed, err := limiter.CheckSlidingWindowSimple(ctx, key, limit, windowSeconds)
	require.NoError(t, err)
	assert.False(t, allowed, "Request should be blocked when limit exceeded")

	// Manually clear the window to simulate time expiration
	// (miniredis FastForward doesn't affect Go's time.Now() calls)
	err = client.Del(ctx, key).Err()
	require.NoError(t, err)

	// Should allow new requests after window is cleared
	allowed, err = limiter.CheckSlidingWindowSimple(ctx, key, limit, windowSeconds)
	require.NoError(t, err)
	assert.True(t, allowed, "Request should be allowed after window expires")
}

func TestWebhookRateLimiter_CheckEventPublicationLimit(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	limiter := NewWebhookRateLimiter(client)
	ctx := context.Background()

	ownerID := uuid.New().String()

	// Initialize quota store
	GlobalWebhookQuotaStore = &mockQuotaStore{
		quotas: map[string]DBWebhookQuota{
			ownerID: {
				OwnerId:            uuid.MustParse(ownerID),
				MaxEventsPerMinute: 5,
			},
		},
	}

	// Should allow first 5 events
	for i := 0; i < 5; i++ {
		err := limiter.CheckEventPublicationLimit(ctx, ownerID)
		assert.NoError(t, err, "Event %d should be allowed", i+1)
	}

	// 6th event should be blocked
	err := limiter.CheckEventPublicationLimit(ctx, ownerID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "event publication rate limit exceeded")
}

// Mock quota store for testing
type mockQuotaStore struct {
	quotas map[string]DBWebhookQuota
}

func (m *mockQuotaStore) Get(ownerID string) (DBWebhookQuota, error) {
	if quota, ok := m.quotas[ownerID]; ok {
		return quota, nil
	}
	return DBWebhookQuota{}, nil
}

func (m *mockQuotaStore) GetOrDefault(ownerID string) DBWebhookQuota {
	if quota, ok := m.quotas[ownerID]; ok {
		return quota
	}
	ownerUUID, _ := uuid.Parse(ownerID)
	return DBWebhookQuota{
		OwnerId:                          ownerUUID,
		MaxSubscriptions:                 DefaultMaxSubscriptions,
		MaxEventsPerMinute:               DefaultMaxEventsPerMinute,
		MaxSubscriptionRequestsPerMinute: DefaultMaxSubscriptionRequestsPerMinute,
		MaxSubscriptionRequestsPerDay:    DefaultMaxSubscriptionRequestsPerDay,
	}
}

func (m *mockQuotaStore) Create(item DBWebhookQuota) (DBWebhookQuota, error) {
	m.quotas[item.OwnerId.String()] = item
	return item, nil
}

func (m *mockQuotaStore) Update(ownerID string, item DBWebhookQuota) error {
	m.quotas[ownerID] = item
	return nil
}

func (m *mockQuotaStore) Delete(ownerID string) error {
	delete(m.quotas, ownerID)
	return nil
}

func (m *mockQuotaStore) List(offset, limit int) ([]DBWebhookQuota, error) {
	var result []DBWebhookQuota
	for _, quota := range m.quotas {
		result = append(result, quota)
	}
	return result, nil
}

func (m *mockQuotaStore) Count() (int, error) {
	return len(m.quotas), nil
}

// Mock subscription store for testing
type mockSubscriptionStore struct {
	countByOwner int
}

func (m *mockSubscriptionStore) Get(id string) (DBWebhookSubscription, error) {
	return DBWebhookSubscription{}, nil
}

func (m *mockSubscriptionStore) List(offset, limit int, filter func(DBWebhookSubscription) bool) []DBWebhookSubscription {
	return []DBWebhookSubscription{}
}

func (m *mockSubscriptionStore) ListByOwner(ownerID string, offset, limit int) ([]DBWebhookSubscription, error) {
	return []DBWebhookSubscription{}, nil
}

func (m *mockSubscriptionStore) ListByThreatModel(threatModelID string, offset, limit int) ([]DBWebhookSubscription, error) {
	return []DBWebhookSubscription{}, nil
}

func (m *mockSubscriptionStore) ListActiveByOwner(ownerID string) ([]DBWebhookSubscription, error) {
	return []DBWebhookSubscription{}, nil
}

func (m *mockSubscriptionStore) ListPendingVerification() ([]DBWebhookSubscription, error) {
	return []DBWebhookSubscription{}, nil
}

func (m *mockSubscriptionStore) ListPendingDelete() ([]DBWebhookSubscription, error) {
	return []DBWebhookSubscription{}, nil
}

func (m *mockSubscriptionStore) ListIdle(daysIdle int) ([]DBWebhookSubscription, error) {
	return []DBWebhookSubscription{}, nil
}

func (m *mockSubscriptionStore) ListBroken(minFailures int, daysSinceSuccess int) ([]DBWebhookSubscription, error) {
	return []DBWebhookSubscription{}, nil
}

func (m *mockSubscriptionStore) Create(item DBWebhookSubscription, idSetter func(DBWebhookSubscription, string) DBWebhookSubscription) (DBWebhookSubscription, error) {
	return item, nil
}

func (m *mockSubscriptionStore) Update(id string, item DBWebhookSubscription) error {
	return nil
}

func (m *mockSubscriptionStore) UpdateStatus(id string, status string) error {
	return nil
}

func (m *mockSubscriptionStore) UpdateChallenge(id string, challenge string, challengesSent int) error {
	return nil
}

func (m *mockSubscriptionStore) UpdatePublicationStats(id string, success bool) error {
	return nil
}

func (m *mockSubscriptionStore) IncrementTimeouts(id string) error {
	return nil
}

func (m *mockSubscriptionStore) ResetTimeouts(id string) error {
	return nil
}

func (m *mockSubscriptionStore) Delete(id string) error {
	return nil
}

func (m *mockSubscriptionStore) Count() int {
	return m.countByOwner
}

func (m *mockSubscriptionStore) CountByOwner(ownerID string) (int, error) {
	return m.countByOwner, nil
}
