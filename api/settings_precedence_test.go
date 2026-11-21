package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/dberrors"
)

// These tests pin the precedence rule for operational settings (#415).
//
// The bug they guard against was found in production: timmy.enabled was set to
// true in the settings database, yet every Timmy endpoint still answered
// "Timmy AI assistant is not enabled". GetMigratableSettings emits *every* key
// including ones nobody configured — they carry the struct default — so
// the config lookup found timmy.enabled=false in the config layer and the
// database row was never consulted. Any operational setting with a default,
// which is all of them, was unreachable from the database.
//
// The rule (one for every read path since #1168; see configOutranksRow): an
// admin-set row wins; a seeded row yields to explicit config; with no usable
// row the config layer is used; bootstrap keys (database URL, JWT secret) are
// never DB-backed. A bare struct default must yield to a database row.

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

// Bootstrap settings are never DB-backed, so they must always resolve from the
// config layer even when they carry a default.
func TestGetConfigSetting_BootstrapAlwaysWins(t *testing.T) {
	s := NewSettingsService(setupSettingsTestDB(t), nil)
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

	got, err := s.GetString(context.Background(), "database.url")
	if err != nil || got != "postgres://localhost/tmi" {
		t.Fatalf("bootstrap settings are not database-backed and must always resolve from config; got %q, %v", got, err)
	}
}

// A key the config layer does not know about at all is unaffected.
func TestGetConfigSetting_UnknownKeyNotFound(t *testing.T) {
	s := &SettingsService{}
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{
		operationalSetting("timmy.enabled", "false", "config", false),
	}})

	if _, found := s.getConfigSettingRaw("does.not.exist"); found {
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
	const key = "operator.contact"
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
	const key = "operator.contact"
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

// ---------------------------------------------------------------------------
// GetString/GetBool/GetInt/GetJSON follow the same rule as GetResolvedString
// (#1168): an admin-set row outranks config, explicit or not.
// ---------------------------------------------------------------------------

// The #1168 repro: config does not set timmy.enabled, an admin PUTs false, then
// the operator exports TMI_TIMMY_ENABLED=true. The admin API reports false, so
// GetBool must too.
func TestRowWins1168_AdminRowOutranksExplicitConfig(t *testing.T) {
	db := setupSettingsTestDB(t)
	row := models.SystemSetting{
		SettingKey:  "timmy.enabled",
		Value:       "false",
		SettingType: models.SystemSettingTypeBool,
		Origin:      models.NullableDBVarchar{String: models.SystemSettingOriginExplicit, Valid: true},
	}
	require.NoError(t, db.Create(&row).Error)

	s := NewSettingsService(db, nil)
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{
		operationalSetting("timmy.enabled", "true", "environment", true),
	}})

	got, err := s.GetBool(context.Background(), "timmy.enabled")
	require.NoError(t, err)
	require.False(t, got, "an admin-set row must win over explicit config")
}

func TestRowWins1168_PrecedenceTable(t *testing.T) {
	const key = "upload.max_file_size_mb"
	tests := []struct {
		name        string
		cfgPresent  bool
		cfgExplicit bool
		rowOrigin   string // "none" == no row
		rowValue    string
		want        string
	}{
		{"explicit row beats explicit config", true, true, models.SystemSettingOriginExplicit, "20", "20"},
		{"explicit row beats default config", true, false, models.SystemSettingOriginExplicit, "20", "20"},
		{"explicit row, no config entry", false, false, models.SystemSettingOriginExplicit, "20", "20"},
		{"seeded row loses to explicit config", true, true, models.SystemSettingOriginSeeded, "20", "30"},
		{"seeded row beats default config", true, false, models.SystemSettingOriginSeeded, "20", "20"},
		{"no row, explicit config", true, true, "none", "", "30"},
		{"no row, default config", true, false, "none", "", "30"},
		{"empty explicit row is absent, explicit config wins", true, true, models.SystemSettingOriginExplicit, "", "30"},
		{"no row, no config entry", false, false, "none", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupSettingsTestDB(t)
			if tt.rowOrigin != "none" {
				seedRow(t, db, key, tt.rowValue, tt.rowOrigin)
			}
			s := NewSettingsService(db, nil)
			var cfg []MigratableSetting
			if tt.cfgPresent {
				m := operationalSetting(key, "30", "config", tt.cfgExplicit)
				m.Type = "int"
				cfg = append(cfg, m)
			}
			s.SetConfigProvider(&stubConfigProvider{settings: cfg})

			gotStr, err := s.GetString(context.Background(), key)
			require.NoError(t, err)
			require.Equal(t, tt.want, gotStr)

			gotInt, err := s.GetInt(context.Background(), key)
			require.NoError(t, err)
			wantInt := 0
			if tt.want != "" {
				wantInt, err = strconv.Atoi(tt.want)
				require.NoError(t, err)
			}
			require.Equal(t, wantInt, gotInt)

			// GetResolvedString must agree with GetString on every case.
			resolved, _, err := s.GetResolvedString(context.Background(), key)
			require.NoError(t, err)
			require.Equal(t, gotStr, resolved)
		})
	}
}

