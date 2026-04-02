package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupDeliveryRedisStore creates a WebhookDeliveryRedisStore backed by miniredis for testing.
func setupDeliveryRedisStore(t *testing.T) (*WebhookDeliveryRedisStore, context.Context) {
	t.Helper()
	_, mini := setupTestRedis(t)
	t.Cleanup(mini.Close)

	redisDB, err := db.NewRedisDB(db.RedisConfig{
		Host: mini.Host(),
		Port: mini.Port(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = redisDB.Close() })

	store := NewWebhookDeliveryRedisStore(redisDB)
	return store, context.Background()
}

// newTestDeliveryRecord creates a WebhookDeliveryRecord with sensible defaults for testing.
func newTestDeliveryRecord(subID uuid.UUID) *WebhookDeliveryRecord {
	return &WebhookDeliveryRecord{
		SubscriptionID: subID,
		EventType:      "threat_model.updated",
		Payload:        `{"id":"abc"}`,
		Status:         DeliveryStatusPending,
	}
}

func TestWebhookDeliveryRedisStore_CreateAndGet(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	subID := uuid.New()
	rec := newTestDeliveryRecord(subID)

	err := store.Create(ctx, rec)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, rec.ID, "ID should be generated")
	assert.False(t, rec.CreatedAt.IsZero(), "CreatedAt should be set")
	assert.False(t, rec.LastActivityAt.IsZero(), "LastActivityAt should be set")

	got, err := store.Get(ctx, rec.ID)
	require.NoError(t, err)
	assert.Equal(t, rec.ID, got.ID)
	assert.Equal(t, subID, got.SubscriptionID)
	assert.Equal(t, "threat_model.updated", got.EventType)
	assert.Equal(t, `{"id":"abc"}`, got.Payload)
	assert.Equal(t, DeliveryStatusPending, got.Status)
	assert.Equal(t, 0, got.Attempts)
	assert.Nil(t, got.NextRetryAt)
	assert.Nil(t, got.DeliveredAt)
}

