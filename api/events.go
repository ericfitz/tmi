package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/redis/go-redis/v9"
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

	// Survey Events
	EventSurveyCreated = "survey.created"
	EventSurveyUpdated = "survey.updated"
	EventSurveyDeleted = "survey.deleted"

	// Survey Response Events
	EventSurveyResponseCreated = "survey_response.created"
	EventSurveyResponseUpdated = "survey_response.updated"
	EventSurveyResponseDeleted = "survey_response.deleted"

	// Team events
	EventTeamCreated = "team.created"
	EventTeamUpdated = "team.updated"
	EventTeamDeleted = "team.deleted"

	// Project events
	EventProjectCreated = "project.created"
	EventProjectUpdated = "project.updated"
	EventProjectDeleted = "project.deleted"

	// Addon Events
	EventAddonInvoked = "addon.invoked"

	// Document extraction outcome events (async pipeline, #347)
	EventDocumentExtractionCompleted = "document.extraction_completed"
	EventDocumentExtractionFailed    = "document.extraction_failed"

	// System audit events (T7, #395): emitted for every system_audit_entries
	// write (admin-write middleware and step-up adapter paths).
	EventSystemAuditAdminWrite = "system_audit.admin_write"
)

// EventPayload represents the structure of an event emitted to Redis
// SEM@b2651daf2f388dbef96d4ddd4a0bb46fcb8da56b: struct carrying event type, resource references, owner, source addon, and data for a Redis Stream entry (pure)
type EventPayload struct {
	EventType     string         `json:"event_type"`
	ThreatModelID string         `json:"threat_model_id,omitempty"`
	ObjectID      string         `json:"object_id"`
	ObjectType    string         `json:"object_type"`
	OwnerID       string         `json:"owner_id"`
	Timestamp     time.Time      `json:"timestamp"`
	Data          map[string]any `json:"data,omitempty"`
	// SourceAddonID is set when the request that caused the event acted under
	// an addon delegation token (#876). The webhook consumer uses it to skip
	// delivering the event back to that addon's own subscription.
	SourceAddonID string `json:"source_addon_id,omitempty"`
}

// SEM@9ea792b9df3b1ab947a5ab9a404a0fbccd779d21: context key type for the delegating addon of a request (pure)
type sourceAddonIDContextKey struct{}

// SEM@b2651daf2f388dbef96d4ddd4a0bb46fcb8da56b: build a context tagging subsequent events with the delegating addon (pure)
func WithSourceAddonID(ctx context.Context, addonID string) context.Context {
	return context.WithValue(ctx, sourceAddonIDContextKey{}, addonID)
}

// SEM@b2651daf2f388dbef96d4ddd4a0bb46fcb8da56b: fetch the delegating addon ID from a context, empty if none (pure)
func SourceAddonIDFromContext(ctx context.Context) string {
	s, _ := ctx.Value(sourceAddonIDContextKey{}).(string)
	return s
}

// EventEmitter handles event emission to Redis Streams
// SEM@9ea792b9df3b1ab947a5ab9a404a0fbccd779d21: struct that emits deduplicated resource events to a Redis Stream
type EventEmitter struct {
	redisClient *redis.Client
	streamKey   string
}

// NewEventEmitter creates a new event emitter
// SEM@9ea792b9df3b1ab947a5ab9a404a0fbccd779d21: build an event emitter bound to a Redis client and stream key (pure)
func NewEventEmitter(redisClient *redis.Client, streamKey string) *EventEmitter {
	return &EventEmitter{
		redisClient: redisClient,
		streamKey:   streamKey,
	}
}

