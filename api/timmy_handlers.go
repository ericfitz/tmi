package api

// Timmy chat endpoint handlers.
// These implement the ServerInterface methods generated from the OpenAPI spec.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/ericfitz/tmi/api/models"
	tmiotel "github.com/ericfitz/tmi/internal/otel"
	"github.com/ericfitz/tmi/internal/slogging"
)

// CreateTimmyChatSession creates a new chat session and streams preparation progress via SSE.
func (s *Server) CreateTimmyChatSession(c *gin.Context, threatModelId ThreatModelId) {
	logger := slogging.Get().WithContext(c)

	userID, err := getTimmyUserID(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if s.timmySessionManager == nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy is not configured"))
		return
	}

	// Parse optional request body
	var title string
	var req CreateTimmySessionRequest
	if c.Request.ContentLength > 0 {
		if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
			HandleRequestError(c, InvalidInputError("invalid request body: "+bindErr.Error()))
			return
		}
		if req.Title != nil {
			title = *req.Title
		}
	}

	// Set up SSE writer
	sse := NewSSEWriter(c)

	tracer := otel.Tracer("tmi.timmy")
	ctx, span := tracer.Start(c.Request.Context(), "timmy.session.create",
		trace.WithAttributes(
			attribute.String("tmi.sse.stream_type", "chat_session_create"),
			attribute.String("tmi.threat_model.id", threatModelId.String()),
		),
	)
	defer span.End()

	// Progress callback sends SSE events
	progressCb := func(phase, entityType, entityName string, progress int, detail string) {
		if sse.IsClientGone() {
			return
		}
		_ = sse.SendEvent("progress", map[string]any{
			"phase":       phase,
			"entity_type": entityType,
			"entity_name": entityName,
			"progress":    progress,
			"detail":      detail,
		})
	}

	session, createErr := s.timmySessionManager.CreateSession(
		ctx, userID, threatModelId.String(), title, progressCb,
	)
	if createErr != nil {
		logger.Error("Failed to create Timmy session: %v", createErr)
		_ = sse.SendError("session_creation_failed", createErr.Error())
		return
	}

	if m := tmiotel.GlobalMetrics; m != nil {
		m.TimmyActiveSessions.Add(ctx, 1)
	}

	// Send session_created event with the session data
	apiSession := timmySessionToAPI(session)
	_ = sse.SendEvent("session_created", apiSession)

	// Send ready event
	_ = sse.SendEvent("ready", map[string]string{"status": "ready"})
}

// ListTimmyChatSessions lists the current user's sessions for a threat model.
func (s *Server) ListTimmyChatSessions(c *gin.Context, threatModelId ThreatModelId, params ListTimmyChatSessionsParams) {
	userID, err := getTimmyUserID(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if GlobalTimmySessionStore == nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy session store is not configured"))
		return
	}

	// Validate and extract pagination
	if valErr := ValidatePaginationParams(params.Limit, params.Offset); valErr != nil {
		HandleRequestError(c, valErr)
		return
	}

	limit := 20
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}

	sessions, total, listErr := GlobalTimmySessionStore.ListByUserAndThreatModel(
		c.Request.Context(), userID, threatModelId.String(), offset, limit,
	)
	if listErr != nil {
		HandleRequestError(c, StoreErrorToRequestError(listErr, "sessions not found", "failed to list sessions"))
		return
	}

	apiSessions := make([]TimmyChatSession, 0, len(sessions))
	for i := range sessions {
		apiSessions = append(apiSessions, timmySessionToAPI(&sessions[i]))
	}

	c.JSON(http.StatusOK, ListTimmySessionsResponse{
		Sessions: apiSessions,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	})
}

// GetTimmyChatSession retrieves a specific session.
func (s *Server) GetTimmyChatSession(c *gin.Context, threatModelId ThreatModelId, sessionId SessionId) {
	session, err := s.getAndVerifyTimmySession(c, threatModelId, sessionId)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	c.JSON(http.StatusOK, timmySessionToAPI(session))
}

