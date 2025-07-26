package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEntity is a sample entity for testing
type TestEntity struct {
	ID         string
	Name       string
	CreatedAt  time.Time
	ModifiedAt time.Time
}

// SetCreatedAt implements WithTimestamps
func (e *TestEntity) SetCreatedAt(t time.Time) {
	e.CreatedAt = t
}

// SetModifiedAt implements WithTimestamps
func (e *TestEntity) SetModifiedAt(t time.Time) {
	e.ModifiedAt = t
}

// TestStore_CRUD tests basic CRUD operations on the store
func TestStore_CRUD(t *testing.T) {
	// Create a new store
	store := NewDataStore[TestEntity]()

	// Test create
	entity := TestEntity{Name: "Test Entity"}

	// ID setter function
	idSetter := func(e TestEntity, id string) TestEntity {
		e.ID = id
		return e
	}

	created, err := store.Create(entity, idSetter)
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, "Test Entity", created.Name)

	// Test get
	retrieved, err := store.Get(created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, retrieved.ID)
	assert.Equal(t, created.Name, retrieved.Name)

	// Test list
	items := store.List(0, 10, nil)
	assert.Len(t, items, 1)

	// Test update
	retrieved.Name = "Updated Entity"
	err = store.Update(retrieved.ID, retrieved)
	require.NoError(t, err)

	updated, err := store.Get(retrieved.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Entity", updated.Name)

	// Test delete
	err = store.Delete(updated.ID)
	require.NoError(t, err)

	_, err = store.Get(updated.ID)
	assert.Error(t, err)
	assert.Empty(t, store.List(0, 10, nil))
}

// TestStore_Filtering tests filtering functionality
func TestStore_Filtering(t *testing.T) {
	store := NewDataStore[TestEntity]()

	idSetter := func(e TestEntity, id string) TestEntity {
		e.ID = id
		return e
	}

	// Create test data
	entities := []TestEntity{
		{Name: "Alpha"},
		{Name: "Beta"},
		{Name: "Alpha 2"},
	}

	for _, e := range entities {
		_, err := store.Create(e, idSetter)
		require.NoError(t, err)
	}

	// Test filtering
	filtered := store.List(0, 10, func(e TestEntity) bool {
		return e.Name == "Alpha" || e.Name == "Alpha 2"
	})

	assert.Len(t, filtered, 2)
	names := map[string]bool{}
	for _, e := range filtered {
		names[e.Name] = true
	}
	assert.True(t, names["Alpha"])
	assert.True(t, names["Alpha 2"])
	assert.False(t, names["Beta"])

	// Test pagination
	all := store.List(0, 10, nil)
	assert.Len(t, all, 3)

	page1 := store.List(0, 2, nil)
	assert.Len(t, page1, 2)

	page2 := store.List(2, 2, nil)
	assert.Len(t, page2, 1)

	// Test count
	assert.Equal(t, 3, store.Count())
}

// TestUpdateTimestamps tests the timestamp update functionality
func TestUpdateTimestamps(t *testing.T) {
	entity := &TestEntity{Name: "Test"}

	// Test new entity
	updated := UpdateTimestamps(entity, true)
	assert.False(t, updated.CreatedAt.IsZero())
	assert.False(t, updated.ModifiedAt.IsZero())
	assert.Equal(t, updated.CreatedAt, updated.ModifiedAt)

	createdAt := updated.CreatedAt

	// Wait a moment so timestamps will be different
	time.Sleep(10 * time.Millisecond)

	// Test existing entity
	updated = UpdateTimestamps(entity, false)
	assert.Equal(t, createdAt, updated.CreatedAt)     // CreatedAt unchanged
	assert.NotEqual(t, createdAt, updated.ModifiedAt) // ModifiedAt updated
	assert.False(t, updated.ModifiedAt.IsZero())
}
