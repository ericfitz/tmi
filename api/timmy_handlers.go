package api

// Timmy chat endpoint handlers.
// These implement the ServerInterface methods generated from the OpenAPI spec.

import (
	"context"
	"encoding/json"
	"fmt"
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
// SEM@c309061af96f4db6e2d3a7da1d077b6a6f2f3c75: create a new Timmy chat session and stream preparation progress to the caller via SSE
func (s *Server) CreateTimmyChatSession(c *gin.Context, threatModelId ThreatModelId) {
	logger := slogging.Get().WithContext(c)

	userID, err := getTimmyUserID(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	rt, rtErr := s.getTimmyRuntime(c.Request.Context())
	if rtErr != nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy is temporarily unavailable"))
		return
	}
	if rt == nil || rt.SessionManager == nil {
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
		if err := sse.SendEvent("progress", map[string]any{
			"phase":       phase,
			"entity_type": entityType,
			"entity_name": entityName,
			"progress":    progress,
			"detail":      detail,
		}); err != nil {
			// Latched by the writer, so the IsClientGone check above
			// short-circuits every later callback: snapshotting continues (the
			// session is still worth creating and is readable via
			// GET .../chat/sessions) but we stop writing to a dead socket.
			logger.Warn("Timmy: progress event not delivered (%s): %v", phase, err)
		}
	}

	session, skipped, createErr := rt.SessionManager.CreateSession(
		ctx, userID, threatModelId.String(), title, progressCb,
	)
	if createErr != nil {
		logger.Error("Failed to create Timmy session: %v", createErr)
		sendTimmyError(c, sse, "session_creation_failed", createErr)
		return
	}

	if m := tmiotel.GlobalMetrics; m != nil {
		m.TimmyActiveSessions.Add(ctx, 1)
	}

	// Send skipped_sources event if any documents were excluded
	if len(skipped) > 0 {
		if err := sse.SendEvent("skipped_sources", skipped); err != nil {
			logger.Warn("Timmy: skipped_sources not delivered: %v", err)
		}
	}

	// Send session_created event with the session data. The session exists in
	// the database either way — a client that misses this event recovers by
	// listing sessions — but losing it is worth a log line, not silence.
	apiSession := timmySessionToAPI(session)
	if err := sse.SendEvent("session_created", apiSession); err != nil {
		logger.Warn("Timmy: session created but session_created was not delivered: %v", err)
		return // ready would fail identically
	}

	// Send ready event
	if err := sse.SendEvent("ready", map[string]string{"status": "ready"}); err != nil {
		logger.Warn("Timmy: ready event not delivered: %v", err)
	}
}

// ListTimmyChatSessions lists the current user's sessions for a threat model.
// SEM@773397b4fdff89166751fd8b5643ac59abce3367: list the authenticated user's Timmy chat sessions for a threat model with pagination (reads DB)
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
// SEM@773397b4fdff89166751fd8b5643ac59abce3367: fetch a single Timmy chat session, verifying ownership and threat model match (reads DB)
func (s *Server) GetTimmyChatSession(c *gin.Context, threatModelId ThreatModelId, sessionId SessionId) {
	session, err := s.getAndVerifyTimmySession(c, threatModelId, sessionId)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	c.JSON(http.StatusOK, timmySessionToAPI(session))
}

// DeleteTimmyChatSession soft-deletes a session.
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: soft-delete a Timmy chat session after verifying ownership (reads DB)
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

	if deleteErr := GlobalTimmySessionStore.SoftDelete(c.Request.Context(), string(session.ID)); deleteErr != nil {
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
// SEM@c309061af96f4db6e2d3a7da1d077b6a6f2f3c75: send a user message to Timmy and stream the assistant response token-by-token via SSE
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

	rt, rtErr := s.getTimmyRuntime(c.Request.Context())
	if rtErr != nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy is temporarily unavailable"))
		return
	}
	if rt == nil || rt.SessionManager == nil {
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

	// Status callback sends SSE status events (#393). Per the documented
	// ordering contract, all status events fire BEFORE message_start —
	// once tokens begin streaming, no further status events are emitted.
	statusCb := func(phase, entityType, entityName, detail string) {
		if sse.IsClientGone() {
			return
		}
		payload := map[string]any{"phase": phase}
		if entityType != "" {
			payload["entity_type"] = entityType
		}
		if entityName != "" {
			payload["entity_name"] = entityName
		}
		if detail != "" {
			payload["detail"] = detail
		}
		if err := sse.SendEvent("status", payload); err != nil {
			// Latched by the writer; the token callback aborts generation on
			// the next call. Nothing useful to do here beyond not pretending
			// the event was delivered.
			logger.Warn("Timmy: status event not delivered (%s): %v", phase, err)
		}
	}

	// llmCancel is assigned below, once the LLM context exists. The token
	// callback captures it so a client that stops receiving also stops
	// generation — see the abort path inside tokenCb.
	var llmCancel context.CancelFunc

	// Token callback sends SSE token events. message_start is deferred
	// until the first token arrives so it appears AFTER any status events
	// (matching the recommended ordering in the OpenAPI spec).
	messageStartSent := false
	// abortIfUnreachable stops the in-flight LLM call when the client can no
	// longer receive. Returning early only stops us WRITING; the model keeps
	// generating, and every one of those tokens is billed for a stream nobody
	// is reading. Production showed 15.7s of exactly that after a mid-stream
	// write failure. Cancelling the LLM context ends generation instead.
	abortIfUnreachable := func() bool {
		if !sse.IsClientGone() {
			return false
		}
		if llmCancel != nil {
			logger.Warn("Timmy: client unreachable (%v); cancelling LLM generation", sse.Err())
			llmCancel()
		}
		return true
	}
	tokenCb := func(token string) {
		if abortIfUnreachable() {
			return
		}
		if !messageStartSent {
			messageStartSent = true
			if err := sse.SendEvent("message_start", map[string]string{"status": "processing"}); err != nil {
				abortIfUnreachable()
				return
			}
		}
		if err := sse.SendToken(token); err != nil {
			abortIfUnreachable()
		}
	}

	// Create a dedicated context for the LLM call. Two important properties:
	//
	//  1. Strip the parent deadline. The global ContextTimeout middleware imposes
	//     a 30s deadline on every request, but `context.WithTimeout(parent, 120s)`
	//     can only SHORTEN a parent's deadline, never extend it — so deriving the
	//     LLM context from `ctx` directly silently capped LLM inference at 30s.
	//     `context.WithoutCancel` returns a context that preserves request-scoped
	//     values (auth, trace span, request-id) but discards the parent deadline
	//     and cancellation, letting us layer our own.
	//
	//  2. Client-disconnect handling stays in the SSE writer: the token callback
	//     short-circuits via `sse.IsClientGone()`, so we don't lose the
	//     "stop work when client disappears" behaviour by detaching from the
	//     request context's cancellation. Because this context is detached, that
	//     callback is also the ONLY thing that can end generation early — which
	//     is why it cancels this context (via llmCancel, declared above and
	//     assigned here) rather than merely returning.
	llmTimeout := 120 * time.Second
	if rt.SessionManager.config.LLMTimeoutSeconds > 0 {
		llmTimeout = time.Duration(rt.SessionManager.config.LLMTimeoutSeconds) * time.Second
	}
	llmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), llmTimeout)
	llmCancel = cancel
	defer llmCancel()

	assistantMsg, handleErr := rt.SessionManager.HandleMessage(
		llmCtx, sessionId.String(), userID, req.Content, tokenCb, statusCb,
	)
	if handleErr != nil {
		logger.Error("Failed to handle Timmy message: %v", handleErr)
		sendTimmyError(c, sse, "message_failed", handleErr)
		return
	}

	// Defensive: if the LLM returned a non-streamed response (no tokens
	// emitted), the deferred message_start above never fired. Emit it now
	// so clients always see message_start before message_end.
	if !messageStartSent {
		if err := sse.SendEvent("message_start", map[string]string{"status": "processing"}); err != nil {
			logger.Warn("Timmy: could not deliver message_start: %v", err)
		}
	}

	// Send message_end event with the assistant message. A failure here is the
	// signature the browser saw as a truncated answer: the message IS persisted
	// and readable via GET .../messages, but this client never received it, so
	// log it at warn rather than dropping it silently.
	apiMsg := timmyMessageToAPI(assistantMsg)
	if err := sse.SendEvent("message_end", apiMsg); err != nil {
		logger.Warn("Timmy: message completed but message_end was not delivered "+
			"(client will appear to have been cut off mid-answer): %v", err)
	}
}

