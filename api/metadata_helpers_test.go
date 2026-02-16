package api

import (
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Metadata{}))
	return db
}

func TestLoadEntityMetadata(t *testing.T) {
	db := setupTestDB(t)

	// Insert test data directly
	testEntries := []models.Metadata{
		{ID: "1", EntityType: "asset", EntityID: "abc", Key: "env", Value: "prod"},
		{ID: "2", EntityType: "asset", EntityID: "abc", Key: "team", Value: "platform"},
		{ID: "3", EntityType: "asset", EntityID: "other", Key: "env", Value: "staging"},
	}
	for _, e := range testEntries {
		require.NoError(t, db.Create(&e).Error)
	}

	t.Run("loads metadata for matching entity", func(t *testing.T) {
		metadata, err := loadEntityMetadata(db, "asset", "abc")
		require.NoError(t, err)
		assert.Len(t, metadata, 2)
		assert.Equal(t, "env", metadata[0].Key)
		assert.Equal(t, "prod", metadata[0].Value)
		assert.Equal(t, "team", metadata[1].Key)
	})

	t.Run("returns empty for non-existent entity", func(t *testing.T) {
		metadata, err := loadEntityMetadata(db, "asset", "nonexistent")
		require.NoError(t, err)
		assert.Empty(t, metadata)
	})

	t.Run("filters by entity type", func(t *testing.T) {
		metadata, err := loadEntityMetadata(db, "document", "abc")
		require.NoError(t, err)
		assert.Empty(t, metadata)
	})

	t.Run("results ordered by key ASC", func(t *testing.T) {
		metadata, err := loadEntityMetadata(db, "asset", "abc")
		require.NoError(t, err)
		require.Len(t, metadata, 2)
		assert.True(t, metadata[0].Key < metadata[1].Key, "keys should be in ascending order")
	})
}

func TestSaveEntityMetadata(t *testing.T) {
	t.Run("saves metadata entries", func(t *testing.T) {
		db := setupTestDB(t)

		metadata := []Metadata{
			{Key: "env", Value: "prod"},
			{Key: "team", Value: "platform"},
		}

		err := saveEntityMetadata(db, "asset", "abc", metadata)
		require.NoError(t, err)

		loaded, err := loadEntityMetadata(db, "asset", "abc")
		require.NoError(t, err)
		assert.Len(t, loaded, 2)
	})

	t.Run("empty metadata is no-op", func(t *testing.T) {
		db := setupTestDB(t)

		err := saveEntityMetadata(db, "asset", "abc", []Metadata{})
		require.NoError(t, err)

		err = saveEntityMetadata(db, "asset", "abc", nil)
		require.NoError(t, err)
	})

	t.Run("upserts on conflict", func(t *testing.T) {
		db := setupTestDB(t)

		// Save initial
		err := saveEntityMetadata(db, "asset", "abc", []Metadata{
			{Key: "env", Value: "staging"},
		})
		require.NoError(t, err)

		// Save again with updated value
		err = saveEntityMetadata(db, "asset", "abc", []Metadata{
			{Key: "env", Value: "prod"},
		})
		require.NoError(t, err)

		loaded, err := loadEntityMetadata(db, "asset", "abc")
		require.NoError(t, err)
		assert.Len(t, loaded, 1)
		assert.Equal(t, "prod", loaded[0].Value)
	})
}

func TestDeleteAndSaveEntityMetadata(t *testing.T) {
	t.Run("replaces all metadata", func(t *testing.T) {
		db := setupTestDB(t)

		// Save initial metadata
		err := saveEntityMetadata(db, "asset", "abc", []Metadata{
			{Key: "env", Value: "staging"},
			{Key: "team", Value: "old-team"},
		})
		require.NoError(t, err)

		// Replace with new metadata
		err = deleteAndSaveEntityMetadata(db, "asset", "abc", []Metadata{
			{Key: "env", Value: "prod"},
			{Key: "region", Value: "us-east-1"},
		})
		require.NoError(t, err)

		loaded, err := loadEntityMetadata(db, "asset", "abc")
		require.NoError(t, err)
		assert.Len(t, loaded, 2)

		// Old "team" key should be gone
		keys := make(map[string]string)
		for _, m := range loaded {
			keys[m.Key] = m.Value
		}
		assert.Equal(t, "prod", keys["env"])
		assert.Equal(t, "us-east-1", keys["region"])
		_, hasTeam := keys["team"]
		assert.False(t, hasTeam, "old 'team' key should have been deleted")
	})

	t.Run("delete with empty metadata clears all", func(t *testing.T) {
		db := setupTestDB(t)

		err := saveEntityMetadata(db, "asset", "abc", []Metadata{
			{Key: "env", Value: "prod"},
		})
		require.NoError(t, err)

		err = deleteAndSaveEntityMetadata(db, "asset", "abc", []Metadata{})
		require.NoError(t, err)

		loaded, err := loadEntityMetadata(db, "asset", "abc")
		require.NoError(t, err)
		assert.Empty(t, loaded)
	})

	t.Run("does not affect other entities", func(t *testing.T) {
		db := setupTestDB(t)

		err := saveEntityMetadata(db, "asset", "abc", []Metadata{{Key: "env", Value: "prod"}})
		require.NoError(t, err)
		err = saveEntityMetadata(db, "asset", "xyz", []Metadata{{Key: "env", Value: "staging"}})
		require.NoError(t, err)

		err = deleteAndSaveEntityMetadata(db, "asset", "abc", []Metadata{{Key: "env", Value: "dev"}})
		require.NoError(t, err)

		// xyz should be untouched
		loaded, err := loadEntityMetadata(db, "asset", "xyz")
		require.NoError(t, err)
		assert.Len(t, loaded, 1)
		assert.Equal(t, "staging", loaded[0].Value)
	})
}