// DeleteTimmyChatSession soft-deletes a session.
func (s *Server) DeleteTimmyChatSession(c *gin.Context, threatModelId ThreatModelId, sessionId SessionId) {
	session, err := s.getAndVerifyTimmySession(c, threatModelId, sessionId)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if GlobalTimmySessionStore == nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy session store is not configured"))
		return
	}

	if deleteErr := GlobalTimmySessionStore.SoftDelete(c.Request.Context(), session.ID); deleteErr != nil {
		HandleRequestError(c, StoreErrorToRequestError(deleteErr, "session not found", "failed to delete session"))
		return
	}

	if m := tmiotel.GlobalMetrics; m != nil {
		m.TimmyActiveSessions.Add(c.Request.Context(), -1)
	}

	c.Status(http.StatusNoContent)
	c.Writer.WriteHeaderNow()
}

// CreateTimmyChatMessage sends a message and streams the assistant's response via SSE.
func (s *Server) CreateTimmyChatMessage(c *gin.Context, threatModelId ThreatModelId, sessionId SessionId) {
	logger := slogging.Get().WithContext(c)

	// Verify session ownership
	_, verifyErr := s.getAndVerifyTimmySession(c, threatModelId, sessionId)
	if verifyErr != nil {
		HandleRequestError(c, verifyErr)
		return
	}

	userID, err := getTimmyUserID(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if s.timmySessionManager == nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy is not configured"))
		return
	}

	// Parse request body
	var req CreateTimmyMessageRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		HandleRequestError(c, InvalidInputError("invalid request body: "+bindErr.Error()))
		return
	}

	if req.Content == "" {
		HandleRequestError(c, InvalidInputError("message content is required"))
		return
	}

	// Set up SSE writer
	sse := NewSSEWriter(c)

	tracer := otel.Tracer("tmi.timmy")
	ctx, span := tracer.Start(c.Request.Context(), "timmy.message.handle",
		trace.WithAttributes(
			attribute.String("tmi.sse.stream_type", "chat_message"),
			attribute.String("tmi.sse.session_id", sessionId.String()),
		),
	)
	defer span.End()

	// Send message_start event
	_ = sse.SendEvent("message_start", map[string]string{"status": "processing"})

	// Token callback sends SSE token events
	tokenCb := func(token string) {
		if sse.IsClientGone() {
			return
		}
		_ = sse.SendToken(token)
	}

	// Create a dedicated context for the LLM call with a longer timeout than the
	// global 30s request timeout. LLM inference with large conversation contexts
	// can take well over 30s, especially with local models.
	llmTimeout := 120 * time.Second
	if s.timmySessionManager != nil && s.timmySessionManager.config.LLMTimeoutSeconds > 0 {
		llmTimeout = time.Duration(s.timmySessionManager.config.LLMTimeoutSeconds) * time.Second
	}
	llmCtx, llmCancel := context.WithTimeout(ctx, llmTimeout)
	defer llmCancel()

	assistantMsg, handleErr := s.timmySessionManager.HandleMessage(
		llmCtx, sessionId.String(), userID, req.Content, tokenCb,
	)
	if handleErr != nil {
		logger.Error("Failed to handle Timmy message: %v", handleErr)
		_ = sse.SendError("message_failed", handleErr.Error())
		return
	}

	// Send message_end event with the assistant message
	apiMsg := timmyMessageToAPI(assistantMsg)
	_ = sse.SendEvent("message_end", apiMsg)
}

// ListTimmyChatMessages lists message history for a session.
func (s *Server) ListTimmyChatMessages(c *gin.Context, threatModelId ThreatModelId, sessionId SessionId, params ListTimmyChatMessagesParams) {
	// Verify session ownership
	_, verifyErr := s.getAndVerifyTimmySession(c, threatModelId, sessionId)
	if verifyErr != nil {
		HandleRequestError(c, verifyErr)
		return
	}

	if GlobalTimmyMessageStore == nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy message store is not configured"))
		return
	}

	// Validate and extract pagination
	if valErr := ValidatePaginationParams(params.Limit, params.Offset); valErr != nil {
		HandleRequestError(c, valErr)
		return
	}

	limit := 50
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}

	messages, total, listErr := GlobalTimmyMessageStore.ListBySession(
		c.Request.Context(), sessionId.String(), offset, limit,
	)
	if listErr != nil {
		HandleRequestError(c, StoreErrorToRequestError(listErr, "messages not found", "failed to list messages"))
		return
	}

	apiMessages := make([]TimmyChatMessage, 0, len(messages))
	for i := range messages {
		apiMessages = append(apiMessages, timmyMessageToAPI(&messages[i]))
	}

	c.JSON(http.StatusOK, ListTimmyMessagesResponse{
		Messages: apiMessages,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	})
}

