package config

// ContentSourcesConfig holds configuration for all content source providers.
// SEM@586d554a03f2ad309bdab24eb6ead126adc17e6e: aggregated configuration for all content source providers including Google Drive and Microsoft (pure)
type ContentSourcesConfig struct {
	GoogleDrive     GoogleDriveConfig     `yaml:"google_drive"`
	GoogleWorkspace GoogleWorkspaceConfig `yaml:"google_workspace"`
	Confluence      ConfluenceConfig      `yaml:"confluence"`
	Microsoft       MicrosoftConfig       `yaml:"microsoft"`
}

// GoogleDriveConfig holds Google Drive service account configuration.
//
// The Browser*/Picker* fields are public, browser-safe values used to bootstrap
// the in-browser Google Picker for service-mode Drive sources. They are
// optional; when all three are set, the /config response advertises them via
// ContentProvider.picker_config so the client can render a real picker instead
// of URL-paste UX. None of these values authorize anything on their own — the
// user OAuth handshake happens client-side via Google Identity Services with
// PKCE; no client_secret is ever surfaced.
// SEM@f2e01937e40c91e87ac47a34d11870fde716d093: Google Drive service account and browser picker configuration settings (pure)
type GoogleDriveConfig struct {
	Enabled              bool   `yaml:"enabled" env:"TMI_CONTENT_SOURCE_GOOGLE_DRIVE_ENABLED"`
	ServiceAccountEmail  string `yaml:"service_account_email" env:"TMI_CONTENT_SOURCE_GOOGLE_DRIVE_SERVICE_ACCOUNT_EMAIL"`
	CredentialsFile      string `yaml:"credentials_file" env:"TMI_CONTENT_SOURCE_GOOGLE_DRIVE_CREDENTIALS_FILE"`
	BrowserOAuthClientID string `yaml:"browser_oauth_client_id" env:"TMI_CONTENT_SOURCE_GOOGLE_DRIVE_BROWSER_OAUTH_CLIENT_ID"`
	PickerDeveloperKey   string `yaml:"picker_developer_key" env:"TMI_CONTENT_SOURCE_GOOGLE_DRIVE_PICKER_DEVELOPER_KEY"`
	PickerAppID          string `yaml:"picker_app_id" env:"TMI_CONTENT_SOURCE_GOOGLE_DRIVE_PICKER_APP_ID"`
}

// IsConfigured returns true if Google Drive has the minimum required configuration.
// SEM@d45ce3e9fce97c6e783aa7b8abbc40743c54ff54: return true when Google Drive is enabled with minimum required credentials (pure)
func (c GoogleDriveConfig) IsConfigured() bool {
	return c.Enabled && c.CredentialsFile != ""
}

// HasPickerConfig returns true when all three browser-safe picker bootstrap
// values are set. The /config handler emits ContentProvider.picker_config only
// when all three are present — partial configuration is treated as unconfigured.
// SEM@f2e01937e40c91e87ac47a34d11870fde716d093: return true when all three browser-safe Google Picker bootstrap values are set (pure)
func (c GoogleDriveConfig) HasPickerConfig() bool {
	return c.BrowserOAuthClientID != "" && c.PickerDeveloperKey != "" && c.PickerAppID != ""
}

// PickerConfig returns the browser-safe picker bootstrap map suitable for the
// /config response. Returns nil when HasPickerConfig is false.
// SEM@f2e01937e40c91e87ac47a34d11870fde716d093: build the browser-safe Google Picker bootstrap map, or nil if incomplete (pure)
func (c GoogleDriveConfig) PickerConfig() map[string]string {
	if !c.HasPickerConfig() {
		return nil
	}
	return map[string]string{
		"client_id":     c.BrowserOAuthClientID,
		"developer_key": c.PickerDeveloperKey,
		"app_id":        c.PickerAppID,
	}
}

// MicrosoftConfig holds Microsoft Graph (OneDrive-for-Business + SharePoint)
// content source configuration. The OAuth client_id/client_secret live under
// content_oauth.providers.microsoft; this struct holds the source-side values
// needed for picker initialization (client_id and tenant_id are surfaced to
// the browser via the picker-token endpoint) and the picker-grant call
// (application_object_id is the TMI Entra app's object id, used as the
// Graph permission grantee).
// SEM@586d554a03f2ad309bdab24eb6ead126adc17e6e: Microsoft Graph content source settings for picker initialization and Graph permission grants (pure)
type MicrosoftConfig struct {
	Enabled             bool   `yaml:"enabled" env:"TMI_CONTENT_SOURCE_MICROSOFT_ENABLED"`
	TenantID            string `yaml:"tenant_id" env:"TMI_CONTENT_SOURCE_MICROSOFT_TENANT_ID"`
	ClientID            string `yaml:"client_id" env:"TMI_CONTENT_SOURCE_MICROSOFT_CLIENT_ID"`
	ApplicationObjectID string `yaml:"application_object_id" env:"TMI_CONTENT_SOURCE_MICROSOFT_APPLICATION_OBJECT_ID"`
	PickerOrigin        string `yaml:"picker_origin" env:"TMI_CONTENT_SOURCE_MICROSOFT_PICKER_ORIGIN"`
}

// IsConfigured returns true when all required fields are present and Enabled.
// PickerOrigin is optional — Experience 1 (paste-URL) works without it; only
// Experience 2 (picker) requires it. Operators must set it for picker UX.
// SEM@d45ce3e9fce97c6e783aa7b8abbc40743c54ff54: return true when Microsoft content source is enabled with all required tenant and app fields (pure)
func (c MicrosoftConfig) IsConfigured() bool {
	return c.Enabled && c.TenantID != "" && c.ClientID != "" && c.ApplicationObjectID != ""
}
