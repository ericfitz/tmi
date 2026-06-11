package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// consumerTestSubStore is a minimal WebhookSubscriptionStoreInterface stub for
// WebhookEventConsumer tests. Only the methods exercised by processMessage and
// processSystemAuditEvent are implemented substantively; the rest are no-ops.
type consumerTestSubStore struct {
	subs []DBWebhookSubscription
}

func (s *consumerTestSubStore) Get(_ context.Context, _ string) (DBWebhookSubscription, error) {
	return DBWebhookSubscription{}, nil
}
func (s *consumerTestSubStore) List(_ context.Context, _, _ int, _ func(DBWebhookSubscription) bool) []DBWebhookSubscription {
	return nil
}
func (s *consumerTestSubStore) ListByOwner(_ context.Context, _ string, _, _ int) ([]DBWebhookSubscription, error) {
	return nil, nil
}
func (s *consumerTestSubStore) ListByThreatModel(_ context.Context, _ string, _, _ int) ([]DBWebhookSubscription, error) {
	return nil, nil
}
func (s *consumerTestSubStore) ListActiveByOwner(_ context.Context, ownerID string) ([]DBWebhookSubscription, error) {
	var result []DBWebhookSubscription
	for _, sub := range s.subs {
		if sub.OwnerId.String() == ownerID && sub.Status == "active" {
			result = append(result, sub)
		}
	}
	return result, nil
}
func (s *consumerTestSubStore) ListActiveByEventType(_ context.Context, eventType string) ([]DBWebhookSubscription, error) {
	var result []DBWebhookSubscription
	for _, sub := range s.subs {
		if sub.Status != "active" {
			continue
		}
		for _, e := range sub.Events {
			if e == eventType || e == "*" {
				result = append(result, sub)
				break
			}
		}
	}
	return result, nil
}
func (s *consumerTestSubStore) ListPendingVerification(_ context.Context) ([]DBWebhookSubscription, error) {
	return nil, nil
}
func (s *consumerTestSubStore) ListPendingDelete(_ context.Context) ([]DBWebhookSubscription, error) {
	return nil, nil
}
func (s *consumerTestSubStore) ListIdle(_ context.Context, _ int) ([]DBWebhookSubscription, error) {
	return nil, nil
}
func (s *consumerTestSubStore) ListBroken(_ context.Context, _, _ int) ([]DBWebhookSubscription, error) {
	return nil, nil
}
func (s *consumerTestSubStore) Create(_ context.Context, item DBWebhookSubscription, idSetter func(DBWebhookSubscription, string) DBWebhookSubscription) (DBWebhookSubscription, error) {
	return item, nil
}
func (s *consumerTestSubStore) Update(_ context.Context, _ string, _ DBWebhookSubscription) error {
	return nil
}
func (s *consumerTestSubStore) UpdateStatus(_ context.Context, _, _ string) error { return nil }
func (s *consumerTestSubStore) UpdateChallenge(_ context.Context, _, _ string, _ int) error {
	return nil
}
func (s *consumerTestSubStore) UpdatePublicationStats(_ context.Context, _ string, _ bool) error {
	return nil
}
func (s *consumerTestSubStore) IncrementTimeouts(_ context.Context, _ string) error { return nil }
func (s *consumerTestSubStore) ResetTimeouts(_ context.Context, _ string) error     { return nil }
func (s *consumerTestSubStore) Delete(_ context.Context, _ string) error            { return nil }
func (s *consumerTestSubStore) Count(_ context.Context) int                         { return len(s.subs) }
func (s *consumerTestSubStore) CountByOwner(_ context.Context, _ string) (int, error) {
	return 0, nil
}

// capturingDeliveryStore records Create calls for assertion in consumer tests.
type capturingDeliveryStore struct {
	mockDeliveryRedisStore
	created []*WebhookDeliveryRecord
}

