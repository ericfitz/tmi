package api

import (
	"time"

	"github.com/google/uuid"
)

// DBWebhookSubscription represents a webhook subscription in the database
type DBWebhookSubscription struct {
	Id                  uuid.UUID  `json:"id"`
	OwnerId             uuid.UUID  `json:"owner_id"`
	ThreatModelId       *uuid.UUID `json:"threat_model_id,omitempty"` // NULL means all threat models
	Name                string     `json:"name"`
	Url                 string     `json:"url"`
	Events              []string   `json:"events"`
	Secret              string     `json:"secret,omitempty"`
	Status              string     `json:"status"` // pending_verification, active, pending_delete
	Challenge           string     `json:"challenge,omitempty"`
	ChallengesSent      int        `json:"challenges_sent"`
	CreatedAt           time.Time  `json:"created_at"`
	ModifiedAt          time.Time  `json:"modified_at"`
	LastSuccessfulUse   *time.Time `json:"last_successful_use,omitempty"`
	PublicationFailures int        `json:"publication_failures"`
	TimeoutCount        int        `json:"timeout_count"` // Count of consecutive addon invocation timeouts
}

// SetCreatedAt implements WithTimestamps
func (w *DBWebhookSubscription) SetCreatedAt(t time.Time) {
	w.CreatedAt = t
}

// SetModifiedAt implements WithTimestamps
func (w *DBWebhookSubscription) SetModifiedAt(t time.Time) {
	w.ModifiedAt = t
}

// DBWebhookDelivery represents a webhook delivery attempt in the database
type DBWebhookDelivery struct {
	Id             uuid.UUID  `json:"id"` // UUIDv7 for time-ordered IDs
	SubscriptionId uuid.UUID  `json:"subscription_id"`
	EventType      string     `json:"event_type"`
	Payload        string     `json:"payload"` // JSON string
	Status         string     `json:"status"`  // pending, delivered, failed
	Attempts       int        `json:"attempts"`
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
}

// DBWebhookQuota represents per-owner rate limits with database timestamps
// This is the internal database model; the API uses the generated WebhookQuota type
type DBWebhookQuota struct {
	OwnerId                          uuid.UUID `json:"owner_id"`
	MaxSubscriptions                 int       `json:"max_subscriptions"`
	MaxEventsPerMinute               int       `json:"max_events_per_minute"`
	MaxSubscriptionRequestsPerMinute int       `json:"max_subscription_requests_per_minute"`
	MaxSubscriptionRequestsPerDay    int       `json:"max_subscription_requests_per_day"`
	CreatedAt                        time.Time `json:"created_at"`
	ModifiedAt                       time.Time `json:"modified_at"`
}

// SetCreatedAt implements WithTimestamps for DBWebhookQuota
func (w *DBWebhookQuota) SetCreatedAt(t time.Time) {
	w.CreatedAt = t
}

// SetModifiedAt implements WithTimestamps for DBWebhookQuota
func (w *DBWebhookQuota) SetModifiedAt(t time.Time) {
	w.ModifiedAt = t
}

// WebhookUrlDenyListEntry represents a URL pattern to block
type WebhookUrlDenyListEntry struct {
	Id          uuid.UUID `json:"id"`
	Pattern     string    `json:"pattern"`
	PatternType string    `json:"pattern_type"` // glob, regex
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// WebhookSubscriptionStoreInterface defines operations for webhook subscriptions
type WebhookSubscriptionStoreInterface interface {
	Get(id string) (DBWebhookSubscription, error)
	List(offset, limit int, filter func(DBWebhookSubscription) bool) []DBWebhookSubscription
	ListByOwner(ownerID string, offset, limit int) ([]DBWebhookSubscription, error)
	ListByThreatModel(threatModelID string, offset, limit int) ([]DBWebhookSubscription, error)
	ListActiveByOwner(ownerID string) ([]DBWebhookSubscription, error)
	ListPendingVerification() ([]DBWebhookSubscription, error)
	ListPendingDelete() ([]DBWebhookSubscription, error)
	ListIdle(daysIdle int) ([]DBWebhookSubscription, error)
	ListBroken(minFailures int, daysSinceSuccess int) ([]DBWebhookSubscription, error)
	Create(item DBWebhookSubscription, idSetter func(DBWebhookSubscription, string) DBWebhookSubscription) (DBWebhookSubscription, error)
	Update(id string, item DBWebhookSubscription) error
	UpdateStatus(id string, status string) error
	UpdateChallenge(id string, challenge string, challengesSent int) error
	UpdatePublicationStats(id string, success bool) error
	IncrementTimeouts(id string) error
	ResetTimeouts(id string) error
	Delete(id string) error
	Count() int
	CountByOwner(ownerID string) (int, error)
}

// WebhookDeliveryStoreInterface defines operations for webhook deliveries
type WebhookDeliveryStoreInterface interface {
	Get(id string) (DBWebhookDelivery, error)
	List(offset, limit int, filter func(DBWebhookDelivery) bool) []DBWebhookDelivery
	ListBySubscription(subscriptionID string, offset, limit int) ([]DBWebhookDelivery, error)
	ListPending(limit int) ([]DBWebhookDelivery, error)
	ListReadyForRetry() ([]DBWebhookDelivery, error)
	Create(item DBWebhookDelivery) (DBWebhookDelivery, error)
	Update(id string, item DBWebhookDelivery) error
	UpdateStatus(id string, status string, deliveredAt *time.Time) error
	UpdateRetry(id string, attempts int, nextRetryAt *time.Time, lastError string) error
	Delete(id string) error
	DeleteOld(daysOld int) (int, error)
	Count() int
}

// WebhookQuotaStoreInterface defines operations for webhook quotas
type WebhookQuotaStoreInterface interface {
	Get(ownerID string) (DBWebhookQuota, error)
	GetOrDefault(ownerID string) DBWebhookQuota
	List(offset, limit int) ([]DBWebhookQuota, error)
	Create(item DBWebhookQuota) (DBWebhookQuota, error)
	Update(ownerID string, item DBWebhookQuota) error
	Delete(ownerID string) error
}

// WebhookUrlDenyListStoreInterface defines operations for URL deny list
type WebhookUrlDenyListStoreInterface interface {
	List() ([]WebhookUrlDenyListEntry, error)
	Create(item WebhookUrlDenyListEntry) (WebhookUrlDenyListEntry, error)
	Delete(id string) error
}

// Global webhook store instances
var GlobalWebhookSubscriptionStore WebhookSubscriptionStoreInterface
var GlobalWebhookDeliveryStore WebhookDeliveryStoreInterface
var GlobalWebhookQuotaStore WebhookQuotaStoreInterface
var GlobalWebhookUrlDenyListStore WebhookUrlDenyListStoreInterface

// Default quota values
const (
	DefaultMaxSubscriptions                 = 10
	DefaultMaxEventsPerMinute               = 12
	DefaultMaxSubscriptionRequestsPerMinute = 10
	DefaultMaxSubscriptionRequestsPerDay    = 20
)
