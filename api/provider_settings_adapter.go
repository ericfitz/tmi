package api

import (
	"context"

	"github.com/ericfitz/tmi/auth"
)

// ProviderSettingsReaderAdapter adapts SettingsServiceInterface to auth.ProviderSettingsReader.
// SEM@18141a4245588ce0371c97df5c94e4ac7066ef7c: adapt SettingsServiceInterface to the auth.ProviderSettingsReader contract (pure)
type ProviderSettingsReaderAdapter struct {
	settings SettingsServiceInterface
}

// NewProviderSettingsReaderAdapter creates a new adapter.
// SEM@18141a4245588ce0371c97df5c94e4ac7066ef7c: build a ProviderSettingsReaderAdapter wrapping a settings service (pure)
func NewProviderSettingsReaderAdapter(settings SettingsServiceInterface) *ProviderSettingsReaderAdapter {
	return &ProviderSettingsReaderAdapter{settings: settings}
}

// ListByPrefix returns all settings whose key starts with the given prefix,
// converted to auth.ProviderSetting.
// SEM@5dfa9dcf64aa0662920dbbab3bca200db1b22c73: fetch provider settings whose key matches a given prefix, converted to auth types (reads DB)
func (a *ProviderSettingsReaderAdapter) ListByPrefix(ctx context.Context, prefix string) ([]auth.ProviderSetting, error) {
	dbSettings, err := a.settings.ListByPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}

	result := make([]auth.ProviderSetting, len(dbSettings))
	for i, s := range dbSettings {
		result[i] = auth.ProviderSetting{
			Key:   string(s.SettingKey),
			Value: string(s.Value),
		}
	}
	return result, nil
}
