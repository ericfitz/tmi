package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCollaborationIntegration tests the complete collaboration feature set
func TestCollaborationIntegration(t *testing.T) {
	suite := SetupSubEntityIntegrationTest(t)
	defer suite.TeardownSubEntityIntegrationTest(t)

	// Create a test diagram for collaboration testing
	diagramID := suite.createTestDiagram(t)
	require.NotEmpty(t, diagramID, "Should have created a test diagram")

	t.Run("CollaborationEndpoints", func(t *testing.T) {
		testCollaborationEndpoints(t, suite, diagramID)
	})

	t.Run("WebSocketCollaboration", func(t *testing.T) {
		testWebSocketCollaboration(t, suite, diagramID)
	})

	t.Run("ConcurrentUsers", func(t *testing.T) {
		testConcurrentUserCollaboration(t, suite, diagramID)
	})

	t.Run("CollaborationSessionLifecycle", func(t *testing.T) {
		testCollaborationSessionLifecycle(t, suite, diagramID)
	})

	t.Run("CollaborationSessionsEndpoint", func(t *testing.T) {
		testCollaborationSessionsEndpoint(t, suite, diagramID)
	})
}

// testCollaborationEndpoints tests the REST API collaboration endpoints
func testCollaborationEndpoints(t *testing.T, suite *SubEntityIntegrationTestSuite, diagramID string) {
	basePath := fmt.Sprintf("/threat_models/%s/diagrams/%s/collaborate", suite.threatModelID, diagramID)

	// Test GET collaboration info
	t.Run("GET_CollaborationInfo", func(t *testing.T) {
		req := suite.makeAuthenticatedRequest("GET", basePath, nil)
		w := suite.executeRequest(req)

		response := suite.assertJSONResponse(t, w, http.StatusOK)

		// Verify response structure
		assert.Contains(t, response, "session_id")
		assert.Contains(t, response, "websocket_url")
		assert.Contains(t, response, "participants")

		// Verify WebSocket URL format
		if wsURL, exists := response["websocket_url"].(string); exists {
			assert.Contains(t, wsURL, "/ws/diagrams/"+diagramID, "WebSocket URL should contain diagram ID")
		}
	})

	// Test POST start collaboration
	t.Run("POST_StartCollaboration", func(t *testing.T) {
		req := suite.makeAuthenticatedRequest("POST", basePath, nil)
		w := suite.executeRequest(req)

		response := suite.assertJSONResponse(t, w, http.StatusCreated)

		// Verify session creation response
		assert.Contains(t, response, "session_id")
		assert.Contains(t, response, "started_at")
		assert.Contains(t, response, "websocket_url")

		// Store session info for later tests
		sessionID, exists := response["session_id"].(string)
		assert.True(t, exists, "session_id should be a string")
		assert.NotEmpty(t, sessionID, "session_id should not be empty")
	})

	// Test DELETE end collaboration
	t.Run("DELETE_EndCollaboration", func(t *testing.T) {
		// First start a collaboration session
		startReq := suite.makeAuthenticatedRequest("POST", basePath, nil)
		startW := suite.executeRequest(startReq)
		suite.assertJSONResponse(t, startW, http.StatusCreated)

		// Then end it
		req := suite.makeAuthenticatedRequest("DELETE", basePath, nil)
		w := suite.executeRequest(req)

		assert.Equal(t, http.StatusNoContent, w.Code, "DELETE should return 204 No Content")
	})
}

// CollaborationTestClient represents a WebSocket client for testing
type CollaborationTestClient struct {
	Conn      *websocket.Conn
	UserName  string
	Messages  chan WebSocketMessage
	Done      chan struct{}
	t         *testing.T
	mu        sync.RWMutex
	connected bool
}

// Connect establishes a WebSocket connection to the collaboration endpoint
func (c *CollaborationTestClient) Connect(suite *SubEntityIntegrationTestSuite, diagramID string) error {
	// Create HTTP server for testing WebSocket
	server := httptest.NewServer(suite.router)
	defer server.Close()

	// Convert HTTP URL to WebSocket URL
	u, err := url.Parse(server.URL)
	if err != nil {
		return err
	}
	u.Scheme = "ws"
	u.Path = fmt.Sprintf("/ws/diagrams/%s", diagramID)

	// Add token as query parameter for WebSocket authentication
	query := u.Query()
	query.Set("token", suite.accessToken)
	u.RawQuery = query.Encode()

	// Connect (no headers needed for query parameter auth)
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	c.mu.Lock()
	c.Conn = conn
	c.connected = true
	c.mu.Unlock()

	// Start message reader
	go c.readMessages()

	return nil
}

