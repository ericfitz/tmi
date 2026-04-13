package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupWsTicketTestServer creates a Server with a WebSocketHub containing a test session,
// and a threat model that grants the given owner read access.
// Returns the server, session ID, and threat model ID.
func setupWsTicketTestServer(t *testing.T, ownerProviderID string) (*Server, string, string) {
	t.Helper()

	// Initialize test stores
	InitTestFixtures()

	// Create a server with a ticket store and WebSocket hub
	wsHub := NewWebSocketHubForTests()
	server := &Server{
		wsHub:       wsHub,
		ticketStore: NewInMemoryTicketStore(),
	}

	// The test fixtures already set up a threat model in ThreatModelStore with
	// TestFixtures.OwnerUser as owner. Use that threat model ID.
	tmID := TestFixtures.ThreatModelID

	// Create a session in the hub
	sessionID := uuid.New().String()
	diagramID := TestFixtures.DiagramID

	wsHub.mu.Lock()
	wsHub.Diagrams[diagramID] = &DiagramSession{
		ID:            sessionID,
		DiagramID:     diagramID,
		ThreatModelID: tmID,
		State:         SessionStateActive,
		Clients:       make(map[*WebSocketClient]bool),
		Broadcast:     make(chan []byte, 256),
		Register:      make(chan *WebSocketClient),
		Unregister:    make(chan *WebSocketClient),
		LastActivity:  time.Now(),
		CreatedAt:     time.Now(),
		Hub:           wsHub,
		Host:          ResolvedUser{Provider: "test", ProviderID: ownerProviderID, Email: ownerProviderID},
		DeniedUsers:   make(map[string]bool),
	}
	wsHub.mu.Unlock()

	return server, sessionID, tmID
}

func TestGetWsTicket_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// The test fixtures owner is testEmailDefault which is the OwnerUser
	server, sessionID, _ := setupWsTicketTestServer(t, TestFixtures.OwnerUser)

	c, w := CreateTestGinContext("GET", "/ws/ticket?session_id="+sessionID)
	// Set auth context matching the owner of the test fixture threat model
	SetFullUserContext(c, TestFixtures.OwnerUser, TestFixtures.OwnerUser, uuid.New().String(), "test", nil)

	params := GetWsTicketParams{
		SessionId: uuid.MustParse(sessionID),
	}

	server.GetWsTicket(c, params)

	assert.Equal(t, http.StatusOK, w.Code, "Should return 200 for valid session and authenticated user")

	var resp WsTicketResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err, "Response should be valid JSON")
	assert.NotEmpty(t, resp.Ticket, "Ticket should not be empty")

	// Verify Cache-Control header
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"), "Should set Cache-Control: no-store")
}

func TestGetWsTicket_Unauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server, sessionID, _ := setupWsTicketTestServer(t, "some-provider-id")

	c, w := CreateTestGinContext("GET", "/ws/ticket?session_id="+sessionID)
	// Do NOT set userEmail - simulate unauthenticated request

	params := GetWsTicketParams{
		SessionId: uuid.MustParse(sessionID),
	}

	server.GetWsTicket(c, params)

	assert.Equal(t, http.StatusUnauthorized, w.Code, "Should return 401 for unauthenticated request")

	var errResp Error
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "unauthorized", errResp.Error)
}

func TestGetWsTicket_SessionNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server, _, _ := setupWsTicketTestServer(t, TestFixtures.OwnerUser)

	// Use a random session ID that does not exist
	nonexistentSessionID := uuid.New().String()
	c, w := CreateTestGinContext("GET", "/ws/ticket?session_id="+nonexistentSessionID)
	SetFullUserContext(c, TestFixtures.OwnerUser, TestFixtures.OwnerUser, uuid.New().String(), "test", nil)

	params := GetWsTicketParams{
		SessionId: uuid.MustParse(nonexistentSessionID),
	}

	server.GetWsTicket(c, params)

	assert.Equal(t, http.StatusNotFound, w.Code, "Should return 404 for nonexistent session")
}

func TestGetWsTicket_CacheControlHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server, sessionID, _ := setupWsTicketTestServer(t, TestFixtures.OwnerUser)

	c, w := CreateTestGinContext("GET", "/ws/ticket?session_id="+sessionID)
	SetFullUserContext(c, TestFixtures.OwnerUser, TestFixtures.OwnerUser, uuid.New().String(), "test", nil)

	params := GetWsTicketParams{
		SessionId: uuid.MustParse(sessionID),
	}

	server.GetWsTicket(c, params)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"), "Cache-Control must be no-store")
}

func TestGetWsTicket_NoTicketStore(t *testing.T) {
	gin.SetMode(gin.TestMode)

	InitTestFixtures()

	// Server without ticket store
	server := &Server{
		wsHub:       NewWebSocketHubForTests(),
		ticketStore: nil,
	}

	sessionID := uuid.New().String()
	c, w := CreateTestGinContext("GET", "/ws/ticket?session_id="+sessionID)
	SetFullUserContext(c, TestFixtures.OwnerUser, TestFixtures.OwnerUser, uuid.New().String(), "test", nil)

	params := GetWsTicketParams{
		SessionId: uuid.MustParse(sessionID),
	}

	server.GetWsTicket(c, params)

	assert.Equal(t, http.StatusInternalServerError, w.Code, "Should return 500 when ticket store is not configured")
}

func TestGetWsTicket_UnauthorizedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server, sessionID, _ := setupWsTicketTestServer(t, TestFixtures.OwnerUser)

	// Use external user who does NOT have access to the threat model
	c, w := CreateTestGinContext("GET", "/ws/ticket?session_id="+sessionID)
	SetFullUserContext(c, TestUsers.External.Email, TestUsers.External.ProviderID, TestUsers.External.InternalUUID, TestUsers.External.IdP, nil)
	c.Set("userProvider", TestUsers.External.IdP)

	params := GetWsTicketParams{
		SessionId: uuid.MustParse(sessionID),
	}

	server.GetWsTicket(c, params)

	// User without access should get 404 (not 403, to avoid revealing resource existence)
	assert.Equal(t, http.StatusNotFound, w.Code, "Should return 404 for user without access")
}
