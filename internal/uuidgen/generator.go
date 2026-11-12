package uuidgen

import (
	"fmt"

	"github.com/google/uuid"
)

// EntityType represents the different entity types in the system
type EntityType string

const (
	EntityTypeThreat   EntityType = "threat"
	EntityTypeMetadata EntityType = "metadata"
)

// NewForEntity generates a UUID appropriate for the given entity type.
// High-volume entities (threats, metadata) use UUIDv7 for better index locality.
// All other entities use UUIDv4 for compatibility and distribution.
func NewForEntity(entityType EntityType) (uuid.UUID, error) {
	switch entityType {
	case EntityTypeThreat, EntityTypeMetadata:
		// Use UUIDv7 for high-volume entities that benefit from sequential ordering
		return uuid.NewV7()
	default:
		// Use UUIDv4 for all other entities
		return uuid.NewRandom()
	}
}

// NewForEntityString is a convenience wrapper that accepts string entity types
func NewForEntityString(entityType string) (uuid.UUID, error) {
	return NewForEntity(EntityType(entityType))
}

// MustNewForEntity is like NewForEntity but panics on error.
// Should only be used in situations where UUID generation failure is unrecoverable.
func MustNewForEntity(entityType EntityType) uuid.UUID {
	id, err := NewForEntity(entityType)
	if err != nil {
		panic(fmt.Sprintf("failed to generate UUID for entity type %s: %v", entityType, err))
	}
	return id
}

// MustNewForEntityString is like NewForEntityString but panics on error
func MustNewForEntityString(entityType string) uuid.UUID {
	id, err := NewForEntityString(entityType)
	if err != nil {
		panic(fmt.Sprintf("failed to generate UUID for entity type %s: %v", entityType, err))
	}
	return id
}

// NewV4 generates a UUIDv4 for entities that should use random UUIDs
func NewV4() (uuid.UUID, error) {
	return uuid.NewRandom()
}

// MustNewV4 is like NewV4 but panics on error
func MustNewV4() uuid.UUID {
	id, err := NewV4()
	if err != nil {
		panic(fmt.Sprintf("failed to generate UUIDv4: %v", err))
	}
	return id
}

// NewV7 generates a UUIDv7 for entities that benefit from time-ordered UUIDs
func NewV7() (uuid.UUID, error) {
	return uuid.NewV7()
}

// MustNewV7 is like NewV7 but panics on error
func MustNewV7() uuid.UUID {
	id, err := NewV7()
	if err != nil {
		panic(fmt.Sprintf("failed to generate UUIDv7: %v", err))
	}
	return id
}
