package api

import "github.com/ericfitz/tmi/internal/config"

// ConfigProviderAdapter adapts config.Config to implement the ConfigProvider interface
// SEM@f25790d896e8e128807a3c9a0a517fcbe6f710fe: adapter bridging app config to the API migratable-settings interface (pure)
type ConfigProviderAdapter struct {
	cfg *config.Config
}

// NewConfigProviderAdapter creates a new ConfigProviderAdapter
// SEM@f25790d896e8e128807a3c9a0a517fcbe6f710fe: build a ConfigProviderAdapter wrapping the given config (pure)
func NewConfigProviderAdapter(cfg *config.Config) *ConfigProviderAdapter {
	return &ConfigProviderAdapter{cfg: cfg}
}

// GetMigratableSettings returns migratable settings from the config
// SEM@10b74985ed52c143cb0fb6e853b2d5f106de198f: convert config migratable settings to API-layer MigratableSetting values (pure)
func (a *ConfigProviderAdapter) GetMigratableSettings() []MigratableSetting {
	configSettings := a.cfg.GetMigratableSettings()
	settings := make([]MigratableSetting, len(configSettings))
	for i, s := range configSettings {
		settings[i] = MigratableSetting{
			Key:         s.Key,
			Value:       s.Value,
			Type:        s.Type,
			Description: s.Description,
			Secret:      s.Secret,
			Source:      s.Source,
			Explicit:    s.Explicit,
			Class:       s.Class,
		}
	}
	return settings
}