func (c *capturingDeliveryStore) Create(_ context.Context, record *WebhookDeliveryRecord) error {
	if record.ID == uuid.Nil {
		record.ID = uuid.New()
	}
	c.records[record.ID] = record
	c.created = append(c.created, record)
	return nil
}

// makeConsumerMessage builds a redis.XMessage with the fields processMessage expects.
func makeConsumerMessage(eventType, ownerID, objectID string, payload EventPayload) redis.XMessage {
	payloadBytes, _ := json.Marshal(payload)
	return redis.XMessage{
		ID: "1-0",
		Values: map[string]any{
			"event_type": eventType,
			"owner_id":   ownerID,
			"object_id":  objectID,
			"payload":    string(payloadBytes),
		},
	}
}

// TestProcessSystemAuditEvent_FansOutToMatchingSubscriptions verifies that a
// system_audit.* message is delivered to every active subscription that
// declares the event type, regardless of owner, without requiring a valid
// owner UUID in the message.
func TestProcessSystemAuditEvent_FansOutToMatchingSubscriptions(t *testing.T) {
	owner1 := uuid.New()
	owner2 := uuid.New()
	subID1 := uuid.New()
	subID2 := uuid.New()
	subID3 := uuid.New() // does NOT subscribe to the event type

	subStore := &consumerTestSubStore{
		subs: []DBWebhookSubscription{
			{Id: subID1, OwnerId: owner1, Status: "active", Events: []string{EventSystemAuditAdminWrite}, Url: "https://a.example.com"},
			{Id: subID2, OwnerId: owner2, Status: "active", Events: []string{EventSystemAuditAdminWrite}, Url: "https://b.example.com"},
			{Id: subID3, OwnerId: owner1, Status: "active", Events: []string{"diagram.updated"}, Url: "https://c.example.com"},
		},
	}

	deliveryStore := &capturingDeliveryStore{
		mockDeliveryRedisStore: mockDeliveryRedisStore{records: make(map[uuid.UUID]*WebhookDeliveryRecord)},
	}

	// Swap globals; restore after test.
	origSubStore := GlobalWebhookSubscriptionStore
	origDeliveryStore := GlobalWebhookDeliveryRedisStore
	GlobalWebhookSubscriptionStore = subStore
	GlobalWebhookDeliveryRedisStore = deliveryStore
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalWebhookDeliveryRedisStore = origDeliveryStore
	}()

	consumer := NewWebhookEventConsumer(nil, "stream", "group", "consumer-1")

	payload := EventPayload{
		EventType:  EventSystemAuditAdminWrite,
		ObjectID:   uuid.New().String(),
		ObjectType: "system_audit_entry",
		Timestamp:  time.Now().UTC(),
		Data:       map[string]any{"entry_id": uuid.New().String()},
	}
	// system_audit.* messages have an empty owner_id; pass the zero UUID.
	msg := makeConsumerMessage(EventSystemAuditAdminWrite, "", payload.ObjectID, payload)

	err := consumer.processMessage(context.Background(), msg)
	require.NoError(t, err)

	// Two matching subscriptions → two delivery records created.
	require.Len(t, deliveryStore.created, 2)
	deliveredSubIDs := map[uuid.UUID]bool{}
	for _, rec := range deliveryStore.created {
		deliveredSubIDs[rec.SubscriptionID] = true
		assert.Equal(t, EventSystemAuditAdminWrite, rec.EventType)
	}
	assert.True(t, deliveredSubIDs[subID1], "subscription 1 should receive delivery")
	assert.True(t, deliveredSubIDs[subID2], "subscription 2 should receive delivery")
	assert.False(t, deliveredSubIDs[subID3], "subscription 3 (wrong event type) must not receive delivery")
}

