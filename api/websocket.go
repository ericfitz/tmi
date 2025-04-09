package api

import (
	"context"
	"encoding/json"
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
	// User email
	UserEmail string
	// Buffered channel of outbound messages
	Send chan []byte
}

// WebSocketMessage represents a message sent over WebSocket
type WebSocketMessage struct {
	// Type of message (update, join, leave)
	Event string `json:"event"`
	// User who sent the message
	UserID string `json:"user_id"`
	// JSON Patch operation
	Operation interface{} `json:"operation,omitempty"`
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
				UserID:    client.UserEmail,
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
				UserID:    client.UserEmail,
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
	userEmail, exists := c.Get("user_email")
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
		UserEmail: userEmail.(string),
		Send:      make(chan []byte, 256),
	}

	// Register client
	session.Register <- client

	// Start goroutines
	go client.ReadPump()
	go client.WritePump()
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

		// Create server message
		msg := WebSocketMessage{
			Event:     "update",
			UserID:    c.UserEmail,
			Timestamp: time.Now().UTC(),
		}

		// Unmarshal operation
		var op interface{}
		if err := json.Unmarshal(clientMsg.Operation, &op); err != nil {
			log.Printf("Error parsing operation: %v", err)
			continue
		}
		msg.Operation = op

		// Marshal and broadcast
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			log.Printf("Error marshaling message: %v", err)
			continue
		}

		c.Session.Broadcast <- msgBytes
	}
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