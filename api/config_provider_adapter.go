package api

import "github.com/ericfitz/tmi/internal/config"

// ConfigProviderAdapter adapts config.Config to implement the ConfigProvider interface
type ConfigProviderAdapter struct {
	cfg *config.Config
}

// NewConfigProviderAdapter creates a new ConfigProviderAdapter
func NewConfigProviderAdapter(cfg *config.Config) *ConfigProviderAdapter {
	return &ConfigProviderAdapter{cfg: cfg}
}

// GetMigratableSettings returns migratable settings from the config
func (a *ConfigProviderAdapter) GetMigratableSettings() []MigratableSetting {
	configSettings := a.cfg.GetMigratableSettings()
	settings := make([]MigratableSetting, len(configSettings))
	for i, s := range configSettings {
		settings[i] = MigratableSetting{
			Key:         s.Key,
			Value:       s.Value,
			Type:        s.Type,
			Description: s.Description,
		}
	}
	return settings
}
