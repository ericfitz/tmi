// Package models defines GORM models for the TMI database schema.
package models

import (
	"time"
)

// SystemSetting represents a system-wide configuration setting stored in the database.
// These settings can be modified at runtime without requiring server restart.
// Settings are cached with short TTL for performance.
type SystemSetting struct {
	// SettingKey is the unique identifier for this setting (e.g., "rate_limit.requests_per_minute")
	// Named SettingKey instead of Key to avoid Oracle reserved word conflict
	SettingKey string `gorm:"column:setting_key;primaryKey;type:varchar(256)" json:"key"`
	Value      string `gorm:"type:varchar(4000);not null" json:"value"`
	// SettingType stores the value type: "string", "int", "bool", "json"
	// Note: default tag removed for Oracle compatibility (unquoted string defaults cause syntax errors)
	SettingType string    `gorm:"column:setting_type;type:varchar(50);not null" json:"type"`
	Description *string   `gorm:"type:varchar(1024)" json:"description,omitempty"`
	ModifiedAt  time.Time `gorm:"not null;autoUpdateTime" json:"modified_at"`
	ModifiedBy  *string   `gorm:"type:varchar(36)" json:"modified_by,omitempty"` // User InternalUUID
	// Note: Foreign key relationship to User removed to avoid Oracle migration issues
}

// TableName specifies the table name for SystemSetting
func (SystemSetting) TableName() string {
	return tableName("system_settings")
}

// SystemSettingType constants for the Type field
const (
	SystemSettingTypeString = "string"
	SystemSettingTypeInt    = "int"
	SystemSettingTypeBool   = "bool"
	SystemSettingTypeJSON   = "json"
)

// DefaultSystemSettings returns the default system settings that should be seeded
// when the database is initialized. These provide sensible defaults that can be
// overridden by administrators.
func DefaultSystemSettings() []SystemSetting {
	defaultDescription := func(s string) *string { return &s }

	return []SystemSetting{
		{
			SettingKey:  "rate_limit.requests_per_minute",
			Value:       "100",
			SettingType: SystemSettingTypeInt,
			Description: defaultDescription("Maximum API requests per minute per user"),
		},
		{
			SettingKey:  "rate_limit.requests_per_hour",
			Value:       "1000",
			SettingType: SystemSettingTypeInt,
			Description: defaultDescription("Maximum API requests per hour per user"),
		},
		{
			SettingKey:  "session.timeout_minutes",
			Value:       "60",
			SettingType: SystemSettingTypeInt,
			Description: defaultDescription("JWT token expiration in minutes"),
		},
		{
			SettingKey:  "websocket.max_participants",
			Value:       "10",
			SettingType: SystemSettingTypeInt,
			Description: defaultDescription("Maximum participants per collaboration session"),
		},
		{
			SettingKey:  "features.saml_enabled",
			Value:       "false",
			SettingType: SystemSettingTypeBool,
			Description: defaultDescription("Enable SAML authentication"),
		},
		{
			SettingKey:  "features.webhooks_enabled",
			Value:       "true",
			SettingType: SystemSettingTypeBool,
			Description: defaultDescription("Enable webhook subscriptions"),
		},
		{
			SettingKey:  "ui.default_theme",
			Value:       "auto",
			SettingType: SystemSettingTypeString,
			Description: defaultDescription("Default UI theme (auto, light, dark)"),
		},
		{
			SettingKey:  "upload.max_file_size_mb",
			Value:       "10",
			SettingType: SystemSettingTypeInt,
			Description: defaultDescription("Maximum file upload size in megabytes"),
		},
	}
}
