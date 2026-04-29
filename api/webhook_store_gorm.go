package api

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormWebhookSubscriptionStore implements WebhookSubscriptionStoreInterface using GORM
type GormWebhookSubscriptionStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

// NewGormWebhookSubscriptionStore creates a new GORM-backed webhook subscription store
func NewGormWebhookSubscriptionStore(db *gorm.DB) *GormWebhookSubscriptionStore {
	return &GormWebhookSubscriptionStore{db: db}
}

// Get retrieves a webhook subscription by ID using GORM
func (s *GormWebhookSubscriptionStore) Get(id string) (DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var sub models.WebhookSubscription
	if err := s.db.First(&sub, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DBWebhookSubscription{}, ErrWebhookNotFound
		}
		// Classify non-NotFound errors so transient ADB errors
		// (ORA-03113/ORA-08177/etc.) propagate as ErrTransient instead
		// of raw GORM errors. Matches GormWebhookQuotaStore.Get.
		return DBWebhookSubscription{}, dberrors.Classify(err)
	}

	return s.toDBModel(&sub), nil
}

// List retrieves webhook subscriptions with pagination and filtering using GORM
func (s *GormWebhookSubscriptionStore) List(offset, limit int, filter func(DBWebhookSubscription) bool) []DBWebhookSubscription {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var subs []models.WebhookSubscription
	query := s.db.Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Find(&subs).Error; err != nil {
		return []DBWebhookSubscription{}
	}

	var result []DBWebhookSubscription
	for _, sub := range subs {
		dbSub := s.toDBModel(&sub)
		if filter == nil || filter(dbSub) {
			result = append(result, dbSub)
		}
	}

	return result
}

// ListByOwner retrieves subscriptions for a specific owner using GORM
func (s *GormWebhookSubscriptionStore) ListByOwner(ownerID string, offset, limit int) ([]DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var subs []models.WebhookSubscription
	query := s.db.Where("owner_internal_uuid = ?", ownerID).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Find(&subs).Error; err != nil {
		return nil, dberrors.Classify(err)
	}

	result := make([]DBWebhookSubscription, 0, len(subs))
	for _, sub := range subs {
		result = append(result, s.toDBModel(&sub))
	}

	return result, nil
}

// ListByThreatModel retrieves subscriptions for a specific threat model using GORM
func (s *GormWebhookSubscriptionStore) ListByThreatModel(threatModelID string, offset, limit int) ([]DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var subs []models.WebhookSubscription
	query := s.db.Where("threat_model_id = ?", threatModelID).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Find(&subs).Error; err != nil {
		return nil, dberrors.Classify(err)
	}

	result := make([]DBWebhookSubscription, 0, len(subs))
	for _, sub := range subs {
		result = append(result, s.toDBModel(&sub))
	}

	return result, nil
}

// ListActiveByOwner retrieves active subscriptions for an owner using GORM
func (s *GormWebhookSubscriptionStore) ListActiveByOwner(ownerID string) ([]DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var subs []models.WebhookSubscription
	if err := s.db.Where("owner_internal_uuid = ? AND status = ?", ownerID, "active").
		Order("created_at DESC").Find(&subs).Error; err != nil {
		return nil, dberrors.Classify(err)
	}

	result := make([]DBWebhookSubscription, 0, len(subs))
	for _, sub := range subs {
		result = append(result, s.toDBModel(&sub))
	}

	return result, nil
}

// ListPendingVerification retrieves subscriptions pending verification using GORM
func (s *GormWebhookSubscriptionStore) ListPendingVerification() ([]DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var subs []models.WebhookSubscription
	// Use clause.OrderByColumn for cross-database compatibility (Oracle requires uppercase column names)
	if err := s.db.Where(map[string]any{"status": "pending_verification"}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{OrderByCol(s.db.Name(), "created_at", false)}}).
		Find(&subs).Error; err != nil {
		return nil, dberrors.Classify(err)
	}

	result := make([]DBWebhookSubscription, 0, len(subs))
	for _, sub := range subs {
		result = append(result, s.toDBModel(&sub))
	}

	return result, nil
}

// ListPendingDelete retrieves subscriptions pending deletion using GORM
func (s *GormWebhookSubscriptionStore) ListPendingDelete() ([]DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var subs []models.WebhookSubscription
	// Use clause expressions for cross-database compatibility (Oracle requires uppercase column names)
	if err := s.db.Where(map[string]any{"status": "pending_delete"}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{OrderByCol(s.db.Name(), "modified_at", false)}}).
		Find(&subs).Error; err != nil {
		return nil, dberrors.Classify(err)
	}

	result := make([]DBWebhookSubscription, 0, len(subs))
	for _, sub := range subs {
		result = append(result, s.toDBModel(&sub))
	}

	return result, nil
}

