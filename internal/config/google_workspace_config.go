package config

// GoogleWorkspaceConfig holds settings for the delegated Google Workspace
// content source. OAuth-provider settings (client id/secret, endpoints,
// scopes) live under content_oauth.providers.google_workspace.
//
// PickerDeveloperKey and PickerAppID are Google Cloud project values that
// the browser-side Google Picker JS needs; they are not secrets.
type GoogleWorkspaceConfig struct {
	Enabled            bool   `yaml:"enabled" env:"TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_ENABLED"`
	PickerDeveloperKey string `yaml:"picker_developer_key" env:"TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_PICKER_DEVELOPER_KEY"`
	PickerAppID        string `yaml:"picker_app_id" env:"TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_PICKER_APP_ID"`
}

// IsConfigured returns true when the source is enabled and both picker
// inputs are non-empty.
func (c GoogleWorkspaceConfig) IsConfigured() bool {
	return c.Enabled && c.PickerDeveloperKey != "" && c.PickerAppID != ""
}
