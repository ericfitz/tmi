package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// WebSocketHub maintains active connections and broadcasts messages
type WebSocketHub struct {
	// Registered connections by diagram ID
	Diagrams map[string]*DiagramSession
	// Mutex for thread safety
	mu sync.RWMutex
}

// DiagramSession represents a collaborative editing session
type DiagramSession struct {
	// Session ID
	ID string
	// Diagram ID
	DiagramID string
	// Connected clients
	Clients map[*WebSocketClient]bool
	// Inbound messages from clients
	Broadcast chan []byte
	// Register requests
	Register chan *WebSocketClient
	// Unregister requests
	Unregister chan *WebSocketClient
	// Last activity timestamp
	LastActivity time.Time
	// Mutex for thread safety
	mu sync.RWMutex
}

// WebSocketClient represents a connected client
type WebSocketClient struct {
	// Hub reference
	Hub *WebSocketHub
	// Diagram session reference
	Session *DiagramSession
	// The websocket connection
	Conn *websocket.Conn
	// User name (can be email, username, etc.)
	UserName string
	// Buffered channel of outbound messages
	Send chan []byte
}

// WebSocketMessage represents a message sent over WebSocket
type WebSocketMessage struct {
	// Type of message (update, join, leave)
	Event string `json:"event"`
	// User who sent the message
	UserID string `json:"user_id"`
	// Diagram operation
	Operation DiagramOperation `json:"operation,omitempty"`
	// Timestamp
	Timestamp time.Time `json:"timestamp"`
}

// Upgrader upgrades HTTP connections to WebSocket
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// More secure origin check
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")

		// Get dev mode flag from context or default to false
		isDev := false
		if ctx := r.Context(); ctx != nil {
			if val := ctx.Value("isDev"); val != nil {
				if devMode, ok := val.(bool); ok {
					isDev = devMode
				}
			}
		}

		// In development mode, accept all origins
		if isDev {
			return true
		}

		// If no origin header, assume it's same-origin request
		if origin == "" {
			return true
		}

		// Get allowed origins from context
		tlsSubjectName := ""
		if ctx := r.Context(); ctx != nil {
			if val := ctx.Value("tlsSubjectName"); val != nil {
				if name, ok := val.(string); ok {
					tlsSubjectName = name
				}
			}
		}

		// Basic default allowed origins
		allowedOrigins := []string{
			"http://localhost",
			"https://localhost",
			"http://127.0.0.1",
			"https://127.0.0.1",
		}

		// Add the configured subject name if available
		if tlsSubjectName != "" {
			allowedOrigins = append(allowedOrigins,
				"http://"+tlsSubjectName,
				"https://"+tlsSubjectName)
		}

		// Get the host from the request
		host := r.Host
		allowedOrigins = append(allowedOrigins,
			"http://"+host,
			"https://"+host)

		// Check if origin matches any allowed origins
		for _, allowed := range allowedOrigins {
			if strings.HasPrefix(origin, allowed) {
				return true
			}
		}

		log.Printf("Rejected WebSocket connection from origin: %s", origin)
		return false
	},
}

// NewWebSocketHub creates a new WebSocket hub
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		Diagrams: make(map[string]*DiagramSession),
	}
}

// buildWebSocketURL constructs the absolute WebSocket URL from request context
func (h *WebSocketHub) buildWebSocketURL(c *gin.Context, threatModelId openapi_types.UUID, diagramID string) string {
	// Get config information from the context
	tlsEnabled := false
	tlsSubjectName := ""
	serverPort := "8080"

	// Try to extract from request context
	if val, exists := c.Get("tlsEnabled"); exists {
		if enabled, ok := val.(bool); ok {
			tlsEnabled = enabled
		}
	}

	if val, exists := c.Get("tlsSubjectName"); exists {
		if name, ok := val.(string); ok {
			tlsSubjectName = name
		}
	}

	if val, exists := c.Get("serverPort"); exists {
		if port, ok := val.(string); ok {
			serverPort = port
		}
	}

	// Determine websocket protocol
	scheme := "ws"
	if tlsEnabled {
		scheme = "wss"
	}

	// Determine host
	host := c.Request.Host
	if tlsSubjectName != "" && tlsEnabled {
		// Use configured subject name if available
		host = tlsSubjectName
		// Add port if not the default HTTPS port
		if serverPort != "443" {
			host = fmt.Sprintf("%s:%s", host, serverPort)
		}
	}

	// Build WebSocket URL with the specific path
	return fmt.Sprintf("%s://%s/threat_models/%s/diagrams/%s/ws", scheme, host, threatModelId.String(), diagramID)
}

