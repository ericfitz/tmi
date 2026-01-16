package api

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test data helpers for webhook tests

func createTestWebhookSubscription() DBWebhookSubscription {
	id := uuid.New()
	ownerID := uuid.New()
	tmID := uuid.New()
	now := time.Now().UTC()
	lastUse := now.Add(-1 * time.Hour)

	return DBWebhookSubscription{
		Id:                  id,
		OwnerId:             ownerID,
		ThreatModelId:       &tmID,
		Name:                "Test Webhook",
		Url:                 "https://example.com/webhook",
		Events:              []string{"threat.created", "threat.updated"},
		Secret:              "test-secret-12345",
		Status:              "active",
		Challenge:           "challenge-abc",
		ChallengesSent:      1,
		CreatedAt:           now,
		ModifiedAt:          now,
		LastSuccessfulUse:   &lastUse,
		PublicationFailures: 0,
		TimeoutCount:        0,
	}
}

func createTestWebhookDelivery() DBWebhookDelivery {
	id := uuid.New()
	subID := uuid.New()
	now := time.Now().UTC()
	nextRetry := now.Add(5 * time.Minute)

	return DBWebhookDelivery{
		Id:             id,
		SubscriptionId: subID,
		EventType:      "threat.created",
		Payload:        `{"event":"threat.created","data":{"id":"123"}}`,
		Status:         "pending",
		Attempts:       1,
		NextRetryAt:    &nextRetry,
		LastError:      "connection timeout",
		CreatedAt:      now,
		DeliveredAt:    nil,
	}
}

func createTestWebhookQuota() DBWebhookQuota {
	ownerID := uuid.New()
	now := time.Now().UTC()

	return DBWebhookQuota{
		OwnerId:                          ownerID,
		MaxSubscriptions:                 20,
		MaxEventsPerMinute:               60,
		MaxSubscriptionRequestsPerMinute: 30,
		MaxSubscriptionRequestsPerDay:    100,
		CreatedAt:                        now,
		ModifiedAt:                       now,
	}
}

// =============================================================================
// DBWebhookSubscriptionDatabaseStore Tests
// =============================================================================

func TestNewDBWebhookSubscriptionDatabaseStore(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	store := NewDBWebhookSubscriptionDatabaseStore(db)

	assert.NotNil(t, store)
	assert.Equal(t, db, store.db)
}

