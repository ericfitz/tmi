package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
)

// These tests pin the precedence rule for operational settings (#415).
//
// The bug they guard against was found in production: timmy.enabled was set to
// true in the settings database, yet every Timmy endpoint still answered
// "Timmy AI assistant is not enabled". GetMigratableSettings emits *every* key
// including ones nobody configured — they carry the struct default — so
// getConfigSetting found timmy.enabled=false in the config layer and the
// database row was never consulted. Any operational setting with a default,
// which is all of them, was unreachable from the database.
//
// The rule: the config layer wins only when a value was actually configured —
// an env var is set, or the key was present in the YAML file — or when the key
// is bootstrap (database URL, JWT secret), which is never DB-backed. A bare
// struct default must yield to the database.

type stubConfigProvider struct{ settings []MigratableSetting }

func (s *stubConfigProvider) GetMigratableSettings() []MigratableSetting { return s.settings }

func operationalSetting(key, value, source string, explicit bool) MigratableSetting {
	return MigratableSetting{
		Key:      key,
		Value:    value,
		Type:     "bool",
		Source:   source,
		Explicit: explicit,
		Class:    config.ConfigClass{Category: config.CategoryOperational},
	}
}

// The production failure, reduced: an operational key nobody configured must
// not shadow the database.
func TestGetConfigSetting_DefaultDoesNotShadowDatabase(t *testing.T) {
	s := &SettingsService{}
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{
		operationalSetting("timmy.enabled", "false", "config", false),
	}})

	if _, found := s.getConfigSetting("timmy.enabled"); found {
		t.Fatal("a bare default was returned from the config layer; " +
			"the database value can never take effect (this is the #415 bug)")
	}
}

// An operator who sets the env var still wins — that is the documented escape
// hatch, and on AWS every ConfigMap key arrives as an env var via envFrom, so
// removing this would silently drop live configuration.
func TestGetConfigSetting_EnvVarStillWins(t *testing.T) {
	s := &SettingsService{}
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{
		operationalSetting("timmy.enabled", "true", "environment", true),
	}})

	got, found := s.getConfigSetting("timmy.enabled")
	if !found {
		t.Fatal("an env-var-set operational value must win over the database")
	}
	if got.Value != "true" {
		t.Fatalf("value = %q, want \"true\"", got.Value)
	}
}

// A value written explicitly in the YAML file also wins. Without this,
// config-development.yml's server.disable_rate_limiting=true would silently
// yield to the database's false and start rate-limiting dev — which also
// breaks CATS fuzzing.
func TestGetConfigSetting_ExplicitFileValueWins(t *testing.T) {
	s := &SettingsService{}
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{
		operationalSetting("server.disable_rate_limiting", "true", "config", true),
	}})

	got, found := s.getConfigSetting("server.disable_rate_limiting")
	if !found {
		t.Fatal("a value explicitly present in the config file must win over the database")
	}
	if got.Value != "true" {
		t.Fatalf("value = %q, want \"true\"", got.Value)
	}
}

// Bootstrap settings are never DB-backed, so they must always resolve from the
// config layer even when they carry a default.
func TestGetConfigSetting_BootstrapAlwaysWins(t *testing.T) {
	s := &SettingsService{}
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{
		{
			Key:      "database.url",
			Value:    "postgres://localhost/tmi",
			Type:     "string",
			Source:   "config",
			Explicit: false,
			Class:    config.ConfigClass{Category: config.CategoryBootstrap},
		},
	}})

	if _, found := s.getConfigSetting("database.url"); !found {
		t.Fatal("bootstrap settings are not database-backed and must always resolve from config")
	}
}

// A key the config layer does not know about at all is unaffected.
func TestGetConfigSetting_UnknownKeyNotFound(t *testing.T) {
	s := &SettingsService{}
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{
		operationalSetting("timmy.enabled", "false", "config", false),
	}})

	if _, found := s.getConfigSetting("does.not.exist"); found {
		t.Fatal("unknown key should not be found in the config layer")
	}
}

// ---------------------------------------------------------------------------
// GetResolvedString — the converged precedence rule (#794)
// ---------------------------------------------------------------------------
//
// These tests pin the four-case table that replaced the two contradictory
// rules described at the top of this file. The case that matters most is
// "explicit config + seeded DB row -> config wins": that is the exact shape of
// the 2026-08-20 production outage, where a registry-seeded
// http://localhost:8080/oauth2/callback row outranked a correctly-set
// TMI_OAUTH_CALLBACK_URL and broke OAuth login for every provider.

// seedRow inserts a system_settings row with the given origin. An empty origin
// string writes NULL, which reads back as seeded — the fail-safe direction.
func seedRow(t *testing.T, db *gorm.DB, key, value, origin string) {
	t.Helper()
	row := models.SystemSetting{
		SettingKey:  models.DBVarchar(key),
		Value:       models.DBText(value),
		SettingType: models.SystemSettingTypeString,
	}
	if origin != "" {
		row.Origin = models.NullableDBVarchar{String: origin, Valid: true}
	}
	require.NoError(t, db.Create(&row).Error)
}

