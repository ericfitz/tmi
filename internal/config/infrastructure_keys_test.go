// internal/config/infrastructure_keys_test.go
package config

import "testing"

func TestIsInfrastructureKey(t *testing.T) {
	tests := []struct {
		key            string
		infrastructure bool
	}{
		// Infrastructure keys - must always come from file/env
		{"logging.level", true},
		{"logging.is_dev", true},
		{"logging.log_dir", true},
		{"observability.enabled", true},
		{"observability.sampling_rate", true},
		{"database.url", true},
		{"database.connection_pool.max_open_conns", true},
		{"database.redis.host", true},
		{"database.redis.password", true},
		{"server.port", true},
		{"server.interface", true},
		{"server.tls_enabled", true},
		{"server.tls_cert_file", true},
		{"server.tls_key_file", true},
		{"server.tls_subject_name", true},
		{"server.cors.allowed_origins", true},
		{"server.trusted_proxies", true},
		{"server.http_to_https_redirect", true},
		{"server.read_timeout", true},
		{"server.write_timeout", true},
		{"server.idle_timeout", true},
		{"secrets.provider", true},
		{"secrets.vault_address", true},
		{"secrets.oci_vault_id", true},
		{"auth.build_mode", true},
		{"auth.jwt.secret", true},
		{"auth.jwt.signing_method", true},
		{"administrators", true},
		// DB-eligible keys - can be stored in database
		{"auth.jwt.expiration_seconds", false},
		{"auth.jwt.refresh_token_days", false},
		{"auth.jwt.session_lifetime_days", false},
		{"auth.auto_promote_first_user", false},
		{"auth.everyone_is_a_reviewer", false},
		{"auth.cookie.enabled", false},
		{"auth.cookie.domain", false},
		{"auth.cookie.secure", false},
		{"auth.oauth.providers.google.enabled", false},
		{"auth.oauth.providers.google.client_id", false},
		{"auth.oauth_callback_url", false},
		{"auth.saml.providers.okta.enabled", false},
		{"features.saml_enabled", false},
		{"features.webhooks_enabled", false},
		{"websocket.inactivity_timeout_seconds", false},
		{"operator.name", false},
		{"operator.contact", false},
		{"session.timeout_minutes", false},
		{"rate_limit.requests_per_minute", false},
		{"ui.default_theme", false},
		{"upload.max_file_size_mb", false},
		// Edge cases
		{"server.base_url", false},
		{"server.rate_limit_public_rpm", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := IsInfrastructureKey(tt.key)
			if got != tt.infrastructure {
				t.Errorf("IsInfrastructureKey(%q) = %v, want %v", tt.key, got, tt.infrastructure)
			}
		})
	}
}