// ListIdle retrieves subscriptions that have been idle for a certain number of days using GORM
func (s *GormWebhookSubscriptionStore) ListIdle(daysIdle int) ([]DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	cutoff := time.Now().UTC().AddDate(0, 0, -daysIdle)

	var subs []models.WebhookSubscription
	// Use clause expressions for cross-database compatibility (Oracle requires uppercase column names)
	// Complex OR condition: (last_successful_use IS NOT NULL AND last_successful_use < cutoff) OR (last_successful_use IS NULL AND created_at < cutoff)
	if err := s.db.Where(map[string]any{"status": "active"}).
		Where(
			s.db.Where(clause.Expr{SQL: "? IS NOT NULL", Vars: []any{Col(s.db.Name(), "last_successful_use")}}).
				Where(clause.Expr{SQL: "? < ?", Vars: []any{Col(s.db.Name(), "last_successful_use"), cutoff}}).
				Or(
					s.db.Where(clause.Expr{SQL: "? IS NULL", Vars: []any{Col(s.db.Name(), "last_successful_use")}}).
						Where(clause.Expr{SQL: "? < ?", Vars: []any{Col(s.db.Name(), "created_at"), cutoff}}),
				),
		).Find(&subs).Error; err != nil {
		return nil, err
	}

	result := make([]DBWebhookSubscription, 0, len(subs))
	for _, sub := range subs {
		result = append(result, s.toDBModel(&sub))
	}

	return result, nil
}

// ListBroken retrieves subscriptions with too many failures using GORM
func (s *GormWebhookSubscriptionStore) ListBroken(minFailures int, daysSinceSuccess int) ([]DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	cutoff := time.Now().UTC().AddDate(0, 0, -daysSinceSuccess)

	var subs []models.WebhookSubscription
	// Use clause expressions for cross-database compatibility (Oracle requires uppercase column names)
	// Condition: status = active AND publication_failures >= minFailures AND (last_successful_use IS NULL OR last_successful_use < cutoff)
	if err := s.db.Where(map[string]any{"status": "active"}).
		Where(clause.Expr{SQL: "? >= ?", Vars: []any{Col(s.db.Name(), "publication_failures"), minFailures}}).
		Where(
			s.db.Where(clause.Expr{SQL: "? IS NULL", Vars: []any{Col(s.db.Name(), "last_successful_use")}}).
				Or(clause.Expr{SQL: "? < ?", Vars: []any{Col(s.db.Name(), "last_successful_use"), cutoff}}),
		).Find(&subs).Error; err != nil {
		return nil, err
	}

	result := make([]DBWebhookSubscription, 0, len(subs))
	for _, sub := range subs {
		result = append(result, s.toDBModel(&sub))
	}

	return result, nil
}

// Create creates a new webhook subscription using GORM
func (s *GormWebhookSubscriptionStore) Create(item DBWebhookSubscription, idSetter func(DBWebhookSubscription, string) DBWebhookSubscription) (DBWebhookSubscription, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Update timestamps
	updatedItem := UpdateTimestamps(&item, true)
	item = *updatedItem

	// Generate ID if not set
	if item.Id == uuid.Nil {
		item = idSetter(item, uuid.New().String())
	}

	// Convert to GORM model
	gormSub := s.toGormModel(&item)

	err := authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Create(&gormSub).Error; err != nil {
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return DBWebhookSubscription{}, err
	}

	return item, nil
}

// Update updates an existing webhook subscription using GORM
func (s *GormWebhookSubscriptionStore) Update(id string, item DBWebhookSubscription) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Update modified timestamp
	updatedItem := UpdateTimestamps(&item, false)
	item = *updatedItem

	// Convert to GORM model
	// Use struct-based Updates to ensure custom types (like StringArray for Events)
	// are properly serialized via their Value() method. Map-based Updates bypasses custom type handling.
	gormSub := s.toGormModel(&item)

	return authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Model(&models.WebhookSubscription{}).Where("id = ?", id).Updates(gormSub)
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrWebhookNotFound
		}
		return nil
	})
}

// UpdateStatus updates only the status field using GORM
func (s *GormWebhookSubscriptionStore) UpdateStatus(id string, status string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	return authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Model(&models.WebhookSubscription{}).Where("id = ?", id).Updates(map[string]any{
			"status": status,
		})
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrWebhookNotFound
		}
		return nil
	})
}

