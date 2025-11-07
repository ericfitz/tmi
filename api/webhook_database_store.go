package api

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// DBWebhookSubscriptionDatabaseStore implements WebhookSubscriptionStoreInterface
type DBWebhookSubscriptionDatabaseStore struct {
	db    *sql.DB
	mutex sync.RWMutex
}

// NewDBWebhookSubscriptionDatabaseStore creates a new database-backed store
func NewDBWebhookSubscriptionDatabaseStore(db *sql.DB) *DBWebhookSubscriptionDatabaseStore {
	return &DBWebhookSubscriptionDatabaseStore{db: db}
}

// Get retrieves a webhook subscription by ID
func (s *DBWebhookSubscriptionDatabaseStore) Get(id string) (DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var sub DBWebhookSubscription
	var threatModelId sql.NullString
	var secret sql.NullString
	var challenge sql.NullString
	var lastSuccessfulUse sql.NullTime

	query := `
		SELECT id, owner_id, threat_model_id, name, url, events, secret, status,
		       challenge, challenges_sent, created_at, modified_at,
		       last_successful_use, publication_failures
		FROM webhook_subscriptions
		WHERE id = $1
	`

	err := s.db.QueryRow(query, id).Scan(
		&sub.Id, &sub.OwnerId, &threatModelId, &sub.Name, &sub.Url,
		pq.Array(&sub.Events), &secret, &sub.Status, &challenge,
		&sub.ChallengesSent, &sub.CreatedAt, &sub.ModifiedAt,
		&lastSuccessfulUse, &sub.PublicationFailures,
	)

	if err == sql.ErrNoRows {
		return DBWebhookSubscription{}, fmt.Errorf("webhook subscription not found")
	}
	if err != nil {
		return DBWebhookSubscription{}, err
	}

	// Handle nullable fields
	if threatModelId.Valid {
		tmId := uuid.MustParse(threatModelId.String)
		sub.ThreatModelId = &tmId
	}
	if secret.Valid {
		sub.Secret = secret.String
	}
	if challenge.Valid {
		sub.Challenge = challenge.String
	}
	if lastSuccessfulUse.Valid {
		sub.LastSuccessfulUse = &lastSuccessfulUse.Time
	}

	return sub, nil
}

