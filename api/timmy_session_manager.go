package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/tmc/langchaingo/llms"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// SourceSnapshotEntry represents a single entity included in a Timmy session's source snapshot.
//
// URI is set only for entities whose content lives at an external URL (today: documents).
// DB-resident entities (notes, assets, threats, repositories) leave it empty so the embedding
// registry routes them to DirectTextProvider rather than the URI-driven content pipeline.
// SEM@bd0bc10d5d1f787df91f432d9ae0dd6313a302c6: represents a threat-model entity included in a Timmy session source snapshot (pure)
type SourceSnapshotEntry struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Name       string `json:"name"`
	URI        string `json:"uri,omitempty"`
}

// SessionProgressCallback reports progress during session creation phases
// SEM@9ac60db5b7f349b340ab9963342361d7b81db7e2: callback type reporting phase progress during Timmy session creation (pure)
type SessionProgressCallback func(phase, entityType, entityName string, progress int, detail string)

// MessageStatusCallback reports pre-token-stream phase transitions during
// HandleMessage so the client can surface "Timmy is …" affordances
// (loading embeddings, querying, waiting for LLM, …) instead of a
// generic spinner. `phase` is a stable snake_case identifier; the rest
// are optional and may be empty. See OpenAPI `createTimmyChatMessage`
// for the documented shape.
// SEM@f904455aae213e6a2855645d19e4aa64e39619e6: callback type reporting pre-stream phase transitions during Timmy message handling (pure)
type MessageStatusCallback func(phase, entityType, entityName, detail string)

// TimmySessionManager orchestrates Timmy session and message lifecycle,
// wiring together LLM, vector index, content providers, and rate limiting
// SEM@63d2546d6591e57d65783c3032d4412409c2b328: orchestrates Timmy session lifecycle, wiring LLM, vector index, embeddings, and rate limiting (mutates shared state)
type TimmySessionManager struct {
	config           config.TimmyConfig
	llmService       *TimmyLLMService
	vectorManager    *VectorIndexManager
	providerRegistry *EmbeddingSourceRegistry
	contextBuilder   *ContextBuilder
	rateLimiter      *TimmyRateLimiter
	reranker         Reranker                     // nil if not configured
	decomposer       QueryDecomposer              // nil if not enabled
	stampedConfig    config.StampedConfigProvider // nil until SetStampedConfigProvider is called
	// liveConfig reads current tuning knobs (top-k, session caps, conversation
	// history, chunk sizes) per request from the database. nil when not wired
	// (unit tests), in which case cfgFor falls back to the frozen config.
	liveConfig func(ctx context.Context) config.TimmyConfig
}

// NewTimmySessionManager creates a new session manager with all required dependencies
// SEM@63d2546d6591e57d65783c3032d4412409c2b328: build a TimmySessionManager with all required LLM, vector, and rate-limit dependencies (pure)
func NewTimmySessionManager(
	cfg config.TimmyConfig,
	llm *TimmyLLMService,
	vm *VectorIndexManager,
	registry *EmbeddingSourceRegistry,
	rl *TimmyRateLimiter,
	reranker Reranker,
	decomposer QueryDecomposer,
) *TimmySessionManager {
	return &TimmySessionManager{
		config:           cfg,
		llmService:       llm,
		vectorManager:    vm,
		providerRegistry: registry,
		contextBuilder:   NewContextBuilder(),
		rateLimiter:      rl,
		reranker:         reranker,
		decomposer:       decomposer,
	}
}

// SetStampedConfigProvider wires the shared-invariant embedding profile into
// the session manager. Once set, the query path reads the embedding model
// through the provider so that ingest and query always see the same value.
// This mirrors the SetSettingsService wiring pattern used elsewhere in the
// server startup sequence.
// SEM@534d697cb5ef139c97865f31e32bd7d0b6af458f: wire the shared embedding profile provider so ingest and query use the same model (mutates shared state)
func (sm *TimmySessionManager) SetStampedConfigProvider(p config.StampedConfigProvider) {
	sm.stampedConfig = p
}

// SetLiveConfig wires a live-config reader so tuning knobs (top-k, session
// caps, conversation history, chunk sizes) are read per request from the
// database instead of the build-time snapshot. When unset, the session
// manager falls back to its frozen config (used by unit tests).
// SEM@63d2546d6591e57d65783c3032d4412409c2b328: wire a per-request live config reader for tuning knobs instead of the build-time snapshot (mutates shared state)
func (sm *TimmySessionManager) SetLiveConfig(read func(ctx context.Context) config.TimmyConfig) {
	sm.liveConfig = read
}

// cfgFor returns the live TimmyConfig when a live-config reader is wired,
// else the build-time snapshot. Used for tuning knobs that may change at
// runtime without an LLM-client rebuild.
// SEM@63d2546d6591e57d65783c3032d4412409c2b328: return live TimmyConfig for the request context, falling back to the frozen config (pure)
func (sm *TimmySessionManager) cfgFor(ctx context.Context) config.TimmyConfig {
	if sm.liveConfig != nil {
		return sm.liveConfig(ctx)
	}
	return sm.config
}

