package api

import (
	"context"

	"github.com/ericfitz/tmi/auth"
)

// ProviderSettingsReaderAdapter adapts SettingsServiceInterface to auth.ProviderSettingsReader.
type ProviderSettingsReaderAdapter struct {
	settings SettingsServiceInterface
}

// NewProviderSettingsReaderAdapter creates a new adapter.
func NewProviderSettingsReaderAdapter(settings SettingsServiceInterface) *ProviderSettingsReaderAdapter {
	return &ProviderSettingsReaderAdapter{settings: settings}
}

// ListByPrefix returns all settings whose key starts with the given prefix,
// converted to auth.ProviderSetting.
func (a *ProviderSettingsReaderAdapter) ListByPrefix(ctx context.Context, prefix string) ([]auth.ProviderSetting, error) {
	dbSettings, err := a.settings.ListByPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}

	result := make([]auth.ProviderSetting, len(dbSettings))
	for i, s := range dbSettings {
		result[i] = auth.ProviderSetting{
			Key:   s.SettingKey,
			Value: s.Value,
		}
	}
	return result, nil
}
