package api

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// DBWebhookSubscription represents a webhook subscription in the database
type DBWebhookSubscription struct {
	Id            uuid.UUID  `json:"id"`
	OwnerId       uuid.UUID  `json:"owner_id"`
	ThreatModelId *uuid.UUID `json:"threat_model_id,omitempty"` // NULL means all threat models
	Name          string     `json:"name"`
	Url           string     `json:"url"`
	Events        []string   `json:"events"`
	//nolint:gosec // G117 - webhook HMAC signing secret
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

// WebhookSubscriptionStoreInterface defines operations for webhook subscriptions.
// All methods accept a context.Context so the underlying GORM transaction
// retry wrapper can use the caller-supplied context for cancellation
// instead of falling back to context.Background(). This makes Oracle
// ORA-08177/ORA-00060 retry chains cancellable when the originating HTTP
// request is cancelled.
type WebhookSubscriptionStoreInterface interface {
	Get(ctx context.Context, id string) (DBWebhookSubscription, error)
	List(ctx context.Context, offset, limit int, filter func(DBWebhookSubscription) bool) []DBWebhookSubscription
	ListByOwner(ctx context.Context, ownerID string, offset, limit int) ([]DBWebhookSubscription, error)
	ListByThreatModel(ctx context.Context, threatModelID string, offset, limit int) ([]DBWebhookSubscription, error)
	ListActiveByOwner(ctx context.Context, ownerID string) ([]DBWebhookSubscription, error)
	ListPendingVerification(ctx context.Context) ([]DBWebhookSubscription, error)
	ListPendingDelete(ctx context.Context) ([]DBWebhookSubscription, error)
	ListIdle(ctx context.Context, daysIdle int) ([]DBWebhookSubscription, error)
	ListBroken(ctx context.Context, minFailures int, daysSinceSuccess int) ([]DBWebhookSubscription, error)
	Create(ctx context.Context, item DBWebhookSubscription, idSetter func(DBWebhookSubscription, string) DBWebhookSubscription) (DBWebhookSubscription, error)
	Update(ctx context.Context, id string, item DBWebhookSubscription) error
	UpdateStatus(ctx context.Context, id string, status string) error
	UpdateChallenge(ctx context.Context, id string, challenge string, challengesSent int) error
	UpdatePublicationStats(ctx context.Context, id string, success bool) error
	IncrementTimeouts(ctx context.Context, id string) error
	ResetTimeouts(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) int
	CountByOwner(ctx context.Context, ownerID string) (int, error)
}

// WebhookQuotaStoreInterface defines operations for webhook quotas.
// See WebhookSubscriptionStoreInterface for ctx threading rationale.
type WebhookQuotaStoreInterface interface {
	Get(ctx context.Context, ownerID string) (DBWebhookQuota, error)
	GetOrDefault(ctx context.Context, ownerID string) DBWebhookQuota
	List(ctx context.Context, offset, limit int) ([]DBWebhookQuota, error)
	Count(ctx context.Context) (int, error)
	Create(ctx context.Context, item DBWebhookQuota) (DBWebhookQuota, error)
	Update(ctx context.Context, ownerID string, item DBWebhookQuota) error
	Delete(ctx context.Context, ownerID string) error
}

// WebhookUrlDenyListStoreInterface defines operations for URL deny list.
// See WebhookSubscriptionStoreInterface for ctx threading rationale.
type WebhookUrlDenyListStoreInterface interface {
	List(ctx context.Context) ([]WebhookUrlDenyListEntry, error)
	Create(ctx context.Context, item WebhookUrlDenyListEntry) (WebhookUrlDenyListEntry, error)
	Delete(ctx context.Context, id string) error
}

// Global webhook store instances
var GlobalWebhookSubscriptionStore WebhookSubscriptionStoreInterface
var GlobalWebhookQuotaStore WebhookQuotaStoreInterface
var GlobalWebhookUrlDenyListStore WebhookUrlDenyListStoreInterface

// Default quota values
const (
	DefaultMaxSubscriptions                 = 10
	DefaultMaxEventsPerMinute               = 12
	DefaultMaxSubscriptionRequestsPerMinute = 10
	DefaultMaxSubscriptionRequestsPerDay    = 20
)
