package uuidgen

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewForEntity(t *testing.T) {
	tests := []struct {
		name         string
		entityType   EntityType
		expectedFunc func(uuid.UUID) bool
		wantErr      bool
	}{
		{
			name:       "threat uses UUIDv7",
			entityType: EntityTypeThreat,
			expectedFunc: func(id uuid.UUID) bool {
				return id.Version() == 7
			},
		},
		{
			name:       "metadata uses UUIDv7",
			entityType: EntityTypeMetadata,
			expectedFunc: func(id uuid.UUID) bool {
				return id.Version() == 7
			},
		},
		{
			name:       "other entity uses UUIDv4",
			entityType: EntityType("other"),
			expectedFunc: func(id uuid.UUID) bool {
				return id.Version() == 4
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := NewForEntity(tt.entityType)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotEqual(t, uuid.Nil, id)
			assert.True(t, tt.expectedFunc(id), "UUID version check failed for %s", tt.entityType)
		})
	}
}

func TestNewForEntityString(t *testing.T) {
	tests := []struct {
		name         string
		entityType   string
		expectedFunc func(uuid.UUID) bool
	}{
		{
			name:       "threat string uses UUIDv7",
			entityType: "threat",
			expectedFunc: func(id uuid.UUID) bool {
				return id.Version() == 7
			},
		},
		{
			name:       "metadata string uses UUIDv7",
			entityType: "metadata",
			expectedFunc: func(id uuid.UUID) bool {
				return id.Version() == 7
			},
		},
		{
			name:       "other string uses UUIDv4",
			entityType: "document",
			expectedFunc: func(id uuid.UUID) bool {
				return id.Version() == 4
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := NewForEntityString(tt.entityType)
			require.NoError(t, err)
			assert.NotEqual(t, uuid.Nil, id)
			assert.True(t, tt.expectedFunc(id), "UUID version check failed for %s", tt.entityType)
		})
	}
}

func TestMustNewForEntity(t *testing.T) {
	// Test normal operation
	id := MustNewForEntity(EntityTypeThreat)
	assert.NotEqual(t, uuid.Nil, id)
	assert.Equal(t, uuid.Version(7), id.Version())

	// Test that it doesn't panic on valid input
	assert.NotPanics(t, func() {
		MustNewForEntity(EntityTypeMetadata)
	})
}

func TestMustNewForEntityString(t *testing.T) {
	// Test normal operation
	id := MustNewForEntityString("threat")
	assert.NotEqual(t, uuid.Nil, id)
	assert.Equal(t, uuid.Version(7), id.Version())

	// Test that it doesn't panic on valid input
	assert.NotPanics(t, func() {
		MustNewForEntityString("metadata")
	})
}

func TestNewV4(t *testing.T) {
	id, err := NewV4()
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, id)
	assert.Equal(t, uuid.Version(4), id.Version())
}

func TestMustNewV4(t *testing.T) {
	assert.NotPanics(t, func() {
		id := MustNewV4()
		assert.NotEqual(t, uuid.Nil, id)
		assert.Equal(t, uuid.Version(4), id.Version())
	})
}

func TestNewV7(t *testing.T) {
	id, err := NewV7()
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, id)
	assert.Equal(t, uuid.Version(7), id.Version())
}

func TestMustNewV7(t *testing.T) {
	assert.NotPanics(t, func() {
		id := MustNewV7()
		assert.NotEqual(t, uuid.Nil, id)
		assert.Equal(t, uuid.Version(7), id.Version())
	})
}

func TestUUIDv7TimeOrdering(t *testing.T) {
	// Generate multiple UUIDv7s and verify they are in chronological order
	var ids []uuid.UUID
	for range 10 {
		id, err := NewV7()
		require.NoError(t, err)
		ids = append(ids, id)
	}

	// UUIDv7 should be lexicographically sortable due to time-based prefix
	for i := 1; i < len(ids); i++ {
		// Convert to strings and compare lexicographically
		prev := ids[i-1].String()
		curr := ids[i].String()
		assert.True(t, prev <= curr, "UUIDv7 should be chronologically ordered: %s should be <= %s", prev, curr)
	}
}

func TestEntityTypeConstants(t *testing.T) {
	assert.Equal(t, EntityType("threat"), EntityTypeThreat)
	assert.Equal(t, EntityType("metadata"), EntityTypeMetadata)
}

func BenchmarkNewForEntity(b *testing.B) {
	b.Run("UUIDv7_for_threats", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = NewForEntity(EntityTypeThreat)
		}
	})

	b.Run("UUIDv4_for_others", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = NewForEntity(EntityType("other"))
		}
	})
}
