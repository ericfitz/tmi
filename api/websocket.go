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
	// Threat Model ID (parent of the diagram)
	ThreatModelID string
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

	// Enhanced collaboration state
	// Session owner (user who created the session)
	Owner string
	// Current presenter (user whose cursor/selection is broadcast)
	CurrentPresenter string
	// Operation history for conflict resolution
	OperationHistory *OperationHistory
	// Next sequence number for operations
	NextSequenceNumber uint64
	// Recent corrections tracking for sync issue detection
	recentCorrections map[string]int
	// Client sequence tracking for out-of-order detection
	clientLastSequence map[string]uint64

	// Mutex for thread safety
	mu sync.RWMutex
}

// OperationHistory tracks mutations for conflict resolution and undo/redo
type OperationHistory struct {
	// Operations by sequence number
	Operations map[uint64]*HistoryEntry
	// Current diagram state snapshot for conflict detection
	CurrentState map[string]*Cell
	// Maximum history entries to keep
	MaxEntries int
	// Current position in history for undo/redo (points to last applied operation)
	CurrentPosition uint64
	// Mutex for thread safety
	mutex sync.RWMutex
}

// HistoryEntry represents a single operation in history
type HistoryEntry struct {
	SequenceNumber uint64
	OperationID    string
	UserID         string
	Timestamp      time.Time
	Operation      CellPatchOperation
	// State before this operation (for undo)
	PreviousState map[string]*Cell
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

// WebSocketMessage represents the legacy message format (kept for backward compatibility)
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

// Enhanced message types for collaborative editing - using existing AsyncAPI types

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
func (h *WebSocketHub) GetOrCreateSession(diagramID string, threatModelID string, ownerUserID string) *DiagramSession {
	h.mu.Lock()
	defer h.mu.Unlock()

	if session, ok := h.Diagrams[diagramID]; ok {
		session.LastActivity = time.Now().UTC()
		// Update threat model ID if it wasn't set (for backward compatibility)
		if session.ThreatModelID == "" && threatModelID != "" {
			session.ThreatModelID = threatModelID
		}
		// Set owner if not already set
		if session.Owner == "" && ownerUserID != "" {
			session.Owner = ownerUserID
			session.CurrentPresenter = ownerUserID // Owner starts as presenter
		}
		return session
	}

	session := &DiagramSession{
		ID:            uuid.New().String(),
		DiagramID:     diagramID,
		ThreatModelID: threatModelID,
		Clients:       make(map[*WebSocketClient]bool),
		Broadcast:     make(chan []byte),
		Register:      make(chan *WebSocketClient),
		Unregister:    make(chan *WebSocketClient),
		LastActivity:  time.Now().UTC(),

		// Enhanced collaboration state
		Owner:              ownerUserID,
		CurrentPresenter:   ownerUserID, // Owner starts as presenter
		NextSequenceNumber: 1,
		OperationHistory:   NewOperationHistory(),
		recentCorrections:  make(map[string]int),
		clientLastSequence: make(map[string]uint64),
	}

	h.Diagrams[diagramID] = session

	// Record session start
	if GlobalPerformanceMonitor != nil {
		GlobalPerformanceMonitor.RecordSessionStart(session.ID, diagramID)
	}

	go session.Run()

	return session
}

// NewOperationHistory creates a new operation history
func NewOperationHistory() *OperationHistory {
	return &OperationHistory{
		Operations:      make(map[uint64]*HistoryEntry),
		CurrentState:    make(map[string]*Cell),
		MaxEntries:      100, // Keep last 100 operations
		CurrentPosition: 0,   // No operations applied yet
	}
}

// CanUndo returns true if there are operations to undo
func (h *OperationHistory) CanUndo() bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.CurrentPosition > 0
}

// CanRedo returns true if there are operations to redo
func (h *OperationHistory) CanRedo() bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	// Check if there's a next operation after current position
	nextSeq := h.CurrentPosition + 1
	_, exists := h.Operations[nextSeq]
	return exists
}

// GetUndoOperation returns the operation to undo and the previous state
func (h *OperationHistory) GetUndoOperation() (*HistoryEntry, map[string]*Cell, bool) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	if h.CurrentPosition == 0 {
		return nil, nil, false
	}

	entry, exists := h.Operations[h.CurrentPosition]
	if !exists {
		return nil, nil, false
	}

	return entry, entry.PreviousState, true
}

// GetRedoOperation returns the operation to redo
func (h *OperationHistory) GetRedoOperation() (*HistoryEntry, bool) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	nextSeq := h.CurrentPosition + 1
	entry, exists := h.Operations[nextSeq]
	return entry, exists
}

// MoveToPosition updates the current position in history (for undo/redo)
func (h *OperationHistory) MoveToPosition(newPosition uint64) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.CurrentPosition = newPosition
}

// AddOperation adds a new operation to history and updates current position
func (h *OperationHistory) AddOperation(entry *HistoryEntry) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	// Add operation to history
	h.Operations[entry.SequenceNumber] = entry

	// Update current position to this operation
	h.CurrentPosition = entry.SequenceNumber

	// Update current state
	// Apply operation to current state (simplified implementation)
	for _, cellOp := range entry.Operation.Cells {
		switch cellOp.Operation {
		case "add", "update":
			if cellOp.Data != nil {
				h.CurrentState[cellOp.ID] = cellOp.Data
			}
		case "remove":
			delete(h.CurrentState, cellOp.ID)
		}
	}

	// Clean up old entries if needed
	if len(h.Operations) > h.MaxEntries {
		h.cleanupOldEntries()
	}
}

// cleanupOldEntries removes old entries to keep history size manageable
func (h *OperationHistory) cleanupOldEntries() {
	// Find the oldest entry to keep (current position - max entries / 2)
	keepFrom := uint64(0)
	if h.MaxEntries > 0 && h.MaxEntries <= 1000000 { // Reasonable upper bound
		halfMaxInt := h.MaxEntries / 2
		if halfMaxInt >= 0 {
			halfMax := uint64(halfMaxInt)
			if h.CurrentPosition > halfMax {
				keepFrom = h.CurrentPosition - halfMax
			}
		}
	}

	for seq := range h.Operations {
		if seq < keepFrom {
			delete(h.Operations, seq)
		}
	}
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

		// Skip sessions without threat model ID (shouldn't happen with new code)
		if session.ThreatModelID == "" {
			session.mu.RUnlock()
			continue
		}

		// Parse threat model ID
		threatModelId, err := uuid.Parse(session.ThreatModelID)
		if err != nil {
			session.mu.RUnlock()
			continue
		}

		// Get the threat model to check access and extract name
		tm, err := ThreatModelStore.Get(threatModelId.String())
		if err != nil {
			session.mu.RUnlock()
			continue
		}

		// Check if user has access to this threat model
		hasAccess, err := CheckResourceAccess(userName, tm, RoleReader)
		if err != nil || !hasAccess {
			session.mu.RUnlock()
			continue
		}

		// Find the diagram in the threat model to get its name
		var diagramName string
		if tm.Diagrams != nil {
			for _, diagramUnion := range *tm.Diagrams {
				if dfdDiag, err := diagramUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil {
					if dfdDiag.Id.String() == diagramID {
						diagramName = dfdDiag.Name
						break
					}
				}
			}
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
			SessionId:       &sessionUUID,
			DiagramId:       diagramUUID,
			DiagramName:     diagramName,
			ThreatModelId:   threatModelId,
			ThreatModelName: tm.Name,
			Participants:    participants,
			WebsocketUrl:    h.buildWebSocketURL(c, threatModelId, diagramID),
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
		log.Printf("[DEBUG] ThreatModelStore is nil, denying WebSocket access for diagram %s", diagramID)
		return openapi_types.UUID{}
	}

	// Search through all threat models to find the one containing this diagram
	// Use a large limit to get all threat models (in practice we should have pagination)
	threatModels := ThreatModelStore.List(0, 1000, nil)
	log.Printf("[DEBUG] Searching for diagram %s in %d threat models", diagramID, len(threatModels))

	for _, tm := range threatModels {
		if tm.Diagrams != nil {
			log.Printf("[DEBUG] Checking threat model %s with %d diagrams", tm.Id.String(), len(*tm.Diagrams))
			for _, diagramUnion := range *tm.Diagrams {
				// Convert union type to DfdDiagram to get the ID
				if dfdDiag, err := diagramUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil {
					log.Printf("[DEBUG] Found diagram %s in threat model %s", dfdDiag.Id.String(), tm.Id.String())
					if dfdDiag.Id.String() == diagramID {
						log.Printf("[DEBUG] Match found! Diagram %s belongs to threat model %s", diagramID, tm.Id.String())
						return *tm.Id
					}
				} else {
					log.Printf("[DEBUG] Failed to convert diagram union to DfdDiagram: %v", err)
				}
			}
		} else {
			log.Printf("[DEBUG] Threat model %s has nil Diagrams", tm.Id.String())
		}
	}

	log.Printf("[DEBUG] Diagram %s not found in any threat model", diagramID)
	return openapi_types.UUID{}
}

