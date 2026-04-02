# Webhook Delivery Unification — Phases 2 & 3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unify webhook delivery infrastructure so all deliveries (resource-change events and addon invocations) flow through a single Redis-backed pipeline with shared endpoints, workers, and cleanup.

**Architecture:** Replace the dual-path delivery system (Postgres `WebhookDeliveryWorker` + Redis `AddonInvocationWorker`) with a single Redis-backed `WebhookDeliveryRedisStore` and unified worker. New `/webhook-deliveries/{id}` and `/webhook-deliveries/{id}/status` endpoints replace the `/invocations/*` endpoints. Addon invocations emit events to Redis Streams and flow through the same `WebhookEventConsumer` → delivery worker pipeline as resource-change events.

**Tech Stack:** Go, Redis (go-redis/v8), Gin, oapi-codegen, miniredis (tests), GORM (migration only)

**Design spec:** `docs/superpowers/specs/2026-03-29-webhook-delivery-unification-design.md`
**Issue:** #220

---

## File Structure

### New Files
| File | Responsibility |
|------|---------------|
| `api/webhook_delivery_redis_store.go` | Unified Redis-backed delivery store (type, interface, implementation) |
| `api/webhook_delivery_redis_store_test.go` | Unit tests for Redis delivery store |
| `api/webhook_delivery_handlers.go` | `GET /webhook-deliveries/{id}` and `POST /webhook-deliveries/{id}/status` handlers |
| `api/webhook_delivery_handlers_test.go` | Unit tests for new delivery handlers |
| `auth/migrations/xxx_drop_webhook_deliveries.go` | Migration to drop `webhook_deliveries` Postgres table (Phase 3) |

### Modified Files
| File | Changes |
|------|---------|
| `api-schema/tmi-openapi.json` | Add `/webhook-deliveries/{id}` endpoints, remove `/invocations/*` endpoints |
| `api/api.go` | Regenerated from OpenAPI spec |
| `api/webhook_event_consumer.go` | Handle `addon.invoked` events, create Redis delivery records |
| `api/webhook_delivery_worker.go` | Use Redis store, add `X-TMI-Callback: async` handling |
| `api/webhook_cleanup_worker.go` | Unified cleanup for all Redis deliveries |
| `api/addon_invocation_handlers.go` | Remove `GetInvocation`, `ListInvocations`, `UpdateInvocationStatus`; modify `InvokeAddon` to emit to Redis Streams |
| `api/addon_type_converters.go` | Remove invocation-specific converters, add delivery converters |
| `api/server_addon.go` | Remove invocation route delegations |
| `api/webhook_handlers.go` | Repoint admin delivery endpoints to Redis store |
| `api/webhook_store.go` | Remove `WebhookDeliveryStoreInterface`, `DBWebhookDelivery`, globals |
| `api/webhook_store_gorm.go` | Remove `GormWebhookDeliveryStore` |
| `api/store.go` | Update `InitializeGormStores` to skip delivery store |
| `api/events.go` | Rename `ResourceID`/`ResourceType` → `ObjectID`/`ObjectType` in `EventPayload` |
| `cmd/server/main.go` | Initialize Redis delivery store, remove old workers |

### Removed Files
| File | Reason |
|------|--------|
| `api/addon_invocation_store.go` | Replaced by `webhook_delivery_redis_store.go` |
| `api/addon_invocation_worker.go` | Delivery handled by unified `WebhookDeliveryWorker` |
| `api/addon_invocation_worker_test.go` | Tests move to new store/worker tests |
| `api/addon_invocation_cleanup_worker.go` | Cleanup handled by unified `WebhookCleanupWorker` |
| `api/addon_invocation_handlers_test.go` | Tests rewritten for new handlers |

---

## Phase 2: Unified Redis Delivery Model + Migrate Addon Invocations

### Task 1: Define Unified Delivery Record and Redis Store Interface

**Files:**
- Create: `api/webhook_delivery_redis_store.go`

- [ ] **Step 1: Create the unified delivery type and store interface**

```go
// api/webhook_delivery_redis_store.go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	db "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// Delivery status constants
const (
	DeliveryStatusPending    = "pending"
	DeliveryStatusInProgress = "in_progress"
	DeliveryStatusDelivered  = "delivered"
	DeliveryStatusFailed     = "failed"
)

// Delivery TTL constants
const (
	DeliveryTTLActive    = 4 * time.Hour  // pending / in_progress backstop
	DeliveryTTLTerminal  = 7 * 24 * time.Hour // failed / delivered / completed
	DeliveryStaleTimeout = 15 * time.Minute    // inactivity timeout for in_progress
)

// WebhookDeliveryRecord is the unified delivery record stored in Redis.
// Used for both resource-change events and addon invocations.
type WebhookDeliveryRecord struct {
	ID             uuid.UUID  `json:"id"`
	SubscriptionID uuid.UUID  `json:"subscription_id"`
	EventType      string     `json:"event_type"`
	Payload        string     `json:"payload"`
	Status         string     `json:"status"`
	StatusPercent  int        `json:"status_percent"`
	StatusMessage  string     `json:"status_message"`
	Attempts       int        `json:"attempts"`
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	LastActivityAt time.Time  `json:"last_activity_at"`

	// Addon-specific fields (only populated for addon.invoked events)
	AddonID        *uuid.UUID `json:"addon_id,omitempty"`
	InvokedByUUID  *uuid.UUID `json:"invoked_by_uuid,omitempty"`
	InvokedByEmail string     `json:"invoked_by_email,omitempty"`
	InvokedByName  string     `json:"invoked_by_name,omitempty"`
}

// WebhookDeliveryRedisStoreInterface defines operations on unified delivery records.
type WebhookDeliveryRedisStoreInterface interface {
	Create(ctx context.Context, record *WebhookDeliveryRecord) error
	Get(ctx context.Context, id uuid.UUID) (*WebhookDeliveryRecord, error)
	Update(ctx context.Context, record *WebhookDeliveryRecord) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, deliveredAt *time.Time) error
	UpdateRetry(ctx context.Context, id uuid.UUID, attempts int, nextRetryAt *time.Time, lastError string) error
	Delete(ctx context.Context, id uuid.UUID) error

	ListPending(ctx context.Context, limit int) ([]WebhookDeliveryRecord, error)
	ListReadyForRetry(ctx context.Context) ([]WebhookDeliveryRecord, error)
	ListStale(ctx context.Context, timeout time.Duration) ([]WebhookDeliveryRecord, error)
	ListBySubscription(ctx context.Context, subscriptionID uuid.UUID, limit, offset int) ([]WebhookDeliveryRecord, int, error)
	ListAll(ctx context.Context, limit, offset int) ([]WebhookDeliveryRecord, int, error)
}

// GlobalWebhookDeliveryRedisStore is the global singleton for the unified delivery store.
var GlobalWebhookDeliveryRedisStore WebhookDeliveryRedisStoreInterface
```

- [ ] **Step 2: Commit**

```bash
git add api/webhook_delivery_redis_store.go
git commit -m "feat(api): define unified WebhookDeliveryRecord and Redis store interface

Phase 2 of webhook delivery unification (#220). Defines the unified
delivery record type and store interface that will replace both
GormWebhookDeliveryStore and AddonInvocationRedisStore.

Refs #220"
```

