package config

// TimmyConfig holds configuration for the Timmy AI assistant feature
type TimmyConfig struct {
	Enabled                   bool   `yaml:"enabled" env:"TMI_TIMMY_ENABLED"`
	LLMProvider               string `yaml:"llm_provider" env:"TMI_TIMMY_LLM_PROVIDER"`
	LLMModel                  string `yaml:"llm_model" env:"TMI_TIMMY_LLM_MODEL"`
	LLMAPIKey                 string `yaml:"llm_api_key" env:"TMI_TIMMY_LLM_API_KEY"`
	EmbeddingProvider         string `yaml:"embedding_provider" env:"TMI_TIMMY_EMBEDDING_PROVIDER"`
	EmbeddingModel            string `yaml:"embedding_model" env:"TMI_TIMMY_EMBEDDING_MODEL"`
	EmbeddingAPIKey           string `yaml:"embedding_api_key" env:"TMI_TIMMY_EMBEDDING_API_KEY"`
	LLMBaseURL                string `yaml:"llm_base_url" env:"TMI_TIMMY_LLM_BASE_URL"`
	EmbeddingBaseURL          string `yaml:"embedding_base_url" env:"TMI_TIMMY_EMBEDDING_BASE_URL"`
	RetrievalTopK             int    `yaml:"retrieval_top_k" env:"TMI_TIMMY_RETRIEVAL_TOP_K"`
	MaxConversationHistory    int    `yaml:"max_conversation_history" env:"TMI_TIMMY_MAX_CONVERSATION_HISTORY"`
	OperatorSystemPrompt      string `yaml:"operator_system_prompt" env:"TMI_TIMMY_OPERATOR_SYSTEM_PROMPT"`
	MaxMemoryMB               int    `yaml:"max_memory_mb" env:"TMI_TIMMY_MAX_MEMORY_MB"`
	InactivityTimeoutSeconds  int    `yaml:"inactivity_timeout_seconds" env:"TMI_TIMMY_INACTIVITY_TIMEOUT_SECONDS"`
	MaxMessagesPerUserPerHour int    `yaml:"max_messages_per_user_per_hour" env:"TMI_TIMMY_MAX_MESSAGES_PER_USER_PER_HOUR"`
	MaxSessionsPerThreatModel int    `yaml:"max_sessions_per_threat_model" env:"TMI_TIMMY_MAX_SESSIONS_PER_THREAT_MODEL"`
	MaxConcurrentLLMRequests  int    `yaml:"max_concurrent_llm_requests" env:"TMI_TIMMY_MAX_CONCURRENT_LLM_REQUESTS"`
	ChunkSize                 int    `yaml:"chunk_size" env:"TMI_TIMMY_CHUNK_SIZE"`
	ChunkOverlap              int    `yaml:"chunk_overlap" env:"TMI_TIMMY_CHUNK_OVERLAP"`
	LLMTimeoutSeconds         int    `yaml:"llm_timeout_seconds" env:"TMI_TIMMY_LLM_TIMEOUT_SECONDS"`
}

// DefaultTimmyConfig returns configuration with sensible defaults
func DefaultTimmyConfig() TimmyConfig {
	return TimmyConfig{
		Enabled:                   false,
		RetrievalTopK:             10,
		MaxConversationHistory:    50,
		MaxMemoryMB:               256,
		InactivityTimeoutSeconds:  3600,
		MaxMessagesPerUserPerHour: 60,
		MaxSessionsPerThreatModel: 50,
		MaxConcurrentLLMRequests:  10,
		ChunkSize:                 512,
		ChunkOverlap:              50,
		LLMTimeoutSeconds:         120,
	}
}

// IsConfigured returns true if the required LLM and embedding providers are configured
func (tc TimmyConfig) IsConfigured() bool {
	return tc.LLMProvider != "" && tc.LLMModel != "" &&
		tc.EmbeddingProvider != "" && tc.EmbeddingModel != ""
}
