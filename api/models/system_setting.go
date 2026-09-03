// Package models defines GORM models for the TMI database schema.
package models

import (
	"time"

	"github.com/ericfitz/tmi/internal/config"
)

// SystemSetting represents a system-wide configuration setting stored in the database.
// These settings can be modified at runtime without requiring server restart.
// Settings are cached with short TTL for performance.
// SEM@2daf3be663df9da54323f16d115f12d78d435c3f: GORM model for a runtime-configurable system setting with key, typed value, and audit fields (reads DB)
type SystemSetting struct {
	// SettingKey is the unique identifier for this setting (e.g., "websocket.max_participants")
	// Named SettingKey instead of Key to avoid Oracle reserved word conflict
	SettingKey DBVarchar `gorm:"primaryKey;not null;size:256" json:"key"`
	Value      DBText    `gorm:"not null" json:"value"`
	// SettingType stores the value type: "string", "int", "bool", "json", "float"
	// Note: default tag removed for Oracle compatibility (unquoted string defaults cause syntax errors)
	SettingType DBVarchar         `gorm:"size:50;not null" json:"type"`
	Description NullableDBText    `gorm:"" json:"description,omitempty"`
	ModifiedAt  time.Time         `gorm:"not null;autoUpdateTime" json:"modified_at"`
	ModifiedBy  NullableDBVarchar `gorm:"size:36" json:"modified_by,omitempty"` // User InternalUUID
	// Origin records who wrote this row's current value: SystemSettingOriginSeeded
	// when SeedDefaults inserted the registry default at first boot, or
	// SystemSettingOriginExplicit when an operator deliberately set it (via the
	// admin API, SettingsService.Set, or dbtool --import-config).
	//
	// NULL means SEEDED — the fail-safe direction. Every way an origin value
	// can go missing (an empty string bound on Oracle, a future writer that
	// forgets the stamp, a stale pre-upgrade Redis entry whose cached JSON has
	// no origin key) therefore degrades to "seeded", which makes the config
	// layer win. Config is the layer an operator can see and control, so
	// losing this value must never hand authority to a database row nobody set
	// — that is precisely the 2026-08-20 outage (oracle-db-admin review, #794).
	//
	// Rows that predate this column are stamped explicit where they show
	// operator intent, by BackfillSystemSettingOrigin in internal/dbschema.
	// The column itself is added with no `ALTER TABLE ... DEFAULT` (Oracle
	// rejects unquoted string defaults — see the SettingType comment above).
	// Never write "" here: Oracle binds an
	// empty string as NULL, so an empty string and NULL would be
	// indistinguishable there. Only NULL, SystemSettingOriginSeeded, or
	// SystemSettingOriginExplicit are valid values.
	// Not part of the wire API: modelToAPISystemSetting (api/config_handlers.go)
	// builds the API SystemSetting field by field and never copies Origin
	// across, so it never reaches a client regardless of this tag. The tag is
	// a real json name (not "-") because the Redis cache tier round-trips a
	// SystemSetting through json.Marshal/Unmarshal (setInRedisCache /
	// getFromRedisCache) — "-" would silently drop Origin on every Redis
	// cache hit and make every setting look explicit after a warm read
	// (oracle-db-admin review, #794).
	Origin NullableDBVarchar `gorm:"size:16" json:"origin,omitempty"`
	// Source indicates where the effective value comes from: "database", "config", "environment", "vault"
	// Computed at response time, not stored in the database.
	Source string `gorm:"-" json:"source"`
	// ReadOnly indicates whether this setting can be modified via the API.
	// True when source is not "database". Computed at response time.
	ReadOnly bool `gorm:"-" json:"read_only"`
}

// TableName specifies the table name for SystemSetting
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: return the database table name for the SystemSetting model (pure)
func (SystemSetting) TableName() string {
	return tableName("system_settings")
}

// SystemSettingType constants for the Type field
const (
	SystemSettingTypeString = "string"
	SystemSettingTypeInt    = "int"
	SystemSettingTypeBool   = "bool"
	SystemSettingTypeJSON   = "json"
	// SystemSettingTypeFloat backs observability.sampling_rate, whose config
	// field is a float64. It was previously declared "string", which made
	// --export-config emit a quoted value that --import-config could not read
	// back into the field (#791).
	SystemSettingTypeFloat = "float"
)

// SystemSettingOrigin constants for the Origin field
const (
	// SystemSettingOriginSeeded marks a row inserted by SeedDefaults with a
	// registry default value — no operator has ever set it.
	SystemSettingOriginSeeded = "seeded"
	// SystemSettingOriginExplicit marks a row an operator deliberately set,
	// via the admin API, SettingsService.Set, or dbtool --import-config.
	SystemSettingOriginExplicit = "explicit"
)

// IsExplicit reports whether an operator deliberately set this row's value,
// as opposed to it having been seeded with a registry default at first boot.
//
// Only an explicit "explicit" counts. NULL, "seeded", and any unexpected value
// all read as NOT explicit, so the database only outranks an explicitly
// configured env/YAML value when something deliberately said so. See the
// Origin field's comment for why that polarity is the safe one.
// SEM@2daf3be663df9da54323f16d115f12d78d435c3f: report whether a setting's value was deliberately set rather than seeded (pure)
func (s *SystemSetting) IsExplicit() bool {
	return s.Origin.Valid && s.Origin.String == SystemSettingOriginExplicit
}

// DefaultSystemSettings returns the default system settings that should be seeded
// when the database is initialized. These provide sensible defaults that can be
// overridden by administrators.
//
// This is a projection of the internal/config registry
// (config.SeedableOperationalDefs), not a hand-kept parallel list — a
// parallel list is what left rate_limit.requests_per_minute and
// rate_limit.requests_per_hour seeded here with no classification entry
// anywhere else, so GET/DELETE /admin/settings/{key} 404'd on keys the LIST
// endpoint showed (#809).
// SEM@62e82fc4e96a1c18f4e1ac1d698de4fefb974b42: build the seed list of default system settings for database initialization (pure)
func DefaultSystemSettings() []SystemSetting {
	desc := func(s string) NullableDBText { return NullableDBText{String: s, Valid: true} }

	defs := config.SeedableOperationalDefs()
	out := make([]SystemSetting, 0, len(defs))
	for _, d := range defs {
		out = append(out, SystemSetting{
			SettingKey:  DBVarchar(d.Key),
			Value:       DBText(d.Default),
			SettingType: DBVarchar(d.Type), // registry tokens ARE the stored constants; pinned by TestSystemSettingType_RegistryTokensAreStoredConstants (#811)
			Description: desc(d.Description),
		})
	}
	return out
}
