package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/config"
	tmiotel "github.com/ericfitz/tmi/internal/otel"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/tmc/langchaingo/embeddings"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
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

	// Create HTTP client with configurable timeout (default 120s)
	// LangChainGo's default is 30s which is too short for large conversation contexts
	timeoutSec := cfg.LLMTimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	httpClient := &http.Client{
		Timeout:   time.Duration(timeoutSec) * time.Second,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	// Create chat model using openai.New with functional options
	chatOpts := []openai.Option{
		openai.WithModel(cfg.LLMModel),
		openai.WithToken(cfg.LLMAPIKey),
		openai.WithHTTPClient(httpClient),
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
		openai.WithModel(cfg.TextEmbeddingModel),
		openai.WithToken(cfg.TextEmbeddingAPIKey),
		openai.WithEmbeddingModel(cfg.TextEmbeddingModel),
		openai.WithHTTPClient(httpClient),
	}
	if cfg.TextEmbeddingBaseURL != "" {
		embOpts = append(embOpts, openai.WithBaseURL(cfg.TextEmbeddingBaseURL))
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
	tracer := otel.Tracer("tmi.timmy")
	ctx, embedSpan := tracer.Start(ctx, "timmy.embedding.generate",
		trace.WithAttributes(
			attribute.String("tmi.timmy.embedding_model", s.config.TextEmbeddingModel),
			attribute.Int("tmi.timmy.text_count", len(texts)),
		),
	)
	defer embedSpan.End()

	embedStart := time.Now()
	vectors, err := s.embedder.EmbedDocuments(ctx, texts)
	embedDuration := time.Since(embedStart)
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}

	if m := tmiotel.GlobalMetrics; m != nil {
		m.TimmyEmbedDuration.Record(ctx, embedDuration.Seconds())
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

	tracer := otel.Tracer("tmi.timmy")
	ctx, llmSpan := tracer.Start(ctx, "timmy.llm.generate",
		trace.WithAttributes(
			attribute.String("tmi.timmy.model", s.config.LLMModel),
		),
	)

	allMessages := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, systemPrompt),
	}
	allMessages = append(allMessages, messages...)

	var fullResponse strings.Builder
	tokenCount := 0

	llmStart := time.Now()
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
	llmDuration := time.Since(llmStart)
	llmSpan.SetAttributes(attribute.Int("tmi.timmy.token_count", tokenCount))
	llmSpan.End()
	if err != nil {
		logger.Error("LLM generation failed: %v", err)
		return "", 0, fmt.Errorf("LLM generation failed: %w", err)
	}

	if m := tmiotel.GlobalMetrics; m != nil {
		m.TimmyLLMDuration.Record(ctx, llmDuration.Seconds())
		m.TimmyLLMTokens.Add(ctx, int64(tokenCount), metric.WithAttributes(attribute.String("direction", "completion")))
	}

	return fullResponse.String(), tokenCount, nil
}

// GetBasePrompt returns the system prompt (base + operator extension)
func (s *TimmyLLMService) GetBasePrompt() string {
	return s.basePrompt
}