// UpdateChallenge updates challenge-related fields using GORM
func (s *GormWebhookSubscriptionStore) UpdateChallenge(id string, challenge string, challengesSent int) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	return authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Model(&models.WebhookSubscription{}).Where("id = ?", id).Updates(map[string]any{
			"challenge":       challenge,
			"challenges_sent": challengesSent,
		})
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrWebhookNotFound
		}
		return nil
	})
}

// UpdatePublicationStats updates publication statistics using GORM
func (s *GormWebhookSubscriptionStore) UpdatePublicationStats(id string, success bool) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	now := time.Now().UTC()

	return authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		var result *gorm.DB
		if success {
			result = tx.Model(&models.WebhookSubscription{}).Where("id = ?", id).Updates(map[string]any{
				"last_successful_use":  now,
				"publication_failures": 0,
			})
		} else {
			result = tx.Model(&models.WebhookSubscription{}).Where("id = ?", id).Updates(map[string]any{
				"publication_failures": gorm.Expr("publication_failures + 1"),
			})
		}
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrWebhookNotFound
		}
		return nil
	})
}

// IncrementTimeouts increments the timeout count using GORM
func (s *GormWebhookSubscriptionStore) IncrementTimeouts(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	return authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Model(&models.WebhookSubscription{}).Where("id = ?", id).Updates(map[string]any{
			"timeout_count": gorm.Expr("timeout_count + 1"),
		})
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrWebhookNotFound
		}
		return nil
	})
}

// ResetTimeouts resets the timeout count using GORM
func (s *GormWebhookSubscriptionStore) ResetTimeouts(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	return authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Model(&models.WebhookSubscription{}).Where("id = ?", id).Updates(map[string]any{
			"timeout_count": 0,
		})
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrWebhookNotFound
		}
		return nil
	})
}

// Delete deletes a webhook subscription using GORM
func (s *GormWebhookSubscriptionStore) Delete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Delete(&models.WebhookSubscription{}, "id = ?", id)
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrWebhookNotFound
		}
		return nil
	})
}

// Count returns the total number of webhook subscriptions using GORM
func (s *GormWebhookSubscriptionStore) Count() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var count int64
	s.db.Model(&models.WebhookSubscription{}).Count(&count)
	return int(count)
}

// CountByOwner returns the number of subscriptions for a specific owner using GORM
func (s *GormWebhookSubscriptionStore) CountByOwner(ownerID string) (int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var count int64
	if err := s.db.Model(&models.WebhookSubscription{}).Where("owner_internal_uuid = ?", ownerID).Count(&count).Error; err != nil {
		return 0, dberrors.Classify(err)
	}
	return int(count), nil
}

// toDBModel converts a GORM model to DBWebhookSubscription
func (s *GormWebhookSubscriptionStore) toDBModel(sub *models.WebhookSubscription) DBWebhookSubscription {
	dbSub := DBWebhookSubscription{
		Id:                  uuid.MustParse(sub.ID),
		OwnerId:             uuid.MustParse(sub.OwnerInternalUUID),
		Name:                sub.Name,
		Url:                 sub.URL,
		Events:              []string(sub.Events),
		Status:              sub.Status,
		ChallengesSent:      sub.ChallengesSent,
		CreatedAt:           sub.CreatedAt,
		ModifiedAt:          sub.ModifiedAt,
		PublicationFailures: sub.PublicationFailures,
		TimeoutCount:        sub.TimeoutCount,
	}

	if sub.ThreatModelID != nil && *sub.ThreatModelID != "" {
		tmID := uuid.MustParse(*sub.ThreatModelID)
		dbSub.ThreatModelId = &tmID
	}
	if sub.Secret != nil {
		dbSub.Secret = *sub.Secret
	}
	if sub.Challenge != nil {
		dbSub.Challenge = *sub.Challenge
	}
	if sub.LastSuccessfulUse != nil {
		dbSub.LastSuccessfulUse = sub.LastSuccessfulUse
	}

	return dbSub
}