// GetTimmyUsage returns aggregated usage statistics (admin only).
func (s *Server) GetTimmyUsage(c *gin.Context, params GetTimmyUsageParams) {
	if GlobalTimmyUsageStore == nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy usage store is not configured"))
		return
	}

	// Set default time range: last 30 days
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -30)
	end := now

	if params.StartDate != nil {
		start = *params.StartDate
	}
	if params.EndDate != nil {
		end = *params.EndDate
	}

	// Validate date range
	if start.After(end) {
		HandleRequestError(c, InvalidInputError("start_date must be before end_date"))
		return
	}
	// Reject dates more than 10 years in the past or future as likely invalid
	tenYearsAgo := now.AddDate(-10, 0, 0)
	tenYearsFromNow := now.AddDate(10, 0, 0)
	if start.Before(tenYearsAgo) || end.After(tenYearsFromNow) {
		HandleRequestError(c, InvalidInputError("date range is outside the acceptable bounds (within 10 years of today)"))
		return
	}

	userID := ""
	if params.UserId != nil {
		userID = params.UserId.String()
	}
	tmID := ""
	if params.ThreatModelId != nil {
		tmID = params.ThreatModelId.String()
	}

	agg, aggErr := GlobalTimmyUsageStore.GetAggregated(
		c.Request.Context(), userID, tmID, start, end,
	)
	if aggErr != nil {
		HandleRequestError(c, StoreErrorToRequestError(aggErr, "usage data not found", "failed to get usage data"))
		return
	}

	// Build response using the generated TimmyUsageResponse type
	record := TimmyUsageRecord{
		MessageCount:     &agg.TotalMessages,
		PromptTokens:     &agg.TotalPromptTokens,
		CompletionTokens: &agg.TotalCompletionTokens,
		EmbeddingTokens:  &agg.TotalEmbeddingTokens,
		PeriodStart:      &start,
		PeriodEnd:        &end,
	}
	if params.UserId != nil {
		record.UserId = params.UserId
	}
	if params.ThreatModelId != nil {
		record.ThreatModelId = params.ThreatModelId
	}

	c.JSON(http.StatusOK, TimmyUsageResponse{
		Total: 1,
		Usage: []TimmyUsageRecord{record},
	})
}

// GetTimmyStatus returns current memory and index status (admin only).
func (s *Server) GetTimmyStatus(c *gin.Context) {
	if s.vectorManager == nil {
		c.JSON(http.StatusOK, TimmyStatusResponse{
			ActiveSessions:        0,
			EvictionsPressure:     0,
			EvictionsTotal:        0,
			LoadedIndexes:         0,
			MemoryBudgetBytes:     0,
			MemoryUsedBytes:       0,
			MemoryUtilizationPct:  0,
			SessionsRejectedTotal: 0,
		})
		return
	}

	status := s.vectorManager.GetStatus()

	// Sum active sessions from per-index details
	activeSessions := 0
	if indexes, ok := status["indexes"].([]map[string]any); ok {
		for _, idx := range indexes {
			activeSessions += toInt(idx["active_sessions"])
		}
	}

	c.JSON(http.StatusOK, TimmyStatusResponse{
		ActiveSessions:        activeSessions,
		EvictionsPressure:     toInt(status["evictions_pressure"]),
		EvictionsTotal:        toInt(status["evictions_total"]),
		LoadedIndexes:         toInt(status["indexes_loaded"]),
		MemoryBudgetBytes:     toInt(status["memory_budget_bytes"]),
		MemoryUsedBytes:       toInt(status["memory_used_bytes"]),
		MemoryUtilizationPct:  toFloat32(status["memory_utilization_pct"]),
		SessionsRejectedTotal: toInt(status["sessions_rejected"]),
	})
}

// --- Helper functions ---

// getTimmyUserID extracts the authenticated user's internal UUID from the gin context.
func getTimmyUserID(c *gin.Context) (string, error) {
	userInternalUUID, exists := c.Get("userInternalUUID")
	if !exists {
		return "", UnauthorizedError("user not authenticated")
	}
	userID, ok := userInternalUUID.(string)
	if !ok || userID == "" {
		return "", UnauthorizedError("user not authenticated")
	}
	return userID, nil
}

