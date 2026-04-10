package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/tmc/langchaingo/llms"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// SourceSnapshotEntry represents a single entity included in a Timmy session's source snapshot
type SourceSnapshotEntry struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Name       string `json:"name"`
}

// SessionProgressCallback reports progress during session creation phases
type SessionProgressCallback func(phase, entityType, entityName string, progress int, detail string)

// TimmySessionManager orchestrates Timmy session and message lifecycle,
// wiring together LLM, vector index, content providers, and rate limiting
type TimmySessionManager struct {
	config           config.TimmyConfig
	llmService       *TimmyLLMService
	vectorManager    *VectorIndexManager
	providerRegistry *ContentProviderRegistry
	chunker          *TextChunker
	contextBuilder   *ContextBuilder
	rateLimiter      *TimmyRateLimiter
}

// NewTimmySessionManager creates a new session manager with all required dependencies
func NewTimmySessionManager(
	cfg config.TimmyConfig,
	llm *TimmyLLMService,
	vm *VectorIndexManager,
	registry *ContentProviderRegistry,
	rl *TimmyRateLimiter,
) *TimmySessionManager {
	return &TimmySessionManager{
		config:           cfg,
		llmService:       llm,
		vectorManager:    vm,
		providerRegistry: registry,
		chunker:          NewTextChunker(cfg.ChunkSize, cfg.ChunkOverlap),
		contextBuilder:   NewContextBuilder(),
		rateLimiter:      rl,
	}
}

// CreateSession creates a new Timmy chat session for a user and threat model.
// It snapshots timmy-enabled entities, creates the session record, and
// optionally prepares the vector index (if LLM service is configured).
// Returns the created session, any skipped sources, and an error.
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
	if activeCount >= sm.config.MaxSessionsPerThreatModel {
		return nil, nil, &RequestError{
			Status:  429,
			Code:    "session_limit_exceeded",
			Message: fmt.Sprintf("threat model has reached the maximum of %d active sessions", sm.config.MaxSessionsPerThreatModel),
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
		ThreatModelID:  threatModelID,
		UserID:         userID,
		Title:          title,
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
		ctx, indexSpan := tracer.Start(ctx, "timmy.session.index_prepare")
		indexErr := sm.prepareVectorIndex(ctx, threatModelID, sources, progress)
		indexSpan.End()
		if indexErr != nil {
			// Log but don't fail session creation — vector search degrades gracefully
			logger.Warn("Failed to prepare vector index for session %s: %v", session.ID, indexErr)
		}
	}

	return session, skipped, nil
}

