package api

import (
	"context"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

// TimmyConfigProvider assembles a live config.TimmyConfig from the settings
// service. Reads honor config-first precedence (env/config file > database),
// which SettingsServiceInterface.GetString/GetBool/GetInt already implement.
type TimmyConfigProvider struct {
	settings SettingsServiceInterface
}

// NewTimmyConfigProvider constructs a provider over the given settings service.
func NewTimmyConfigProvider(settings SettingsServiceInterface) *TimmyConfigProvider {
	return &TimmyConfigProvider{settings: settings}
}

// Current reads all timmy.* keys and returns an assembled TimmyConfig. It
// starts from DefaultTimmyConfig so unset numeric knobs keep sane defaults,
// then overlays any value present in settings.
func (p *TimmyConfigProvider) Current(ctx context.Context) config.TimmyConfig {
	cfg := config.DefaultTimmyConfig()
	if p.settings == nil {
		return cfg
	}
	logger := slogging.Get()

	getStr := func(key string, dst *string) {
		if v, err := p.settings.GetString(ctx, key); err == nil {
			if v != "" {
				*dst = v
			}
		} else {
			logger.Warn("TimmyConfigProvider: read %s failed: %v", key, err)
		}
	}
	getInt := func(key string, dst *int) {
		if v, err := p.settings.GetInt(ctx, key); err == nil {
			if v != 0 {
				*dst = v
			}
		} else {
			logger.Warn("TimmyConfigProvider: read %s failed: %v", key, err)
		}
	}
	getBool := func(key string, dst *bool) {
		if v, err := p.settings.GetBool(ctx, key); err == nil {
			*dst = v
		} else {
			logger.Warn("TimmyConfigProvider: read %s failed: %v", key, err)
		}
	}

	getBool("timmy.enabled", &cfg.Enabled)
	getStr("timmy.llm_provider", &cfg.LLMProvider)
	getStr("timmy.llm_model", &cfg.LLMModel)
	getStr("timmy.llm_api_key", &cfg.LLMAPIKey)
	getStr("timmy.llm_base_url", &cfg.LLMBaseURL)
	getStr("timmy.text_embedding_provider", &cfg.TextEmbeddingProvider)
	getStr("timmy.text_embedding_model", &cfg.TextEmbeddingModel)
	getStr("timmy.text_embedding_api_key", &cfg.TextEmbeddingAPIKey)
	getStr("timmy.text_embedding_base_url", &cfg.TextEmbeddingBaseURL)
	getInt("timmy.embedding_dimension", &cfg.EmbeddingDimension)
	getInt("timmy.text_retrieval_top_k", &cfg.TextRetrievalTopK)
	getStr("timmy.code_embedding_provider", &cfg.CodeEmbeddingProvider)
	getStr("timmy.code_embedding_model", &cfg.CodeEmbeddingModel)
	getStr("timmy.code_embedding_api_key", &cfg.CodeEmbeddingAPIKey)
	getStr("timmy.code_embedding_base_url", &cfg.CodeEmbeddingBaseURL)
	getInt("timmy.code_retrieval_top_k", &cfg.CodeRetrievalTopK)
	getBool("timmy.query_decomposition_enabled", &cfg.QueryDecompositionEnabled)
	getStr("timmy.rerank_provider", &cfg.RerankProvider)
	getStr("timmy.rerank_model", &cfg.RerankModel)
	getStr("timmy.rerank_api_key", &cfg.RerankAPIKey)
	getStr("timmy.rerank_base_url", &cfg.RerankBaseURL)
	getInt("timmy.rerank_top_k", &cfg.RerankTopK)
	getInt("timmy.max_conversation_history", &cfg.MaxConversationHistory)
	getStr("timmy.operator_system_prompt", &cfg.OperatorSystemPrompt)
	getInt("timmy.max_memory_mb", &cfg.MaxMemoryMB)
	getInt("timmy.inactivity_timeout_seconds", &cfg.InactivityTimeoutSeconds)
	getInt("timmy.max_messages_per_user_per_hour", &cfg.MaxMessagesPerUserPerHour)
	getInt("timmy.max_sessions_per_threat_model", &cfg.MaxSessionsPerThreatModel)
	getInt("timmy.max_concurrent_llm_requests", &cfg.MaxConcurrentLLMRequests)
	getInt("timmy.chunk_size", &cfg.ChunkSize)
	getInt("timmy.chunk_overlap", &cfg.ChunkOverlap)
	getInt("timmy.llm_timeout_seconds", &cfg.LLMTimeoutSeconds)

	return cfg
}