---

### Task 2: Implement WebhookDeliveryRedisStore — CRUD Operations

**Files:**
- Modify: `api/webhook_delivery_redis_store.go`

- [ ] **Step 1: Implement the Redis store struct and CRUD methods**

Append to `api/webhook_delivery_redis_store.go`:

```go
const webhookDeliveryKeyPrefix = "webhook:delivery:"

// WebhookDeliveryRedisStore implements WebhookDeliveryRedisStoreInterface using Redis.
type WebhookDeliveryRedisStore struct {
	redis   *db.RedisDB
	builder *db.RedisKeyBuilder
}

// NewWebhookDeliveryRedisStore creates a new unified delivery store.
func NewWebhookDeliveryRedisStore(redis *db.RedisDB) *WebhookDeliveryRedisStore {
	return &WebhookDeliveryRedisStore{
		redis:   redis,
		builder: db.NewRedisKeyBuilder(),
	}
}

func (s *WebhookDeliveryRedisStore) deliveryKey(id uuid.UUID) string {
	return webhookDeliveryKeyPrefix + id.String()
}

func (s *WebhookDeliveryRedisStore) ttlForStatus(status string) time.Duration {
	switch status {
	case DeliveryStatusPending, DeliveryStatusInProgress:
		return DeliveryTTLActive
	default:
		return DeliveryTTLTerminal
	}
}

// Create stores a new delivery record in Redis.
func (s *WebhookDeliveryRedisStore) Create(ctx context.Context, record *WebhookDeliveryRecord) error {
	if record.ID == uuid.Nil {
		record.ID = uuid.Must(uuid.NewV7())
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if record.LastActivityAt.IsZero() {
		record.LastActivityAt = record.CreatedAt
	}
	if record.Status == "" {
		record.Status = DeliveryStatusPending
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal delivery record: %w", err)
	}

	key := s.deliveryKey(record.ID)
	ttl := s.ttlForStatus(record.Status)

	if err := s.redis.GetClient().Set(ctx, key, string(data), ttl).Err(); err != nil {
		return fmt.Errorf("failed to store delivery record: %w", err)
	}

	return nil
}

// Get retrieves a delivery record by ID.
func (s *WebhookDeliveryRedisStore) Get(ctx context.Context, id uuid.UUID) (*WebhookDeliveryRecord, error) {
	key := s.deliveryKey(id)
	data, err := s.redis.GetClient().Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("delivery record not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get delivery record: %w", err)
	}

	var record WebhookDeliveryRecord
	if err := json.Unmarshal([]byte(data), &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal delivery record: %w", err)
	}
	return &record, nil
}

// Update replaces a delivery record in Redis, refreshing TTL.
func (s *WebhookDeliveryRedisStore) Update(ctx context.Context, record *WebhookDeliveryRecord) error {
	record.LastActivityAt = time.Now().UTC()

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal delivery record: %w", err)
	}

	key := s.deliveryKey(record.ID)
	ttl := s.ttlForStatus(record.Status)

	if err := s.redis.GetClient().Set(ctx, key, string(data), ttl).Err(); err != nil {
		return fmt.Errorf("failed to update delivery record: %w", err)
	}
	return nil
}

// UpdateStatus updates just the status and delivered_at fields.
func (s *WebhookDeliveryRedisStore) UpdateStatus(ctx context.Context, id uuid.UUID, status string, deliveredAt *time.Time) error {
	record, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	record.Status = status
	record.DeliveredAt = deliveredAt
	return s.Update(ctx, record)
}

// UpdateRetry updates retry-related fields.
func (s *WebhookDeliveryRedisStore) UpdateRetry(ctx context.Context, id uuid.UUID, attempts int, nextRetryAt *time.Time, lastError string) error {
	record, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	record.Attempts = attempts
	record.NextRetryAt = nextRetryAt
	record.LastError = lastError
	return s.Update(ctx, record)
}

// Delete removes a delivery record.
func (s *WebhookDeliveryRedisStore) Delete(ctx context.Context, id uuid.UUID) error {
	key := s.deliveryKey(id)
	if err := s.redis.GetClient().Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete delivery record: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Commit**

```bash
git add api/webhook_delivery_redis_store.go
git commit -m "feat(api): implement WebhookDeliveryRedisStore CRUD operations

Implements Create, Get, Update, UpdateStatus, UpdateRetry, Delete for
the unified Redis-backed delivery store. TTLs refresh on every update:
4h for active states, 7d for terminal states.

