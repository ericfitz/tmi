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
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	// Setup test data with different permission levels
	testUser := "test@example.com"

	// NOTE: Stores should be initialized via InitializeDatabaseStores() in production
	// Skip store initialization in tests - they should use database stores
	if ThreatModelStore == nil || DiagramStore == nil {
		t.Skip("Stores not initialized - run integration tests instead")
	}

	desc1 := "User has read access"
	desc2 := "User has reader access"
	desc3 := "User has no access"

	// Create threat models with different access levels
	threatModelWithAccess1 := ThreatModel{
		Name:        "Accessible Model 1",
		Description: &desc1,
		CreatedBy:   &testUser, // User created this one (should have owner access)
		Authorization: []Authorization{
			{Role: "owner", Subject: testUser},
		},
		CreatedAt:  func() *time.Time { t := time.Now().UTC(); return &t }(),
		ModifiedAt: func() *time.Time { t := time.Now().UTC(); return &t }(),
	}
	threatModelWithAccess2 := ThreatModel{
		Name:        "Accessible Model 2",
		Description: &desc2,
		CreatedBy:   stringPointer("other@example.com"),
		Authorization: []Authorization{
			{Role: "owner", Subject: "other@example.com"},
			{Role: "reader", Subject: testUser}, // User is reader
		},
		CreatedAt:  func() *time.Time { t := time.Now().UTC(); return &t }(),
		ModifiedAt: func() *time.Time { t := time.Now().UTC(); return &t }(),
	}
	threatModelWithoutAccess := ThreatModel{
		Name:        "Inaccessible Model",
		Description: &desc3,
		CreatedBy:   stringPointer("other@example.com"),
		Authorization: []Authorization{
			{Role: "owner", Subject: "other@example.com"},
			{Role: "reader", Subject: "someone@example.com"},
		},
		CreatedAt:  func() *time.Time { t := time.Now().UTC(); return &t }(),
		ModifiedAt: func() *time.Time { t := time.Now().UTC(); return &t }(),
	}

	// Create threat models in store
	tm1, _ := ThreatModelStore.Create(threatModelWithAccess1, func(tm ThreatModel, id string) ThreatModel {
		uuid, _ := ParseUUID(id)
		tm.Id = &uuid
		return tm
	})
	tm2, _ := ThreatModelStore.Create(threatModelWithAccess2, func(tm ThreatModel, id string) ThreatModel {
		uuid, _ := ParseUUID(id)
		tm.Id = &uuid
		return tm
	})
	tm3, _ := ThreatModelStore.Create(threatModelWithoutAccess, func(tm ThreatModel, id string) ThreatModel {
		uuid, _ := ParseUUID(id)
		tm.Id = &uuid
		return tm
	})

	// Create diagrams for each threat model
	now := time.Now().UTC()
	diagram1 := DfdDiagram{
		Name:       "Diagram 1",
		Type:       "dfd",
		CreatedAt:  &now,
		ModifiedAt: &now,
		Cells:      []DfdDiagram_Cells_Item{},
	}
	now2 := time.Now().UTC()
	diagram2 := DfdDiagram{
		Name:       "Diagram 2",
		Type:       "dfd",
		CreatedAt:  &now2,
		ModifiedAt: &now2,
		Cells:      []DfdDiagram_Cells_Item{},
	}
	now3 := time.Now().UTC()
	diagram3 := DfdDiagram{
		Name:       "Diagram 3",
		Type:       "dfd",
		CreatedAt:  &now3,
		ModifiedAt: &now3,
		Cells:      []DfdDiagram_Cells_Item{},
	}

	// Create diagrams in store
	d1, _ := DiagramStore.Create(diagram1, func(d DfdDiagram, id string) DfdDiagram {
		uuid, _ := ParseUUID(id)
		d.Id = &uuid
		return d
	})
	d2, _ := DiagramStore.Create(diagram2, func(d DfdDiagram, id string) DfdDiagram {
		uuid, _ := ParseUUID(id)
		d.Id = &uuid
		return d
	})
	d3, _ := DiagramStore.Create(diagram3, func(d DfdDiagram, id string) DfdDiagram {
		uuid, _ := ParseUUID(id)
		d.Id = &uuid
		return d
	})

	// Add diagrams to threat models
	var diagramUnion1, diagramUnion2, diagramUnion3 Diagram
	if err := diagramUnion1.FromDfdDiagram(d1); err != nil {
		t.Fatalf("Failed to convert diagram1: %v", err)
	}
	if err := diagramUnion2.FromDfdDiagram(d2); err != nil {
		t.Fatalf("Failed to convert diagram2: %v", err)
	}
	if err := diagramUnion3.FromDfdDiagram(d3); err != nil {
		t.Fatalf("Failed to convert diagram3: %v", err)
	}

	tm1.Diagrams = &[]Diagram{diagramUnion1}
	tm2.Diagrams = &[]Diagram{diagramUnion2}
	tm3.Diagrams = &[]Diagram{diagramUnion3}

	// Update threat models with diagrams
	if err := ThreatModelStore.Update(tm1.Id.String(), tm1); err != nil {
		t.Fatalf("Failed to update threat model 1: %v", err)
	}
	if err := ThreatModelStore.Update(tm2.Id.String(), tm2); err != nil {
		t.Fatalf("Failed to update threat model 2: %v", err)
	}
	if err := ThreatModelStore.Update(tm3.Id.String(), tm3); err != nil {
		t.Fatalf("Failed to update threat model 3: %v", err)
	}

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
			name: "sessions with proper authorization filtering",
			setupSessions: func(hub *WebSocketHub) {
				// Create session for accessible diagram 1 (user owns threat model)
				session1 := &DiagramSession{
					ID:           uuid.New().String(),
					DiagramID:    d1.Id.String(),
					Clients:      make(map[*WebSocketClient]bool),
					Broadcast:    make(chan []byte),
					Register:     make(chan *WebSocketClient),
					Unregister:   make(chan *WebSocketClient),
					LastActivity: time.Now().UTC(),
				}
				client1 := &WebSocketClient{UserName: "alice@example.com"}
				session1.Clients[client1] = true
				hub.Diagrams[d1.Id.String()] = session1

				// Create session for accessible diagram 2 (user is reader)
				session2 := &DiagramSession{
					ID:           uuid.New().String(),
					DiagramID:    d2.Id.String(),
					Clients:      make(map[*WebSocketClient]bool),
					Broadcast:    make(chan []byte),
					Register:     make(chan *WebSocketClient),
					Unregister:   make(chan *WebSocketClient),
					LastActivity: time.Now().UTC(),
				}
				client2 := &WebSocketClient{UserName: "bob@example.com"}
				session2.Clients[client2] = true
				hub.Diagrams[d2.Id.String()] = session2

				// Create session for inaccessible diagram (user has no access)
				session3 := &DiagramSession{
					ID:           uuid.New().String(),
					DiagramID:    d3.Id.String(),
					Clients:      make(map[*WebSocketClient]bool),
					Broadcast:    make(chan []byte),
					Register:     make(chan *WebSocketClient),
					Unregister:   make(chan *WebSocketClient),
					LastActivity: time.Now().UTC(),
				}
				client3 := &WebSocketClient{UserName: "charlie@example.com"}
				session3.Clients[client3] = true
				hub.Diagrams[d3.Id.String()] = session3
			},
			expectedStatus: http.StatusOK,
			expectedCount:  2, // Should only see 2 sessions (for accessible threat models)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			gin.SetMode(gin.TestMode)
			router := gin.New()

			// Add mock authentication middleware
			router.Use(func(c *gin.Context) {
				c.Set("userEmail", "test@example.com")
				c.Next()
			})

			// Create server with fresh WebSocket hub
			server := &Server{
				wsHub: NewWebSocketHubForTests(),
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
						assert.NotEmpty(t, participant.User.UserId, "participant should have user_id")
					}
				}
			}
		})
	}
}

func TestWebSocketHub_GetActiveSessions(t *testing.T) {
	// Test the hub method directly
	hub := NewWebSocketHubForTests()

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
	client := &WebSocketClient{
		UserID:   "test@example.com",
		UserName: "test@example.com",
	}
	validSession.Clients[client] = true
	hub.Diagrams[validDiagramID] = validSession

	sessions = hub.GetActiveSessions()
	assert.Len(t, sessions, 1)

	session := sessions[0]
	assert.Equal(t, validDiagramID, session.DiagramId.String())
	assert.Len(t, session.Participants, 1)
	assert.Equal(t, "test@example.com", session.Participants[0].User.UserId)
}
