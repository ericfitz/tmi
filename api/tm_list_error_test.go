package api

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// A failed list query must surface as an error, not as an empty page (#909).
// SEM@c91b16ea67b50cc273cb925b803aeb2cac07d517: verify a failing threat model list query returns an error instead of an empty page (reads DB)
func TestListWithCountsReturnsQueryError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// No AutoMigrate: threat_models does not exist, so the SELECT fails.
	items, total, err := NewGormThreatModelStore(db).ListWithCounts(0, 10, nil, nil)
	require.Error(t, err)
	require.Empty(t, items)
	require.Zero(t, total)
}
