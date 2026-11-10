package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// #1150: repository, document and asset writes must return (and cache) the
// timestamps the database holds, at the microsecond resolution it stores.

// setupSubResourceTimestampDB extends the note test DB with the tables the
// repository, document and asset stores write.
func setupSubResourceTimestampDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	db, _, tm := setupNoteTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.Repository{}, &models.Document{}, &models.Asset{}))
	return db, string(tm.ID)
}

// storedTimes reads the timestamps the database holds for one row.
func storedTimes(t *testing.T, db *gorm.DB, model any, id string) (time.Time, time.Time) {
	t.Helper()
	var row struct {
		CreatedAt  time.Time
		ModifiedAt time.Time
	}
	require.NoError(t, db.Model(model).Select("created_at", "modified_at").
		Where("id = ?", id).Take(&row).Error)
	return row.CreatedAt, row.ModifiedAt
}

// backdate rewrites a row's timestamps so an update visibly moves modified_at.
func backdate(t *testing.T, db *gorm.DB, model any, id string, at time.Time) {
	t.Helper()
	require.NoError(t, db.Model(model).Where("id = ?", id).
		UpdateColumns(map[string]any{"created_at": at, "modified_at": at}).Error)
}

// assertStoredTimes checks a returned entity's timestamps against the row.
func assertStoredTimes(t *testing.T, db *gorm.DB, model any, id string, gotCreated, gotModified *time.Time) {
	t.Helper()
	created, modified := storedTimes(t, db, model, id)
	require.NotNil(t, gotCreated, "created_at missing")
	require.NotNil(t, gotModified, "modified_at missing")
	assert.True(t, created.Equal(*gotCreated), "created_at: got %v, stored %v", *gotCreated, created)
	assert.True(t, modified.Equal(*gotModified), "modified_at: got %v, stored %v", *gotModified, modified)
	assert.Zero(t, gotCreated.Nanosecond()%1000, "created_at must carry microsecond resolution")
	assert.Zero(t, gotModified.Nanosecond()%1000, "modified_at must carry microsecond resolution")
}

var tsOld = time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

func TestGormRepositoryRepository_ReturnsStoredTimestamps(t *testing.T) {
	ctx := context.Background()
	newRepo := func(id uuid.UUID) *Repository {
		return &Repository{Id: &id, Uri: "https://github.com/example/repo"}
	}

	t.Run("Create", func(t *testing.T) {
		db, tm := setupSubResourceTimestampDB(t)
		store := NewGormRepositoryRepository(db, nil, nil)
		r := newRepo(uuid.New())
		require.NoError(t, store.Create(ctx, r, tm))
		assertStoredTimes(t, db, &models.Repository{}, r.Id.String(), r.CreatedAt, r.ModifiedAt)
	})

	t.Run("Update", func(t *testing.T) {
		db, tm := setupSubResourceTimestampDB(t)
		store := NewGormRepositoryRepository(db, nil, nil)
		id := uuid.New()
		require.NoError(t, store.Create(ctx, newRepo(id), tm))
		backdate(t, db, &models.Repository{}, id.String(), tsOld)

		update := newRepo(id) // PUT body: no timestamps
		require.NoError(t, store.Update(ctx, update, tm))
		_, modified := storedTimes(t, db, &models.Repository{}, id.String())
		require.True(t, modified.After(tsOld), "update must move modified_at")
		assertStoredTimes(t, db, &models.Repository{}, id.String(), update.CreatedAt, update.ModifiedAt)
	})

	t.Run("BulkCreate", func(t *testing.T) {
		db, tm := setupSubResourceTimestampDB(t)
		store := NewGormRepositoryRepository(db, nil, nil)
		batch := []Repository{*newRepo(uuid.New()), *newRepo(uuid.New())}
		require.NoError(t, store.BulkCreate(ctx, batch, tm))
		for _, r := range batch {
			assertStoredTimes(t, db, &models.Repository{}, r.Id.String(), r.CreatedAt, r.ModifiedAt)
		}
	})
}

