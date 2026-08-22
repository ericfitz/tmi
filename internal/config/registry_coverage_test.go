package config

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegistry_CoversEveryReachableKey asserts that every setting key the
// server can reach — from the Config struct, the classification registry, or
// the database seed list — has a SettingDef with a real Category.
//
// The previous guardrail could not do this: ValidateClassifications took
// whatever slice it was handed, in practice GetMigratableSettings(), which by
// construction contains only keys that already have a Config struct field,
// and which emits conditionally so the set changed with runtime values.
func TestRegistry_CoversEveryReachableKey(t *testing.T) {
	// declared covers a key two ways: by its SettingDef.Key (the common
	// case), and by its SettingDef.YAMLPath (for the handful of defs whose
	// canonical settings Key deliberately differs from the raw Config
	// struct path — e.g. "auth.saml.enabled" is declared as
	// "features.saml_enabled" with YAMLPath: "auth.saml.enabled"). The
	// env-tag source below yields raw struct yaml paths, not settings keys,
	// so a rename would otherwise look uncovered despite being fully
	// declared. This can't be gamed with a fabricated YAMLPath:
	// TestSettingDefs_YAMLPathsAreReal (setting_defs_bijection_test.go)
	// already asserts every non-empty YAMLPath names a real yaml-tagged
	// Config field.
	declared := map[string]bool{}
	for _, d := range AllSettingDefs() {
		declared[d.Key] = true
		if d.YAMLPath != "" {
			declared[d.YAMLPath] = true
		}
	}

	reachable := map[string]string{} // key -> where it came from

	for k := range envTaggedKeys(t) {
		reachable[k] = "Config struct env tag"
	}
	for k := range exactClassifications {
		reachable[k] = "classification registry"
	}
	for _, d := range SeedableOperationalDefs() {
		reachable[d.Key] = "database seed list"
	}

	var uncovered []string
	for k, src := range reachable {
		if !declared[k] {
			uncovered = append(uncovered, k+" (from "+src+")")
		}
	}
	sort.Strings(uncovered)

	assert.Empty(t, uncovered,
		"every reachable setting key must have a SettingDef; add one to internal/config/setting_defs_*.go")
}

func TestRegistry_NoKeyIsDeclaredTwice(t *testing.T) {
	seen := map[string]int{}
	for _, d := range AllSettingDefs() {
		seen[d.Key]++
	}
	var dupes []string
	for k, n := range seen {
		if n > 1 {
			dupes = append(dupes, k)
		}
	}
	sort.Strings(dupes)
	assert.Empty(t, dupes, "a setting must be declared exactly once")
}

func TestRegistry_EveryDefHasANonZeroCategory(t *testing.T) {
	for _, d := range AllSettingDefs() {
		assert.NotEqual(t, CategoryUnclassified, d.Class.Category,
			"%s has the zero Category", d.Key)
	}
}

func TestRegistry_PassesValidation(t *testing.T) {
	assert.NoError(t, ValidateSettingDefs(AllSettingDefs()))
}

// seededKeys is the exact set of settings written into system_settings at
// database initialisation. Changing this list changes what every NEW database
// gets seeded with, so it is pinned deliberately rather than derived.
var seededKeys = []string{
	"features.saml_enabled",
	"features.webhooks_enabled",
	"features.websocket_enabled",
	"rate_limit.requests_per_hour",
	"rate_limit.requests_per_minute",
	"session.timeout_minutes",
	"ui.default_theme",
	"upload.max_file_size_mb",
	"websocket.max_participants",
}

// TestSeedableOperationalDefs_MatchesGoldenList pins SeedableOperationalDefs
// to the exact golden list above. Nothing else stops someone flipping
// Seeded: true on another operational def and silently adding a seed row to
// every new database; this test is that stop. SeedableOperationalDefs is
// already sorted by key, so seededKeys is compared verbatim rather than
// re-sorted — keeping it alphabetized here doubles as documentation.
func TestSeedableOperationalDefs_MatchesGoldenList(t *testing.T) {
	var got []string
	for _, d := range SeedableOperationalDefs() {
		got = append(got, d.Key)
	}

	assert.Equal(t, seededKeys, got,
		"the set of seeded keys changed — this list is deliberately pinned by name "+
			"(not derived from Class) since it decides what every NEW database gets "+
			"seeded with; update seededKeys above only if that is an intended change")
}

// TestSettingDefs_OnlySessionTimeoutHasEmptyYAMLPathWithGet closes the
// falsely-empty-YAMLPath hole: a def with a non-nil Get but an empty
// YAMLPath escapes the bijection test, the env-var match test, and
// validation (which makes YAMLPath optional for transitional defs). Only
// session.timeout_minutes is legitimate here — it is genuinely derived
// (computed from auth.jwt.expiration_seconds, no struct field of its own).
// Defs with a nil Get (database-only settings, e.g. the DB-only
// client-config knobs) legitimately have an empty YAMLPath too and are
// excluded from this check.
func TestSettingDefs_OnlySessionTimeoutHasEmptyYAMLPathWithGet(t *testing.T) {
	var offenders []string
	for _, d := range AllSettingDefs() {
		if d.Get == nil {
			continue // database-only setting: no config-file path to speak of
		}
		if d.YAMLPath == "" && d.Key != "session.timeout_minutes" {
			offenders = append(offenders, d.Key)
		}
	}
	sort.Strings(offenders)
	assert.Empty(t, offenders,
		"only session.timeout_minutes may have a non-nil Get with an empty YAMLPath "+
			"(it is derived from auth.jwt.expiration_seconds, with no struct field of "+
			"its own); an empty YAMLPath on any other def with a Get usually means a "+
			"real config path was blanked out")

	d, ok := DefFor("session.timeout_minutes")
	require.True(t, ok, "session.timeout_minutes must be declared")
	assert.NotNil(t, d.Get, "session.timeout_minutes must still have a Get accessor")
	assert.Empty(t, d.YAMLPath, "session.timeout_minutes has no struct field of its own")
}