// TestProcessSystemAuditEvent_NoMatchingSubscriptions verifies that a
// system_audit.* message with no matching subscriptions is still handled
// successfully (returns nil — will be XAck'd) so the PEL is never poisoned.
func TestProcessSystemAuditEvent_NoMatchingSubscriptions(t *testing.T) {
	subStore := &consumerTestSubStore{} // no subscriptions at all

	deliveryStore := &capturingDeliveryStore{
		mockDeliveryRedisStore: mockDeliveryRedisStore{records: make(map[uuid.UUID]*WebhookDeliveryRecord)},
	}

	origSubStore := GlobalWebhookSubscriptionStore
	origDeliveryStore := GlobalWebhookDeliveryRedisStore
	GlobalWebhookSubscriptionStore = subStore
	GlobalWebhookDeliveryRedisStore = deliveryStore
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalWebhookDeliveryRedisStore = origDeliveryStore
	}()

	consumer := NewWebhookEventConsumer(nil, "stream", "group", "consumer-1")

	payload := EventPayload{EventType: EventSystemAuditAdminWrite, Timestamp: time.Now().UTC()}
	msg := makeConsumerMessage(EventSystemAuditAdminWrite, "", uuid.New().String(), payload)

	err := consumer.processMessage(context.Background(), msg)
	assert.NoError(t, err, "no matching subscriptions must not be an error; message must be XAck'd")
	assert.Empty(t, deliveryStore.created)
}

// TestProcessSystemAuditEvent_EmptyOwnerUUIDNoError verifies that the old
// code path — which parsed owner_id as a UUID and would error on empty input —
// is bypassed for system_audit.* events. The message must process cleanly even
// though owner_id is the empty string.
func TestProcessSystemAuditEvent_EmptyOwnerUUIDNoError(t *testing.T) {
	subStore := &consumerTestSubStore{}
	deliveryStore := &capturingDeliveryStore{
		mockDeliveryRedisStore: mockDeliveryRedisStore{records: make(map[uuid.UUID]*WebhookDeliveryRecord)},
	}

	origSubStore := GlobalWebhookSubscriptionStore
	origDeliveryStore := GlobalWebhookDeliveryRedisStore
	GlobalWebhookSubscriptionStore = subStore
	GlobalWebhookDeliveryRedisStore = deliveryStore
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalWebhookDeliveryRedisStore = origDeliveryStore
	}()

	consumer := NewWebhookEventConsumer(nil, "stream", "group", "consumer-1")

	payload := EventPayload{EventType: EventSystemAuditAdminWrite, Timestamp: time.Now().UTC()}
	// owner_id is explicitly the empty string — was previously fatal.
	msg := redis.XMessage{
		ID: "2-0",
		Values: map[string]any{
			"event_type": EventSystemAuditAdminWrite,
			"owner_id":   "",
			"object_id":  uuid.New().String(),
			"payload":    func() string { b, _ := json.Marshal(payload); return string(b) }(),
		},
	}

	err := consumer.processMessage(context.Background(), msg)
	assert.NoError(t, err, "empty owner_id must not cause an error for system_audit.* events")
}

// TestListActiveByEventType_StoreMethod exercises the consumerTestSubStore's
// ListActiveByEventType to verify the in-Go filter logic used by both the test
// stub and the GORM implementation.
func TestListActiveByEventType_StoreMethod(t *testing.T) {
	owner := uuid.New()
	store := &consumerTestSubStore{
		subs: []DBWebhookSubscription{
			{Id: uuid.New(), OwnerId: owner, Status: "active", Events: []string{EventSystemAuditAdminWrite}},
			{Id: uuid.New(), OwnerId: owner, Status: "active", Events: []string{"*"}},                                      // wildcard
			{Id: uuid.New(), OwnerId: owner, Status: "active", Events: []string{"diagram.updated"}},                        // different event
			{Id: uuid.New(), OwnerId: owner, Status: "pending_verification", Events: []string{EventSystemAuditAdminWrite}}, // not active
		},
	}

	result, err := store.ListActiveByEventType(context.Background(), EventSystemAuditAdminWrite)
	require.NoError(t, err)
	assert.Len(t, result, 2, "only active subs matching the event type (including wildcard) should be returned")
}