// expectedEmbeddingModel returns the embedding model the query path must use
// for the given index type, read through the stamped config provider.
// Falls back to the static config when no provider has been wired (e.g. in
// unit tests that build a TimmySessionManager without a provider).
// SEM@534d697cb5ef139c97865f31e32bd7d0b6af458f: fetch the expected embedding model name for an index type from the stamped config provider
func (sm *TimmySessionManager) expectedEmbeddingModel(ctx context.Context, indexType string) (string, error) {
	if sm.stampedConfig == nil {
		// Fallback for code paths that have not been wired with a provider
		// (e.g. some unit tests): use the static config.
		if indexType == IndexTypeCode {
			return sm.config.CodeEmbeddingModel, nil
		}
		return sm.config.TextEmbeddingModel, nil
	}
	// StampedConfig.Embedding currently carries only the TEXT embedding model;
	// code embeddings are not yet tracked through the shared-invariant
	// mechanism, so the provider-wired path returns the text model for both
	// index types. The nil-fallback above still honors the code/text split.
	sc, err := sm.stampedConfig.Get(ctx)
	if err != nil {
		return "", err
	}
	return sc.Embedding.Model, nil
}

// CreateSession creates a new Timmy chat session for a user and threat model.
// It snapshots timmy-enabled entities, creates the session record, and
// optionally prepares the vector index (if LLM service is configured).
// Returns the created session, any skipped sources, and an error.
// SEM@63d2546d6591e57d65783c3032d4412409c2b328: build a Timmy session: snapshot entities, persist session record, and prepare vector index (reads DB)
func (sm *TimmySessionManager) CreateSession(
	ctx context.Context,
	userID, threatModelID, title string,
	progress SessionProgressCallback,
) (*models.TimmySession, []SkippedSource, error) {
	logger := slogging.Get()

	// Check session count limit
	activeCount, err := GlobalTimmySessionStore.CountActiveByThreatModel(ctx, threatModelID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to count active sessions: %w", err)
	}
	c := sm.cfgFor(ctx)
	if activeCount >= c.MaxSessionsPerThreatModel {
		return nil, nil, &RequestError{
			Status:  429,
			Code:    "session_limit_exceeded",
			Message: fmt.Sprintf("threat model has reached the maximum of %d active sessions", c.MaxSessionsPerThreatModel),
		}
	}

	tracer := otel.Tracer("tmi.timmy")

	// Snapshot timmy-enabled sources
	if progress != nil {
		progress("snapshot", "", "", 0, "scanning entities")
	}
	ctx, snapshotSpan := tracer.Start(ctx, "timmy.session.snapshot")
	sources, skipped, err := sm.snapshotSources(ctx, threatModelID)
	snapshotSpan.End()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to snapshot sources: %w", err)
	}
	if progress != nil {
		progress("snapshot", "", "", 100, fmt.Sprintf("found %d entities", len(sources)))
	}

	// Serialize snapshot to JSON
	snapshotJSON, err := json.Marshal(sources)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal source snapshot: %w", err)
	}

	// Create session record
	session := &models.TimmySession{
		ThreatModelID:  models.DBVarchar(threatModelID),
		UserID:         models.DBVarchar(userID),
		Title:          models.DBVarchar(title),
		SourceSnapshot: models.JSONRaw(snapshotJSON),
		Status:         "active",
	}

	if err := GlobalTimmySessionStore.Create(ctx, session); err != nil {
		return nil, nil, fmt.Errorf("failed to create session: %w", err)
	}

	logger.Info("Created Timmy session %s for user %s on threat model %s with %d sources",
		session.ID, userID, threatModelID, len(sources))

	// Prepare vector index if LLM service is available
	if sm.llmService != nil && sm.vectorManager != nil {
		textSources, codeSources := splitSourcesByIndexType(sources)

		ctx, indexSpan := tracer.Start(ctx, "timmy.session.index_prepare")
		indexErr := sm.prepareVectorIndex(ctx, threatModelID, IndexTypeText, textSources, progress)
		indexSpan.End()
		if indexErr != nil {
			logger.Warn("Failed to prepare text vector index for session %s: %v", session.ID, indexErr)
		}

		if sm.config.IsCodeIndexConfigured() && len(codeSources) > 0 {
			ctx, codeIndexSpan := tracer.Start(ctx, "timmy.session.code_index_prepare")
			codeIndexErr := sm.prepareVectorIndex(ctx, threatModelID, IndexTypeCode, codeSources, progress)
			codeIndexSpan.End()
			if codeIndexErr != nil {
				logger.Warn("Failed to prepare code vector index for session %s: %v", session.ID, codeIndexErr)
			}
		}
	}

	return session, skipped, nil
}

