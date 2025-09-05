package api

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInMemoryMetadataStore tests the in-memory metadata store implementation
func TestInMemoryMetadataStore(t *testing.T) {
	store := NewInMemoryMetadataStore()
	ctx := context.Background()

	entityType := "threat_model"
	entityID := uuid.New().String()

	t.Run("Create and Get", func(t *testing.T) {
		metadata := &Metadata{
			Key:   "test-key",
			Value: "test-value",
		}

		err := store.Create(ctx, entityType, entityID, metadata)
		require.NoError(t, err)

		retrieved, err := store.Get(ctx, entityType, entityID, "test-key")
		require.NoError(t, err)
		assert.Equal(t, metadata.Key, retrieved.Key)
		assert.Equal(t, metadata.Value, retrieved.Value)
	})

	t.Run("Get non-existent metadata", func(t *testing.T) {
		_, err := store.Get(ctx, entityType, entityID, "non-existent-key")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Get from non-existent entity", func(t *testing.T) {
		_, err := store.Get(ctx, "non-existent-type", "non-existent-id", "test-key")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Update", func(t *testing.T) {
		// First create
		metadata := &Metadata{
			Key:   "update-key",
			Value: "original-value",
		}
		err := store.Create(ctx, entityType, entityID, metadata)
		require.NoError(t, err)

		// Then update
		metadata.Value = "updated-value"
		err = store.Update(ctx, entityType, entityID, metadata)
		require.NoError(t, err)

		// Verify update
		retrieved, err := store.Get(ctx, entityType, entityID, "update-key")
		require.NoError(t, err)
		assert.Equal(t, "updated-value", retrieved.Value)
	})

	t.Run("Update non-existent metadata", func(t *testing.T) {
		metadata := &Metadata{
			Key:   "non-existent-key",
			Value: "test-value",
		}

		err := store.Update(ctx, entityType, entityID, metadata)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Delete", func(t *testing.T) {
		// First create
		metadata := &Metadata{
			Key:   "delete-key",
			Value: "delete-value",
		}
		err := store.Create(ctx, entityType, entityID, metadata)
		require.NoError(t, err)

		// Then delete
		err = store.Delete(ctx, entityType, entityID, "delete-key")
		require.NoError(t, err)

		// Verify deletion
		_, err = store.Get(ctx, entityType, entityID, "delete-key")
		assert.Error(t, err)
	})

	t.Run("Delete non-existent metadata", func(t *testing.T) {
		err := store.Delete(ctx, entityType, entityID, "non-existent-key")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("List metadata", func(t *testing.T) {
		listEntityID := uuid.New().String()

		// Create test metadata
		metadata1 := &Metadata{Key: "list-key-1", Value: "list-value-1"}
		metadata2 := &Metadata{Key: "list-key-2", Value: "list-value-2"}
		metadata3 := &Metadata{Key: "list-key-3", Value: "list-value-3"}

		err := store.Create(ctx, entityType, listEntityID, metadata1)
		require.NoError(t, err)
		err = store.Create(ctx, entityType, listEntityID, metadata2)
		require.NoError(t, err)
		err = store.Create(ctx, entityType, listEntityID, metadata3)
		require.NoError(t, err)

		// Test list
		retrieved, err := store.List(ctx, entityType, listEntityID)
		require.NoError(t, err)
		assert.Len(t, retrieved, 3)

		// Verify all metadata are present
		keys := make(map[string]string)
		for _, meta := range retrieved {
			keys[meta.Key] = meta.Value
		}
		assert.Equal(t, "list-value-1", keys["list-key-1"])
		assert.Equal(t, "list-value-2", keys["list-key-2"])
		assert.Equal(t, "list-value-3", keys["list-key-3"])
	})

	t.Run("List empty entity", func(t *testing.T) {
		nonExistentID := uuid.New().String()
		retrieved, err := store.List(ctx, entityType, nonExistentID)
		require.NoError(t, err)
		assert.Empty(t, retrieved)
	})

	t.Run("Post", func(t *testing.T) {
		metadata := &Metadata{
			Key:   "post-key",
			Value: "post-value",
		}

		err := store.Post(ctx, entityType, entityID, metadata)
		require.NoError(t, err)

		// Verify it was created
		retrieved, err := store.Get(ctx, entityType, entityID, "post-key")
		require.NoError(t, err)
		assert.Equal(t, metadata.Value, retrieved.Value)
	})

	t.Run("BulkCreate", func(t *testing.T) {
		bulkEntityID := uuid.New().String()
		metadata := []Metadata{
			{Key: "bulk-1", Value: "bulk-value-1"},
			{Key: "bulk-2", Value: "bulk-value-2"},
			{Key: "bulk-3", Value: "bulk-value-3"},
		}

		err := store.BulkCreate(ctx, entityType, bulkEntityID, metadata)
		require.NoError(t, err)

		// Verify all were created
		for _, meta := range metadata {
			retrieved, err := store.Get(ctx, entityType, bulkEntityID, meta.Key)
			require.NoError(t, err)
			assert.Equal(t, meta.Value, retrieved.Value)
		}
	})

	t.Run("BulkCreate empty slice", func(t *testing.T) {
		err := store.BulkCreate(ctx, entityType, entityID, []Metadata{})
		require.NoError(t, err) // Should not error
	})

	t.Run("BulkUpdate", func(t *testing.T) {
		updateEntityID := uuid.New().String()

		// First create some metadata
		initial := []Metadata{
			{Key: "update-1", Value: "original-1"},
			{Key: "update-2", Value: "original-2"},
		}
		err := store.BulkCreate(ctx, entityType, updateEntityID, initial)
		require.NoError(t, err)

		// Then bulk update
		updates := []Metadata{
			{Key: "update-1", Value: "updated-1"},
			{Key: "update-2", Value: "updated-2"},
		}
		err = store.BulkUpdate(ctx, entityType, updateEntityID, updates)
		require.NoError(t, err)

		// Verify updates
		for _, meta := range updates {
			retrieved, err := store.Get(ctx, entityType, updateEntityID, meta.Key)
			require.NoError(t, err)
			assert.Equal(t, meta.Value, retrieved.Value)
		}
	})

	t.Run("BulkDelete", func(t *testing.T) {
		deleteEntityID := uuid.New().String()

		// First create some metadata
		metadata := []Metadata{
			{Key: "delete-1", Value: "delete-value-1"},
			{Key: "delete-2", Value: "delete-value-2"},
			{Key: "delete-3", Value: "delete-value-3"},
		}
		err := store.BulkCreate(ctx, entityType, deleteEntityID, metadata)
		require.NoError(t, err)

		// Then bulk delete some keys
		keysToDelete := []string{"delete-1", "delete-3"}
		err = store.BulkDelete(ctx, entityType, deleteEntityID, keysToDelete)
		require.NoError(t, err)

		// Verify deletions
		_, err = store.Get(ctx, entityType, deleteEntityID, "delete-1")
		assert.Error(t, err)
		_, err = store.Get(ctx, entityType, deleteEntityID, "delete-3")
		assert.Error(t, err)

		// Verify remaining key still exists
		retrieved, err := store.Get(ctx, entityType, deleteEntityID, "delete-2")
		require.NoError(t, err)
		assert.Equal(t, "delete-value-2", retrieved.Value)
	})

	t.Run("BulkDelete empty slice", func(t *testing.T) {
		err := store.BulkDelete(ctx, entityType, entityID, []string{})
		require.NoError(t, err) // Should not error
	})

	t.Run("GetByKey", func(t *testing.T) {
		// Create metadata with same key across different entities
		entity1ID := uuid.New().String()
		entity2ID := uuid.New().String()

		metadata1 := &Metadata{Key: "shared-key", Value: "value-1"}
		metadata2 := &Metadata{Key: "shared-key", Value: "value-2"}
		unique := &Metadata{Key: "unique-key", Value: "unique-value"}

		err := store.Create(ctx, entityType, entity1ID, metadata1)
		require.NoError(t, err)
		err = store.Create(ctx, entityType, entity2ID, metadata2)
		require.NoError(t, err)
		err = store.Create(ctx, entityType, entity1ID, unique)
		require.NoError(t, err)

		// Test GetByKey for shared key
		results, err := store.GetByKey(ctx, "shared-key")
		require.NoError(t, err)
		assert.Len(t, results, 2)

		values := make(map[string]bool)
		for _, meta := range results {
			assert.Equal(t, "shared-key", meta.Key)
			values[meta.Value] = true
		}
		assert.True(t, values["value-1"])
		assert.True(t, values["value-2"])

		// Test GetByKey for unique key
		results, err = store.GetByKey(ctx, "unique-key")
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "unique-value", results[0].Value)

		// Test GetByKey for non-existent key
		results, err = store.GetByKey(ctx, "non-existent")
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("ListKeys", func(t *testing.T) {
		keysEntityID := uuid.New().String()

		// Create metadata with various keys
		metadata := []Metadata{
			{Key: "alpha", Value: "value-alpha"},
			{Key: "beta", Value: "value-beta"},
			{Key: "gamma", Value: "value-gamma"},
		}
		err := store.BulkCreate(ctx, entityType, keysEntityID, metadata)
		require.NoError(t, err)

		// Test ListKeys
		keys, err := store.ListKeys(ctx, entityType, keysEntityID)
		require.NoError(t, err)
		assert.Len(t, keys, 3)

		keySet := make(map[string]bool)
		for _, key := range keys {
			keySet[key] = true
		}
		assert.True(t, keySet["alpha"])
		assert.True(t, keySet["beta"])
		assert.True(t, keySet["gamma"])
	})

	t.Run("ListKeys empty entity", func(t *testing.T) {
		emptyEntityID := uuid.New().String()
		keys, err := store.ListKeys(ctx, entityType, emptyEntityID)
		require.NoError(t, err)
		assert.Empty(t, keys)
	})

	t.Run("Cache operations are no-ops", func(t *testing.T) {
		err := store.InvalidateCache(ctx, entityType, entityID)
		assert.NoError(t, err)

		err = store.WarmCache(ctx, entityType, entityID)
		assert.NoError(t, err)
	})

	t.Run("Multiple entity types", func(t *testing.T) {
		// Test that metadata is properly isolated by entity type
		entityID := uuid.New().String()

		threat := &Metadata{Key: "test", Value: "threat-value"}
		document := &Metadata{Key: "test", Value: "document-value"}

		err := store.Create(ctx, "threat", entityID, threat)
		require.NoError(t, err)
		err = store.Create(ctx, "document", entityID, document)
		require.NoError(t, err)

		// Verify isolation
		threatMeta, err := store.Get(ctx, "threat", entityID, "test")
		require.NoError(t, err)
		assert.Equal(t, "threat-value", threatMeta.Value)

		docMeta, err := store.Get(ctx, "document", entityID, "test")
		require.NoError(t, err)
		assert.Equal(t, "document-value", docMeta.Value)

		// Verify they don't interfere
		threatList, err := store.List(ctx, "threat", entityID)
		require.NoError(t, err)
		assert.Len(t, threatList, 1)

		docList, err := store.List(ctx, "document", entityID)
		require.NoError(t, err)
		assert.Len(t, docList, 1)
	})
}
