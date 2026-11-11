package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
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

	t.Run("Get", func(t *testing.T) {
		db, tm := setupSubResourceTimestampDB(t)
		store := NewGormRepositoryRepository(db, nil, nil)
		id := uuid.New()
		require.NoError(t, store.Create(ctx, newRepo(id), tm))
		got, err := store.Get(ctx, id.String())
		require.NoError(t, err)
		assertStoredTimes(t, db, &models.Repository{}, id.String(), got.CreatedAt, got.ModifiedAt)
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

// upperColumnNamer mimics gorm-oracle's naming: column names are uppercase,
// so field.DBName is "MODIFIED_AT" rather than "modified_at". SQLite folds
// identifier case, so the schema still migrates and queries still run.
type upperColumnNamer struct{ schema.NamingStrategy }

func (n upperColumnNamer) ColumnName(table, column string) string {
	return strings.ToUpper(n.NamingStrategy.ColumnName(table, column))
}

// An Oracle-only failure guard: with hooks enabled, a lowercase "modified_at"
// map key would not stop GORM's autoUpdateTime injector (it checks field.Name
// and field.DBName), so the UPDATE would assign the column twice and Oracle
// would reject it with ORA-00957 while PostgreSQL passed.
func TestGormAssetRepository_UpdateAssignsModifiedAtOnce(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy:                           upperColumnNamer{},
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Asset{}, &models.Metadata{}, &models.AliasCounter{}))

	var updateSQL []string
	require.NoError(t, db.Callback().Update().After("gorm:update").Register("test:capture_sql",
		func(tx *gorm.DB) { updateSQL = append(updateSQL, tx.Statement.SQL.String()) }))

	store := NewGormAssetRepository(db, nil, nil)
	tm := uuid.New().String()
	id := uuid.New()
	require.NoError(t, store.Create(ctx, &Asset{Id: &id, Name: "asset", Type: AssetTypeData}, tm))
	updateSQL = nil
	require.NoError(t, store.Update(ctx, &Asset{Id: &id, Name: "renamed", Type: AssetTypeData}, tm))

	var assetUpdates int
	for _, q := range updateSQL {
		if !strings.Contains(strings.ToUpper(q), "UPDATE `ASSETS`") && !strings.Contains(strings.ToUpper(q), `UPDATE "ASSETS"`) {
			continue
		}
		assetUpdates++
		set := strings.ToUpper(q)
		if i := strings.Index(set, " WHERE "); i >= 0 {
			set = set[:i]
		}
		assert.Equal(t, 1, strings.Count(set, "MODIFIED_AT"), "modified_at must be assigned exactly once: %s", q)
	}
	require.Equal(t, 1, assetUpdates, "expected one UPDATE on assets, captured: %v", updateSQL)
}