// HandleMessage processes a user message: builds context, calls LLM, persists messages.
// The onToken callback receives streaming tokens as they arrive from the LLM.
// The onStatus callback (optional, may be nil) receives phase transitions
// ahead of token streaming so clients can surface "Timmy is …" affordances.
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: process a user chat message: build context, stream LLM response, persist messages, record usage (reads DB)
func (sm *TimmySessionManager) HandleMessage(
	ctx context.Context,
	sessionID, userID, userMessage string,
	onToken func(token string),
	onStatus MessageStatusCallback,
) (*models.TimmyMessage, error) {
	emitStatus := func(phase, entityType, entityName, detail string) {
		if onStatus != nil {
			onStatus(phase, entityType, entityName, detail)
		}
	}
	logger := slogging.Get()

	// Get session
	session, err := GlobalTimmySessionStore.Get(ctx, sessionID)
	if err != nil {
		return nil, &RequestError{
			Status:  404,
			Code:    "session_not_found",
			Message: "session not found",
		}
	}

	const sessionStatusActive = "active"
	if session.Status != sessionStatusActive {
		return nil, &RequestError{
			Status:  409,
			Code:    "session_not_active",
			Message: "session is not active",
		}
	}

	// Check message rate limit
	if sm.rateLimiter != nil && !sm.rateLimiter.AllowMessage(userID) {
		return nil, &RequestError{
			Status:  429,
			Code:    "message_rate_limit",
			Message: "message rate limit exceeded, please wait before sending another message",
		}
	}

	// Acquire LLM slot
	if sm.rateLimiter != nil && !sm.rateLimiter.AcquireLLMSlot() {
		return nil, &RequestError{
			Status:  503,
			Code:    "llm_busy",
			Message: "all LLM slots are in use, please try again shortly",
		}
	}
	if sm.rateLimiter != nil {
		defer sm.rateLimiter.ReleaseLLMSlot()
	}

	// Get next sequence number
	seq, err := GlobalTimmyMessageStore.GetNextSequence(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get next sequence: %w", err)
	}

	// Persist user message
	userMsg := &models.TimmyMessage{
		SessionID: models.DBVarchar(sessionID),
		Role:      "user",
		Content:   models.DBText(userMessage),
		Sequence:  seq,
	}
	if err := GlobalTimmyMessageStore.Create(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("failed to persist user message: %w", err)
	}

	// Build Tier 1 and Tier 2 context
	var sources []SourceSnapshotEntry
	if session.SourceSnapshot != nil {
		_ = json.Unmarshal(session.SourceSnapshot, &sources)
	}

	tracer := otel.Tracer("tmi.timmy")
	ctx, buildSpan := tracer.Start(ctx, "timmy.context.build")
	emitStatus("building_context", "", "", fmt.Sprintf("%d entities", len(sources)))
	summaries := sm.buildEntitySummaries(sources)
	tier1 := sm.contextBuilder.BuildTier1Context(summaries)

	// Build Tier 2 context via vector search
	tier2 := ""
	if sm.llmService != nil && sm.vectorManager != nil {
		emitStatus("querying_embeddings", "", "", "")
		tier2 = sm.buildTier2Context(ctx, string(session.ThreatModelID), userMessage)
	}
	buildSpan.SetAttributes(
		attribute.Int("tmi.timmy.tier1_entities", len(summaries)),
		attribute.Int("tmi.timmy.tier2_results", func() int {
			if tier2 == "" {
				return 0
			}
			return 1
		}()),
	)
	buildSpan.End()

	// Get conversation history
	emitStatus("loading_history", "", "", "")
	history, err := sm.getConversationHistory(ctx, sessionID)
	if err != nil {
		logger.Warn("Failed to load conversation history for session %s: %v", sessionID, err)
		// Continue without history rather than failing
		history = nil
	}

	// Build full system prompt
	basePrompt := timmyBasePrompt
	if sm.llmService != nil {
		basePrompt = sm.llmService.GetBasePrompt()
	}
	systemPrompt := sm.contextBuilder.BuildFullContext(basePrompt, tier1, tier2)

	// Build LLM message sequence: history + current user message
	var llmMessages []llms.MessageContent
	llmMessages = append(llmMessages, history...)
	llmMessages = append(llmMessages, llms.TextParts(llms.ChatMessageTypeHuman, userMessage))

	// Call LLM with streaming
	if sm.llmService == nil {
		return nil, &RequestError{
			Status:  503,
			Code:    "llm_not_configured",
			Message: "LLM service is not configured",
		}
	}

	emitStatus("waiting_for_llm", "", "", "")
	responseText, tokenCount, err := sm.llmService.GenerateStreamingResponse(ctx, systemPrompt, llmMessages, onToken)
	if err != nil {
		return nil, fmt.Errorf("LLM generation failed: %w", err)
	}

	// Persist assistant message
	assistantSeq, err := GlobalTimmyMessageStore.GetNextSequence(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get next sequence for assistant message: %w", err)
	}

	assistantMsg := &models.TimmyMessage{
		SessionID:  models.DBVarchar(sessionID),
		Role:       "assistant",
		Content:    models.DBText(responseText),
		TokenCount: tokenCount,
		Sequence:   assistantSeq,
	}
	if err := GlobalTimmyMessageStore.Create(ctx, assistantMsg); err != nil {
		return nil, fmt.Errorf("failed to persist assistant message: %w", err)
	}

	// Auto-generate session title from the first user message (#394).
	// Fires only on the very first turn, when the existing title is empty or
	// matches the client placeholder. Runs in a goroutine with its own context
	// so SSE-stream cancellation can't interrupt it; failures are swallowed
	// (the placeholder title is left in place).
	if seq == 1 && shouldAutoRenameTitle(string(session.Title)) && len(strings.TrimSpace(userMessage)) >= 3 && sm.llmService != nil {
		//nolint:gosec // G118 - SSE stream context may cancel before title generation completes; detached context is intentional (see autoRenameSession)
		go sm.autoRenameSession(sessionID, userMessage)
	}

	// Record usage asynchronously (best-effort)
	now := time.Now().UTC()
	usage := &models.TimmyUsage{
		UserID:           models.DBVarchar(userID),
		SessionID:        models.DBVarchar(sessionID),
		ThreatModelID:    session.ThreatModelID,
		MessageCount:     1,
		CompletionTokens: tokenCount,
		PeriodStart:      now.Truncate(time.Hour),
		PeriodEnd:        now.Truncate(time.Hour).Add(time.Hour),
	}
	if err := GlobalTimmyUsageStore.Record(ctx, usage); err != nil {
		logger.Warn("Failed to record usage for session %s: %v", sessionID, err)
	}

	logger.Info("Handled message in session %s: %d tokens generated", sessionID, tokenCount)
	return assistantMsg, nil
}