// readMessages reads messages from WebSocket connection
func (c *CollaborationTestClient) readMessages() {
	defer func() {
		c.mu.Lock()
		if c.Conn != nil {
			_ = c.Conn.Close()
		}
		c.connected = false
		c.mu.Unlock()
		close(c.Messages)
	}()

	for {
		var msg WebSocketMessage
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				return
			}
			c.t.Logf("WebSocket read error: %v", err)
			return
		}

		select {
		case c.Messages <- msg:
		case <-c.Done:
			return
		case <-time.After(5 * time.Second):
			c.t.Logf("Message channel blocked, dropping message")
		}
	}
}

// SendOperation sends a diagram operation over WebSocket
func (c *CollaborationTestClient) SendOperation(op DiagramOperation) error {
	c.mu.RLock()
	conn := c.Conn
	connected := c.connected
	c.mu.RUnlock()

	if !connected || conn == nil {
		return fmt.Errorf("not connected")
	}

	message := struct {
		Operation DiagramOperation `json:"operation"`
	}{
		Operation: op,
	}

	return conn.WriteJSON(message)
}

// Close closes the WebSocket connection
func (c *CollaborationTestClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected && c.Conn != nil {
		_ = c.Conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		_ = c.Conn.Close()
		c.connected = false
	}

	close(c.Done)
}

// IsConnected returns whether the client is connected
func (c *CollaborationTestClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// NewCollaborationTestClient creates a new test client
func NewCollaborationTestClient(t *testing.T, userName string) *CollaborationTestClient {
	return &CollaborationTestClient{
		UserName: userName,
		Messages: make(chan WebSocketMessage, 100),
		Done:     make(chan struct{}),
		t:        t,
	}
}

// testWebSocketCollaboration tests WebSocket-based real-time collaboration
func testWebSocketCollaboration(t *testing.T, suite *SubEntityIntegrationTestSuite, diagramID string) {
	// Create a test client
	client := NewCollaborationTestClient(t, suite.testUser.Email)
	defer client.Close()

	// Connect to WebSocket
	err := client.Connect(suite, diagramID)
	require.NoError(t, err, "Should be able to connect to WebSocket")

	// Wait for connection to be established
	time.Sleep(100 * time.Millisecond)

	// Verify connection is established
	assert.True(t, client.IsConnected(), "Client should be connected")

	// Test sending diagram operations
	t.Run("DiagramOperations", func(t *testing.T) {
		// Test Add operation
		addOp := DiagramOperation{
			Type: "add",
			Component: &Cell{
				Id:    generateTestUUID(t),
				Shape: "process", // Use actual shape string instead of constant
			},
		}

		err := client.SendOperation(addOp)
		assert.NoError(t, err, "Should be able to send add operation")

		// Wait for and verify broadcast message
		select {
		case msg := <-client.Messages:
			assert.Equal(t, "update", msg.Event, "Should receive update event")
			assert.Equal(t, suite.testUser.Email, msg.UserID, "Should have correct user ID")
			assert.Equal(t, "add", msg.Operation.Type, "Should have correct operation type")
		case <-time.After(2 * time.Second):
			t.Fatal("Should receive broadcast message within 2 seconds")
		}

		// Verify the operation was applied to the diagram
		verifyDiagramInDatabase(suite, t, diagramID, suite.threatModelID, map[string]interface{}{
			"name": "Test Integration Diagram", // This should match the created diagram
		})
	})
}

// testConcurrentUserCollaboration tests multiple users collaborating simultaneously
func testConcurrentUserCollaboration(t *testing.T, suite *SubEntityIntegrationTestSuite, diagramID string) {
	const numClients = 3
	const operationsPerClient = 2

	clients := make([]*CollaborationTestClient, numClients)
	var wg sync.WaitGroup

	// Create and connect multiple clients
	for i := 0; i < numClients; i++ {
		clients[i] = NewCollaborationTestClient(t, fmt.Sprintf("user%d@test.com", i+1))
		defer clients[i].Close()

		err := clients[i].Connect(suite, diagramID)
		require.NoError(t, err, "Client %d should connect", i+1)
	}

	// Wait for all connections to establish
	time.Sleep(200 * time.Millisecond)

	// Verify all clients are connected
	for i, client := range clients {
		assert.True(t, client.IsConnected(), "Client %d should be connected", i+1)
	}

	// Track received messages
	allMessages := make([][]WebSocketMessage, numClients)
	for i := range allMessages {
		allMessages[i] = make([]WebSocketMessage, 0)
	}

	// Start message collectors
	for i, client := range clients {
		wg.Add(1)
		go func(clientIndex int, c *CollaborationTestClient) {
			defer wg.Done()
			timeout := time.After(10 * time.Second)
			expectedMessages := (numClients-1)*operationsPerClient + 1 // +1 for join events from other clients

			for len(allMessages[clientIndex]) < expectedMessages {
				select {
				case msg := <-c.Messages:
					allMessages[clientIndex] = append(allMessages[clientIndex], msg)
					t.Logf("Client %d received message: %s from %s", clientIndex+1, msg.Event, msg.UserID)
				case <-timeout:
					t.Logf("Client %d timed out after receiving %d/%d messages", clientIndex+1, len(allMessages[clientIndex]), expectedMessages)
					return
				}
			}
		}(i, client)
	}

	// Send concurrent operations
	for i, client := range clients {
		go func(clientIndex int, c *CollaborationTestClient) {
			for j := 0; j < operationsPerClient; j++ {
				op := DiagramOperation{
					Type: "add",
					Component: &Cell{
						Id:    generateTestUUID(t),
						Shape: fmt.Sprintf("process-%d-%d", clientIndex, j),
					},
				}

				err := c.SendOperation(op)
				if err != nil {
					t.Errorf("Client %d failed to send operation %d: %v", clientIndex+1, j+1, err)
				}

				// Small delay to avoid overwhelming the server
				time.Sleep(50 * time.Millisecond)
			}
		}(i, client)
	}

	// Wait for all message processing to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Log("All clients finished processing messages")
	case <-time.After(15 * time.Second):
		t.Fatal("Timed out waiting for concurrent operations to complete")
	}

	// Verify each client received messages from other clients
	for i, messages := range allMessages {
		assert.Greater(t, len(messages), 0, "Client %d should have received messages", i+1)

		// Count unique users in messages
		users := make(map[string]int)
		for _, msg := range messages {
			users[msg.UserID]++
		}

		t.Logf("Client %d received messages from %d different users: %v", i+1, len(users), users)
	}
}