// validateWebSocketDiagramAccessDirect validates that a user has at least reader access to a diagram
// using the threat model ID directly from the URL path
// This is critical for WebSocket security to prevent unauthorized access to collaboration sessions
func (h *WebSocketHub) validateWebSocketDiagramAccessDirect(userName string, threatModelID string, diagramID string) bool {
	// Safety check: if ThreatModelStore is not initialized (e.g., in tests), deny access
	if ThreatModelStore == nil {
		return false
	}

	// Parse the threat model ID
	threatModelUUID, err := uuid.Parse(threatModelID)
	if err != nil {
		return false
	}

	// Get the threat model to check permissions
	tm, err := ThreatModelStore.Get(threatModelUUID.String())
	if err != nil {
		// If we can't get the threat model, deny access
		return false
	}

	// Check if the diagram actually exists in this threat model
	diagramExists := false
	if tm.Diagrams != nil {
		for _, diagramUnion := range *tm.Diagrams {
			if dfdDiag, err := diagramUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil {
				if dfdDiag.Id.String() == diagramID {
					diagramExists = true
					break
				}
			}
		}
	}

	if !diagramExists {
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

				// Check if the leaving client was the current presenter
				wasPresenter := client.UserName == s.CurrentPresenter

				s.mu.Unlock()

				// Handle presenter leaving session
				if wasPresenter {
					s.handlePresenterDisconnection(client.UserName)
				}
			} else {
				s.mu.Unlock()
			}

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
	// Get threat model ID and diagram ID from path
	threatModelID := c.Param("id")
	diagramID := c.Param("diagram_id")

	// Validate threat model ID format
	if _, err := uuid.Parse(threatModelID); err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_id",
			ErrorDescription: "Invalid threat model ID format, must be a valid UUID",
		})
		return
	}

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
	if !h.validateWebSocketDiagramAccessDirect(userNameStr, threatModelID, diagramID) {
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
	session := h.GetOrCreateSession(diagramID, threatModelID, userNameStr)

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

// ProcessMessage handles enhanced message types for collaborative editing
func (s *DiagramSession) ProcessMessage(client *WebSocketClient, message []byte) {
	// First try to parse as enhanced message format
	var baseMsg struct {
		MessageType string          `json:"message_type"`
		UserID      string          `json:"user_id"`
		Raw         json.RawMessage `json:"-"`
	}

	if err := json.Unmarshal(message, &baseMsg); err != nil {
		log.Printf("Error parsing message: %v", err)
		return
	}

	// Handle different message types
	switch baseMsg.MessageType {
	case "diagram_operation":
		s.processDiagramOperation(client, message)

	case "presenter_request":
		s.processPresenterRequest(client, message)

	case "change_presenter":
		s.processChangePresenter(client, message)

	case "presenter_denied":
		s.processPresenterDenied(client, message)

	case "presenter_cursor":
		s.processPresenterCursor(client, message)

	case "presenter_selection":
		s.processPresenterSelection(client, message)

	case "resync_request":
		s.processResyncRequest(client, message)

	case "undo_request":
		s.processUndoRequest(client, message)

	case "redo_request":
		s.processRedoRequest(client, message)

	default:
		// Fall back to legacy message format
		s.processLegacyMessage(client, message)
	}
}

// processDiagramOperation handles enhanced diagram operations
func (s *DiagramSession) processDiagramOperation(client *WebSocketClient, message []byte) {
	startTime := time.Now()

	var msg DiagramOperationMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Error parsing diagram operation: %v", err)
		return
	}

	// Record message metrics
	if GlobalPerformanceMonitor != nil {
		GlobalPerformanceMonitor.RecordMessage(s.ID, len(message), 0)
	}

	// Validate message
	if msg.UserID != client.UserName {
		log.Printf("User ID mismatch in diagram operation: %s != %s", msg.UserID, client.UserName)
		return
	}

	// Check authorization (this will be implemented in the authorization filtering task)
	if !s.checkMutationPermission(client.UserName) {
		s.sendAuthorizationDenied(client, msg.OperationID, "insufficient_permissions")

		// Send enhanced state correction for affected cells
		affectedCellIDs := extractCellIDs(msg.Operation.Cells)
		s.sendStateCorrectionWithReason(client, affectedCellIDs, "unauthorized_operation")
		return
	}

	// Check for out-of-order message delivery if client has a sequence number
	if msg.SequenceNumber != nil {
		s.mu.Lock()
		lastSeq, exists := s.clientLastSequence[client.UserName]
		expectedSeq := lastSeq + 1

		if exists && *msg.SequenceNumber != expectedSeq {
			if *msg.SequenceNumber < expectedSeq {
				log.Printf("Duplicate or old message from %s: expected %d, got %d",
					client.UserName, expectedSeq, *msg.SequenceNumber)
				s.trackPotentialSyncIssue(client.UserName, "duplicate_message")
			} else {
				log.Printf("Message gap detected from %s: expected %d, got %d (gap of %d)",
					client.UserName, expectedSeq, *msg.SequenceNumber, *msg.SequenceNumber-expectedSeq)
				s.trackPotentialSyncIssue(client.UserName, "message_gap")
			}
		}

		// Update client's last sequence number
		s.clientLastSequence[client.UserName] = *msg.SequenceNumber
		s.mu.Unlock()
	}

	// Assign sequence number
	s.mu.Lock()
	sequenceNumber := s.NextSequenceNumber
	s.NextSequenceNumber++
	s.mu.Unlock()

	msg.SequenceNumber = &sequenceNumber

	// Process operation using shared processor
	processor := NewCellOperationProcessor(DiagramStore)
	result, err := processor.ProcessCellOperations(s.DiagramID, msg.Operation)
	if err != nil {
		log.Printf("Failed to process cell operation: %v", err)
		return
	}

	if !result.Valid {
		log.Printf("Operation %s validation failed: %s", msg.OperationID, result.Reason)

		if result.CorrectionNeeded {
			s.sendStateCorrection(client, result.CellsModified)
		}
		return
	}

	if !result.StateChanged {
		log.Printf("Operation %s resulted in no state changes", msg.OperationID)

		// Record operation performance even for no-op operations
		if GlobalPerformanceMonitor != nil {
			perf := &OperationPerformance{
				OperationID:      msg.OperationID,
				UserID:           msg.UserID,
				StartTime:        startTime,
				TotalTime:        time.Since(startTime),
				CellCount:        len(msg.Operation.Cells),
				StateChanged:     false,
				ConflictDetected: false,
			}
			GlobalPerformanceMonitor.RecordOperation(perf)
		}
		return // Don't broadcast no-op operations
	}

	// Update operation history
	s.addToHistory(msg, result.PreviousState, result.PreviousState) // For now, use same state

	// Record operation performance
	if GlobalPerformanceMonitor != nil {
		perf := &OperationPerformance{
			OperationID:      msg.OperationID,
			UserID:           msg.UserID,
			StartTime:        startTime,
			TotalTime:        time.Since(startTime),
			CellCount:        len(msg.Operation.Cells),
			StateChanged:     result.StateChanged,
			ConflictDetected: !result.Valid,
		}
		GlobalPerformanceMonitor.RecordOperation(perf)
	}

	log.Printf("Successfully applied operation %s from %s with sequence %d",
		msg.OperationID, msg.UserID, *msg.SequenceNumber)

	// Broadcast to all other clients (not the sender)
	s.broadcastToOthers(client, msg)
}

