package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api"
	"github.com/gin-gonic/gin"
)

const errTicketSessionMismatch = "ticket session mismatch"

// mockTicketStore is a minimal TicketStore for testing TicketValidator session_id cross-check.
type mockTicketStore struct {
	userID       string
	provider     string
	internalUUID string
	sessionID    string
	err          error
}

func (m *mockTicketStore) IssueTicket(_ context.Context, _, _, _, _ string, _ time.Duration) (string, error) {
	return "mock-ticket", nil
}

func (m *mockTicketStore) ValidateTicket(_ context.Context, _ string) (string, string, string, string, error) {
	if m.err != nil {
		return "", "", "", "", m.err
	}
	return m.userID, m.provider, m.internalUUID, m.sessionID, nil
}

func TestTicketValidator_SessionIDMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := &mockTicketStore{
		userID:    "user123",
		provider:  "tmi",
		sessionID: "session-abc",
	}

	// TicketValidator with nil authHandlers/config — we only test the session_id cross-check,
	// and the user lookup will fail. That is fine: we assert no error from session_id mismatch logic.
	validator := &TicketValidator{
		ticketStore: store,
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/ws/diagrams/123?ticket=tok&session_id=session-abc", nil)

	err := validator.ValidateTicket(c, "tok")
	// The error we get is from user lookup (database not available), NOT from session_id mismatch.
	// If session_id cross-check had failed, the error would be errTicketSessionMismatch.
	if err != nil && err.Error() == errTicketSessionMismatch {
		t.Fatalf("expected no session_id mismatch error, got: %v", err)
	}
}

func TestTicketValidator_SessionIDMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := &mockTicketStore{
		userID:    "user123",
		provider:  "tmi",
		sessionID: "session-abc",
	}

	validator := &TicketValidator{
		ticketStore: store,
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/ws/diagrams/123?ticket=tok&session_id=session-WRONG", nil)

	err := validator.ValidateTicket(c, "tok")
	if err == nil {
		t.Fatal("expected error for session_id mismatch, got nil")
	}
	if err.Error() != errTicketSessionMismatch {
		t.Fatalf("expected 'ticket session mismatch' error, got: %v", err)
	}
}

func TestTicketValidator_NoSessionIDQueryParam(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := &mockTicketStore{
		userID:    "user123",
		provider:  "tmi",
		sessionID: "session-abc",
	}

	validator := &TicketValidator{
		ticketStore: store,
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	// No session_id query param — cross-check should be skipped
	c.Request, _ = http.NewRequest(http.MethodGet, "/ws/diagrams/123?ticket=tok", nil)

	err := validator.ValidateTicket(c, "tok")
	// The error we get is from user lookup (database not available), NOT from session_id mismatch.
	if err != nil && err.Error() == errTicketSessionMismatch {
		t.Fatalf("expected no session_id mismatch error when query param absent, got: %v", err)
	}
}

func TestExtractToken_WebSocketTicketPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	extractor := &TokenExtractor{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/ws/diagrams/123?ticket=my-ticket-value", nil)

	tokenStr, err := extractor.ExtractToken(c)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tokenStr != "ticket:my-ticket-value" {
		t.Fatalf("expected 'ticket:my-ticket-value', got '%s'", tokenStr)
	}
}

func TestExtractToken_TicketEndpointUsesNormalAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	extractor := &TokenExtractor{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	// /ws/ticket is a REST endpoint, should NOT use ticket-based auth
	c.Request, _ = http.NewRequest(http.MethodGet, "/ws/ticket", nil)
	c.Request.Header.Set("Authorization", "Bearer my-jwt-token")

	tokenStr, err := extractor.ExtractToken(c)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tokenStr != "my-jwt-token" {
		t.Fatalf("expected 'my-jwt-token', got '%s'", tokenStr)
	}
}

func TestExtractToken_WebSocketMissingTicket(t *testing.T) {
	gin.SetMode(gin.TestMode)

	extractor := &TokenExtractor{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	// WebSocket path with no ticket param
	c.Request, _ = http.NewRequest(http.MethodGet, "/ws/diagrams/123", nil)

	_, err := extractor.ExtractToken(c)
	if err == nil {
		t.Fatal("expected error for missing ticket, got nil")
	}
}

// Verify that the mockTicketStore implements the TicketStore interface at compile time
var _ api.TicketStore = (*mockTicketStore)(nil)
