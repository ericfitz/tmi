package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEntityTypeToIndexType(t *testing.T) {
	tests := []struct {
		entityType string
		expected   string
	}{
		{"asset", IndexTypeText},
		{"threat", IndexTypeText},
		{"diagram", IndexTypeText},
		{"document", IndexTypeText},
		{"note", IndexTypeText},
		{"repository", IndexTypeCode},
	}

	for _, tt := range tests {
		t.Run(tt.entityType, func(t *testing.T) {
			assert.Equal(t, tt.expected, EntityTypeToIndexType(tt.entityType))
		})
	}
}

func TestIndexTypeConstants(t *testing.T) {
	assert.Equal(t, "text", IndexTypeText)
	assert.Equal(t, "code", IndexTypeCode)
}
