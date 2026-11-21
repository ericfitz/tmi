package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
)

// An explicit row that fails its per-key semantic validator is no usable row
// (#1181, ADR decision 9): the config layer answers, exactly as for an
// unreadable row (#1169), at runtime and in the admin reads.

const (
	invalidRowKey      = "timmy.llm_provider"
	invalidRowBadValue = "bogus-provider"
)

func timmyProviderConfig(value string) MigratableSetting {
	return MigratableSetting{
		Key:      invalidRowKey,
		Value:    value,
		Type:     "string",
		Source:   "env",
		Explicit: true,
		Class:    config.ConfigClass{Category: config.CategoryOperational},
	}
}

func seedExplicitRow(t *testing.T, db *gorm.DB, key, value string) {
	t.Helper()
	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  models.DBVarchar(key),
		Value:       models.DBText(value),
		SettingType: models.SystemSettingTypeString,
		Origin:      models.NullableDBVarchar{String: models.SystemSettingOriginExplicit, Valid: true},
	}).Error)
}

func invalidRowService(t *testing.T, rowValue string, cfg ...MigratableSetting) (*SettingsService, *gorm.DB) {
	t.Helper()
	db := setupSettingsTestDB(t)
	seedExplicitRow(t, db, invalidRowKey, rowValue)
	s := NewSettingsService(db, nil)
	s.SetConfigProvider(&stubConfigProvider{settings: cfg})
	return s, db
}

func TestGetString_InvalidExplicitRowFallsBackToConfig(t *testing.T) {
	s, _ := invalidRowService(t, invalidRowBadValue, timmyProviderConfig("anthropic"))

	got, err := s.GetString(context.Background(), invalidRowKey)
	require.NoError(t, err)
	assert.Equal(t, "anthropic", got)
}

func TestGetResolvedString_InvalidExplicitRowFallsBackToConfig(t *testing.T) {
	s, _ := invalidRowService(t, invalidRowBadValue, timmyProviderConfig("anthropic"))

	got, exists, err := s.GetResolvedString(context.Background(), invalidRowKey)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "anthropic", got)
}

func TestGetString_InvalidExplicitRowNoConfigIsAbsent(t *testing.T) {
	s, _ := invalidRowService(t, invalidRowBadValue)

	got, exists, err := s.GetResolvedString(context.Background(), invalidRowKey)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Empty(t, got)
}

func TestGetString_InvalidExplicitRowCounterpartValidRowWins(t *testing.T) {
	s, _ := invalidRowService(t, "openai", timmyProviderConfig("anthropic"))

	got, err := s.GetString(context.Background(), invalidRowKey)
	require.NoError(t, err)
	assert.Equal(t, "openai", got)
}

