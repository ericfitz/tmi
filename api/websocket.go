package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
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
	// Allow all origins for development; restrict in production
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// NewWebSocketHub creates a new WebSocket hub
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		Diagrams: make(map[string]*DiagramSession),
	}
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
	diagramID := c.Param("id")

	// Get user from context
	userName, exists := c.Get("user_name")
	if !exists {
		c.JSON(http.StatusUnauthorized, Error{
			Error:   "unauthorized",
			Message: "User not authenticated",
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
		Hub:       h,
		Session:   session,
		Conn:      conn,
		UserName:  userName.(string),
		Send:      make(chan []byte, 256),
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
	Component *DiagramComponent `json:"component,omitempty"`
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
		if err := validateDiagramComponent(op.Component); err != nil {
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
			if err := validateDiagramComponent(op.Component); err != nil {
				return fmt.Errorf("invalid component data: %w", err)
			}
		}
		
		// If properties provided, validate them
		if op.Properties != nil {
			for key, _ := range op.Properties {
				if len(key) > 255 {
					return fmt.Errorf("property key exceeds maximum length: %s", key)
				}
			}
		}
	}
	
	return nil
}

// Validate diagram component
func validateDiagramComponent(component *DiagramComponent) error {
	if component == nil {
		return fmt.Errorf("component cannot be nil")
	}
	
	// Validate type is not empty
	if component.Type == "" {
		return fmt.Errorf("component type is required")
	}
	
	// Validate type length
	if len(component.Type) > 100 {
		return fmt.Errorf("component type exceeds maximum length")
	}
	
	// Validate data if present
	if component.Data != nil {
		// Check for reasonable size
		if len(component.Data) > 50 {
			return fmt.Errorf("component data has too many fields")
		}
	}
	
	return nil
}

// ReadPump pumps messages from WebSocket to hub
func (c *WebSocketClient) ReadPump() {
	defer func() {
		c.Session.Unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(4096) // 4KB message limit
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
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

	// Ensure components array exists
	if diagram.Components == nil {
		components := []DiagramComponent{}
		diagram.Components = &components
	}

	// Update diagram based on operation type
	switch op.Type {
	case "add":
		// Add a new component
		if op.Component != nil {
			*diagram.Components = append(*diagram.Components, *op.Component)
		}
	case "update":
		// Update an existing component
		for i := range *diagram.Components {
			comp := &(*diagram.Components)[i]
			if comp.Id.String() == op.ComponentID {
				// Update existing component with new properties
				if op.Properties != nil {
					for key, value := range op.Properties {
						if comp.Data == nil {
							comp.Data = make(map[string]interface{})
						}
						comp.Data[key] = value
					}
				}
				break
			}
		}
	case "remove":
		// Remove a component
		for i, comp := range *diagram.Components {
			if comp.Id.String() == op.ComponentID {
				// Remove by replacing with last element and truncating
				lastIndex := len(*diagram.Components) - 1
				if i != lastIndex {
					(*diagram.Components)[i] = (*diagram.Components)[lastIndex]
				}
				*diagram.Components = (*diagram.Components)[:lastIndex]
				break
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
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// Hub closed the channel
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages
			n := len(c.Send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.Send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}