// toGormModel converts a DBWebhookSubscription to GORM model
func (s *GormWebhookSubscriptionStore) toGormModel(sub *DBWebhookSubscription) *models.WebhookSubscription {
	gormSub := &models.WebhookSubscription{
		ID:                  sub.Id.String(),
		OwnerInternalUUID:   sub.OwnerId.String(),
		Name:                sub.Name,
		URL:                 sub.Url,
		Events:              models.StringArray(sub.Events),
		Status:              sub.Status,
		ChallengesSent:      sub.ChallengesSent,
		CreatedAt:           sub.CreatedAt,
		ModifiedAt:          sub.ModifiedAt,
		PublicationFailures: sub.PublicationFailures,
		TimeoutCount:        sub.TimeoutCount,
	}

	if sub.ThreatModelId != nil {
		tmID := sub.ThreatModelId.String()
		gormSub.ThreatModelID = &tmID
	}
	if sub.Secret != "" {
		gormSub.Secret = &sub.Secret
	}
	if sub.Challenge != "" {
		gormSub.Challenge = &sub.Challenge
	}
	if sub.LastSuccessfulUse != nil {
		gormSub.LastSuccessfulUse = sub.LastSuccessfulUse
	}

	return gormSub
}

// GormWebhookQuotaStore implements WebhookQuotaStoreInterface using GORM
type GormWebhookQuotaStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

// NewGormWebhookQuotaStore creates a new GORM-backed webhook quota store
func NewGormWebhookQuotaStore(db *gorm.DB) *GormWebhookQuotaStore {
	return &GormWebhookQuotaStore{db: db}
}

// Get retrieves a webhook quota by owner ID using GORM
func (s *GormWebhookQuotaStore) Get(ownerID string) (DBWebhookQuota, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var quota models.WebhookQuota
	if err := s.db.First(&quota, "owner_id = ?", ownerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DBWebhookQuota{}, ErrWebhookQuotaNotFound
		}
		return DBWebhookQuota{}, dberrors.Classify(err)
	}

	return s.toDBModel(&quota), nil
}

// GetOrDefault retrieves a quota or returns default values using GORM.
//
// Semantics: fail-open. Any error from Get falls back to the per-tenant
// defaults so request-handling never panics for missing quota rows.
// Non-NotFound errors (i.e., transient ADB outages such as ORA-03113 /
// ORA-08177) emit a WARN log so an outage is visible in observability
// even though the response succeeds with default limits. The historical
// behavior was to silently swallow all errors; the WARN is the only
// behavior change.
func (s *GormWebhookQuotaStore) GetOrDefault(ownerID string) DBWebhookQuota {
	quota, err := s.Get(ownerID)
	if err != nil {
		if !errors.Is(err, ErrWebhookQuotaNotFound) {
			slogging.Get().Warn("webhook quota lookup failed for owner=%s, falling back to defaults: %v", ownerID, err)
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
	return quota
}

// List retrieves all webhook quotas with pagination using GORM
func (s *GormWebhookQuotaStore) List(offset, limit int) ([]DBWebhookQuota, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var quotas []models.WebhookQuota
	query := s.db.Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Find(&quotas).Error; err != nil {
		return nil, dberrors.Classify(err)
	}

	result := make([]DBWebhookQuota, 0, len(quotas))
	for _, quota := range quotas {
		result = append(result, s.toDBModel(&quota))
	}

	return result, nil
}

// Create creates a new webhook quota using GORM
func (s *GormWebhookQuotaStore) Create(item DBWebhookQuota) (DBWebhookQuota, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Update timestamps
	updatedItem := UpdateTimestamps(&item, true)
	item = *updatedItem

	// Convert to GORM model
	gormQuota := s.toGormModel(&item)

	err := authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Create(&gormQuota).Error; err != nil {
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return DBWebhookQuota{}, err
	}

	return item, nil
}

// Update updates an existing webhook quota using GORM
func (s *GormWebhookQuotaStore) Update(ownerID string, item DBWebhookQuota) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag

	return authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Model(&models.WebhookQuota{}).Where("owner_id = ?", ownerID).Updates(map[string]any{
			"max_subscriptions":                    item.MaxSubscriptions,
			"max_events_per_minute":                item.MaxEventsPerMinute,
			"max_subscription_requests_per_minute": item.MaxSubscriptionRequestsPerMinute,
			"max_subscription_requests_per_day":    item.MaxSubscriptionRequestsPerDay,
		})
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrWebhookQuotaNotFound
		}
		return nil
	})
}

// Delete deletes a webhook quota using GORM
func (s *GormWebhookQuotaStore) Delete(ownerID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Delete(&models.WebhookQuota{}, "owner_id = ?", ownerID)
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrWebhookQuotaNotFound
		}
		return nil
	})
}