// titlePlaceholderPattern matches the client-supplied placeholder title used
// before auto-rename takes effect, e.g. "Chat — May 9, 2026, 3:14 PM".
// The em-dash (U+2014) is the canonical separator the client emits;
// a plain hyphen is also accepted to be lenient.
var titlePlaceholderPattern = regexp.MustCompile(`^Chat\s*[—-]\s*`)

// shouldAutoRenameTitle returns true when the session's current title is
// considered a default that the auto-rename pipeline may overwrite. Returns
// false for any user-set title so we never clobber a deliberate name.
// SEM@31f1e9f6c50875c19da05aa43964a24bc7d7d156: report whether a session title is a placeholder eligible for auto-rename (pure)
func shouldAutoRenameTitle(current string) bool {
	trimmed := strings.TrimSpace(current)
	if trimmed == "" {
		return true
	}
	return titlePlaceholderPattern.MatchString(trimmed)
}

const (
	titleGenSystemPrompt = "Summarize the user's question in 5 words or fewer for use as a chat title. Reply with only the title text, no quotes, no punctuation at the end."
	titleGenInputCap     = 500
	// titleGenMaxChars caps the rune count of the generated title. Must keep
	// titleGenMaxChars*4 <= models.TimmySession.Title byte width (varchar(256))
	// because Oracle ADB defaults to VARCHAR2 BYTE semantics and AL32UTF8
	// allows up to 4 bytes per rune. 60 runes -> 240 bytes leaves 16 bytes of
	// headroom; do not raise past 64 without widening the column.
	titleGenMaxChars = 60
	titleGenTimeout  = 30 * time.Second
)

// sanitizeGeneratedTitle trims the LLM response, strips surrounding quotes
// and markdown emphasis, removes line breaks, and clamps to titleGenMaxChars
// runes. Returns an empty string if the result would be unusable.
// SEM@31f1e9f6c50875c19da05aa43964a24bc7d7d156: strip quotes, markdown emphasis, and excess whitespace from an LLM-generated title, clamped to max runes (pure)
func sanitizeGeneratedTitle(raw string) string {
	t := strings.TrimSpace(raw)
	// Collapse any line breaks the model may emit.
	t = strings.ReplaceAll(t, "\r", " ")
	t = strings.ReplaceAll(t, "\n", " ")
	// Strip surrounding markdown emphasis (**bold**, *italic*, _underline_).
	for _, pair := range []string{"**", "*", "_"} {
		if strings.HasPrefix(t, pair) && strings.HasSuffix(t, pair) && len(t) > 2*len(pair) {
			t = t[len(pair) : len(t)-len(pair)]
			t = strings.TrimSpace(t)
		}
	}
	// Strip surrounding quotes (ASCII and curly).
	for _, pair := range [][2]string{
		{`"`, `"`},
		{`'`, `'`},
		{"“", "”"}, // “ ”
		{"‘", "’"}, // ‘ ’
	} {
		if strings.HasPrefix(t, pair[0]) && strings.HasSuffix(t, pair[1]) && len(t) >= len(pair[0])+len(pair[1]) {
			t = t[len(pair[0]) : len(t)-len(pair[1])]
			t = strings.TrimSpace(t)
		}
	}
	// Strip a single trailing terminator.
	t = strings.TrimRight(t, ".!?;:,")
	t = strings.TrimSpace(t)
	// Collapse runs of whitespace.
	t = strings.Join(strings.Fields(t), " ")

	// Clamp to titleGenMaxChars runes (not bytes).
	if utf8.RuneCountInString(t) > titleGenMaxChars {
		runes := []rune(t)
		t = strings.TrimSpace(string(runes[:titleGenMaxChars]))
	}
	return t
}

// autoRenameSession runs in a fresh goroutine after the first turn completes.
// It calls the LLM with a small system prompt, sanitizes the result, and
// persists it via UpdateTitle. All failures are logged and swallowed — the
// placeholder title is left in place rather than surfacing an error to the
// client.
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: generate and persist an LLM-derived session title from the first user message asynchronously (reads DB)
func (sm *TimmySessionManager) autoRenameSession(sessionID, firstUserMessage string) {
	logger := slogging.Get()

	// Detached context with its own timeout. The request context may be
	// cancelled the moment the SSE stream closes, so we cannot reuse it.
	ctx, cancel := context.WithTimeout(context.Background(), titleGenTimeout)
	defer cancel()

	// Cap the input size to keep cost bounded.
	input := firstUserMessage
	if utf8.RuneCountInString(input) > titleGenInputCap {
		runes := []rune(input)
		input = string(runes[:titleGenInputCap])
	}

	tracer := otel.Tracer("tmi.timmy")
	ctx, span := tracer.Start(ctx, "timmy.session.auto_rename")
	defer span.End()

	raw, err := sm.llmService.GenerateResponse(ctx, titleGenSystemPrompt, input)
	if err != nil {
		logger.Warn("Auto-title generation failed for session %s: %v", sessionID, err)
		return
	}

	title := sanitizeGeneratedTitle(raw)
	if title == "" {
		logger.Debug("Auto-title generation produced empty result for session %s; leaving placeholder", sessionID)
		return
	}

	// Re-check current title before writing: a concurrent rename or a user
	// rename that landed during the LLM call should win over our overwrite.
	current, err := GlobalTimmySessionStore.Get(ctx, sessionID)
	if err != nil {
		logger.Warn("Auto-title pre-check failed for session %s: %v", sessionID, err)
		return
	}
	if !shouldAutoRenameTitle(string(current.Title)) {
		logger.Debug("Skipping auto-title for session %s: title was set in the meantime (%q)", sessionID, current.Title)
		return
	}

	if err := GlobalTimmySessionStore.UpdateTitle(ctx, sessionID, title); err != nil {
		logger.Warn("Auto-title persist failed for session %s: %v", sessionID, err)
		return
	}
	logger.Info("Auto-renamed session %s -> %q", sessionID, title)
}