// getAndVerifyTimmySession fetches a session and verifies ownership and threat model match.
func (s *Server) getAndVerifyTimmySession(c *gin.Context, threatModelId ThreatModelId, sessionId SessionId) (*models.TimmySession, error) {
	userID, err := getTimmyUserID(c)
	if err != nil {
		return nil, err
	}

	if GlobalTimmySessionStore == nil {
		return nil, ServiceUnavailableError("Timmy session store is not configured")
	}

	session, getErr := GlobalTimmySessionStore.Get(c.Request.Context(), sessionId.String())
	if getErr != nil {
		return nil, StoreErrorToRequestError(getErr, "session not found", "failed to get session")
	}

	if session.ThreatModelID != threatModelId.String() {
		return nil, NotFoundError("session not found in the specified threat model")
	}

	if session.UserID != userID {
		return nil, ForbiddenError("you do not have access to this session")
	}

	return session, nil
}

// timmySessionToAPI converts a GORM model TimmySession to the generated API type.
func timmySessionToAPI(s *models.TimmySession) TimmyChatSession {
	id := openapi_types.UUID{}
	_ = id.UnmarshalText([]byte(s.ID))

	tmID := openapi_types.UUID{}
	_ = tmID.UnmarshalText([]byte(s.ThreatModelID))

	userID := openapi_types.UUID{}
	_ = userID.UnmarshalText([]byte(s.UserID))

	status := TimmyChatSessionStatus(s.Status)

	apiSession := TimmyChatSession{
		Id:               &id,
		ThreatModelId:    &tmID,
		UserId:           &userID,
		Status:           status,
		CreatedAt:        &s.CreatedAt,
		ModifiedAt:       &s.ModifiedAt,
		SystemPromptHash: nilIfEmpty(s.SystemPromptHash),
	}

	if s.Title != "" {
		title := s.Title
		apiSession.Title = &title
	}

	// Convert source snapshot from raw JSON to the API struct
	if s.SourceSnapshot != nil {
		var entries []SourceSnapshotEntry
		if err := json.Unmarshal(s.SourceSnapshot, &entries); err == nil && len(entries) > 0 {
			snapshots := make([]struct {
				EntityId   *openapi_types.UUID `json:"entity_id,omitempty"`
				EntityType *string             `json:"entity_type,omitempty"`
			}, 0, len(entries))
			for _, e := range entries {
				entryID := openapi_types.UUID{}
				_ = entryID.UnmarshalText([]byte(e.EntityID))
				entryType := e.EntityType
				snapshots = append(snapshots, struct {
					EntityId   *openapi_types.UUID `json:"entity_id,omitempty"`
					EntityType *string             `json:"entity_type,omitempty"`
				}{
					EntityId:   &entryID,
					EntityType: &entryType,
				})
			}
			apiSession.SourceSnapshot = &snapshots
		}
	}

	return apiSession
}

// timmyMessageToAPI converts a GORM model TimmyMessage to the generated API type.
func timmyMessageToAPI(m *models.TimmyMessage) TimmyChatMessage {
	id := openapi_types.UUID{}
	_ = id.UnmarshalText([]byte(m.ID))

	sessID := openapi_types.UUID{}
	_ = sessID.UnmarshalText([]byte(m.SessionID))

	seq := m.Sequence
	tokenCount := m.TokenCount

	return TimmyChatMessage{
		Id:         &id,
		SessionId:  &sessID,
		Role:       TimmyChatMessageRole(m.Role),
		Content:    string(m.Content),
		Sequence:   &seq,
		TokenCount: &tokenCount,
		CreatedAt:  &m.CreatedAt,
	}
}

// nilIfEmpty returns a pointer to the string if non-empty, or nil.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// toInt converts an any value to int, handling int64 and int types.
func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	default:
		return 0
	}
}

// toFloat32 converts an any value to float32.
func toFloat32(v any) float32 {
	switch val := v.(type) {
	case float32:
		return val
	case float64:
		return float32(val)
	case int:
		return float32(val)
	case int64:
		return float32(val)
	default:
		return 0
	}
}
