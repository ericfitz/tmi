package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
)

// Event type constants for webhook emissions
const (
	// Threat Model Events
	EventThreatModelCreated = "threat_model.created"
	EventThreatModelUpdated = "threat_model.updated"
	EventThreatModelDeleted = "threat_model.deleted"

	// Diagram Events
	EventDiagramCreated = "diagram.created"
	EventDiagramUpdated = "diagram.updated"
	EventDiagramDeleted = "diagram.deleted"

	// Document Events
	EventDocumentCreated = "document.created"
	EventDocumentUpdated = "document.updated"
	EventDocumentDeleted = "document.deleted"

	// Note Events
	EventNoteCreated = "note.created"
	EventNoteUpdated = "note.updated"
	EventNoteDeleted = "note.deleted"

	// Repository Events
	EventRepositoryCreated = "repository.created"
	EventRepositoryUpdated = "repository.updated"
	EventRepositoryDeleted = "repository.deleted"

	// Asset Events
	EventAssetCreated = "asset.created"
	EventAssetUpdated = "asset.updated"
	EventAssetDeleted = "asset.deleted"

	// Threat Events
	EventThreatCreated = "threat.created"
	EventThreatUpdated = "threat.updated"
	EventThreatDeleted = "threat.deleted"

	// Metadata Events
	EventMetadataCreated = "metadata.created"
	EventMetadataUpdated = "metadata.updated"
	EventMetadataDeleted = "metadata.deleted"

	// Addon Events
	EventAddonInvoked = "addon.invoked"
)

// EventPayload represents the structure of an event emitted to Redis
type EventPayload struct {
	EventType     string                 `json:"event_type"`
	ThreatModelID string                 `json:"threat_model_id,omitempty"`
	ResourceID    string                 `json:"resource_id"`
	ResourceType  string                 `json:"resource_type"`
	OwnerID       string                 `json:"owner_id"`
	Timestamp     time.Time              `json:"timestamp"`
	Data          map[string]interface{} `json:"data,omitempty"`
}

// EventEmitter handles event emission to Redis Streams
type EventEmitter struct {
	redisClient *redis.Client
	streamKey   string
}

// NewEventEmitter creates a new event emitter
func NewEventEmitter(redisClient *redis.Client, streamKey string) *EventEmitter {
	return &EventEmitter{
		redisClient: redisClient,
		streamKey:   streamKey,
	}
}

// EmitEvent emits an event to Redis Stream with deduplication
func (e *EventEmitter) EmitEvent(ctx context.Context, payload EventPayload) error {
	logger := slogging.Get()

	// Set timestamp if not already set
	if payload.Timestamp.IsZero() {
		payload.Timestamp = time.Now().UTC()
	}

	// Check for Redis availability
	if e.redisClient == nil {
		logger.Warn("Redis client not available, skipping event emission for %s", payload.EventType)
		return nil
	}

	// Generate deduplication key (event type + resource ID + timestamp window)
	dedupKey := e.generateDedupKey(payload)
	dedupTTL := 60 * time.Second

	// Check if this event was recently emitted
	exists, err := e.redisClient.SetNX(ctx, dedupKey, "1", dedupTTL).Result()
	if err != nil {
		logger.Error("failed to check event deduplication: %v", err)
		// Continue with emission - better to have duplicates than miss events
	} else if !exists {
		logger.Debug("skipping duplicate event: %s for resource %s", payload.EventType, payload.ResourceID)
		return nil
	}

	// Serialize payload to JSON
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to serialize event payload: %v", err)
		return fmt.Errorf("failed to serialize event payload: %w", err)
	}

	// Emit to Redis Stream
	values := map[string]interface{}{
		"event_type":      payload.EventType,
		"threat_model_id": payload.ThreatModelID,
		"resource_id":     payload.ResourceID,
		"resource_type":   payload.ResourceType,
		"owner_id":        payload.OwnerID,
		"timestamp":       payload.Timestamp.Format(time.RFC3339),
		"payload":         string(payloadJSON),
	}

	_, err = e.redisClient.XAdd(ctx, &redis.XAddArgs{
		Stream: e.streamKey,
		Values: values,
	}).Result()

	if err != nil {
		logger.Error("failed to emit event to Redis Stream: %v", err)
		// Don't fail the CRUD operation - log and continue
		return nil
	}

	logger.Debug("emitted event: %s for resource %s (owner: %s)", payload.EventType, payload.ResourceID, payload.OwnerID)
	return nil
}

// generateDedupKey creates a deduplication key for the event
func (e *EventEmitter) generateDedupKey(payload EventPayload) string {
	// Create a hash of event type + resource ID + timestamp (rounded to 5-second window)
	timestamp := payload.Timestamp.Truncate(5 * time.Second).Unix()
	data := fmt.Sprintf("%s:%s:%d", payload.EventType, payload.ResourceID, timestamp)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("event:dedup:%x", hash[:8])
}

// Global event emitter instance
var GlobalEventEmitter *EventEmitter

// Global auth service for owner UUID lookups
var GlobalAuthServiceForEvents AuthService

// InitializeEventEmitter initializes the global event emitter
func InitializeEventEmitter(redisClient *redis.Client, streamKey string) {
	GlobalEventEmitter = NewEventEmitter(redisClient, streamKey)
}

// SetGlobalAuthServiceForEvents sets the global auth service for event owner lookups
func SetGlobalAuthServiceForEvents(authService AuthService) {
	GlobalAuthServiceForEvents = authService
}

// GetOwnerInternalUUID looks up the owner's internal UUID from provider and provider_id
// Returns the provider_id if lookup fails (fallback for tests/in-memory mode)
func GetOwnerInternalUUID(ctx context.Context, provider, providerID string) string {
	if GlobalAuthServiceForEvents == nil {
		slogging.Get().Warn("GlobalAuthServiceForEvents not set - using provider_id as fallback")
		return providerID
	}

	// Get the underlying auth.Service from the adapter
	adapter, ok := GlobalAuthServiceForEvents.(*AuthServiceAdapter)
	if !ok {
		slogging.Get().Warn("Auth service is not AuthServiceAdapter - using provider_id as fallback")
		return providerID
	}

	authService := adapter.GetService()
	if authService == nil {
		slogging.Get().Warn("Auth service not available - using provider_id as fallback")
		return providerID
	}

	// Look up user by provider and provider_id
	user, err := authService.GetUserByProviderID(ctx, provider, providerID)
	if err != nil {
		slogging.Get().Warn("Failed to lookup user by provider=%s, provider_id=%s: %v - using provider_id as fallback", provider, providerID, err)
		return providerID
	}

	return user.InternalUUID
}
