package api

import (
	"testing"
	"time"
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
	t.Skip("Generic in-memory store removed - use database tests instead")
}

func TestStore_Filtering(t *testing.T) {
	t.Skip("Generic in-memory store removed - use database tests instead")
}

func TestUpdateTimestamps(t *testing.T) {
	t.Skip("Generic in-memory store removed - use database tests instead")
}
