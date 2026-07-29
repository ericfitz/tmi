package api

import (
	"testing"

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