// snapshotSources reads all sub-entity stores for the given threat model
// and returns entries where timmy_enabled is true or nil (defaults to true),
// along with any sources that were skipped.
// SEM@d6f3b757d6d206d606880902c0c0be607dfc1ee1: collect timmy-enabled entity entries across all sub-entity types for a threat model (reads DB)
func (sm *TimmySessionManager) snapshotSources(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, []SkippedSource, error) {
	var entries []SourceSnapshotEntry
	var allSkipped []SkippedSource

	simpleCollectors := []func() ([]SourceSnapshotEntry, error){
		func() ([]SourceSnapshotEntry, error) { return sm.snapshotAssets(ctx, threatModelID) },
		func() ([]SourceSnapshotEntry, error) { return sm.snapshotThreats(ctx, threatModelID) },
		func() ([]SourceSnapshotEntry, error) { return sm.snapshotNotes(ctx, threatModelID) },
		func() ([]SourceSnapshotEntry, error) { return sm.snapshotRepositories(ctx, threatModelID) },
		func() ([]SourceSnapshotEntry, error) { return sm.snapshotDiagrams() },
	}

	for _, collect := range simpleCollectors {
		items, err := collect()
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, items...)
	}

	docEntries, skipped, err := sm.snapshotDocuments(ctx, threatModelID)
	if err != nil {
		return nil, nil, err
	}
	entries = append(entries, docEntries...)
	allSkipped = append(allSkipped, skipped...)

	return entries, allSkipped, nil
}

// SnapshotSources is the public wrapper around snapshotSources for use by the refresh handler.
// SEM@d6f3b757d6d206d606880902c0c0be607dfc1ee1: public wrapper to collect timmy-enabled source entries for a threat model (reads DB)
func (sm *TimmySessionManager) SnapshotSources(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, []SkippedSource, error) {
	return sm.snapshotSources(ctx, threatModelID)
}

const snapshotMaxItems = 1000

// SEM@f7d829c2058f4f0be9f76648be2cbcfc3501f485: list timmy-enabled assets for a threat model as snapshot entries (reads DB)
func (sm *TimmySessionManager) snapshotAssets(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, error) {
	if GlobalAssetRepository == nil {
		return nil, nil
	}
	assets, err := GlobalAssetRepository.List(ctx, threatModelID, 0, snapshotMaxItems)
	if err != nil {
		return nil, fmt.Errorf("failed to list assets: %w", err)
	}
	var entries []SourceSnapshotEntry
	for _, a := range assets {
		if isTimmyEnabled(a.TimmyEnabled) {
			entries = append(entries, newSnapshotEntry("asset", uuidPtrToString(a.Id), a.Name))
		}
	}
	return entries, nil
}

// SEM@f7d829c2058f4f0be9f76648be2cbcfc3501f485: list timmy-enabled threats for a threat model as snapshot entries (reads DB)
func (sm *TimmySessionManager) snapshotThreats(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, error) {
	if GlobalThreatRepository == nil {
		return nil, nil
	}
	filter := ThreatFilter{Offset: 0, Limit: snapshotMaxItems}
	threats, _, err := GlobalThreatRepository.List(ctx, threatModelID, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list threats: %w", err)
	}
	var entries []SourceSnapshotEntry
	for _, t := range threats {
		if isTimmyEnabled(t.TimmyEnabled) {
			entries = append(entries, newSnapshotEntry("threat", uuidPtrToString(t.Id), t.Name))
		}
	}
	return entries, nil
}

// SEM@bd0bc10d5d1f787df91f432d9ae0dd6313a302c6: list timmy-enabled documents for a threat model as snapshot entries, skipping auth-required documents (reads DB)
func (sm *TimmySessionManager) snapshotDocuments(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, []SkippedSource, error) {
	if GlobalDocumentRepository == nil {
		return nil, nil, nil
	}
	docs, err := GlobalDocumentRepository.List(ctx, threatModelID, 0, snapshotMaxItems)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list documents: %w", err)
	}
	var entries []SourceSnapshotEntry
	var skipped []SkippedSource
	for _, d := range docs {
		if !isTimmyEnabled(d.TimmyEnabled) {
			continue
		}
		// Skip documents that require authentication (auth_required status).
		// Documents with "unknown", "accessible", or "pending_access" status are
		// still included — "unknown" means no validation has happened yet, and
		// "pending_access" means an access request is in flight.
		if d.AccessStatus != nil && *d.AccessStatus == DocumentAccessStatusAuthRequired {
			var entityID openapi_types.UUID
			if d.Id != nil {
				entityID = *d.Id
			}
			skipped = append(skipped, SkippedSource{
				EntityId: entityID,
				Name:     d.Name,
				Reason:   "document requires authentication (access_status=auth_required)",
			})
			continue
		}
		entry := newSnapshotEntry("document", uuidPtrToString(d.Id), d.Name)
		// Carry the document URI so the embedding registry can route to the URI-driven
		// content pipeline (PipelineEmbeddingSource). Without this, every URL-bearing
		// document falls through CanHandle and never gets embedded.
		entry.URI = d.Uri
		entries = append(entries, entry)
	}
	return entries, skipped, nil
}