// processPresenterRequest handles presenter mode requests
func (s *DiagramSession) processPresenterRequest(client *WebSocketClient, message []byte) {
	var msg PresenterRequestMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Error parsing presenter request: %v", err)
		return
	}

	// Validate user ID matches client
	if msg.UserID != client.UserName {
		log.Printf("User ID mismatch in presenter request: %s != %s", msg.UserID, client.UserName)
		return
	}

	s.mu.RLock()
	currentPresenter := s.CurrentPresenter
	owner := s.Owner
	s.mu.RUnlock()

	// If user is already the presenter, ignore
	if msg.UserID == currentPresenter {
		log.Printf("User %s is already the presenter", msg.UserID)
		return
	}

	// If user is the owner, automatically grant presenter mode
	if msg.UserID == owner {
		s.mu.Lock()
		s.CurrentPresenter = msg.UserID
		s.mu.Unlock()

		// Broadcast new presenter to all clients
		broadcastMsg := CurrentPresenterMessage{
			MessageType:      "current_presenter",
			CurrentPresenter: msg.UserID,
		}
		s.broadcastMessage(broadcastMsg)
		log.Printf("Owner %s became presenter in session %s", msg.UserID, s.ID)
		return
	}

	// For non-owners, notify the owner of the presenter request
	// The owner can then use change_presenter to grant or send presenter_denied to deny
	ownerClient := s.findClientByUserID(owner)
	if ownerClient != nil {
		// Forward the request to the owner for approval
		s.sendToClient(ownerClient, msg)
		log.Printf("Forwarded presenter request from %s to owner %s in session %s", msg.UserID, owner, s.ID)
	} else {
		log.Printf("Owner %s not connected, cannot process presenter request from %s", owner, msg.UserID)

		// Send denial to requester since owner is not available
		deniedMsg := PresenterDeniedMessage{
			MessageType: "presenter_denied",
			UserID:      "system",
			TargetUser:  msg.UserID,
		}
		s.sendToClient(client, deniedMsg)
	}
}

// processChangePresenter handles owner changing presenter
func (s *DiagramSession) processChangePresenter(client *WebSocketClient, message []byte) {
	var msg ChangePresenterMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Error parsing change presenter: %v", err)
		return
	}

	// Only owner can change presenter
	s.mu.RLock()
	owner := s.Owner
	s.mu.RUnlock()

	if client.UserName != owner {
		log.Printf("Non-owner attempted to change presenter: %s", client.UserName)
		return
	}

	// Change presenter
	s.mu.Lock()
	s.CurrentPresenter = msg.NewPresenter
	s.mu.Unlock()

	// Broadcast new presenter to all clients
	broadcastMsg := CurrentPresenterMessage{
		MessageType:      "current_presenter",
		CurrentPresenter: msg.NewPresenter,
	}
	s.broadcastMessage(broadcastMsg)
	log.Printf("Owner %s changed presenter to %s in session %s", client.UserName, msg.NewPresenter, s.ID)
}

// processPresenterDenied handles owner denying presenter requests
func (s *DiagramSession) processPresenterDenied(client *WebSocketClient, message []byte) {
	var msg PresenterDeniedMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Error parsing presenter denied: %v", err)
		return
	}

	// Only owner can deny presenter requests
	s.mu.RLock()
	owner := s.Owner
	s.mu.RUnlock()

	if client.UserName != owner {
		log.Printf("Non-owner attempted to deny presenter request: %s", client.UserName)
		return
	}

	// Validate user ID matches client (sender should be owner)
	if msg.UserID != client.UserName {
		log.Printf("User ID mismatch in presenter denied: %s != %s", msg.UserID, client.UserName)
		return
	}

	// Find the target user to send the denial
	targetClient := s.findClientByUserID(msg.TargetUser)
	if targetClient != nil {
		s.sendToClient(targetClient, msg)
		log.Printf("Owner %s denied presenter request from %s in session %s", msg.UserID, msg.TargetUser, s.ID)
	} else {
		log.Printf("Target user %s not found for presenter denial in session %s", msg.TargetUser, s.ID)
	}
}

// processPresenterCursor handles cursor position updates
func (s *DiagramSession) processPresenterCursor(client *WebSocketClient, message []byte) {
	var msg PresenterCursorMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Error parsing presenter cursor: %v", err)
		return
	}

	// Validate user ID matches client
	if msg.UserID != client.UserName {
		log.Printf("User ID mismatch in presenter cursor: %s != %s", msg.UserID, client.UserName)
		return
	}

	// Only current presenter can send cursor updates
	s.mu.RLock()
	currentPresenter := s.CurrentPresenter
	s.mu.RUnlock()

	if client.UserName != currentPresenter {
		log.Printf("Non-presenter attempted to send cursor: %s", client.UserName)
		return
	}

	// Broadcast cursor to all other clients
	s.broadcastToOthers(client, msg)
}

// processPresenterSelection handles selection updates
func (s *DiagramSession) processPresenterSelection(client *WebSocketClient, message []byte) {
	var msg PresenterSelectionMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Error parsing presenter selection: %v", err)
		return
	}

	// Validate user ID matches client
	if msg.UserID != client.UserName {
		log.Printf("User ID mismatch in presenter selection: %s != %s", msg.UserID, client.UserName)
		return
	}

	// Only current presenter can send selection updates
	s.mu.RLock()
	currentPresenter := s.CurrentPresenter
	s.mu.RUnlock()

	if client.UserName != currentPresenter {
		log.Printf("Non-presenter attempted to send selection: %s", client.UserName)
		return
	}

	// Broadcast selection to all other clients
	s.broadcastToOthers(client, msg)
}

// processResyncRequest handles client resync requests
func (s *DiagramSession) processResyncRequest(client *WebSocketClient, message []byte) {
	var msg ResyncRequestMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Error parsing resync request: %v", err)
		return
	}

	// Validate user ID matches client
	if msg.UserID != client.UserName {
		log.Printf("User ID mismatch in resync request: %s != %s", msg.UserID, client.UserName)
		return
	}

	log.Printf("Client %s requested resync for diagram %s", client.UserName, s.DiagramID)

	// According to the plan, we use REST API for resync for simplicity
	// Send a message telling the client to use the REST endpoint for resync
	resyncResponse := ResyncResponseMessage{
		MessageType:   "resync_response",
		UserID:        "system",
		TargetUser:    msg.UserID,
		Method:        "rest_api",
		DiagramID:     s.DiagramID,
		ThreatModelID: s.ThreatModelID,
	}

	s.sendToClient(client, resyncResponse)
	log.Printf("Sent resync response to %s for diagram %s", msg.UserID, s.DiagramID)

	// Record performance metrics
	if GlobalPerformanceMonitor != nil {
		GlobalPerformanceMonitor.RecordResyncRequest(s.ID, msg.UserID)
	}
}

// processUndoRequest handles undo requests
func (s *DiagramSession) processUndoRequest(client *WebSocketClient, message []byte) {
	var msg UndoRequestMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Error parsing undo request: %v", err)
		return
	}

	// Validate user ID matches client
	if msg.UserID != client.UserName {
		log.Printf("User ID mismatch in undo request: %s != %s", msg.UserID, client.UserName)
		return
	}

	// Check permission
	if !s.checkMutationPermission(client.UserName) {
		s.sendAuthorizationDenied(client, "", "insufficient_permissions")
		return
	}

	// Check if undo is possible
	if !s.OperationHistory.CanUndo() {
		log.Printf("No operations to undo for user %s", client.UserName)
		// Send message indicating no undo available
		response := HistoryOperationMessage{
			MessageType:   "history_operation",
			OperationType: "undo",
			Message:       "no_operations_to_undo",
		}
		s.sendToClient(client, response)
		return
	}

	// Get the operation to undo
	entry, previousState, ok := s.OperationHistory.GetUndoOperation()
	if !ok {
		log.Printf("Failed to get undo operation for user %s", client.UserName)
		// Send resync required as fallback
		response := HistoryOperationMessage{
			MessageType:   "history_operation",
			OperationType: "undo",
			Message:       "resync_required",
		}
		s.sendToClient(client, response)
		return
	}

	// Apply the undo by restoring previous state
	err := s.applyHistoryState(previousState)
	if err != nil {
		log.Printf("Failed to apply undo state: %v", err)
		// Send resync required as fallback
		response := HistoryOperationMessage{
			MessageType:   "history_operation",
			OperationType: "undo",
			Message:       "resync_required",
		}
		s.sendToClient(client, response)
		return
	}

	// Update history position
	s.OperationHistory.MoveToPosition(entry.SequenceNumber - 1)

	// Broadcast the undo result to all clients (they should resync)
	response := HistoryOperationMessage{
		MessageType:   "history_operation",
		OperationType: "undo",
		Message:       "resync_required", // For now, tell clients to resync
	}
	s.broadcastToAllClients(response)
	log.Printf("Processed undo request from %s, reverted to sequence %d", client.UserName, entry.SequenceNumber-1)
}

