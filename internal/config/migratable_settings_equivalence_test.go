package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isGeneratedProviderKey reports whether a key comes from the per-provider
// generators (auth.oauth.providers.*, auth.saml.providers.*,
// content_oauth.providers.*) rather than from a static SettingDef. These
// subtrees have dynamic cardinality — one instance per configured provider —
// so they cannot have a fixed dotted-key SettingDef and are out of scope for
// the registry projection.
func isGeneratedProviderKey(key string) bool {
	for _, p := range []string{
		"auth.oauth.providers.",
		"auth.saml.providers.",
		"content_oauth.providers.",
	} {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// sampleConfig returns a Config with enough fields populated to exercise the
// OmitWhenEmpty conditional-emission paths (see OmitWhenEmpty's doc comment
// on SettingDef).
func sampleConfig() *Config {
	c := &Config{}
	c.Server.Port = "8080"
	c.Server.Interface = "0.0.0.0"
	c.Server.BaseURL = "https://api.example.test"
	c.Server.TLSEnabled = true
	c.Server.TLSCertFile = "/tmp/cert.pem"
	c.Server.TLSKeyFile = "/tmp/key.pem"
	c.Server.CORS.AllowedOrigins = []string{"https://ui.example.test"}
	return c
}

// TestGetMigratableSettings_ValuesComeFromConfig pins that projected values
// come from the live Config for a representative sample of types, including
// OmitWhenEmpty keys populated with a real value.
func TestGetMigratableSettings_ValuesComeFromConfig(t *testing.T) {
	c := sampleConfig()
	byKey := map[string]MigratableSetting{}
	for _, s := range c.GetMigratableSettings() {
		byKey[s.Key] = s
	}

	require.Contains(t, byKey, "server.port")
	assert.Equal(t, "8080", byKey["server.port"].Value)
	assert.Equal(t, "TMI_SERVER_PORT", byKey["server.port"].EnvVar)

	require.Contains(t, byKey, "server.tls_enabled")
	assert.Equal(t, "true", byKey["server.tls_enabled"].Value)

	require.Contains(t, byKey, "server.cors.allowed_origins")
	assert.Equal(t, `["https://ui.example.test"]`, byKey["server.cors.allowed_origins"].Value)

	// A populated OmitWhenEmpty key must still be emitted, with its real value.
	require.Contains(t, byKey, "server.base_url")
	assert.Equal(t, "https://api.example.test", byKey["server.base_url"].Value)

	// server.tls_cert_file / server.tls_key_file are special-cased on
	// server.tls_enabled (not OmitWhenEmpty) — with TLS enabled they must
	// appear.
	require.Contains(t, byKey, "server.tls_cert_file")
	assert.Equal(t, "/tmp/cert.pem", byKey["server.tls_cert_file"].Value)
	require.Contains(t, byKey, "server.tls_key_file")
	assert.Equal(t, "/tmp/key.pem", byKey["server.tls_key_file"].Value)
}

// TestGetMigratableSettings_OmitsEmptyOptionalKeys pins the conditional
// emission this refactor preserves — NOT the "make emission unconditional"
// instruction in the original brief, which the controller's Ruling 16
// rejected as unsafe (see OmitWhenEmpty's doc comment on SettingDef).
// A zero-value Config must not emit any OmitWhenEmpty key, exactly
// reproducing the pre-registry builders' `if x != ""` / `if len(x) > 0` /
// `if x > 0` guards.
func TestGetMigratableSettings_OmitsEmptyOptionalKeys(t *testing.T) {
	c := &Config{}
	byKey := map[string]MigratableSetting{}
	for _, s := range c.GetMigratableSettings() {
		byKey[s.Key] = s
	}

	// One representative key per emptiness kind (plain string "", JSON
	// "[]"/"null", and numeric "0") plus every OmitWhenEmpty key that also
	// belongs to DefaultOperationalSettings() — those are the ones where
	// getting this wrong seeds a spurious row (see the test below).
	omitWhenEmptyKeys := []string{
		"server.base_url",
		"server.cors.allowed_origins",
		"database.redis.url",
		"auth.oauth_callback_url",
		"auth.oauth.client_callback_allowlist",
		"operator.name",
		"operator.contact",
		"operator.jurisdiction",
		"secrets.vault_address",
		"secrets.vault_path",
		"secrets.aws_region",
		"secrets.aws_secret_name",
		"secrets.azure_vault_url",
		"secrets.gcp_project_id",
		"secrets.gcp_secret_name",
		"secrets.oci_compartment_id",
		"secrets.oci_vault_id",
		"secrets.oci_secret_name",
		"ssrf.issue_uri.allowlist",
		"ssrf.issue_uri.schemes",
		"ssrf.document_uri.allowlist",
		"ssrf.document_uri.schemes",
		"ssrf.repository_uri.allowlist",
		"ssrf.repository_uri.schemes",
		"ssrf.timmy.allowlist",
		"ssrf.timmy.schemes",
		"ssrf.webhook.allowlist",
		"ssrf.webhook.schemes",
		"content_oauth.callback_url",
		"content_sources.google_drive.service_account_email",
		"content_sources.google_drive.credentials_file",
		"content_sources.google_drive.browser_oauth_client_id",
		"content_sources.google_drive.picker_developer_key",
		"content_sources.google_drive.picker_app_id",
		"content_sources.google_workspace.picker_developer_key",
		"content_sources.google_workspace.picker_app_id",
		"content_sources.microsoft.tenant_id",
		"content_sources.microsoft.client_id",
		"content_sources.microsoft.application_object_id",
		"content_sources.microsoft.picker_origin",
		"alerting.webhook_url",
		"alerting.webhook_secret",
		"administrators",
		"websocket.inactivity_timeout_seconds",
		"session.timeout_minutes",
	}
	for _, key := range omitWhenEmptyKeys {
		_, ok := byKey[key]
		assert.False(t, ok, "%s must be omitted from a zero-value Config (OmitWhenEmpty)", key)
	}

	// server.tls_cert_file / server.tls_key_file: omitted because
	// server.tls_enabled is false on a zero-value Config, not because of
	// OmitWhenEmpty (they deliberately don't carry that flag — see the
	// comment above their declarations in setting_defs_server.go).
	_, ok := byKey["server.tls_cert_file"]
	assert.False(t, ok, "server.tls_cert_file must be omitted when server.tls_enabled is false")
	_, ok = byKey["server.tls_key_file"]
	assert.False(t, ok, "server.tls_key_file must be omitted when server.tls_enabled is false")
}

// TestGetMigratableSettings_CarriesClassAndSecrecy pins that every emitted
// key's Class and secrecy come from the registry — or, for generated
// provider keys (which have no SettingDef by design), from prefix
// classification.
func TestGetMigratableSettings_CarriesClassAndSecrecy(t *testing.T) {
	c := sampleConfig()
	for _, s := range c.GetMigratableSettings() {
		// Per-provider keys are generated, not statically declared, and have
		// no SettingDef by design. Their secrecy lives on the per-setting
		// Secret flag rather than on Class.Secret, so comparing them against
		// a SettingDef would be both impossible and wrong.
		if isGeneratedProviderKey(s.Key) {
			assert.True(t, s.Class.Category != CategoryUnclassified,
				"generated provider key %s must still be prefix-classified", s.Key)
			continue
		}
		d, ok := DefFor(s.Key)
		require.True(t, ok, "emitted key %s has no SettingDef", s.Key)
		assert.Equal(t, d.Class.Category, s.Class.Category, "class mismatch for %s", s.Key)
		assert.Equal(t, d.IsSecret(), s.IsSecret(), "secrecy mismatch for %s", s.Key)
	}
}

func TestGetMigratableSettings_ExplicitTracksEnvAndFile(t *testing.T) {
	t.Setenv("TMI_SERVER_PORT", "9090")
	c := sampleConfig()
	for _, s := range c.GetMigratableSettings() {
		if s.Key == "server.port" {
			assert.Equal(t, "environment", s.Source)
			assert.True(t, s.Explicit, "an env-set key must be Explicit")
		}
	}
}

// TestDefaultOperationalSettings_MatchesPreRegistryBaseline is the single
// most important test in this file. want is the exact key/value set
// captured from DefaultOperationalSettings() BEFORE this refactor (the
// pre-registry, per-section-builder implementation of GetMigratableSettings,
// commit 382a6fd2), by running a throwaway test and recording its output —
// see the task report for the capture command.
//
// api/settings_service.go's SeedDefaults and #794's origin backfill
// (internal/dbschema/system_setting_origin_backfill.go) both consume
// DefaultOperationalSettings() to decide, respectively, what a fresh
// database seeds into system_settings and whether an existing NULL-origin
// row reads as seeded or explicit. If this set changes — a key appears,
// disappears, or its value changes — a fresh database seeds different rows
// and #794's backfill classifies existing rows differently. This test is
// the gate that would catch that.
func TestDefaultOperationalSettings_MatchesPreRegistryBaseline(t *testing.T) {
	want := map[string]string{
		"auth.auto_promote_first_user":                    "false",
		"auth.cookie.domain":                              "",
		"auth.cookie.enabled":                             "true",
		"auth.cookie.secure":                              "false",
		"auth.everyone_is_a_reviewer":                     "false",
		"auth.jwt.expiration_seconds":                     "3600",
		"auth.jwt.refresh_token_days":                     "7",
		"auth.jwt.session_lifetime_days":                  "7",
		"auth.oauth_callback_url":                         "http://localhost:8080/oauth2/callback",
		"auth.step_up_window_seconds":                     "300",
		"content_extractors.compressed_size_bytes":        "20971520",
		"content_extractors.decompressed_size_bytes":      "52428800",
		"content_extractors.markdown_size_bytes":          "131072",
		"content_extractors.part_size_bytes":              "20971520",
		"content_extractors.per_user_concurrency_default": "2",
		"content_extractors.pptx_slides":                  "100",
		"content_extractors.wall_clock_budget":            "30s",
		"content_extractors.xlsx_cells":                   "1000",
		"content_sources.confluence.enabled":              "false",
		"content_sources.google_drive.enabled":            "false",
		"content_sources.google_workspace.enabled":        "false",
		"content_sources.microsoft.enabled":               "false",
		"extraction.async_enabled":                        "false",
		"features.saml_enabled":                           "false",
		"observability.enabled":                           "false",
		"observability.prometheus_port":                   "0",
		"observability.sampling_rate":                     "1",
		"server.disable_rate_limiting":                    "false",
		"server.ratelimit_public_rpm":                     "0",
		"server.require_if_match":                         "false",
		"session.timeout_minutes":                         "60",
		"timmy.chunk_overlap":                             "50",
		"timmy.chunk_size":                                "512",
		"timmy.code_embedding_api_key":                    "",
		"timmy.code_embedding_base_url":                   "",
		"timmy.code_embedding_model":                      "",
		"timmy.code_embedding_provider":                   "",
		"timmy.code_retrieval_top_k":                      "10",
		"timmy.dump_extracted_text_to_note":               "false",
		"timmy.embedding_cleanup_interval_minutes":        "60",
		"timmy.embedding_dimension":                       "0",
		"timmy.embedding_idle_days_active":                "30",
		"timmy.embedding_idle_days_closed":                "7",
		"timmy.enabled":                                   "false",
		"timmy.inactivity_timeout_seconds":                "3600",
		"timmy.llm_api_key":                               "",
		"timmy.llm_base_url":                              "",
		"timmy.llm_max_tokens":                            "4096",
		"timmy.llm_model":                                 "",
		"timmy.llm_provider":                              "",
		"timmy.llm_timeout_seconds":                       "120",
		"timmy.max_concurrent_llm_requests":               "10",
		"timmy.max_conversation_history":                  "50",
		"timmy.max_memory_mb":                             "256",
		"timmy.max_messages_per_user_per_hour":            "60",
		"timmy.max_sessions_per_threat_model":             "50",
		"timmy.operator_system_prompt":                    "",
		"timmy.query_decomposition_enabled":               "false",
		"timmy.rerank_api_key":                            "",
		"timmy.rerank_base_url":                           "",
		"timmy.rerank_model":                              "",
		"timmy.rerank_provider":                           "",
		"timmy.rerank_top_k":                              "10",
		"timmy.text_embedding_api_key":                    "",
		"timmy.text_embedding_base_url":                   "",
		"timmy.text_embedding_model":                      "",
		"timmy.text_embedding_provider":                   "",
		"timmy.text_retrieval_top_k":                      "10",
		"webhooks.allow_http_targets":                     "false",
		"websocket.inactivity_timeout_seconds":            "300",
	}

	got := DefaultOperationalSettings()
	gotMap := make(map[string]string, len(got))
	for _, s := range got {
		gotMap[s.Key] = s.Value
	}

	assert.Equal(t, want, gotMap,
		"DefaultOperationalSettings() must match the pre-registry baseline exactly — "+
			"a diff here means SeedDefaults would seed a different row set on a fresh "+
			"database, or #794's origin backfill would classify existing rows differently")
	assert.Len(t, want, 70, "baseline has a fixed size; update deliberately, not by drift")
}