// An unusable (empty) non-explicit config default must not turn a typed read
// into a parse error when no row exists.
func TestRowWins1168_EmptyConfigDefaultIsUnset(t *testing.T) {
	db := setupSettingsTestDB(t)
	s := NewSettingsService(db, nil)
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{
		operationalSetting("timmy.enabled", "", "config", false),
	}})
	got, err := s.GetBool(context.Background(), "timmy.enabled")
	require.NoError(t, err)
	require.False(t, got)
}

func TestRowWins1168_GetJSON(t *testing.T) {
	const key = "ssrf.webhook.allowlist"
	db := setupSettingsTestDB(t)
	seedRow(t, db, key, `["row"]`, models.SystemSettingOriginExplicit)
	s := NewSettingsService(db, nil)
	m := operationalSetting(key, `["cfg"]`, "environment", true)
	m.Type = "json"
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{m}})

	var got []string
	require.NoError(t, s.GetJSON(context.Background(), key, &got))
	require.Equal(t, []string{"row"}, got)
}

// ---------------------------------------------------------------------------
// Transient database faults (#1168 review): explicit config used to bypass the
// database, so a transient fault must still return the config layer's value.
// ---------------------------------------------------------------------------

// failQueries makes every GORM query fail with a message dberrors classifies as
// transient (or not, per msg).
func failQueries(t *testing.T, db *gorm.DB, msg string) {
	t.Helper()
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail", func(tx *gorm.DB) {
		_ = tx.AddError(errors.New(msg))
	}))
}

func TestRowWins1168_TransientDBFaultFallsBackToConfig(t *testing.T) {
	const key = "upload.max_file_size_mb"
	for _, explicit := range []bool{true, false} {
		t.Run(fmt.Sprintf("config explicit=%v", explicit), func(t *testing.T) {
			db := setupSettingsTestDB(t)
			failQueries(t, db, "statement is closed")
			s := NewSettingsService(db, nil)
			m := operationalSetting(key, "30", "config", explicit)
			m.Type = "int"
			s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{m}})

			got, err := s.GetInt(context.Background(), key)
			require.NoError(t, err)
			require.Equal(t, 30, got)
			str, err := s.GetString(context.Background(), key)
			require.NoError(t, err)
			require.Equal(t, "30", str)
		})
	}
}

func TestRowWins1168_TransientDBFaultWithoutConfigErrors(t *testing.T) {
	db := setupSettingsTestDB(t)
	failQueries(t, db, "statement is closed")
	s := NewSettingsService(db, nil)
	s.SetConfigProvider(&stubConfigProvider{})

	_, err := s.GetString(context.Background(), "upload.max_file_size_mb")
	require.Error(t, err)
	require.ErrorIs(t, err, dberrors.ErrTransient)
}

func TestRowWins1168_NonTransientDBFaultStillErrors(t *testing.T) {
	db := setupSettingsTestDB(t)
	failQueries(t, db, "some permanent failure")
	s := NewSettingsService(db, nil)
	m := operationalSetting("upload.max_file_size_mb", "30", "config", true)
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{m}})

	_, err := s.GetString(context.Background(), "upload.max_file_size_mb")
	require.Error(t, err)
}
