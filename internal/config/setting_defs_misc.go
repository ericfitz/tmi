package config

import "strconv"

// miscSettingDefs declares the sections not covered by the other
// setting_defs_*.go files: websocket.*, webhooks.*, operator.*, secrets.*
// (the secrets-provider bootstrap block), alerting.* (the audit alert sink),
// ssrf.* (per-URI-class allowlists), the DB-only client-config keys
// (features.webhooks_enabled, features.websocket_enabled,
// websocket.max_participants, upload.max_file_size_mb, ui.default_theme),
// and the one derived key with no Config struct field of its own,
// session.timeout_minutes. See setting_def.go for the SettingDef contract
// and classification_registry.go for the source of truth on each key's
// ConfigClass.
var miscSettingDefs = []SettingDef{
	// --- websocket.* ---
	{
		Key:          "websocket.inactivity_timeout_seconds",
		Class:        classificationFor("websocket.inactivity_timeout_seconds"),
		Type:         "int",
		Description:  "WebSocket inactivity timeout in seconds",
		Default:      "300",
		YAMLPath:     "websocket.inactivity_timeout_seconds",
		EnvVar:       "TMI_WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS",
		Get:          func(c *Config) string { return strconv.Itoa(c.WebSocket.InactivityTimeoutSeconds) },
		Transitional: true,
	},

	// --- webhooks.* ---
	{
		Key:          "webhooks.allow_http_targets",
		Class:        classificationFor("webhooks.allow_http_targets"),
		Type:         "bool",
		Description:  "Allow non-HTTPS webhook target URLs (intra-cluster use only)",
		Default:      "false",
		YAMLPath:     "webhooks.allow_http_targets",
		EnvVar:       "TMI_WEBHOOK_ALLOW_HTTP_TARGETS",
		Get:          func(c *Config) string { return strconv.FormatBool(c.Webhooks.AllowHTTPTargets) },
		Transitional: true,
	},

	// --- operator.* ---
	{
		Key:          "operator.name",
		Class:        classificationFor("operator.name"),
		Type:         "string",
		Description:  "Operator/maintainer name",
		Default:      "",
		YAMLPath:     "operator.name",
		EnvVar:       "TMI_OPERATOR_NAME",
		Get:          func(c *Config) string { return c.Operator.Name },
		Transitional: true,
	},
	{
		Key:          "operator.contact",
		Class:        classificationFor("operator.contact"),
		Type:         "string",
		Description:  "Operator contact information",
		Default:      "",
		YAMLPath:     "operator.contact",
		EnvVar:       "TMI_OPERATOR_CONTACT",
		Get:          func(c *Config) string { return c.Operator.Contact },
		Transitional: true,
	},
	{
		Key:          "operator.jurisdiction",
		Class:        classificationFor("operator.jurisdiction"),
		Type:         "string",
		Description:  "Legal jurisdiction under which the service operates",
		Default:      "",
		YAMLPath:     "operator.jurisdiction",
		EnvVar:       "TMI_OPERATOR_JURISDICTION",
		Get:          func(c *Config) string { return c.Operator.Jurisdiction },
		Transitional: true,
	},

	// --- secrets.* (bootstrap: the secrets-provider backend coordinates) ---
	{
		Key:         "secrets.provider",
		Class:       classificationFor("secrets.provider"),
		Type:        "string",
		Description: "Secret provider type",
		YAMLPath:    "secrets.provider",
		EnvVar:      "TMI_SECRETS_PROVIDER",
		Get:         func(c *Config) string { return c.Secrets.Provider },
	},
	{
		Key:         "secrets.vault_address",
		Class:       classificationFor("secrets.vault_address"),
		Type:        "string",
		Description: "HashiCorp Vault address",
		YAMLPath:    "secrets.vault_address",
		EnvVar:      "TMI_VAULT_ADDRESS",
		Get:         func(c *Config) string { return c.Secrets.VaultAddress },
	},
	{
		Key:         "secrets.vault_path",
		Class:       classificationFor("secrets.vault_path"),
		Type:        "string",
		Description: "HashiCorp Vault path",
		YAMLPath:    "secrets.vault_path",
		EnvVar:      "TMI_VAULT_PATH",
		Get:         func(c *Config) string { return c.Secrets.VaultPath },
	},
	{
		Key:         "secrets.vault_token",
		Class:       classificationFor("secrets.vault_token"),
		Type:        "string",
		Description: "HashiCorp Vault token",
		YAMLPath:    "secrets.vault_token",
		EnvVar:      "TMI_VAULT_TOKEN",
		Get:         func(c *Config) string { return c.Secrets.VaultToken },
	},
	{
		Key:         "secrets.aws_region",
		Class:       classificationFor("secrets.aws_region"),
		Type:        "string",
		Description: "AWS region",
		YAMLPath:    "secrets.aws_region",
		EnvVar:      "TMI_AWS_REGION",
		Get:         func(c *Config) string { return c.Secrets.AWSRegion },
	},
	{
		Key:         "secrets.aws_secret_name",
		Class:       classificationFor("secrets.aws_secret_name"),
		Type:        "string",
		Description: "AWS secret name",
		YAMLPath:    "secrets.aws_secret_name",
		EnvVar:      "TMI_AWS_SECRET_NAME",
		Get:         func(c *Config) string { return c.Secrets.AWSSecretName },
	},
	{
		Key:         "secrets.azure_vault_url",
		Class:       classificationFor("secrets.azure_vault_url"),
		Type:        "string",
		Description: "Azure Key Vault URL",
		YAMLPath:    "secrets.azure_vault_url",
		EnvVar:      "TMI_AZURE_VAULT_URL",
		Get:         func(c *Config) string { return c.Secrets.AzureVaultURL },
	},
	{
		Key:         "secrets.gcp_project_id",
		Class:       classificationFor("secrets.gcp_project_id"),
		Type:        "string",
		Description: "GCP project ID",
		YAMLPath:    "secrets.gcp_project_id",
		EnvVar:      "TMI_GCP_PROJECT_ID",
		Get:         func(c *Config) string { return c.Secrets.GCPProjectID },
	},
	{
		Key:         "secrets.gcp_secret_name",
		Class:       classificationFor("secrets.gcp_secret_name"),
		Type:        "string",
		Description: "GCP secret name",
		YAMLPath:    "secrets.gcp_secret_name",
		EnvVar:      "TMI_GCP_SECRET_NAME",
		Get:         func(c *Config) string { return c.Secrets.GCPSecretName },
	},
	{
		Key:         "secrets.oci_compartment_id",
		Class:       classificationFor("secrets.oci_compartment_id"),
		Type:        "string",
		Description: "OCI compartment ID",
		YAMLPath:    "secrets.oci_compartment_id",
		EnvVar:      "TMI_OCI_COMPARTMENT_ID",
		Get:         func(c *Config) string { return c.Secrets.OCICompartmentID },
	},
	{
		Key:         "secrets.oci_vault_id",
		Class:       classificationFor("secrets.oci_vault_id"),
		Type:        "string",
		Description: "OCI vault ID",
		YAMLPath:    "secrets.oci_vault_id",
		EnvVar:      "TMI_OCI_VAULT_ID",
		Get:         func(c *Config) string { return c.Secrets.OCIVaultID },
	},
	{
		Key:         "secrets.oci_secret_name",
		Class:       classificationFor("secrets.oci_secret_name"),
		Type:        "string",
		Description: "OCI secret name",
		YAMLPath:    "secrets.oci_secret_name",
		EnvVar:      "TMI_OCI_SECRET_NAME",
		Get:         func(c *Config) string { return c.Secrets.OCISecretName },
	},

	// --- alerting.* (bootstrap: read once at startup by
	// EnsurePinnedAlertSubscription; see classification_registry.go) ---
	{
		Key:         "alerting.enabled",
		Class:       classificationFor("alerting.enabled"),
		Type:        "bool",
		Description: "Enable the operator-pinned audit alert sink webhook subscription (#395)",
		YAMLPath:    "alerting.enabled",
		EnvVar:      "TMI_ALERTING_ENABLED",
		Get:         func(c *Config) string { return strconv.FormatBool(c.Alerting.Enabled) },
	},
	{
		Key:         "alerting.webhook_url",
		Class:       classificationFor("alerting.webhook_url"),
		Type:        "string",
		Description: "URL of the audit alert sink webhook endpoint (#395)",
		YAMLPath:    "alerting.webhook_url",
		EnvVar:      "TMI_ALERTING_WEBHOOK_URL",
		Get:         func(c *Config) string { return c.Alerting.WebhookURL },
	},
	{
		Key:         "alerting.webhook_secret",
		Class:       classificationFor("alerting.webhook_secret"),
		Type:        "string",
		Description: "HMAC signing secret for the audit alert sink webhook (#395)",
		YAMLPath:    "alerting.webhook_secret",
		EnvVar:      "TMI_ALERTING_WEBHOOK_SECRET",
		Get:         func(c *Config) string { return c.Alerting.WebhookSecret },
	},

	// --- ssrf.* (operational, security-sensitive allowlists; empty is
	// fail-closed, so Default is deliberately "" rather than a wildcard).
	// YAMLPath is deliberately empty on every entry below: SSRFURIConfig's
	// Allowlist/Schemes fields carry a yaml tag but no env tag, and the
	// bijection test only recognizes config paths bound to an env tag — a
	// non-empty YAMLPath here would register as "extra". Get still reads the
	// real Config field. ---
	{
		Key:          "ssrf.issue_uri.allowlist",
		Class:        classificationFor("ssrf.issue_uri.allowlist"),
		Type:         "string",
		Description:  "SSRF allowlist for ssrf.issue_uri (comma-separated host patterns)",
		Default:      "",
		YAMLPath:     "", // no env tag on the underlying SSRFURIConfig field
		Get:          func(c *Config) string { return c.SSRF.IssueURI.Allowlist },
		Transitional: true,
	},
	{
		Key:          "ssrf.issue_uri.schemes",
		Class:        classificationFor("ssrf.issue_uri.schemes"),
		Type:         "string",
		Description:  "Permitted URI schemes for ssrf.issue_uri (comma-separated, e.g. https)",
		Default:      "",
		YAMLPath:     "", // no env tag on the underlying SSRFURIConfig field
		Get:          func(c *Config) string { return c.SSRF.IssueURI.Schemes },
		Transitional: true,
	},
	{
		Key:          "ssrf.document_uri.allowlist",
		Class:        classificationFor("ssrf.document_uri.allowlist"),
		Type:         "string",
		Description:  "SSRF allowlist for ssrf.document_uri (comma-separated host patterns)",
		Default:      "",
		YAMLPath:     "", // no env tag on the underlying SSRFURIConfig field
		Get:          func(c *Config) string { return c.SSRF.DocumentURI.Allowlist },
		Transitional: true,
	},
	{
		Key:          "ssrf.document_uri.schemes",
		Class:        classificationFor("ssrf.document_uri.schemes"),
		Type:         "string",
		Description:  "Permitted URI schemes for ssrf.document_uri (comma-separated, e.g. https)",
		Default:      "",
		YAMLPath:     "", // no env tag on the underlying SSRFURIConfig field
		Get:          func(c *Config) string { return c.SSRF.DocumentURI.Schemes },
		Transitional: true,
	},
	{
		Key:          "ssrf.repository_uri.allowlist",
		Class:        classificationFor("ssrf.repository_uri.allowlist"),
		Type:         "string",
		Description:  "SSRF allowlist for ssrf.repository_uri (comma-separated host patterns)",
		Default:      "",
		YAMLPath:     "", // no env tag on the underlying SSRFURIConfig field
		Get:          func(c *Config) string { return c.SSRF.RepositoryURI.Allowlist },
		Transitional: true,
	},
	{
		Key:          "ssrf.repository_uri.schemes",
		Class:        classificationFor("ssrf.repository_uri.schemes"),
		Type:         "string",
		Description:  "Permitted URI schemes for ssrf.repository_uri (comma-separated, e.g. https)",
		Default:      "",
		YAMLPath:     "", // no env tag on the underlying SSRFURIConfig field
		Get:          func(c *Config) string { return c.SSRF.RepositoryURI.Schemes },
		Transitional: true,
	},
	{
		Key:          "ssrf.timmy.allowlist",
		Class:        classificationFor("ssrf.timmy.allowlist"),
		Type:         "string",
		Description:  "SSRF allowlist for ssrf.timmy (comma-separated host patterns)",
		Default:      "",
		YAMLPath:     "", // no env tag on the underlying SSRFURIConfig field
		Get:          func(c *Config) string { return c.SSRF.Timmy.Allowlist },
		Transitional: true,
	},
	{
		Key:          "ssrf.timmy.schemes",
		Class:        classificationFor("ssrf.timmy.schemes"),
		Type:         "string",
		Description:  "Permitted URI schemes for ssrf.timmy (comma-separated, e.g. https)",
		Default:      "",
		YAMLPath:     "", // no env tag on the underlying SSRFURIConfig field
		Get:          func(c *Config) string { return c.SSRF.Timmy.Schemes },
		Transitional: true,
	},
	{
		Key:          "ssrf.webhook.allowlist",
		Class:        classificationFor("ssrf.webhook.allowlist"),
		Type:         "string",
		Description:  "SSRF allowlist for ssrf.webhook (comma-separated host patterns)",
		Default:      "",
		YAMLPath:     "", // no env tag on the underlying SSRFURIConfig field
		Get:          func(c *Config) string { return c.SSRF.Webhook.Allowlist },
		Transitional: true,
	},
	{
		Key:          "ssrf.webhook.schemes",
		Class:        classificationFor("ssrf.webhook.schemes"),
		Type:         "string",
		Description:  "Permitted URI schemes for ssrf.webhook (comma-separated, e.g. https)",
		Default:      "",
		YAMLPath:     "", // no env tag on the underlying SSRFURIConfig field
		Get:          func(c *Config) string { return c.SSRF.Webhook.Schemes },
		Transitional: true,
	},

	// --- DB-only client-config keys: no Config struct field, seeded
	// directly into system_settings by models.DefaultSystemSettings. See
	// classification_registry.go's "DB-only client-config knobs" comment. ---
	{
		Key:         "features.webhooks_enabled",
		Class:       classificationFor("features.webhooks_enabled"),
		Type:        "bool",
		Description: "Enable webhook subscriptions",
		Default:     "true",
		Seeded:      true,
	},
	{
		Key:         "features.websocket_enabled",
		Class:       classificationFor("features.websocket_enabled"),
		Type:        "bool",
		Description: "Enable WebSocket collaboration",
		Default:     "true",
		Seeded:      true,
	},
	{
		Key:         "websocket.max_participants",
		Class:       classificationFor("websocket.max_participants"),
		Type:        "int",
		Description: "Maximum participants per collaboration session",
		Default:     "10",
		Seeded:      true,
	},
	{
		Key:         "upload.max_file_size_mb",
		Class:       classificationFor("upload.max_file_size_mb"),
		Type:        "int",
		Description: "Maximum file upload size in megabytes",
		Default:     "10",
		Seeded:      true,
	},
	{
		Key:         "ui.default_theme",
		Class:       classificationFor("ui.default_theme"),
		Type:        "string",
		Description: "Default UI theme (auto, light, dark)",
		Default:     "auto",
		Seeded:      true,
	},

	// --- Derived key: no Config struct field of its own ---
	// migratable_settings.go:433-441 computes this as
	// Auth.JWT.ExpirationSeconds / 60 and reports it under the same env var
	// that auth.jwt.expiration_seconds also uses. YAMLPath is deliberately
	// empty — a non-empty path here would collide with auth.jwt.expiration_seconds
	// in the bijection test's map and one would silently win.
	{
		Key:          "session.timeout_minutes",
		YAMLPath:     "", // derived: no struct field of its own
		EnvVar:       "TMI_JWT_EXPIRATION_SECONDS",
		Type:         "int",
		Description:  "JWT token expiration in minutes",
		Default:      "60",
		Transitional: true,
		Seeded:       true,
		Class:        classificationFor("session.timeout_minutes"),
		Get:          func(c *Config) string { return strconv.Itoa(c.Auth.JWT.ExpirationSeconds / 60) },
	},
}