// GetOrCreateSession returns an existing session or creates a new one
func (h *WebSocketHub) GetOrCreateSession(diagramID string) *DiagramSession {
	h.mu.Lock()
	defer h.mu.Unlock()

	if session, ok := h.Diagrams[diagramID]; ok {
		session.LastActivity = time.Now().UTC()
		return session
	}

	session := &DiagramSession{
		ID:           uuid.New().String(),
		DiagramID:    diagramID,
		Clients:      make(map[*WebSocketClient]bool),
		Broadcast:    make(chan []byte),
		Register:     make(chan *WebSocketClient),
		Unregister:   make(chan *WebSocketClient),
		LastActivity: time.Now().UTC(),
	}

	h.Diagrams[diagramID] = session
	go session.Run()

	return session
}

// GetSession returns an existing session or nil if none exists
func (h *WebSocketHub) GetSession(diagramID string) *DiagramSession {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if session, ok := h.Diagrams[diagramID]; ok {
		return session
	}

	return nil
}

// GetActiveSessions returns all active collaboration sessions
func (h *WebSocketHub) GetActiveSessions() []CollaborationSession {
	h.mu.RLock()
	defer h.mu.RUnlock()

	sessions := make([]CollaborationSession, 0, len(h.Diagrams))
	for diagramID, session := range h.Diagrams {
		session.mu.RLock()
		// Convert diagram ID to UUID
		diagramUUID, err := uuid.Parse(diagramID)
		if err != nil {
			session.mu.RUnlock()
			continue
		}

		// Convert clients to participants
		participants := make([]struct {
			JoinedAt *time.Time `json:"joined_at,omitempty"`
			UserId   *string    `json:"user_id,omitempty"`
		}, 0, len(session.Clients))

		for client := range session.Clients {
			participants = append(participants, struct {
				JoinedAt *time.Time `json:"joined_at,omitempty"`
				UserId   *string    `json:"user_id,omitempty"`
			}{
				JoinedAt: &session.LastActivity,
				UserId:   &client.UserName,
			})
		}

		// Get threat model ID from diagram
		threatModelId := h.getThreatModelIdForDiagram(diagramID)

		// Convert session ID to UUID
		sessionUUID, err := uuid.Parse(session.ID)
		if err != nil {
			session.mu.RUnlock()
			continue
		}

		sessions = append(sessions, CollaborationSession{
			SessionId:     &sessionUUID,
			DiagramId:     diagramUUID,
			ThreatModelId: threatModelId,
			Participants:  participants,
			WebsocketUrl:  fmt.Sprintf("/threat_models/%s/diagrams/%s/ws", threatModelId.String(), diagramID),
		})
		session.mu.RUnlock()
	}

	return sessions
}

