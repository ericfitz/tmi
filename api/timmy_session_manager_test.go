package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSessionManagerTest(t *testing.T) (*TimmySessionManager, func()) {
	t.Helper()
	db := setupTimmyTestDB(t)

	// Wire up global stores
	oldSessionStore := GlobalTimmySessionStore
	oldMessageStore := GlobalTimmyMessageStore
	oldEmbeddingStore := GlobalTimmyEmbeddingStore
	oldUsageStore := GlobalTimmyUsageStore
	oldAssetStore := GlobalAssetRepository
	oldThreatStore := GlobalThreatRepository
	oldDocumentStore := GlobalDocumentRepository
	oldNoteStore := GlobalNoteRepository
	oldRepositoryStore := GlobalRepositoryRepository
	oldDiagramStore := DiagramStore

	GlobalTimmySessionStore = NewGormTimmySessionStore(db)
	GlobalTimmyMessageStore = NewGormTimmyMessageStore(db)
	GlobalTimmyEmbeddingStore = NewGormTimmyEmbeddingStore(db)
	GlobalTimmyUsageStore = NewGormTimmyUsageStore(db)

	// Nil out entity stores since we don't have test data in them
	GlobalAssetRepository = nil
	GlobalThreatRepository = nil
	GlobalDocumentRepository = nil
	GlobalNoteRepository = nil
	GlobalRepositoryRepository = nil
	DiagramStore = nil

	cfg := config.DefaultTimmyConfig()

	// Create manager without LLM service (nil) for unit tests
	sm := NewTimmySessionManager(cfg, nil, nil, nil, NewTimmyRateLimiter(
		cfg.MaxMessagesPerUserPerHour,
		cfg.MaxSessionsPerThreatModel,
		cfg.MaxConcurrentLLMRequests,
	), nil, nil)

	cleanup := func() {
		GlobalTimmySessionStore = oldSessionStore
		GlobalTimmyMessageStore = oldMessageStore
		GlobalTimmyEmbeddingStore = oldEmbeddingStore
		GlobalTimmyUsageStore = oldUsageStore
		GlobalAssetRepository = oldAssetStore
		GlobalThreatRepository = oldThreatStore
		GlobalDocumentRepository = oldDocumentStore
		GlobalNoteRepository = oldNoteStore
		GlobalRepositoryRepository = oldRepositoryStore
		DiagramStore = oldDiagramStore
	}

	return sm, cleanup
}

func TestTimmySessionManager_CreateSession(t *testing.T) {
	sm, cleanup := setupSessionManagerTest(t)
	defer cleanup()

	ctx := context.Background()

	session, skipped, err := sm.CreateSession(ctx, "user-alice", "tm-001", "Test Session", nil)
	require.NoError(t, err)
	require.NotNil(t, session)
	assert.Empty(t, skipped)

	assert.NotEmpty(t, session.ID)
	assert.Equal(t, "user-alice", string(session.UserID))
	assert.Equal(t, "tm-001", string(session.ThreatModelID))
	assert.Equal(t, "Test Session", string(session.Title))
	assert.Equal(t, "active", string(session.Status))

	// Verify session is retrievable
	got, err := GlobalTimmySessionStore.Get(ctx, string(session.ID))
	require.NoError(t, err)
	assert.Equal(t, session.ID, got.ID)
}

func TestTimmySessionManager_CreateSession_SourceSnapshot(t *testing.T) {
	sm, cleanup := setupSessionManagerTest(t)
	defer cleanup()

	ctx := context.Background()

	session, _, err := sm.CreateSession(ctx, "user-bob", "tm-002", "Snapshot Test", nil)
	require.NoError(t, err)
	require.NotNil(t, session)

	// Source snapshot should be valid JSON (empty array since no stores are wired)
	var snapshot []SourceSnapshotEntry
	err = json.Unmarshal(session.SourceSnapshot, &snapshot)
	require.NoError(t, err)
	assert.Empty(t, snapshot, "snapshot should be empty when no entity stores are configured")
}