func TestGetResolvedString_PrecedenceTable(t *testing.T) {
	const key = "auth.oauth_callback_url"
	const cfgValue = "https://api.tmi.dev/oauth2/callback"
	const dbValue = "http://localhost:8080/oauth2/callback"

	tests := []struct {
		name          string
		cfgExplicit   bool
		dbOrigin      string // "" == no row at all
		wantValue     string
		wantExists    bool
		wantRationale string
	}{
		{
			name:          "neither explicit: database wins",
			cfgExplicit:   false,
			dbOrigin:      models.SystemSettingOriginSeeded,
			wantValue:     dbValue,
			wantExists:    true,
			wantRationale: "both sides are defaults; prefer the runtime-editable one",
		},
		{
			name:          "only database explicit: database wins",
			cfgExplicit:   false,
			dbOrigin:      models.SystemSettingOriginExplicit,
			wantValue:     dbValue,
			wantExists:    true,
			wantRationale: "#415: a bare struct default must yield to the database",
		},
		{
			name:          "only config explicit: config wins",
			cfgExplicit:   true,
			dbOrigin:      models.SystemSettingOriginSeeded,
			wantValue:     cfgValue,
			wantExists:    true,
			wantRationale: "the 2026-08-20 outage: a seeded default must never beat an operator",
		},
		{
			name:          "both explicit: database wins",
			cfgExplicit:   true,
			dbOrigin:      models.SystemSettingOriginExplicit,
			wantValue:     dbValue,
			wantExists:    true,
			wantRationale: "#419/#767: an admin's runtime edit takes effect without a redeploy",
		},
		{
			name:          "row with NULL origin reads as seeded, so config wins",
			cfgExplicit:   true,
			dbOrigin:      "",
			wantValue:     cfgValue,
			wantExists:    true,
			wantRationale: "fail-safe polarity: a lost origin must never hand authority to a row nobody set",
		},
		{
			name:          "unexpected origin value also reads as seeded",
			cfgExplicit:   true,
			dbOrigin:      "banana",
			wantValue:     cfgValue,
			wantExists:    true,
			wantRationale: "only a deliberate 'explicit' counts; anything else degrades safely",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupSettingsTestDB(t)
			seedRow(t, db, key, dbValue, tt.dbOrigin)

			s := NewSettingsService(db, nil)
			s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{
				operationalSetting(key, cfgValue, "environment", tt.cfgExplicit),
			}})

			got, exists, err := s.GetResolvedString(context.Background(), key)
			require.NoError(t, err)
			require.Equal(t, tt.wantExists, exists)
			require.Equalf(t, tt.wantValue, got, "rationale: %s", tt.wantRationale)
		})
	}
}

// No row at all: the config layer is the only source, explicit or not.
func TestGetResolvedString_NoDatabaseRowFallsBackToConfig(t *testing.T) {
	db := setupSettingsTestDB(t)
	s := NewSettingsService(db, nil)
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{
		operationalSetting("timmy.enabled", "true", "config", false),
	}})

	got, exists, err := s.GetResolvedString(context.Background(), "timmy.enabled")
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "true", got)
}

// An empty row value is treated as absent. On Oracle ” binds as NULL, so an
// empty-but-present row is indistinguishable from a missing one; the rule must
// not depend on which engine is underneath.
func TestGetResolvedString_EmptyRowTreatedAsAbsent(t *testing.T) {
	const key = "auth.oauth_callback_url"
	db := setupSettingsTestDB(t)
	seedRow(t, db, key, "", models.SystemSettingOriginExplicit)

	s := NewSettingsService(db, nil)
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{
		operationalSetting(key, "https://api.tmi.dev/oauth2/callback", "environment", true),
	}})

	got, exists, err := s.GetResolvedString(context.Background(), key)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "https://api.tmi.dev/oauth2/callback", got,
		"an empty DB row must not shadow a configured value")
}

// Bootstrap keys never consult the database, even when a row exists.
func TestGetResolvedString_BootstrapNeverReadsDatabase(t *testing.T) {
	const key = "database.url"
	db := setupSettingsTestDB(t)
	seedRow(t, db, key, "postgres://wrong/db", models.SystemSettingOriginExplicit)

	s := NewSettingsService(db, nil)
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{{
		Key:      key,
		Value:    "postgres://localhost/tmi",
		Type:     "string",
		Source:   "config",
		Explicit: false,
		Class:    config.ConfigClass{Category: config.CategoryBootstrap},
	}}})

	got, exists, err := s.GetResolvedString(context.Background(), key)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "postgres://localhost/tmi", got,
		"a bootstrap key must resolve from config even when a DB row exists")
}

// A key no layer supplies resolves to "not found" rather than an empty win.
func TestGetResolvedString_UnknownKey(t *testing.T) {
	db := setupSettingsTestDB(t)
	s := NewSettingsService(db, nil)
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{}})

	got, exists, err := s.GetResolvedString(context.Background(), "does.not.exist")
	require.NoError(t, err)
	require.False(t, exists)
	require.Equal(t, "", got)
}
