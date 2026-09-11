package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const errTicketSessionMismatch = "ticket session mismatch"

// mockTicketStore is a minimal TicketStore for testing TicketValidator session_id cross-check.
type mockTicketStore struct {
	userID       string
	provider     string
	internalUUID string
	sessionID    string
	tokenHash    string
	credentialID string
	err          error
}

func (m *mockTicketStore) IssueTicket(_ context.Context, _ api.TicketClaims, _ time.Duration) (string, error) {
	return "mock-ticket", nil
}

func (m *mockTicketStore) ValidateTicket(_ context.Context, _ string) (api.TicketClaims, error) {
	if m.err != nil {
		return api.TicketClaims{}, m.err
	}
	return api.TicketClaims{
		UserID: m.userID, Provider: m.provider, InternalUUID: m.internalUUID, SessionID: m.sessionID,
		TokenHash: m.tokenHash, CredentialID: m.credentialID,
	}, nil
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

// #869: the ticket path must expose the minting token's revocation handles so
// checkRevocation can refuse the upgrade after a revoke.
func TestTicketValidator_SetsRevocationContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := &mockTicketStore{
		userID: "user123", provider: "tmi", sessionID: "session-abc",
		tokenHash: "hash-1", credentialID: "cred-1",
	}
	validator := &TicketValidator{ticketStore: store}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/ws/diagrams/123?ticket=tok", nil)

	_ = validator.ValidateTicket(c, "tok") // user lookup fails (no DB); context is set before it
	if got := c.GetString("authTokenHash"); got != "hash-1" {
		t.Fatalf("authTokenHash = %q, want hash-1", got)
	}
	if got := c.GetString("serviceAccountCredentialID"); got != "cred-1" {
		t.Fatalf("serviceAccountCredentialID = %q, want cred-1", got)
	}
	if !c.GetBool("isServiceAccount") {
		t.Fatal("isServiceAccount should be true for a credential-minted ticket")
	}
}

// #869: checkRevocation is shared by the JWT and ticket paths; a blacklisted
// token hash or a revoked credential must yield 401.
func TestCheckRevocation_TicketHandles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()
	blacklist := auth.NewTokenBlacklist(client, nil)
	a := &JWTAuthenticator{blacklistChecker: NewTokenBlacklistChecker(blacklist)}
	logger := slogging.Get()

	newCtx := func(hash, cred string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request, _ = http.NewRequest(http.MethodGet, "/ws/diagrams/123", nil)
		c.Set("authTokenHash", hash)
		if cred != "" {
			c.Set("serviceAccountCredentialID", cred)
		}
		return c
	}

	if e := a.checkRevocation(newCtx("clean", "cred-ok"), logger); e != nil {
		t.Fatalf("clean token/credential rejected: %v", e)
	}
	if e := a.checkRevocation(newCtx("", ""), logger); e != nil {
		t.Fatalf("legacy ticket without hash rejected: %v", e)
	}

	if err := mr.Set("blacklist:token:"+auth.HashToken("tok"), "blacklisted"); err != nil {
		t.Fatalf("miniredis set: %v", err)
	}
	if e := a.checkRevocation(newCtx(auth.HashToken("tok"), ""), logger); e == nil || e.StatusCode != http.StatusUnauthorized {
		t.Fatalf("blacklisted token hash not rejected with 401: %v", e)
	}

	if err := blacklist.RevokeCredential(context.Background(), "cred-gone", time.Hour); err != nil {
		t.Fatalf("RevokeCredential: %v", err)
	}
	if e := a.checkRevocation(newCtx("clean", "cred-gone"), logger); e == nil || e.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked credential not rejected with 401: %v", e)
	}
}

// Verify that the mockTicketStore implements the TicketStore interface at compile time
var _ api.TicketStore = (*mockTicketStore)(nil)