// List retrieves webhook subscriptions with pagination and filtering
func (s *DBWebhookSubscriptionDatabaseStore) List(offset, limit int, filter func(DBWebhookSubscription) bool) []DBWebhookSubscription {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	query := `
		SELECT id, owner_id, threat_model_id, name, url, events, secret, status,
		       challenge, challenges_sent, created_at, modified_at,
		       last_successful_use, publication_failures
		FROM webhook_subscriptions
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := s.db.Query(query, limit, offset)
	if err != nil {
		return []DBWebhookSubscription{}
	}
	defer func() { _ = rows.Close() }()

	return s.scanSubscriptions(rows, filter)
}

// ListByOwner retrieves subscriptions for a specific owner
func (s *DBWebhookSubscriptionDatabaseStore) ListByOwner(ownerID string, offset, limit int) ([]DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	query := `
		SELECT id, owner_id, threat_model_id, name, url, events, secret, status,
		       challenge, challenges_sent, created_at, modified_at,
		       last_successful_use, publication_failures
		FROM webhook_subscriptions
		WHERE owner_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.Query(query, ownerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanSubscriptions(rows, nil), nil
}

// ListByThreatModel retrieves subscriptions for a specific threat model
func (s *DBWebhookSubscriptionDatabaseStore) ListByThreatModel(threatModelID string, offset, limit int) ([]DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	query := `
		SELECT id, owner_id, threat_model_id, name, url, events, secret, status,
		       challenge, challenges_sent, created_at, modified_at,
		       last_successful_use, publication_failures
		FROM webhook_subscriptions
		WHERE threat_model_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.Query(query, threatModelID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanSubscriptions(rows, nil), nil
}

// ListActiveByOwner retrieves active subscriptions for an owner
func (s *DBWebhookSubscriptionDatabaseStore) ListActiveByOwner(ownerID string) ([]DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	query := `
		SELECT id, owner_id, threat_model_id, name, url, events, secret, status,
		       challenge, challenges_sent, created_at, modified_at,
		       last_successful_use, publication_failures
		FROM webhook_subscriptions
		WHERE owner_id = $1 AND status = 'active'
		ORDER BY created_at DESC
	`

	rows, err := s.db.Query(query, ownerID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanSubscriptions(rows, nil), nil
}

// ListPendingVerification retrieves subscriptions pending verification
func (s *DBWebhookSubscriptionDatabaseStore) ListPendingVerification() ([]DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	query := `
		SELECT id, owner_id, threat_model_id, name, url, events, secret, status,
		       challenge, challenges_sent, created_at, modified_at,
		       last_successful_use, publication_failures
		FROM webhook_subscriptions
		WHERE status = 'pending_verification'
		ORDER BY created_at ASC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanSubscriptions(rows, nil), nil
}

// ListPendingDelete retrieves subscriptions pending deletion
func (s *DBWebhookSubscriptionDatabaseStore) ListPendingDelete() ([]DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	query := `
		SELECT id, owner_id, threat_model_id, name, url, events, secret, status,
		       challenge, challenges_sent, created_at, modified_at,
		       last_successful_use, publication_failures
		FROM webhook_subscriptions
		WHERE status = 'pending_delete'
		ORDER BY modified_at ASC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanSubscriptions(rows, nil), nil
}

// ListIdle retrieves subscriptions that have been idle for a certain number of days
func (s *DBWebhookSubscriptionDatabaseStore) ListIdle(daysIdle int) ([]DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	query := `
		SELECT id, owner_id, threat_model_id, name, url, events, secret, status,
		       challenge, challenges_sent, created_at, modified_at,
		       last_successful_use, publication_failures
		FROM webhook_subscriptions
		WHERE status = 'active'
		  AND (
		    (last_successful_use IS NOT NULL AND last_successful_use < NOW() - INTERVAL '1 day' * $1)
		    OR (last_successful_use IS NULL AND created_at < NOW() - INTERVAL '1 day' * $1)
		  )
	`

	rows, err := s.db.Query(query, daysIdle)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanSubscriptions(rows, nil), nil
}

// ListBroken retrieves subscriptions with too many failures
func (s *DBWebhookSubscriptionDatabaseStore) ListBroken(minFailures int, daysSinceSuccess int) ([]DBWebhookSubscription, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	query := `
		SELECT id, owner_id, threat_model_id, name, url, events, secret, status,
		       challenge, challenges_sent, created_at, modified_at,
		       last_successful_use, publication_failures
		FROM webhook_subscriptions
		WHERE status = 'active'
		  AND publication_failures >= $1
		  AND (last_successful_use IS NULL OR last_successful_use < NOW() - INTERVAL '1 day' * $2)
	`

	rows, err := s.db.Query(query, minFailures, daysSinceSuccess)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanSubscriptions(rows, nil), nil
}

// Create creates a new webhook subscription
func (s *DBWebhookSubscriptionDatabaseStore) Create(item DBWebhookSubscription, idSetter func(DBWebhookSubscription, string) DBWebhookSubscription) (DBWebhookSubscription, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Update timestamps
	updatedItem := UpdateTimestamps(&item, true)
	item = *updatedItem

	// Generate ID if not set
	if item.Id == uuid.Nil {
		item = idSetter(item, uuid.New().String())
	}

	query := `
		INSERT INTO webhook_subscriptions (
			id, owner_id, threat_model_id, name, url, events, secret, status,
			challenge, challenges_sent, created_at, modified_at,
			last_successful_use, publication_failures
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`

	var threatModelId interface{}
	if item.ThreatModelId != nil {
		threatModelId = *item.ThreatModelId
	}

	var secret interface{}
	if item.Secret != "" {
		secret = item.Secret
	}

	var challenge interface{}
	if item.Challenge != "" {
		challenge = item.Challenge
	}

	var lastSuccessfulUse interface{}
	if item.LastSuccessfulUse != nil {
		lastSuccessfulUse = *item.LastSuccessfulUse
	}

	_, err := s.db.Exec(query,
		item.Id, item.OwnerId, threatModelId, item.Name, item.Url,
		pq.Array(item.Events), secret, item.Status, challenge,
		item.ChallengesSent, item.CreatedAt, item.ModifiedAt,
		lastSuccessfulUse, item.PublicationFailures,
	)

	if err != nil {
		return DBWebhookSubscription{}, err
	}

	return item, nil
}

// Update updates an existing webhook subscription
func (s *DBWebhookSubscriptionDatabaseStore) Update(id string, item DBWebhookSubscription) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Update modified timestamp
	updatedItem := UpdateTimestamps(&item, false)
	item = *updatedItem

	query := `
		UPDATE webhook_subscriptions
		SET owner_id = $1, threat_model_id = $2, name = $3, url = $4, events = $5,
		    secret = $6, status = $7, challenge = $8, challenges_sent = $9,
		    modified_at = $10, last_successful_use = $11, publication_failures = $12
		WHERE id = $13
	`

	var threatModelId interface{}
	if item.ThreatModelId != nil {
		threatModelId = *item.ThreatModelId
	}

	var secret interface{}
	if item.Secret != "" {
		secret = item.Secret
	}

	var challenge interface{}
	if item.Challenge != "" {
		challenge = item.Challenge
	}

	var lastSuccessfulUse interface{}
	if item.LastSuccessfulUse != nil {
		lastSuccessfulUse = *item.LastSuccessfulUse
	}

	result, err := s.db.Exec(query,
		item.OwnerId, threatModelId, item.Name, item.Url,
		pq.Array(item.Events), secret, item.Status, challenge,
		item.ChallengesSent, item.ModifiedAt, lastSuccessfulUse,
		item.PublicationFailures, id,
	)

	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("webhook subscription not found")
	}

	return nil
}

// UpdateStatus updates only the status field
func (s *DBWebhookSubscriptionDatabaseStore) UpdateStatus(id string, status string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	query := `
		UPDATE webhook_subscriptions
		SET status = $1, modified_at = $2
		WHERE id = $3
	`

	result, err := s.db.Exec(query, status, time.Now().UTC(), id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("webhook subscription not found")
	}

	return nil
}

// UpdateChallenge updates challenge-related fields
func (s *DBWebhookSubscriptionDatabaseStore) UpdateChallenge(id string, challenge string, challengesSent int) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	query := `
		UPDATE webhook_subscriptions
		SET challenge = $1, challenges_sent = $2, modified_at = $3
		WHERE id = $4
	`

	result, err := s.db.Exec(query, challenge, challengesSent, time.Now().UTC(), id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("webhook subscription not found")
	}

	return nil
}

// UpdatePublicationStats updates publication statistics
func (s *DBWebhookSubscriptionDatabaseStore) UpdatePublicationStats(id string, success bool) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var query string
	if success {
		query = `
			UPDATE webhook_subscriptions
			SET last_successful_use = $1, publication_failures = 0, modified_at = $1
			WHERE id = $2
		`
	} else {
		query = `
			UPDATE webhook_subscriptions
			SET publication_failures = publication_failures + 1, modified_at = $1
			WHERE id = $2
		`
	}

	result, err := s.db.Exec(query, time.Now().UTC(), id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("webhook subscription not found")
	}

	return nil
}

// Delete deletes a webhook subscription
func (s *DBWebhookSubscriptionDatabaseStore) Delete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	query := `DELETE FROM webhook_subscriptions WHERE id = $1`

	result, err := s.db.Exec(query, id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("webhook subscription not found")
	}

	return nil
}

// Count returns the total number of webhook subscriptions
func (s *DBWebhookSubscriptionDatabaseStore) Count() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var count int
	query := `SELECT COUNT(*) FROM webhook_subscriptions`
	err := s.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// CountByOwner returns the number of subscriptions for a specific owner
func (s *DBWebhookSubscriptionDatabaseStore) CountByOwner(ownerID string) (int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var count int
	query := `SELECT COUNT(*) FROM webhook_subscriptions WHERE owner_id = $1`
	err := s.db.QueryRow(query, ownerID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// scanSubscriptions is a helper to scan rows into DBWebhookSubscription structs
func (s *DBWebhookSubscriptionDatabaseStore) scanSubscriptions(rows *sql.Rows, filter func(DBWebhookSubscription) bool) []DBWebhookSubscription {
	var subscriptions []DBWebhookSubscription

	for rows.Next() {
		var sub DBWebhookSubscription
		var threatModelId sql.NullString
		var secret sql.NullString
		var challenge sql.NullString
		var lastSuccessfulUse sql.NullTime

		err := rows.Scan(
			&sub.Id, &sub.OwnerId, &threatModelId, &sub.Name, &sub.Url,
			pq.Array(&sub.Events), &secret, &sub.Status, &challenge,
			&sub.ChallengesSent, &sub.CreatedAt, &sub.ModifiedAt,
			&lastSuccessfulUse, &sub.PublicationFailures,
		)

		if err != nil {
			continue
		}

		// Handle nullable fields
		if threatModelId.Valid {
			tmId := uuid.MustParse(threatModelId.String)
			sub.ThreatModelId = &tmId
		}
		if secret.Valid {
			sub.Secret = secret.String
		}
		if challenge.Valid {
			sub.Challenge = challenge.String
		}
		if lastSuccessfulUse.Valid {
			sub.LastSuccessfulUse = &lastSuccessfulUse.Time
		}

		// Apply filter if provided
		if filter == nil || filter(sub) {
			subscriptions = append(subscriptions, sub)
		}
	}

	return subscriptions
}

// DBWebhookDeliveryDatabaseStore implements WebhookDeliveryStoreInterface
type DBWebhookDeliveryDatabaseStore struct {
	db    *sql.DB
	mutex sync.RWMutex
}

// NewDBWebhookDeliveryDatabaseStore creates a new database-backed store
func NewDBWebhookDeliveryDatabaseStore(db *sql.DB) *DBWebhookDeliveryDatabaseStore {
	return &DBWebhookDeliveryDatabaseStore{db: db}
}

// Get retrieves a webhook delivery by ID
func (s *DBWebhookDeliveryDatabaseStore) Get(id string) (DBWebhookDelivery, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var delivery DBWebhookDelivery
	var nextRetryAt sql.NullTime
	var lastError sql.NullString
	var deliveredAt sql.NullTime

	query := `
		SELECT id, subscription_id, event_type, payload, status, attempts,
		       next_retry_at, last_error, created_at, delivered_at
		FROM webhook_deliveries
		WHERE id = $1
	`

	err := s.db.QueryRow(query, id).Scan(
		&delivery.Id, &delivery.SubscriptionId, &delivery.EventType,
		&delivery.Payload, &delivery.Status, &delivery.Attempts,
		&nextRetryAt, &lastError, &delivery.CreatedAt, &deliveredAt,
	)

	if err == sql.ErrNoRows {
		return DBWebhookDelivery{}, fmt.Errorf("webhook delivery not found")
	}
	if err != nil {
		return DBWebhookDelivery{}, err
	}

	// Handle nullable fields
	if nextRetryAt.Valid {
		delivery.NextRetryAt = &nextRetryAt.Time
	}
	if lastError.Valid {
		delivery.LastError = lastError.String
	}
	if deliveredAt.Valid {
		delivery.DeliveredAt = &deliveredAt.Time
	}

	return delivery, nil
}

// List retrieves webhook deliveries with pagination and filtering
func (s *DBWebhookDeliveryDatabaseStore) List(offset, limit int, filter func(DBWebhookDelivery) bool) []DBWebhookDelivery {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	query := `
		SELECT id, subscription_id, event_type, payload, status, attempts,
		       next_retry_at, last_error, created_at, delivered_at
		FROM webhook_deliveries
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := s.db.Query(query, limit, offset)
	if err != nil {
		return []DBWebhookDelivery{}
	}
	defer func() { _ = rows.Close() }()

	return s.scanDeliveries(rows, filter)
}

// ListBySubscription retrieves deliveries for a specific subscription
func (s *DBWebhookDeliveryDatabaseStore) ListBySubscription(subscriptionID string, offset, limit int) ([]DBWebhookDelivery, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	query := `
		SELECT id, subscription_id, event_type, payload, status, attempts,
		       next_retry_at, last_error, created_at, delivered_at
		FROM webhook_deliveries
		WHERE subscription_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.Query(query, subscriptionID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanDeliveries(rows, nil), nil
}

// ListPending retrieves pending deliveries
func (s *DBWebhookDeliveryDatabaseStore) ListPending(limit int) ([]DBWebhookDelivery, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	query := `
		SELECT id, subscription_id, event_type, payload, status, attempts,
		       next_retry_at, last_error, created_at, delivered_at
		FROM webhook_deliveries
		WHERE status = 'pending'
		ORDER BY created_at ASC
		LIMIT $1
	`

	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanDeliveries(rows, nil), nil
}

// ListReadyForRetry retrieves deliveries ready for retry
func (s *DBWebhookDeliveryDatabaseStore) ListReadyForRetry() ([]DBWebhookDelivery, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	query := `
		SELECT id, subscription_id, event_type, payload, status, attempts,
		       next_retry_at, last_error, created_at, delivered_at
		FROM webhook_deliveries
		WHERE status = 'pending' AND next_retry_at IS NOT NULL AND next_retry_at <= NOW()
		ORDER BY next_retry_at ASC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanDeliveries(rows, nil), nil
}

// Create creates a new webhook delivery
func (s *DBWebhookDeliveryDatabaseStore) Create(item DBWebhookDelivery) (DBWebhookDelivery, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Set created_at if not set
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}

	// Generate UUIDv7 if not set
	if item.Id == uuid.Nil {
		// Note: UUIDv7 generation would require a library or custom implementation
		// For now, use standard UUID v4
		item.Id = uuid.New()
	}

	query := `
		INSERT INTO webhook_deliveries (
			id, subscription_id, event_type, payload, status, attempts,
			next_retry_at, last_error, created_at, delivered_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	var nextRetryAt interface{}
	if item.NextRetryAt != nil {
		nextRetryAt = *item.NextRetryAt
	}

	var lastError interface{}
	if item.LastError != "" {
		lastError = item.LastError
	}

	var deliveredAt interface{}
	if item.DeliveredAt != nil {
		deliveredAt = *item.DeliveredAt
	}

	_, err := s.db.Exec(query,
		item.Id, item.SubscriptionId, item.EventType, item.Payload,
		item.Status, item.Attempts, nextRetryAt, lastError,
		item.CreatedAt, deliveredAt,
	)

	if err != nil {
		return DBWebhookDelivery{}, err
	}

	return item, nil
}

// Update updates an existing webhook delivery
func (s *DBWebhookDeliveryDatabaseStore) Update(id string, item DBWebhookDelivery) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	query := `
		UPDATE webhook_deliveries
		SET subscription_id = $1, event_type = $2, payload = $3, status = $4,
		    attempts = $5, next_retry_at = $6, last_error = $7, delivered_at = $8
		WHERE id = $9
	`

	var nextRetryAt interface{}
	if item.NextRetryAt != nil {
		nextRetryAt = *item.NextRetryAt
	}

	var lastError interface{}
	if item.LastError != "" {
		lastError = item.LastError
	}

	var deliveredAt interface{}
	if item.DeliveredAt != nil {
		deliveredAt = *item.DeliveredAt
	}

	result, err := s.db.Exec(query,
		item.SubscriptionId, item.EventType, item.Payload, item.Status,
		item.Attempts, nextRetryAt, lastError, deliveredAt, id,
	)

	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("webhook delivery not found")
	}

	return nil
}

// UpdateStatus updates only the status and delivered_at fields
func (s *DBWebhookDeliveryDatabaseStore) UpdateStatus(id string, status string, deliveredAt *time.Time) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	query := `
		UPDATE webhook_deliveries
		SET status = $1, delivered_at = $2
		WHERE id = $3
	`

	var deliveredAtVal interface{}
	if deliveredAt != nil {
		deliveredAtVal = *deliveredAt
	}

	result, err := s.db.Exec(query, status, deliveredAtVal, id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("webhook delivery not found")
	}

	return nil
}

// UpdateRetry updates retry-related fields
func (s *DBWebhookDeliveryDatabaseStore) UpdateRetry(id string, attempts int, nextRetryAt *time.Time, lastError string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	query := `
		UPDATE webhook_deliveries
		SET attempts = $1, next_retry_at = $2, last_error = $3
		WHERE id = $4
	`

	var nextRetryAtVal interface{}
	if nextRetryAt != nil {
		nextRetryAtVal = *nextRetryAt
	}

	result, err := s.db.Exec(query, attempts, nextRetryAtVal, lastError, id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("webhook delivery not found")
	}

	return nil
}

// Delete deletes a webhook delivery
func (s *DBWebhookDeliveryDatabaseStore) Delete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	query := `DELETE FROM webhook_deliveries WHERE id = $1`

	result, err := s.db.Exec(query, id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("webhook delivery not found")
	}

	return nil
}

// DeleteOld deletes deliveries older than a certain number of days
func (s *DBWebhookDeliveryDatabaseStore) DeleteOld(daysOld int) (int, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	query := `
		DELETE FROM webhook_deliveries
		WHERE status IN ('delivered', 'failed')
		  AND created_at < NOW() - INTERVAL '1 day' * $1
	`

	result, err := s.db.Exec(query, daysOld)
	if err != nil {
		return 0, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return int(rows), nil
}

// Count returns the total number of webhook deliveries
func (s *DBWebhookDeliveryDatabaseStore) Count() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var count int
	query := `SELECT COUNT(*) FROM webhook_deliveries`
	err := s.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// scanDeliveries is a helper to scan rows into DBWebhookDelivery structs
func (s *DBWebhookDeliveryDatabaseStore) scanDeliveries(rows *sql.Rows, filter func(DBWebhookDelivery) bool) []DBWebhookDelivery {
	var deliveries []DBWebhookDelivery

	for rows.Next() {
		var delivery DBWebhookDelivery
		var nextRetryAt sql.NullTime
		var lastError sql.NullString
		var deliveredAt sql.NullTime

		err := rows.Scan(
			&delivery.Id, &delivery.SubscriptionId, &delivery.EventType,
			&delivery.Payload, &delivery.Status, &delivery.Attempts,
			&nextRetryAt, &lastError, &delivery.CreatedAt, &deliveredAt,
		)

		if err != nil {
			continue
		}

		// Handle nullable fields
		if nextRetryAt.Valid {
			delivery.NextRetryAt = &nextRetryAt.Time
		}
		if lastError.Valid {
			delivery.LastError = lastError.String
		}
		if deliveredAt.Valid {
			delivery.DeliveredAt = &deliveredAt.Time
		}

		// Apply filter if provided
		if filter == nil || filter(delivery) {
			deliveries = append(deliveries, delivery)
		}
	}

	return deliveries
}

// WebhookQuotaDatabaseStore implements WebhookQuotaStoreInterface
type WebhookQuotaDatabaseStore struct {
	db    *sql.DB
	mutex sync.RWMutex
}

// NewWebhookQuotaDatabaseStore creates a new database-backed store
func NewWebhookQuotaDatabaseStore(db *sql.DB) *WebhookQuotaDatabaseStore {
	return &WebhookQuotaDatabaseStore{db: db}
}

// Get retrieves a webhook quota by owner ID
func (s *WebhookQuotaDatabaseStore) Get(ownerID string) (WebhookQuota, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var quota WebhookQuota

	query := `
		SELECT owner_id, max_subscriptions, max_events_per_minute,
		       max_subscription_requests_per_minute, max_subscription_requests_per_day,
		       created_at, modified_at
		FROM webhook_quotas
		WHERE owner_id = $1
	`

	err := s.db.QueryRow(query, ownerID).Scan(
		&quota.OwnerId, &quota.MaxSubscriptions, &quota.MaxEventsPerMinute,
		&quota.MaxSubscriptionRequestsPerMinute, &quota.MaxSubscriptionRequestsPerDay,
		&quota.CreatedAt, &quota.ModifiedAt,
	)

	if err == sql.ErrNoRows {
		return WebhookQuota{}, fmt.Errorf("webhook quota not found")
	}
	if err != nil {
		return WebhookQuota{}, err
	}

	return quota, nil
}

// GetOrDefault retrieves a quota or returns default values
func (s *WebhookQuotaDatabaseStore) GetOrDefault(ownerID string) WebhookQuota {
	quota, err := s.Get(ownerID)
	if err != nil {
		// Return default quota
		ownerUUID, _ := uuid.Parse(ownerID)
		return WebhookQuota{
			OwnerId:                          ownerUUID,
			MaxSubscriptions:                 DefaultMaxSubscriptions,
			MaxEventsPerMinute:               DefaultMaxEventsPerMinute,
			MaxSubscriptionRequestsPerMinute: DefaultMaxSubscriptionRequestsPerMinute,
			MaxSubscriptionRequestsPerDay:    DefaultMaxSubscriptionRequestsPerDay,
		}
	}
	return quota
}

// Create creates a new webhook quota
func (s *WebhookQuotaDatabaseStore) Create(item WebhookQuota) (WebhookQuota, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Update timestamps
	updatedItem := UpdateTimestamps(&item, true)
	item = *updatedItem

	query := `
		INSERT INTO webhook_quotas (
			owner_id, max_subscriptions, max_events_per_minute,
			max_subscription_requests_per_minute, max_subscription_requests_per_day,
			created_at, modified_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := s.db.Exec(query,
		item.OwnerId, item.MaxSubscriptions, item.MaxEventsPerMinute,
		item.MaxSubscriptionRequestsPerMinute, item.MaxSubscriptionRequestsPerDay,
		item.CreatedAt, item.ModifiedAt,
	)

	if err != nil {
		return WebhookQuota{}, err
	}

	return item, nil
}

// Update updates an existing webhook quota
func (s *WebhookQuotaDatabaseStore) Update(ownerID string, item WebhookQuota) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Update modified timestamp
	updatedItem := UpdateTimestamps(&item, false)
	item = *updatedItem

	query := `
		UPDATE webhook_quotas
		SET max_subscriptions = $1, max_events_per_minute = $2,
		    max_subscription_requests_per_minute = $3, max_subscription_requests_per_day = $4,
		    modified_at = $5
		WHERE owner_id = $6
	`

	result, err := s.db.Exec(query,
		item.MaxSubscriptions, item.MaxEventsPerMinute,
		item.MaxSubscriptionRequestsPerMinute, item.MaxSubscriptionRequestsPerDay,
		item.ModifiedAt, ownerID,
	)

	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("webhook quota not found")
	}

	return nil
}

