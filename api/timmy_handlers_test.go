package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants for Timmy handler tests
const (
	timmyTestUserAlice = "user-alice-uuid"
	timmyTestUserBob   = "user-bob-uuid"
	timmyTestFakeUUID  = "00000000-0000-0000-0000-ffffffffffff"
	timmyTestTMID1     = "00000000-0000-0000-0000-100000000001"
	timmyTestTMID2     = "00000000-0000-0000-0000-100000000002"
	timmyTestTMID3     = "00000000-0000-0000-0000-100000000003"
	timmyTestTMID4     = "00000000-0000-0000-0000-100000000004"
	timmyTestTMID5     = "00000000-0000-0000-0000-100000000005"
	timmyTestTMID6     = "00000000-0000-0000-0000-100000000006"
	timmyTestTMID7     = "00000000-0000-0000-0000-100000000007"
	timmyTestTMID8     = "00000000-0000-0000-0000-100000000008"
	timmyTestOtherTMID = "00000000-0000-0000-0000-100000000099"
)

// setupTimmyHandlerTest sets up stores, creates a server, and returns a cleanup function.
func setupTimmyHandlerTest(t *testing.T) (*Server, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := setupTimmyTestDB(t)

	oldSessionStore := GlobalTimmySessionStore
	oldMessageStore := GlobalTimmyMessageStore
	oldUsageStore := GlobalTimmyUsageStore

	GlobalTimmySessionStore = NewGormTimmySessionStore(db)
	GlobalTimmyMessageStore = NewGormTimmyMessageStore(db)
	GlobalTimmyUsageStore = NewGormTimmyUsageStore(db)

	server := NewServerForTests()

	cleanup := func() {
		GlobalTimmySessionStore = oldSessionStore
		GlobalTimmyMessageStore = oldMessageStore
		GlobalTimmyUsageStore = oldUsageStore
	}

	return server, cleanup
}

// createTestTimmySession creates a session directly in the store for testing.
func createTestTimmySession(t *testing.T, userID, tmID, title, status string) *models.TimmySession {
	t.Helper()
	session := &models.TimmySession{
		ThreatModelID: tmID,
		UserID:        userID,
		Title:         title,
		Status:        status,
	}
	err := GlobalTimmySessionStore.Create(context.Background(), session)
	require.NoError(t, err)
	return session
}

// createTestTimmyMessage creates a message directly in the store for testing.
func createTestTimmyMessage(t *testing.T, sessionID, role, content string, seq int) *models.TimmyMessage {
	t.Helper()
	msg := &models.TimmyMessage{
		SessionID: sessionID,
		Role:      role,
		Content:   models.DBText(content),
		Sequence:  seq,
	}
	err := GlobalTimmyMessageStore.Create(context.Background(), msg)
	require.NoError(t, err)
	return msg
}

// createTestTimmyUsage creates a usage record directly in the store for testing.
func createTestTimmyUsage(t *testing.T, userID, sessionID, tmID string, messages, promptTokens, completionTokens int) {
	t.Helper()
	now := time.Now().UTC()
	usage := &models.TimmyUsage{
		UserID:           userID,
		SessionID:        sessionID,
		ThreatModelID:    tmID,
		MessageCount:     messages,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		PeriodStart:      now.Truncate(time.Hour),
		PeriodEnd:        now.Truncate(time.Hour).Add(time.Hour),
	}
	err := GlobalTimmyUsageStore.Record(context.Background(), usage)
	require.NoError(t, err)
}

// timmyTestContext creates a gin.Context with auth set and returns the response recorder.
func timmyTestContext(method, path, userID string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, nil)
	c.Set("userInternalUUID", userID)
	return c, w
}

// timmyJSONBody marshals v to JSON and returns an io.Reader.
func timmyJSONBody(t *testing.T, v any) io.Reader {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewReader(data)
}

// --- ListTimmyChatSessions tests ---

