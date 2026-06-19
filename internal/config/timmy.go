package config

// TimmyConfig holds configuration for the Timmy AI assistant feature
// SEM@07385154fa2286de1a8805dbf00575c0f52ce941: configuration struct for the Timmy AI assistant feature (pure)
type TimmyConfig struct {
	Enabled                         bool   `yaml:"enabled" env:"TMI_TIMMY_ENABLED"`
	LLMProvider                     string `yaml:"llm_provider" env:"TMI_TIMMY_LLM_PROVIDER"`
	LLMModel                        string `yaml:"llm_model" env:"TMI_TIMMY_LLM_MODEL"`
	LLMAPIKey                       string `yaml:"llm_api_key" env:"TMI_TIMMY_LLM_API_KEY"`
	LLMBaseURL                      string `yaml:"llm_base_url" env:"TMI_TIMMY_LLM_BASE_URL"`
	TextEmbeddingProvider           string `yaml:"text_embedding_provider" env:"TMI_TIMMY_TEXT_EMBEDDING_PROVIDER"`
	TextEmbeddingModel              string `yaml:"text_embedding_model" env:"TMI_TIMMY_TEXT_EMBEDDING_MODEL"`
	TextEmbeddingAPIKey             string `yaml:"text_embedding_api_key" env:"TMI_TIMMY_TEXT_EMBEDDING_API_KEY"`
	TextEmbeddingBaseURL            string `yaml:"text_embedding_base_url" env:"TMI_TIMMY_TEXT_EMBEDDING_BASE_URL"`
	EmbeddingDimension              int    `yaml:"embedding_dimension" env:"TMI_TIMMY_EMBEDDING_DIMENSION"`
	TextRetrievalTopK               int    `yaml:"text_retrieval_top_k" env:"TMI_TIMMY_TEXT_RETRIEVAL_TOP_K"`
	CodeEmbeddingProvider           string `yaml:"code_embedding_provider" env:"TMI_TIMMY_CODE_EMBEDDING_PROVIDER"`
	CodeEmbeddingModel              string `yaml:"code_embedding_model" env:"TMI_TIMMY_CODE_EMBEDDING_MODEL"`
	CodeEmbeddingAPIKey             string `yaml:"code_embedding_api_key" env:"TMI_TIMMY_CODE_EMBEDDING_API_KEY"`
	CodeEmbeddingBaseURL            string `yaml:"code_embedding_base_url" env:"TMI_TIMMY_CODE_EMBEDDING_BASE_URL"`
	CodeRetrievalTopK               int    `yaml:"code_retrieval_top_k" env:"TMI_TIMMY_CODE_RETRIEVAL_TOP_K"`
	QueryDecompositionEnabled       bool   `yaml:"query_decomposition_enabled" env:"TMI_TIMMY_QUERY_DECOMPOSITION_ENABLED"`
	RerankProvider                  string `yaml:"rerank_provider" env:"TMI_TIMMY_RERANK_PROVIDER"`
	RerankModel                     string `yaml:"rerank_model" env:"TMI_TIMMY_RERANK_MODEL"`
	RerankAPIKey                    string `yaml:"rerank_api_key" env:"TMI_TIMMY_RERANK_API_KEY"`
	RerankBaseURL                   string `yaml:"rerank_base_url" env:"TMI_TIMMY_RERANK_BASE_URL"`
	RerankTopK                      int    `yaml:"rerank_top_k" env:"TMI_TIMMY_RERANK_TOP_K"`
	MaxConversationHistory          int    `yaml:"max_conversation_history" env:"TMI_TIMMY_MAX_CONVERSATION_HISTORY"`
	OperatorSystemPrompt            string `yaml:"operator_system_prompt" env:"TMI_TIMMY_OPERATOR_SYSTEM_PROMPT"`
	MaxMemoryMB                     int    `yaml:"max_memory_mb" env:"TMI_TIMMY_MAX_MEMORY_MB"`
	InactivityTimeoutSeconds        int    `yaml:"inactivity_timeout_seconds" env:"TMI_TIMMY_INACTIVITY_TIMEOUT_SECONDS"`
	MaxMessagesPerUserPerHour       int    `yaml:"max_messages_per_user_per_hour" env:"TMI_TIMMY_MAX_MESSAGES_PER_USER_PER_HOUR"`
	MaxSessionsPerThreatModel       int    `yaml:"max_sessions_per_threat_model" env:"TMI_TIMMY_MAX_SESSIONS_PER_THREAT_MODEL"`
	MaxConcurrentLLMRequests        int    `yaml:"max_concurrent_llm_requests" env:"TMI_TIMMY_MAX_CONCURRENT_LLM_REQUESTS"`
	ChunkSize                       int    `yaml:"chunk_size" env:"TMI_TIMMY_CHUNK_SIZE"`
	ChunkOverlap                    int    `yaml:"chunk_overlap" env:"TMI_TIMMY_CHUNK_OVERLAP"`
	LLMTimeoutSeconds               int    `yaml:"llm_timeout_seconds" env:"TMI_TIMMY_LLM_TIMEOUT_SECONDS"`
	EmbeddingCleanupIntervalMinutes int    `yaml:"embedding_cleanup_interval_minutes" env:"TMI_TIMMY_EMBEDDING_CLEANUP_INTERVAL_MINUTES"`
	EmbeddingIdleDaysActive         int    `yaml:"embedding_idle_days_active" env:"TMI_TIMMY_EMBEDDING_IDLE_DAYS_ACTIVE"`
	EmbeddingIdleDaysClosed         int    `yaml:"embedding_idle_days_closed" env:"TMI_TIMMY_EMBEDDING_IDLE_DAYS_CLOSED"`

	// DumpExtractedTextToNote, when true, persists the markdown produced by
	// each successful content extraction as a Note on the parent threat
	// model. Dev/test inspection aid only — the server refuses to start
	// with this flag enabled if Auth.BuildMode == "production".
	DumpExtractedTextToNote bool `yaml:"dump_extracted_text_to_note" env:"TMI_TIMMY_DUMP_EXTRACTED_TEXT_TO_NOTE"`
}

