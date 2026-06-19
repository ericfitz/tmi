package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// Boot-race note: there is no unique constraint on operator_pinned=true in the
// webhook_subscriptions table. On concurrent first-boots (e.g. multi-replica
// deployments where all replicas start simultaneously with no pre-existing pinned
// row), multiple instances may each call EnsurePinnedAlertSubscription and each
// create a pinned row. The result is a brief window of duplicate alert sinks,
// reconciled on the next boot cycle when List() returns the first row and the
// others are ignored (Update path). This produces duplicate alert deliveries —
// harmless noise — and never suppresses alerts. A future migration may add a
// partial unique index (operator_pinned=true) to eliminate the race.

// AlertingBootstrap holds the alerting configuration values needed to upsert
// the operator-pinned audit alert sink webhook subscription (#395).
// SEM@13c4215bf8e204da342579717f97f7393bb5fe2f: configuration for the operator-pinned audit alert sink webhook subscription
type AlertingBootstrap struct {
	Enabled bool
	URL     string
	Secret  string
}

// EnsurePinnedAlertSubscription upserts the single operator-pinned webhook
// subscription for out-of-band audit alerting (T7, #395).
//
// When cfg.Enabled is true:
//   - If no pinned subscription exists yet, a new active subscription is
//     created owned by the operator system user with the EventSystemAuditAdminWrite
//     event type.
//   - If a pinned subscription already exists, it is updated in-place with
//     the current URL/Secret from config and its status is set to "active".
//
// When cfg.Enabled is false:
//   - If a pinned subscription exists with status != "inactive", it is
//     deactivated (status set to "inactive").
//   - The function is a no-op if no pinned subscription exists.
//
// The denyListStore is used for URL validation when cfg.Enabled is true. A nil
// denyListStore skips URL validation (e.g., in unit tests).
//
// Returns the active/updated subscription (zero value if disabled / no-op).
// SEM@13c4215bf8e204da342579717f97f7393bb5fe2f: upsert or deactivate the operator-pinned audit alert webhook subscription from config (reads DB)
func EnsurePinnedAlertSubscription(
	ctx context.Context,
	store WebhookSubscriptionStoreInterface,
	denyListStore WebhookUrlDenyListStoreInterface,
	cfg AlertingBootstrap,
) (DBWebhookSubscription, error) {
	logger := slogging.Get()

	// Find any existing pinned subscription.
	existing := store.List(ctx, 0, 0, func(s DBWebhookSubscription) bool {
		return s.OperatorPinned
	})

	if !cfg.Enabled {
		// Deactivate the pinned subscription if it exists and is active.
		if len(existing) == 0 {
			logger.Debug("alerting disabled and no pinned subscription exists — no-op")
			return DBWebhookSubscription{}, nil
		}
		sub := existing[0]
		if sub.Status != "inactive" {
			if err := store.UpdateStatus(ctx, sub.Id.String(), "inactive"); err != nil {
				return DBWebhookSubscription{}, fmt.Errorf("deactivating pinned alert subscription: %w", err)
			}
			logger.Info("deactivated operator-pinned audit alert subscription %s", sub.Id)
			sub.Status = "inactive"
		} else {
			logger.Debug("operator-pinned audit alert subscription already inactive")
		}
		return sub, nil
	}

	// Enabled path: validate URL.
	if cfg.URL == "" {
		return DBWebhookSubscription{}, fmt.Errorf("alerting.enabled is true but alerting.webhook_url is empty")
	}

	if denyListStore != nil {
		validator := NewWebhookUrlValidatorWithHTTP(denyListStore, false)
		if err := validator.ValidateWebhookURL(ctx, cfg.URL); err != nil {
			return DBWebhookSubscription{}, fmt.Errorf("alerting.webhook_url failed validation: %w", err)
		}
	}

	ownerUUID := uuid.MustParse(OperatorSystemUserUUID)
	now := time.Now().UTC()

	if len(existing) > 0 {
		// Update the existing pinned subscription in-place.
		sub := existing[0]
		sub.Url = cfg.URL
		sub.Secret = cfg.Secret
		sub.Status = "active"
		sub.Events = []string{EventSystemAuditAdminWrite}
		sub.ModifiedAt = now

		if err := store.Update(ctx, sub.Id.String(), sub); err != nil {
			return DBWebhookSubscription{}, fmt.Errorf("updating pinned alert subscription: %w", err)
		}
		logger.Info("updated operator-pinned audit alert subscription %s → %s", sub.Id, cfg.URL)
		return sub, nil
	}

	// Create a new pinned subscription.
	newSub := DBWebhookSubscription{
		OwnerId:        ownerUUID,
		Name:           "Operator Audit Alert Sink",
		Url:            cfg.URL,
		Events:         []string{EventSystemAuditAdminWrite},
		Secret:         cfg.Secret,
		Status:         "active",
		ChallengesSent: 0,
		CreatedAt:      now,
		ModifiedAt:     now,
		OperatorPinned: true,
	}

	idSetter := func(s DBWebhookSubscription, id string) DBWebhookSubscription {
		s.Id = uuid.MustParse(id)
		return s
	}

	created, err := store.Create(ctx, newSub, idSetter)
	if err != nil {
		return DBWebhookSubscription{}, fmt.Errorf("creating pinned alert subscription: %w", err)
	}
	logger.Info("created operator-pinned audit alert subscription %s → %s", created.Id, cfg.URL)
	return created, nil
}