// Count returns the total number of webhook quotas using GORM
func (s *GormWebhookQuotaStore) Count() (int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var count int64
	if err := s.db.Model(&models.WebhookQuota{}).Count(&count).Error; err != nil {
		return 0, dberrors.Classify(err)
	}
	return int(count), nil
}

// toDBModel converts a GORM model to DBWebhookQuota
func (s *GormWebhookQuotaStore) toDBModel(quota *models.WebhookQuota) DBWebhookQuota {
	return DBWebhookQuota{
		OwnerId:                          uuid.MustParse(quota.OwnerID),
		MaxSubscriptions:                 quota.MaxSubscriptions,
		MaxEventsPerMinute:               quota.MaxEventsPerMinute,
		MaxSubscriptionRequestsPerMinute: quota.MaxSubscriptionRequestsPerMinute,
		MaxSubscriptionRequestsPerDay:    quota.MaxSubscriptionRequestsPerDay,
		CreatedAt:                        quota.CreatedAt,
		ModifiedAt:                       quota.ModifiedAt,
	}
}

// toGormModel converts a DBWebhookQuota to GORM model
func (s *GormWebhookQuotaStore) toGormModel(quota *DBWebhookQuota) *models.WebhookQuota {
	return &models.WebhookQuota{
		OwnerID:                          quota.OwnerId.String(),
		MaxSubscriptions:                 quota.MaxSubscriptions,
		MaxEventsPerMinute:               quota.MaxEventsPerMinute,
		MaxSubscriptionRequestsPerMinute: quota.MaxSubscriptionRequestsPerMinute,
		MaxSubscriptionRequestsPerDay:    quota.MaxSubscriptionRequestsPerDay,
		CreatedAt:                        quota.CreatedAt,
		ModifiedAt:                       quota.ModifiedAt,
	}
}

// GormWebhookUrlDenyListStore implements WebhookUrlDenyListStoreInterface using GORM
type GormWebhookUrlDenyListStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

// NewGormWebhookUrlDenyListStore creates a new GORM-backed store
func NewGormWebhookUrlDenyListStore(db *gorm.DB) *GormWebhookUrlDenyListStore {
	return &GormWebhookUrlDenyListStore{db: db}
}

// List retrieves all deny list entries using GORM
func (s *GormWebhookUrlDenyListStore) List() ([]WebhookUrlDenyListEntry, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var entries []models.WebhookURLDenyList
	if err := s.db.Order("pattern_type, pattern").Find(&entries).Error; err != nil {
		return nil, dberrors.Classify(err)
	}

	result := make([]WebhookUrlDenyListEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, s.toDBModel(&entry))
	}

	return result, nil
}

// Create creates a new deny list entry using GORM
func (s *GormWebhookUrlDenyListStore) Create(item WebhookUrlDenyListEntry) (WebhookUrlDenyListEntry, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Set created_at if not set
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}

	// Generate ID if not set
	if item.Id == uuid.Nil {
		item.Id = uuid.New()
	}

	// Convert to GORM model
	gormEntry := s.toGormModel(&item)

	err := authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Create(&gormEntry).Error; err != nil {
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return WebhookUrlDenyListEntry{}, err
	}

	return item, nil
}

// Delete deletes a deny list entry using GORM
func (s *GormWebhookUrlDenyListStore) Delete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Delete(&models.WebhookURLDenyList{}, "id = ?", id)
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrDenyListEntryNotFound
		}
		return nil
	})
}

// toDBModel converts a GORM model to WebhookUrlDenyListEntry
func (s *GormWebhookUrlDenyListStore) toDBModel(entry *models.WebhookURLDenyList) WebhookUrlDenyListEntry {
	result := WebhookUrlDenyListEntry{
		Id:          uuid.MustParse(entry.ID),
		Pattern:     entry.Pattern,
		PatternType: entry.PatternType,
		CreatedAt:   entry.CreatedAt,
	}
	if entry.Description != nil {
		result.Description = *entry.Description
	}
	return result
}

// toGormModel converts a WebhookUrlDenyListEntry to GORM model
func (s *GormWebhookUrlDenyListStore) toGormModel(entry *WebhookUrlDenyListEntry) *models.WebhookURLDenyList {
	gormEntry := &models.WebhookURLDenyList{
		ID:          entry.Id.String(),
		Pattern:     entry.Pattern,
		PatternType: entry.PatternType,
		CreatedAt:   entry.CreatedAt,
	}
	if entry.Description != "" {
		gormEntry.Description = &entry.Description
	}
	return gormEntry
}