// GetActiveSessionsForUser returns all active collaboration sessions that the specified user has access to
func (h *WebSocketHub) GetActiveSessionsForUser(c *gin.Context, userName string) []CollaborationSession {
	h.mu.RLock()
	defer h.mu.RUnlock()

	sessions := make([]CollaborationSession, 0, len(h.Diagrams))
	for diagramID, session := range h.Diagrams {
		session.mu.RLock()
		// Convert diagram ID to UUID
		diagramUUID, err := uuid.Parse(diagramID)
		if err != nil {
			session.mu.RUnlock()
			continue
		}

		// Get threat model ID from diagram
		threatModelId := h.getThreatModelIdForDiagram(diagramID)
		if threatModelId == (openapi_types.UUID{}) {
			// If we can't find the threat model, skip this session
			session.mu.RUnlock()
			continue
		}

		// Check if user has access to this threat model
		if !h.userHasAccessToThreatModel(userName, threatModelId) {
			session.mu.RUnlock()
			continue
		}

		// Convert clients to participants - include sessions even with no clients
		participants := make([]struct {
			JoinedAt *time.Time `json:"joined_at,omitempty"`
			UserId   *string    `json:"user_id,omitempty"`
		}, 0, len(session.Clients))

		for client := range session.Clients {
			participants = append(participants, struct {
				JoinedAt *time.Time `json:"joined_at,omitempty"`
				UserId   *string    `json:"user_id,omitempty"`
			}{
				JoinedAt: &session.LastActivity,
				UserId:   &client.UserName,
			})
		}

		// Convert session ID to UUID
		sessionUUID, err := uuid.Parse(session.ID)
		if err != nil {
			session.mu.RUnlock()
			continue
		}

		sessions = append(sessions, CollaborationSession{
			SessionId:     &sessionUUID,
			DiagramId:     diagramUUID,
			ThreatModelId: threatModelId,
			Participants:  participants,
			WebsocketUrl:  h.buildWebSocketURL(c, threatModelId, diagramID),
		})
		session.mu.RUnlock()
	}

	return sessions
}

// userHasAccessToThreatModel checks if a user has any level of access to a threat model
func (h *WebSocketHub) userHasAccessToThreatModel(userName string, threatModelId openapi_types.UUID) bool {
	// Safety check: if ThreatModelStore is not initialized (e.g., in tests), return false
	if ThreatModelStore == nil {
		return false
	}

	// Get the threat model
	tm, err := ThreatModelStore.Get(threatModelId.String())
	if err != nil {
		return false
	}

	// Check if user has any access to the threat model (reader, writer, or owner)
	hasAccess, err := CheckResourceAccess(userName, tm, RoleReader)
	if err != nil {
		return false
	}

	return hasAccess
}

// getThreatModelIdForDiagram finds the threat model that contains a specific diagram
func (h *WebSocketHub) getThreatModelIdForDiagram(diagramID string) openapi_types.UUID {
	// Safety check: if ThreatModelStore is not initialized (e.g., in tests), return empty UUID
	if ThreatModelStore == nil {
		return openapi_types.UUID{}
	}

	// Search through all threat models to find the one containing this diagram
	// Use a large limit to get all threat models (in practice we should have pagination)
	threatModels := ThreatModelStore.List(0, 1000, nil)

	for _, tm := range threatModels {
		if tm.Diagrams != nil {
			for _, diagramUnion := range *tm.Diagrams {
				// Convert union type to DfdDiagram to get the ID
				if dfdDiag, err := diagramUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil {
					if dfdDiag.Id.String() == diagramID {
						return *tm.Id
					}
				}
			}
		}
	}

	return openapi_types.UUID{}
}

// validateWebSocketDiagramAccess validates that a user has at least reader access to a diagram
// This is critical for WebSocket security to prevent unauthorized access to collaboration sessions
func (h *WebSocketHub) validateWebSocketDiagramAccess(userName string, diagramID string) bool {
	// Safety check: if ThreatModelStore is not initialized (e.g., in tests), deny access
	if ThreatModelStore == nil {
		return false
	}

	// Get the threat model that contains this diagram
	threatModelId := h.getThreatModelIdForDiagram(diagramID)
	if threatModelId == (openapi_types.UUID{}) {
		// If we can't find the parent threat model, deny access
		return false
	}

	// Get the threat model to check permissions
	tm, err := ThreatModelStore.Get(threatModelId.String())
	if err != nil {
		// If we can't get the threat model, deny access
		return false
	}

	// Check if user has at least reader access to the threat model (and thus the diagram)
	// Users need reader access minimum to participate in collaboration
	hasAccess, err := CheckResourceAccess(userName, tm, RoleReader)
	if err != nil {
		// If there's an error checking access, deny access
		return false
	}

	return hasAccess
}