// Delete deletes a webhook quota
func (s *WebhookQuotaDatabaseStore) Delete(ownerID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	query := `DELETE FROM webhook_quotas WHERE owner_id = $1`

	result, err := s.db.Exec(query, ownerID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("webhook quota not found")
	}

	return nil
}

// WebhookUrlDenyListDatabaseStore implements WebhookUrlDenyListStoreInterface
type WebhookUrlDenyListDatabaseStore struct {
	db    *sql.DB
	mutex sync.RWMutex
}

// NewWebhookUrlDenyListDatabaseStore creates a new database-backed store
func NewWebhookUrlDenyListDatabaseStore(db *sql.DB) *WebhookUrlDenyListDatabaseStore {
	return &WebhookUrlDenyListDatabaseStore{db: db}
}

// List retrieves all deny list entries
func (s *WebhookUrlDenyListDatabaseStore) List() ([]WebhookUrlDenyListEntry, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	query := `
		SELECT id, pattern, pattern_type, description, created_at
		FROM webhook_url_deny_list
		ORDER BY pattern_type, pattern
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var entries []WebhookUrlDenyListEntry
	for rows.Next() {
		var entry WebhookUrlDenyListEntry
		var description sql.NullString

		err := rows.Scan(
			&entry.Id, &entry.Pattern, &entry.PatternType,
			&description, &entry.CreatedAt,
		)

		if err != nil {
			continue
		}

		if description.Valid {
			entry.Description = description.String
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// Create creates a new deny list entry
func (s *WebhookUrlDenyListDatabaseStore) Create(item WebhookUrlDenyListEntry) (WebhookUrlDenyListEntry, error) {
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

	query := `
		INSERT INTO webhook_url_deny_list (id, pattern, pattern_type, description, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	var description interface{}
	if item.Description != "" {
		description = item.Description
	}

	_, err := s.db.Exec(query,
		item.Id, item.Pattern, item.PatternType, description, item.CreatedAt,
	)

	if err != nil {
		return WebhookUrlDenyListEntry{}, err
	}

	return item, nil
}

// Delete deletes a deny list entry
func (s *WebhookUrlDenyListDatabaseStore) Delete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	query := `DELETE FROM webhook_url_deny_list WHERE id = $1`

	result, err := s.db.Exec(query, id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("webhook url deny list entry not found")
	}

	return nil
}