// ListTimmyChatMessages lists message history for a session.
// SEM@773397b4fdff89166751fd8b5643ac59abce3367: list paginated message history for a Timmy chat session (reads DB)
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
// SEM@5b38b9a109d5e10e1a9a58a35a692f19c30a0ed5: fetch aggregated Timmy token usage statistics for an optional user and threat model over a date range (reads DB)
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
// SEM@c309061af96f4db6e2d3a7da1d077b6a6f2f3c75: return current Timmy vector memory and session status metrics (reads DB)
func (s *Server) GetTimmyStatus(c *gin.Context) {
	rt, rtErr := s.getTimmyRuntime(c.Request.Context())
	if rtErr != nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy is temporarily unavailable"))
		return
	}
	if rt == nil || rt.VectorManager == nil {
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

	status := rt.VectorManager.GetStatus()

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

// RefreshTimmySources re-scans sources for an active session, picking up
// any documents whose access_status has changed to "accessible".
// SEM@c309061af96f4db6e2d3a7da1d077b6a6f2f3c75: re-snapshot content sources for a session, updating the stored source snapshot (reads DB)
func (s *Server) RefreshTimmySources(c *gin.Context, threatModelId ThreatModelId, sessionId SessionId) {
	logger := slogging.Get().WithContext(c)

	session, err := s.getAndVerifyTimmySession(c, threatModelId, sessionId)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	rt, rtErr := s.getTimmyRuntime(c.Request.Context())
	if rtErr != nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy is temporarily unavailable"))
		return
	}
	if rt == nil || rt.SessionManager == nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy is not configured"))
		return
	}

	// Re-snapshot sources
	sources, skipped, snapshotErr := rt.SessionManager.SnapshotSources(
		c.Request.Context(), threatModelId.String(),
	)
	if snapshotErr != nil {
		logger.Error("Failed to refresh sources: %v", snapshotErr)
		HandleRequestError(c, ServerError("Failed to refresh sources"))
		return
	}

	// Update session snapshot
	snapshotJSON, _ := json.Marshal(sources)
	if updateErr := GlobalTimmySessionStore.UpdateSnapshot(c.Request.Context(), string(session.ID), models.JSONRaw(snapshotJSON)); updateErr != nil {
		logger.Error("Failed to update session snapshot: %v", updateErr)
		HandleRequestError(c, ServerError("Failed to update session"))
		return
	}

	// Ensure skipped_sources is an empty array, not null (per OpenAPI schema)
	if skipped == nil {
		skipped = []SkippedSource{}
	}
	c.JSON(http.StatusOK, map[string]any{
		"source_count":    len(sources),
		"skipped_sources": skipped,
	})
}

