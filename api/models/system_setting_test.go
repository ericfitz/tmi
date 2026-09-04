package models

import (
	"encoding/json"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSystemSetting_IsExplicit pins the #794 precedence rule and, critically,
// its polarity: "explicit" is the ONLY value that yields true. NULL, "seeded",
// and anything unexpected all read as not-explicit, so the database outranks a
// configured env/YAML value only when something deliberately said so.
//
// The polarity is the safety property. Every way an origin value can go
// missing — an empty string bound on Oracle, a writer that forgets the stamp,
// a stale pre-upgrade Redis entry with no origin key — lands on "not
// explicit", which makes the config layer win. Config is what an operator can
// see and control; a row nobody set winning is the 2026-08-20 outage.
func TestSystemSetting_IsExplicit(t *testing.T) {
	tests := []struct {
		name   string
		origin NullableDBVarchar
		want   bool
	}{
		{
			name:   "NULL origin is not explicit (fail-safe)",
			origin: NullableDBVarchar{Valid: false},
			want:   false,
		},
		{
			name:   "seeded origin is not explicit",
			origin: NullableDBVarchar{String: SystemSettingOriginSeeded, Valid: true},
			want:   false,
		},
		{
			name:   "explicit origin is explicit",
			origin: NullableDBVarchar{String: SystemSettingOriginExplicit, Valid: true},
			want:   true,
		},
		{
			name:   "unexpected origin value is not explicit (fail-safe)",
			origin: NullableDBVarchar{String: "some-future-value", Valid: true},
			want:   false,
		},
		{
			name:   "empty-string origin is not explicit (Oracle binds '' as NULL)",
			origin: NullableDBVarchar{String: "", Valid: true},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SystemSetting{Origin: tt.origin}
			if got := s.IsExplicit(); got != tt.want {
				t.Errorf("IsExplicit() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSystemSetting_OriginJSONRoundTrip guards against Origin regressing to
// json:"-". The Redis cache tier (SettingsService.setInRedisCache /
// getFromRedisCache) round-trips a SystemSetting through json.Marshal /
// json.Unmarshal; "-" would silently drop Origin on every Redis cache hit,
// making every seeded row look explicit after a warm read (oracle-db-admin
// review, #794). This is orthogonal to the wire API, which never marshals
// models.SystemSetting directly — modelToAPISystemSetting builds the API
// type field by field and never copies Origin across.
func TestSystemSetting_OriginJSONRoundTrip(t *testing.T) {
	original := SystemSetting{
		SettingKey:  "session.timeout_minutes",
		Value:       "60",
		SettingType: SystemSettingTypeInt,
		Origin:      NullableDBVarchar{String: SystemSettingOriginSeeded, Valid: true},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var roundTripped SystemSetting
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !roundTripped.Origin.Valid || roundTripped.Origin.String != SystemSettingOriginSeeded {
		t.Errorf("Origin after round-trip = (valid=%v, %q), want (valid=true, %q)",
			roundTripped.Origin.Valid, roundTripped.Origin.String, SystemSettingOriginSeeded)
	}
	if roundTripped.IsExplicit() {
		t.Error("round-tripped seeded row reports IsExplicit() == true; Origin was lost in JSON marshaling")
	}
}

// TestDefaultSystemSettings_MatchesRegistryProjection pins that
// DefaultSystemSettings is a projection of config.SeedableOperationalDefs,
// not a hand-kept parallel list — a parallel list is what left rate_limit.*
// seeded here with no classification entry anywhere else (#809).
func TestDefaultSystemSettings_MatchesRegistryProjection(t *testing.T) {
	defs := config.SeedableOperationalDefs()
	seeds := DefaultSystemSettings()

	require.Equal(t, len(defs), len(seeds),
		"DefaultSystemSettings must be a projection of the registry, not a parallel list")

	byKey := map[string]SystemSetting{}
	for _, s := range seeds {
		byKey[string(s.SettingKey)] = s
	}
	for _, d := range defs {
		s, ok := byKey[d.Key]
		require.True(t, ok, "registry declares %s but DefaultSystemSettings does not seed it", d.Key)
		assert.Equal(t, d.Default, string(s.Value), "seed value for %s must be the registry Default", d.Key)
		assert.Equal(t, d.Description, s.Description.String, "seed description for %s", d.Key)
	}
}

// TestDefaultSystemSettings_DoesNotSeedRetiredRateLimitKeys pins #813: the
// two dead rate_limit.* keys must not come back into the seed list. They were
// seeded but read by nothing; real rate limiting runs off
// server.disable_rate_limiting / server.ratelimit_public_rpm.
//
// The registry-side guardrail is config.TestRetiredRateLimitKeys_AreGone; this
// is the projection-side half, so a regression is caught at whichever end it
// is reintroduced.
// SEM@24731679561a852b21b37271caffd9a597080f0b: validate retired rate-limit keys are not seeded
func TestDefaultSystemSettings_DoesNotSeedRetiredRateLimitKeys(t *testing.T) {
	keys := map[string]bool{}
	for _, s := range DefaultSystemSettings() {
		keys[string(s.SettingKey)] = true
	}
	assert.False(t, keys["rate_limit.requests_per_minute"],
		"rate_limit.requests_per_minute was retired by #813 and must not be seeded")
	assert.False(t, keys["rate_limit.requests_per_hour"],
		"rate_limit.requests_per_hour was retired by #813 and must not be seeded")
}

// TestSystemSettingType_RegistryTokensAreStoredConstants pins #811: both
// DefaultSystemSettings and SettingsService.SeedDefaults write the registry's
// type token to system_settings.setting_type verbatim, and the column has no
// CHECK constraint on either engine. The token set must therefore equal the
// stored constants by construction; a new registry type lands here first.
func TestSystemSettingType_RegistryTokensAreStoredConstants(t *testing.T) {
	stored := map[string]bool{
		SystemSettingTypeString: true,
		SystemSettingTypeInt:    true,
		SystemSettingTypeBool:   true,
		SystemSettingTypeJSON:   true,
		SystemSettingTypeFloat:  true,
	}
	for _, d := range config.AllSettingDefs() {
		assert.Truef(t, stored[d.Type], "setting %s declares type %q, which is not a SystemSettingType constant", d.Key, d.Type)
	}
}