// processRedoRequest handles redo requests
func (s *DiagramSession) processRedoRequest(client *WebSocketClient, message []byte) {
	var msg RedoRequestMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Error parsing redo request: %v", err)
		return
	}

	// Validate user ID matches client
	if msg.UserID != client.UserName {
		log.Printf("User ID mismatch in redo request: %s != %s", msg.UserID, client.UserName)
		return
	}

	// Check permission
	if !s.checkMutationPermission(client.UserName) {
		s.sendAuthorizationDenied(client, "", "insufficient_permissions")
		return
	}

	// Check if redo is possible
	if !s.OperationHistory.CanRedo() {
		log.Printf("No operations to redo for user %s", client.UserName)
		// Send message indicating no redo available
		response := HistoryOperationMessage{
			MessageType:   "history_operation",
			OperationType: "redo",
			Message:       "no_operations_to_redo",
		}
		s.sendToClient(client, response)
		return
	}

	// Get the operation to redo
	entry, ok := s.OperationHistory.GetRedoOperation()
	if !ok {
		log.Printf("Failed to get redo operation for user %s", client.UserName)
		// Send resync required as fallback
		response := HistoryOperationMessage{
			MessageType:   "history_operation",
			OperationType: "redo",
			Message:       "resync_required",
		}
		s.sendToClient(client, response)
		return
	}

	// Apply the redo by re-executing the operation
	err := s.applyHistoryOperation(entry.Operation)
	if err != nil {
		log.Printf("Failed to apply redo operation: %v", err)
		// Send resync required as fallback
		response := HistoryOperationMessage{
			MessageType:   "history_operation",
			OperationType: "redo",
			Message:       "resync_required",
		}
		s.sendToClient(client, response)
		return
	}

	// Update history position
	s.OperationHistory.MoveToPosition(entry.SequenceNumber)

	// Broadcast the redo result to all clients (they should resync)
	response := HistoryOperationMessage{
		MessageType:   "history_operation",
		OperationType: "redo",
		Message:       "resync_required", // For now, tell clients to resync
	}
	s.broadcastToAllClients(response)
	log.Printf("Processed redo request from %s, restored to sequence %d", client.UserName, entry.SequenceNumber)
}

// processLegacyMessage handles backward compatibility with old message format
func (s *DiagramSession) processLegacyMessage(client *WebSocketClient, message []byte) {
	// Parse legacy message format
	var clientMsg struct {
		Operation json.RawMessage `json:"operation"`
	}
	if err := json.Unmarshal(message, &clientMsg); err != nil {
		log.Printf("Error parsing legacy WebSocket message: %v", err)
		return
	}

	// Validate message size
	if len(clientMsg.Operation) > 1024*50 { // 50KB limit
		log.Printf("Operation too large (%d bytes), ignoring", len(clientMsg.Operation))
		return
	}

	// Create server message
	msg := WebSocketMessage{
		Event:     "update",
		UserID:    client.UserName,
		Timestamp: time.Now().UTC(),
	}

	// Unmarshal operation
	var op DiagramOperation
	if err := json.Unmarshal(clientMsg.Operation, &op); err != nil {
		log.Printf("Error parsing operation: %v", err)
		return
	}

	// Validate operation
	if err := validateDiagramOperation(op); err != nil {
		log.Printf("Invalid diagram operation: %v", err)
		return
	}

	msg.Operation = op

	// Apply operation to the diagram
	if err := applyDiagramOperation(s.DiagramID, op); err != nil {
		log.Printf("Error applying operation to diagram: %v", err)
		// Still broadcast the operation to maintain consistency
	}

	// Marshal and broadcast
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling message: %v", err)
		return
	}

	s.Broadcast <- msgBytes
}

// Helper methods

// handlePresenterDisconnection handles when the current presenter leaves the session
func (s *DiagramSession) handlePresenterDisconnection(disconnectedUserID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Printf("Presenter %s disconnected from session %s, reassigning presenter", disconnectedUserID, s.ID)

	// Reset presenter according to the plan:
	// 1. First try to set presenter back to session owner
	// 2. If owner has also left, set presenter to first remaining user with write permissions

	var newPresenter string

	// Check if owner is still connected
	ownerConnected := false
	for client := range s.Clients {
		if client.UserName == s.Owner {
			ownerConnected = true
			newPresenter = s.Owner
			break
		}
	}

	// If owner is not connected, find first user with write permissions
	if !ownerConnected && s.ThreatModelID != "" {
		// Get the threat model to check user permissions
		tm, err := ThreatModelStore.Get(s.ThreatModelID)
		if err != nil {
			log.Printf("Failed to get threat model %s for presenter reassignment: %v", s.ThreatModelID, err)
		} else {
			// Find first connected user with write permissions
			for client := range s.Clients {
				hasWriteAccess, err := CheckResourceAccess(client.UserName, tm, RoleWriter)
				if err == nil && hasWriteAccess {
					newPresenter = client.UserName
					break
				}
			}
		}
	}

	// If we found a new presenter, assign and broadcast
	if newPresenter != "" {
		s.CurrentPresenter = newPresenter

		// Broadcast new presenter to all clients
		broadcastMsg := CurrentPresenterMessage{
			MessageType:      "current_presenter",
			CurrentPresenter: newPresenter,
		}

		// Release the lock before broadcasting to avoid deadlock
		s.mu.Unlock()
		s.broadcastMessage(broadcastMsg)
		s.mu.Lock()

		log.Printf("Set new presenter to %s in session %s after %s disconnected", newPresenter, s.ID, disconnectedUserID)
	} else {
		// No suitable presenter found, clear presenter
		s.CurrentPresenter = ""
		log.Printf("No suitable presenter found for session %s after %s disconnected", s.ID, disconnectedUserID)
	}
}

// findClientByUserID finds a connected client by their user ID
func (s *DiagramSession) findClientByUserID(userID string) *WebSocketClient {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for client := range s.Clients {
		if client.UserName == userID {
			return client
		}
	}
	return nil
}

// extractCellIDs extracts cell IDs from cell operations
func extractCellIDs(cells []CellOperation) []string {
	ids := make([]string, len(cells))
	for i, cell := range cells {
		ids[i] = cell.ID
	}
	return ids
}