// RequestDocumentAccess re-sends the access request for a pending_access document.
// SEM@8429fbdd74c6f347eff47e11551b900e16a1dc06: re-send an access request for a pending_access document via its content source
func (s *Server) RequestDocumentAccess(c *gin.Context, threatModelId ThreatModelId, documentId DocumentId) {
	logger := slogging.Get().WithContext(c)

	if GlobalDocumentRepository == nil {
		HandleRequestError(c, ServiceUnavailableError("Document store is not configured"))
		return
	}

	doc, err := GlobalDocumentRepository.Get(c.Request.Context(), documentId.String())
	if err != nil {
		HandleRequestError(c, NotFoundError("Document not found"))
		return
	}

	// Only allow access requests for documents in pending_access status
	if doc.AccessStatus == nil || *doc.AccessStatus != DocumentAccessStatusPendingAccess {
		status := DocumentAccessStatus("unknown")
		if doc.AccessStatus != nil {
			status = *doc.AccessStatus
		}
		HandleRequestError(c, ConflictError(fmt.Sprintf(
			"Document access status is '%s', not 'pending_access'. Only pending_access documents can have access requested.",
			status,
		)))
		return
	}

	csBundle := s.getContentSourceBundle(c.Request.Context())
	if csBundle == nil || csBundle.Sources == nil {
		HandleRequestError(c, ServiceUnavailableError("Content pipeline not configured"))
		return
	}

	src, ok := csBundle.Sources.FindSource(c.Request.Context(), doc.Uri)
	if !ok {
		HandleRequestError(c, &RequestError{
			Status:  422,
			Code:    "no_source",
			Message: "no content source available for this document's URI",
		})
		return
	}

	requester, ok := src.(AccessRequester)
	if !ok {
		HandleRequestError(c, &RequestError{
			Status:  422,
			Code:    "access_request_not_supported",
			Message: "the content source for this document does not support access requests",
		})
		return
	}

	if reqErr := requester.RequestAccess(c.Request.Context(), doc.Uri); reqErr != nil {
		logger.Error("Failed to request access for document %s: %v", documentId, reqErr)
		HandleRequestError(c, ServerError("Failed to request access"))
		return
	}

	c.JSON(http.StatusOK, map[string]string{
		"status":  "access_requested",
		"message": "access request has been sent to the document owner",
	})
}

