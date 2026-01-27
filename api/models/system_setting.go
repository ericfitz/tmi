// Package models defines GORM models for the TMI database schema.
package models

import (
	"time"
)

// SystemSetting represents a system-wide configuration setting stored in the database.
// These settings can be modified at runtime without requiring server restart.
// Settings are cached with short TTL for performance.
type SystemSetting struct {
	Key         string    `gorm:"primaryKey;type:varchar(256)" json:"key"`
	Value       string    `gorm:"type:varchar(4000);not null" json:"value"`
	Type        string    `gorm:"type:varchar(50);not null;default:string" json:"type"` // "string", "int", "bool", "json"
	Description *string   `gorm:"type:varchar(1024)" json:"description,omitempty"`
	ModifiedAt  time.Time `gorm:"not null;autoUpdateTime" json:"modified_at"`
	ModifiedBy  *string   `gorm:"type:varchar(36)" json:"modified_by,omitempty"` // User InternalUUID

	// Relationships
	Modifier *User `gorm:"foreignKey:ModifiedBy;references:InternalUUID" json:"-"`
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
			Key:         "rate_limit.requests_per_minute",
			Value:       "100",
			Type:        SystemSettingTypeInt,
			Description: defaultDescription("Maximum API requests per minute per user"),
		},
		{
			Key:         "rate_limit.requests_per_hour",
			Value:       "1000",
			Type:        SystemSettingTypeInt,
			Description: defaultDescription("Maximum API requests per hour per user"),
		},
		{
			Key:         "session.timeout_minutes",
			Value:       "60",
			Type:        SystemSettingTypeInt,
			Description: defaultDescription("JWT token expiration in minutes"),
		},
		{
			Key:         "websocket.max_participants",
			Value:       "10",
			Type:        SystemSettingTypeInt,
			Description: defaultDescription("Maximum participants per collaboration session"),
		},
		{
			Key:         "features.saml_enabled",
			Value:       "false",
			Type:        SystemSettingTypeBool,
			Description: defaultDescription("Enable SAML authentication"),
		},
		{
			Key:         "features.webhooks_enabled",
			Value:       "true",
			Type:        SystemSettingTypeBool,
			Description: defaultDescription("Enable webhook subscriptions"),
		},
		{
			Key:         "ui.default_theme",
			Value:       "auto",
			Type:        SystemSettingTypeString,
			Description: defaultDescription("Default UI theme (auto, light, dark)"),
		},
		{
			Key:         "upload.max_file_size_mb",
			Value:       "10",
			Type:        SystemSettingTypeInt,
			Description: defaultDescription("Maximum file upload size in megabytes"),
		},
	}
}