// getUserRole gets the user's role for the session's threat model
func (s *DiagramSession) getUserRole(userID string) Role {
	// Anonymous users are readers at best
	if userID == "" {
		return RoleReader
	}

	// If no threat model ID, assume writer for backward compatibility
	if s.ThreatModelID == "" {
		return RoleWriter
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(s.ThreatModelID)
	if err != nil {
		// If we can't get the threat model, assume reader for safety
		return RoleReader
	}

	// Check user permissions using existing utility
	role := GetUserRole(userID, tm)
	return role
}

// checkMutationPermission checks if user can perform mutations
func (s *DiagramSession) checkMutationPermission(userID string) bool {
	// Anonymous users cannot perform mutations
	if userID == "" {
		return false
	}

	// If no threat model ID, allow (for backward compatibility with direct diagram access)
	if s.ThreatModelID == "" {
		return true
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(s.ThreatModelID)
	if err != nil {
		// If we can't get the threat model, deny access for safety
		return false
	}

	// Check if user has write access to the threat model
	hasWriteAccess, err := CheckResourceAccess(userID, tm, RoleWriter)
	if err != nil {
		// If there's an error checking access, deny for safety
		return false
	}

	return hasWriteAccess
}

// sendAuthorizationDenied sends authorization denied message to client
func (s *DiagramSession) sendAuthorizationDenied(client *WebSocketClient, operationID, reason string) {
	msg := AuthorizationDeniedMessage{
		MessageType:         "authorization_denied",
		OriginalOperationID: operationID,
		Reason:              reason,
	}
	s.sendToClient(client, msg)

	// Record performance metrics
	if GlobalPerformanceMonitor != nil {
		GlobalPerformanceMonitor.RecordAuthorizationDenied(s.ID, client.UserName, reason)
	}
}

// sendStateCorrection sends the current state of specified cells to correct client state
func (s *DiagramSession) sendStateCorrection(client *WebSocketClient, affectedCellIDs []string) {
	s.sendStateCorrectionWithReason(client, affectedCellIDs, "operation_failed")
}

// sendStateCorrectionWithReason sends state correction with detailed logging and reason tracking
func (s *DiagramSession) sendStateCorrectionWithReason(client *WebSocketClient, affectedCellIDs []string, reason string) {
	if len(affectedCellIDs) == 0 {
		return
	}

	log.Printf("Sending state correction to %s for cells %v (reason: %s)", client.UserName, affectedCellIDs, reason)

	// Check user permission level for enhanced messaging
	userRole := s.getUserRole(client.UserName)
	s.sendEnhancedStateCorrection(client, affectedCellIDs, reason, userRole)
}

// sendEnhancedStateCorrection sends enhanced state correction with role-specific messaging
func (s *DiagramSession) sendEnhancedStateCorrection(client *WebSocketClient, affectedCellIDs []string, reason string, userRole Role) {

	// Get current diagram state
	diagram, err := DiagramStore.Get(s.DiagramID)
	if err != nil {
		log.Printf("Error getting diagram for state correction: %v", err)
		return
	}

	// Build correction cells for the affected cell IDs
	var correctionCells []Cell
	converter := NewCellConverter()

	// Convert diagram cells to a map for quick lookup
	cellMap := make(map[string]Cell)
	totalCells := 0
	for _, cellItem := range diagram.Cells {
		// Convert DfdDiagram_Cells_Item to Cell using existing conversion logic
		if cell, err := converter.ConvertUnionItemToCell(cellItem); err == nil {
			cellMap[cell.Id.String()] = cell
			totalCells++
		}
	}

	// Include current state of affected cells in the correction
	correctionsSent := 0
	for _, cellID := range affectedCellIDs {
		if cell, exists := cellMap[cellID]; exists {
			correctionCells = append(correctionCells, cell)
			correctionsSent++
		}
		// If cell doesn't exist, it means it was deleted - this is also valid state to communicate
	}

	// Send the correction with enhanced messaging based on user role and reason
	if len(correctionCells) > 0 || correctionsSent < len(affectedCellIDs) {
		correctionMsg := StateCorrectionMessage{
			MessageType: "state_correction",
			Cells:       correctionCells,
		}
		s.sendToClient(client, correctionMsg)

		// Enhanced logging based on reason and user role
		s.logEnhancedStateCorrection(client.UserName, reason, userRole, correctionsSent, len(affectedCellIDs)-correctionsSent, totalCells)

		// Track correction frequency for potential sync issues
		s.trackCorrectionEvent(client.UserName, reason)

		// Record performance metrics
		if GlobalPerformanceMonitor != nil {
			GlobalPerformanceMonitor.RecordStateCorrection(s.ID, client.UserName, reason, len(correctionCells))
		}
	}
}

// logEnhancedStateCorrection provides detailed logging for state corrections
func (s *DiagramSession) logEnhancedStateCorrection(userID string, reason string, userRole Role, correctionsSent, deletionsSent, totalCells int) {
	roleStr := string(userRole)

	switch reason {
	case "unauthorized_operation":
		log.Printf("STATE CORRECTION [UNAUTHORIZED]: User %s (%s role) attempted unauthorized operation - sent %d cell corrections, %d deletions (total cells: %d)",
			userID, roleStr, correctionsSent, deletionsSent, totalCells)

		// Enhanced security logging for unauthorized operations
		if userRole == RoleReader {
			log.Printf("SECURITY ALERT: Read-only user %s attempted to modify diagram %s", userID, s.DiagramID)
		}

	case "operation_failed":
		log.Printf("STATE CORRECTION [OPERATION_FAILED]: User %s (%s role) operation failed - sent %d cell corrections, %d deletions (total cells: %d)",
			userID, roleStr, correctionsSent, deletionsSent, totalCells)

	case "out_of_order_sequence", "duplicate_message", "message_gap":
		log.Printf("STATE CORRECTION [SYNC_ISSUE]: User %s (%s role) sync issue (%s) - sent %d cell corrections, %d deletions (total cells: %d)",
			userID, roleStr, reason, correctionsSent, deletionsSent, totalCells)

	default:
		log.Printf("STATE CORRECTION [%s]: User %s (%s role) - sent %d cell corrections, %d deletions (total cells: %d)",
			strings.ToUpper(reason), userID, roleStr, correctionsSent, deletionsSent, totalCells)
	}
}

// trackCorrectionEvent tracks state corrections for detecting sync issues
func (s *DiagramSession) trackCorrectionEvent(userID, reason string) {
	// Simple in-memory tracking - in production this might be more sophisticated
	s.mu.Lock()
	defer s.mu.Unlock()

	// Add correction tracking to session metadata if needed
	// For now, just log patterns that might indicate sync issues
	correctionKey := fmt.Sprintf("%s_%s", userID, reason)

	// Check if this user is experiencing frequent corrections
	if s.recentCorrections == nil {
		s.recentCorrections = make(map[string]int)
	}

	s.recentCorrections[correctionKey]++

	// Log potential sync issues
	if s.recentCorrections[correctionKey] >= 3 {
		log.Printf("WARNING: User %s has received %d state corrections for reason '%s' - potential sync issue",
			userID, s.recentCorrections[correctionKey], reason)
	}
}

// trackPotentialSyncIssue tracks potential synchronization issues for detecting client-server desync
func (s *DiagramSession) trackPotentialSyncIssue(userID, issueType string) {
	// Simple in-memory tracking for sequence-related sync issues
	s.mu.Lock()
	defer s.mu.Unlock()

	issueKey := fmt.Sprintf("%s_%s", userID, issueType)

	// Initialize tracking map if needed
	if s.recentCorrections == nil {
		s.recentCorrections = make(map[string]int)
	}

	s.recentCorrections[issueKey]++

	// Log potential sync issues based on frequency
	if s.recentCorrections[issueKey] >= 5 {
		log.Printf("WARNING: User %s has experienced %d '%s' issues - may need resync",
			userID, s.recentCorrections[issueKey], issueType)

		// Send automatic resync recommendation to client
		s.sendResyncRecommendation(userID, issueType)

		// Reset counter after sending recommendation
		s.recentCorrections[issueKey] = 0
	}
}

// sendResyncRecommendation sends a resync recommendation to a client experiencing sync issues
func (s *DiagramSession) sendResyncRecommendation(userID, issueType string) {
	// Find the client by user ID
	client := s.findClientByUserID(userID)
	if client == nil {
		log.Printf("Cannot send resync recommendation: client %s not found", userID)
		return
	}

	// Send a resync response message to recommend the client resync via REST API
	resyncResponse := ResyncResponseMessage{
		MessageType:   "resync_response",
		UserID:        "system",
		TargetUser:    userID,
		Method:        "rest_api",
		DiagramID:     s.DiagramID,
		ThreatModelID: s.ThreatModelID,
	}

	s.sendToClient(client, resyncResponse)
	log.Printf("Sent automatic resync recommendation to %s due to %s issues", userID, issueType)
}

// applyHistoryState applies a historical state to the diagram (for undo)
func (s *DiagramSession) applyHistoryState(state map[string]*Cell) error {
	// Get current diagram
	diagram, err := DiagramStore.Get(s.DiagramID)
	if err != nil {
		return fmt.Errorf("failed to get diagram: %w", err)
	}

	// Clear existing cells and apply historical state
	diagram.Cells = []DfdDiagram_Cells_Item{}

	// Create converter for cell transformations
	converter := NewCellConverter()

	// Convert historical state back to diagram cells
	for _, cell := range state {
		// Convert Cell to DfdDiagram_Cells_Item
		cellUnion, err := converter.ConvertCellToUnionItem(*cell)
		if err != nil {
			log.Printf("Warning: failed to convert cell %s: %v", cell.Id.String(), err)
			continue
		}
		diagram.Cells = append(diagram.Cells, cellUnion)
	}

	// Update modification time
	now := time.Now().UTC()
	diagram.ModifiedAt = now

	// Save updated diagram
	return DiagramStore.Update(s.DiagramID, diagram)
}

// applyHistoryOperation applies a historical operation to the diagram (for redo)
func (s *DiagramSession) applyHistoryOperation(operation CellPatchOperation) error {
	// Use the shared processor to apply the operation
	processor := NewCellOperationProcessor(DiagramStore)
	result, err := processor.ProcessCellOperations(s.DiagramID, operation)
	if err != nil {
		return fmt.Errorf("failed to process operation: %w", err)
	}

	if !result.Valid {
		return fmt.Errorf("operation invalid: %s", result.Reason)
	}

	return nil
}

// broadcastToAllClients broadcasts a message to all connected clients
func (s *DiagramSession) broadcastToAllClients(message interface{}) {
	msgBytes, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshaling broadcast message: %v", err)
		return
	}

	s.mu.RLock()
	clients := make([]*WebSocketClient, 0, len(s.Clients))
	for client := range s.Clients {
		clients = append(clients, client)
	}
	s.mu.RUnlock()

	// Send to all clients
	for _, client := range clients {
		select {
		case client.Send <- msgBytes:
		default:
			log.Printf("Failed to send message to client %s", client.UserName)
		}
	}
}

// sendToClient sends a message to a specific client
func (s *DiagramSession) sendToClient(client *WebSocketClient, message interface{}) {
	msgBytes, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshaling message: %v", err)
		return
	}

	select {
	case client.Send <- msgBytes:
	default:
		log.Printf("Client send channel full, dropping message")
	}
}