// The dump flag validator depends on the build mode: a row of true is valid in
// development and unusable in production.
func TestGetBool_DumpFlagInvalidExplicitRowFollowsBuildMode(t *testing.T) {
	const key = "timmy.dump_extracted_text_to_note"
	dumpCfg := func(value, buildMode string) []MigratableSetting {
		return []MigratableSetting{
			{Key: key, Value: value, Type: "bool", Source: "env", Explicit: true,
				Class: config.ConfigClass{Category: config.CategoryOperational}},
			{Key: "auth.build_mode", Value: buildMode, Type: "string", Source: "env",
				Class: config.ConfigClass{Category: config.CategoryBootstrap}},
		}
	}
	for mode, want := range map[string]bool{"production": false, "dev": true} {
		t.Run(mode, func(t *testing.T) {
			db := setupSettingsTestDB(t)
			seedExplicitRow(t, db, key, "true")
			s := NewSettingsService(db, nil)
			s.SetConfigProvider(&stubConfigProvider{settings: dumpCfg("false", mode)})

			got, err := s.GetBool(context.Background(), key)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestConfigOutranksRow_InvalidExplicitRow(t *testing.T) {
	explicit := models.NullableDBVarchar{String: models.SystemSettingOriginExplicit, Valid: true}
	bad := &models.SystemSetting{SettingKey: invalidRowKey, Value: invalidRowBadValue, Origin: explicit}
	good := &models.SystemSetting{SettingKey: invalidRowKey, Value: "openai", Origin: explicit}
	noValidator := &models.SystemSetting{SettingKey: "ui.default_theme", Value: "anything", Origin: explicit}

	assert.True(t, configOutranksRow(false, bad, false))
	assert.True(t, configOutranksRow(true, bad, true))
	assert.False(t, configOutranksRow(false, good, false))
	assert.False(t, configOutranksRow(false, noValidator, false), "keys without a validator are never invalid")
}

func TestAdminReads_InvalidExplicitRowShowsConfigValue(t *testing.T) {
	s, _ := invalidRowService(t, invalidRowBadValue, timmyProviderConfig("anthropic"))
	r := unreadableRouter(t, s, []MigratableSetting{timmyProviderConfig("anthropic")})

	code, one, _ := doJSON(t, r, http.MethodGet, "/admin/settings/"+invalidRowKey)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "anthropic", one["value"])
	assert.Equal(t, "env", one["source"])

	code, _, all := doJSON(t, r, http.MethodGet, "/admin/settings")
	require.Equal(t, http.StatusOK, code)
	for _, e := range all {
		if e["key"] == invalidRowKey {
			assert.Equal(t, "anthropic", e["value"])
			assert.Equal(t, "env", e["source"])
			return
		}
	}
	t.Fatal("key missing from list")
}

// Boot check seam: the effective Timmy config resolved through the real
// service passes validation when the only invalid input is a database row.
func TestTimmyConfigProvider_InvalidExplicitRowYieldsValidEffectiveConfig(t *testing.T) {
	db := setupSettingsTestDB(t)
	seedExplicitRow(t, db, invalidRowKey, invalidRowBadValue)
	seedExplicitRow(t, db, "timmy.dump_extracted_text_to_note", "true")
	s := NewSettingsService(db, nil)
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{
		timmyProviderConfig("anthropic"),
		{Key: "timmy.dump_extracted_text_to_note", Value: "false", Type: "bool", Source: "env", Explicit: true,
			Class: config.ConfigClass{Category: config.CategoryOperational}},
		{Key: "auth.build_mode", Value: "production", Type: "string", Source: "env",
			Class: config.ConfigClass{Category: config.CategoryBootstrap}},
	}})

	cfg := NewTimmyConfigProvider(s).Current(context.Background())
	assert.Equal(t, "anthropic", cfg.LLMProvider)
	assert.False(t, cfg.DumpExtractedTextToNote)
	require.NoError(t, config.ValidateTimmyConfig(cfg, "production"))
}

// With no config entry to fall back to, an invalid row is unusable at runtime,
// so the admin reads must not present it as the value in effect (#1181). A
// test-only validator key stands in for a validated key without a config entry.
func TestAdminReads_InvalidExplicitRowWithoutConfigEntryIsAbsent(t *testing.T) {
	const key = "test.invalid_row_no_config"
	settingSemanticValidators[key] = func(value string, _ bool) error {
		if value != "ok" {
			return assert.AnError
		}
		return nil
	}
	t.Cleanup(func() { delete(settingSemanticValidators, key) })

	db := setupSettingsTestDB(t)
	seedExplicitRow(t, db, key, "bad")
	s := NewSettingsService(db, nil)
	r := unreadableRouter(t, s, nil)

	_, exists, err := s.GetResolvedString(context.Background(), key)
	require.NoError(t, err)
	assert.False(t, exists, "runtime sees the key as absent")

	code, _, _ := doJSON(t, r, http.MethodGet, "/admin/settings/"+key)
	assert.Equal(t, http.StatusNotFound, code)

	code, _, all := doJSON(t, r, http.MethodGet, "/admin/settings")
	require.Equal(t, http.StatusOK, code)
	for _, e := range all {
		assert.NotEqual(t, key, e["key"], "invalid row must not be listed")
	}

	// A valid row for the same key is still shown.
	require.NoError(t, db.Model(&models.SystemSetting{}).Where("setting_key = ?", key).Update("value", "ok").Error)
	s2 := NewSettingsService(db, nil)
	r2 := unreadableRouter(t, s2, nil)
	code, one, _ := doJSON(t, r2, http.MethodGet, "/admin/settings/"+key)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "ok", one["value"])
}
