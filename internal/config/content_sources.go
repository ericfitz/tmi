package config

// ContentSourcesConfig holds configuration for all content source providers.
type ContentSourcesConfig struct {
	GoogleDrive     GoogleDriveConfig     `yaml:"google_drive"`
	GoogleWorkspace GoogleWorkspaceConfig `yaml:"google_workspace"`
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
