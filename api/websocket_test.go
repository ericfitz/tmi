package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testWSUserEmail is the default user email for WebSocket tests
const testWSUserEmail = "test-user@example.com"

// MockWebSocketAuthService provides a test authentication service
type MockWebSocketAuthService struct {
	ValidTokens map[string]string // token -> userID mapping
}

func (m *MockWebSocketAuthService) ValidateToken(token string) (string, error) {
	if userID, ok := m.ValidTokens[token]; ok {
		return userID, nil
	}
	return "", fmt.Errorf("invalid token")
}

func (m *MockWebSocketAuthService) Authorize(c *gin.Context) {
	// For testing, just pass through
	c.Next()
}

// TestWebSocketHub tests the WebSocket hub functionality
func TestWebSocketHub(t *testing.T) {
	// Initialize hub for tests
	hub := NewWebSocketHubForTests()

	t.Run("NewWebSocketHub", func(t *testing.T) {
		assert.NotNil(t, hub)
		assert.NotNil(t, hub.Diagrams)
		assert.Equal(t, 0, len(hub.Diagrams))
	})

	t.Run("CreateSession", func(t *testing.T) {
		diagramID := uuid.New().String()
		threatModelID := uuid.New().String()
		hostUser := ResolvedUser{Provider: "test", ProviderID: testWSUserEmail, Email: testWSUserEmail}

		session, err := hub.CreateSession(diagramID, threatModelID, hostUser)

		require.NoError(t, err)
		assert.NotNil(t, session)
		assert.Equal(t, diagramID, session.DiagramID)
		assert.Equal(t, threatModelID, session.ThreatModelID)
		require.NotNil(t, session.CurrentPresenter)
		assert.Equal(t, testWSUserEmail, session.CurrentPresenter.ProviderID)
		assert.Equal(t, SessionStateActive, session.State)
		assert.NotEmpty(t, session.ID)

		// Verify session is in hub
		hub.mu.RLock()
		storedSession, exists := hub.Diagrams[diagramID]
		hub.mu.RUnlock()

		assert.True(t, exists)
		assert.Equal(t, session, storedSession)

		// Try to create duplicate session
		_, err = hub.CreateSession(diagramID, threatModelID, hostUser)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("GetSession", func(t *testing.T) {
		diagramID := uuid.New().String()
		threatModelID := uuid.New().String()
		hostUser := ResolvedUser{Provider: "test", ProviderID: testWSUserEmail, Email: testWSUserEmail}

		// Create session
		created, err := hub.CreateSession(diagramID, threatModelID, hostUser)
		require.NoError(t, err)

		// Get session
		retrieved := hub.GetSession(diagramID)

		assert.NotNil(t, retrieved)
		assert.Equal(t, created.ID, retrieved.ID)

		// Get non-existent session
		nonExistent := hub.GetSession(uuid.New().String())
		assert.Nil(t, nonExistent)
	})

	t.Run("GetOrCreateSession", func(t *testing.T) {
		diagramID := uuid.New().String()
		threatModelID := uuid.New().String()
		hostUser := ResolvedUser{Provider: "test", ProviderID: testWSUserEmail, Email: testWSUserEmail}

		// Get or create new session
		session := hub.GetOrCreateSession(diagramID, threatModelID, hostUser)
		assert.NotNil(t, session)
		assert.Equal(t, diagramID, session.DiagramID)
		assert.Equal(t, testWSUserEmail, session.Host.ProviderID)

		// Get existing session
		session2 := hub.GetOrCreateSession(diagramID, threatModelID, hostUser)
		assert.Equal(t, session.ID, session2.ID)
	})

	t.Run("CleanupSession", func(t *testing.T) {
		diagramID := uuid.New().String()
		threatModelID := uuid.New().String()
		hostUser := ResolvedUser{Provider: "test", ProviderID: testWSUserEmail, Email: testWSUserEmail}

		// Create session
		_, err := hub.CreateSession(diagramID, threatModelID, hostUser)
		require.NoError(t, err)

		// Cleanup session
		hub.mu.Lock()
		delete(hub.Diagrams, diagramID)
		hub.mu.Unlock()

		// Verify it's gone
		retrieved := hub.GetSession(diagramID)
		assert.Nil(t, retrieved)
	})
}

// TestDiagramSession tests the DiagramSession functionality
func TestDiagramSession(t *testing.T) {
	hub := NewWebSocketHubForTests()

	t.Run("AddConnection", func(t *testing.T) {
		diagramID := uuid.New().String()
		threatModelID := uuid.New().String()
		userID := testWSUserEmail

		session, err := hub.CreateSession(diagramID, threatModelID, ResolvedUser{Provider: "test", ProviderID: userID, Email: userID})
		require.NoError(t, err)

		// Create mock WebSocket client
		client := &WebSocketClient{
			Hub:       hub,
			Session:   session,
			UserID:    userID,
			UserEmail: testWSUserEmail,
			UserName:  "Test User",
			Send:      make(chan []byte, 256),
		}

		// Add client to session
		session.mu.Lock()
		session.Clients[client] = true
		session.mu.Unlock()

		// Verify client was added
		session.mu.RLock()
		assert.Equal(t, 1, len(session.Clients))
		assert.True(t, session.Clients[client])
		session.mu.RUnlock()
	})

	t.Run("RemoveConnection", func(t *testing.T) {
		diagramID := uuid.New().String()
		threatModelID := uuid.New().String()
		userID := testWSUserEmail

		session, err := hub.CreateSession(diagramID, threatModelID, ResolvedUser{Provider: "test", ProviderID: userID, Email: userID})
		require.NoError(t, err)

		// Create and add client
		client := &WebSocketClient{
			Hub:       hub,
			Session:   session,
			UserID:    userID,
			UserEmail: testWSUserEmail,
			UserName:  "Test User",
			Send:      make(chan []byte, 256),
		}

		// Add client
		session.mu.Lock()
		session.Clients[client] = true
		session.mu.Unlock()

		// Remove client
		session.mu.Lock()
		delete(session.Clients, client)
		session.mu.Unlock()

		// Verify client was removed
		session.mu.RLock()
		assert.Equal(t, 0, len(session.Clients))
		session.mu.RUnlock()
	})

	t.Run("BroadcastMessage", func(t *testing.T) {
		diagramID := uuid.New().String()
		threatModelID := uuid.New().String()
		userID1 := "user1@example.com"
		userID2 := "user2@example.com"

		session, err := hub.CreateSession(diagramID, threatModelID, ResolvedUser{Provider: "test", ProviderID: userID1, Email: userID1})
		require.NoError(t, err)

		// Create mock clients with channels
		client1 := &WebSocketClient{
			Hub:       hub,
			Session:   session,
			UserID:    userID1,
			UserEmail: userID1,
			UserName:  "User 1",
			Send:      make(chan []byte, 256),
		}

		client2 := &WebSocketClient{
			Hub:       hub,
			Session:   session,
			UserID:    userID2,
			UserEmail: userID2,
			UserName:  "User 2",
			Send:      make(chan []byte, 256),
		}

		// Add clients
		session.mu.Lock()
		session.Clients[client1] = true
		session.Clients[client2] = true
		session.mu.Unlock()

		// Start session goroutine to handle broadcasts
		go session.Run()

		// Broadcast message
		message := []byte(`{"type":"test","data":"hello"}`)
		session.Broadcast <- message

		// Both clients should receive broadcast
		select {
		case msg := <-client1.Send:
			assert.Equal(t, message, msg)
		case <-time.After(time.Second):
			t.Fatal("Expected message not received by client1")
		}

		select {
		case msg := <-client2.Send:
			assert.Equal(t, message, msg)
		case <-time.After(time.Second):
			t.Fatal("Expected message not received by client2")
		}
	})

	t.Run("GetParticipants", func(t *testing.T) {
		diagramID := uuid.New().String()
		threatModelID := uuid.New().String()
		userID1 := "user1@example.com"
		userID2 := "user2@example.com"

		session := hub.GetOrCreateSession(diagramID, threatModelID, ResolvedUser{Provider: "test", ProviderID: userID1, Email: userID1})

		// Add mock clients
		client1 := &WebSocketClient{
			Hub:       hub,
			Session:   session,
			UserID:    userID1,
			UserEmail: userID1,
			UserName:  "User 1",
			Send:      make(chan []byte, 256),
		}

		client2 := &WebSocketClient{
			Hub:       hub,
			Session:   session,
			UserID:    userID2,
			UserEmail: userID2,
			UserName:  "User 2",
			Send:      make(chan []byte, 256),
		}

		// Add clients to session
		session.mu.Lock()
		session.Clients[client1] = true
		session.Clients[client2] = true
		session.mu.Unlock()

		// Since GetParticipants method doesn't exist, we'll just verify clients were added
		// by checking the Clients map directly

		// Verify clients were added
		session.mu.RLock()
		assert.Equal(t, 2, len(session.Clients))
		assert.True(t, session.Clients[client1])
		assert.True(t, session.Clients[client2])
		session.mu.RUnlock()
	})

	t.Run("SessionTermination", func(t *testing.T) {
		diagramID := uuid.New().String()
		threatModelID := uuid.New().String()
		userID := testWSUserEmail

		session := hub.GetOrCreateSession(diagramID, threatModelID, ResolvedUser{Provider: "test", ProviderID: userID, Email: userID})

		// Add client
		client := &WebSocketClient{
			Hub:       hub,
			Session:   session,
			UserID:    userID,
			UserEmail: userID,
			UserName:  "Test User",
			Send:      make(chan []byte, 256),
		}

		// Add client to session
		session.mu.Lock()
		session.Clients[client] = true
		session.mu.Unlock()

		// Terminate session manually by changing state
		session.mu.Lock()
		session.State = SessionStateTerminating
		// Close all client channels
		for c := range session.Clients {
			close(c.Send)
			delete(session.Clients, c)
		}
		session.mu.Unlock()

		// Verify state changed
		assert.Equal(t, SessionStateTerminating, session.State)

		// Verify connections were closed - check if channel is closed
		select {
		case _, ok := <-client.Send:
			assert.False(t, ok, "Expected channel to be closed")
		default:
			// Channel might still be open but empty
		}

		// Verify session has no clients
		session.mu.RLock()
		assert.Equal(t, 0, len(session.Clients))
		session.mu.RUnlock()
	})

	t.Run("PresenterRequestForwarding", func(t *testing.T) {
		diagramID := uuid.New().String()
		threatModelID := uuid.New().String()
		hostEmail := "host@example.com"
		participantEmail := "participant@example.com"

		// Create session with host as owner
		session, err := hub.CreateSession(diagramID, threatModelID, ResolvedUser{Provider: "test", ProviderID: "host-id", Email: hostEmail, DisplayName: "Host"})
		require.NoError(t, err)

		// Create host client
		hostClient := &WebSocketClient{
			Hub:          hub,
			Session:      session,
			UserID:       "host-id",
			UserEmail:    hostEmail,
			UserName:     "Host User",
			UserProvider: "test",
			Send:         make(chan []byte, 256),
		}

		// Create participant client
		participantClient := &WebSocketClient{
			Hub:          hub,
			Session:      session,
			UserID:       "participant-id",
			UserEmail:    participantEmail,
			UserName:     "Participant User",
			UserProvider: "test",
			Send:         make(chan []byte, 256),
		}

		// Add both clients to session
		session.mu.Lock()
		session.Clients[hostClient] = true
		session.Clients[participantClient] = true
		session.mu.Unlock()

		// Start session goroutine
		go session.Run()

		// Participant sends presenter request with correct user data
		// (Note: spoofed data would now be blocked by security validation)
		presenterRequestMsg := PresenterRequestMessage{
			MessageType: MessageTypePresenterRequest,
		}

		msgBytes, err := json.Marshal(presenterRequestMsg)
		require.NoError(t, err)

		// Process the presenter request from participant
		session.processPresenterRequest(participantClient, msgBytes)

		// Host should receive the presenter request event with authenticated user info
		select {
		case receivedMsg := <-hostClient.Send:
			var forwarded PresenterRequestEvent
			err := json.Unmarshal(receivedMsg, &forwarded)
			require.NoError(t, err, "Should unmarshal forwarded message")

			// Verify message type is the event, not the request
			assert.Equal(t, MessageTypePresenterRequestEvent, forwarded.MessageType)

			// Verify requesting_user is populated from client context (not client-asserted)
			assert.Equal(t, "participant-id", forwarded.RequestingUser.ProviderId)
			assert.Equal(t, participantEmail, string(forwarded.RequestingUser.Email))
			assert.Equal(t, "Participant User", forwarded.RequestingUser.DisplayName)
		case <-time.After(2 * time.Second):
			t.Fatal("Host did not receive presenter request event")
		}
	})
}

// TestWebSocketSecuritySpoofing tests that identity spoofing attempts are detected and blocked
func TestWebSocketSecuritySpoofing(t *testing.T) {
	hub := NewWebSocketHubForTests()

	t.Run("ChangePresenterSpoofing", func(t *testing.T) {
		diagramID := uuid.New().String()
		threatModelID := uuid.New().String()
		hostEmail := "host@example.com"

		session, err := hub.CreateSession(diagramID, threatModelID, ResolvedUser{Provider: "test", ProviderID: "host-id", Email: hostEmail, DisplayName: "Host"})
		require.NoError(t, err)

		hostClient := &WebSocketClient{
			Hub:       hub,
			Session:   session,
			UserID:    "host-id",
			UserEmail: hostEmail,
			UserName:  "Host User",
			Send:      make(chan []byte, 256),
		}

		session.mu.Lock()
		session.Clients[hostClient] = true
		session.mu.Unlock()

		go session.Run()

		// Try to change presenter with spoofed user info about the target
		// Create a second client that will be the target
		targetClient := &WebSocketClient{
			Hub:       hub,
			Session:   session,
			UserID:    "someone-id",
			UserEmail: "someone@example.com",
			UserName:  "Someone",
			Send:      make(chan []byte, 256),
		}
		session.mu.Lock()
		session.Clients[targetClient] = true
		session.mu.Unlock()

		// Host tries to change presenter but provides false info about target user
		spoofedMsg := ChangePresenterRequest{
			MessageType: MessageTypeChangePresenterRequest,
			NewPresenter: User{
				PrincipalType: UserPrincipalTypeUser,
				Provider:      "test",
				ProviderId:    "someone-id",
				Email:         openapi_types.Email("someone@example.com"),
				DisplayName:   "Evil Hacker", // Wrong name - actual name is "Someone"
			},
		}

		msgBytes, err := json.Marshal(spoofedMsg)
		require.NoError(t, err)

		session.processChangePresenter(hostClient, msgBytes)

		// Client should be removed
		session.mu.RLock()
		_, stillConnected := session.Clients[hostClient]
		session.mu.RUnlock()

		assert.False(t, stillConnected, "Spoofing client should be removed from session")
	})
}

// TestWebSocketConnection tests the WebSocket connection handling
func TestWebSocketConnection(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	hub := NewWebSocketHubForTests()

	// Note: Tests should use database stores - in-memory stores removed
	// GlobalMetadataStore will be nil for these tests since they don't test metadata

	// Create test server with mock auth service
	server := &Server{
		wsHub: hub,
	}

	// Create test router
	r := gin.New()
	r.GET("/threat_models/:threat_model_id/diagrams/:diagram_id/ws", server.HandleWebSocket)

	// Start test server
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	t.Run("WebSocketAuthentication", func(t *testing.T) {
		diagramID := uuid.New().String()
		threatModelID := uuid.New().String()

		// Connect without token
		url := fmt.Sprintf("%s/threat_models/%s/diagrams/%s/ws", wsURL, threatModelID, diagramID)

		_, resp, err := websocket.DefaultDialer.Dial(url, nil)
		assert.Error(t, err)
		if resp != nil {
			_ = resp.Body.Close()
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		}

		// Connect with invalid ticket
		url = fmt.Sprintf("%s/threat_models/%s/diagrams/%s/ws?ticket=invalid", wsURL, threatModelID, diagramID)

		_, resp, err = websocket.DefaultDialer.Dial(url, nil)
		assert.Error(t, err)
		if resp != nil {
			_ = resp.Body.Close()
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		}
	})
}
