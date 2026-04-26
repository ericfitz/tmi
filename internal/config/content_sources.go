package config

// ContentSourcesConfig holds configuration for all content source providers.
type ContentSourcesConfig struct {
	GoogleDrive     GoogleDriveConfig     `yaml:"google_drive"`
	GoogleWorkspace GoogleWorkspaceConfig `yaml:"google_workspace"`
	Confluence      ConfluenceConfig      `yaml:"confluence"`
	Microsoft       MicrosoftConfig       `yaml:"microsoft"`
}

// GoogleDriveConfig holds Google Drive service account configuration.
type GoogleDriveConfig struct {
	Enabled             bool   `yaml:"enabled" env:"TMI_CONTENT_SOURCE_GOOGLE_DRIVE_ENABLED"`
	ServiceAccountEmail string `yaml:"service_account_email" env:"TMI_CONTENT_SOURCE_GOOGLE_DRIVE_SERVICE_ACCOUNT_EMAIL"`
	CredentialsFile     string `yaml:"credentials_file" env:"TMI_CONTENT_SOURCE_GOOGLE_DRIVE_CREDENTIALS_FILE"`
}

// IsConfigured returns true if Google Drive has the minimum required configuration.
func (c GoogleDriveConfig) IsConfigured() bool {
	return c.Enabled && c.CredentialsFile != ""
}

// MicrosoftConfig holds Microsoft Graph (OneDrive-for-Business + SharePoint)
// content source configuration. The OAuth client_id/client_secret live under
// content_oauth.providers.microsoft; this struct holds the source-side values
// needed for picker initialization (client_id and tenant_id are surfaced to
// the browser via the picker-token endpoint) and the picker-grant call
// (application_object_id is the TMI Entra app's object id, used as the
// Graph permission grantee).
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
func (c MicrosoftConfig) IsConfigured() bool {
	return c.Enabled && c.TenantID != "" && c.ClientID != "" && c.ApplicationObjectID != ""
}