func TestGormDocumentRepository_ReturnsStoredTimestamps(t *testing.T) {
	ctx := context.Background()
	newDoc := func(id uuid.UUID) *Document {
		return &Document{Id: &id, Name: "doc", Uri: "https://example.com/doc"}
	}

	t.Run("Create", func(t *testing.T) {
		db, tm := setupSubResourceTimestampDB(t)
		store := NewGormDocumentRepository(db, nil, nil)
		d := newDoc(uuid.New())
		require.NoError(t, store.Create(ctx, d, tm))
		assertStoredTimes(t, db, &models.Document{}, d.Id.String(), d.CreatedAt, d.ModifiedAt)
	})

	t.Run("Update", func(t *testing.T) {
		db, tm := setupSubResourceTimestampDB(t)
		store := NewGormDocumentRepository(db, nil, nil)
		id := uuid.New()
		require.NoError(t, store.Create(ctx, newDoc(id), tm))
		backdate(t, db, &models.Document{}, id.String(), tsOld)

		update := newDoc(id)
		require.NoError(t, store.Update(ctx, update, tm))
		_, modified := storedTimes(t, db, &models.Document{}, id.String())
		require.True(t, modified.After(tsOld), "update must move modified_at")
		assertStoredTimes(t, db, &models.Document{}, id.String(), update.CreatedAt, update.ModifiedAt)
	})

	t.Run("BulkCreate", func(t *testing.T) {
		db, tm := setupSubResourceTimestampDB(t)
		store := NewGormDocumentRepository(db, nil, nil)
		batch := []Document{*newDoc(uuid.New()), *newDoc(uuid.New())}
		require.NoError(t, store.BulkCreate(ctx, batch, tm))
		for _, d := range batch {
			assertStoredTimes(t, db, &models.Document{}, d.Id.String(), d.CreatedAt, d.ModifiedAt)
		}
	})
}

func TestGormAssetRepository_ReturnsStoredTimestamps(t *testing.T) {
	ctx := context.Background()
	newAsset := func(id uuid.UUID) *Asset {
		return &Asset{Id: &id, Name: "asset", Type: AssetTypeData}
	}

	t.Run("Create", func(t *testing.T) {
		db, tm := setupSubResourceTimestampDB(t)
		store := NewGormAssetRepository(db, nil, nil)
		a := newAsset(uuid.New())
		require.NoError(t, store.Create(ctx, a, tm))
		assertStoredTimes(t, db, &models.Asset{}, a.Id.String(), a.CreatedAt, a.ModifiedAt)
	})

	t.Run("Update", func(t *testing.T) {
		db, tm := setupSubResourceTimestampDB(t)
		store := NewGormAssetRepository(db, nil, nil)
		id := uuid.New()
		require.NoError(t, store.Create(ctx, newAsset(id), tm))
		backdate(t, db, &models.Asset{}, id.String(), tsOld)

		update := newAsset(id)
		require.NoError(t, store.Update(ctx, update, tm))
		_, modified := storedTimes(t, db, &models.Asset{}, id.String())
		require.True(t, modified.After(tsOld), "update must move modified_at")
		assertStoredTimes(t, db, &models.Asset{}, id.String(), update.CreatedAt, update.ModifiedAt)
	})

	t.Run("BulkCreate", func(t *testing.T) {
		db, tm := setupSubResourceTimestampDB(t)
		store := NewGormAssetRepository(db, nil, nil)
		batch := []Asset{*newAsset(uuid.New()), *newAsset(uuid.New())}
		require.NoError(t, store.BulkCreate(ctx, batch, tm))
		for _, a := range batch {
			assertStoredTimes(t, db, &models.Asset{}, a.Id.String(), a.CreatedAt, a.ModifiedAt)
		}
	})
}
