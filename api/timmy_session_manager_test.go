package api

import (
	"context"
	"encoding/json"
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
	oldAssetStore := GlobalAssetStore
	oldThreatStore := GlobalThreatStore
	oldDocumentStore := GlobalDocumentStore
	oldNoteStore := GlobalNoteStore
	oldRepositoryStore := GlobalRepositoryStore
	oldDiagramStore := DiagramStore

	GlobalTimmySessionStore = NewGormTimmySessionStore(db)
	GlobalTimmyMessageStore = NewGormTimmyMessageStore(db)
	GlobalTimmyEmbeddingStore = NewGormTimmyEmbeddingStore(db)
	GlobalTimmyUsageStore = NewGormTimmyUsageStore(db)

	// Nil out entity stores since we don't have test data in them
	GlobalAssetStore = nil
	GlobalThreatStore = nil
	GlobalDocumentStore = nil
	GlobalNoteStore = nil
	GlobalRepositoryStore = nil
	DiagramStore = nil

	cfg := config.DefaultTimmyConfig()

	// Create manager without LLM service (nil) for unit tests
	sm := NewTimmySessionManager(cfg, nil, nil, nil, NewTimmyRateLimiter(
		cfg.MaxMessagesPerUserPerHour,
		cfg.MaxSessionsPerThreatModel,
		cfg.MaxConcurrentLLMRequests,
	))

	cleanup := func() {
		GlobalTimmySessionStore = oldSessionStore
		GlobalTimmyMessageStore = oldMessageStore
		GlobalTimmyEmbeddingStore = oldEmbeddingStore
		GlobalTimmyUsageStore = oldUsageStore
		GlobalAssetStore = oldAssetStore
		GlobalThreatStore = oldThreatStore
		GlobalDocumentStore = oldDocumentStore
		GlobalNoteStore = oldNoteStore
		GlobalRepositoryStore = oldRepositoryStore
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
	assert.Equal(t, "user-alice", session.UserID)
	assert.Equal(t, "tm-001", session.ThreatModelID)
	assert.Equal(t, "Test Session", session.Title)
	assert.Equal(t, "active", session.Status)

	// Verify session is retrievable
	got, err := GlobalTimmySessionStore.Get(ctx, session.ID)
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
	_, err = sm.HandleMessage(ctx, session.ID, "user-alice", "Hello Timmy!", nil)
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

	_, err := sm.HandleMessage(ctx, "nonexistent-session", "user-alice", "Hello!", nil)
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
	_, _ = sm.HandleMessage(ctx, session.ID, "user-alice", "Test message", nil)

	// Verify user message was persisted
	messages, count, err := GlobalTimmyMessageStore.ListBySession(ctx, session.ID, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	require.Len(t, messages, 1)
	assert.Equal(t, "user", messages[0].Role)
	assert.Equal(t, models.DBText("Test message"), messages[0].Content)
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
