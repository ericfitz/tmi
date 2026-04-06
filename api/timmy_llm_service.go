package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/tmc/langchaingo/embeddings"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

const timmyBasePrompt = `You are Timmy, a security analysis assistant for threat modeling. You help users understand, analyze, and improve their threat models.

Your role:
- Analyze threats and identify gaps in security coverage
- Explain data flows and attack surfaces based on the threat model's diagrams and assets
- Suggest mitigations based on identified threats
- Answer questions about any aspect of the threat model
- Summarize content across multiple entities

Your rules:
- Always ground your responses in the threat model data provided. Cite specific entities by name.
- Clearly distinguish between facts from the threat model and your general security knowledge.
- Never fabricate CVE numbers, CVSS scores, or threat identifiers that aren't in the source data.
- If information is insufficient to answer a question, say so rather than speculating.`

// TimmyLLMService provides LLM chat and embedding capabilities via LangChainGo
type TimmyLLMService struct {
	chatModel  llms.Model
	embedder   embeddings.Embedder
	config     config.TimmyConfig
	basePrompt string
}

// NewTimmyLLMService creates a new LLM service from configuration
func NewTimmyLLMService(cfg config.TimmyConfig) (*TimmyLLMService, error) {
	if !cfg.IsConfigured() {
		return nil, fmt.Errorf("timmy LLM/embedding providers not configured")
	}

	// Create chat model using openai.New with functional options
	chatOpts := []openai.Option{
		openai.WithModel(cfg.LLMModel),
		openai.WithToken(cfg.LLMAPIKey),
	}
	if cfg.LLMBaseURL != "" {
		chatOpts = append(chatOpts, openai.WithBaseURL(cfg.LLMBaseURL))
	}
	chatModel, err := openai.New(chatOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM chat model: %w", err)
	}

	// Create a separate LLM client configured for embeddings
	embOpts := []openai.Option{
		openai.WithModel(cfg.EmbeddingModel),
		openai.WithToken(cfg.EmbeddingAPIKey),
		openai.WithEmbeddingModel(cfg.EmbeddingModel),
	}
	if cfg.EmbeddingBaseURL != "" {
		embOpts = append(embOpts, openai.WithBaseURL(cfg.EmbeddingBaseURL))
	}
	embLLM, err := openai.New(embOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding model: %w", err)
	}

	// Wrap the embedding-capable LLM in an Embedder
	embedder, err := embeddings.NewEmbedder(embLLM)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	prompt := timmyBasePrompt
	if cfg.OperatorSystemPrompt != "" {
		prompt = prompt + "\n\n" + cfg.OperatorSystemPrompt
	}

	return &TimmyLLMService{
		chatModel:  chatModel,
		embedder:   embedder,
		config:     cfg,
		basePrompt: prompt,
	}, nil
}

// EmbedTexts returns embeddings for the given texts
func (s *TimmyLLMService) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	vectors, err := s.embedder.EmbedDocuments(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}
	return vectors, nil
}

// EmbeddingDimension returns the dimension by embedding a test string
func (s *TimmyLLMService) EmbeddingDimension(ctx context.Context) (int, error) {
	vectors, err := s.EmbedTexts(ctx, []string{"dimension test"})
	if err != nil {
		return 0, err
	}
	if len(vectors) == 0 {
		return 0, fmt.Errorf("no embedding returned")
	}
	return len(vectors[0]), nil
}

// GenerateStreamingResponse sends a chat request and streams tokens via callback.
// It returns the full response text, an approximate token count, and any error.
func (s *TimmyLLMService) GenerateStreamingResponse(
	ctx context.Context,
	systemPrompt string,
	messages []llms.MessageContent,
	onToken func(token string),
) (string, int, error) {
	logger := slogging.Get()

	allMessages := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, systemPrompt),
	}
	allMessages = append(allMessages, messages...)

	var fullResponse strings.Builder
	tokenCount := 0

	_, err := s.chatModel.GenerateContent(ctx, allMessages,
		llms.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
			token := string(chunk)
			fullResponse.WriteString(token)
			tokenCount++
			if onToken != nil {
				onToken(token)
			}
			return nil
		}),
	)
	if err != nil {
		logger.Error("LLM generation failed: %v", err)
		return "", 0, fmt.Errorf("LLM generation failed: %w", err)
	}

	return fullResponse.String(), tokenCount, nil
}

// GetBasePrompt returns the system prompt (base + operator extension)
func (s *TimmyLLMService) GetBasePrompt() string {
	return s.basePrompt
}