// EmitEvent emits an event to Redis Stream with deduplication
// SEM@b2651daf2f388dbef96d4ddd4a0bb46fcb8da56b: publish a deduplicated resource event tagged with any delegating addon to the Redis Stream; skips on duplicate or unavailable Redis
func (e *EventEmitter) EmitEvent(ctx context.Context, payload EventPayload) error {
	logger := slogging.Get()

	// Set timestamp if not already set
	if payload.Timestamp.IsZero() {
		payload.Timestamp = time.Now().UTC()
	}
	if payload.SourceAddonID == "" {
		payload.SourceAddonID = SourceAddonIDFromContext(ctx)
	}

	// Check for Redis availability
	if e.redisClient == nil {
		logger.Warn("Redis client not available, skipping event emission for %s", payload.EventType)
		return nil
	}

	// Generate deduplication key (event type + resource ID + timestamp window)
	dedupKey := e.generateDedupKey(payload)
	dedupTTL := 10 * time.Second

	// Check if this event was recently emitted
	dedupErr := e.redisClient.SetArgs(ctx, dedupKey, "1", redis.SetArgs{Mode: "NX", TTL: dedupTTL}).Err()
	if dedupErr != nil && !errors.Is(dedupErr, redis.Nil) {
		logger.Error("failed to check event deduplication: %v", dedupErr)
		// Continue with emission - better to have duplicates than miss events
	} else if errors.Is(dedupErr, redis.Nil) {
		// Key already exists — this is a duplicate, skip emission
		logger.Debug("skipping duplicate event: %s for object %s", payload.EventType, payload.ObjectID)
		return nil
	}

	// Serialize payload to JSON
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to serialize event payload: %v", err)
		return fmt.Errorf("failed to serialize event payload: %w", err)
	}

	// Emit to Redis Stream
	values := map[string]any{
		"event_type":      payload.EventType,
		"threat_model_id": payload.ThreatModelID,
		"object_id":       payload.ObjectID,
		"object_type":     payload.ObjectType,
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

	logger.Debug("emitted event: %s for object %s (owner: %s)", payload.EventType, payload.ObjectID, payload.OwnerID)
	return nil
}

// generateDedupKey creates a deduplication key for the event
// SEM@00add3d4f7dc1c0a9cc072d7e6ca32ace4d03641: compute a short-lived deduplication cache key for an event from type, object ID, and timestamp window (pure)
func (e *EventEmitter) generateDedupKey(payload EventPayload) string {
	// Create a hash of event type + resource ID + timestamp (rounded to 1-second window)
	timestamp := payload.Timestamp.Truncate(1 * time.Second).Unix()
	data := fmt.Sprintf("%s:%s:%d", payload.EventType, payload.ObjectID, timestamp)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("event:dedup:%x", hash[:8])
}

// Global event emitter instance
var GlobalEventEmitter *EventEmitter

// Global auth service for owner UUID lookups
var GlobalAuthServiceForEvents AuthService

// InitializeEventEmitter initializes the global event emitter
// SEM@9ea792b9df3b1ab947a5ab9a404a0fbccd779d21: initialize the global event emitter with the given Redis client and stream key (mutates shared state)
func InitializeEventEmitter(redisClient *redis.Client, streamKey string) {
	GlobalEventEmitter = NewEventEmitter(redisClient, streamKey)
}

// SetGlobalAuthServiceForEvents sets the global auth service for event owner lookups
// SEM@f26a80b2c254e75f44d8b4302b64ff465d4a2ac5: register the global auth service used for owner UUID lookups during event emission (mutates shared state)
func SetGlobalAuthServiceForEvents(authService AuthService) {
	GlobalAuthServiceForEvents = authService
}

// GetOwnerInternalUUID looks up the owner's internal UUID from provider and provider_id
// Returns the provider_id if lookup fails (fallback for tests/in-memory mode)
// SEM@f26a80b2c254e75f44d8b4302b64ff465d4a2ac5: resolve a user's internal UUID from provider and provider ID; falls back to provider ID on error (reads DB)
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

// threatModelOwnerInternalUUID resolves the owner id that webhook fan-out
// (ListActiveByOwner) keys on, for events emitted from a threat model's
// sub-resources. Empty when the threat model cannot be loaded.
// SEM@c161adfd8ba839441ccd825e818d342a09c63849: fetch a threat model owner's internal UUID for event fan-out (reads DB)
func threatModelOwnerInternalUUID(ctx context.Context, threatModelID string) string {
	if ThreatModelStore == nil {
		return ""
	}
	tm, err := ThreatModelStore.Get(threatModelID)
	if err != nil {
		return ""
	}
	return GetOwnerInternalUUID(ctx, tm.Owner.Provider, tm.Owner.ProviderId)
}
