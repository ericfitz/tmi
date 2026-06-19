package config

// GoogleWorkspaceConfig holds settings for the delegated Google Workspace
// content source. OAuth-provider settings (client id/secret, endpoints,
// scopes) live under content_oauth.providers.google_workspace.
//
// PickerDeveloperKey and PickerAppID are Google Cloud project values that
// the browser-side Google Picker JS needs; they are not secrets.
// SEM@d1d001debf78074ff7ac194deeb67ced6da6be19: config struct for the Google Workspace content source, including Picker credentials (pure)
type GoogleWorkspaceConfig struct {
	Enabled            bool   `yaml:"enabled" env:"TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_ENABLED"`
	PickerDeveloperKey string `yaml:"picker_developer_key" env:"TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_PICKER_DEVELOPER_KEY"`
	PickerAppID        string `yaml:"picker_app_id" env:"TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_PICKER_APP_ID"`
}

// IsConfigured returns true when the source is enabled and both picker
// inputs are non-empty.
// SEM@d1d001debf78074ff7ac194deeb67ced6da6be19: validate that Google Workspace integration is enabled and fully configured (pure)
func (c GoogleWorkspaceConfig) IsConfigured() bool {
	return c.Enabled && c.PickerDeveloperKey != "" && c.PickerAppID != ""
}
