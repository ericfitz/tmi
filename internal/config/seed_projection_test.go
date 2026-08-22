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

// TestClassificationFor_SeededKeysAreNotInternal is the regression test for
// #809 round 1: a SettingDef.Class populated correctly is not enough on its
// own, because api/config_handlers.go's GET/DELETE /admin/settings/{key}
// checks config.ClassificationFor(key).Visibility, which resolves through
// classification_registry.go's exactClassifications/prefixClassifications —
// a separate table from the SettingDef registry until Phase E converges
// them. A key can pass every SettingDef-level assertion above and still
// 404 at the actual endpoint if it has no exactClassifications entry. This
// test ties every seeded key to that real runtime resolution path, so it
// would have caught rate_limit.* still 404ing after round 1's declaration
// went in.
func TestClassificationFor_SeededKeysAreNotInternal(t *testing.T) {
	for _, d := range SeedableOperationalDefs() {
		cls := ClassificationFor(d.Key)
		assert.NotEqual(t, VisibilityInternal, cls.Visibility,
			"%s is seeded into system_settings and shown by GET /admin/settings, "+
				"so ClassificationFor must not resolve it to VisibilityInternal — "+
				"that is what makes GET/DELETE /admin/settings/{key} return 404 (#809)", d.Key)
		assert.NotEqual(t, CategoryUnclassified, cls.Category, "%s must be classified", d.Key)
	}
}