func TestWebhookDeliveryRedisStore_GetNotFound(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	_, err := store.Get(ctx, uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWebhookDeliveryRedisStore_UpdateStatus(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	rec := newTestDeliveryRecord(uuid.New())
	require.NoError(t, store.Create(ctx, rec))

	now := time.Now().UTC()
	err := store.UpdateStatus(ctx, rec.ID, DeliveryStatusDelivered, &now)
	require.NoError(t, err)

	got, err := store.Get(ctx, rec.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliveryStatusDelivered, got.Status)
	require.NotNil(t, got.DeliveredAt)
	assert.WithinDuration(t, now, *got.DeliveredAt, time.Second)
}

func TestWebhookDeliveryRedisStore_UpdateRetry(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	rec := newTestDeliveryRecord(uuid.New())
	require.NoError(t, store.Create(ctx, rec))

	retryAt := time.Now().UTC().Add(5 * time.Minute)
	err := store.UpdateRetry(ctx, rec.ID, 2, &retryAt, "connection refused")
	require.NoError(t, err)

	got, err := store.Get(ctx, rec.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, got.Attempts)
	require.NotNil(t, got.NextRetryAt)
	assert.WithinDuration(t, retryAt, *got.NextRetryAt, time.Second)
	assert.Equal(t, "connection refused", got.LastError)
}

func TestWebhookDeliveryRedisStore_Delete(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	rec := newTestDeliveryRecord(uuid.New())
	require.NoError(t, store.Create(ctx, rec))

	err := store.Delete(ctx, rec.ID)
	require.NoError(t, err)

	_, err = store.Get(ctx, rec.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWebhookDeliveryRedisStore_ListPending(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	subID := uuid.New()

	// Create 3 pending records
	for range 3 {
		rec := newTestDeliveryRecord(subID)
		require.NoError(t, store.Create(ctx, rec))
	}

	// Create 1 delivered record
	delivered := newTestDeliveryRecord(subID)
	require.NoError(t, store.Create(ctx, delivered))
	now := time.Now().UTC()
	require.NoError(t, store.UpdateStatus(ctx, delivered.ID, DeliveryStatusDelivered, &now))

	pending, err := store.ListPending(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, pending, 3)
	for _, p := range pending {
		assert.Equal(t, DeliveryStatusPending, p.Status)
	}
}

func TestWebhookDeliveryRedisStore_ListPending_SkipsFutureRetry(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	// Create a pending record with NextRetryAt far in the future
	rec := newTestDeliveryRecord(uuid.New())
	require.NoError(t, store.Create(ctx, rec))

	futureRetry := time.Now().UTC().Add(1 * time.Hour)
	require.NoError(t, store.UpdateRetry(ctx, rec.ID, 1, &futureRetry, "timeout"))

	pending, err := store.ListPending(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, pending, 0, "pending record with future NextRetryAt should be skipped")
}

func TestWebhookDeliveryRedisStore_ListReadyForRetry(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	rec := newTestDeliveryRecord(uuid.New())
	require.NoError(t, store.Create(ctx, rec))

	// Set retry in the past with Attempts > 0
	pastRetry := time.Now().UTC().Add(-5 * time.Minute)
	require.NoError(t, store.UpdateRetry(ctx, rec.ID, 1, &pastRetry, "timeout"))

	ready, err := store.ListReadyForRetry(ctx)
	require.NoError(t, err)
	assert.Len(t, ready, 1)
	assert.Equal(t, rec.ID, ready[0].ID)
}

func TestWebhookDeliveryRedisStore_ListStale(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	rec := newTestDeliveryRecord(uuid.New())
	require.NoError(t, store.Create(ctx, rec))

	// Set status to in_progress
	require.NoError(t, store.UpdateStatus(ctx, rec.ID, DeliveryStatusInProgress, nil))

	// Update() auto-sets LastActivityAt to now, so we need to directly write to Redis
	// with a past timestamp.
	got, err := store.Get(ctx, rec.ID)
	require.NoError(t, err)
	got.LastActivityAt = time.Now().UTC().Add(-20 * time.Minute)
	data, err := json.Marshal(got)
	require.NoError(t, err)
	store.redis.GetClient().Set(ctx, store.buildDeliveryKey(rec.ID), string(data), DeliveryTTLActive)

	stale, err := store.ListStale(ctx, DeliveryStaleTimeout)
	require.NoError(t, err)
	assert.Len(t, stale, 1)
	assert.Equal(t, rec.ID, stale[0].ID)
}

func TestWebhookDeliveryRedisStore_ListBySubscription(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	sub1 := uuid.New()
	sub2 := uuid.New()

	// Create 3 records for sub1
	for range 3 {
		rec := newTestDeliveryRecord(sub1)
		require.NoError(t, store.Create(ctx, rec))
	}
	// Create 2 records for sub2
	for range 2 {
		rec := newTestDeliveryRecord(sub2)
		require.NoError(t, store.Create(ctx, rec))
	}

	// List sub1 with pagination
	records, total, err := store.ListBySubscription(ctx, sub1, 2, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, records, 2)
	for _, r := range records {
		assert.Equal(t, sub1, r.SubscriptionID)
	}

	// Page 2
	records2, total2, err := store.ListBySubscription(ctx, sub1, 2, 2)
	require.NoError(t, err)
	assert.Equal(t, 3, total2)
	assert.Len(t, records2, 1)
}

func TestWebhookDeliveryRedisStore_ListAll(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	// Create 5 records
	for range 5 {
		rec := newTestDeliveryRecord(uuid.New())
		require.NoError(t, store.Create(ctx, rec))
	}

	// Page 1: limit=3, offset=0
	records, total, err := store.ListAll(ctx, 3, 0)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, records, 3)

	// Page 2: limit=3, offset=3
	records2, total2, err := store.ListAll(ctx, 3, 3)
	require.NoError(t, err)
	assert.Equal(t, 5, total2)
	assert.Len(t, records2, 2)
}

func TestWebhookDeliveryRedisStore_AddonSpecificFields(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	addonID := uuid.New()
	invokerUUID := uuid.New()

	rec := &WebhookDeliveryRecord{
		SubscriptionID: uuid.New(),
		EventType:      "addon.invoked",
		Payload:        `{"action":"run"}`,
		Status:         DeliveryStatusPending,
		AddonID:        &addonID,
		InvokedByUUID:  &invokerUUID,
		InvokedByEmail: "alice@example.com",
		InvokedByName:  "Alice",
	}
	require.NoError(t, store.Create(ctx, rec))

	got, err := store.Get(ctx, rec.ID)
	require.NoError(t, err)
	require.NotNil(t, got.AddonID)
	assert.Equal(t, addonID, *got.AddonID)
	require.NotNil(t, got.InvokedByUUID)
	assert.Equal(t, invokerUUID, *got.InvokedByUUID)
	assert.Equal(t, "alice@example.com", got.InvokedByEmail)
	assert.Equal(t, "Alice", got.InvokedByName)
}

func TestWebhookDeliveryRedisStore_TTLHandling(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	// Create a pending record -> should get active TTL
	rec := newTestDeliveryRecord(uuid.New())
	require.NoError(t, store.Create(ctx, rec))

	key := store.buildDeliveryKey(rec.ID)
	ttl := store.redis.GetClient().TTL(ctx, key).Val()
	assert.InDelta(t, DeliveryTTLActive.Seconds(), ttl.Seconds(), 5,
		"pending record should have active TTL (~4h)")

	// Update to delivered -> should get terminal TTL
	now := time.Now().UTC()
	require.NoError(t, store.UpdateStatus(ctx, rec.ID, DeliveryStatusDelivered, &now))

	ttl = store.redis.GetClient().TTL(ctx, key).Val()
	assert.InDelta(t, DeliveryTTLTerminal.Seconds(), ttl.Seconds(), 5,
		"delivered record should have terminal TTL (~7d)")
}

func TestWebhookDeliveryRedisStore_SortOrder(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	subID := uuid.New()
	var ids []uuid.UUID

	// Create 4 records with incrementing timestamps
	for i := range 4 {
		rec := &WebhookDeliveryRecord{
			SubscriptionID: subID,
			EventType:      "threat_model.updated",
			Payload:        `{}`,
			Status:         DeliveryStatusPending,
			CreatedAt:      time.Date(2025, 1, 1, 0, i, 0, 0, time.UTC),
		}
		require.NoError(t, store.Create(ctx, rec))
		ids = append(ids, rec.ID)
	}

	// ListAll should return in CreatedAt ascending order
	records, _, err := store.ListAll(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, records, 4)

	for i := 0; i < len(records)-1; i++ {
		assert.True(t, records[i].CreatedAt.Before(records[i+1].CreatedAt) || records[i].CreatedAt.Equal(records[i+1].CreatedAt),
			"records should be sorted by CreatedAt ascending: index %d (%v) should be before index %d (%v)",
			i, records[i].CreatedAt, i+1, records[i+1].CreatedAt)
	}

	// Verify the order matches creation order
	for i, rec := range records {
		assert.Equal(t, ids[i], rec.ID, "record at index %d should match creation order", i)
	}
}