// CloseSession closes a session and removes it
func (h *WebSocketHub) CloseSession(diagramID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if session, ok := h.Diagrams[diagramID]; ok {
		// Notify clients of close
		for client := range session.Clients {
			close(client.Send)
		}
		delete(h.Diagrams, diagramID)
	}
}

// CleanupInactiveSessions removes sessions inactive for 15+ minutes
func (h *WebSocketHub) CleanupInactiveSessions() {
	h.mu.Lock()
	defer h.mu.Unlock()

	timeout := time.Now().UTC().Add(-15 * time.Minute)
	for diagramID, session := range h.Diagrams {
		session.mu.RLock()
		lastActivity := session.LastActivity
		clientCount := len(session.Clients)
		session.mu.RUnlock()

		if lastActivity.Before(timeout) || clientCount == 0 {
			// Close session
			for client := range session.Clients {
				close(client.Send)
			}
			delete(h.Diagrams, diagramID)
		}
	}
}

// StartCleanupTimer starts a periodic cleanup timer
func (h *WebSocketHub) StartCleanupTimer(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.CleanupInactiveSessions()
		case <-ctx.Done():
			return
		}
	}
}

// Run processes messages for a diagram session
func (s *DiagramSession) Run() {
	for {
		select {
		case client := <-s.Register:
			s.mu.Lock()
			s.Clients[client] = true
			s.LastActivity = time.Now().UTC()
			s.mu.Unlock()

			// Notify other clients that someone joined
			msg := WebSocketMessage{
				Event:     "join",
				UserID:    client.UserName,
				Timestamp: time.Now().UTC(),
			}
			if msgBytes, err := json.Marshal(msg); err == nil {
				s.Broadcast <- msgBytes
			}

		case client := <-s.Unregister:
			s.mu.Lock()
			if _, ok := s.Clients[client]; ok {
				delete(s.Clients, client)
				close(client.Send)
				s.LastActivity = time.Now().UTC()
			}
			s.mu.Unlock()

			// Notify other clients that someone left
			msg := WebSocketMessage{
				Event:     "leave",
				UserID:    client.UserName,
				Timestamp: time.Now().UTC(),
			}
			if msgBytes, err := json.Marshal(msg); err == nil {
				s.Broadcast <- msgBytes
			}

		case message := <-s.Broadcast:
			s.mu.Lock()
			s.LastActivity = time.Now().UTC()
			// Send to all clients
			for client := range s.Clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(s.Clients, client)
				}
			}
			s.mu.Unlock()
		}
	}
}

// HandleWS handles WebSocket connections
func (h *WebSocketHub) HandleWS(c *gin.Context) {
	// Get diagram ID from path
	diagramID := c.Param("diagram_id")

	// Validate diagram ID format
	if _, err := uuid.Parse(diagramID); err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_id",
			ErrorDescription: "Invalid diagram ID format, must be a valid UUID",
		})
		return
	}

	// Get user from context
	userName, exists := c.Get("user_name")
	if !exists {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated",
		})
		return
	}

	userNameStr, ok := userName.(string)
	if !ok || userNameStr == "" {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "Invalid user authentication",
		})
		return
	}

	// CRITICAL: Validate user has access to the diagram before allowing WebSocket connection
	if !h.validateWebSocketDiagramAccess(userNameStr, diagramID) {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "You don't have sufficient permissions to collaborate on this diagram",
		})
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	// Get or create session
	session := h.GetOrCreateSession(diagramID)

	// Create client
	client := &WebSocketClient{
		Hub:      h,
		Session:  session,
		Conn:     conn,
		UserName: userNameStr,
		Send:     make(chan []byte, 256),
	}

	// Register client
	session.Register <- client

	// Start goroutines
	go client.ReadPump()
	go client.WritePump()
}