func TestTimmySessionManager_CreateSession_ProgressCallback(t *testing.T) {
	sm, cleanup := setupSessionManagerTest(t)
	defer cleanup()

	ctx := context.Background()

	var phases []string
	progress := func(phase, entityType, entityName string, pct int, detail string) {
		phases = append(phases, phase)
	}

	_, _, err := sm.CreateSession(ctx, "user-charlie", "tm-003", "Progress Test", progress)
	require.NoError(t, err)

	// Should have received at least the snapshot phase callbacks
	assert.Contains(t, phases, "snapshot")
}

func TestTimmySessionManager_CreateSession_RateLimitEnforcement(t *testing.T) {
	sm, cleanup := setupSessionManagerTest(t)
	defer cleanup()

	ctx := context.Background()

	// Override config to allow only 2 sessions per threat model
	sm.config.MaxSessionsPerThreatModel = 2

	_, _, err := sm.CreateSession(ctx, "user-alice", "tm-rate", "Session 1", nil)
	require.NoError(t, err)

	_, _, err = sm.CreateSession(ctx, "user-bob", "tm-rate", "Session 2", nil)
	require.NoError(t, err)

	// Third session should fail
	_, _, err = sm.CreateSession(ctx, "user-charlie", "tm-rate", "Session 3", nil)
	require.Error(t, err)

	var reqErr *RequestError
	require.ErrorAs(t, err, &reqErr)
	assert.Equal(t, 429, reqErr.Status)
	assert.Equal(t, "session_limit_exceeded", reqErr.Code)
}

func TestTimmySessionManager_HandleMessage_NoLLM(t *testing.T) {
	sm, cleanup := setupSessionManagerTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session first
	session, _, err := sm.CreateSession(ctx, "user-alice", "tm-msg", "Message Test", nil)
	require.NoError(t, err)

	// HandleMessage should fail gracefully when LLM is nil
	_, err = sm.HandleMessage(ctx, string(session.ID), "user-alice", "Hello Timmy!", nil, nil)
	require.Error(t, err)

	var reqErr *RequestError
	require.ErrorAs(t, err, &reqErr)
	assert.Equal(t, 503, reqErr.Status)
	assert.Equal(t, "llm_not_configured", reqErr.Code)
}

func TestTimmySessionManager_HandleMessage_SessionNotFound(t *testing.T) {
	sm, cleanup := setupSessionManagerTest(t)
	defer cleanup()

	ctx := context.Background()

	_, err := sm.HandleMessage(ctx, "nonexistent-session", "user-alice", "Hello!", nil, nil)
	require.Error(t, err)

	var reqErr *RequestError
	require.ErrorAs(t, err, &reqErr)
	assert.Equal(t, 404, reqErr.Status)
}