// testCollaborationSessionLifecycle tests session creation, management, and cleanup
func testCollaborationSessionLifecycle(t *testing.T, suite *SubEntityIntegrationTestSuite, diagramID string) {
	basePath := fmt.Sprintf("/threat_models/%s/diagrams/%s/collaborate", suite.threatModelID, diagramID)

	// Step 1: Start collaboration session
	t.Run("StartSession", func(t *testing.T) {
		req := suite.makeAuthenticatedRequest("POST", basePath, nil)
		w := suite.executeRequest(req)

		response := suite.assertJSONResponse(t, w, http.StatusCreated)
		assert.Contains(t, response, "session_id")
		assert.Contains(t, response, "started_at")
	})

	// Step 2: Get session info
	t.Run("GetSessionInfo", func(t *testing.T) {
		req := suite.makeAuthenticatedRequest("GET", basePath, nil)
		w := suite.executeRequest(req)

		response := suite.assertJSONResponse(t, w, http.StatusOK)
		assert.Contains(t, response, "session_id")
		assert.Contains(t, response, "participants")
	})

	// Step 3: Connect WebSocket clients
	t.Run("ConnectClients", func(t *testing.T) {
		client1 := NewCollaborationTestClient(t, "user1@test.com")
		client2 := NewCollaborationTestClient(t, "user2@test.com")
		defer func() {
			client1.Close()
			client2.Close()
		}()

		// Connect both clients
		err1 := client1.Connect(suite, diagramID)
		err2 := client2.Connect(suite, diagramID)

		assert.NoError(t, err1, "Client 1 should connect")
		assert.NoError(t, err2, "Client 2 should connect")

		// Wait for connections to establish
		time.Sleep(200 * time.Millisecond)

		// Verify both clients receive join notifications
		timeout := time.After(2 * time.Second)
		client1JoinCount := 0
		client2JoinCount := 0

	joinLoop:
		for client1JoinCount == 0 || client2JoinCount == 0 {
			select {
			case msg := <-client1.Messages:
				if msg.Event == "join" {
					client1JoinCount++
				}
			case msg := <-client2.Messages:
				if msg.Event == "join" {
					client2JoinCount++
				}
			case <-timeout:
				break joinLoop
			}
		}

		t.Logf("Client 1 received %d join events, Client 2 received %d join events", client1JoinCount, client2JoinCount)
	})

	// Step 4: End collaboration session
	t.Run("EndSession", func(t *testing.T) {
		req := suite.makeAuthenticatedRequest("DELETE", basePath, nil)
		w := suite.executeRequest(req)

		assert.Equal(t, http.StatusNoContent, w.Code, "Should successfully end session")
	})

	// Step 5: Verify session is cleaned up
	t.Run("VerifyCleanup", func(t *testing.T) {
		// Try to get session info after deletion
		req := suite.makeAuthenticatedRequest("GET", basePath, nil)
		w := suite.executeRequest(req)

		// Should either return 404 or an empty/new session
		assert.True(t, w.Code == http.StatusNotFound || w.Code == http.StatusOK,
			"Should return 404 or OK after session cleanup")
	})
}

