package config

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestMigratableSettings_CoverEveryConfigField is the build-time gate that
// closes #421's "I added a setting and forgot to migrate it" class of bug.
//
// The test walks Config with reflection (DiscoverConfigFields) and the
// per-subsystem helpers' emit set (GetMigratableSettings). Every leaf
// field discovered by reflection must either:
//
//   - appear in GetMigratableSettings(), OR
//   - be listed in ExpectedMigratableKeysSkipped() with a justification.
//
// A field that reflection sees but neither path covers is reported as a
// hard failure: either add it to getMigratable*Settings, or add an entry
// to ExpectedMigratableKeysSkipped explaining why it doesn't migrate.
//
// This is the lighter-weight half of #421's vision: full reflection-based
// emit (with type/source/description inference from struct tags) is a
// larger refactor; the gate alone removes the silent-omission risk that
// motivated the issue. Subsequent work can incrementally lift more of the
// helper logic into the reflection layer.
func TestMigratableSettings_CoverEveryConfigField(t *testing.T) {
	discovered := DiscoverConfigFields()

	// We walk the emit set against a FULLY-POPULATED Config rather than
	// getDefaultConfig(), because several helpers gate emission on a
	// non-zero value (`if c.Server.BaseURL != "" { ... }`). Those guards
	// preserve the legacy "don't write an empty row" behavior but make
	// the helpers indistinguishable from "field is never emitted" to a
	// static walker. A populated sentinel config disambiguates them.
	emitted := populatedConfig().GetMigratableSettings()
	emittedSet := make(map[string]bool, len(emitted))
	for _, s := range emitted {
		emittedSet[s.Key] = true
	}
	skipSet := ExpectedMigratableKeysSkipped()

	var missing []string
	for _, df := range discovered {
		if emittedSet[df.DottedKey] {
			continue
		}
		if _, ok := skipSet[df.DottedKey]; ok {
			continue
		}
		missing = append(missing, df.DottedKey)
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf(
			"%d Config field(s) are not emitted as migratable settings and not in the skip list.\n"+
				"For each key below, either:\n"+
				"  - add an emit-block in internal/config/migratable_settings.go (preferred), OR\n"+
				"  - add an entry to ExpectedMigratableKeysSkipped() in migratable_discovery.go with a justification.\n\n"+
				"Uncovered keys:\n  %s",
			len(missing),
			strings.Join(missing, "\n  "),
		)
	}
}

// populatedConfig returns a Config with every leaf string/int/bool field
// set to a sentinel non-zero/non-empty value, so the conditional emits
// in getMigratable*Settings actually fire. This is the test fixture for
// TestMigratableSettings_CoverEveryConfigField.
//
// The values themselves are meaningless — they just need to be non-zero
// so the helpers' `if x != "" { ... }` guards pass. The function does
// NOT need to maintain coverage as Config grows; reflection-based
// population means new fields are populated automatically.
func populatedConfig() *Config {
	c := getDefaultConfig()
	populateNonZero(reflect.ValueOf(c).Elem())
	return c
}

func populateNonZero(v reflect.Value) {
	if !v.CanSet() {
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			populateNonZero(v.Field(i))
		}
	case reflect.String:
		if v.String() == "" {
			v.SetString("sentinel")
		}
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Int() == 0 {
			v.SetInt(1)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v.Uint() == 0 {
			v.SetUint(1)
		}
	case reflect.Float32, reflect.Float64:
		if v.Float() == 0 {
			v.SetFloat(0.5)
		}
	case reflect.Slice, reflect.Map, reflect.Array, reflect.Pointer, reflect.Interface, reflect.Chan, reflect.Func:
		// Leave alone — the gate skips dynamic-cardinality types and
		// these are not where the false positives come from.
	}
}

// TestDiscoverConfigFields_FindsKnownLeaves sanity-checks the reflection
// walker against a few known leaves so a regression in the walker is
// detected even before TestMigratableSettings_CoverEveryConfigField runs.
func TestDiscoverConfigFields_FindsKnownLeaves(t *testing.T) {
	got := DiscoverConfigFields()
	keys := make(map[string]bool, len(got))
	for _, f := range got {
		keys[f.DottedKey] = true
	}
	want := []string{
		"server.port",
		"server.tls_enabled",
		"database.url",
		"database.connection_pool.max_open_conns",
		"database.redis.host",
		"auth.build_mode",
		"auth.jwt.secret",
		"auth.cookie.enabled",
		"logging.level",
		"operator.name",
		"secrets.provider",
		"timmy.enabled",
		"timmy.llm_api_key",
	}
	for _, w := range want {
		if !keys[w] {
			t.Errorf("expected DiscoverConfigFields to find %q, got keys: %v", w, keysSorted(keys))
		}
	}
}

// TestDiscoverConfigFields_SkipsMapsAndSlices ensures the walker does not
// emit dotted keys for dynamic-cardinality types (provider maps, the
// administrators slice). Those are walked by the per-instance helpers in
// migratable_settings.go and must NOT appear in the discovery output.
func TestDiscoverConfigFields_SkipsMapsAndSlices(t *testing.T) {
	for _, f := range DiscoverConfigFields() {
		if strings.HasPrefix(f.DottedKey, "auth.oauth.providers") ||
			strings.HasPrefix(f.DottedKey, "auth.saml.providers") ||
			strings.HasPrefix(f.DottedKey, "content_oauth.providers") ||
			f.DottedKey == "administrators" {
			t.Errorf("walker emitted a dynamic-cardinality key %q — maps and slice-of-structs must be skipped", f.DottedKey)
		}
	}
}

func keysSorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