// broadcastMessage broadcasts a message to all clients
func (s *DiagramSession) broadcastMessage(message interface{}) {
	msgBytes, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshaling broadcast message: %v", err)
		return
	}

	s.Broadcast <- msgBytes
}

// broadcastToOthers broadcasts a message to all clients except the sender
func (s *DiagramSession) broadcastToOthers(sender *WebSocketClient, message interface{}) {
	msgBytes, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshaling message: %v", err)
		return
	}

	s.mu.RLock()
	for client := range s.Clients {
		if client != sender {
			select {
			case client.Send <- msgBytes:
			default:
				log.Printf("Client send channel full, dropping message")
			}
		}
	}
	s.mu.RUnlock()
}

// CellOperationProcessor processes cell operations with validation and conflict detection
type CellOperationProcessor struct {
	diagramStore DiagramStoreInterface
	converter    *CellConverter
}

// NewCellOperationProcessor creates a new cell operation processor
func NewCellOperationProcessor(store DiagramStoreInterface) *CellOperationProcessor {
	return &CellOperationProcessor{
		diagramStore: store,
		converter:    NewCellConverter(),
	}
}

// ProcessCellOperations processes a batch of cell operations with full validation
func (cop *CellOperationProcessor) ProcessCellOperations(diagramID string, operation CellPatchOperation) (*OperationValidationResult, error) {
	// Get current diagram state
	diagram, err := cop.diagramStore.Get(diagramID)
	if err != nil {
		return nil, fmt.Errorf("failed to get diagram %s: %w", diagramID, err)
	}

	// Build current state map for conflict detection
	currentState := make(map[string]*Cell)
	for _, cellItem := range diagram.Cells {
		if cell, err := cop.converter.ConvertUnionItemToCell(cellItem); err == nil {
			currentState[cell.Id.String()] = &cell
		}
	}

	// Process and validate operations
	result := cop.processAndValidateCellOperations(&diagram, currentState, operation)

	if result.Valid && result.StateChanged {
		// Save the updated diagram
		if err := cop.diagramStore.Update(diagramID, diagram); err != nil {
			result.Valid = false
			result.Reason = "save_failed"
			return result, fmt.Errorf("failed to save diagram: %w", err)
		}
	}

	return result, nil
}

// processAndValidateCellOperations processes and validates cell operations (extracted from DiagramSession)
func (cop *CellOperationProcessor) processAndValidateCellOperations(diagram *DfdDiagram, currentState map[string]*Cell, operation CellPatchOperation) *OperationValidationResult {
	result := &OperationValidationResult{
		Valid:         true,
		StateChanged:  false,
		CellsModified: make([]string, 0),
		PreviousState: make(map[string]*Cell),
	}

	// Copy current state as previous state
	for k, v := range currentState {
		cellCopy := *v // Copy cell value
		result.PreviousState[k] = &cellCopy
	}

	// Validate operation structure
	if operation.Type != "patch" {
		result.Valid = false
		result.Reason = "invalid_operation_type"
		return result
	}

	if len(operation.Cells) == 0 {
		result.Valid = false
		result.Reason = "empty_cell_operations"
		return result
	}

	// Process each cell operation
	for _, cellOp := range operation.Cells {
		cellResult := cop.validateAndProcessCellOperation(diagram, currentState, cellOp)

		if !cellResult.Valid {
			result.Valid = false
			result.Reason = cellResult.Reason
			result.ConflictDetected = cellResult.ConflictDetected
			result.CorrectionNeeded = cellResult.CorrectionNeeded
			result.CellsModified = append(result.CellsModified, cellOp.ID)
			return result
		}

		if cellResult.StateChanged {
			result.StateChanged = true
			result.CellsModified = append(result.CellsModified, cellOp.ID)
		}
	}

	return result
}

// validateAndProcessCellOperation validates and processes a single cell operation (extracted from DiagramSession)
func (cop *CellOperationProcessor) validateAndProcessCellOperation(diagram *DfdDiagram, currentState map[string]*Cell, cellOp CellOperation) *OperationValidationResult {
	result := &OperationValidationResult{Valid: true}

	switch cellOp.Operation {
	case "add":
		return cop.validateAddOperation(diagram, currentState, cellOp)
	case "update":
		return cop.validateUpdateOperation(diagram, currentState, cellOp)
	case "remove":
		return cop.validateRemoveOperation(diagram, currentState, cellOp)
	default:
		result.Valid = false
		result.Reason = "invalid_cell_operation"
		return result
	}
}

// validateAddOperation validates adding a new cell
func (cop *CellOperationProcessor) validateAddOperation(diagram *DfdDiagram, currentState map[string]*Cell, cellOp CellOperation) *OperationValidationResult {
	result := &OperationValidationResult{Valid: true}

	// Check if cell already exists (conflict)
	if _, exists := currentState[cellOp.ID]; exists {
		result.Valid = false
		result.Reason = "cell_already_exists"
		result.ConflictDetected = true
		result.CorrectionNeeded = true
		return result
	}

	// Validate cell data
	if cellOp.Data == nil {
		result.Valid = false
		result.Reason = "add_requires_cell_data"
		return result
	}

	if err := cop.validateCellData(cellOp.Data); err != nil {
		result.Valid = false
		result.Reason = "invalid_cell_data"
		return result
	}

	// Apply the add operation to diagram
	cellItem, err := cop.converter.ConvertCellToUnionItem(*cellOp.Data)
	if err != nil {
		result.Valid = false
		result.Reason = "cell_conversion_failed"
		return result
	}

	diagram.Cells = append(diagram.Cells, cellItem)
	result.StateChanged = true

	return result
}

// validateUpdateOperation validates updating an existing cell
func (cop *CellOperationProcessor) validateUpdateOperation(diagram *DfdDiagram, currentState map[string]*Cell, cellOp CellOperation) *OperationValidationResult {
	result := &OperationValidationResult{Valid: true}

	// Check if cell exists
	existingCell, exists := currentState[cellOp.ID]
	if !exists {
		result.Valid = false
		result.Reason = "update_nonexistent_cell"
		result.ConflictDetected = true
		result.CorrectionNeeded = true
		return result
	}

	// Validate cell data
	if cellOp.Data == nil {
		result.Valid = false
		result.Reason = "update_requires_cell_data"
		return result
	}

	if err := cop.validateCellData(cellOp.Data); err != nil {
		result.Valid = false
		result.Reason = "invalid_cell_data"
		return result
	}

	// Check if update actually changes anything
	stateChanged := cop.detectCellChanges(existingCell, cellOp.Data)
	if !stateChanged {
		result.StateChanged = false
		return result // Valid but no changes
	}

	// Apply the update operation to diagram
	found := false
	for i := range diagram.Cells {
		cellItem := &diagram.Cells[i]
		if cell, err := cop.converter.ConvertUnionItemToCell(*cellItem); err == nil {
			if cell.Id.String() == cellOp.ID {
				// Replace with updated cell
				if updatedCellItem, err := cop.converter.ConvertCellToUnionItem(*cellOp.Data); err == nil {
					diagram.Cells[i] = updatedCellItem
					found = true
					break
				}
			}
		}
	}

	if !found {
		result.Valid = false
		result.Reason = "cell_not_found_in_diagram"
		result.ConflictDetected = true
		return result
	}

	result.StateChanged = true

	return result
}