func TestTimmyListSessions_Success(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	createTestTimmySession(t, timmyTestUserAlice, timmyTestTMID1, "Session 1", "active")
	createTestTimmySession(t, timmyTestUserAlice, timmyTestTMID1, "Session 2", "active")
	// Session from different user should not appear
	createTestTimmySession(t, timmyTestUserBob, timmyTestTMID1, "Bob Session", "active")

	c, w := timmyTestContext("GET", "/threat_models/"+timmyTestTMID1+"/timmy/sessions", timmyTestUserAlice)

	server.ListTimmyChatSessions(c, mustParseTimmyUUID(timmyTestTMID1), ListTimmyChatSessionsParams{})

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListTimmySessionsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Total)
	assert.Len(t, resp.Sessions, 2)
	assert.Equal(t, 20, resp.Limit)
	assert.Equal(t, 0, resp.Offset)
}

func TestTimmyListSessions_Pagination(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	for i := range 5 {
		createTestTimmySession(t, timmyTestUserAlice, timmyTestTMID2, "Session "+string(rune('A'+i)), "active")
	}

	limit := 2
	offset := 1
	c, w := timmyTestContext("GET", "/threat_models/"+timmyTestTMID2+"/timmy/sessions?limit=2&offset=1", timmyTestUserAlice)
	params := ListTimmyChatSessionsParams{
		Limit:  &limit,
		Offset: &offset,
	}

	server.ListTimmyChatSessions(c, mustParseTimmyUUID(timmyTestTMID2), params)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListTimmySessionsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 5, resp.Total)
	assert.Len(t, resp.Sessions, 2)
	assert.Equal(t, 2, resp.Limit)
	assert.Equal(t, 1, resp.Offset)
}

func TestTimmyListSessions_Unauthenticated(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	// No userInternalUUID set
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	server.ListTimmyChatSessions(c, mustParseTimmyUUID(timmyTestTMID1), ListTimmyChatSessionsParams{})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- GetTimmyChatSession tests ---

func TestTimmyGetSession_Success(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	session := createTestTimmySession(t, timmyTestUserAlice, timmyTestTMID3, "My Session", "active")

	c, w := timmyTestContext("GET", "/", timmyTestUserAlice)

	server.GetTimmyChatSession(c, mustParseTimmyUUID(timmyTestTMID3), mustParseTimmyUUID(session.ID))

	assert.Equal(t, http.StatusOK, w.Code)

	var resp TimmyChatSession
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, session.ID, resp.Id.String())
	assert.NotNil(t, resp.Title)
	assert.Equal(t, "My Session", *resp.Title)
}

