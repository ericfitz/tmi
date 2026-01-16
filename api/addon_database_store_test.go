package api

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test data helpers for addon tests

func createTestAddon() *Addon {
	id := uuid.New()
	webhookID := uuid.New()
	tmID := uuid.New()
	now := time.Now().UTC()

	return &Addon{
		ID:            id,
		CreatedAt:     now,
		Name:          "Test Addon",
		WebhookID:     webhookID,
		Description:   "A test addon for threat analysis",
		Icon:          "security",
		Objects:       []string{"threat", "diagram"},
		ThreatModelID: &tmID,
	}
}

// =============================================================================
// AddonDatabaseStore Tests
// =============================================================================

func TestNewAddonDatabaseStore(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	store := NewAddonDatabaseStore(db)

	assert.NotNil(t, store)
	assert.Equal(t, db, store.db)
}

func TestAddonDatabaseStore_Create(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		addon := createTestAddon()
		addon.ID = uuid.Nil // Will be generated

		rows := sqlmock.NewRows([]string{"id", "created_at"}).
			AddRow(uuid.New(), time.Now().UTC())

		mock.ExpectQuery("INSERT INTO addons").
			WithArgs(
				sqlmock.AnyArg(), // ID (generated)
				sqlmock.AnyArg(), // CreatedAt
				addon.Name,
				addon.WebhookID,
				addon.Description,
				addon.Icon,
				pq.Array(addon.Objects),
				addon.ThreatModelID,
			).
			WillReturnRows(rows)

		err = store.Create(context.Background(), addon)

		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, addon.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("WithExistingID", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		addon := createTestAddon()

		rows := sqlmock.NewRows([]string{"id", "created_at"}).
			AddRow(addon.ID, addon.CreatedAt)

		mock.ExpectQuery("INSERT INTO addons").
			WithArgs(
				addon.ID,
				addon.CreatedAt,
				addon.Name,
				addon.WebhookID,
				addon.Description,
				addon.Icon,
				pq.Array(addon.Objects),
				addon.ThreatModelID,
			).
			WillReturnRows(rows)

		err = store.Create(context.Background(), addon)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		addon := createTestAddon()

		mock.ExpectQuery("INSERT INTO addons").
			WithArgs(
				addon.ID,
				addon.CreatedAt,
				addon.Name,
				addon.WebhookID,
				addon.Description,
				addon.Icon,
				pq.Array(addon.Objects),
				addon.ThreatModelID,
			).
			WillReturnError(assert.AnError)

		err = store.Create(context.Background(), addon)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create add-on")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestAddonDatabaseStore_Get(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		testAddon := createTestAddon()

		rows := sqlmock.NewRows([]string{
			"id", "created_at", "name", "webhook_id", "description", "icon", "objects", "threat_model_id",
		}).AddRow(
			testAddon.ID,
			testAddon.CreatedAt,
			testAddon.Name,
			testAddon.WebhookID,
			testAddon.Description,
			testAddon.Icon,
			pq.Array(testAddon.Objects),
			testAddon.ThreatModelID.String(),
		)

		mock.ExpectQuery("SELECT (.+) FROM addons WHERE id").
			WithArgs(testAddon.ID).
			WillReturnRows(rows)

		result, err := store.Get(context.Background(), testAddon.ID)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, testAddon.ID, result.ID)
		assert.Equal(t, testAddon.Name, result.Name)
		assert.Equal(t, testAddon.WebhookID, result.WebhookID)
		assert.Equal(t, testAddon.Description, result.Description)
		assert.Equal(t, testAddon.Objects, result.Objects)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		testID := uuid.New()

		mock.ExpectQuery("SELECT (.+) FROM addons WHERE id").
			WithArgs(testID).
			WillReturnError(sql.ErrNoRows)

		result, err := store.Get(context.Background(), testID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		testID := uuid.New()

		mock.ExpectQuery("SELECT (.+) FROM addons WHERE id").
			WithArgs(testID).
			WillReturnError(assert.AnError)

		result, err := store.Get(context.Background(), testID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to get add-on")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NullableFieldsHandling", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		testID := uuid.New()
		webhookID := uuid.New()
		now := time.Now().UTC()

		// Return row with null optional fields
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "name", "webhook_id", "description", "icon", "objects", "threat_model_id",
		}).AddRow(
			testID,
			now,
			"Test Addon",
			webhookID,
			nil, // description
			nil, // icon
			pq.Array([]string{}),
			nil, // threat_model_id
		)

		mock.ExpectQuery("SELECT (.+) FROM addons WHERE id").
			WithArgs(testID).
			WillReturnRows(rows)

		result, err := store.Get(context.Background(), testID)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result.Description)
		assert.Empty(t, result.Icon)
		assert.Nil(t, result.ThreatModelID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestAddonDatabaseStore_List(t *testing.T) {
	t.Run("SuccessWithoutFilter", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		testAddon := createTestAddon()

		// Mock count query
		countRows := sqlmock.NewRows([]string{"count"}).AddRow(1)
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM addons").
			WillReturnRows(countRows)

		// Mock list query
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "name", "webhook_id", "description", "icon", "objects", "threat_model_id",
		}).AddRow(
			testAddon.ID,
			testAddon.CreatedAt,
			testAddon.Name,
			testAddon.WebhookID,
			testAddon.Description,
			testAddon.Icon,
			pq.Array(testAddon.Objects),
			testAddon.ThreatModelID.String(),
		)

		mock.ExpectQuery("SELECT (.+) FROM addons ORDER BY created_at DESC LIMIT").
			WithArgs(10, 0).
			WillReturnRows(rows)

		result, total, err := store.List(context.Background(), 10, 0, nil)

		assert.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Len(t, result, 1)
		assert.Equal(t, testAddon.Name, result[0].Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("SuccessWithThreatModelFilter", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		testAddon := createTestAddon()
		tmID := uuid.New()

		// Mock count query with filter
		countRows := sqlmock.NewRows([]string{"count"}).AddRow(1)
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM addons WHERE threat_model_id").
			WithArgs(&tmID).
			WillReturnRows(countRows)

		// Mock list query with filter
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "name", "webhook_id", "description", "icon", "objects", "threat_model_id",
		}).AddRow(
			testAddon.ID,
			testAddon.CreatedAt,
			testAddon.Name,
			testAddon.WebhookID,
			testAddon.Description,
			testAddon.Icon,
			pq.Array(testAddon.Objects),
			tmID.String(),
		)

		mock.ExpectQuery("SELECT (.+) FROM addons WHERE threat_model_id").
			WithArgs(&tmID, 10, 0).
			WillReturnRows(rows)

		result, total, err := store.List(context.Background(), 10, 0, &tmID)

		assert.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Len(t, result, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("CountError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)

		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM addons").
			WillReturnError(assert.AnError)

		_, _, err = store.List(context.Background(), 10, 0, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to count add-ons")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("QueryError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)

		// Mock count query
		countRows := sqlmock.NewRows([]string{"count"}).AddRow(5)
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM addons").
			WillReturnRows(countRows)

		// Mock list query error
		mock.ExpectQuery("SELECT (.+) FROM addons ORDER BY created_at DESC LIMIT").
			WithArgs(10, 0).
			WillReturnError(assert.AnError)

		_, _, err = store.List(context.Background(), 10, 0, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list add-ons")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("EmptyResult", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)

		// Mock count query
		countRows := sqlmock.NewRows([]string{"count"}).AddRow(0)
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM addons").
			WillReturnRows(countRows)

		// Mock empty list query
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "name", "webhook_id", "description", "icon", "objects", "threat_model_id",
		})

		mock.ExpectQuery("SELECT (.+) FROM addons ORDER BY created_at DESC LIMIT").
			WithArgs(10, 0).
			WillReturnRows(rows)

		result, total, err := store.List(context.Background(), 10, 0, nil)

		assert.NoError(t, err)
		assert.Equal(t, 0, total)
		assert.Empty(t, result)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestAddonDatabaseStore_Delete(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		testID := uuid.New()

		mock.ExpectExec("DELETE FROM addons WHERE id").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.Delete(context.Background(), testID)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		testID := uuid.New()

		mock.ExpectExec("DELETE FROM addons WHERE id").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.Delete(context.Background(), testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		testID := uuid.New()

		mock.ExpectExec("DELETE FROM addons WHERE id").
			WithArgs(testID).
			WillReturnError(assert.AnError)

		err = store.Delete(context.Background(), testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete add-on")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestAddonDatabaseStore_GetByWebhookID(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		testAddon := createTestAddon()
		webhookID := testAddon.WebhookID

		rows := sqlmock.NewRows([]string{
			"id", "created_at", "name", "webhook_id", "description", "icon", "objects", "threat_model_id",
		}).AddRow(
			testAddon.ID,
			testAddon.CreatedAt,
			testAddon.Name,
			testAddon.WebhookID,
			testAddon.Description,
			testAddon.Icon,
			pq.Array(testAddon.Objects),
			testAddon.ThreatModelID.String(),
		)

		mock.ExpectQuery("SELECT (.+) FROM addons WHERE webhook_id").
			WithArgs(webhookID).
			WillReturnRows(rows)

		result, err := store.GetByWebhookID(context.Background(), webhookID)

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, testAddon.ID, result[0].ID)
		assert.Equal(t, testAddon.Name, result[0].Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("MultipleAddons", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		webhookID := uuid.New()
		now := time.Now().UTC()

		rows := sqlmock.NewRows([]string{
			"id", "created_at", "name", "webhook_id", "description", "icon", "objects", "threat_model_id",
		}).AddRow(
			uuid.New(),
			now,
			"Addon 1",
			webhookID,
			"Description 1",
			"icon1",
			pq.Array([]string{"threat"}),
			nil,
		).AddRow(
			uuid.New(),
			now,
			"Addon 2",
			webhookID,
			"Description 2",
			"icon2",
			pq.Array([]string{"diagram"}),
			nil,
		)

		mock.ExpectQuery("SELECT (.+) FROM addons WHERE webhook_id").
			WithArgs(webhookID).
			WillReturnRows(rows)

		result, err := store.GetByWebhookID(context.Background(), webhookID)

		assert.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "Addon 1", result[0].Name)
		assert.Equal(t, "Addon 2", result[1].Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("EmptyResult", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		webhookID := uuid.New()

		rows := sqlmock.NewRows([]string{
			"id", "created_at", "name", "webhook_id", "description", "icon", "objects", "threat_model_id",
		})

		mock.ExpectQuery("SELECT (.+) FROM addons WHERE webhook_id").
			WithArgs(webhookID).
			WillReturnRows(rows)

		result, err := store.GetByWebhookID(context.Background(), webhookID)

		assert.NoError(t, err)
		assert.Empty(t, result)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		webhookID := uuid.New()

		mock.ExpectQuery("SELECT (.+) FROM addons WHERE webhook_id").
			WithArgs(webhookID).
			WillReturnError(assert.AnError)

		result, err := store.GetByWebhookID(context.Background(), webhookID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to get add-ons by webhook")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NullableFieldsHandling", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		webhookID := uuid.New()
		now := time.Now().UTC()

		rows := sqlmock.NewRows([]string{
			"id", "created_at", "name", "webhook_id", "description", "icon", "objects", "threat_model_id",
		}).AddRow(
			uuid.New(),
			now,
			"Test Addon",
			webhookID,
			nil, // description
			nil, // icon
			pq.Array([]string{}),
			nil, // threat_model_id
		)

		mock.ExpectQuery("SELECT (.+) FROM addons WHERE webhook_id").
			WithArgs(webhookID).
			WillReturnRows(rows)

		result, err := store.GetByWebhookID(context.Background(), webhookID)

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Empty(t, result[0].Description)
		assert.Empty(t, result[0].Icon)
		assert.Nil(t, result[0].ThreatModelID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestAddonDatabaseStore_CountActiveInvocations(t *testing.T) {
	t.Run("StoreNotInitialized", func(t *testing.T) {
		db, _, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewAddonDatabaseStore(db)
		testID := uuid.New()

		// Save and restore global store
		oldStore := GlobalAddonInvocationStore
		GlobalAddonInvocationStore = nil
		defer func() { GlobalAddonInvocationStore = oldStore }()

		count, err := store.CountActiveInvocations(context.Background(), testID)

		assert.NoError(t, err)
		assert.Equal(t, 0, count)
	})
}