// SEM@f7d829c2058f4f0be9f76648be2cbcfc3501f485: list timmy-enabled notes for a threat model as snapshot entries (reads DB)
func (sm *TimmySessionManager) snapshotNotes(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, error) {
	if GlobalNoteRepository == nil {
		return nil, nil
	}
	notes, err := GlobalNoteRepository.List(ctx, threatModelID, 0, snapshotMaxItems)
	if err != nil {
		return nil, fmt.Errorf("failed to list notes: %w", err)
	}
	var entries []SourceSnapshotEntry
	for _, n := range notes {
		if isTimmyEnabled(n.TimmyEnabled) {
			entries = append(entries, newSnapshotEntry("note", uuidPtrToString(n.Id), n.Name))
		}
	}
	return entries, nil
}

// SEM@f7d829c2058f4f0be9f76648be2cbcfc3501f485: list timmy-enabled repositories for a threat model as snapshot entries (reads DB)
func (sm *TimmySessionManager) snapshotRepositories(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, error) {
	if GlobalRepositoryRepository == nil {
		return nil, nil
	}
	repos, err := GlobalRepositoryRepository.List(ctx, threatModelID, 0, snapshotMaxItems)
	if err != nil {
		return nil, fmt.Errorf("failed to list repositories: %w", err)
	}
	var entries []SourceSnapshotEntry
	for _, r := range repos {
		if isTimmyEnabled(r.TimmyEnabled) {
			name := ""
			if r.Name != nil {
				name = *r.Name
			}
			entries = append(entries, newSnapshotEntry("repository", uuidPtrToString(r.Id), name))
		}
	}
	return entries, nil
}

// SEM@9ac60db5b7f349b340ab9963342361d7b81db7e2: list timmy-enabled diagrams from the in-memory store as snapshot entries (pure)
func (sm *TimmySessionManager) snapshotDiagrams() ([]SourceSnapshotEntry, error) {
	if DiagramStore == nil {
		return nil, nil
	}
	diagrams := DiagramStore.List(0, snapshotMaxItems, func(_ DfdDiagram) bool {
		return true
	})
	var entries []SourceSnapshotEntry
	for _, d := range diagrams {
		if isTimmyEnabled(d.TimmyEnabled) {
			entries = append(entries, newSnapshotEntry("diagram", uuidPtrToString(d.Id), d.Name))
		}
	}
	return entries, nil
}

// newSnapshotEntry creates a SourceSnapshotEntry with the given values
// SEM@9ac60db5b7f349b340ab9963342361d7b81db7e2: build a SourceSnapshotEntry from entity type, ID, and name (pure)
func newSnapshotEntry(entityType, entityID, name string) SourceSnapshotEntry {
	return SourceSnapshotEntry{
		EntityType: entityType,
		EntityID:   entityID,
		Name:       name,
	}
}