// DefaultTimmyConfig returns configuration with sensible defaults
// SEM@e2a2db17eb103a24591f87c9d0932532a5750588: build a TimmyConfig populated with sensible operational defaults (pure)
func DefaultTimmyConfig() TimmyConfig {
	return TimmyConfig{
		Enabled:                         false,
		TextRetrievalTopK:               10,
		CodeRetrievalTopK:               10,
		RerankTopK:                      10,
		MaxConversationHistory:          50,
		MaxMemoryMB:                     256,
		InactivityTimeoutSeconds:        3600,
		MaxMessagesPerUserPerHour:       60,
		MaxSessionsPerThreatModel:       50,
		MaxConcurrentLLMRequests:        10,
		ChunkSize:                       512,
		ChunkOverlap:                    50,
		LLMTimeoutSeconds:               120,
		EmbeddingCleanupIntervalMinutes: 60,
		EmbeddingIdleDaysActive:         30,
		EmbeddingIdleDaysClosed:         7,
	}
}

// IsConfigured returns true if the required LLM and text embedding providers are configured
// SEM@37a05c9c7bcde023781ade490d31e55313f5a793: validate that LLM and text embedding providers are configured (pure)
func (tc TimmyConfig) IsConfigured() bool {
	return tc.LLMProvider != "" && tc.LLMModel != "" &&
		tc.TextEmbeddingProvider != "" && tc.TextEmbeddingModel != ""
}

// IsCodeIndexConfigured returns true if the code embedding provider and model are configured
// SEM@37a05c9c7bcde023781ade490d31e55313f5a793: validate that code embedding provider and model are configured (pure)
func (tc TimmyConfig) IsCodeIndexConfigured() bool {
	return tc.CodeEmbeddingProvider != "" && tc.CodeEmbeddingModel != ""
}

// IsRerankConfigured returns true if the reranker provider and model are configured
// SEM@cd3e5390e6efba1b5a645e4a00ef2a5b842d39ff: validate that reranker provider and model are configured (pure)
func (tc TimmyConfig) IsRerankConfigured() bool {
	return tc.RerankProvider != "" && tc.RerankModel != ""
}
