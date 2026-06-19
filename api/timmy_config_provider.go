package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

// TimmyConfigProvider assembles a live config.TimmyConfig from the settings
// service. Reads honor config-first precedence (env/config file > database),
// which SettingsServiceInterface.GetString/GetBool/GetInt already implement.
// SEM@c7c567dc271187337d2712f95c4866c013093ecf: adapter that assembles a live TimmyConfig from the settings service with config-first precedence (pure)
type TimmyConfigProvider struct {
	settings SettingsServiceInterface
}

// NewTimmyConfigProvider constructs a provider over the given settings service.
// SEM@c7c567dc271187337d2712f95c4866c013093ecf: build a TimmyConfigProvider over the given settings service (pure)
func NewTimmyConfigProvider(settings SettingsServiceInterface) *TimmyConfigProvider {
	return &TimmyConfigProvider{settings: settings}
}

// Current reads all timmy.* keys and returns an assembled TimmyConfig. It
// starts from DefaultTimmyConfig so unset numeric knobs keep sane defaults,
// then overlays any value present in settings.
// SEM@960dc5fa7f13423b7b5dd06ca76a9f2df67be632: fetch all timmy.* settings and return an assembled TimmyConfig with defaults for unset keys (reads DB)
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
	// GetInt returns (0, nil) for both a missing key and a key explicitly set
	// to 0, so getInt cannot distinguish the two and treats 0 as "unset",
	// preserving the default. This is acceptable for Timmy because 0 is never a
	// valid runtime value for any of these knobs (e.g. chunk_overlap=0 or
	// embedding_dimension=0 are degenerate/invalid), matching the existing
	// `val > 0` convention elsewhere in the package. We deliberately do not
	// change SettingsServiceInterface to surface key presence here.
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
	getInt("timmy.embedding_cleanup_interval_minutes", &cfg.EmbeddingCleanupIntervalMinutes)
	getInt("timmy.embedding_idle_days_active", &cfg.EmbeddingIdleDaysActive)
	getInt("timmy.embedding_idle_days_closed", &cfg.EmbeddingIdleDaysClosed)
	getBool("timmy.dump_extracted_text_to_note", &cfg.DumpExtractedTextToNote)

	return cfg
}

// WiringHash returns a stable hash over the fields that, when changed, require
// rebuilding the LLM/embedding clients. Tuning knobs (top-k, limits, history)
// are intentionally excluded so changing them does not force a costly client
// rebuild. The enable flag is also excluded — it is evaluated by the
// middleware, not the client build. LLMTimeoutSeconds is INCLUDED because
// NewTimmyLLMService bakes it into the SafeHTTPClient at construction;
// OperatorSystemPrompt is INCLUDED because it is baked into the base prompt at
// construction.
// SEM@cf89687423b4b2d922619ea8e021c2f13cb32481: compute a stable hash over the config fields that require LLM client rebuild when changed (pure)
func (p *TimmyConfigProvider) WiringHash(cfg config.TimmyConfig) string {
	fields := []string{
		cfg.LLMProvider, cfg.LLMModel, cfg.LLMAPIKey, cfg.LLMBaseURL,
		cfg.TextEmbeddingProvider, cfg.TextEmbeddingModel, cfg.TextEmbeddingAPIKey, cfg.TextEmbeddingBaseURL,
		strconv.Itoa(cfg.EmbeddingDimension),
		cfg.CodeEmbeddingProvider, cfg.CodeEmbeddingModel, cfg.CodeEmbeddingAPIKey, cfg.CodeEmbeddingBaseURL,
		cfg.RerankProvider, cfg.RerankModel, cfg.RerankAPIKey, cfg.RerankBaseURL,
		cfg.OperatorSystemPrompt,
		strconv.Itoa(cfg.LLMTimeoutSeconds),
	}
	// NUL separator avoids collisions between concatenated field boundaries.
	sum := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	return hex.EncodeToString(sum[:])
}