// uuidPtrToString safely converts a UUID pointer to string
// SEM@9ac60db5b7f349b340ab9963342361d7b81db7e2: convert a UUID pointer to its string representation, returning empty string for nil (pure)
func uuidPtrToString(id *openapi_types.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// isTimmyEnabled returns true if the timmy_enabled flag is nil (default true) or explicitly true
// SEM@9ac60db5b7f349b340ab9963342361d7b81db7e2: report whether a timmy_enabled flag is true, treating nil as true (pure)
func isTimmyEnabled(flag *bool) bool {
	return flag == nil || *flag
}

// splitSourcesByIndexType partitions source snapshot entries into text and code sources
// SEM@5fb51be4d5e699f50ff2c061acaf629dade9ada2: partition source snapshot entries into text and code index buckets (pure)
func splitSourcesByIndexType(sources []SourceSnapshotEntry) (textSources, codeSources []SourceSnapshotEntry) {
	for _, src := range sources {
		if EntityTypeToIndexType(src.EntityType) == IndexTypeCode {
			codeSources = append(codeSources, src)
		} else {
			textSources = append(textSources, src)
		}
	}
	return textSources, codeSources
}

// classifyStaleness returns a short reason describing why an entity's
// embeddings are stale (or "" when fresh). Used to populate progress
// messages and debug logs. Order is deliberate: dimension before model
// because dimension is what mathematically breaks similarity, and is the
// more diagnostic answer when both differ.
// SEM@a1eba327cad1ac47aa22830ede7f0d2adfcdf78e: return the reason an entity's embeddings are stale, or empty string if fresh (pure)
func classifyStaleness(present bool, meta EntityEmbeddingMeta, hash, expModel string, expDim int) string {
	switch {
	case !present:
		return "new entity"
	case meta.EmbeddingDim != expDim:
		return "dimension changed"
	case meta.EmbeddingModel != expModel:
		return "model changed"
	case meta.ContentHash != hash:
		return "content changed"
	default:
		return ""
	}
}

// prepareVectorIndex ensures the vector index is loaded and up-to-date for
// the threat model. For each source entity, it checks cached metadata
// (content_hash + embedding_model + embedding_dim) against the active
// embedder, and re-embeds stale or new content. If the in-memory index
// cannot be loaded because stored embeddings disagree with the active model
// or dimension, the stale rows are pruned and the load is retried once.
// SEM@63d2546d6591e57d65783c3032d4412409c2b328: ensure the vector index is current by re-embedding stale or new entities for a threat model (reads DB)
func (sm *TimmySessionManager) prepareVectorIndex(
	ctx context.Context,
	threatModelID, indexType string,
	sources []SourceSnapshotEntry,
	progress SessionProgressCallback,
) error {
	logger := slogging.Get()

	if progress != nil {
		progress("indexing", "", "", 0, "loading vector index")
	}

	// Determine embedding dimension and the expected model name for this index type.
	dim, err := sm.llmService.EmbeddingDimension(ctx, indexType)
	if err != nil {
		return fmt.Errorf("failed to determine embedding dimension: %w", err)
	}
	expectedModel, err := sm.expectedEmbeddingModel(ctx, indexType)
	if err != nil {
		return fmt.Errorf("failed to read expected embedding model: %w", err)
	}

	// Get or load the index. If stored rows disagree with (expectedModel, dim),
	// purge the stale rows and retry once.
	idx, err := sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, indexType, expectedModel, dim)
	var mismatch *ErrEmbeddingModelMismatch
	if errors.As(err, &mismatch) {
		logger.Warn("Embedding model mismatch for tm=%s index=%s (stored %s/%d, expected %s/%d) — purging stale rows",
			threatModelID, indexType, mismatch.StaleModel, mismatch.StaleDim,
			expectedModel, dim)
		if progress != nil {
			progress("indexing", "", "", 0, "embedding model changed — re-indexing")
		}
		if _, perr := GlobalTimmyEmbeddingStore.DeleteEntitiesWithStaleEmbeddingMetadata(
			ctx, threatModelID, indexType, expectedModel, dim,
		); perr != nil {
			return fmt.Errorf("purge stale embeddings: %w", perr)
		}
		sm.vectorManager.InvalidateIndex(threatModelID, indexType)
		idx, err = sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, indexType, expectedModel, dim)
		if errors.As(err, &mismatch) {
			return fmt.Errorf("embedding store did not honor purge: %w", err)
		}
	}
	if err != nil {
		return fmt.Errorf("failed to load vector index: %w", err)
	}

	// Load existing per-entity metadata (hash + model + dim) — not vectors.
	existingMeta, err := GlobalTimmyEmbeddingStore.ListEntityMetadataByThreatModelAndIndexType(ctx, threatModelID, indexType)
	if err != nil {
		return fmt.Errorf("failed to load embedding metadata: %w", err)
	}

	total := len(sources)
	for i, src := range sources {
		if progress != nil {
			pct := 0
			if total > 0 {
				pct = (i * 100) / total
			}
			progress("indexing", src.EntityType, src.Name, pct, "processing")
		}

		// Extract content
		ref := EntityReference{
			EntityType: src.EntityType,
			EntityID:   src.EntityID,
			Name:       src.Name,
			URI:        src.URI,
		}
		content, err := sm.providerRegistry.Extract(ctx, ref)
		if err != nil {
			logger.Warn("Failed to extract content for %s/%s: %v", src.EntityType, src.EntityID, err)
			continue
		}
		if content.Text == "" {
			continue
		}

		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content.Text)))
		key := EntityKey{EntityType: src.EntityType, EntityID: src.EntityID}
		meta, present := existingMeta[key]

		reason := classifyStaleness(present, meta, hash, expectedModel, dim)
		if reason == "" {
			// Fresh — embeddings still valid.
			continue
		}

		logger.Debug("Re-embedding %s/%s (%s)", src.EntityType, src.EntityID, reason)
		if progress != nil {
			pct := 0
			if total > 0 {
				pct = (i * 100) / total
			}
			progress("indexing", src.EntityType, src.Name, pct, fmt.Sprintf("re-embedding (%s)", reason))
		}

		// Delete old embeddings for this entity.
		if _, err := GlobalTimmyEmbeddingStore.DeleteByEntity(ctx, threatModelID, src.EntityType, src.EntityID); err != nil {
			logger.Warn("Failed to delete old embeddings for %s/%s: %v", src.EntityType, src.EntityID, err)
		}

		// Chunk the content. Build the chunker on demand from live config so
		// chunk-size/overlap knob changes take effect without an LLM-client
		// rebuild; NewTextChunker is cheap (stores two ints).
		c := sm.cfgFor(ctx)
		chunks := NewTextChunker(c.ChunkSize, c.ChunkOverlap).Chunk(content.Text)
		if len(chunks) == 0 {
			continue
		}

		// Embed all chunks.
		vectors, err := sm.llmService.EmbedTexts(ctx, chunks, indexType)
		if err != nil {
			logger.Warn("Failed to embed chunks for %s/%s: %v", src.EntityType, src.EntityID, err)
			continue
		}

		// Persist embeddings and add to in-memory index.
		var embeddingRecords []models.TimmyEmbedding
		for j, chunk := range chunks {
			if j >= len(vectors) {
				break
			}
			emb := models.TimmyEmbedding{
				ThreatModelID:  models.DBVarchar(threatModelID),
				EntityType:     models.DBVarchar(src.EntityType),
				EntityID:       models.DBVarchar(src.EntityID),
				ChunkIndex:     j,
				ContentHash:    models.DBVarchar(hash),
				IndexType:      models.DBVarchar(indexType),
				EmbeddingModel: models.DBVarchar(expectedModel),
				EmbeddingDim:   len(vectors[j]),
				VectorData:     float32ToBytes(vectors[j]),
				ChunkText:      models.DBText(chunk),
			}
			embeddingRecords = append(embeddingRecords, emb)

			entryID := fmt.Sprintf("%s:%s:%d", src.EntityType, src.EntityID, j)
			idx.Add(entryID, vectors[j], chunk)
		}

		if len(embeddingRecords) > 0 {
			if err := GlobalTimmyEmbeddingStore.CreateBatch(ctx, embeddingRecords); err != nil {
				logger.Warn("Failed to persist embeddings for %s/%s: %v", src.EntityType, src.EntityID, err)
			}
		}
	}

	if progress != nil {
		progress("indexing", "", "", 100, "vector index ready")
	}

	return nil
}