// validateRemoveOperation validates removing a cell
func (cop *CellOperationProcessor) validateRemoveOperation(diagram *DfdDiagram, currentState map[string]*Cell, cellOp CellOperation) *OperationValidationResult {
	result := &OperationValidationResult{Valid: true}

	// Check if cell exists
	if _, exists := currentState[cellOp.ID]; !exists {
		// Removing non-existent cell is idempotent - not an error
		result.StateChanged = false
		return result
	}

	// Apply the remove operation to diagram
	found := false
	for i, cellItem := range diagram.Cells {
		if cell, err := cop.converter.ConvertUnionItemToCell(cellItem); err == nil {
			if cell.Id.String() == cellOp.ID {
				// Remove by replacing with last element and truncating
				lastIndex := len(diagram.Cells) - 1
				if i != lastIndex {
					diagram.Cells[i] = diagram.Cells[lastIndex]
				}
				diagram.Cells = diagram.Cells[:lastIndex]
				found = true
				break
			}
		}
	}

	result.StateChanged = found

	return result
}

// validateCellData validates cell data structure
func (cop *CellOperationProcessor) validateCellData(cell *Cell) error {
	if cell == nil {
		return fmt.Errorf("cell cannot be nil")
	}

	// Validate ID is not zero UUID
	if cell.Id.String() == "00000000-0000-0000-0000-000000000000" {
		return fmt.Errorf("cell ID is required")
	}

	// Validate required fields
	if cell.Shape == "" {
		return fmt.Errorf("cell shape is required")
	}

	// Basic validation - more detailed validation would be done during conversion
	return nil
}

// detectCellChanges compares two cells to determine if there are meaningful changes
func (cop *CellOperationProcessor) detectCellChanges(existing, updated *Cell) bool {
	if existing.Id != updated.Id {
		return true
	}
	if existing.Shape != updated.Shape {
		return true
	}

	// Compare visibility
	if (existing.Visible == nil) != (updated.Visible == nil) {
		return true
	}
	if existing.Visible != nil && updated.Visible != nil && *existing.Visible != *updated.Visible {
		return true
	}

	// Compare Z-index
	if (existing.ZIndex == nil) != (updated.ZIndex == nil) {
		return true
	}
	if existing.ZIndex != nil && updated.ZIndex != nil && *existing.ZIndex != *updated.ZIndex {
		return true
	}

	// For more detailed comparison, we'd need to convert to specific types (Node/Edge)
	// and compare their specific properties, but for now we assume any difference
	// in the Cell struct warrants an update
	return false
}

// OperationValidationResult represents the result of operation validation
type OperationValidationResult struct {
	Valid            bool
	Reason           string
	CorrectionNeeded bool
	ConflictDetected bool
	StateChanged     bool
	CellsModified    []string
	PreviousState    map[string]*Cell
}

// applyOperation applies a diagram operation with validation and conflict detection
func (s *DiagramSession) applyOperation(client *WebSocketClient, msg DiagramOperationMessage) bool {

	// Get current diagram state
	diagram, err := DiagramStore.Get(s.DiagramID)
	if err != nil {
		log.Printf("Failed to get diagram %s: %v", s.DiagramID, err)
		return false
	}

	// Build current state map for conflict detection
	currentState := make(map[string]*Cell)
	converter := NewCellConverter()
	for _, cellItem := range diagram.Cells {
		if cell, err := converter.ConvertUnionItemToCell(cellItem); err == nil {
			currentState[cell.Id.String()] = &cell
		}
	}

	// Process each cell operation
	result := s.processAndValidateCellOperations(&diagram, currentState, msg.Operation)

	if !result.Valid {
		log.Printf("Operation %s validation failed: %s", msg.OperationID, result.Reason)

		if result.CorrectionNeeded {
			s.sendStateCorrection(client, result.CellsModified)
		}

		return false
	}

	if !result.StateChanged {
		log.Printf("Operation %s resulted in no state changes", msg.OperationID)
		return false // Don't broadcast no-op operations
	}

	// Update operation history
	s.addToHistory(msg, result.PreviousState, currentState)

	log.Printf("Successfully applied operation %s from %s with sequence %d",
		msg.OperationID, msg.UserID, *msg.SequenceNumber)

	return true
}

// processAndValidateCellOperations processes and validates cell operations
func (s *DiagramSession) processAndValidateCellOperations(diagram *DfdDiagram, currentState map[string]*Cell, operation CellPatchOperation) OperationValidationResult {
	result := OperationValidationResult{
		Valid:         true,
		StateChanged:  false,
		CellsModified: make([]string, 0),
		PreviousState: make(map[string]*Cell),
	}

	// Copy current state as previous state
	for k, v := range currentState {
		cellCopy := *v // Copy cell value
		result.PreviousState[k] = &cellCopy
	}

	// Validate operation structure
	if operation.Type != "patch" {
		result.Valid = false
		result.Reason = "invalid_operation_type"
		return result
	}

	if len(operation.Cells) == 0 {
		result.Valid = false
		result.Reason = "empty_cell_operations"
		return result
	}

	// Process each cell operation
	for _, cellOp := range operation.Cells {
		cellResult := s.validateAndProcessCellOperation(diagram, currentState, cellOp)

		if !cellResult.Valid {
			result.Valid = false
			result.Reason = cellResult.Reason
			result.ConflictDetected = cellResult.ConflictDetected
			result.CorrectionNeeded = cellResult.CorrectionNeeded
			result.CellsModified = append(result.CellsModified, cellOp.ID)
			return result
		}

		if cellResult.StateChanged {
			result.StateChanged = true
			result.CellsModified = append(result.CellsModified, cellOp.ID)
		}
	}

	return result
}

// validateAndProcessCellOperation validates and processes a single cell operation
func (s *DiagramSession) validateAndProcessCellOperation(diagram *DfdDiagram, currentState map[string]*Cell, cellOp CellOperation) OperationValidationResult {
	result := OperationValidationResult{Valid: true}

	switch cellOp.Operation {
	case "add":
		return s.validateAddOperation(diagram, currentState, cellOp)
	case "update":
		return s.validateUpdateOperation(diagram, currentState, cellOp)
	case "remove":
		return s.validateRemoveOperation(diagram, currentState, cellOp)
	default:
		result.Valid = false
		result.Reason = "invalid_cell_operation"
		return result
	}
}

// validateAddOperation validates adding a new cell
func (s *DiagramSession) validateAddOperation(diagram *DfdDiagram, currentState map[string]*Cell, cellOp CellOperation) OperationValidationResult {
	result := OperationValidationResult{Valid: true}

	// Check if cell already exists (conflict)
	if _, exists := currentState[cellOp.ID]; exists {
		result.Valid = false
		result.Reason = "cell_already_exists"
		result.ConflictDetected = true
		result.CorrectionNeeded = true
		return result
	}

	// Validate cell data
	if cellOp.Data == nil {
		result.Valid = false
		result.Reason = "add_requires_cell_data"
		return result
	}

	if err := s.validateCellData(cellOp.Data); err != nil {
		result.Valid = false
		result.Reason = "invalid_cell_data"
		return result
	}

	// Apply the add operation to diagram
	converter := NewCellConverter()
	cellItem, err := converter.ConvertCellToUnionItem(*cellOp.Data)
	if err != nil {
		result.Valid = false
		result.Reason = "cell_conversion_failed"
		return result
	}

	diagram.Cells = append(diagram.Cells, cellItem)
	result.StateChanged = true

	// Save changes
	if err := DiagramStore.Update(s.DiagramID, *diagram); err != nil {
		log.Printf("Failed to save diagram after add operation: %v", err)
		result.Valid = false
		result.Reason = "save_failed"
		return result
	}

	return result
}