func TestDBWebhookSubscriptionDatabaseStore_Get(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()
		testID := testSub.Id.String()

		rows := sqlmock.NewRows([]string{
			"id", "owner_internal_uuid", "threat_model_id", "name", "url", "events", "secret", "status",
			"challenge", "challenges_sent", "created_at", "modified_at",
			"last_successful_use", "publication_failures", "timeout_count",
		}).AddRow(
			testSub.Id, testSub.OwnerId, testSub.ThreatModelId.String(), testSub.Name, testSub.Url,
			pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
			testSub.ChallengesSent, testSub.CreatedAt, testSub.ModifiedAt,
			testSub.LastSuccessfulUse, testSub.PublicationFailures, testSub.TimeoutCount,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions").
			WithArgs(testID).
			WillReturnRows(rows)

		result, err := store.Get(testID)

		assert.NoError(t, err)
		assert.Equal(t, testSub.Name, result.Name)
		assert.Equal(t, testSub.Url, result.Url)
		assert.Equal(t, testSub.Events, result.Events)
		assert.Equal(t, testSub.Status, result.Status)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions").
			WithArgs(testID).
			WillReturnError(sql.ErrNoRows)

		_, err = store.Get(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions").
			WithArgs(testID).
			WillReturnError(assert.AnError)

		_, err = store.Get(testID)

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NullableFieldsHandling", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()
		subID := uuid.New()
		ownerID := uuid.New()
		now := time.Now().UTC()

		// Return row with null optional fields
		rows := sqlmock.NewRows([]string{
			"id", "owner_internal_uuid", "threat_model_id", "name", "url", "events", "secret", "status",
			"challenge", "challenges_sent", "created_at", "modified_at",
			"last_successful_use", "publication_failures", "timeout_count",
		}).AddRow(
			subID, ownerID, nil, "Test Sub", "https://example.com/hook",
			pq.Array([]string{"threat.created"}), nil, "pending_verification", nil,
			0, now, now,
			nil, 0, 0,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions").
			WithArgs(testID).
			WillReturnRows(rows)

		result, err := store.Get(testID)

		assert.NoError(t, err)
		assert.Nil(t, result.ThreatModelId)
		assert.Empty(t, result.Secret)
		assert.Empty(t, result.Challenge)
		assert.Nil(t, result.LastSuccessfulUse)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_List(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()

		rows := sqlmock.NewRows([]string{
			"id", "owner_internal_uuid", "threat_model_id", "name", "url", "events", "secret", "status",
			"challenge", "challenges_sent", "created_at", "modified_at",
			"last_successful_use", "publication_failures", "timeout_count",
		}).AddRow(
			testSub.Id, testSub.OwnerId, testSub.ThreatModelId.String(), testSub.Name, testSub.Url,
			pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
			testSub.ChallengesSent, testSub.CreatedAt, testSub.ModifiedAt,
			testSub.LastSuccessfulUse, testSub.PublicationFailures, testSub.TimeoutCount,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions ORDER BY created_at DESC LIMIT").
			WithArgs(10, 0).
			WillReturnRows(rows)

		result := store.List(0, 10, nil)

		assert.Len(t, result, 1)
		assert.Equal(t, testSub.Name, result[0].Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("WithFilter", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()
		testSub.Status = "active"

		testSub2 := createTestWebhookSubscription()
		testSub2.Id = uuid.New()
		testSub2.Status = "pending_verification"

		rows := sqlmock.NewRows([]string{
			"id", "owner_internal_uuid", "threat_model_id", "name", "url", "events", "secret", "status",
			"challenge", "challenges_sent", "created_at", "modified_at",
			"last_successful_use", "publication_failures", "timeout_count",
		}).AddRow(
			testSub.Id, testSub.OwnerId, testSub.ThreatModelId.String(), testSub.Name, testSub.Url,
			pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
			testSub.ChallengesSent, testSub.CreatedAt, testSub.ModifiedAt,
			testSub.LastSuccessfulUse, testSub.PublicationFailures, testSub.TimeoutCount,
		).AddRow(
			testSub2.Id, testSub2.OwnerId, testSub2.ThreatModelId.String(), testSub2.Name, testSub2.Url,
			pq.Array(testSub2.Events), testSub2.Secret, testSub2.Status, testSub2.Challenge,
			testSub2.ChallengesSent, testSub2.CreatedAt, testSub2.ModifiedAt,
			testSub2.LastSuccessfulUse, testSub2.PublicationFailures, testSub2.TimeoutCount,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions").
			WithArgs(10, 0).
			WillReturnRows(rows)

		// Filter for active subscriptions only
		result := store.List(0, 10, func(sub DBWebhookSubscription) bool {
			return sub.Status == "active"
		})

		assert.Len(t, result, 1)
		assert.Equal(t, "active", result[0].Status)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions").
			WithArgs(10, 0).
			WillReturnError(assert.AnError)

		result := store.List(0, 10, nil)

		assert.Empty(t, result)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_ListByOwner(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()
		ownerID := testSub.OwnerId.String()

		rows := sqlmock.NewRows([]string{
			"id", "owner_internal_uuid", "threat_model_id", "name", "url", "events", "secret", "status",
			"challenge", "challenges_sent", "created_at", "modified_at",
			"last_successful_use", "publication_failures", "timeout_count",
		}).AddRow(
			testSub.Id, testSub.OwnerId, testSub.ThreatModelId.String(), testSub.Name, testSub.Url,
			pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
			testSub.ChallengesSent, testSub.CreatedAt, testSub.ModifiedAt,
			testSub.LastSuccessfulUse, testSub.PublicationFailures, testSub.TimeoutCount,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions WHERE owner_internal_uuid").
			WithArgs(ownerID, 10, 0).
			WillReturnRows(rows)

		result, err := store.ListByOwner(ownerID, 0, 10)

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		ownerID := uuid.New().String()

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions WHERE owner_internal_uuid").
			WithArgs(ownerID, 10, 0).
			WillReturnError(assert.AnError)

		_, err = store.ListByOwner(ownerID, 0, 10)

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_ListByThreatModel(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()
		tmID := testSub.ThreatModelId.String()

		rows := sqlmock.NewRows([]string{
			"id", "owner_internal_uuid", "threat_model_id", "name", "url", "events", "secret", "status",
			"challenge", "challenges_sent", "created_at", "modified_at",
			"last_successful_use", "publication_failures", "timeout_count",
		}).AddRow(
			testSub.Id, testSub.OwnerId, testSub.ThreatModelId.String(), testSub.Name, testSub.Url,
			pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
			testSub.ChallengesSent, testSub.CreatedAt, testSub.ModifiedAt,
			testSub.LastSuccessfulUse, testSub.PublicationFailures, testSub.TimeoutCount,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions WHERE threat_model_id").
			WithArgs(tmID, 10, 0).
			WillReturnRows(rows)

		result, err := store.ListByThreatModel(tmID, 0, 10)

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_ListActiveByOwner(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()
		ownerID := testSub.OwnerId.String()

		rows := sqlmock.NewRows([]string{
			"id", "owner_internal_uuid", "threat_model_id", "name", "url", "events", "secret", "status",
			"challenge", "challenges_sent", "created_at", "modified_at",
			"last_successful_use", "publication_failures", "timeout_count",
		}).AddRow(
			testSub.Id, testSub.OwnerId, testSub.ThreatModelId.String(), testSub.Name, testSub.Url,
			pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
			testSub.ChallengesSent, testSub.CreatedAt, testSub.ModifiedAt,
			testSub.LastSuccessfulUse, testSub.PublicationFailures, testSub.TimeoutCount,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions WHERE owner_internal_uuid = \\$1 AND status = 'active'").
			WithArgs(ownerID).
			WillReturnRows(rows)

		result, err := store.ListActiveByOwner(ownerID)

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_ListPendingVerification(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()
		testSub.Status = "pending_verification"

		rows := sqlmock.NewRows([]string{
			"id", "owner_internal_uuid", "threat_model_id", "name", "url", "events", "secret", "status",
			"challenge", "challenges_sent", "created_at", "modified_at",
			"last_successful_use", "publication_failures", "timeout_count",
		}).AddRow(
			testSub.Id, testSub.OwnerId, testSub.ThreatModelId.String(), testSub.Name, testSub.Url,
			pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
			testSub.ChallengesSent, testSub.CreatedAt, testSub.ModifiedAt,
			testSub.LastSuccessfulUse, testSub.PublicationFailures, testSub.TimeoutCount,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions WHERE status = 'pending_verification'").
			WillReturnRows(rows)

		result, err := store.ListPendingVerification()

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "pending_verification", result[0].Status)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_ListPendingDelete(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()
		testSub.Status = "pending_delete"

		rows := sqlmock.NewRows([]string{
			"id", "owner_internal_uuid", "threat_model_id", "name", "url", "events", "secret", "status",
			"challenge", "challenges_sent", "created_at", "modified_at",
			"last_successful_use", "publication_failures", "timeout_count",
		}).AddRow(
			testSub.Id, testSub.OwnerId, testSub.ThreatModelId.String(), testSub.Name, testSub.Url,
			pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
			testSub.ChallengesSent, testSub.CreatedAt, testSub.ModifiedAt,
			testSub.LastSuccessfulUse, testSub.PublicationFailures, testSub.TimeoutCount,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions WHERE status = 'pending_delete'").
			WillReturnRows(rows)

		result, err := store.ListPendingDelete()

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "pending_delete", result[0].Status)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_ListIdle(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()

		rows := sqlmock.NewRows([]string{
			"id", "owner_internal_uuid", "threat_model_id", "name", "url", "events", "secret", "status",
			"challenge", "challenges_sent", "created_at", "modified_at",
			"last_successful_use", "publication_failures", "timeout_count",
		}).AddRow(
			testSub.Id, testSub.OwnerId, testSub.ThreatModelId.String(), testSub.Name, testSub.Url,
			pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
			testSub.ChallengesSent, testSub.CreatedAt, testSub.ModifiedAt,
			testSub.LastSuccessfulUse, testSub.PublicationFailures, testSub.TimeoutCount,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions WHERE status = 'active'").
			WithArgs(30).
			WillReturnRows(rows)

		result, err := store.ListIdle(30)

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_ListBroken(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()
		testSub.PublicationFailures = 5

		rows := sqlmock.NewRows([]string{
			"id", "owner_internal_uuid", "threat_model_id", "name", "url", "events", "secret", "status",
			"challenge", "challenges_sent", "created_at", "modified_at",
			"last_successful_use", "publication_failures", "timeout_count",
		}).AddRow(
			testSub.Id, testSub.OwnerId, testSub.ThreatModelId.String(), testSub.Name, testSub.Url,
			pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
			testSub.ChallengesSent, testSub.CreatedAt, testSub.ModifiedAt,
			testSub.LastSuccessfulUse, testSub.PublicationFailures, testSub.TimeoutCount,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_subscriptions WHERE status = 'active'").
			WithArgs(3, 7).
			WillReturnRows(rows)

		result, err := store.ListBroken(3, 7)

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_Create(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()
		testSub.Id = uuid.Nil // Will be generated

		mock.ExpectExec("INSERT INTO webhook_subscriptions").
			WithArgs(
				sqlmock.AnyArg(), testSub.OwnerId, testSub.ThreatModelId, testSub.Name, testSub.Url,
				pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
				testSub.ChallengesSent, sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), testSub.PublicationFailures, testSub.TimeoutCount,
			).
			WillReturnResult(sqlmock.NewResult(1, 1))

		result, err := store.Create(testSub, func(sub DBWebhookSubscription, id string) DBWebhookSubscription {
			sub.Id = uuid.MustParse(id)
			return sub
		})

		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, result.Id)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("WithExistingID", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()

		mock.ExpectExec("INSERT INTO webhook_subscriptions").
			WithArgs(
				testSub.Id, testSub.OwnerId, testSub.ThreatModelId, testSub.Name, testSub.Url,
				pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
				testSub.ChallengesSent, sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), testSub.PublicationFailures, testSub.TimeoutCount,
			).
			WillReturnResult(sqlmock.NewResult(1, 1))

		result, err := store.Create(testSub, nil)

		assert.NoError(t, err)
		assert.Equal(t, testSub.Id, result.Id)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()

		mock.ExpectExec("INSERT INTO webhook_subscriptions").
			WithArgs(
				testSub.Id, testSub.OwnerId, testSub.ThreatModelId, testSub.Name, testSub.Url,
				pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
				testSub.ChallengesSent, sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), testSub.PublicationFailures, testSub.TimeoutCount,
			).
			WillReturnError(assert.AnError)

		_, err = store.Create(testSub, nil)

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_Update(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()
		testID := testSub.Id.String()

		mock.ExpectExec("UPDATE webhook_subscriptions").
			WithArgs(
				testSub.OwnerId, testSub.ThreatModelId, testSub.Name, testSub.Url,
				pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
				testSub.ChallengesSent, sqlmock.AnyArg(), sqlmock.AnyArg(),
				testSub.PublicationFailures, testID,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.Update(testID, testSub)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testSub := createTestWebhookSubscription()
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_subscriptions").
			WithArgs(
				testSub.OwnerId, sqlmock.AnyArg(), testSub.Name, testSub.Url,
				pq.Array(testSub.Events), testSub.Secret, testSub.Status, testSub.Challenge,
				testSub.ChallengesSent, sqlmock.AnyArg(), sqlmock.AnyArg(),
				testSub.PublicationFailures, testID,
			).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.Update(testID, testSub)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_UpdateStatus(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_subscriptions SET status").
			WithArgs("active", sqlmock.AnyArg(), testID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.UpdateStatus(testID, "active")

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_subscriptions SET status").
			WithArgs("active", sqlmock.AnyArg(), testID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.UpdateStatus(testID, "active")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_UpdateChallenge(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_subscriptions SET challenge").
			WithArgs("new-challenge", 2, sqlmock.AnyArg(), testID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.UpdateChallenge(testID, "new-challenge", 2)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_subscriptions SET challenge").
			WithArgs("new-challenge", 2, sqlmock.AnyArg(), testID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.UpdateChallenge(testID, "new-challenge", 2)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_UpdatePublicationStats(t *testing.T) {
	t.Run("SuccessOnSuccess", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_subscriptions SET last_successful_use").
			WithArgs(sqlmock.AnyArg(), testID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.UpdatePublicationStats(testID, true)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("SuccessOnFailure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_subscriptions SET publication_failures = publication_failures \\+ 1").
			WithArgs(sqlmock.AnyArg(), testID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.UpdatePublicationStats(testID, false)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_subscriptions SET last_successful_use").
			WithArgs(sqlmock.AnyArg(), testID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.UpdatePublicationStats(testID, true)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_IncrementTimeouts(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_subscriptions SET timeout_count = timeout_count \\+ 1").
			WithArgs(sqlmock.AnyArg(), testID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.IncrementTimeouts(testID)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_subscriptions SET timeout_count = timeout_count \\+ 1").
			WithArgs(sqlmock.AnyArg(), testID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.IncrementTimeouts(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_ResetTimeouts(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_subscriptions SET timeout_count = 0").
			WithArgs(sqlmock.AnyArg(), testID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.ResetTimeouts(testID)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_subscriptions SET timeout_count = 0").
			WithArgs(sqlmock.AnyArg(), testID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.ResetTimeouts(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_Delete(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("DELETE FROM webhook_subscriptions WHERE id").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.Delete(testID)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("DELETE FROM webhook_subscriptions WHERE id").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.Delete(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("DELETE FROM webhook_subscriptions WHERE id").
			WithArgs(testID).
			WillReturnError(assert.AnError)

		err = store.Delete(testID)

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_Count(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)

		rows := sqlmock.NewRows([]string{"count"}).AddRow(5)
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM webhook_subscriptions").
			WillReturnRows(rows)

		count := store.Count()

		assert.Equal(t, 5, count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)

		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM webhook_subscriptions").
			WillReturnError(assert.AnError)

		count := store.Count()

		assert.Equal(t, 0, count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookSubscriptionDatabaseStore_CountByOwner(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		ownerID := uuid.New().String()

		rows := sqlmock.NewRows([]string{"count"}).AddRow(3)
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM webhook_subscriptions WHERE owner_internal_uuid").
			WithArgs(ownerID).
			WillReturnRows(rows)

		count, err := store.CountByOwner(ownerID)

		assert.NoError(t, err)
		assert.Equal(t, 3, count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookSubscriptionDatabaseStore(db)
		ownerID := uuid.New().String()

		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM webhook_subscriptions WHERE owner_internal_uuid").
			WithArgs(ownerID).
			WillReturnError(assert.AnError)

		_, err = store.CountByOwner(ownerID)

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =============================================================================
// DBWebhookDeliveryDatabaseStore Tests
// =============================================================================

func TestNewDBWebhookDeliveryDatabaseStore(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	store := NewDBWebhookDeliveryDatabaseStore(db)

	assert.NotNil(t, store)
	assert.Equal(t, db, store.db)
}

func TestDBWebhookDeliveryDatabaseStore_Get(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testDelivery := createTestWebhookDelivery()
		testID := testDelivery.Id.String()

		rows := sqlmock.NewRows([]string{
			"id", "subscription_id", "event_type", "payload", "status", "attempts",
			"next_retry_at", "last_error", "created_at", "delivered_at",
		}).AddRow(
			testDelivery.Id, testDelivery.SubscriptionId, testDelivery.EventType,
			testDelivery.Payload, testDelivery.Status, testDelivery.Attempts,
			testDelivery.NextRetryAt, testDelivery.LastError, testDelivery.CreatedAt, testDelivery.DeliveredAt,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_deliveries WHERE id").
			WithArgs(testID).
			WillReturnRows(rows)

		result, err := store.Get(testID)

		assert.NoError(t, err)
		assert.Equal(t, testDelivery.EventType, result.EventType)
		assert.Equal(t, testDelivery.Payload, result.Payload)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectQuery("SELECT (.+) FROM webhook_deliveries WHERE id").
			WithArgs(testID).
			WillReturnError(sql.ErrNoRows)

		_, err = store.Get(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookDeliveryDatabaseStore_List(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testDelivery := createTestWebhookDelivery()

		rows := sqlmock.NewRows([]string{
			"id", "subscription_id", "event_type", "payload", "status", "attempts",
			"next_retry_at", "last_error", "created_at", "delivered_at",
		}).AddRow(
			testDelivery.Id, testDelivery.SubscriptionId, testDelivery.EventType,
			testDelivery.Payload, testDelivery.Status, testDelivery.Attempts,
			testDelivery.NextRetryAt, testDelivery.LastError, testDelivery.CreatedAt, testDelivery.DeliveredAt,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_deliveries ORDER BY created_at DESC LIMIT").
			WithArgs(10, 0).
			WillReturnRows(rows)

		result := store.List(0, 10, nil)

		assert.Len(t, result, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)

		mock.ExpectQuery("SELECT (.+) FROM webhook_deliveries").
			WithArgs(10, 0).
			WillReturnError(assert.AnError)

		result := store.List(0, 10, nil)

		assert.Empty(t, result)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookDeliveryDatabaseStore_ListBySubscription(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testDelivery := createTestWebhookDelivery()
		subID := testDelivery.SubscriptionId.String()

		rows := sqlmock.NewRows([]string{
			"id", "subscription_id", "event_type", "payload", "status", "attempts",
			"next_retry_at", "last_error", "created_at", "delivered_at",
		}).AddRow(
			testDelivery.Id, testDelivery.SubscriptionId, testDelivery.EventType,
			testDelivery.Payload, testDelivery.Status, testDelivery.Attempts,
			testDelivery.NextRetryAt, testDelivery.LastError, testDelivery.CreatedAt, testDelivery.DeliveredAt,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_deliveries WHERE subscription_id").
			WithArgs(subID, 10, 0).
			WillReturnRows(rows)

		result, err := store.ListBySubscription(subID, 0, 10)

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookDeliveryDatabaseStore_ListPending(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testDelivery := createTestWebhookDelivery()

		rows := sqlmock.NewRows([]string{
			"id", "subscription_id", "event_type", "payload", "status", "attempts",
			"next_retry_at", "last_error", "created_at", "delivered_at",
		}).AddRow(
			testDelivery.Id, testDelivery.SubscriptionId, testDelivery.EventType,
			testDelivery.Payload, testDelivery.Status, testDelivery.Attempts,
			testDelivery.NextRetryAt, testDelivery.LastError, testDelivery.CreatedAt, testDelivery.DeliveredAt,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_deliveries WHERE status = 'pending'").
			WithArgs(100).
			WillReturnRows(rows)

		result, err := store.ListPending(100)

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookDeliveryDatabaseStore_ListReadyForRetry(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testDelivery := createTestWebhookDelivery()

		rows := sqlmock.NewRows([]string{
			"id", "subscription_id", "event_type", "payload", "status", "attempts",
			"next_retry_at", "last_error", "created_at", "delivered_at",
		}).AddRow(
			testDelivery.Id, testDelivery.SubscriptionId, testDelivery.EventType,
			testDelivery.Payload, testDelivery.Status, testDelivery.Attempts,
			testDelivery.NextRetryAt, testDelivery.LastError, testDelivery.CreatedAt, testDelivery.DeliveredAt,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_deliveries WHERE status = 'pending' AND next_retry_at IS NOT NULL AND next_retry_at <= NOW").
			WillReturnRows(rows)

		result, err := store.ListReadyForRetry()

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookDeliveryDatabaseStore_Create(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testDelivery := createTestWebhookDelivery()
		testDelivery.Id = uuid.Nil // Will be generated

		mock.ExpectExec("INSERT INTO webhook_deliveries").
			WithArgs(
				sqlmock.AnyArg(), testDelivery.SubscriptionId, testDelivery.EventType, testDelivery.Payload,
				testDelivery.Status, testDelivery.Attempts, sqlmock.AnyArg(), testDelivery.LastError,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(1, 1))

		result, err := store.Create(testDelivery)

		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, result.Id)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testDelivery := createTestWebhookDelivery()

		mock.ExpectExec("INSERT INTO webhook_deliveries").
			WithArgs(
				testDelivery.Id, testDelivery.SubscriptionId, testDelivery.EventType, testDelivery.Payload,
				testDelivery.Status, testDelivery.Attempts, sqlmock.AnyArg(), testDelivery.LastError,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(assert.AnError)

		_, err = store.Create(testDelivery)

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookDeliveryDatabaseStore_Update(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testDelivery := createTestWebhookDelivery()
		testID := testDelivery.Id.String()

		mock.ExpectExec("UPDATE webhook_deliveries SET").
			WithArgs(
				testDelivery.SubscriptionId, testDelivery.EventType, testDelivery.Payload, testDelivery.Status,
				testDelivery.Attempts, sqlmock.AnyArg(), testDelivery.LastError, sqlmock.AnyArg(), testID,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.Update(testID, testDelivery)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testDelivery := createTestWebhookDelivery()
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_deliveries SET").
			WithArgs(
				testDelivery.SubscriptionId, testDelivery.EventType, testDelivery.Payload, testDelivery.Status,
				testDelivery.Attempts, sqlmock.AnyArg(), testDelivery.LastError, sqlmock.AnyArg(), testID,
			).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.Update(testID, testDelivery)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookDeliveryDatabaseStore_UpdateStatus(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testID := uuid.New().String()
		now := time.Now()

		mock.ExpectExec("UPDATE webhook_deliveries SET status").
			WithArgs("delivered", sqlmock.AnyArg(), testID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.UpdateStatus(testID, "delivered", &now)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_deliveries SET status").
			WithArgs("delivered", nil, testID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.UpdateStatus(testID, "delivered", nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookDeliveryDatabaseStore_UpdateRetry(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testID := uuid.New().String()
		nextRetry := time.Now().Add(5 * time.Minute)

		mock.ExpectExec("UPDATE webhook_deliveries SET attempts").
			WithArgs(2, sqlmock.AnyArg(), "connection timeout", testID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.UpdateRetry(testID, 2, &nextRetry, "connection timeout")

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_deliveries SET attempts").
			WithArgs(2, nil, "error", testID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.UpdateRetry(testID, 2, nil, "error")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookDeliveryDatabaseStore_Delete(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("DELETE FROM webhook_deliveries WHERE id").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.Delete(testID)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("DELETE FROM webhook_deliveries WHERE id").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.Delete(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookDeliveryDatabaseStore_DeleteOld(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)

		mock.ExpectExec("DELETE FROM webhook_deliveries WHERE status IN").
			WithArgs(30).
			WillReturnResult(sqlmock.NewResult(0, 10))

		count, err := store.DeleteOld(30)

		assert.NoError(t, err)
		assert.Equal(t, 10, count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)

		mock.ExpectExec("DELETE FROM webhook_deliveries WHERE status IN").
			WithArgs(30).
			WillReturnError(assert.AnError)

		_, err = store.DeleteOld(30)

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDBWebhookDeliveryDatabaseStore_Count(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)

		rows := sqlmock.NewRows([]string{"count"}).AddRow(15)
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM webhook_deliveries").
			WillReturnRows(rows)

		count := store.Count()

		assert.Equal(t, 15, count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDBWebhookDeliveryDatabaseStore(db)

		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM webhook_deliveries").
			WillReturnError(assert.AnError)

		count := store.Count()

		assert.Equal(t, 0, count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =============================================================================
// WebhookQuotaDatabaseStore Tests
// =============================================================================

func TestNewWebhookQuotaDatabaseStore(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	store := NewWebhookQuotaDatabaseStore(db)

	assert.NotNil(t, store)
	assert.Equal(t, db, store.db)
}

func TestWebhookQuotaDatabaseStore_Get(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookQuotaDatabaseStore(db)
		testQuota := createTestWebhookQuota()
		ownerID := testQuota.OwnerId.String()

		rows := sqlmock.NewRows([]string{
			"owner_id", "max_subscriptions", "max_events_per_minute",
			"max_subscription_requests_per_minute", "max_subscription_requests_per_day",
			"created_at", "modified_at",
		}).AddRow(
			testQuota.OwnerId, testQuota.MaxSubscriptions, testQuota.MaxEventsPerMinute,
			testQuota.MaxSubscriptionRequestsPerMinute, testQuota.MaxSubscriptionRequestsPerDay,
			testQuota.CreatedAt, testQuota.ModifiedAt,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_quotas WHERE owner_id").
			WithArgs(ownerID).
			WillReturnRows(rows)

		result, err := store.Get(ownerID)

		assert.NoError(t, err)
		assert.Equal(t, testQuota.MaxSubscriptions, result.MaxSubscriptions)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookQuotaDatabaseStore(db)
		ownerID := uuid.New().String()

		mock.ExpectQuery("SELECT (.+) FROM webhook_quotas WHERE owner_id").
			WithArgs(ownerID).
			WillReturnError(sql.ErrNoRows)

		_, err = store.Get(ownerID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestWebhookQuotaDatabaseStore_GetOrDefault(t *testing.T) {
	t.Run("ReturnsExistingQuota", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookQuotaDatabaseStore(db)
		testQuota := createTestWebhookQuota()
		ownerID := testQuota.OwnerId.String()

		rows := sqlmock.NewRows([]string{
			"owner_id", "max_subscriptions", "max_events_per_minute",
			"max_subscription_requests_per_minute", "max_subscription_requests_per_day",
			"created_at", "modified_at",
		}).AddRow(
			testQuota.OwnerId, testQuota.MaxSubscriptions, testQuota.MaxEventsPerMinute,
			testQuota.MaxSubscriptionRequestsPerMinute, testQuota.MaxSubscriptionRequestsPerDay,
			testQuota.CreatedAt, testQuota.ModifiedAt,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_quotas WHERE owner_id").
			WithArgs(ownerID).
			WillReturnRows(rows)

		result := store.GetOrDefault(ownerID)

		assert.Equal(t, testQuota.MaxSubscriptions, result.MaxSubscriptions)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("ReturnsDefaultWhenNotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookQuotaDatabaseStore(db)
		ownerID := uuid.New().String()

		mock.ExpectQuery("SELECT (.+) FROM webhook_quotas WHERE owner_id").
			WithArgs(ownerID).
			WillReturnError(sql.ErrNoRows)

		result := store.GetOrDefault(ownerID)

		assert.Equal(t, DefaultMaxSubscriptions, result.MaxSubscriptions)
		assert.Equal(t, DefaultMaxEventsPerMinute, result.MaxEventsPerMinute)
		assert.Equal(t, DefaultMaxSubscriptionRequestsPerMinute, result.MaxSubscriptionRequestsPerMinute)
		assert.Equal(t, DefaultMaxSubscriptionRequestsPerDay, result.MaxSubscriptionRequestsPerDay)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestWebhookQuotaDatabaseStore_List(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookQuotaDatabaseStore(db)
		testQuota := createTestWebhookQuota()

		rows := sqlmock.NewRows([]string{
			"owner_id", "max_subscriptions", "max_events_per_minute",
			"max_subscription_requests_per_minute", "max_subscription_requests_per_day",
			"created_at", "modified_at",
		}).AddRow(
			testQuota.OwnerId, testQuota.MaxSubscriptions, testQuota.MaxEventsPerMinute,
			testQuota.MaxSubscriptionRequestsPerMinute, testQuota.MaxSubscriptionRequestsPerDay,
			testQuota.CreatedAt, testQuota.ModifiedAt,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_quotas ORDER BY created_at DESC LIMIT").
			WithArgs(10, 0).
			WillReturnRows(rows)

		result, err := store.List(0, 10)

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookQuotaDatabaseStore(db)

		mock.ExpectQuery("SELECT (.+) FROM webhook_quotas").
			WithArgs(10, 0).
			WillReturnError(assert.AnError)

		_, err = store.List(0, 10)

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestWebhookQuotaDatabaseStore_Create(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookQuotaDatabaseStore(db)
		testQuota := createTestWebhookQuota()

		mock.ExpectExec("INSERT INTO webhook_quotas").
			WithArgs(
				testQuota.OwnerId, testQuota.MaxSubscriptions, testQuota.MaxEventsPerMinute,
				testQuota.MaxSubscriptionRequestsPerMinute, testQuota.MaxSubscriptionRequestsPerDay,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(1, 1))

		result, err := store.Create(testQuota)

		assert.NoError(t, err)
		assert.Equal(t, testQuota.OwnerId, result.OwnerId)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookQuotaDatabaseStore(db)
		testQuota := createTestWebhookQuota()

		mock.ExpectExec("INSERT INTO webhook_quotas").
			WithArgs(
				testQuota.OwnerId, testQuota.MaxSubscriptions, testQuota.MaxEventsPerMinute,
				testQuota.MaxSubscriptionRequestsPerMinute, testQuota.MaxSubscriptionRequestsPerDay,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(assert.AnError)

		_, err = store.Create(testQuota)

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestWebhookQuotaDatabaseStore_Update(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookQuotaDatabaseStore(db)
		testQuota := createTestWebhookQuota()
		ownerID := testQuota.OwnerId.String()

		mock.ExpectExec("UPDATE webhook_quotas SET").
			WithArgs(
				testQuota.MaxSubscriptions, testQuota.MaxEventsPerMinute,
				testQuota.MaxSubscriptionRequestsPerMinute, testQuota.MaxSubscriptionRequestsPerDay,
				sqlmock.AnyArg(), ownerID,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.Update(ownerID, testQuota)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookQuotaDatabaseStore(db)
		testQuota := createTestWebhookQuota()
		ownerID := uuid.New().String()

		mock.ExpectExec("UPDATE webhook_quotas SET").
			WithArgs(
				testQuota.MaxSubscriptions, testQuota.MaxEventsPerMinute,
				testQuota.MaxSubscriptionRequestsPerMinute, testQuota.MaxSubscriptionRequestsPerDay,
				sqlmock.AnyArg(), ownerID,
			).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.Update(ownerID, testQuota)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestWebhookQuotaDatabaseStore_Delete(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookQuotaDatabaseStore(db)
		ownerID := uuid.New().String()

		mock.ExpectExec("DELETE FROM webhook_quotas WHERE owner_id").
			WithArgs(ownerID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.Delete(ownerID)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookQuotaDatabaseStore(db)
		ownerID := uuid.New().String()

		mock.ExpectExec("DELETE FROM webhook_quotas WHERE owner_id").
			WithArgs(ownerID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.Delete(ownerID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =============================================================================
// WebhookUrlDenyListDatabaseStore Tests
// =============================================================================

func TestNewWebhookUrlDenyListDatabaseStore(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	store := NewWebhookUrlDenyListDatabaseStore(db)

	assert.NotNil(t, store)
	assert.Equal(t, db, store.db)
}

func TestWebhookUrlDenyListDatabaseStore_List(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookUrlDenyListDatabaseStore(db)
		testID := uuid.New()
		now := time.Now().UTC()

		rows := sqlmock.NewRows([]string{
			"id", "pattern", "pattern_type", "description", "created_at",
		}).AddRow(
			testID, "*.local", "glob", "Block local domains", now,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_url_deny_list ORDER BY pattern_type, pattern").
			WillReturnRows(rows)

		result, err := store.List()

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "*.local", result[0].Pattern)
		assert.Equal(t, "glob", result[0].PatternType)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("WithNullDescription", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookUrlDenyListDatabaseStore(db)
		testID := uuid.New()
		now := time.Now().UTC()

		rows := sqlmock.NewRows([]string{
			"id", "pattern", "pattern_type", "description", "created_at",
		}).AddRow(
			testID, "^10\\.", "regex", nil, now,
		)

		mock.ExpectQuery("SELECT (.+) FROM webhook_url_deny_list").
			WillReturnRows(rows)

		result, err := store.List()

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Empty(t, result[0].Description)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookUrlDenyListDatabaseStore(db)

		mock.ExpectQuery("SELECT (.+) FROM webhook_url_deny_list").
			WillReturnError(assert.AnError)

		_, err = store.List()

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestWebhookUrlDenyListDatabaseStore_Create(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookUrlDenyListDatabaseStore(db)
		entry := WebhookUrlDenyListEntry{
			Pattern:     "*.evil.com",
			PatternType: "glob",
			Description: "Block evil domain",
		}

		mock.ExpectExec("INSERT INTO webhook_url_deny_list").
			WithArgs(sqlmock.AnyArg(), entry.Pattern, entry.PatternType, entry.Description, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))

		result, err := store.Create(entry)

		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, result.Id)
		assert.Equal(t, entry.Pattern, result.Pattern)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("WithExistingID", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookUrlDenyListDatabaseStore(db)
		entry := WebhookUrlDenyListEntry{
			Id:          uuid.New(),
			Pattern:     "*.test.com",
			PatternType: "glob",
			CreatedAt:   time.Now().UTC(),
		}

		mock.ExpectExec("INSERT INTO webhook_url_deny_list").
			WithArgs(entry.Id, entry.Pattern, entry.PatternType, nil, entry.CreatedAt).
			WillReturnResult(sqlmock.NewResult(1, 1))

		result, err := store.Create(entry)

		assert.NoError(t, err)
		assert.Equal(t, entry.Id, result.Id)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookUrlDenyListDatabaseStore(db)
		entry := WebhookUrlDenyListEntry{
			Pattern:     "*.bad.com",
			PatternType: "glob",
		}

		mock.ExpectExec("INSERT INTO webhook_url_deny_list").
			WithArgs(sqlmock.AnyArg(), entry.Pattern, entry.PatternType, nil, sqlmock.AnyArg()).
			WillReturnError(assert.AnError)

		_, err = store.Create(entry)

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestWebhookUrlDenyListDatabaseStore_Delete(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookUrlDenyListDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("DELETE FROM webhook_url_deny_list WHERE id").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.Delete(testID)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookUrlDenyListDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("DELETE FROM webhook_url_deny_list WHERE id").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.Delete(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewWebhookUrlDenyListDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("DELETE FROM webhook_url_deny_list WHERE id").
			WithArgs(testID).
			WillReturnError(assert.AnError)

		err = store.Delete(testID)

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