// searchIndexRaw embeds the query and performs vector search, returning raw results
// SEM@534d697cb5ef139c97865f31e32bd7d0b6af458f: embed a query and search the vector index, returning raw similarity results (reads DB)
func (sm *TimmySessionManager) searchIndexRaw(ctx context.Context, threatModelID, indexType, query string, topK int) []VectorSearchResult {
	if sm.llmService == nil || sm.vectorManager == nil {
		return nil
	}

	logger := slogging.Get()

	vectors, err := sm.llmService.EmbedTexts(ctx, []string{query}, indexType)
	if err != nil {
		logger.Warn("Failed to embed query for %s vector search: %v", indexType, err)
		return nil
	}
	if len(vectors) == 0 {
		return nil
	}

	dim := len(vectors[0])
	expectedModel, err := sm.expectedEmbeddingModel(ctx, indexType)
	if err != nil {
		logger.Warn("Failed to read stamped embedding model for %s search: %v", indexType, err)
		return nil
	}
	idx, err := sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, indexType, expectedModel, dim)
	if err != nil {
		var mismatch *ErrEmbeddingModelMismatch
		if errors.As(err, &mismatch) {
			logger.Warn("Embedding model mismatch during search for tm=%s index=%s (stored %s/%d, expected %s/%d) — session was not properly prepared; failing query",
				threatModelID, indexType, mismatch.StaleModel, mismatch.StaleDim,
				expectedModel, dim)
		} else {
			logger.Warn("Failed to get %s vector index for search: %v", indexType, err)
		}
		return nil
	}
	defer sm.vectorManager.ReleaseIndex(threatModelID, indexType)

	return idx.Search(vectors[0], topK)
}

// buildTier2Context runs the full query pipeline: decompose -> search -> rerank -> format
// SEM@63d2546d6591e57d65783c3032d4412409c2b328: run the full query pipeline — decompose, search, rerank, format — for LLM context (reads DB)
func (sm *TimmySessionManager) buildTier2Context(ctx context.Context, threatModelID, query string) string {
	if sm.llmService == nil || sm.vectorManager == nil {
		return ""
	}

	logger := slogging.Get()

	// Step 1: Query decomposition (optional)
	textQuery := query
	codeQuery := query
	if sm.decomposer != nil {
		decomposed, err := sm.decomposer.Decompose(ctx, query, sm.config.IsCodeIndexConfigured())
		if err != nil {
			logger.Warn("Query decomposition failed, using original query: %v", err)
		} else {
			textQuery = decomposed.TextQuery
			if textQuery == "" {
				textQuery = query
			}
			codeQuery = decomposed.CodeQuery
			if codeQuery == "" {
				codeQuery = query
			}
		}
	}

	// Step 2: Search both indexes
	var allResults []VectorSearchResult

	textResults := sm.searchIndexRaw(ctx, threatModelID, IndexTypeText, textQuery, sm.cfgFor(ctx).TextRetrievalTopK)
	allResults = append(allResults, textResults...)

	if sm.config.IsCodeIndexConfigured() {
		codeResults := sm.searchIndexRaw(ctx, threatModelID, IndexTypeCode, codeQuery, sm.cfgFor(ctx).CodeRetrievalTopK)
		allResults = append(allResults, codeResults...)
	}

	if len(allResults) == 0 {
		return ""
	}

	// Step 3: Reranking (optional)
	if sm.reranker != nil {
		documents := make([]string, len(allResults))
		for i, r := range allResults {
			documents[i] = r.ChunkText
		}

		reranked, err := sm.reranker.Rerank(ctx, query, documents)
		if err != nil {
			logger.Warn("Reranking failed, using unranked results: %v", err)
		} else {
			rerankedResults := make([]VectorSearchResult, len(reranked))
			for i, rr := range reranked {
				rerankedResults[i] = VectorSearchResult{
					ID:         allResults[rr.Index].ID,
					ChunkText:  rr.Document,
					Similarity: float32(rr.Score),
				}
			}
			allResults = rerankedResults
		}
	}

	// Step 4: Format results
	return sm.contextBuilder.BuildTier2ContextFromResults(allResults)
}

// buildEntitySummaries converts source snapshot entries into EntitySummary objects for Tier 1 context
// SEM@9ac60db5b7f349b340ab9963342361d7b81db7e2: convert source snapshot entries to EntitySummary objects for Tier 1 context (pure)
func (sm *TimmySessionManager) buildEntitySummaries(sources []SourceSnapshotEntry) []EntitySummary {
	summaries := make([]EntitySummary, 0, len(sources))
	for _, src := range sources {
		summaries = append(summaries, EntitySummary{
			EntityType: src.EntityType,
			EntityID:   src.EntityID,
			Name:       src.Name,
		})
	}
	return summaries
}

// getConversationHistory loads recent messages and converts them to LLM message format
// SEM@63d2546d6591e57d65783c3032d4412409c2b328: fetch recent session messages and convert them to LLM message content format (reads DB)
func (sm *TimmySessionManager) getConversationHistory(ctx context.Context, sessionID string) ([]llms.MessageContent, error) {
	messages, _, err := GlobalTimmyMessageStore.ListBySession(ctx, sessionID, 0, sm.cfgFor(ctx).MaxConversationHistory)
	if err != nil {
		return nil, err
	}

	var result []llms.MessageContent
	for _, msg := range messages {
		var msgType llms.ChatMessageType
		switch msg.Role {
		case "user":
			msgType = llms.ChatMessageTypeHuman
		case "assistant":
			msgType = llms.ChatMessageTypeAI
		default:
			continue
		}
		result = append(result, llms.TextParts(msgType, string(msg.Content)))
	}

	return result, nil
}