// validateUpdateOperation validates updating an existing cell
func (s *DiagramSession) validateUpdateOperation(diagram *DfdDiagram, currentState map[string]*Cell, cellOp CellOperation) OperationValidationResult {
	result := OperationValidationResult{Valid: true}

	// Check if cell exists
	existingCell, exists := currentState[cellOp.ID]
	if !exists {
		result.Valid = false
		result.Reason = "update_nonexistent_cell"
		result.ConflictDetected = true
		result.CorrectionNeeded = true
		return result
	}

	// Validate cell data
	if cellOp.Data == nil {
		result.Valid = false
		result.Reason = "update_requires_cell_data"
		return result
	}

	if err := s.validateCellData(cellOp.Data); err != nil {
		result.Valid = false
		result.Reason = "invalid_cell_data"
		return result
	}

	// Check if update actually changes anything
	stateChanged := s.detectCellChanges(existingCell, cellOp.Data)
	if !stateChanged {
		result.StateChanged = false
		return result // Valid but no changes
	}

	// Apply the update operation to diagram
	converter := NewCellConverter()
	found := false
	for i := range diagram.Cells {
		cellItem := &diagram.Cells[i]
		if cell, err := converter.ConvertUnionItemToCell(*cellItem); err == nil {
			if cell.Id.String() == cellOp.ID {
				// Replace with updated cell
				if updatedCellItem, err := converter.ConvertCellToUnionItem(*cellOp.Data); err == nil {
					diagram.Cells[i] = updatedCellItem
					found = true
					break
				}
			}
		}
	}

	if !found {
		result.Valid = false
		result.Reason = "cell_not_found_in_diagram"
		result.ConflictDetected = true
		return result
	}

	result.StateChanged = true

	// Save changes
	if err := DiagramStore.Update(s.DiagramID, *diagram); err != nil {
		log.Printf("Failed to save diagram after update operation: %v", err)
		result.Valid = false
		result.Reason = "save_failed"
		return result
	}

	return result
}

// validateRemoveOperation validates removing a cell
func (s *DiagramSession) validateRemoveOperation(diagram *DfdDiagram, currentState map[string]*Cell, cellOp CellOperation) OperationValidationResult {
	result := OperationValidationResult{Valid: true}

	// Check if cell exists
	if _, exists := currentState[cellOp.ID]; !exists {
		// Removing non-existent cell is idempotent - not an error
		result.StateChanged = false
		return result
	}

	// Apply the remove operation to diagram
	converter := NewCellConverter()
	found := false
	for i, cellItem := range diagram.Cells {
		if cell, err := converter.ConvertUnionItemToCell(cellItem); err == nil {
			if cell.Id.String() == cellOp.ID {
				// Remove by replacing with last element and truncating
				lastIndex := len(diagram.Cells) - 1
				if i != lastIndex {
					diagram.Cells[i] = diagram.Cells[lastIndex]
				}
				diagram.Cells = diagram.Cells[:lastIndex]
				found = true
				break
			}
		}
	}

	result.StateChanged = found

	if found {
		// Save changes
		if err := DiagramStore.Update(s.DiagramID, *diagram); err != nil {
			log.Printf("Failed to save diagram after remove operation: %v", err)
			result.Valid = false
			result.Reason = "save_failed"
			return result
		}
	}

	return result
}

// validateCellData validates cell data structure
func (s *DiagramSession) validateCellData(cell *Cell) error {
	if cell == nil {
		return fmt.Errorf("cell cannot be nil")
	}

	// Validate ID is not zero UUID
	if cell.Id.String() == "00000000-0000-0000-0000-000000000000" {
		return fmt.Errorf("cell ID is required")
	}

	// Validate required fields
	if cell.Shape == "" {
		return fmt.Errorf("cell shape is required")
	}

	// Basic validation - more detailed validation would be done during conversion
	return nil
}

// detectCellChanges compares two cells to determine if there are meaningful changes
func (s *DiagramSession) detectCellChanges(existing, updated *Cell) bool {
	if existing.Id != updated.Id {
		return true
	}
	if existing.Shape != updated.Shape {
		return true
	}

	// Compare visibility
	if (existing.Visible == nil) != (updated.Visible == nil) {
		return true
	}
	if existing.Visible != nil && updated.Visible != nil && *existing.Visible != *updated.Visible {
		return true
	}

	// Compare Z-index
	if (existing.ZIndex == nil) != (updated.ZIndex == nil) {
		return true
	}
	if existing.ZIndex != nil && updated.ZIndex != nil && *existing.ZIndex != *updated.ZIndex {
		return true
	}

	// For more detailed comparison, we'd need to convert to specific types (Node/Edge)
	// and compare their specific properties, but for now we assume any difference
	// in the Cell struct warrants an update
	return false
}

// addToHistory adds an operation to the history for conflict resolution
func (s *DiagramSession) addToHistory(msg DiagramOperationMessage, previousState, currentState map[string]*Cell) {
	if s.OperationHistory == nil {
		return
	}

	s.OperationHistory.mutex.Lock()
	defer s.OperationHistory.mutex.Unlock()

	entry := &HistoryEntry{
		SequenceNumber: *msg.SequenceNumber,
		OperationID:    msg.OperationID,
		UserID:         msg.UserID,
		Timestamp:      time.Now().UTC(),
		Operation:      msg.Operation,
		PreviousState:  previousState,
	}

	// Use the new AddOperation method which handles position tracking and cleanup
	s.OperationHistory.AddOperation(entry)
}

// cleanupOldHistory removes old history entries to stay within limits
func (s *DiagramSession) cleanupOldHistory() {
	// Find oldest entries to remove
	var sequences []uint64
	for seq := range s.OperationHistory.Operations {
		sequences = append(sequences, seq)
	}

	// Sort and remove oldest
	if len(sequences) > s.OperationHistory.MaxEntries {
		// Simple approach: remove entries older than MaxEntries/2 to free up space
		toRemove := len(sequences) - s.OperationHistory.MaxEntries/2
		for i := 0; i < toRemove; i++ {
			delete(s.OperationHistory.Operations, sequences[i])
		}
	}
}

// History utility methods for operation tracking

// GetHistoryEntry retrieves a specific history entry by sequence number
func (s *DiagramSession) GetHistoryEntry(sequenceNumber uint64) (*HistoryEntry, bool) {
	if s.OperationHistory == nil {
		return nil, false
	}

	s.OperationHistory.mutex.RLock()
	defer s.OperationHistory.mutex.RUnlock()

	entry, exists := s.OperationHistory.Operations[sequenceNumber]
	return entry, exists
}

// GetHistoryStats returns statistics about the operation history
func (s *DiagramSession) GetHistoryStats() map[string]interface{} {
	if s.OperationHistory == nil {
		return map[string]interface{}{
			"total_operations":  0,
			"earliest_sequence": 0,
			"latest_sequence":   0,
		}
	}

	s.OperationHistory.mutex.RLock()
	defer s.OperationHistory.mutex.RUnlock()

	stats := map[string]interface{}{
		"total_operations": len(s.OperationHistory.Operations),
	}

	if len(s.OperationHistory.Operations) > 0 {
		var earliest, latest uint64
		first := true
		for seq := range s.OperationHistory.Operations {
			if first || seq < earliest {
				earliest = seq
			}
			if first || seq > latest {
				latest = seq
			}
			first = false
		}
		stats["earliest_sequence"] = earliest
		stats["latest_sequence"] = latest
	} else {
		stats["earliest_sequence"] = 0
		stats["latest_sequence"] = 0
	}

	return stats
}

// GetRecentOperations returns the most recent N operations
func (s *DiagramSession) GetRecentOperations(count int) []*HistoryEntry {
	if s.OperationHistory == nil || count <= 0 {
		return []*HistoryEntry{}
	}

	s.OperationHistory.mutex.RLock()
	defer s.OperationHistory.mutex.RUnlock()

	// Collect all sequence numbers
	var sequences []uint64
	for seq := range s.OperationHistory.Operations {
		sequences = append(sequences, seq)
	}

	// Sort in descending order to get most recent first
	for i := 0; i < len(sequences)-1; i++ {
		for j := i + 1; j < len(sequences); j++ {
			if sequences[i] < sequences[j] {
				sequences[i], sequences[j] = sequences[j], sequences[i]
			}
		}
	}

	// Get the most recent entries up to count
	var results []*HistoryEntry
	limit := count
	if limit > len(sequences) {
		limit = len(sequences)
	}

	for i := 0; i < limit; i++ {
		if entry, exists := s.OperationHistory.Operations[sequences[i]]; exists {
			results = append(results, entry)
		}
	}

	return results
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

		// Process message using enhanced message handler
		c.Session.ProcessMessage(c, message)
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
