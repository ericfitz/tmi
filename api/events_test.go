package api

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventEmitter_EmitEvent(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	emitter := NewEventEmitter(client, "test:events")
	ctx := context.Background()

	payload := EventPayload{
		EventType:     EventThreatModelCreated,
		ThreatModelID: uuid.New().String(),
		ResourceID:    uuid.New().String(),
		ResourceType:  "threat_model",
		OwnerID:       uuid.New().String(),
		Timestamp:     time.Now().UTC(),
		Data: map[string]any{
			"name": "Test Threat Model",
		},
	}

	// Emit event
	err := emitter.EmitEvent(ctx, payload)
	require.NoError(t, err)

	// Verify event was added to stream
	result, err := client.XLen(ctx, "test:events").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), result, "Stream should have 1 event")

	// Read the event
	messages, err := client.XRange(ctx, "test:events", "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, messages, 1)

	// Verify event data
	msg := messages[0]
	assert.Equal(t, payload.EventType, msg.Values["event_type"])
	assert.Equal(t, payload.ResourceID, msg.Values["resource_id"])
	assert.Equal(t, payload.OwnerID, msg.Values["owner_id"])
}

func TestEventEmitter_Deduplication(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	emitter := NewEventEmitter(client, "test:events")
	ctx := context.Background()

	payload := EventPayload{
		EventType:     EventThreatModelCreated,
		ThreatModelID: uuid.New().String(),
		ResourceID:    uuid.New().String(),
		ResourceType:  "threat_model",
		OwnerID:       uuid.New().String(),
		Timestamp:     time.Now().UTC(),
	}

	// Emit first event
	err := emitter.EmitEvent(ctx, payload)
	require.NoError(t, err)

	// Try to emit same event again (should be deduplicated within 5-second window)
	payload.Timestamp = time.Now().UTC() // Update timestamp slightly
	err = emitter.EmitEvent(ctx, payload)
	require.NoError(t, err)

	// Should still only have 1 event (second was deduplicated)
	result, err := client.XLen(ctx, "test:events").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), result, "Duplicate event should be deduplicated")
}

func TestEventEmitter_NilRedisClient(t *testing.T) {
	emitter := NewEventEmitter(nil, "test:events")
	ctx := context.Background()

	payload := EventPayload{
		EventType:  EventThreatModelCreated,
		ResourceID: uuid.New().String(),
		OwnerID:    uuid.New().String(),
	}

	// Should not error when Redis is nil
	err := emitter.EmitEvent(ctx, payload)
	assert.NoError(t, err, "Should gracefully handle nil Redis client")
}

func TestEventEmitter_AutoTimestamp(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	emitter := NewEventEmitter(client, "test:events")
	ctx := context.Background()

	payload := EventPayload{
		EventType:  EventThreatModelCreated,
		ResourceID: uuid.New().String(),
		OwnerID:    uuid.New().String(),
		// No timestamp set
	}

	err := emitter.EmitEvent(ctx, payload)
	require.NoError(t, err)

	// Read the event and verify timestamp was set
	messages, err := client.XRange(ctx, "test:events", "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, messages, 1)

	timestampStr := messages[0].Values["timestamp"].(string)
	eventTime, err := time.Parse(time.RFC3339, timestampStr)
	require.NoError(t, err)

	// Timestamp should be recent (within last minute)
	assert.WithinDuration(t, time.Now().UTC(), eventTime, 1*time.Minute,
		"Auto-generated timestamp should be recent")
}

func TestEventTypes_Constants(t *testing.T) {
	// Verify all event type constants are defined
	assert.Equal(t, "threat_model.created", EventThreatModelCreated)
	assert.Equal(t, "threat_model.updated", EventThreatModelUpdated)
	assert.Equal(t, "threat_model.deleted", EventThreatModelDeleted)
	assert.Equal(t, "diagram.created", EventDiagramCreated)
	assert.Equal(t, "diagram.updated", EventDiagramUpdated)
	assert.Equal(t, "diagram.deleted", EventDiagramDeleted)
	assert.Equal(t, "document.created", EventDocumentCreated)
	assert.Equal(t, "document.updated", EventDocumentUpdated)
	assert.Equal(t, "document.deleted", EventDocumentDeleted)
}