// DiagramOperation defines a change to a diagram
type DiagramOperation struct {
	// Operation type (add, remove, update)
	Type string `json:"type"`
	// Component ID (for update/remove)
	ComponentID string `json:"component_id,omitempty"`
	// Component data (for add/update)
	Component *Cell `json:"component,omitempty"`
	// Properties to update (for update)
	Properties map[string]interface{} `json:"properties,omitempty"`
}

// Validate the diagram operation
func validateDiagramOperation(op DiagramOperation) error {
	// Validate operation type
	if op.Type != "add" && op.Type != "update" && op.Type != "remove" {
		return fmt.Errorf("invalid operation type: %s", op.Type)
	}

	// Validate operation parameters based on type
	switch op.Type {
	case "add":
		// Add requires a component
		if op.Component == nil {
			return fmt.Errorf("add operation requires component data")
		}

		// Validate component data
		if err := validateCell(op.Component); err != nil {
			return fmt.Errorf("invalid component data: %w", err)
		}

	case "remove":
		// Remove requires component ID
		if op.ComponentID == "" {
			return fmt.Errorf("remove operation requires component_id")
		}

		// Validate component ID
		if _, err := uuid.Parse(op.ComponentID); err != nil {
			return fmt.Errorf("invalid component ID format: %w", err)
		}

	case "update":
		// Update requires component ID and either component or properties
		if op.ComponentID == "" {
			return fmt.Errorf("update operation requires component_id")
		}

		// Validate component ID
		if _, err := uuid.Parse(op.ComponentID); err != nil {
			return fmt.Errorf("invalid component ID format: %w", err)
		}

		// Properties or component required
		if op.Properties == nil && op.Component == nil {
			return fmt.Errorf("update operation requires either properties or component")
		}

		// If component provided, validate it
		if op.Component != nil {
			if err := validateCell(op.Component); err != nil {
				return fmt.Errorf("invalid component data: %w", err)
			}
		}

		// If properties provided, validate them
		if op.Properties != nil {
			for key := range op.Properties {
				if len(key) > 255 {
					return fmt.Errorf("property key exceeds maximum length: %s", key)
				}
			}
		}
	}

	return nil
}

// Validate cell
func validateCell(cell *Cell) error {
	if cell == nil {
		return fmt.Errorf("cell cannot be nil")
	}

	// Validate ID is not empty (check for zero UUID)
	if cell.Id.String() == "00000000-0000-0000-0000-000000000000" {
		return fmt.Errorf("cell ID is required")
	}

	return nil
}

// ReadPump pumps messages from WebSocket to hub
func (c *WebSocketClient) ReadPump() {
	defer func() {
		c.Session.Unregister <- c
		if err := c.Conn.Close(); err != nil {
			log.Printf("Error closing connection: %v", err)
		}
	}()

	c.Conn.SetReadLimit(4096) // 4KB message limit
	if err := c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		log.Printf("Error setting read deadline: %v", err)
		return
	}
	c.Conn.SetPongHandler(func(string) error {
		if err := c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			log.Printf("Error setting read deadline in pong handler: %v", err)
		}
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		// Process message
		var clientMsg struct {
			Operation json.RawMessage `json:"operation"`
		}
		if err := json.Unmarshal(message, &clientMsg); err != nil {
			log.Printf("Error parsing WebSocket message: %v", err)
			continue
		}

		// Validate message size
		if len(clientMsg.Operation) > 1024*50 { // 50KB limit
			log.Printf("Operation too large (%d bytes), ignoring", len(clientMsg.Operation))
			continue
		}

		// Create server message
		msg := WebSocketMessage{
			Event:     "update",
			UserID:    c.UserName,
			Timestamp: time.Now().UTC(),
		}

		// Unmarshal operation
		var op DiagramOperation
		if err := json.Unmarshal(clientMsg.Operation, &op); err != nil {
			log.Printf("Error parsing operation: %v", err)
			continue
		}

		// Validate operation
		if err := validateDiagramOperation(op); err != nil {
			log.Printf("Invalid diagram operation: %v", err)
			continue
		}

		msg.Operation = op

		// Apply operation to the diagram
		if err := applyDiagramOperation(c.Session.DiagramID, op); err != nil {
			log.Printf("Error applying operation to diagram: %v", err)
			// Still broadcast the operation to maintain consistency
		}

		// Marshal and broadcast
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			log.Printf("Error marshaling message: %v", err)
			continue
		}

		c.Session.Broadcast <- msgBytes
	}
}

