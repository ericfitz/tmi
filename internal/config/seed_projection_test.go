package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedableOperationalDefs_IncludesPreviouslyUnclassifiedKeys(t *testing.T) {
	// #809: these two were seeded into system_settings but had no
	// classification entry, so they resolved to VisibilityInternal and 404'd
	// on GET/DELETE while appearing in the LIST response.
	for _, key := range []string{
		"rate_limit.requests_per_minute",
		"rate_limit.requests_per_hour",
	} {
		d, ok := DefFor(key)
		require.True(t, ok, "%s must be declared", key)
		assert.Equal(t, CategoryOperational, d.Class.Category)
		assert.Equal(t, VisibilityAdminOnly, d.Class.Visibility,
			"%s is not consumed by tmi-ux, so admin-only rather than public", key)
		assert.False(t, d.Transitional, "%s has no config path", key)
	}
}

func TestSeedableOperationalDefs_AreSortedAndNonEmpty(t *testing.T) {
	defs := SeedableOperationalDefs()
	require.NotEmpty(t, defs)
	for i := 1; i < len(defs); i++ {
		assert.Less(t, defs[i-1].Key, defs[i].Key, "SeedableOperationalDefs must be sorted by key")
	}
	for _, d := range defs {
		assert.Equal(t, CategoryOperational, d.Class.Category)
		assert.NotEmpty(t, d.Default)
	}
}