// --- Helper functions ---

// sendTimmyError reports a handler failure on an SSE endpoint. Before any
// byte is written the HTTP status is still ours to set (#652): strip the
// SSE headers and return a normal JSON error. Once the stream is committed
// (progress/token events), fall back to an SSE error event.
// SEM marker pending: run `/sem-annotate --update api/timmy_handlers.go api/timmy_session_manager.go` (new entity, no commit sha yet).
func sendTimmyError(c *gin.Context, sse *SSEWriter, code string, err error) {
	if !c.Writer.Written() {
		c.Writer.Header().Del("X-Accel-Buffering")
		c.Writer.Header().Set("Content-Type", "application/json")
		HandleRequestError(c, err)
		return
	}
	if sendErr := sse.SendError(code, err.Error()); sendErr != nil {
		slogging.Get().WithContext(c).Warn("Timmy: could not deliver %s: %v", code, sendErr)
	}
}

// getTimmyUserID extracts the authenticated user's internal UUID from the gin context.
// SEM@773397b4fdff89166751fd8b5643ac59abce3367: extract the authenticated user's internal UUID from the Gin context (pure)
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
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: fetch a Timmy session and verify it belongs to the authenticated user and requested threat model (reads DB)
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

	if string(session.ThreatModelID) != threatModelId.String() {
		return nil, NotFoundError("session not found in the specified threat model")
	}

	if string(session.UserID) != userID {
		return nil, ForbiddenError("you do not have access to this session")
	}

	return session, nil
}

// timmySessionToAPI converts a GORM model TimmySession to the generated API type.
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: convert a GORM TimmySession model to its API DTO including source snapshot (pure)
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
		SystemPromptHash: nilIfEmpty(s.SystemPromptHash.String),
	}

	if s.Title != "" {
		title := string(s.Title)
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
// SEM@773397b4fdff89166751fd8b5643ac59abce3367: convert a GORM TimmyMessage model to its API DTO (pure)
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
// SEM@773397b4fdff89166751fd8b5643ac59abce3367: return a pointer to the string if non-empty, otherwise nil (pure)
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// toInt converts an any value to int, handling int64 and int types.
// SEM@773397b4fdff89166751fd8b5643ac59abce3367: convert an any value to int, handling int, int64, and float64 types (pure)
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
// SEM@773397b4fdff89166751fd8b5643ac59abce3367: convert an any value to float32, handling numeric types (pure)
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