// applyDiagramOperation applies a diagram operation to the stored diagram
func applyDiagramOperation(diagramID string, op DiagramOperation) error {
	// Get the diagram from the store
	diagram, err := DiagramStore.Get(diagramID)
	if err != nil {
		return err
	}

	// Ensure cells array exists (no longer need to check since Cells is not a pointer)
	if diagram.Cells == nil {
		diagram.Cells = []DfdDiagram_Cells_Item{}
	}

	// Update diagram based on operation type
	switch op.Type {
	case "add":
		// Add a new component
		if op.Component != nil {
			// Convert Cell to DfdDiagram_Cells_Item using conversion utility
			converter := NewCellConverter()
			cellItem, err := converter.ConvertCellToUnionItem(*op.Component)
			if err != nil {
				return fmt.Errorf("failed to convert cell: %w", err)
			}
			diagram.Cells = append(diagram.Cells, cellItem)
		}
	case "update":
		// Update an existing cell
		converter := NewCellConverter()
		for i := range diagram.Cells {
			cellItem := &diagram.Cells[i]
			// Convert to Cell to check ID
			if cell, err := converter.ConvertUnionItemToCell(*cellItem); err == nil {
				if cell.Id.String() == op.ComponentID {
					// Update existing cell with new properties from op.Component
					if op.Component != nil {
						// Replace the entire cell with the updated version
						if updatedCellItem, err := converter.ConvertCellToUnionItem(*op.Component); err == nil {
							diagram.Cells[i] = updatedCellItem
						}
					}
					break
				}
			}
		}
	case "remove":
		// Remove a cell
		converter := NewCellConverter()
		for i, cellItem := range diagram.Cells {
			// Convert to Cell to check ID
			if cell, err := converter.ConvertUnionItemToCell(cellItem); err == nil {
				if cell.Id.String() == op.ComponentID {
					// Remove by replacing with last element and truncating
					lastIndex := len(diagram.Cells) - 1
					if i != lastIndex {
						diagram.Cells[i] = diagram.Cells[lastIndex]
					}
					diagram.Cells = diagram.Cells[:lastIndex]
					break
				}
			}
		}
	}

	// Update modification time
	diagram.ModifiedAt = time.Now().UTC()

	// Save changes
	return DiagramStore.Update(diagramID, diagram)
}

// WritePump pumps messages from hub to WebSocket
func (c *WebSocketClient) WritePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		if err := c.Conn.Close(); err != nil {
			log.Printf("Error closing connection: %v", err)
		}
	}()

	for {
		select {
		case message, ok := <-c.Send:
			if err := c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				log.Printf("Error setting write deadline: %v", err)
				return
			}
			if !ok {
				// Hub closed the channel
				if err := c.Conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
					log.Printf("Error writing close message: %v", err)
				}
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if _, err := w.Write(message); err != nil {
				log.Printf("Error writing message: %v", err)
				return
			}

			// Add queued messages
			n := len(c.Send)
			for i := 0; i < n; i++ {
				if _, err := w.Write([]byte{'\n'}); err != nil {
					log.Printf("Error writing newline: %v", err)
					return
				}
				if _, err := w.Write(<-c.Send); err != nil {
					log.Printf("Error writing queued message: %v", err)
					return
				}
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				log.Printf("Error setting write deadline for ping: %v", err)
				return
			}
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