// HandleMessage processes a user message: builds context, calls LLM, persists messages.
// The onToken callback receives streaming tokens as they arrive from the LLM.
func (sm *TimmySessionManager) HandleMessage(
	ctx context.Context,
	sessionID, userID, userMessage string,
	onToken func(token string),
) (*models.TimmyMessage, error) {
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
		SessionID: sessionID,
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
	summaries := sm.buildEntitySummaries(sources)
	tier1 := sm.contextBuilder.BuildTier1Context(summaries)

	// Build Tier 2 context via vector search
	tier2 := ""
	if sm.llmService != nil && sm.vectorManager != nil {
		tier2 = sm.buildTier2Context(ctx, session.ThreatModelID, userMessage)
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
		SessionID:  sessionID,
		Role:       "assistant",
		Content:    models.DBText(responseText),
		TokenCount: tokenCount,
		Sequence:   assistantSeq,
	}
	if err := GlobalTimmyMessageStore.Create(ctx, assistantMsg); err != nil {
		return nil, fmt.Errorf("failed to persist assistant message: %w", err)
	}

	// Record usage asynchronously (best-effort)
	now := time.Now().UTC()
	usage := &models.TimmyUsage{
		UserID:           userID,
		SessionID:        sessionID,
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

// snapshotSources reads all sub-entity stores for the given threat model
// and returns entries where timmy_enabled is true or nil (defaults to true),
// along with any sources that were skipped.
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
func (sm *TimmySessionManager) SnapshotSources(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, []SkippedSource, error) {
	return sm.snapshotSources(ctx, threatModelID)
}

const snapshotMaxItems = 1000

func (sm *TimmySessionManager) snapshotAssets(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, error) {
	if GlobalAssetStore == nil {
		return nil, nil
	}
	assets, err := GlobalAssetStore.List(ctx, threatModelID, 0, snapshotMaxItems)
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

func (sm *TimmySessionManager) snapshotThreats(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, error) {
	if GlobalThreatStore == nil {
		return nil, nil
	}
	filter := ThreatFilter{Offset: 0, Limit: snapshotMaxItems}
	threats, _, err := GlobalThreatStore.List(ctx, threatModelID, filter)
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

func (sm *TimmySessionManager) snapshotDocuments(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, []SkippedSource, error) {
	if GlobalDocumentStore == nil {
		return nil, nil, nil
	}
	docs, err := GlobalDocumentStore.List(ctx, threatModelID, 0, snapshotMaxItems)
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
		entries = append(entries, newSnapshotEntry("document", uuidPtrToString(d.Id), d.Name))
	}
	return entries, skipped, nil
}

func (sm *TimmySessionManager) snapshotNotes(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, error) {
	if GlobalNoteStore == nil {
		return nil, nil
	}
	notes, err := GlobalNoteStore.List(ctx, threatModelID, 0, snapshotMaxItems)
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

func (sm *TimmySessionManager) snapshotRepositories(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, error) {
	if GlobalRepositoryStore == nil {
		return nil, nil
	}
	repos, err := GlobalRepositoryStore.List(ctx, threatModelID, 0, snapshotMaxItems)
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
func newSnapshotEntry(entityType, entityID, name string) SourceSnapshotEntry {
	return SourceSnapshotEntry{
		EntityType: entityType,
		EntityID:   entityID,
		Name:       name,
	}
}

// uuidPtrToString safely converts a UUID pointer to string
func uuidPtrToString(id *openapi_types.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// isTimmyEnabled returns true if the timmy_enabled flag is nil (default true) or explicitly true
func isTimmyEnabled(flag *bool) bool {
	return flag == nil || *flag
}

// prepareVectorIndex ensures the vector index is loaded and up-to-date for the threat model.
// For each source entity, it checks cached embeddings (content hash match) and
// re-embeds stale or new content.
func (sm *TimmySessionManager) prepareVectorIndex(
	ctx context.Context,
	threatModelID string,
	sources []SourceSnapshotEntry,
	progress SessionProgressCallback,
) error {
	logger := slogging.Get()

	if progress != nil {
		progress("indexing", "", "", 0, "loading vector index")
	}

	// Determine embedding dimension
	dim, err := sm.llmService.EmbeddingDimension(ctx)
	if err != nil {
		return fmt.Errorf("failed to determine embedding dimension: %w", err)
	}

	// Get or load the index
	idx, err := sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, dim)
	if err != nil {
		return fmt.Errorf("failed to load vector index: %w", err)
	}

	// Load existing embeddings for hash comparison
	existingEmbeddings, err := GlobalTimmyEmbeddingStore.ListByThreatModelAndIndexType(ctx, threatModelID, IndexTypeText)
	if err != nil {
		return fmt.Errorf("failed to load existing embeddings: %w", err)
	}

	// Build a map of entity -> content hash from existing embeddings
	type entityKey struct {
		entityType string
		entityID   string
	}
	existingHashes := make(map[entityKey]string)
	for _, emb := range existingEmbeddings {
		key := entityKey{entityType: emb.EntityType, entityID: emb.EntityID}
		existingHashes[key] = emb.ContentHash
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
		}
		content, err := sm.providerRegistry.Extract(ctx, ref)
		if err != nil {
			logger.Warn("Failed to extract content for %s/%s: %v", src.EntityType, src.EntityID, err)
			continue
		}

		if content.Text == "" {
			continue
		}

		// Compute content hash
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content.Text)))

		// Check if embeddings are still fresh
		key := entityKey{entityType: src.EntityType, entityID: src.EntityID}
		if existingHash, ok := existingHashes[key]; ok && existingHash == hash {
			// Content unchanged — embeddings are still valid
			continue
		}

		// Content is stale or new — re-embed
		logger.Debug("Re-embedding %s/%s (content changed)", src.EntityType, src.EntityID)

		// Delete old embeddings for this entity
		if err := GlobalTimmyEmbeddingStore.DeleteByEntity(ctx, threatModelID, src.EntityType, src.EntityID); err != nil {
			logger.Warn("Failed to delete old embeddings for %s/%s: %v", src.EntityType, src.EntityID, err)
		}

		// Chunk the content
		chunks := sm.chunker.Chunk(content.Text)
		if len(chunks) == 0 {
			continue
		}

		// Embed all chunks
		vectors, err := sm.llmService.EmbedTexts(ctx, chunks)
		if err != nil {
			logger.Warn("Failed to embed chunks for %s/%s: %v", src.EntityType, src.EntityID, err)
			continue
		}

		// Persist embeddings and add to in-memory index
		var embeddingRecords []models.TimmyEmbedding
		for j, chunk := range chunks {
			if j >= len(vectors) {
				break
			}
			emb := models.TimmyEmbedding{
				ThreatModelID:  threatModelID,
				EntityType:     src.EntityType,
				EntityID:       src.EntityID,
				ChunkIndex:     j,
				ContentHash:    hash,
				EmbeddingModel: sm.config.TextEmbeddingModel,
				EmbeddingDim:   len(vectors[j]),
				VectorData:     float32ToBytes(vectors[j]),
				ChunkText:      models.DBText(chunk),
			}
			embeddingRecords = append(embeddingRecords, emb)

			// Add to in-memory index
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

// buildTier2Context embeds the user query and performs vector search for relevant chunks
func (sm *TimmySessionManager) buildTier2Context(ctx context.Context, threatModelID, query string) string {
	logger := slogging.Get()

	// Embed the query
	vectors, err := sm.llmService.EmbedTexts(ctx, []string{query})
	if err != nil {
		logger.Warn("Failed to embed query for vector search: %v", err)
		return ""
	}
	if len(vectors) == 0 {
		return ""
	}

	// Determine dimension from query embedding
	dim := len(vectors[0])

	// Get the index (don't increment active sessions — we already have a session)
	idx, err := sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, dim)
	if err != nil {
		logger.Warn("Failed to get vector index for search: %v", err)
		return ""
	}
	// Release the extra session count we just added
	defer sm.vectorManager.ReleaseIndex(threatModelID)

	return sm.contextBuilder.BuildTier2Context(idx, vectors[0], sm.config.TextRetrievalTopK)
}

// buildEntitySummaries converts source snapshot entries into EntitySummary objects for Tier 1 context
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
func (sm *TimmySessionManager) getConversationHistory(ctx context.Context, sessionID string) ([]llms.MessageContent, error) {
	messages, _, err := GlobalTimmyMessageStore.ListBySession(ctx, sessionID, 0, sm.config.MaxConversationHistory)
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
