package config

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// transitionalKeys is the golden list of operational settings that still have
// a config-file or environment delivery path, pending Phase E of the config
// model redesign (docs/superpowers/specs/2026-08-22-config-model-redesign-design.md).
//
// This list may only SHRINK. Adding a key here means adding a new config/env
// path for a database-only setting, which is the thing the redesign exists to
// remove. When it reaches zero, Phase E is complete and goal 2 is enforced.
//
// Populate it in Step 2 from the test's own failure output.
var transitionalKeys = []string{
	"administrators",
	"auth.auto_promote_first_user",
	"auth.cookie.domain",
	"auth.cookie.enabled",
	"auth.cookie.secure",
	"auth.everyone_is_a_reviewer",
	"auth.jwt.expiration_seconds",
	"auth.jwt.refresh_token_days",
	"auth.jwt.session_lifetime_days",
	"auth.oauth.client_callback_allowlist",
	"auth.oauth_callback_url",
	"auth.step_up_window_seconds",
	"content_extractors.compressed_size_bytes",
	"content_extractors.decompressed_size_bytes",
	"content_extractors.markdown_size_bytes",
	"content_extractors.part_size_bytes",
	"content_extractors.per_user_concurrency_default",
	"content_extractors.pptx_slides",
	"content_extractors.wall_clock_budget",
	"content_extractors.xlsx_cells",
	"content_oauth.callback_url",
	"content_sources.confluence.enabled",
	"content_sources.google_drive.browser_oauth_client_id",
	"content_sources.google_drive.credentials_file",
	"content_sources.google_drive.enabled",
	"content_sources.google_drive.picker_app_id",
	"content_sources.google_drive.picker_developer_key",
	"content_sources.google_drive.service_account_email",
	"content_sources.google_workspace.enabled",
	"content_sources.google_workspace.picker_app_id",
	"content_sources.google_workspace.picker_developer_key",
	"content_sources.microsoft.application_object_id",
	"content_sources.microsoft.client_id",
	"content_sources.microsoft.enabled",
	"content_sources.microsoft.picker_origin",
	"content_sources.microsoft.tenant_id",
	"extraction.async_enabled",
	"features.saml_enabled",
	"observability.enabled",
	"observability.prometheus_port",
	"observability.sampling_rate",
	"operator.contact",
	"operator.jurisdiction",
	"operator.name",
	"server.disable_rate_limiting",
	"server.ratelimit_public_rpm",
	"server.require_if_match",
	"session.timeout_minutes",
	"ssrf.document_uri.allowlist",
	"ssrf.document_uri.schemes",
	"ssrf.issue_uri.allowlist",
	"ssrf.issue_uri.schemes",
	"ssrf.repository_uri.allowlist",
	"ssrf.repository_uri.schemes",
	"ssrf.timmy.allowlist",
	"ssrf.timmy.schemes",
	"ssrf.webhook.allowlist",
	"ssrf.webhook.schemes",
	"timmy.chunk_overlap",
	"timmy.chunk_size",
	"timmy.code_embedding_api_key",
	"timmy.code_embedding_base_url",
	"timmy.code_embedding_model",
	"timmy.code_embedding_provider",
	"timmy.code_retrieval_top_k",
	"timmy.dump_extracted_text_to_note",
	"timmy.embedding_cleanup_interval_minutes",
	"timmy.embedding_dimension",
	"timmy.embedding_idle_days_active",
	"timmy.embedding_idle_days_closed",
	"timmy.enabled",
	"timmy.inactivity_timeout_seconds",
	"timmy.llm_api_key",
	"timmy.llm_base_url",
	"timmy.llm_max_tokens",
	"timmy.llm_model",
	"timmy.llm_provider",
	"timmy.llm_timeout_seconds",
	"timmy.max_concurrent_llm_requests",
	"timmy.max_conversation_history",
	"timmy.max_memory_mb",
	"timmy.max_messages_per_user_per_hour",
	"timmy.max_sessions_per_threat_model",
	"timmy.operator_system_prompt",
	"timmy.query_decomposition_enabled",
	"timmy.rerank_api_key",
	"timmy.rerank_base_url",
	"timmy.rerank_model",
	"timmy.rerank_provider",
	"timmy.rerank_top_k",
	"timmy.text_embedding_api_key",
	"timmy.text_embedding_base_url",
	"timmy.text_embedding_model",
	"timmy.text_embedding_provider",
	"timmy.text_retrieval_top_k",
	"webhooks.allow_http_targets",
	"websocket.inactivity_timeout_seconds",
}

func TestTransitionalKeys_MatchGoldenListExactly(t *testing.T) {
	var actual []string
	for _, d := range AllSettingDefs() {
		if d.Transitional {
			actual = append(actual, d.Key)
		}
	}
	sort.Strings(actual)

	want := append([]string{}, transitionalKeys...)
	sort.Strings(want)

	assert.Equal(t, want, actual,
		"the transitional list may only shrink: removing a key means its config/env "+
			"path is gone (good); adding one means a new config/env path was introduced "+
			"for a database-only setting (not allowed)")
}

func TestTransitionalKeys_AreAllOperational(t *testing.T) {
	for _, d := range AllSettingDefs() {
		if d.Transitional {
			assert.Equal(t, CategoryOperational, d.Class.Category,
				"%s is Transitional but not operational", d.Key)
		}
	}
}
