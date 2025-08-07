package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleCollaborationSessions(t *testing.T) {
	tests := []struct {
		name           string
		setupSessions  func(*WebSocketHub)
		expectedStatus int
		expectedCount  int
	}{
		{
			name:           "no active sessions",
			setupSessions:  func(hub *WebSocketHub) {},
			expectedStatus: http.StatusOK,
			expectedCount:  0,
		},
		{
			name: "single active session with clients",
			setupSessions: func(hub *WebSocketHub) {
				// Create a diagram session
				diagramID := uuid.New().String()
				session := &DiagramSession{
					ID:           uuid.New().String(),
					DiagramID:    diagramID,
					Clients:      make(map[*WebSocketClient]bool),
					Broadcast:    make(chan []byte),
					Register:     make(chan *WebSocketClient),
					Unregister:   make(chan *WebSocketClient),
					LastActivity: time.Now().UTC(),
				}
				// Add mock clients
				client1 := &WebSocketClient{
					UserName: "alice@example.com",
				}
				client2 := &WebSocketClient{
					UserName: "bob@example.com",
				}
				session.Clients[client1] = true
				session.Clients[client2] = true
				hub.Diagrams[diagramID] = session
			},
			expectedStatus: http.StatusOK,
			expectedCount:  1,
		},
		{
			name: "multiple active sessions",
			setupSessions: func(hub *WebSocketHub) {
				// Create first session
				diagramID1 := uuid.New().String()
				session1 := &DiagramSession{
					ID:           uuid.New().String(),
					DiagramID:    diagramID1,
					Clients:      make(map[*WebSocketClient]bool),
					Broadcast:    make(chan []byte),
					Register:     make(chan *WebSocketClient),
					Unregister:   make(chan *WebSocketClient),
					LastActivity: time.Now().UTC(),
				}
				client1 := &WebSocketClient{UserName: "user1@example.com"}
				session1.Clients[client1] = true
				hub.Diagrams[diagramID1] = session1

				// Create second session
				diagramID2 := uuid.New().String()
				session2 := &DiagramSession{
					ID:           uuid.New().String(),
					DiagramID:    diagramID2,
					Clients:      make(map[*WebSocketClient]bool),
					Broadcast:    make(chan []byte),
					Register:     make(chan *WebSocketClient),
					Unregister:   make(chan *WebSocketClient),
					LastActivity: time.Now().UTC(),
				}
				client2 := &WebSocketClient{UserName: "user2@example.com"}
				session2.Clients[client2] = true
				hub.Diagrams[diagramID2] = session2
			},
			expectedStatus: http.StatusOK,
			expectedCount:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			gin.SetMode(gin.TestMode)
			router := gin.New()

			// Create server with fresh WebSocket hub
			server := &Server{
				wsHub: NewWebSocketHub(),
			}

			// Setup test sessions
			tt.setupSessions(server.wsHub)

			// Register route
			router.GET("/collaboration/sessions", server.HandleCollaborationSessions)

			// Create request
			req, err := http.NewRequest("GET", "/collaboration/sessions", nil)
			require.NoError(t, err)

			// Create response recorder
			w := httptest.NewRecorder()

			// Execute request
			router.ServeHTTP(w, req)

			// Assertions
			assert.Equal(t, tt.expectedStatus, w.Code)

			// Parse response
			var sessions []CollaborationSession
			err = json.Unmarshal(w.Body.Bytes(), &sessions)
			require.NoError(t, err)

			// Check session count
			assert.Len(t, sessions, tt.expectedCount)

			// If we expect sessions, validate their structure
			if tt.expectedCount > 0 {
				for _, session := range sessions {
					// Validate diagram ID is a valid UUID
					_, err := uuid.Parse(session.DiagramId.String())
					assert.NoError(t, err, "diagram_id should be a valid UUID")

					// Validate participants exist
					assert.NotNil(t, session.Participants)

					// Check that participants have user IDs
					for _, participant := range session.Participants {
						assert.NotNil(t, participant.UserId, "participant should have user_id")
						assert.NotEmpty(t, *participant.UserId, "participant user_id should not be empty")
					}
				}
			}
		})
	}
}

func TestWebSocketHub_GetActiveSessions(t *testing.T) {
	// Test the hub method directly
	hub := NewWebSocketHub()

	// Test empty hub
	sessions := hub.GetActiveSessions()
	assert.Len(t, sessions, 0)

	// Add a session with invalid diagram ID (should be skipped)
	invalidSession := &DiagramSession{
		ID:           uuid.New().String(),
		DiagramID:    "invalid-uuid",
		Clients:      make(map[*WebSocketClient]bool),
		LastActivity: time.Now().UTC(),
	}
	hub.Diagrams["invalid-uuid"] = invalidSession

	sessions = hub.GetActiveSessions()
	assert.Len(t, sessions, 0, "sessions with invalid UUIDs should be skipped")

	// Add valid session
	validDiagramID := uuid.New().String()
	validSession := &DiagramSession{
		ID:           uuid.New().String(),
		DiagramID:    validDiagramID,
		Clients:      make(map[*WebSocketClient]bool),
		LastActivity: time.Now().UTC(),
	}
	client := &WebSocketClient{UserName: "test@example.com"}
	validSession.Clients[client] = true
	hub.Diagrams[validDiagramID] = validSession

	sessions = hub.GetActiveSessions()
	assert.Len(t, sessions, 1)

	session := sessions[0]
	assert.Equal(t, validDiagramID, session.DiagramId.String())
	assert.Len(t, session.Participants, 1)
	assert.Equal(t, "test@example.com", *session.Participants[0].UserId)
}