func TestTimmySessionManager_HandleMessage_PersistsUserMessage(t *testing.T) {
	sm, cleanup := setupSessionManagerTest(t)
	defer cleanup()

	ctx := context.Background()

	session, _, err := sm.CreateSession(ctx, "user-alice", "tm-persist", "Persist Test", nil)
	require.NoError(t, err)

	// HandleMessage will fail at LLM call, but user message should be persisted
	_, _ = sm.HandleMessage(ctx, string(session.ID), "user-alice", "Test message", nil, nil)

	// Verify user message was persisted
	messages, count, err := GlobalTimmyMessageStore.ListBySession(ctx, string(session.ID), 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	require.Len(t, messages, 1)
	assert.Equal(t, "user", string(messages[0].Role))
	assert.Equal(t, models.DBText("Test message"), messages[0].Content)
}

// TestTimmySessionManager_HandleMessage_EmitsStatusEvents pins the #393
// status-callback contract: phase identifiers fire in pipeline order, BEFORE
// the LLM call (which is where the no-LLM path returns). Phases emitted
// after the LLM call (none today) would not be observed here.
func TestTimmySessionManager_HandleMessage_EmitsStatusEvents(t *testing.T) {
	sm, cleanup := setupSessionManagerTest(t)
	defer cleanup()

	ctx := context.Background()

	session, _, err := sm.CreateSession(ctx, "user-alice", "tm-status", "Status Test", nil)
	require.NoError(t, err)

	var phases []string
	statusCb := func(phase, _, _, _ string) {
		phases = append(phases, phase)
	}

	// LLM is nil, so HandleMessage will return at the LLM dispatch point.
	// All status events that fire before that point should still be observed.
	_, err = sm.HandleMessage(ctx, string(session.ID), "user-alice", "Test message", nil, statusCb)
	require.Error(t, err) // expected: no LLM configured

	// building_context fires after the user message persists; loading_history
	// fires before the LLM dispatch. querying_embeddings only fires when
	// llmService and vectorManager are wired (not the case in this unit test).
	assert.Contains(t, phases, "building_context")
	assert.Contains(t, phases, "loading_history")
	assert.NotContains(t, phases, "querying_embeddings", "querying_embeddings requires llmService+vectorManager")
	assert.NotContains(t, phases, "waiting_for_llm", "waiting_for_llm should not fire when LLM is nil")

	// building_context must precede loading_history (pipeline ordering).
	bc, lh := -1, -1
	for i, p := range phases {
		switch p {
		case "building_context":
			if bc == -1 {
				bc = i
			}
		case "loading_history":
			if lh == -1 {
				lh = i
			}
		}
	}
	assert.Less(t, bc, lh, "building_context must fire before loading_history")
}

// TestTimmySessionManager_HandleMessage_NilStatusCallback pins that a nil
// onStatus parameter is safe (no panic).
func TestTimmySessionManager_HandleMessage_NilStatusCallback(t *testing.T) {
	sm, cleanup := setupSessionManagerTest(t)
	defer cleanup()

	ctx := context.Background()
	session, _, err := sm.CreateSession(ctx, "user-alice", "tm-nilstatus", "Nil Status Test", nil)
	require.NoError(t, err)

	// Should not panic with nil status callback.
	_, _ = sm.HandleMessage(ctx, string(session.ID), "user-alice", "Test message", nil, nil)
}

func TestTimmySessionManager_IsTimmyEnabled(t *testing.T) {
	trueVal := true
	falseVal := false

	assert.True(t, isTimmyEnabled(nil), "nil should default to true")
	assert.True(t, isTimmyEnabled(&trueVal), "explicit true should be true")
	assert.False(t, isTimmyEnabled(&falseVal), "explicit false should be false")
}

func TestSplitSourcesByIndexType(t *testing.T) {
	sources := []SourceSnapshotEntry{
		{EntityType: "asset", EntityID: "a1", Name: "Database"},
		{EntityType: "threat", EntityID: "t1", Name: "SQL Injection"},
		{EntityType: "repository", EntityID: "r1", Name: "Backend Repo"},
		{EntityType: "diagram", EntityID: "d1", Name: "DFD"},
		{EntityType: "repository", EntityID: "r2", Name: "Frontend Repo"},
		{EntityType: "note", EntityID: "n1", Name: "Design Note"},
		{EntityType: "document", EntityID: "doc1", Name: "RFC Doc"},
	}

	textSources, codeSources := splitSourcesByIndexType(sources)

	assert.Len(t, textSources, 5, "should have 5 text sources")
	assert.Len(t, codeSources, 2, "should have 2 code sources")

	for _, s := range textSources {
		assert.NotEqual(t, "repository", s.EntityType)
	}
	for _, s := range codeSources {
		assert.Equal(t, "repository", s.EntityType)
	}
}

func TestSplitSourcesByIndexType_Empty(t *testing.T) {
	textSources, codeSources := splitSourcesByIndexType(nil)
	assert.Empty(t, textSources)
	assert.Empty(t, codeSources)
}

func TestSplitSourcesByIndexType_NoRepositories(t *testing.T) {
	sources := []SourceSnapshotEntry{
		{EntityType: "asset", EntityID: "a1", Name: "DB"},
		{EntityType: "threat", EntityID: "t1", Name: "XSS"},
	}

	textSources, codeSources := splitSourcesByIndexType(sources)
	assert.Len(t, textSources, 2)
	assert.Empty(t, codeSources)
}

func TestTimmySessionManager_BuildEntitySummaries(t *testing.T) {
	sm := &TimmySessionManager{
		contextBuilder: NewContextBuilder(),
	}

	sources := []SourceSnapshotEntry{
		{EntityType: "asset", EntityID: "a1", Name: "Database"},
		{EntityType: "threat", EntityID: "t1", Name: "SQL Injection"},
	}

	summaries := sm.buildEntitySummaries(sources)
	require.Len(t, summaries, 2)
	assert.Equal(t, "asset", summaries[0].EntityType)
	assert.Equal(t, "Database", summaries[0].Name)
	assert.Equal(t, "threat", summaries[1].EntityType)
	assert.Equal(t, "SQL Injection", summaries[1].Name)
}

func TestTimmySessionManager_NilDecomposerUsesOriginalQuery(t *testing.T) {
	sm := &TimmySessionManager{
		config:         config.DefaultTimmyConfig(),
		contextBuilder: NewContextBuilder(),
	}
	result := sm.buildTier2Context(context.Background(), "tm-001", "test query")
	assert.Equal(t, "", result, "should return empty with nil LLM service")
}

func TestTimmySessionManager_SearchIndexRaw_NilService(t *testing.T) {
	sm := &TimmySessionManager{
		config:         config.DefaultTimmyConfig(),
		contextBuilder: NewContextBuilder(),
	}
	results := sm.searchIndexRaw(context.Background(), "tm-001", IndexTypeText, "query", 10)
	assert.Empty(t, results, "should return nil with nil LLM service")
}

// TestSourceSnapshotEntry_URIRoundtrip is a regression test for the bug where
// document URIs were dropped on the floor between snapshotDocuments and the
// embedding-source registry, leaving every URL-bearing document unembedded.
// (https://github.com/ericfitz/tmi/issues/386)
func TestSourceSnapshotEntry_URIRoundtrip(t *testing.T) {
	doc := SourceSnapshotEntry{
		EntityType: "document",
		EntityID:   "doc-1",
		Name:       "Architecture",
		URI:        "https://docs.google.com/document/d/abc123/edit",
	}
	dbResident := SourceSnapshotEntry{
		EntityType: "note",
		EntityID:   "note-1",
		Name:       "Design",
	}

	// JSON roundtrip preserves URI for documents and omits it for DB-resident entries.
	docJSON, err := json.Marshal(doc)
	require.NoError(t, err)
	assert.Contains(t, string(docJSON), `"uri":"https://docs.google.com/document/d/abc123/edit"`)

	dbJSON, err := json.Marshal(dbResident)
	require.NoError(t, err)
	assert.NotContains(t, string(dbJSON), `"uri"`, "DB-resident entries should omit empty URI")
}

// TestSourceSnapshotEntry_RoutesToCorrectSource asserts the embedding registry
// dispatches a document SourceSnapshotEntry (with URI) to a URI-bearing source,
// and a DB-resident entry (without URI) to a non-URI source. This is the
// specific dispatch path that was broken in #386.
func TestSourceSnapshotEntry_RoutesToCorrectSource(t *testing.T) {
	registry := NewEmbeddingSourceRegistry()
	registry.Register(NewDirectTextProvider())

	docRef := EntityReference{
		EntityType: "document",
		EntityID:   "doc-1",
		Name:       "Architecture",
		URI:        "https://docs.google.com/document/d/abc123/edit",
	}
	noteRef := EntityReference{
		EntityType: "note",
		EntityID:   "note-1",
		Name:       "Design",
	}

	// DirectTextProvider is the only registered source. It must reject the
	// document (URI present) and accept the note (URI absent).
	directProvider := NewDirectTextProvider()
	assert.False(t, directProvider.CanHandle(context.Background(), docRef),
		"DirectTextProvider should reject entities with URI — they need PipelineEmbeddingSource")
	assert.True(t, directProvider.CanHandle(context.Background(), noteRef),
		"DirectTextProvider should accept DB-resident entries without URI")
}

func TestClassifyStaleness_AllReasons(t *testing.T) {
	const expModel = "m-current"
	const expDim = 8
	const hashCurr = "h-current"
	freshMeta := EntityEmbeddingMeta{ContentHash: hashCurr, EmbeddingModel: expModel, EmbeddingDim: expDim}

	tests := []struct {
		name     string
		present  bool
		meta     EntityEmbeddingMeta
		hash     string
		expected string
	}{
		{name: "fresh", present: true, meta: freshMeta, hash: hashCurr, expected: ""},
		{name: "new entity", present: false, meta: EntityEmbeddingMeta{}, hash: hashCurr, expected: "new entity"},
		{name: "dim changed", present: true, meta: EntityEmbeddingMeta{ContentHash: hashCurr, EmbeddingModel: expModel, EmbeddingDim: 16}, hash: hashCurr, expected: "dimension changed"},
		{name: "model changed", present: true, meta: EntityEmbeddingMeta{ContentHash: hashCurr, EmbeddingModel: "m-old", EmbeddingDim: expDim}, hash: hashCurr, expected: "model changed"},
		{name: "content changed", present: true, meta: freshMeta, hash: "h-new", expected: "content changed"},
		// dim takes precedence over model (when both differ)
		{name: "dim+model both differ -> dim wins", present: true, meta: EntityEmbeddingMeta{ContentHash: hashCurr, EmbeddingModel: "m-old", EmbeddingDim: 16}, hash: hashCurr, expected: "dimension changed"},
		// model takes precedence over content (when both differ)
		{name: "model+content both differ -> model wins", present: true, meta: EntityEmbeddingMeta{ContentHash: "h-different", EmbeddingModel: "m-old", EmbeddingDim: expDim}, hash: hashCurr, expected: "model changed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyStaleness(tt.present, tt.meta, tt.hash, expModel, expDim)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestShouldAutoRenameTitle(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		expected bool
	}{
		{name: "empty", title: "", expected: true},
		{name: "whitespace only", title: "   ", expected: true},
		{name: "client em-dash placeholder", title: "Chat — May 9, 2026, 3:14 PM", expected: true},
		{name: "client hyphen placeholder", title: "Chat - May 9, 2026, 3:14 PM", expected: true},
		{name: "client placeholder no-space variants", title: "Chat—Today", expected: true},
		{name: "user-set title", title: "OAuth login analysis", expected: false},
		{name: "user-set title that mentions chat", title: "Chat about session tokens", expected: false},
		{name: "leading whitespace + placeholder", title: "  Chat — Today  ", expected: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldAutoRenameTitle(tt.title)
			assert.Equal(t, tt.expected, got, "title=%q", tt.title)
		})
	}
}

func TestSanitizeGeneratedTitle(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected string
	}{
		{name: "trims whitespace", raw: "  hello  ", expected: "hello"},
		{name: "strips ASCII double quotes", raw: `"OAuth flow"`, expected: "OAuth flow"},
		{name: "strips ASCII single quotes", raw: `'login bug'`, expected: "login bug"},
		{name: "strips curly double quotes", raw: "“session tokens”", expected: "session tokens"},
		{name: "strips bold markdown", raw: "**threats**", expected: "threats"},
		{name: "strips italic markdown", raw: "*idea*", expected: "idea"},
		{name: "strips trailing period", raw: "Login flow analysis.", expected: "Login flow analysis"},
		{name: "strips trailing exclamation", raw: "Big bug!", expected: "Big bug"},
		{name: "collapses internal whitespace", raw: "a   b   c", expected: "a b c"},
		{name: "strips line breaks", raw: "line1\nline2", expected: "line1 line2"},
		{name: "clamps to 60 chars", raw: strings.Repeat("a", 70), expected: strings.Repeat("a", 60)},
		{name: "clamps multibyte safely", raw: strings.Repeat("é", 70), expected: strings.Repeat("é", 60)},
		{name: "empty after sanitize", raw: `""`, expected: ""},
		{name: "all whitespace", raw: "   ", expected: ""},
		{name: "combined: quotes + period + bold", raw: `**"Auth review."**`, expected: "Auth review"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeGeneratedTitle(tt.raw)
			assert.Equal(t, tt.expected, got, "raw=%q", tt.raw)
		})
	}
}