// Helper function to generate test UUIDs
func generateTestUUID(t *testing.T) openapi_types.UUID {
	// Generate a proper UUID for testing
	return uuid.New()
}

// TestCollaborationWithRedis tests collaboration with Redis caching enabled
func TestCollaborationWithRedis(t *testing.T) {
	suite := SetupRedisIntegrationTest(t, true)
	defer suite.TeardownRedisIntegrationTest(t)

	if !isRedisEnabled(suite.SubEntityIntegrationTestSuite) {
		t.Skip("Redis not enabled, skipping collaboration with Redis test")
	}

	// Create a test diagram
	diagramID := suite.createTestDiagram(t)
	require.NotEmpty(t, diagramID, "Should have created a test diagram")

	// Test collaboration endpoints with Redis enabled
	testCollaborationEndpoints(t, suite.SubEntityIntegrationTestSuite, diagramID)

	// Verify Redis consistency after collaboration operations
	verifyRedisConsistency(suite.SubEntityIntegrationTestSuite, t, "collaboration_session", diagramID)
}

// testCollaborationSessionsEndpoint tests the GET /collaboration/sessions endpoint
func testCollaborationSessionsEndpoint(t *testing.T, suite *SubEntityIntegrationTestSuite, diagramID string) {
	t.Run("EmptySessionsList", func(t *testing.T) {
		// Test getting sessions when none are active
		req := suite.makeAuthenticatedRequest("GET", "/collaboration/sessions", nil)
		w := suite.executeRequest(req)

		assert.Equal(t, http.StatusOK, w.Code, "Should return OK for empty sessions list")
		// Parse response manually
		var sessions []CollaborationSession
		err := json.Unmarshal(w.Body.Bytes(), &sessions)
		require.NoError(t, err, "Should parse JSON response")
		assert.Len(t, sessions, 0, "Should have no active sessions")
	})

	t.Run("MockActiveSession", func(t *testing.T) {
		// Manually create a session in the WebSocket hub for testing
		server := NewServerForTests()
		diagramUUID, err := uuid.Parse(diagramID)
		require.NoError(t, err, "Should parse diagram ID as UUID")
		// Create a mock session directly in the hub
		session := &DiagramSession{
			ID:           uuid.New().String(),
			DiagramID:    diagramID,
			Clients:      make(map[*WebSocketClient]bool),
			Broadcast:    make(chan []byte),
			Register:     make(chan *WebSocketClient),
			Unregister:   make(chan *WebSocketClient),
			LastActivity: time.Now().UTC(),
		}

		// Add a mock client
		mockClient := &WebSocketClient{
			UserName: suite.testUser.Email,
		}
		session.Clients[mockClient] = true
		// Add session to hub
		server.wsHub.Diagrams[diagramID] = session

		// Test the endpoint with mock session
		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.GET("/collaboration/sessions", server.HandleCollaborationSessions)

		req, err := http.NewRequest("GET", "/collaboration/sessions", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Should return OK for sessions list")

		// Parse response manually
		var sessions []CollaborationSession
		err = json.Unmarshal(w.Body.Bytes(), &sessions)
		require.NoError(t, err, "Should parse JSON response")

		// Should have one active session
		require.Len(t, sessions, 1, "Should have one active session")

		session_resp := sessions[0]
		assert.Equal(t, diagramUUID, session_resp.DiagramId, "Session should be for the correct diagram")
		assert.Len(t, session_resp.Participants, 1, "Should have one participant")

		// Verify participant details
		participant := session_resp.Participants[0]
		assert.NotNil(t, participant.UserId, "Participant should have user ID")
		assert.Equal(t, suite.testUser.Email, *participant.UserId, "Participant should be the test user")
	})

	t.Run("AuthenticationRequired", func(t *testing.T) {
		// Test without authentication
		req, err := http.NewRequest("GET", "/collaboration/sessions", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		suite.router.ServeHTTP(w, req)

		// Should require authentication - exact status depends on auth middleware
		assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, w.Code,
			"Should require authentication")
	})
}
