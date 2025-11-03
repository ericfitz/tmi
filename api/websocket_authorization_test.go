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
)

func TestWebSocketAuthorizationValidation(t *testing.T) {
	// Initialize test fixtures
	InitTestFixtures()

	// Create test users
	ownerUser := "owner@example.com"
	readerUser := "reader@example.com"
	unauthorizedUser := "unauthorized@example.com"

	// Create a threat model with a diagram
	threatModel := ThreatModel{
		Name:        "Test Threat Model for WebSocket",
		Description: stringPointer("WebSocket authorization test"),
		Owner:       ownerUser,
		CreatedBy:   &ownerUser,
		Authorization: []Authorization{
			{Role: "owner", Subject: ownerUser},
			{Role: "reader", Subject: readerUser},
		},
		CreatedAt:  func() *time.Time { t := time.Now().UTC(); return &t }(),
		ModifiedAt: func() *time.Time { t := time.Now().UTC(); return &t }(),
	}

	// Add threat model to store
	tm, err := ThreatModelStore.Create(threatModel, func(tm ThreatModel, id string) ThreatModel {
		uuid, _ := ParseUUID(id)
		tm.Id = &uuid
		return tm
	})
	if err != nil {
		t.Fatalf("Failed to create threat model: %v", err)
	}

	// Create a diagram in the threat model
	now := time.Now().UTC()
	diagram := DfdDiagram{
		Name:       "Test Diagram",
		Type:       "dfd",
		CreatedAt:  &now,
		ModifiedAt: &now,
		Cells:      []DfdDiagram_Cells_Item{},
	}

	// Add diagram to store
	d, err := DiagramStore.Create(diagram, func(d DfdDiagram, id string) DfdDiagram {
		uuid, _ := ParseUUID(id)
		d.Id = &uuid
		return d
	})
	if err != nil {
		t.Fatalf("Failed to create diagram: %v", err)
	}

	// Add diagram to threat model
	var diagramUnion Diagram
	if err := diagramUnion.FromDfdDiagram(d); err != nil {
		t.Fatalf("Failed to convert diagram: %v", err)
	}
	tm.Diagrams = &[]Diagram{diagramUnion}
	if err := ThreatModelStore.Update(tm.Id.String(), tm); err != nil {
		t.Fatalf("Failed to update threat model with diagram: %v", err)
	}

	diagramID := d.Id.String()

	// Test the authorization validation method directly
	wsHub := NewWebSocketHubForTests()

	tests := []struct {
		name           string
		user           string
		expectedAccess bool
	}{
		{
			name:           "owner has access to diagram",
			user:           ownerUser,
			expectedAccess: true,
		},
		{
			name:           "reader has access to diagram",
			user:           readerUser,
			expectedAccess: true,
		},
		{
			name:           "unauthorized user has no access to diagram",
			user:           unauthorizedUser,
			expectedAccess: false,
		},
		{
			name:           "empty user has no access to diagram",
			user:           "",
			expectedAccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasAccess := wsHub.validateWebSocketDiagramAccess(tt.user, diagramID)
			assert.Equal(t, tt.expectedAccess, hasAccess, "Access check mismatch for user: %s", tt.user)
		})
	}
}

func TestWebSocketAuthorizationHTTPEndpoint(t *testing.T) {
	// Test the HTTP endpoint behavior without WebSocket upgrade
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Add authentication middleware that sets no user (unauthenticated)
	router.Use(func(c *gin.Context) {
		// Don't set user_name to simulate unauthenticated request
		c.Next()
	})

	// Create WebSocket hub and register route
	wsHub := NewWebSocketHubForTests()
	router.GET("/threat_models/:threat_model_id/diagrams/:diagram_id/ws", wsHub.HandleWS)

	// Test unauthenticated request
	threatModelID := uuid.New().String()
	diagramID := uuid.New().String()
	req, err := http.NewRequest("GET", "/threat_models/"+threatModelID+"/diagrams/"+diagramID+"/ws", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should return unauthorized
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var errorResponse Error
	err = json.Unmarshal(w.Body.Bytes(), &errorResponse)
	assert.NoError(t, err)
	assert.Equal(t, "unauthorized", errorResponse.Error)
	assert.Equal(t, "User not authenticated", errorResponse.ErrorDescription)
}

func TestWebSocketAuthorizationInvalidDiagramID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Add authentication middleware
	router.Use(func(c *gin.Context) {
		c.Set("user_name", "test@example.com")
		c.Next()
	})

	// Create WebSocket hub and register route
	wsHub := NewWebSocketHubForTests()
	router.GET("/threat_models/:threat_model_id/diagrams/:diagram_id/ws", wsHub.HandleWS)

	tests := []struct {
		name           string
		diagramID      string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "invalid UUID format",
			diagramID:      "invalid-uuid",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid_id",
		},
		{
			name:           "non-existent diagram",
			diagramID:      uuid.New().String(),
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "unauthorized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			threatModelID := uuid.New().String()
			req, err := http.NewRequest("GET", "/threat_models/"+threatModelID+"/diagrams/"+tt.diagramID+"/ws", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var errorResponse Error
			err = json.Unmarshal(w.Body.Bytes(), &errorResponse)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedError, errorResponse.Error)
		})
	}
}