func TestTimmyGetSession_NotFound(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	c, w := timmyTestContext("GET", "/", timmyTestUserAlice)

	server.GetTimmyChatSession(c, mustParseTimmyUUID(timmyTestTMID3), mustParseTimmyUUID(timmyTestFakeUUID))

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTimmyGetSession_WrongUser(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	session := createTestTimmySession(t, timmyTestUserAlice, timmyTestTMID3, "Alice Session", "active")

	// Bob tries to access Alice's session
	c, w := timmyTestContext("GET", "/", timmyTestUserBob)

	server.GetTimmyChatSession(c, mustParseTimmyUUID(timmyTestTMID3), mustParseTimmyUUID(session.ID))

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestTimmyGetSession_WrongThreatModel(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	session := createTestTimmySession(t, timmyTestUserAlice, timmyTestTMID3, "Session", "active")

	// Access with wrong threat model ID
	c, w := timmyTestContext("GET", "/", timmyTestUserAlice)

	server.GetTimmyChatSession(c, mustParseTimmyUUID(timmyTestOtherTMID), mustParseTimmyUUID(session.ID))

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- DeleteTimmyChatSession tests ---

func TestTimmyDeleteSession_Success(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	session := createTestTimmySession(t, timmyTestUserAlice, timmyTestTMID4, "To Delete", "active")

	c, w := timmyTestContext("DELETE", "/", timmyTestUserAlice)

	server.DeleteTimmyChatSession(c, mustParseTimmyUUID(timmyTestTMID4), mustParseTimmyUUID(session.ID))

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify session is no longer retrievable
	_, err := GlobalTimmySessionStore.Get(context.Background(), session.ID)
	assert.Error(t, err)
}

func TestTimmyDeleteSession_NotFound(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	c, w := timmyTestContext("DELETE", "/", timmyTestUserAlice)

	server.DeleteTimmyChatSession(c, mustParseTimmyUUID(timmyTestTMID4), mustParseTimmyUUID(timmyTestFakeUUID))

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTimmyDeleteSession_WrongUser(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	session := createTestTimmySession(t, timmyTestUserAlice, timmyTestTMID4, "Alice Session", "active")

	c, w := timmyTestContext("DELETE", "/", timmyTestUserBob)

	server.DeleteTimmyChatSession(c, mustParseTimmyUUID(timmyTestTMID4), mustParseTimmyUUID(session.ID))

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- ListTimmyChatMessages tests ---

func TestTimmyListMessages_Success(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	session := createTestTimmySession(t, timmyTestUserAlice, timmyTestTMID5, "Chat Session", "active")

	createTestTimmyMessage(t, session.ID, "user", "Hello", 1)
	createTestTimmyMessage(t, session.ID, "assistant", "Hi there!", 2)
	createTestTimmyMessage(t, session.ID, "user", "Tell me about threats", 3)

	c, w := timmyTestContext("GET", "/", timmyTestUserAlice)

	server.ListTimmyChatMessages(c, mustParseTimmyUUID(timmyTestTMID5), mustParseTimmyUUID(session.ID), ListTimmyChatMessagesParams{})

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListTimmyMessagesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 3, resp.Total)
	assert.Len(t, resp.Messages, 3)
	assert.Equal(t, 50, resp.Limit)
	assert.Equal(t, 0, resp.Offset)

	// Verify order is by sequence ascending
	assert.Equal(t, "Hello", resp.Messages[0].Content)
	assert.Equal(t, "Hi there!", resp.Messages[1].Content)
}

func TestTimmyListMessages_Pagination(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	session := createTestTimmySession(t, timmyTestUserAlice, timmyTestTMID5, "Chat", "active")

	for i := range 10 {
		createTestTimmyMessage(t, session.ID, "user", "Msg", i+1)
	}

	limit := 3
	offset := 2
	c, w := timmyTestContext("GET", "/", timmyTestUserAlice)
	params := ListTimmyChatMessagesParams{Limit: &limit, Offset: &offset}

	server.ListTimmyChatMessages(c, mustParseTimmyUUID(timmyTestTMID5), mustParseTimmyUUID(session.ID), params)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListTimmyMessagesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 10, resp.Total)
	assert.Len(t, resp.Messages, 3)
}

func TestTimmyListMessages_WrongUser(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	session := createTestTimmySession(t, timmyTestUserAlice, timmyTestTMID5, "Alice Chat", "active")

	c, w := timmyTestContext("GET", "/", timmyTestUserBob)

	server.ListTimmyChatMessages(c, mustParseTimmyUUID(timmyTestTMID5), mustParseTimmyUUID(session.ID), ListTimmyChatMessagesParams{})

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- GetTimmyUsage tests ---

func TestTimmyGetUsage_Success(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	createTestTimmyUsage(t, timmyTestUserAlice, "sess-001", timmyTestTMID6, 5, 100, 200)
	createTestTimmyUsage(t, timmyTestUserAlice, "sess-001", timmyTestTMID6, 3, 50, 75)

	// Use explicit date range that encompasses the usage records
	now := time.Now().UTC()
	start := now.Add(-1 * time.Hour)
	end := now.Add(2 * time.Hour)

	c, w := timmyTestContext("GET", "/admin/timmy/usage", timmyTestUserAlice)

	server.GetTimmyUsage(c, GetTimmyUsageParams{
		StartDate: &start,
		EndDate:   &end,
	})

	assert.Equal(t, http.StatusOK, w.Code)

	var resp TimmyUsageResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Total)
	require.Len(t, resp.Usage, 1)

	record := resp.Usage[0]
	assert.NotNil(t, record.MessageCount)
	assert.Equal(t, 8, *record.MessageCount)
	assert.NotNil(t, record.PromptTokens)
	assert.Equal(t, 150, *record.PromptTokens)
	assert.NotNil(t, record.CompletionTokens)
	assert.Equal(t, 275, *record.CompletionTokens)
}

func TestTimmyGetUsage_WithFilters(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	createTestTimmyUsage(t, timmyTestUserAlice, "sess-a", timmyTestTMID6, 5, 100, 200)

	userUUID := mustParseTimmyUUID(timmyTestUserAlice)
	tmUUID := mustParseTimmyUUID(timmyTestTMID6)
	now := time.Now().UTC()
	start := now.Add(-1 * time.Hour)
	end := now.Add(2 * time.Hour)

	c, w := timmyTestContext("GET", "/admin/timmy/usage", timmyTestUserAlice)

	server.GetTimmyUsage(c, GetTimmyUsageParams{
		UserId:        &userUUID,
		ThreatModelId: &tmUUID,
		StartDate:     &start,
		EndDate:       &end,
	})

	assert.Equal(t, http.StatusOK, w.Code)

	var resp TimmyUsageResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Total)
}

// --- GetTimmyStatus tests ---

func TestTimmyGetStatus_NilVectorManager(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	// vectorManager is nil by default
	c, w := timmyTestContext("GET", "/admin/timmy/status", "user-admin")

	server.GetTimmyStatus(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp TimmyStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.ActiveSessions)
	assert.Equal(t, 0, resp.LoadedIndexes)
	assert.Equal(t, 0, resp.MemoryUsedBytes)
	assert.Equal(t, 0, resp.MemoryBudgetBytes)
}

func TestTimmyGetStatus_WithVectorManager(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	db := setupTimmyTestDB(t)
	embStore := NewGormTimmyEmbeddingStore(db)
	vm := NewVectorIndexManager(embStore, 100, 300)
	server.SetVectorManager(vm)

	c, w := timmyTestContext("GET", "/admin/timmy/status", "user-admin")

	server.GetTimmyStatus(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp TimmyStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.LoadedIndexes)
	// Budget should be 100 MB in bytes
	assert.Equal(t, 100*1024*1024, resp.MemoryBudgetBytes)
}

// --- SSE endpoint error path tests ---

func TestTimmyCreateSession_NotConfigured(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	// timmySessionManager is nil by default
	c, w := timmyTestContext("POST", "/", timmyTestUserAlice)

	server.CreateTimmyChatSession(c, mustParseTimmyUUID(timmyTestTMID7))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestTimmyCreateSession_Unauthenticated(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", nil)

	server.CreateTimmyChatSession(c, mustParseTimmyUUID(timmyTestTMID7))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTimmyCreateMessage_NotConfigured(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	session := createTestTimmySession(t, timmyTestUserAlice, timmyTestTMID8, "Session", "active")

	// timmySessionManager is nil
	c, w := timmyTestContext("POST", "/", timmyTestUserAlice)
	c.Request = httptest.NewRequest("POST", "/", timmyJSONBody(t, map[string]string{"content": "hello"}))
	c.Request.Header.Set("Content-Type", "application/json")

	server.CreateTimmyChatMessage(c, mustParseTimmyUUID(timmyTestTMID8), mustParseTimmyUUID(session.ID))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestTimmyCreateMessage_SessionNotFound(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	c, w := timmyTestContext("POST", "/", timmyTestUserAlice)

	server.CreateTimmyChatMessage(c, mustParseTimmyUUID(timmyTestTMID8), mustParseTimmyUUID(timmyTestFakeUUID))

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTimmyCreateMessage_WrongUser(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	session := createTestTimmySession(t, timmyTestUserAlice, timmyTestTMID8, "Alice Session", "active")

	c, w := timmyTestContext("POST", "/", timmyTestUserBob)

	server.CreateTimmyChatMessage(c, mustParseTimmyUUID(timmyTestTMID8), mustParseTimmyUUID(session.ID))

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestTimmyCreateMessage_EmptyContent(t *testing.T) {
	server, cleanup := setupTimmyHandlerTest(t)
	defer cleanup()

	session := createTestTimmySession(t, timmyTestUserAlice, timmyTestTMID8, "Session", "active")
	server.timmySessionManager = &TimmySessionManager{} // non-nil

	c, w := timmyTestContext("POST", "/", timmyTestUserAlice)
	c.Request = httptest.NewRequest("POST", "/", timmyJSONBody(t, map[string]string{"content": ""}))
	c.Request.Header.Set("Content-Type", "application/json")

	server.CreateTimmyChatMessage(c, mustParseTimmyUUID(timmyTestTMID8), mustParseTimmyUUID(session.ID))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- Helper functions ---

func mustParseTimmyUUID(s string) openapi_types.UUID {
	var u openapi_types.UUID
	_ = u.UnmarshalText([]byte(s))
	return u
}