Refs #220"
```

---

### Task 3: Implement WebhookDeliveryRedisStore — Query Operations

**Files:**
- Modify: `api/webhook_delivery_redis_store.go`

- [ ] **Step 1: Implement list and query methods**

Append to `api/webhook_delivery_redis_store.go`:

```go
// scanDeliveries scans all delivery keys and returns records matching a filter.
func (s *WebhookDeliveryRedisStore) scanDeliveries(ctx context.Context, filter func(*WebhookDeliveryRecord) bool) ([]WebhookDeliveryRecord, error) {
	logger := slogging.Get()
	var results []WebhookDeliveryRecord
	var cursor uint64

	for {
		keys, nextCursor, err := s.redis.GetClient().Scan(ctx, cursor, webhookDeliveryKeyPrefix+"*", 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan delivery keys: %w", err)
		}

		for _, key := range keys {
			data, err := s.redis.GetClient().Get(ctx, key).Result()
			if err != nil {
				if err == redis.Nil {
					continue // Key expired between scan and get
				}
				logger.Error("failed to get delivery %s: %v", key, err)
				continue
			}

			var record WebhookDeliveryRecord
			if err := json.Unmarshal([]byte(data), &record); err != nil {
				logger.Error("failed to unmarshal delivery %s: %v", key, err)
				continue
			}

			if filter == nil || filter(&record) {
				results = append(results, record)
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return results, nil
}

// ListPending returns up to `limit` deliveries with status "pending" and no future retry time.
func (s *WebhookDeliveryRedisStore) ListPending(ctx context.Context, limit int) ([]WebhookDeliveryRecord, error) {
	now := time.Now().UTC()
	records, err := s.scanDeliveries(ctx, func(r *WebhookDeliveryRecord) bool {
		if r.Status != DeliveryStatusPending {
			return false
		}
		// Skip deliveries with a future retry time
		if r.NextRetryAt != nil && r.NextRetryAt.After(now) {
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

// ListReadyForRetry returns deliveries with status "pending" and next_retry_at in the past.
func (s *WebhookDeliveryRedisStore) ListReadyForRetry(ctx context.Context) ([]WebhookDeliveryRecord, error) {
	now := time.Now().UTC()
	return s.scanDeliveries(ctx, func(r *WebhookDeliveryRecord) bool {
		return r.Status == DeliveryStatusPending &&
			r.NextRetryAt != nil &&
			!r.NextRetryAt.After(now) &&
			r.Attempts > 0
	})
}

// ListStale returns in_progress deliveries with no activity past the timeout.
func (s *WebhookDeliveryRedisStore) ListStale(ctx context.Context, timeout time.Duration) ([]WebhookDeliveryRecord, error) {
	cutoff := time.Now().UTC().Add(-timeout)
	return s.scanDeliveries(ctx, func(r *WebhookDeliveryRecord) bool {
		return r.Status == DeliveryStatusInProgress && r.LastActivityAt.Before(cutoff)
	})
}

// ListBySubscription returns deliveries for a subscription with pagination.
func (s *WebhookDeliveryRedisStore) ListBySubscription(ctx context.Context, subscriptionID uuid.UUID, limit, offset int) ([]WebhookDeliveryRecord, int, error) {
	all, err := s.scanDeliveries(ctx, func(r *WebhookDeliveryRecord) bool {
		return r.SubscriptionID == subscriptionID
	})
	if err != nil {
		return nil, 0, err
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

// ListAll returns all deliveries with pagination.
func (s *WebhookDeliveryRedisStore) ListAll(ctx context.Context, limit, offset int) ([]WebhookDeliveryRecord, int, error) {
	all, err := s.scanDeliveries(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}
```

- [ ] **Step 2: Commit**

```bash
git add api/webhook_delivery_redis_store.go
git commit -m "feat(api): implement WebhookDeliveryRedisStore query operations

Adds ListPending, ListReadyForRetry, ListStale, ListBySubscription,
and ListAll using Redis SCAN with in-memory filtering.

Refs #220"
```

---

### Task 4: Write Unit Tests for WebhookDeliveryRedisStore

**Files:**
- Create: `api/webhook_delivery_redis_store_test.go`

- [ ] **Step 1: Write tests for CRUD and query operations**

```go
// api/webhook_delivery_redis_store_test.go
package api

import (
	"context"
	"testing"
	"time"

	db "github.com/ericfitz/tmi/auth/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupDeliveryRedisStore(t *testing.T) (*WebhookDeliveryRedisStore, context.Context) {
	t.Helper()
	_, mini := setupTestRedis(t)
	redisDB := db.NewRedisDBForTest(mini.Addr())
	store := NewWebhookDeliveryRedisStore(redisDB)
	return store, context.Background()
}

func TestWebhookDeliveryRedisStore_CreateAndGet(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	record := &WebhookDeliveryRecord{
		SubscriptionID: uuid.New(),
		EventType:      "threat_model.updated",
		Payload:        `{"event_type":"threat_model.updated"}`,
		Status:         DeliveryStatusPending,
	}

	err := store.Create(ctx, record)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, record.ID)
	assert.False(t, record.CreatedAt.IsZero())

	got, err := store.Get(ctx, record.ID)
	require.NoError(t, err)
	assert.Equal(t, record.ID, got.ID)
	assert.Equal(t, record.SubscriptionID, got.SubscriptionID)
	assert.Equal(t, record.EventType, got.EventType)
	assert.Equal(t, DeliveryStatusPending, got.Status)
}

func TestWebhookDeliveryRedisStore_GetNotFound(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	_, err := store.Get(ctx, uuid.New())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWebhookDeliveryRedisStore_UpdateStatus(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	record := &WebhookDeliveryRecord{
		SubscriptionID: uuid.New(),
		EventType:      "addon.invoked",
		Payload:        `{}`,
	}
	require.NoError(t, store.Create(ctx, record))

	now := time.Now().UTC()
	err := store.UpdateStatus(ctx, record.ID, DeliveryStatusDelivered, &now)
	require.NoError(t, err)

	got, err := store.Get(ctx, record.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliveryStatusDelivered, got.Status)
	assert.NotNil(t, got.DeliveredAt)
}

func TestWebhookDeliveryRedisStore_UpdateRetry(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	record := &WebhookDeliveryRecord{
		SubscriptionID: uuid.New(),
		EventType:      "threat_model.created",
		Payload:        `{}`,
	}
	require.NoError(t, store.Create(ctx, record))

	nextRetry := time.Now().UTC().Add(5 * time.Minute)
	err := store.UpdateRetry(ctx, record.ID, 2, &nextRetry, "HTTP 503")
	require.NoError(t, err)

	got, err := store.Get(ctx, record.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, got.Attempts)
	assert.Equal(t, "HTTP 503", got.LastError)
	assert.NotNil(t, got.NextRetryAt)
}

func TestWebhookDeliveryRedisStore_Delete(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	record := &WebhookDeliveryRecord{
		SubscriptionID: uuid.New(),
		EventType:      "addon.invoked",
		Payload:        `{}`,
	}
	require.NoError(t, store.Create(ctx, record))

	err := store.Delete(ctx, record.ID)
	require.NoError(t, err)

	_, err = store.Get(ctx, record.ID)
	assert.Error(t, err)
}

func TestWebhookDeliveryRedisStore_ListPending(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	subID := uuid.New()
	// Create 3 pending, 1 delivered
	for i := 0; i < 3; i++ {
		require.NoError(t, store.Create(ctx, &WebhookDeliveryRecord{
			SubscriptionID: subID,
			EventType:      "threat_model.updated",
			Payload:        `{}`,
			Status:         DeliveryStatusPending,
		}))
	}
	require.NoError(t, store.Create(ctx, &WebhookDeliveryRecord{
		SubscriptionID: subID,
		EventType:      "threat_model.deleted",
		Payload:        `{}`,
		Status:         DeliveryStatusDelivered,
	}))

	pending, err := store.ListPending(ctx, 100)
	require.NoError(t, err)
	assert.Len(t, pending, 3)
}

func TestWebhookDeliveryRedisStore_ListStale(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	// Create an in_progress delivery with old activity
	record := &WebhookDeliveryRecord{
		SubscriptionID: uuid.New(),
		EventType:      "addon.invoked",
		Payload:        `{}`,
		Status:         DeliveryStatusInProgress,
		LastActivityAt: time.Now().UTC().Add(-20 * time.Minute),
	}
	require.NoError(t, store.Create(ctx, record))
	// Override LastActivityAt (Create sets it to now)
	record.LastActivityAt = time.Now().UTC().Add(-20 * time.Minute)
	require.NoError(t, store.Update(ctx, record))
	// Update sets LastActivityAt to now, so directly set it via raw Redis
	// For test purposes, re-read and manually set
	got, _ := store.Get(ctx, record.ID)
	got.LastActivityAt = time.Now().UTC().Add(-20 * time.Minute)
	data, _ := json.Marshal(got)
	store.redis.GetClient().Set(ctx, store.deliveryKey(record.ID), string(data), DeliveryTTLActive)

	stale, err := store.ListStale(ctx, 15*time.Minute)
	require.NoError(t, err)
	assert.Len(t, stale, 1)
	assert.Equal(t, record.ID, stale[0].ID)
}

func TestWebhookDeliveryRedisStore_ListBySubscription(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	subA := uuid.New()
	subB := uuid.New()
	for i := 0; i < 3; i++ {
		require.NoError(t, store.Create(ctx, &WebhookDeliveryRecord{
			SubscriptionID: subA,
			EventType:      "threat_model.updated",
			Payload:        `{}`,
		}))
	}
	require.NoError(t, store.Create(ctx, &WebhookDeliveryRecord{
		SubscriptionID: subB,
		EventType:      "threat_model.deleted",
		Payload:        `{}`,
	}))

	records, total, err := store.ListBySubscription(ctx, subA, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, records, 3)
}

func TestWebhookDeliveryRedisStore_AddonSpecificFields(t *testing.T) {
	store, ctx := setupDeliveryRedisStore(t)

	addonID := uuid.New()
	userID := uuid.New()
	record := &WebhookDeliveryRecord{
		SubscriptionID: uuid.New(),
		EventType:      "addon.invoked",
		Payload:        `{}`,
		AddonID:        &addonID,
		InvokedByUUID:  &userID,
		InvokedByEmail: "alice@example.com",
		InvokedByName:  "Alice",
	}
	require.NoError(t, store.Create(ctx, record))

	got, err := store.Get(ctx, record.ID)
	require.NoError(t, err)
	assert.Equal(t, &addonID, got.AddonID)
	assert.Equal(t, &userID, got.InvokedByUUID)
	assert.Equal(t, "alice@example.com", got.InvokedByEmail)
	assert.Equal(t, "Alice", got.InvokedByName)
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `make test-unit name=TestWebhookDeliveryRedisStore`
Expected: All tests PASS

- [ ] **Step 3: Commit**

```bash
git add api/webhook_delivery_redis_store_test.go
git commit -m "test(api): add unit tests for WebhookDeliveryRedisStore

Covers CRUD operations, status updates, retry tracking, query
operations (ListPending, ListStale, ListBySubscription), and
addon-specific field persistence.

Refs #220"
```

---

### Task 5: Update OpenAPI Spec — Add New Endpoints, Remove Old Ones

**Files:**
- Modify: `api-schema/tmi-openapi.json`

This task modifies the OpenAPI spec using jq. The changes are:

1. Add `GET /webhook-deliveries/{delivery_id}` — dual auth (JWT or HMAC)
2. Add `POST /webhook-deliveries/{delivery_id}/status` — HMAC auth
3. Remove `GET /invocations`
4. Remove `GET /invocations/{id}`
5. Remove `POST /invocations/{id}/status`
6. Add `WebhookDeliveryStatusUpdate` request schema
7. Add `WebhookDeliveryStatusResponse` response schema
8. Update `WebhookDelivery` schema to include `status_percent`, `status_message`, `last_activity_at`, addon fields
9. Add `WebhookDeliveryResponse` wrapping `WebhookDelivery` for GET endpoint

- [ ] **Step 1: Remove old invocation endpoints from the spec**

Use jq to remove `/invocations`, `/invocations/{id}`, and `/invocations/{id}/status` paths from the OpenAPI spec. Also remove the `ListInvocationsResponse`, `InvocationResponse` schemas and `StatusQueryParam`, `AddonIdQueryParam` parameters that are only used by these endpoints.

Run: Review the spec to identify exact paths and schemas to remove, then apply changes via jq.

- [ ] **Step 2: Add new webhook delivery endpoints to the spec**

Add `/webhook-deliveries/{delivery_id}` with GET operation and `/webhook-deliveries/{delivery_id}/status` with POST operation. The GET endpoint uses the existing `DeliveryId` path parameter. The POST endpoint uses `X-Webhook-Signature` header parameter for HMAC auth.

Add schemas:
- `UpdateWebhookDeliveryStatusRequest` with properties: `status` (enum: in_progress, completed, failed), `status_percent` (integer 0-100, optional), `status_message` (string max 1024, optional)
- `UpdateWebhookDeliveryStatusResponse` with properties: `id` (uuid), `status`, `status_percent`, `status_updated_at`

Update `WebhookDelivery` schema to add: `status_percent` (integer 0-100), `status_message` (string), `last_activity_at` (datetime), `addon_id` (uuid, nullable), `invoked_by` (User, nullable). Add `in_progress` to the status enum.

- [ ] **Step 3: Validate the spec**

Run: `make validate-openapi`
Expected: No errors (warnings about vendor extensions are acceptable)

- [ ] **Step 4: Regenerate API code**

Run: `make generate-api`
Expected: `api/api.go` regenerated with new handler interfaces and types

- [ ] **Step 5: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): update OpenAPI spec for unified webhook deliveries

Adds GET /webhook-deliveries/{delivery_id} and POST
/webhook-deliveries/{delivery_id}/status endpoints. Removes
/invocations/* endpoints. Updates WebhookDelivery schema with
status_percent, status_message, addon fields, and in_progress status.

Refs #220"
```

---

### Task 6: Implement GET /webhook-deliveries/{id} Handler

**Files:**
- Create: `api/webhook_delivery_handlers.go`

- [ ] **Step 1: Implement the GET handler with JWT + HMAC dual auth**

```go
// api/webhook_delivery_handlers.go
package api

import (
	"io"
	"net/http"

	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// GetWebhookDeliveryStatus retrieves a webhook delivery by ID.
// Supports dual authentication: JWT (owner/invoker/admin) or HMAC (webhook receiver).
func GetWebhookDeliveryStatus(c *gin.Context, deliveryID openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	if GlobalWebhookDeliveryRedisStore == nil {
		logger.Error("Webhook delivery Redis store not available")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusServiceUnavailable,
			Code:    "service_unavailable",
			Message: "Delivery service not available",
		})
		return
	}

	record, err := GlobalWebhookDeliveryRedisStore.Get(c.Request.Context(), uuid.UUID(deliveryID))
	if err != nil {
		logger.Error("Failed to get delivery: id=%s, error=%v", deliveryID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Delivery not found or expired",
		})
		return
	}

	// Try JWT auth first
	authorized := false
	if _, _, _, err := ValidateAuthenticatedUser(c); err == nil {
		isAdmin, _ := IsUserAdministrator(c)
		if isAdmin {
			authorized = true
		} else {
			// Check if user is the invoker
			var userInternalUUID uuid.UUID
			if internalUUIDInterface, exists := c.Get("userInternalUUID"); exists {
				if uuidVal, ok := internalUUIDInterface.(uuid.UUID); ok {
					userInternalUUID = uuidVal
				} else if uuidStr, ok := internalUUIDInterface.(string); ok {
					userInternalUUID, _ = uuid.Parse(uuidStr)
				}
			}
			if record.InvokedByUUID != nil && *record.InvokedByUUID == userInternalUUID {
				authorized = true
			}
			// Check if user is subscription owner
			if !authorized && GlobalWebhookSubscriptionStore != nil {
				sub, subErr := GlobalWebhookSubscriptionStore.Get(record.SubscriptionID.String())
				if subErr == nil && sub.OwnerId == userInternalUUID {
					authorized = true
				}
			}
		}
	}

	// Try HMAC auth if JWT didn't authorize
	if !authorized {
		signature := c.GetHeader("X-Webhook-Signature")
		if signature != "" && GlobalWebhookSubscriptionStore != nil {
			sub, subErr := GlobalWebhookSubscriptionStore.Get(record.SubscriptionID.String())
			if subErr == nil && sub.Secret != "" {
				// For GET requests, sign the delivery ID as the payload
				if crypto.VerifyHMACSignature([]byte(deliveryID.String()), signature, sub.Secret) {
					authorized = true
				}
			}
		}
	}

	if !authorized {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "Access denied",
		})
		return
	}

	response := deliveryRecordToWebhookDelivery(record)
	c.JSON(http.StatusOK, response)
}

// UpdateWebhookDeliveryStatus updates the status of a webhook delivery (HMAC authenticated callback).
func UpdateWebhookDeliveryStatus(c *gin.Context, deliveryID openapi_types.UUID, params UpdateWebhookDeliveryStatusParams) {
	logger := slogging.Get().WithContext(c)

	if GlobalWebhookDeliveryRedisStore == nil {
		logger.Error("Webhook delivery Redis store not available")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusServiceUnavailable,
			Code:    "service_unavailable",
			Message: "Delivery service not available",
		})
		return
	}

	// Parse request body
	var req UpdateWebhookDeliveryStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "Invalid request body",
		})
		return
	}

	// Validate status
	validStatuses := map[string]bool{
		"in_progress": true,
		"completed":   true,
		"failed":      true,
	}
	if !validStatuses[string(req.Status)] {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Invalid status. Must be: in_progress, completed, or failed",
		})
		return
	}

	// Validate status_percent
	if req.StatusPercent != nil && (*req.StatusPercent < 0 || *req.StatusPercent > 100) {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Status percent must be between 0 and 100",
		})
		return
	}

	// Validate status_message length
	const maxStatusMessageLength = 1024
	if req.StatusMessage != nil && len(*req.StatusMessage) > maxStatusMessageLength {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Status message exceeds maximum length",
		})
		return
	}

	// Get delivery record
	record, err := GlobalWebhookDeliveryRedisStore.Get(c.Request.Context(), uuid.UUID(deliveryID))
	if err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Delivery not found or expired",
		})
		return
	}

	// Get webhook subscription for HMAC verification
	sub, err := GlobalWebhookSubscriptionStore.Get(record.SubscriptionID.String())
	if err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to verify delivery",
		})
		return
	}

	// Verify HMAC signature
	if sub.Secret != "" {
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_request",
				Message: "Failed to read request body",
			})
			return
		}

		signature := c.GetHeader("X-Webhook-Signature")
		if signature == "" {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusUnauthorized,
				Code:    "unauthorized",
				Message: "Missing webhook signature",
			})
			return
		}

		if !crypto.VerifyHMACSignature(bodyBytes, signature, sub.Secret) {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusUnauthorized,
				Code:    "unauthorized",
				Message: "Invalid webhook signature",
			})
			return
		}
	}

	// Validate status transition
	if record.Status == DeliveryStatusDelivered || record.Status == DeliveryStatusFailed {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusConflict,
			Code:    "conflict",
			Message: "Cannot update delivery that is already delivered or failed",
		})
		return
	}

	// Map "completed" from callback to "delivered" internal status
	newStatus := string(req.Status)
	if newStatus == "completed" {
		newStatus = DeliveryStatusDelivered
	}

	// Update record
	record.Status = newStatus
	if req.StatusPercent != nil {
		record.StatusPercent = *req.StatusPercent
	}
	if req.StatusMessage != nil {
		record.StatusMessage = *req.StatusMessage
	}

	if err := GlobalWebhookDeliveryRedisStore.Update(c.Request.Context(), record); err != nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to update delivery status",
		})
		return
	}

	// Reset timeout count on successful completion
	if newStatus == DeliveryStatusDelivered && GlobalWebhookSubscriptionStore != nil {
		if err := GlobalWebhookSubscriptionStore.ResetTimeouts(sub.Id.String()); err != nil {
			logger.Error("Failed to reset timeout count for webhook %s: %v", sub.Id, err)
		}
	}

	response := UpdateWebhookDeliveryStatusResponse{
		Id:              record.ID,
		Status:          UpdateWebhookDeliveryStatusResponseStatus(record.Status),
		StatusPercent:   record.StatusPercent,
		StatusUpdatedAt: record.LastActivityAt,
	}

	logger.Info("Delivery status updated: id=%s, status=%s", deliveryID, newStatus)
	c.JSON(http.StatusOK, response)
}

// deliveryRecordToWebhookDelivery converts a Redis record to an API response.
func deliveryRecordToWebhookDelivery(r *WebhookDeliveryRecord) WebhookDelivery {
	wd := WebhookDelivery{
		Id:             r.ID,
		SubscriptionId: r.SubscriptionID,
		EventType:      WebhookEventType(r.EventType),
		Status:         WebhookDeliveryStatus(r.Status),
		Attempts:       r.Attempts,
		CreatedAt:      r.CreatedAt,
		NextRetryAt:    r.NextRetryAt,
		DeliveredAt:    r.DeliveredAt,
		StatusPercent:  &r.StatusPercent,
		StatusMessage:  &r.StatusMessage,
		LastActivityAt: &r.LastActivityAt,
	}
	if r.Payload != "" {
		var payload interface{}
		if json.Unmarshal([]byte(r.Payload), &payload) == nil {
			wd.Payload = &payload
		}
	}
	if r.LastError != "" {
		wd.LastError = &r.LastError
	}
	if r.AddonID != nil {
		wd.AddonId = r.AddonID
	}
	if r.InvokedByUUID != nil {
		wd.InvokedBy = &User{
			Id:    *r.InvokedByUUID,
			Email: r.InvokedByEmail,
			Name:  &r.InvokedByName,
		}
	}
	return wd
}
```

Note: The exact generated type names (`UpdateWebhookDeliveryStatusParams`, `UpdateWebhookDeliveryStatusRequest`, etc.) depend on the OpenAPI spec structure from Task 5. Adjust names to match what oapi-codegen generates in `api/api.go`.

- [ ] **Step 2: Add server route delegations**

Add route delegations to `api/server_addon.go` (or a new `api/server_webhook_delivery.go`) for the new endpoints. The exact method signatures depend on what oapi-codegen generates.

- [ ] **Step 3: Commit**

```bash
git add api/webhook_delivery_handlers.go api/server_addon.go
git commit -m "feat(api): implement GET/POST /webhook-deliveries/{id} handlers

GET supports dual auth (JWT for owner/invoker/admin, HMAC for webhook
receiver). POST supports HMAC auth for status callbacks. Maps
'completed' callback status to 'delivered' internal status.

Refs #220"
```

---

### Task 7: Modify InvokeAddon to Emit Events to Redis Streams

**Files:**
- Modify: `api/addon_invocation_handlers.go`

- [ ] **Step 1: Rewrite InvokeAddon to create unified delivery records and emit events**

Replace the current `InvokeAddon` function's invocation creation and queueing logic (lines 184-231) to:
1. Create a `WebhookDeliveryRecord` in the Redis store (instead of `AddonInvocation` in the old store)
2. Emit an `addon.invoked` event to Redis Streams via `GlobalEventEmitter.EmitEvent()` (instead of queueing to `GlobalAddonInvocationWorker`)

The record should include addon-specific fields (`AddonID`, `InvokedByUUID`, `InvokedByEmail`, `InvokedByName`).

Key changes in `InvokeAddon`:
- Replace `GlobalAddonInvocationStore.Create()` with `GlobalWebhookDeliveryRedisStore.Create()`
- Replace `GlobalAddonInvocationWorker.QueueInvocation()` with `GlobalEventEmitter.EmitEvent()`
- The delivery ID (UUIDv7) comes from the new record
- Rate limiting still uses `GlobalAddonRateLimiter` (unchanged)

- [ ] **Step 2: Remove GetInvocation, ListInvocations, UpdateInvocationStatus functions**

Delete the `GetInvocation`, `ListInvocations`, and `UpdateInvocationStatus` functions from `api/addon_invocation_handlers.go`. These are replaced by `GetWebhookDeliveryStatus` and `UpdateWebhookDeliveryStatus` in the new handlers.

Keep the `InvokeAddon` function and `checkInvocationDeduplication` helper.

- [ ] **Step 3: Update server_addon.go route delegations**

Remove the `ListInvocations`, `GetInvocation`, and `UpdateInvocationStatus` server methods from `api/server_addon.go`. These routes no longer exist in the OpenAPI spec.

- [ ] **Step 4: Run build to verify compilation**

Run: `make build-server`
Expected: Build succeeds (may need to fix import references)

- [ ] **Step 5: Commit**

```bash
git add api/addon_invocation_handlers.go api/server_addon.go
git commit -m "refactor(api): InvokeAddon creates unified delivery records

InvokeAddon now creates WebhookDeliveryRecord in Redis and emits
addon.invoked events to Redis Streams instead of using the old
AddonInvocationStore + AddonInvocationWorker. Removes old invocation
endpoints (GET/POST /invocations/*).

Refs #220"
```

---

### Task 8: Update WebhookEventConsumer to Handle addon.invoked Events

**Files:**
- Modify: `api/webhook_event_consumer.go`

- [ ] **Step 1: Update processMessage to handle addon.invoked events**

The consumer currently creates `DBWebhookDelivery` records in Postgres. For Phase 2, addon.invoked events need special handling because the delivery record is already created by `InvokeAddon` — the consumer just needs to find the matching subscription and ensure the delivery is queued for the worker.

However, for resource-change events, the consumer still creates records in Postgres (this changes in Phase 3).

Update `processMessage` to:
1. Check if `event_type == "addon.invoked"` — if so, the delivery record already exists in Redis (created by InvokeAddon). Extract the `delivery_id` from the stream message and update the record with the subscription ID.
2. For all other events, continue using the existing Postgres path (unchanged until Phase 3).

Update `createDelivery` to use `GlobalWebhookDeliveryRedisStore` when processing addon events.

- [ ] **Step 2: Update EmitEvent in InvokeAddon to include delivery_id in stream message**

In `addon_invocation_handlers.go`, ensure the event emitted to Redis Streams includes the `delivery_id` field so the consumer can look it up:

```go
GlobalEventEmitter.EmitEvent(ctx, EventPayload{
    EventType:     "addon.invoked",
    ThreatModelID: record.ThreatModelID.String(),
    ResourceID:    record.ID.String(), // delivery_id
    ResourceType:  "webhook_delivery",
    OwnerID:       ownerID,
    Timestamp:     record.CreatedAt,
})
```

- [ ] **Step 3: Run build and unit tests**

Run: `make build-server && make test-unit`
Expected: Build succeeds, tests pass

- [ ] **Step 4: Commit**

```bash
git add api/webhook_event_consumer.go api/addon_invocation_handlers.go
git commit -m "feat(api): WebhookEventConsumer handles addon.invoked events

addon.invoked events are now consumed from Redis Streams. The delivery
record (created by InvokeAddon) is updated with the subscription ID
and queued for the unified delivery worker.

Refs #220"
```

---

### Task 9: Add X-TMI-Callback: async Handling to WebhookDeliveryWorker

**Files:**
- Modify: `api/webhook_delivery_worker.go`

- [ ] **Step 1: Update deliverWebhook to check X-TMI-Callback response header**

Currently `deliverWebhook` marks all 2xx responses as "delivered". Update it to:
1. Check `resp.Header.Get("X-TMI-Callback")` after a successful delivery
2. If `"async"`, set status to `DeliveryStatusInProgress` instead of `DeliveryStatusDelivered`
3. If not async, set status to `DeliveryStatusDelivered` (current behavior)

- [ ] **Step 2: Switch WebhookDeliveryWorker from Postgres to Redis store**

Replace all references to `GlobalWebhookDeliveryStore` (Postgres) with `GlobalWebhookDeliveryRedisStore` (Redis) in the worker. The Redis store methods have slightly different signatures (take `context.Context` as first parameter).

Update `processPendingDeliveries`:
- `GlobalWebhookDeliveryRedisStore.ListPending(ctx, 100)` instead of `GlobalWebhookDeliveryStore.ListPending(100)`
- `GlobalWebhookDeliveryRedisStore.ListReadyForRetry(ctx)` instead of `GlobalWebhookDeliveryStore.ListReadyForRetry()`

Update `deliverWebhook`:
- `GlobalWebhookDeliveryRedisStore.UpdateStatus(ctx, ...)` instead of `GlobalWebhookDeliveryStore.UpdateStatus(...)`

Update `handleDeliveryFailure`:
- `GlobalWebhookDeliveryRedisStore.UpdateStatus(ctx, ...)` and `GlobalWebhookDeliveryRedisStore.UpdateRetry(ctx, ...)` instead of Postgres equivalents

The worker needs to accept `WebhookDeliveryRecord` instead of `DBWebhookDelivery`. Update method signatures accordingly.

- [ ] **Step 3: Run build**

Run: `make build-server`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add api/webhook_delivery_worker.go
git commit -m "feat(api): WebhookDeliveryWorker uses Redis store with async callback support

Switches from Postgres GlobalWebhookDeliveryStore to Redis
GlobalWebhookDeliveryRedisStore. Adds X-TMI-Callback: async response
header handling — deliveries with async callbacks get in_progress
status instead of delivered.

Refs #220"
```

---

### Task 10: Unify Cleanup Worker

**Files:**
- Modify: `api/webhook_cleanup_worker.go`

- [ ] **Step 1: Add stale delivery cleanup to WebhookCleanupWorker**

Add a `cleanupStaleDeliveries` method that:
1. Calls `GlobalWebhookDeliveryRedisStore.ListStale(ctx, DeliveryStaleTimeout)`
2. Marks each stale delivery as failed
3. Increments timeout count on the associated webhook subscription (for addon invocations, look up the addon to find the webhook)

Add this to the existing `performCleanup` method alongside the existing Postgres cleanup operations.

- [ ] **Step 2: Remove cleanupOldDeliveries call from performCleanup**

The `cleanupOldDeliveries` method deletes old Postgres delivery records. This will be fully removed in Phase 3, but for now keep it as a no-op fallback (it won't find records once Phase 3 migrates everything).

- [ ] **Step 3: Run build and tests**

Run: `make build-server && make test-unit`
Expected: Build succeeds, tests pass

- [ ] **Step 4: Commit**

```bash
git add api/webhook_cleanup_worker.go
git commit -m "feat(api): unified cleanup worker handles Redis delivery timeouts

WebhookCleanupWorker now cleans up stale in_progress deliveries from
Redis (15-minute inactivity timeout). Replaces the separate
AddonInvocationCleanupWorker.

Refs #220"
```

---

### Task 11: Remove Old Addon Invocation Infrastructure

**Files:**
- Delete: `api/addon_invocation_store.go`
- Delete: `api/addon_invocation_worker.go`
- Delete: `api/addon_invocation_worker_test.go`
- Delete: `api/addon_invocation_cleanup_worker.go`
- Delete: `api/addon_invocation_handlers_test.go`
- Modify: `api/addon_type_converters.go` (remove invocation converters)
- Modify: `cmd/server/main.go` (remove old worker initialization)

- [ ] **Step 1: Delete old files**

```bash
rm api/addon_invocation_store.go
rm api/addon_invocation_worker.go
rm api/addon_invocation_worker_test.go
rm api/addon_invocation_cleanup_worker.go
rm api/addon_invocation_handlers_test.go
```

- [ ] **Step 2: Clean up addon_type_converters.go**

Remove functions that reference deleted types:
- `statusToInvokeAddonResponseStatus` — keep if still used by InvokeAddon
- `statusToInvocationResponseStatus` — remove (InvocationResponse no longer exists)
- `statusToUpdateResponseStatus` — remove (UpdateInvocationStatusResponseStatus no longer exists)
- `statusFromUpdateRequestStatus` — remove (UpdateInvocationStatusRequestStatus no longer exists)
- `invocationToResponse` — remove (InvocationResponse no longer exists)

Keep: `fromIntPtr`, `toStringSlicePtr`, `toObjectTypeString`, `payloadToString`, `addonToResponse`, and any converters still used.

- [ ] **Step 3: Update cmd/server/main.go**

In `startWebhookWorkers`:
- Remove `AddonInvocationWorker` creation and start (lines ~1063-1067)
- Remove `AddonInvocationCleanupWorker` creation and start (lines ~1070-1075)
- Add `GlobalWebhookDeliveryRedisStore` initialization

In Redis services phase:
- Remove `GlobalAddonInvocationStore` initialization (line ~787)
- Add: `api.GlobalWebhookDeliveryRedisStore = api.NewWebhookDeliveryRedisStore(dbManager.Redis())`

- [ ] **Step 4: Move WebhookDeliveryPayload and WebhookDeliveryData types**

The `WebhookDeliveryPayload` and `WebhookDeliveryData` types were defined in the now-deleted `addon_invocation_worker.go`. Move them to `api/webhook_delivery_worker.go` (where they're used for building payloads) or to a shared location.

Also move the `VerifySignature` function if it's still referenced.

- [ ] **Step 5: Run build**

Run: `make build-server`
Expected: Build succeeds. Fix any compilation errors from removed references.

- [ ] **Step 6: Run lint**

Run: `make lint`
Expected: No new lint errors

- [ ] **Step 7: Run unit tests**

Run: `make test-unit`
Expected: All tests pass

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "refactor(api): remove old addon invocation infrastructure

Removes AddonInvocationStore, AddonInvocationWorker,
AddonInvocationCleanupWorker, and related handlers/tests. All addon
invocation delivery now flows through the unified Redis-backed
WebhookDeliveryRedisStore and WebhookDeliveryWorker.

Refs #220"
```

---

### Task 12: Write Tests for New Delivery Handlers

**Files:**
- Create: `api/webhook_delivery_handlers_test.go`

- [ ] **Step 1: Write tests for GetWebhookDeliveryStatus**

Test cases:
- `Success_AdminCanGet` — admin JWT gets any delivery
- `Success_InvokerCanGet` — invoker (matching InvokedByUUID) gets own delivery
- `Success_SubscriptionOwnerCanGet` — subscription owner gets delivery
- `Success_HMACAuth` — webhook receiver gets delivery with valid HMAC
- `NotFound` — returns 404 for nonexistent delivery
- `Forbidden_NonOwner` — non-admin, non-invoker, non-owner gets 403
- `Forbidden_InvalidHMAC` — invalid HMAC signature gets 403

- [ ] **Step 2: Write tests for UpdateWebhookDeliveryStatus**

Test cases:
- `Success_InProgress` — HMAC-authed callback sets in_progress
- `Success_Completed` — HMAC-authed callback with "completed" maps to "delivered"
- `Success_Failed` — HMAC-authed callback sets failed
- `Conflict_AlreadyDelivered` — returns 409 for delivered delivery
- `Conflict_AlreadyFailed` — returns 409 for failed delivery
- `Unauthorized_MissingSignature` — returns 401 without HMAC
- `Unauthorized_InvalidSignature` — returns 401 with bad HMAC
- `BadRequest_InvalidStatus` — returns 400 for invalid status value
- `ResetsTimeoutCount` — completed status resets webhook timeout count

- [ ] **Step 3: Run tests**

Run: `make test-unit name=TestGetWebhookDeliveryStatus`
Run: `make test-unit name=TestUpdateWebhookDeliveryStatus`
Expected: All tests pass

- [ ] **Step 4: Commit**

```bash
git add api/webhook_delivery_handlers_test.go
git commit -m "test(api): add tests for unified webhook delivery handlers

Tests GET /webhook-deliveries/{id} (dual JWT/HMAC auth, authorization
checks) and POST /webhook-deliveries/{id}/status (HMAC auth, status
transitions, completed→delivered mapping, timeout reset).

Refs #220"
```

---

### Task 13: Update Admin Webhook Delivery Endpoints to Use Redis Store

**Files:**
- Modify: `api/webhook_handlers.go`

- [ ] **Step 1: Repoint ListWebhookDeliveries to Redis store**

Update `ListWebhookDeliveries` to use `GlobalWebhookDeliveryRedisStore.ListAll()` or `GlobalWebhookDeliveryRedisStore.ListBySubscription()` instead of the Postgres store.

Convert `WebhookDeliveryRecord` to `WebhookDelivery` API response using `deliveryRecordToWebhookDelivery()`.

- [ ] **Step 2: Repoint GetWebhookDelivery to Redis store**

Update `GetWebhookDelivery` to use `GlobalWebhookDeliveryRedisStore.Get()` instead of Postgres.

- [ ] **Step 3: Run build and tests**

Run: `make build-server && make test-unit`
Expected: Build succeeds, tests pass

- [ ] **Step 4: Commit**

```bash
git add api/webhook_handlers.go
git commit -m "refactor(api): admin delivery endpoints use Redis store

ListWebhookDeliveries and GetWebhookDelivery now read from
GlobalWebhookDeliveryRedisStore instead of Postgres.

Refs #220"
```

---

### Task 14: Update WebhookEventConsumer to Create Redis Delivery Records (Phase 3)

**Files:**
- Modify: `api/webhook_event_consumer.go`

- [ ] **Step 1: Update createDelivery to use Redis store for all events**

Replace the `createDelivery` method to create `WebhookDeliveryRecord` in Redis instead of `DBWebhookDelivery` in Postgres:

```go
func (c *WebhookEventConsumer) createDelivery(_ context.Context, subscription DBWebhookSubscription, eventType, payload string) error {
	logger := slogging.Get()
	ctx := context.Background()

	record := &WebhookDeliveryRecord{
		ID:             uuid.Must(uuid.NewV7()),
		SubscriptionID: subscription.Id,
		EventType:      eventType,
		Payload:        payload,
		Status:         DeliveryStatusPending,
	}

	if err := GlobalWebhookDeliveryRedisStore.Create(ctx, record); err != nil {
		return fmt.Errorf("failed to create delivery record: %w", err)
	}

	logger.Debug("created delivery %s for subscription %s", record.ID, subscription.Id)
	return nil
}
```

- [ ] **Step 2: Rename EventPayload fields ResourceID/ResourceType → ObjectID/ObjectType**

In `api/events.go`, rename the fields in `EventPayload`:

```go
type EventPayload struct {
	EventType     string         `json:"event_type"`
	ThreatModelID string         `json:"threat_model_id,omitempty"`
	ObjectID      string         `json:"object_id"`      // was ResourceID
	ObjectType    string         `json:"object_type"`     // was ResourceType
	OwnerID       string         `json:"owner_id"`
	Timestamp     time.Time      `json:"timestamp"`
	Data          map[string]any `json:"data,omitempty"`
}
```

Update all references to `ResourceID` → `ObjectID` and `ResourceType` → `ObjectType` across the codebase. Use grep to find all occurrences:
- `api/events.go` — struct definition and `EmitEvent` stream values
- `api/webhook_event_consumer.go` — `processMessage` field extraction
- All callers of `GlobalEventEmitter.EmitEvent()` that set `ResourceID`/`ResourceType`

- [ ] **Step 3: Run build and tests**

Run: `make build-server && make test-unit`
Expected: Build succeeds, tests pass

- [ ] **Step 4: Commit**

```bash
git add api/webhook_event_consumer.go api/events.go
git commit -m "refactor(api): event consumer creates Redis delivery records

All webhook deliveries (resource-change and addon) now go through
Redis. Renames EventPayload.ResourceID/ResourceType to
ObjectID/ObjectType per design spec.

Refs #220"
```

---

### Task 15: Remove Postgres Webhook Delivery Infrastructure (Phase 3)

**Files:**
- Modify: `api/webhook_store.go` — remove `DBWebhookDelivery`, `WebhookDeliveryStoreInterface`, `GlobalWebhookDeliveryStore`
- Modify: `api/webhook_store_gorm.go` — remove `GormWebhookDeliveryStore` implementation
- Modify: `api/store.go` — remove delivery store initialization from `InitializeGormStores`
- Modify: `api/webhook_cleanup_worker.go` — remove `cleanupOldDeliveries` method and its call

- [ ] **Step 1: Remove DBWebhookDelivery and WebhookDeliveryStoreInterface from webhook_store.go**

Remove the `DBWebhookDelivery` struct (lines 40-51), `WebhookDeliveryStoreInterface` interface (lines 108-122), and `GlobalWebhookDeliveryStore` global variable (line 144).

- [ ] **Step 2: Remove GormWebhookDeliveryStore from webhook_store_gorm.go**

Remove the entire `GormWebhookDeliveryStore` struct and all its methods (lines ~507-838). Also remove `NewGormWebhookDeliveryStore`.

- [ ] **Step 3: Remove delivery store initialization from store.go**

In `InitializeGormStores`, remove the line that creates `GlobalWebhookDeliveryStore`:
```go
// Remove this line:
GlobalWebhookDeliveryStore = NewGormWebhookDeliveryStore(db)
```

- [ ] **Step 4: Remove cleanupOldDeliveries from webhook_cleanup_worker.go**

Remove the `cleanupOldDeliveries` method and its call from `performCleanup`. The Redis store handles TTL-based expiry automatically.

- [ ] **Step 5: Run build and lint**

Run: `make build-server && make lint`
Expected: Build succeeds, no lint errors. Fix any remaining references to removed types.

- [ ] **Step 6: Run all unit tests**

Run: `make test-unit`
Expected: All tests pass

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor(api): remove Postgres webhook delivery infrastructure

Removes DBWebhookDelivery, WebhookDeliveryStoreInterface,
GormWebhookDeliveryStore, and GlobalWebhookDeliveryStore. All webhook
deliveries are now stored in Redis with TTL-based expiry.

Refs #220"
```

---

### Task 16: Add Migration to Drop webhook_deliveries Postgres Table

**Files:**
- Create: `auth/migrations/xxx_drop_webhook_deliveries.go`

- [ ] **Step 1: Determine next migration number**

```bash
ls auth/migrations/ | tail -5
```

Use the next sequential number.

- [ ] **Step 2: Create the migration**

```go
// auth/migrations/NNN_drop_webhook_deliveries.go
package migrations

import (
	"gorm.io/gorm"
)

func MigrateNNN(db *gorm.DB) error {
	return db.Exec("DROP TABLE IF EXISTS db_webhook_deliveries").Error
}
```

Register the migration in the migration runner (check `auth/migrations/migrations.go` or similar for the registration pattern).

- [ ] **Step 3: Run build**

Run: `make build-server`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add auth/migrations/
git commit -m "chore(db): add migration to drop webhook_deliveries table

The webhook_deliveries Postgres table is replaced by Redis-backed
delivery records. This migration drops the now-unused table.

Refs #220"
```

---

### Task 17: Final Integration Verification

- [ ] **Step 1: Run full lint**

Run: `make lint`
Expected: No new lint errors

- [ ] **Step 2: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass

- [ ] **Step 3: Run integration tests**

Run: `make test-integration`
Expected: All tests pass

- [ ] **Step 4: Run CATS fuzzing**

Run: `make cats-fuzz`
Then: `make analyze-cats-results`
Expected: No new true-positive errors

- [ ] **Step 5: Final commit if any fixes were needed**

```bash
git add -A
git commit -m "fix(api): address integration test and fuzzing findings

Fixes issues discovered during integration testing and CATS fuzzing
of the unified webhook delivery pipeline.

Closes #220"
```

---

## Dependency Graph

```
Task 1 (types/interface) → Task 2 (CRUD) → Task 3 (queries) → Task 4 (tests)
                                                                      ↓
Task 5 (OpenAPI spec) → Task 6 (new handlers) ─────────────────→ Task 7 (InvokeAddon)
                                                                      ↓
                                                              Task 8 (event consumer)
                                                                      ↓
                                                              Task 9 (delivery worker)
                                                                      ↓
                                                              Task 10 (cleanup worker)
                                                                      ↓
                                                              Task 11 (remove old code)
                                                                      ↓
                                                              Task 12 (handler tests)
                                                                      ↓
                                                              Task 13 (admin endpoints)
                                                                      ↓
                                                              Task 14 (Phase 3: Redis for all)
                                                                      ↓
                                                              Task 15 (remove Postgres delivery)
                                                                      ↓
                                                              Task 16 (migration)
                                                                      ↓
                                                              Task 17 (integration verification)
```

Tasks 1-4 and Task 5 can run in parallel. All other tasks are sequential.
