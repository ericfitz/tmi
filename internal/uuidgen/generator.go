package uuidgen

import (
	"fmt"

	"github.com/google/uuid"
)

// EntityType represents the different entity types in the system
// SEM@54ce780187c82c328300f63352fb57ca67a12d0c: string type discriminating entity categories for UUID version selection (pure)
type EntityType string

const (
	EntityTypeThreat   EntityType = "threat"
	EntityTypeMetadata EntityType = "metadata"
)

// NewForEntity generates a UUID appropriate for the given entity type.
// High-volume entities (threats, metadata) use UUIDv7 for better index locality.
// All other entities use UUIDv4 for compatibility and distribution.
// SEM@54ce780187c82c328300f63352fb57ca67a12d0c: generate a UUID selecting v7 for high-volume entities and v4 for all others (pure)
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
// SEM@54ce780187c82c328300f63352fb57ca67a12d0c: generate a UUID for a string-typed entity name, delegating to NewForEntity (pure)
func NewForEntityString(entityType string) (uuid.UUID, error) {
	return NewForEntity(EntityType(entityType))
}

// MustNewForEntity is like NewForEntity but panics on error.
// Should only be used in situations where UUID generation failure is unrecoverable.
// SEM@54ce780187c82c328300f63352fb57ca67a12d0c: generate a UUID for an entity type, panicking on failure (pure)
func MustNewForEntity(entityType EntityType) uuid.UUID {
	id, err := NewForEntity(entityType)
	if err != nil {
		panic(fmt.Sprintf("failed to generate UUID for entity type %s: %v", entityType, err))
	}
	return id
}

// MustNewForEntityString is like NewForEntityString but panics on error
// SEM@54ce780187c82c328300f63352fb57ca67a12d0c: generate a UUID for a string-typed entity name, panicking on failure (pure)
func MustNewForEntityString(entityType string) uuid.UUID {
	id, err := NewForEntityString(entityType)
	if err != nil {
		panic(fmt.Sprintf("failed to generate UUID for entity type %s: %v", entityType, err))
	}
	return id
}

// NewV4 generates a UUIDv4 for entities that should use random UUIDs
// SEM@54ce780187c82c328300f63352fb57ca67a12d0c: generate a random UUIDv4 (pure)
func NewV4() (uuid.UUID, error) {
	return uuid.NewRandom()
}

// MustNewV4 is like NewV4 but panics on error
// SEM@54ce780187c82c328300f63352fb57ca67a12d0c: generate a random UUIDv4, panicking on failure (pure)
func MustNewV4() uuid.UUID {
	id, err := NewV4()
	if err != nil {
		panic(fmt.Sprintf("failed to generate UUIDv4: %v", err))
	}
	return id
}

// NewV7 generates a UUIDv7 for entities that benefit from time-ordered UUIDs
// SEM@54ce780187c82c328300f63352fb57ca67a12d0c: generate a time-ordered UUIDv7 (pure)
func NewV7() (uuid.UUID, error) {
	return uuid.NewV7()
}

// MustNewV7 is like NewV7 but panics on error
// SEM@54ce780187c82c328300f63352fb57ca67a12d0c: generate a time-ordered UUIDv7, panicking on failure (pure)
func MustNewV7() uuid.UUID {
	id, err := NewV7()
	if err != nil {
		panic(fmt.Sprintf("failed to generate UUIDv7: %v", err))
	}
	return id
}
