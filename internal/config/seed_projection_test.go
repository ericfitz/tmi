package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// retiredRateLimitKeys are the two dead keys #813 removed. They were seeded
// into system_settings but read by nothing; real rate limiting runs off
// server.disable_rate_limiting / server.ratelimit_public_rpm.
var retiredRateLimitKeys = []string{
	"rate_limit.requests_per_minute",
	"rate_limit.requests_per_hour",
}

// TestRetiredRateLimitKeys_AreGone is the #813 guardrail. Re-declaring either
// key -- as a SettingDef, or as an exactClassifications entry, or by flipping
// Seeded back on -- fails this test by name.
//
// Both halves matter. A SettingDef alone puts the key back in the seed
// projection, so every new database gets a row for a setting nothing reads. A
// classification entry alone makes ClassificationFor resolve it, which is what
// #812 added to clear the 404 and what #813 is now removing. Either one
// reintroduces a key the product no longer has.
//
// Watched to fail: restoring either def in setting_defs_misc.go or either
// entry in classification_registry.go trips the corresponding assertion.
// SEM@24731679561a852b21b37271caffd9a597080f0b: validate retired rate-limit keys are undeclared and unseeded
func TestRetiredRateLimitKeys_AreGone(t *testing.T) {
	for _, key := range retiredRateLimitKeys {
		_, declared := DefFor(key)
		assert.False(t, declared,
			"%s was retired by #813 and must not be re-declared as a SettingDef — "+
				"it is read by no handler, so a declaration only re-seeds a row "+
				"that does nothing", key)

		cls := ClassificationFor(key)
		assert.Equal(t, CategoryUnclassified, cls.Category,
			"%s was retired by #813 and must not have a classification entry", key)
	}

	for _, d := range SeedableOperationalDefs() {
		for _, key := range retiredRateLimitKeys {
			assert.NotEqual(t, key, d.Key,
				"%s was retired by #813 and must not be seeded into system_settings", key)
		}
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
