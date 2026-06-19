package api

import (
	"context"
	"encoding/json"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/ericfitz/tmi/internal/slogging"
)

const decomposerSystemPromptWithCode = `You are a query decomposition assistant. Given a user question about a threat model,
generate optimized sub-queries for searching different indexes.

Text index contains: assets, threats, diagrams, documents, notes
Code index contains: repository source code

Respond with JSON only, no other text:
{"text_query": "...", "code_query": "...", "strategy": "parallel"}

If the question is only about text content, set code_query to empty string.
If the question is only about code, set text_query to empty string.
Use "parallel" strategy unless one query's results are needed to formulate the other.`

const decomposerSystemPromptWithoutCode = `You are a query decomposition assistant. Given a user question about a threat model,
generate an optimized search query for the text index.

Text index contains: assets, threats, diagrams, documents, notes

Respond with JSON only, no other text:
{"text_query": "...", "code_query": "", "strategy": "parallel"}`

// DecomposerLLM is a narrow interface for LLM single-turn generation, used by
// LLMQueryDecomposer to allow easy mocking in tests.
// SEM@f06df1eae94dd2ca361cfb88f9f58fdc2bbfced6: interface for single-turn LLM text generation used by the query decomposer (pure)
type DecomposerLLM interface {
	GenerateResponse(ctx context.Context, systemPrompt string, userMessage string) (string, error)
}

// QueryDecomposer breaks a user query into sub-queries optimized for different indexes.
// SEM@f06df1eae94dd2ca361cfb88f9f58fdc2bbfced6: interface for breaking a user query into sub-queries optimized for different indexes (pure)
type QueryDecomposer interface {
	Decompose(ctx context.Context, query string, hasCodeIndex bool) (*DecomposedQuery, error)
}

// DecomposedQuery holds the sub-queries produced by decomposing a user query.
// SEM@f06df1eae94dd2ca361cfb88f9f58fdc2bbfced6: value type holding text and code sub-queries produced by query decomposition (pure)
type DecomposedQuery struct {
	TextQuery string `json:"text_query"`
	CodeQuery string `json:"code_query"`
	Strategy  string `json:"strategy"`
}

// LLMQueryDecomposer implements QueryDecomposer using an LLM.
// SEM@f06df1eae94dd2ca361cfb88f9f58fdc2bbfced6: LLM-backed implementation of QueryDecomposer
type LLMQueryDecomposer struct {
	llm DecomposerLLM
}

// NewLLMQueryDecomposer creates a new LLMQueryDecomposer backed by the given LLM.
// SEM@f06df1eae94dd2ca361cfb88f9f58fdc2bbfced6: build a new LLMQueryDecomposer backed by the given LLM client (pure)
func NewLLMQueryDecomposer(llm DecomposerLLM) *LLMQueryDecomposer {
	return &LLMQueryDecomposer{llm: llm}
}

// Decompose sends the query to the LLM and returns optimized sub-queries.
// On any LLM or parse error it logs a warning and returns a safe fallback using
// the original query for both sub-queries.
// SEM@f06df1eae94dd2ca361cfb88f9f58fdc2bbfced6: decompose a user query into optimized text and code sub-queries via LLM, falling back to the original query on error
func (d *LLMQueryDecomposer) Decompose(ctx context.Context, query string, hasCodeIndex bool) (*DecomposedQuery, error) {
	logger := slogging.Get()

	tracer := otel.Tracer("tmi.timmy")
	ctx, span := tracer.Start(ctx, "timmy.query_decomposer.decompose",
		trace.WithAttributes(
			attribute.Bool("tmi.timmy.has_code_index", hasCodeIndex),
		),
	)
	defer span.End()

	systemPrompt := decomposerSystemPromptWithoutCode
	if hasCodeIndex {
		systemPrompt = decomposerSystemPromptWithCode
	}

	fallback := &DecomposedQuery{
		TextQuery: query,
		CodeQuery: query,
		Strategy:  "parallel",
	}
	if !hasCodeIndex {
		fallback.CodeQuery = ""
	}

	raw, err := d.llm.GenerateResponse(ctx, systemPrompt, query)
	if err != nil {
		logger.Warn("query decomposer: LLM error, using fallback: %v", err)
		return fallback, nil
	}

	extracted := extractJSON(raw)
	var result DecomposedQuery
	if err := json.Unmarshal([]byte(extracted), &result); err != nil {
		logger.Warn("query decomposer: failed to parse LLM response, using fallback: %v", err)
		return fallback, nil
	}

	if result.TextQuery == "" {
		result.TextQuery = query
	}
	if !hasCodeIndex {
		result.CodeQuery = ""
	}

	return &result, nil
}

// extractJSON attempts to extract a JSON object from s. It handles markdown
// code fences (```json...``` and ```...```) and falls back to finding the first
// '{' to last '}'. Returns s unchanged when none of these apply.
// SEM@f06df1eae94dd2ca361cfb88f9f58fdc2bbfced6: extract a JSON object from an LLM response string, stripping markdown code fences (pure)
func extractJSON(s string) string {
	// Try ```json ... ``` block
	if idx := strings.Index(s, "```json"); idx != -1 {
		start := idx + len("```json")
		if end := strings.Index(s[start:], "```"); end != -1 {
			return strings.TrimSpace(s[start : start+end])
		}
	}

	// Try ``` ... ``` block
	if idx := strings.Index(s, "```"); idx != -1 {
		start := idx + len("```")
		if end := strings.Index(s[start:], "```"); end != -1 {
			return strings.TrimSpace(s[start : start+end])
		}
	}

	// Try first { to last }
	first := strings.Index(s, "{")
	last := strings.LastIndex(s, "}")
	if first != -1 && last != -1 && last > first {
		return s[first : last+1]
	}

	return s
}
