package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// getCellID extracts the ID from a union type (Node or Edge)
func getCellID(item *DfdDiagram_Cells_Item) (string, error) {
	if node, err := item.AsNode(); err == nil {
		return node.Id.String(), nil
	}
	if edge, err := item.AsEdge(); err == nil {
		return edge.Id.String(), nil
	}
	return "", fmt.Errorf("cell is neither Node nor Edge")
}

// SessionState represents the lifecycle state of a collaboration session
type SessionState string

const (
	// SessionStateActive means the session is active and accepting connections
	SessionStateActive SessionState = "active"
	// SessionStateTerminating means the session is in the process of terminating
	SessionStateTerminating SessionState = "terminating"
	// SessionStateTerminated means the session has been terminated and should be cleaned up
	SessionStateTerminated SessionState = "terminated"
)

// WebSocketHub maintains active connections and broadcasts messages
type WebSocketHub struct {
	// Registered connections by diagram ID
	Diagrams map[string]*DiagramSession
	// Mutex for thread safety
	mu sync.RWMutex
	// WebSocket logging configuration
	LoggingConfig slogging.WebSocketLoggingConfig
	// Inactivity timeout duration
	InactivityTimeout time.Duration
	// Mutex for diagram update operations (separate from session management)
	updateMutex sync.Mutex
}

// DiagramSession represents a collaborative editing session
type DiagramSession struct {
	// Session ID
	ID string
	// Diagram ID
	DiagramID string
	// Threat Model ID (parent of the diagram)
	ThreatModelID string
	// Session state
	State SessionState
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
	// Session creation timestamp
	CreatedAt time.Time
	// Session termination timestamp (when host disconnected)
	TerminatedAt *time.Time

	// Reference to the hub for cleanup when session terminates
	Hub *WebSocketHub
	// Message router for handling WebSocket messages
	MessageRouter *MessageRouter

	// Enhanced collaboration state
	// Host (user who created the session)
	Host string
	// Current presenter (user whose cursor/selection is broadcast)
	CurrentPresenter string
	// Deny list for removed participants (session-specific)
	DeniedUsers map[string]bool
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
	CurrentState map[string]*DfdDiagram_Cells_Item
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
	PreviousState map[string]*DfdDiagram_Cells_Item
}

// WebSocketClient represents a connected client
type WebSocketClient struct {
	// Hub reference
	Hub *WebSocketHub
	// Diagram session reference
	Session *DiagramSession
	// The websocket connection
	Conn *websocket.Conn
	// User ID from JWT 'sub' claim (immutable identifier)
	UserID string
	// User display name from JWT 'name' claim
	UserName string
	// User email from JWT 'email' claim
	UserEmail string
	// Buffered channel of outbound messages
	Send chan []byte
	// Last activity timestamp
	LastActivity time.Time
	// Flag to indicate the client is closing (prevents send on closed channel)
	closing bool
	// Mutex to protect closing flag
	closingMu sync.RWMutex
}

// toUser converts WebSocketClient user information to a User object for messages
func (c *WebSocketClient) toUser() User {
	return User{
		Id:    c.UserID,
		Name:  c.UserName,
		Email: openapi_types.Email(c.UserEmail),
	}
}

// closeClientChannel safely closes a client's Send channel with proper locking
// to prevent "send on closed channel" panics. This MUST be used instead of
// directly calling close(client.Send) to avoid race conditions.
func (c *WebSocketClient) closeClientChannel() {
	c.closingMu.Lock()
	defer c.closingMu.Unlock()

	// Only close if not already closing/closed
	if !c.closing {
		c.closing = true
		close(c.Send)
	}
}

// getUserByID finds a User in the session by user ID
func (s *DiagramSession) getUserByID(userID string) *User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for client := range s.Clients {
		if client.UserID == userID {
			user := client.toUser()
			return &user
		}
	}
	return nil
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

		// Add environment-configured allowed origins
		if envOrigins := os.Getenv("WEBSOCKET_ALLOWED_ORIGINS"); envOrigins != "" {
			// Split by comma and add each origin
			for _, envOrigin := range strings.Split(envOrigins, ",") {
				envOrigin = strings.TrimSpace(envOrigin)
				if envOrigin != "" {
					allowedOrigins = append(allowedOrigins, envOrigin)
				}
			}
		}

		// Check if origin matches any allowed origins
		for _, allowed := range allowedOrigins {
			if strings.HasPrefix(origin, allowed) {
				return true
			}
		}

		slogging.Get().Warn("Rejected WebSocket connection from origin: %s", origin)
		return false
	},
}

// NewWebSocketHub creates a new WebSocket hub
func NewWebSocketHub(loggingConfig slogging.WebSocketLoggingConfig, inactivityTimeout time.Duration) *WebSocketHub {
	return &WebSocketHub{
		Diagrams:          make(map[string]*DiagramSession),
		LoggingConfig:     loggingConfig,
		InactivityTimeout: inactivityTimeout,
	}
}

// NewWebSocketHubForTests creates a WebSocket hub with default test configuration
func NewWebSocketHubForTests() *WebSocketHub {
	return NewWebSocketHub(slogging.WebSocketLoggingConfig{
		Enabled:        false, // Disable logging in tests by default
		RedactTokens:   true,
		MaxMessageSize: 5 * 1024,
		OnlyDebugLevel: true,
	}, 30*time.Second) // Short timeout for tests
}

// UpdateDiagramResult contains the result of a centralized diagram update
type UpdateDiagramResult struct {
	UpdatedDiagram    DfdDiagram
	PreviousVector    int64
	NewVector         int64
	VectorIncremented bool
}

// UpdateDiagram provides centralized diagram updates with version control and WebSocket notification
// This function:
// 1. Handles all diagram modifications (cells, metadata, properties)
// 2. Auto-increments update_vector when cells[] changes or when explicitly requested
// 3. Notifies WebSocket sessions when updates come from REST API
// 4. Serves as single source of truth for all diagram modifications
// 5. Provides thread-safe updates with proper locking
func (h *WebSocketHub) UpdateDiagram(diagramID string, updateFunc func(DfdDiagram) (DfdDiagram, bool, error), updateSource string, excludeUserID string) (*UpdateDiagramResult, error) {
	// Use dedicated update mutex to prevent race conditions on update_vector
	h.updateMutex.Lock()
	defer h.updateMutex.Unlock()

	// Get the current diagram
	currentDiagram, err := DiagramStore.Get(diagramID)
	if err != nil {
		return nil, fmt.Errorf("failed to get diagram %s: %w", diagramID, err)
	}

	// Store previous update vector
	previousVector := int64(0)
	if currentDiagram.UpdateVector != nil {
		previousVector = *currentDiagram.UpdateVector
	}

	// Apply the update function
	updatedDiagram, shouldIncrementVector, err := updateFunc(currentDiagram)
	if err != nil {
		return nil, fmt.Errorf("update function failed for diagram %s: %w", diagramID, err)
	}

	// Increment update vector if requested
	newVector := previousVector
	vectorIncremented := false
	if shouldIncrementVector {
		newVector = previousVector + 1
		updatedDiagram.UpdateVector = &newVector
		vectorIncremented = true
	} else {
		// Preserve existing update vector even if not incrementing
		updatedDiagram.UpdateVector = &previousVector
	}

	// Handle image.update_vector logic: if image.svg is provided but image.update_vector is not,
	// then set image.update_vector to the current BaseDiagram.update_vector
	if updatedDiagram.Image != nil && updatedDiagram.Image.Svg != nil && updatedDiagram.Image.UpdateVector == nil {
		// Use the current diagram's update_vector (after potential increment)
		currentUpdateVector := newVector
		if !vectorIncremented && updatedDiagram.UpdateVector != nil {
			currentUpdateVector = *updatedDiagram.UpdateVector
		}
		updatedDiagram.Image.UpdateVector = &currentUpdateVector
	}

	// Update timestamps (pass pointer for WithTimestamps interface)
	updatedDiagram = *UpdateTimestamps(&updatedDiagram, false)

	// Save to database
	if err := DiagramStore.Update(diagramID, updatedDiagram); err != nil {
		return nil, fmt.Errorf("failed to update diagram %s: %w", diagramID, err)
	}

	// Notify WebSocket clients if this update came from REST API and vector was incremented
	if updateSource == "rest_api" && vectorIncremented {
		h.notifyWebSocketClientsOfUpdate(diagramID, newVector, excludeUserID)
	}

	return &UpdateDiagramResult{
		UpdatedDiagram:    updatedDiagram,
		PreviousVector:    previousVector,
		NewVector:         newVector,
		VectorIncremented: vectorIncremented,
	}, nil
}

// UpdateDiagramCells provides centralized diagram cell updates (convenience wrapper)
func (h *WebSocketHub) UpdateDiagramCells(diagramID string, newCells []DfdDiagram_Cells_Item, updateSource string, excludeUserID string) (*UpdateDiagramResult, error) {
	updateFunc := func(diagram DfdDiagram) (DfdDiagram, bool, error) {
		// No conversion needed - newCells is already the union type
		diagram.Cells = newCells
		return diagram, true, nil // true = increment update vector for cell changes
	}

	return h.UpdateDiagram(diagramID, updateFunc, updateSource, excludeUserID)
}

// notifyWebSocketClientsOfUpdate sends state correction messages to active WebSocket sessions
// to trigger client resync when diagram is updated via REST API
func (h *WebSocketHub) notifyWebSocketClientsOfUpdate(diagramID string, newUpdateVector int64, excludeUserID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	session, exists := h.Diagrams[diagramID]
	if !exists {
		// No active WebSocket session for this diagram
		return
	}

	// Create state correction message with update_vector
	correctionMsg := StateCorrectionMessage{
		MessageType:  "state_correction",
		UpdateVector: &newUpdateVector,
	}

	// Send correction to all connected clients except the excluded user
	for client := range session.Clients {
		if client.UserID != excludeUserID {
			session.sendToClient(client, correctionMsg)
			slogging.Get().Debug("Sent state correction due to REST API update - diagram: %s, user: %s, update_vector: %d",
				diagramID, client.UserID, newUpdateVector)
		}
	}
}

// buildWebSocketURL constructs the absolute WebSocket URL from request context
func (h *WebSocketHub) buildWebSocketURL(c *gin.Context, threatModelId openapi_types.UUID, diagramID string, sessionID string) string {
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
	url := fmt.Sprintf("%s://%s/threat_models/%s/diagrams/%s/ws", scheme, host, threatModelId.String(), diagramID)

	// Add session ID as query parameter if provided
	if sessionID != "" {
		url = fmt.Sprintf("%s?session_id=%s", url, sessionID)
	}

	return url
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

// HasActiveSession checks if there is an active collaboration session for a diagram
func (h *WebSocketHub) HasActiveSession(diagramID string) bool {
	session := h.GetSession(diagramID)
	if session == nil {
		return false
	}

	session.mu.RLock()
	defer session.mu.RUnlock()

	// Only block if session is in active state
	// Allow operations if session is terminating or terminated
	return session.State == SessionStateActive
}

// CreateSession creates a new collaboration session if none exists, returns error if one already exists
func (h *WebSocketHub) CreateSession(diagramID string, threatModelID string, hostUserID string) (*DiagramSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.Diagrams[diagramID]; ok {
		return nil, fmt.Errorf("collaboration session already exists for diagram %s", diagramID)
	}

	session := &DiagramSession{
		ID:            uuid.New().String(),
		DiagramID:     diagramID,
		ThreatModelID: threatModelID,
		State:         SessionStateActive,
		Clients:       make(map[*WebSocketClient]bool),
		Broadcast:     make(chan []byte, 256),
		Register:      make(chan *WebSocketClient),
		Unregister:    make(chan *WebSocketClient),
		LastActivity:  time.Now().UTC(),
		CreatedAt:     time.Now().UTC(),
		Hub:           h, // Reference to the hub for cleanup
		MessageRouter: NewMessageRouter(),

		// Enhanced collaboration state
		Host:               hostUserID,
		CurrentPresenter:   hostUserID, // Host starts as presenter
		DeniedUsers:        make(map[string]bool),
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

	slogging.Get().Info("Created new session %s for diagram %s (host: %s, threat model: %s)",
		session.ID, diagramID, hostUserID, threatModelID)

	slogging.Get().Debug("Starting session Run() goroutine - Session: %s, Diagram: %s", session.ID, diagramID)
	go session.Run()

	return session, nil
}

// JoinSession joins an existing collaboration session, returns error if none exists
func (h *WebSocketHub) JoinSession(diagramID string, userID string) (*DiagramSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	session, ok := h.Diagrams[diagramID]
	if !ok {
		return nil, fmt.Errorf("no collaboration session exists for diagram %s", diagramID)
	}

	session.LastActivity = time.Now().UTC()
	return session, nil
}

// GetOrCreateSession returns an existing session or creates a new one
func (h *WebSocketHub) GetOrCreateSession(diagramID string, threatModelID string, hostUserID string) *DiagramSession {
	h.mu.Lock()
	defer h.mu.Unlock()

	if session, ok := h.Diagrams[diagramID]; ok {
		session.LastActivity = time.Now().UTC()
		// Update threat model ID if it wasn't set (for backward compatibility)
		if session.ThreatModelID == "" && threatModelID != "" {
			session.ThreatModelID = threatModelID
		}
		// Set host if not already set
		if session.Host == "" && hostUserID != "" {
			session.Host = hostUserID
			session.CurrentPresenter = hostUserID // Host starts as presenter
		}
		slogging.Get().Info("Retrieved existing session %s for diagram %s (host: %s, state: %s)",
			session.ID, diagramID, session.Host, session.State)
		return session
	}

	session := &DiagramSession{
		ID:            uuid.New().String(),
		DiagramID:     diagramID,
		ThreatModelID: threatModelID,
		State:         SessionStateActive,
		Clients:       make(map[*WebSocketClient]bool),
		Broadcast:     make(chan []byte, 256),
		Register:      make(chan *WebSocketClient),
		Unregister:    make(chan *WebSocketClient),
		LastActivity:  time.Now().UTC(),
		CreatedAt:     time.Now().UTC(),
		Hub:           h, // Reference to the hub for cleanup
		MessageRouter: NewMessageRouter(),

		// Enhanced collaboration state
		Host:               hostUserID,
		CurrentPresenter:   hostUserID, // Host starts as presenter
		DeniedUsers:        make(map[string]bool),
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

	slogging.Get().Info("Created new session %s for diagram %s (host: %s, threat model: %s)",
		session.ID, diagramID, hostUserID, threatModelID)

	slogging.Get().Debug("Starting session Run() goroutine - Session: %s, Diagram: %s", session.ID, diagramID)
	go session.Run()

	return session
}

// NewOperationHistory creates a new operation history
func NewOperationHistory() *OperationHistory {
	return &OperationHistory{
		Operations:      make(map[uint64]*HistoryEntry),
		CurrentState:    make(map[string]*DfdDiagram_Cells_Item),
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
func (h *OperationHistory) GetUndoOperation() (*HistoryEntry, map[string]*DfdDiagram_Cells_Item, bool) {
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
		participants := make([]Participant, 0, len(session.Clients))

		// Get the threat model to check permissions for participants
		var tm *ThreatModel
		if session.ThreatModelID != "" {
			if threatModel, err := ThreatModelStore.Get(session.ThreatModelID); err == nil {
				tm = &threatModel
			}
		}

		for client := range session.Clients {
			participant := convertClientToParticipant(client, session, tm)
			if participant != nil {
				participants = append(participants, *participant)
			}
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
			Host:          &session.Host,
			Presenter:     &session.CurrentPresenter,
			DiagramId:     diagramUUID,
			ThreatModelId: threatModelId,
			Participants:  participants,
			WebsocketUrl:  fmt.Sprintf("/threat_models/%s/diagrams/%s/ws?session_id=%s", threatModelId.String(), diagramID, session.ID),
		})
		session.mu.RUnlock()
	}

	return sessions
}

// convertClientToParticipant converts a WebSocket client to a Participant
func convertClientToParticipant(client *WebSocketClient, _ *DiagramSession, tm *ThreatModel) *Participant {
	// Get user's session permissions using existing auth system
	var permissions ParticipantPermissions
	if tm != nil {
		// Use email for permission check for backwards compatibility
		permissionCheckID := client.UserEmail
		if permissionCheckID == "" {
			permissionCheckID = client.UserID
		}
		permsPtr := getSessionPermissionsForUser(permissionCheckID, tm)
		if permsPtr == nil {
			// User is unauthorized
			return nil
		}
		permissions = *permsPtr
	} else {
		// Fallback to writer permissions if threat model not available
		permissions = ParticipantPermissionsWriter
	}

	return &Participant{
		User: User{
			Id:    client.UserID,
			Email: openapi_types.Email(client.UserEmail),
			Name:  client.UserName,
		},
		Permissions:  permissions,
		LastActivity: client.LastActivity,
	}
}

// getSessionPermissionsForUser determines session permissions using the existing auth system
func getSessionPermissionsForUser(userName string, tm *ThreatModel) *ParticipantPermissions {
	// Use the existing AccessCheck system to determine permissions
	// Check for writer/resource owner access first (highest permission)
	hasWriterAccess, err := CheckResourceAccess(userName, tm, RoleWriter)
	if err == nil && hasWriterAccess {
		permissions := ParticipantPermissionsWriter
		return &permissions
	}

	// Check for reader access (lowest permission that grants session access)
	hasReaderAccess, err := CheckResourceAccess(userName, tm, RoleReader)
	if err == nil && hasReaderAccess {
		permissions := ParticipantPermissionsReader
		return &permissions
	}

	// No access
	return nil
}

// buildCollaborationSessionFromDiagramSession creates a CollaborationSession struct from a DiagramSession
func (h *WebSocketHub) buildCollaborationSessionFromDiagramSession(c *gin.Context, diagramID string, session *DiagramSession, currentUser string) (*CollaborationSession, error) {

	session.mu.RLock()
	defer session.mu.RUnlock()

	// Convert diagram ID to UUID
	diagramUUID, err := uuid.Parse(diagramID)
	if err != nil {
		slogging.Get().Error("buildCollaborationSessionFromDiagramSession: Failed to parse diagram ID '%s': %v", diagramID, err)
		return nil, fmt.Errorf("invalid diagram ID: %w", err)
	}

	// Parse threat model ID
	threatModelId, err := uuid.Parse(session.ThreatModelID)
	if err != nil {
		return nil, fmt.Errorf("invalid threat model ID: %w", err)
	}

	// Get the threat model to check access and extract name
	tm, err := ThreatModelStore.Get(session.ThreatModelID)
	if err != nil {
		return nil, fmt.Errorf("threat model not found: %w", err)
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

	// Convert clients to participants with proper permissions
	participants := make([]Participant, 0, len(session.Clients))

	// Track users already processed to avoid duplicates
	processedUsers := make(map[string]bool)

	// First, add users from active WebSocket clients
	for client := range session.Clients {
		participant := convertClientToParticipant(client, session, &tm)
		if participant != nil {
			participants = append(participants, *participant)
			processedUsers[client.UserID] = true
		}
	}

	// Finally, ensure current user is included if not already processed
	if currentUser != "" && !processedUsers[currentUser] {
		// Create a temporary client for the current user
		tempClient := &WebSocketClient{
			UserID:    currentUser,
			UserEmail: currentUser, // Using currentUser as email for backwards compatibility
			UserName:  currentUser, // Default to user ID if name not available
		}
		participant := convertClientToParticipant(tempClient, session, &tm)
		if participant == nil {
			// Current user is unauthorized
			return nil, fmt.Errorf("user %s is not authorized to access this threat model", currentUser)
		}
		// Set the last activity to now since this is a new participant
		participant.LastActivity = time.Now().UTC()
		participants = append(participants, *participant)
	}

	// Convert session ID to UUID
	sessionUUID, err := uuid.Parse(session.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid session ID: %w", err)
	}

	collaborationSession := &CollaborationSession{
		SessionId:       &sessionUUID,
		Host:            &session.Host,
		Presenter:       &session.CurrentPresenter,
		DiagramId:       diagramUUID,
		DiagramName:     diagramName,
		ThreatModelId:   threatModelId,
		ThreatModelName: tm.Name,
		Participants:    participants,
		WebsocketUrl:    h.buildWebSocketURL(c, threatModelId, diagramID, session.ID),
	}

	return collaborationSession, nil
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
		participants := make([]Participant, 0, len(session.Clients))

		for client := range session.Clients {
			participant := convertClientToParticipant(client, session, &tm)
			if participant != nil {
				participants = append(participants, *participant)
			}
		}

		// Convert session ID to UUID
		sessionUUID, err := uuid.Parse(session.ID)
		if err != nil {
			session.mu.RUnlock()
			continue
		}

		sessions = append(sessions, CollaborationSession{
			SessionId:       &sessionUUID,
			Host:            &session.Host,
			Presenter:       &session.CurrentPresenter,
			DiagramId:       diagramUUID,
			DiagramName:     diagramName,
			ThreatModelId:   threatModelId,
			ThreatModelName: tm.Name,
			Participants:    participants,
			WebsocketUrl:    h.buildWebSocketURL(c, threatModelId, diagramID, session.ID),
		})
		session.mu.RUnlock()
	}

	return sessions
}

// getThreatModelIdForDiagram finds the threat model that contains a specific diagram
func (h *WebSocketHub) getThreatModelIdForDiagram(diagramID string) openapi_types.UUID {
	// Safety check: if ThreatModelStore is not initialized (e.g., in tests), return empty UUID
	if ThreatModelStore == nil {
		slogging.Get().Debug(" ThreatModelStore is nil, denying WebSocket access for diagram %s", diagramID)
		return openapi_types.UUID{}
	}

	// Search through all threat models to find the one containing this diagram
	// Use a large limit to get all threat models (in practice we should have pagination)
	threatModels := ThreatModelStore.List(0, 1000, nil)
	slogging.Get().Debug(" Searching for diagram %s in %d threat models", diagramID, len(threatModels))

	for _, tm := range threatModels {
		if tm.Diagrams != nil {
			slogging.Get().Debug(" Checking threat model %s with %d diagrams", tm.Id.String(), len(*tm.Diagrams))
			for _, diagramUnion := range *tm.Diagrams {
				// Convert union type to DfdDiagram to get the ID
				if dfdDiag, err := diagramUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil {
					slogging.Get().Debug(" Found diagram %s in threat model %s", dfdDiag.Id.String(), tm.Id.String())
					if dfdDiag.Id.String() == diagramID {
						slogging.Get().Debug(" Match found! Diagram %s belongs to threat model %s", diagramID, tm.Id.String())
						return *tm.Id
					}
				} else {
					slogging.Get().Debug(" Failed to convert diagram union to DfdDiagram: %v", err)
				}
			}
		} else {
			slogging.Get().Debug(" Threat model %s has nil Diagrams", tm.Id.String())
		}
	}

	slogging.Get().Debug(" Diagram %s not found in any threat model", diagramID)
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
	session, ok := h.Diagrams[diagramID]
	if !ok {
		h.mu.Unlock()
		return
	}
	// Remove session from hub immediately to prevent new connections
	delete(h.Diagrams, diagramID)
	h.mu.Unlock()

	// Handle immediate session termination
	session.mu.Lock()
	session.State = SessionStateTerminated
	now := time.Now().UTC()
	session.TerminatedAt = &now

	// Collect clients to close while holding the lock
	clientsToClose := make([]*WebSocketClient, 0, len(session.Clients))
	for client := range session.Clients {
		clientsToClose = append(clientsToClose, client)
	}
	// Clear the clients map while still holding the lock
	session.Clients = make(map[*WebSocketClient]bool)
	session.mu.Unlock()

	// Close client channels outside the lock to prevent deadlocks
	// and after clearing the clients map to prevent new sends
	for _, client := range clientsToClose {
		// Use helper function for thread-safe channel closure
		client.closeClientChannel()
		slogging.Get().Debug("Closed connection for participant %s due to immediate session termination", client.UserID)
	}

	slogging.Get().Info("Session %s terminated immediately by host %s", session.ID, session.Host)
}

// CleanupInactiveSessions removes sessions that are inactive or empty with grace period
func (h *WebSocketHub) CleanupInactiveSessions() {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now().UTC()
	inactivityTimeout := now.Add(-h.InactivityTimeout)

	for diagramID, session := range h.Diagrams {
		session.mu.RLock()
		lastActivity := session.LastActivity
		clientCount := len(session.Clients)
		sessionState := session.State
		session.mu.RUnlock()

		shouldCleanup := false
		cleanupReason := ""

		// Check if session is terminated - immediate cleanup
		if sessionState == SessionStateTerminated {
			shouldCleanup = true
			cleanupReason = "terminated session (immediate cleanup)"
		} else if lastActivity.Before(inactivityTimeout) {
			// Check for inactivity timeout
			shouldCleanup = true
			cleanupReason = fmt.Sprintf("inactive for %v", h.InactivityTimeout)
		} else if clientCount == 0 {
			// Check for empty sessions - immediate cleanup
			shouldCleanup = true
			cleanupReason = "empty session (immediate cleanup)"
		}

		if shouldCleanup {
			// Close session
			for client := range session.Clients {
				client.closeClientChannel()
			}
			delete(h.Diagrams, diagramID)
			slogging.Get().Info("Cleaned up session %s for diagram %s: %s", session.ID, diagramID, cleanupReason)

			// Record session cleanup
			if GlobalPerformanceMonitor != nil {
				GlobalPerformanceMonitor.RecordSessionEnd(session.ID)
			}
		}
	}
}

// CleanupEmptySessions performs immediate cleanup of empty sessions
func (h *WebSocketHub) CleanupEmptySessions() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for diagramID, session := range h.Diagrams {
		session.mu.RLock()
		clientCount := len(session.Clients)
		session.mu.RUnlock()

		// Immediate cleanup of empty sessions
		if clientCount == 0 {
			// Close session
			for client := range session.Clients {
				client.closeClientChannel()
			}
			delete(h.Diagrams, diagramID)
			slogging.Get().Info("Cleaned up empty session %s for diagram %s (triggered by user departure)", session.ID, diagramID)

			// Record session cleanup
			if GlobalPerformanceMonitor != nil {
				GlobalPerformanceMonitor.RecordSessionEnd(session.ID)
			}
		}
	}
}

// CleanupAllSessions removes all active sessions (used at server startup)
func (h *WebSocketHub) CleanupAllSessions() {
	h.mu.Lock()
	defer h.mu.Unlock()

	sessionCount := len(h.Diagrams)
	if sessionCount == 0 {
		slogging.Get().Info("No active collaboration sessions to clean up")
		return
	}

	slogging.Get().Info("Cleaning up %d existing collaboration sessions from previous server run", sessionCount)

	// Close all sessions
	for diagramID, session := range h.Diagrams {
		// Close all client connections
		for client := range session.Clients {
			client.closeClientChannel()
		}
		slogging.Get().Debug("Cleaned up collaboration session for diagram %s", diagramID)
	}

	// Clear the sessions map
	h.Diagrams = make(map[string]*DiagramSession)
	slogging.Get().Info("All collaboration sessions cleaned up successfully")
}

// StartCleanupTimer starts a periodic cleanup timer
func (h *WebSocketHub) StartCleanupTimer(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second) // Run every 15 seconds to catch empty sessions
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
	// Add panic recovery to prevent session goroutine from crashing
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in DiagramSession.Run() - Session: %s, Diagram: %s, Error: %v, Stack: %s",
				s.ID, s.DiagramID, r, debug.Stack())
		}

		// Remove session from hub when Run exits (either normally or due to panic)
		if s.Hub != nil {
			s.Hub.mu.Lock()
			delete(s.Hub.Diagrams, s.DiagramID)
			s.Hub.mu.Unlock()
			slogging.Get().Info("Session %s removed from hub after termination", s.ID)
		}
	}()

	slogging.Get().Debug("DiagramSession.Run() started - Session: %s, Diagram: %s, Host: %s", s.ID, s.DiagramID, s.Host)

	for {
		select {
		case client := <-s.Register:
			slogging.Get().Debug("Processing Register request in session Run() - Session: %s, User: %s", s.ID, client.UserID)

			// Check if user is on the deny list
			s.mu.RLock()
			isDenied := s.DeniedUsers[client.UserID]
			s.mu.RUnlock()

			if isDenied {
				slogging.Get().Info("Denied user %s attempted to join session %s", client.UserID, s.ID)

				// Send denial message and close connection
				errorMsg := ErrorMessage{
					MessageType: "error",
					Error:       "access_denied",
					Message:     "You have been removed from this collaboration session and cannot rejoin",
					Timestamp:   time.Now().UTC(),
				}

				// Try to send the message first
				if data, err := json.Marshal(errorMsg); err == nil {
					if writeErr := client.Conn.WriteMessage(websocket.TextMessage, data); writeErr != nil {
						slogging.Get().Debug("Failed to send denial message to user %s: %v", client.UserID, writeErr)
					}
				}

				// Close the connection
				if closeErr := client.Conn.Close(); closeErr != nil {
					slogging.Get().Debug("Failed to close connection for denied user %s: %v", client.UserID, closeErr)
				}
				continue
			}

			s.mu.Lock()
			s.Clients[client] = true
			s.LastActivity = time.Now().UTC()
			s.mu.Unlock()
			slogging.Get().Debug("Client registered successfully in session - Session: %s, User: %s, Total clients: %d", s.ID, client.UserID, len(s.Clients))

			// Send initial state to the new client
			// First, send diagram state sync to ensure client has current server state
			diagram, err := DiagramStore.Get(s.DiagramID)
			if err != nil {
				slogging.Get().Error("Failed to get diagram for initial state sync - Session: %s, User: %s, DiagramID: %s, Error: %v",
					s.ID, client.UserID, s.DiagramID, err)
				// Continue anyway - client can request resync later via resync_request message
			} else {
				updateVectorValue := int64(0)
				if diagram.UpdateVector != nil {
					updateVectorValue = *diagram.UpdateVector
				}

				stateSyncMsg := DiagramStateSyncMessage{
					MessageType:  MessageTypeDiagramStateSync,
					DiagramID:    s.DiagramID,
					UpdateVector: diagram.UpdateVector,
					Cells:        diagram.Cells,
				}

				if msgBytes, err := json.Marshal(stateSyncMsg); err == nil {
					select {
					case client.Send <- msgBytes:
						slogging.Get().Info("Sent initial diagram state sync to client - Session: %s, User: %s, UpdateVector: %d, Cells: %d",
							s.ID, client.UserID, updateVectorValue, len(diagram.Cells))
					default:
						slogging.Get().Error("Failed to queue initial state sync for client - Session: %s, User: %s (channel full)",
							s.ID, client.UserID)
					}
				} else {
					slogging.Get().Error("Failed to marshal diagram state sync message - Session: %s, User: %s, Error: %v",
						s.ID, client.UserID, err)
				}
			}

			// Second, send current presenter info
			s.mu.RLock()
			currentPresenter := s.CurrentPresenter
			s.mu.RUnlock()

			if currentPresenter != "" {
				slogging.Get().Debug("Sending current presenter message to new client %s - presenter: %s", client.UserID, currentPresenter)
				presenterMsg := CurrentPresenterMessage{
					MessageType: MessageTypeCurrentPresenter,
					CurrentPresenter: func() User {
						if u := s.getUserByID(currentPresenter); u != nil {
							return *u
						}
						return User{}
					}(),
				}
				if msgBytes, err := json.Marshal(presenterMsg); err == nil {
					select {
					case client.Send <- msgBytes:
						slogging.Get().Debug("Successfully queued current presenter message for client %s", client.UserID)
					default:
						slogging.Get().Error("Failed to send current presenter to new client")
					}
				}
			} else {
				slogging.Get().Debug("No current presenter set for session %s", s.ID)
			}

			// Send participant list to the new client
			slogging.Get().Debug("Sending participants update to new client %s", client.UserID)
			s.sendParticipantsUpdateToClient(client)

			// Notify other clients that someone joined
			msg := ParticipantJoinedMessage{
				MessageType: MessageTypeParticipantJoined,
				JoinedUser: User{
					Id:    client.UserID,
					Name:  client.UserName,
					Email: openapi_types.Email(client.UserEmail),
				},
				Timestamp: time.Now().UTC(),
			}
			s.broadcastToOthers(client, msg)

			// Broadcast updated participant list to all clients
			s.broadcastParticipantsUpdate()

		case client := <-s.Unregister:
			s.mu.Lock()
			if _, ok := s.Clients[client]; ok {
				delete(s.Clients, client)
				client.closeClientChannel()
				s.LastActivity = time.Now().UTC()

				// Check if the leaving client was the current presenter
				wasPresenter := client.UserEmail == s.CurrentPresenter

				// Check if the leaving client was the host
				wasHost := client.UserEmail == s.Host

				s.mu.Unlock()

				// Handle presenter leaving session
				if wasPresenter {
					s.handlePresenterDisconnection(client.UserID)
				}

				// Handle host leaving session
				if wasHost {
					s.handleHostDisconnection(client.UserID)
					return // Exit the session run loop to terminate the session
				}
			} else {
				s.mu.Unlock()
			}

			// Notify other clients that someone left
			msg := ParticipantLeftMessage{
				MessageType: MessageTypeParticipantLeft,
				DepartedUser: User{
					Id:    client.UserID,
					Name:  client.UserName,
					Email: openapi_types.Email(client.UserEmail),
				},
				Timestamp: time.Now().UTC(),
			}
			if msgBytes, err := MarshalAsyncMessage(msg); err == nil {
				select {
				case s.Broadcast <- msgBytes:
					// Successfully queued
				default:
					slogging.Get().Error("Failed to broadcast participant left message: broadcast channel full")
				}
			} else {
				slogging.Get().Error("Failed to marshal participant left message: %v", err)
			}

			// Broadcast updated participant list to all remaining clients
			s.broadcastParticipantsUpdate()

			// Trigger cleanup of empty sessions after user departure
			if s.Hub != nil {
				go s.Hub.CleanupEmptySessions()
			}

		case message := <-s.Broadcast:
			// Log outgoing broadcast message (sanitized to remove newlines)
			sanitizedBroadcast := slogging.SanitizeLogMessage(string(message))
			slogging.Get().Debug("[wsmsg] Broadcasting message - session_id=%s message_size=%d raw_message=%s client_count=%d",
				s.ID, len(message), sanitizedBroadcast, len(s.Clients))
			s.mu.Lock()
			s.LastActivity = time.Now().UTC()
			clientCount := 0
			// Send to all clients
			for client := range s.Clients {
				select {
				case client.Send <- message:
					clientCount++
				default:
					client.closeClientChannel()
					delete(s.Clients, client)
				}
			}
			s.mu.Unlock()
		}
	}
}

// validateWebSocketRequest validates the basic request parameters and returns user info
func (h *WebSocketHub) validateWebSocketRequest(c *gin.Context) (threatModelID, diagramID, userIDStr string, err error) {
	// Get threat model ID and diagram ID from path
	threatModelID = c.Param("threat_model_id")
	diagramID = c.Param("diagram_id")

	// Validate threat model ID format
	if _, err := uuid.Parse(threatModelID); err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_id",
			ErrorDescription: "Invalid threat model ID format, must be a valid UUID",
		})
		return "", "", "", fmt.Errorf("invalid threat model ID")
	}

	// Validate diagram ID format
	if _, err := uuid.Parse(diagramID); err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_id",
			ErrorDescription: "Invalid diagram ID format, must be a valid UUID",
		})
		return "", "", "", fmt.Errorf("invalid diagram ID")
	}

	// Get user ID from context
	userID, exists := c.Get("user_id")
	if !exists {
		// Fallback to legacy userName for backwards compatibility
		if userNameLegacy, exists := c.Get("user_name_legacy"); exists {
			userID = userNameLegacy
		} else {
			c.Header("WWW-Authenticate", "Bearer")
			c.JSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "User not authenticated",
			})
			return "", "", "", fmt.Errorf("user not authenticated")
		}
	}

	userIDStr, ok := userID.(string)
	if !ok || userIDStr == "" {
		c.Header("WWW-Authenticate", "Bearer")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "Invalid user authentication",
		})
		return "", "", "", fmt.Errorf("invalid user authentication")
	}

	return threatModelID, diagramID, userIDStr, nil
}

// HandleWS handles WebSocket connections
func (h *WebSocketHub) HandleWS(c *gin.Context) {
	// Validate request and get parameters
	threatModelID, diagramID, _, err := h.validateWebSocketRequest(c)
	if err != nil {
		return
	}

	// Extract user information from context
	userExtractor := &UserInfoExtractor{}
	userInfo, err := userExtractor.ExtractUserInfo(c)
	if err != nil {
		slogging.Get().Error("Failed to extract user info: %v", err)
		return
	}

	// Upgrade to WebSocket first
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slogging.Get().Info("Failed to upgrade connection: %v", err)
		return
	}

	// Initialize connection manager and validator
	connManager := &WebSocketConnectionManager{}
	validator := &SessionValidator{}

	// Validate user has access to the diagram
	if err := validator.ValidateSessionAccess(h, userInfo, threatModelID, diagramID); err != nil {
		connManager.SendErrorAndClose(conn, "unauthorized", "You don't have sufficient permissions to collaborate on this diagram")
		slogging.Get().Info("Disconnected user %s - no permissions for diagram %s", userInfo.UserID, diagramID)
		return
	}

	// Get optional session_id from query parameters
	sessionID := c.Query("session_id")

	// Get or create session - use email for host tracking
	session := h.GetOrCreateSession(diagramID, threatModelID, userInfo.UserEmail)

	// Log session state
	slogging.Get().Info("WebSocket connection attempt - User: %s, Diagram: %s, Session: %s, Provided SessionID: %s",
		userInfo.UserID, diagramID, session.ID, sessionID)

	// Validate session state
	if err := validator.ValidateSessionState(session); err != nil {
		// Session is terminated, immediately close the connection without sending termination messages
		if err := conn.Close(); err != nil {
			slogging.Get().Debug("Failed to close connection: %v", err)
		}
		slogging.Get().Info("Rejected connection from user %s - %v", userInfo.UserID, err)
		return
	}

	// Validate session ID if provided
	if err := validator.ValidateSessionID(session, sessionID); err != nil {
		slogging.Get().Info("Session ID validation failed for user %s - %v (client may be disconnecting)", userInfo.UserID, err)
		connManager.SendCloseAndClose(conn, websocket.CloseNormalClosure, "Session ID mismatch")
		return
	}

	// Create client
	client := &WebSocketClient{
		Hub:          h,
		Session:      session,
		Conn:         conn,
		UserID:       userInfo.UserID,
		UserName:     userInfo.UserName,
		UserEmail:    userInfo.UserEmail,
		Send:         make(chan []byte, 256),
		LastActivity: time.Now().UTC(),
	}

	// Log before attempting to register client
	slogging.Get().Debug("Attempting to register WebSocket client - User: %s, Session: %s, Diagram: %s", userInfo.UserID, session.ID, diagramID)

	// Register client with timeout to prevent blocking
	if err := connManager.RegisterClientWithTimeout(session, client, 5*time.Second); err != nil {
		slogging.Get().Error("Failed to register client: %v", err)
		if err := conn.Close(); err != nil {
			slogging.Get().Debug("Failed to close connection after timeout: %v", err)
		}
		return
	}

	// Log WebSocket connection
	slogging.LogWebSocketConnection("CONNECTION_ESTABLISHED", session.ID, userInfo.UserID, diagramID, h.LoggingConfig)

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
	Component *DfdDiagram_Cells_Item `json:"component,omitempty"` // DEPRECATED
	// Properties to update (for update)
	Properties map[string]interface{} `json:"properties,omitempty"`
}

// validateCell performs basic cell validation
func validateCell(cell *DfdDiagram_Cells_Item) error {
	if cell == nil {
		return fmt.Errorf("cell cannot be nil")
	}

	// Extract ID from union type and validate
	cellID, err := getCellID(cell)
	if err != nil {
		return fmt.Errorf("invalid cell type: %w", err)
	}

	if cellID == "00000000-0000-0000-0000-000000000000" {
		return fmt.Errorf("cell ID is required")
	}

	return nil
}

// ProcessMessage handles enhanced message types for collaborative editing
func (s *DiagramSession) ProcessMessage(client *WebSocketClient, message []byte) {
	if err := s.MessageRouter.RouteMessage(s, client, message); err != nil {
		slogging.Get().Error("Failed to route WebSocket message - Session: %s, User: %s, Error: %v",
			s.ID, client.UserID, err)
	}
}

// processPresenterRequest handles presenter mode requests
func (s *DiagramSession) processPresenterRequest(client *WebSocketClient, message []byte) {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in processPresenterRequest - Session: %s, User: %s, Error: %v, Stack: %s",
				s.ID, client.UserID, r, debug.Stack())
		}
	}()

	slogging.Get().Debug("Processing presenter request - Session: %s, User: %s", s.ID, client.UserID)

	var msg PresenterRequestMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		slogging.Get().Error("Failed to parse presenter request - Session: %s, User: %s, Error: %v",
			s.ID, client.UserID, err)
		return
	}

	// PresenterRequestMessage has no user field - client is already authenticated

	s.mu.RLock()
	currentPresenter := s.CurrentPresenter
	host := s.Host
	s.mu.RUnlock()

	// If user is already the presenter, ignore
	if client.UserID == currentPresenter {
		slogging.Get().Info("User %s is already the presenter", client.UserID)
		return
	}

	// If user is the host, automatically grant presenter mode
	if client.UserEmail == host {
		s.mu.Lock()
		s.CurrentPresenter = client.UserEmail
		s.mu.Unlock()

		// Broadcast new presenter to all clients
		broadcastMsg := CurrentPresenterMessage{
			MessageType:      MessageTypeCurrentPresenter,
			CurrentPresenter: client.toUser(),
		}
		s.broadcastMessage(broadcastMsg)
		slogging.Get().Info("Host %s became presenter in session %s", client.UserID, s.ID)

		// Also broadcast updated participant list since presenter has changed
		s.broadcastParticipantsUpdate()
		return
	}

	// For non-hosts, notify the host of the presenter request
	// The host can then use change_presenter to grant or send presenter_denied to deny
	hostClient := s.findClientByUserEmail(host)
	if hostClient != nil {
		// Forward the request to the host for approval
		s.sendToClient(hostClient, msg)
		slogging.Get().Info("Forwarded presenter request from %s to host %s in session %s", client.UserID, host, s.ID)
	} else {
		slogging.Get().Info("Host %s not connected, cannot process presenter request from %s", host, client.UserID)

		// Send denial to requester since host is not available
		deniedMsg := PresenterDeniedMessage{
			MessageType: MessageTypePresenterDenied,
			CurrentPresenter: User{
				Id:    "system",
				Email: "system@tmi",
				Name:  "System",
			},
		}
		s.sendToClient(client, deniedMsg)
	}
}

// processChangePresenter handles host changing presenter
func (s *DiagramSession) processChangePresenter(client *WebSocketClient, message []byte) {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in processChangePresenter - Session: %s, User: %s, Error: %v, Stack: %s",
				s.ID, client.UserID, r, debug.Stack())
		}
	}()

	slogging.Get().Debug("Processing change presenter request - Session: %s, User: %s", s.ID, client.UserID)

	var msg ChangePresenterMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		slogging.Get().Error("Failed to parse change presenter request - Session: %s, User: %s, Error: %v",
			s.ID, client.UserID, err)
		return
	}

	// Validate user identity - detect and block spoofing attempts
	if !s.validateAndEnforceIdentity(client, msg.InitiatingUser, "change_presenter") {
		// Client has been removed and blocked for spoofing
		return
	}

	// Only host can change presenter
	s.mu.RLock()
	host := s.Host
	s.mu.RUnlock()

	if client.UserEmail != host {
		slogging.Get().Info("Non-host attempted to change presenter: %s", client.UserEmail)
		return
	}

	// Change presenter
	s.mu.Lock()
	s.CurrentPresenter = msg.NewPresenter.Id
	s.mu.Unlock()

	// Broadcast new presenter to all clients
	broadcastMsg := CurrentPresenterMessage{
		MessageType:      MessageTypeCurrentPresenter,
		CurrentPresenter: msg.NewPresenter,
	}
	s.broadcastMessage(broadcastMsg)
	slogging.Get().Info("Host %s changed presenter to %s in session %s", client.UserID, msg.NewPresenter.Id, s.ID)

	// Also broadcast updated participant list since presenter has changed
	s.broadcastParticipantsUpdate()
}

// processRemoveParticipant handles host removing a participant from the session
func (s *DiagramSession) processRemoveParticipant(client *WebSocketClient, message []byte) {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in processRemoveParticipant - Session: %s, User: %s, Error: %v, Stack: %s",
				s.ID, client.UserID, r, debug.Stack())
		}
	}()

	slogging.Get().Debug("Processing remove participant request - Session: %s, User: %s", s.ID, client.UserID)

	var msg RemoveParticipantMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		slogging.Get().Error("Failed to parse remove participant request - Session: %s, User: %s, Error: %v",
			s.ID, client.UserID, err)
		s.sendErrorMessage(client, "invalid_message", "Failed to parse remove participant request")
		return
	}

	// Validate user identity - detect and block spoofing attempts

	// Only host can remove participants
	s.mu.RLock()
	host := s.Host
	s.mu.RUnlock()

	if client.UserEmail != host {
		slogging.Get().Info("Non-host attempted to remove participant: %s tried to remove %s", client.UserEmail, msg.RemovedUser.Id)
		s.sendErrorMessage(client, "unauthorized", "Only the host can remove participants from the session")
		return
	}

	// Cannot remove yourself
	if msg.RemovedUser.Id == client.UserEmail {
		slogging.Get().Info("Host %s attempted to remove themselves from session %s", client.UserEmail, s.ID)
		s.sendErrorMessage(client, "invalid_request", "Host cannot remove themselves from the session")
		return
	}

	// Find the target client to disconnect
	var targetClient *WebSocketClient
	s.mu.RLock()
	for c := range s.Clients {
		if c.UserID == msg.RemovedUser.Id {
			targetClient = c
			break
		}
	}
	s.mu.RUnlock()

	// Add user to deny list (even if not currently connected)
	s.mu.Lock()
	s.DeniedUsers[msg.RemovedUser.Id] = true
	s.mu.Unlock()

	slogging.Get().Info("Host %s removed participant %s from session %s", client.UserID, msg.RemovedUser.Id, s.ID)

	// If the participant is currently connected, disconnect them
	if targetClient != nil {
		slogging.Get().Info("Disconnecting removed participant %s from session %s", msg.RemovedUser.Id, s.ID)

		// Send notification to the removed participant
		s.sendErrorMessage(targetClient, "removed_from_session", "You have been removed from this collaboration session by the host")

		// Close their connection
		if closeErr := targetClient.Conn.Close(); closeErr != nil {
			slogging.Get().Debug("Failed to close connection for removed participant %s: %v", msg.RemovedUser.Id, closeErr)
		}
	}

	// If the removed user was the current presenter, clear presenter
	s.mu.Lock()
	if s.CurrentPresenter == msg.RemovedUser.Id {
		s.CurrentPresenter = host // Host becomes presenter again (user ID)
		slogging.Get().Info("Removed participant %s was presenter, host %s is now presenter in session %s",
			msg.RemovedUser.Id, host, s.ID)

		// Broadcast new presenter
		broadcastMsg := CurrentPresenterMessage{
			MessageType:      "current_presenter",
			CurrentPresenter: *s.getUserByID(host),
		}
		s.mu.Unlock()
		s.broadcastMessage(broadcastMsg)
		// Also broadcast updated participant list since presenter has changed
		s.broadcastParticipantsUpdate()
	} else {
		s.mu.Unlock()
	}

	// Broadcast updated participant list to all remaining participants
	s.broadcastParticipantsUpdate()
}

// sendErrorMessage sends an error message to a specific client
func (s *DiagramSession) sendErrorMessage(client *WebSocketClient, errorCode, errorMessage string) {
	errorMsg := ErrorMessage{
		MessageType: "error",
		Error:       errorCode,
		Message:     errorMessage,
		Timestamp:   time.Now().UTC(),
	}
	s.sendToClient(client, errorMsg)
}

// processPresenterDenied handles host denying presenter requests
func (s *DiagramSession) processPresenterDenied(client *WebSocketClient, message []byte) {
	var msg PresenterDeniedMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		slogging.Get().Info("Error parsing presenter denied: %v", err)
		return
	}

	// Validate user identity - detect and block spoofing attempts
	// PresenterDeniedMessage validation - client is already authenticated

	// Only host can deny presenter requests
	s.mu.RLock()
	host := s.Host
	s.mu.RUnlock()

	if client.UserEmail != host {
		slogging.Get().Info("Non-host attempted to deny presenter request: %s", client.UserEmail)
		return
	}

	// Find the target user to send the denial
	targetClient := s.findClientByUserID(msg.CurrentPresenter.Id)
	if targetClient != nil {
		s.sendToClient(targetClient, msg)
		slogging.Get().Info("Host %s denied presenter request from %s in session %s", client.UserID, msg.CurrentPresenter.Id, s.ID)
	} else {
		slogging.Get().Info("Target user %s not found for presenter denial in session %s", msg.CurrentPresenter.Id, s.ID)
	}
}

// processPresenterCursor handles cursor position updates
func (s *DiagramSession) processPresenterCursor(client *WebSocketClient, message []byte) {
	var msg PresenterCursorMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		slogging.Get().Info("Error parsing presenter cursor: %v", err)
		return
	}

	// Validate user identity - detect and block spoofing attempts
	// PresenterCursorMessage has no user field - client is already authenticated

	// Only current presenter can send cursor updates
	s.mu.RLock()
	currentPresenter := s.CurrentPresenter
	s.mu.RUnlock()

	if client.UserEmail != currentPresenter {
		slogging.Get().Info("Non-presenter attempted to send cursor: %s", client.UserEmail)
		return
	}

	// Broadcast cursor to all other clients
	s.broadcastToOthers(client, msg)
}

// processPresenterSelection handles selection updates
func (s *DiagramSession) processPresenterSelection(client *WebSocketClient, message []byte) {
	var msg PresenterSelectionMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		slogging.Get().Info("Error parsing presenter selection: %v", err)
		return
	}

	// Validate user identity - detect and block spoofing attempts
	// PresenterSelectionMessage has no user field - client is already authenticated

	// Only current presenter can send selection updates
	s.mu.RLock()
	currentPresenter := s.CurrentPresenter
	s.mu.RUnlock()

	if client.UserEmail != currentPresenter {
		slogging.Get().Info("Non-presenter attempted to send selection: %s", client.UserEmail)
		return
	}

	// Broadcast selection to all other clients
	s.broadcastToOthers(client, msg)
}

// processResyncRequest handles client resync requests
func (s *DiagramSession) processResyncRequest(client *WebSocketClient, message []byte) {
	var msg ResyncRequestMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		slogging.Get().Info("Error parsing resync request: %v", err)
		return
	}

	// User authentication is validated by connection context

	slogging.Get().Info("Client %s requested resync for diagram %s", client.UserID, s.DiagramID)

	// According to the plan, we use REST API for resync for simplicity
	// Send a message telling the client to use the REST endpoint for resync
	resyncResponse := ResyncResponseMessage{
		MessageType:   MessageTypeResyncResponse,
		Method:        "rest_api",
		DiagramID:     s.DiagramID,
		ThreatModelID: s.ThreatModelID,
	}

	s.sendToClient(client, resyncResponse)
	slogging.Get().Info("Sent resync response to %s for diagram %s", client.UserID, s.DiagramID)

	// Record performance metrics
	if GlobalPerformanceMonitor != nil {
		GlobalPerformanceMonitor.RecordResyncRequest(s.ID, client.UserID)
	}
}

// processUndoRequest handles undo requests
func (s *DiagramSession) processUndoRequest(client *WebSocketClient, message []byte) {
	var msg UndoRequestMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		slogging.Get().Info("Error parsing undo request: %v", err)
		return
	}

	// User authentication is validated by connection context

	// Check permission
	// Use email for permission check for backwards compatibility
	permissionCheckID := client.UserEmail
	if permissionCheckID == "" {
		permissionCheckID = client.UserID
	}
	if !s.checkMutationPermission(permissionCheckID) {
		s.sendAuthorizationDenied(client, "", "insufficient_permissions")
		return
	}

	// Check if undo is possible
	if !s.OperationHistory.CanUndo() {
		slogging.Get().Info("No operations to undo for user %s", client.UserID)
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
		slogging.Get().Info("Failed to get undo operation for user %s", client.UserID)
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
		slogging.Get().Info("Failed to apply undo state: %v", err)
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
	slogging.Get().Info("Processed undo request from %s, reverted to sequence %d", client.UserID, entry.SequenceNumber-1)
}

// processRedoRequest handles redo requests
func (s *DiagramSession) processRedoRequest(client *WebSocketClient, message []byte) {
	var msg RedoRequestMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		slogging.Get().Info("Error parsing redo request: %v", err)
		return
	}

	// User authentication is validated by connection context

	// Check permission
	// Use email for permission check for backwards compatibility
	permissionCheckID := client.UserEmail
	if permissionCheckID == "" {
		permissionCheckID = client.UserID
	}
	if !s.checkMutationPermission(permissionCheckID) {
		s.sendAuthorizationDenied(client, "", "insufficient_permissions")
		return
	}

	// Check if redo is possible
	if !s.OperationHistory.CanRedo() {
		slogging.Get().Info("No operations to redo for user %s", client.UserID)
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
		slogging.Get().Info("Failed to get redo operation for user %s", client.UserID)
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
		slogging.Get().Info("Failed to apply redo operation: %v", err)
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
	slogging.Get().Info("Processed redo request from %s, restored to sequence %d", client.UserID, entry.SequenceNumber)
}

// Helper methods

// handlePresenterDisconnection handles when the current presenter leaves the session
func (s *DiagramSession) handlePresenterDisconnection(disconnectedUserID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	slogging.Get().Info("Presenter %s disconnected from session %s, reassigning presenter", disconnectedUserID, s.ID)

	// Reset presenter according to the plan:
	// 1. First try to set presenter back to host
	// 2. If host has also left, set presenter to first remaining user with write permissions

	var newPresenter string

	// Check if host is still connected
	managerConnected := false
	for client := range s.Clients {
		if client.UserEmail == s.Host {
			managerConnected = true
			newPresenter = s.Host
			break
		}
	}

	// If host is not connected, find first user with write permissions
	if !managerConnected && s.ThreatModelID != "" {
		// Get the threat model to check user permissions
		tm, err := ThreatModelStore.Get(s.ThreatModelID)
		if err != nil {
			slogging.Get().Info("Failed to get threat model %s for presenter reassignment: %v", s.ThreatModelID, err)
		} else {
			// Find first connected user with write permissions
			for client := range s.Clients {
				// Use email for permission check for backwards compatibility
				permissionCheckID := client.UserEmail
				if permissionCheckID == "" {
					permissionCheckID = client.UserID
				}
				hasWriteAccess, err := CheckResourceAccess(permissionCheckID, tm, RoleWriter)
				if err == nil && hasWriteAccess {
					newPresenter = client.UserID
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
			CurrentPresenter: *s.getUserByID(newPresenter),
		}

		// Release the lock before broadcasting to avoid deadlock
		s.mu.Unlock()
		s.broadcastMessage(broadcastMsg)
		// Also broadcast updated participant list since presenter has changed
		s.broadcastParticipantsUpdate()
		s.mu.Lock()

		slogging.Get().Info("Set new presenter to %s in session %s after %s disconnected", newPresenter, s.ID, disconnectedUserID)
	} else {
		// No suitable presenter found, clear presenter
		s.CurrentPresenter = ""
		slogging.Get().Info("No suitable presenter found for session %s after %s disconnected", s.ID, disconnectedUserID)
	}
}

// handleHostDisconnection handles when the host leaves
// This method broadcasts session termination messages and prepares for session cleanup
func (s *DiagramSession) handleHostDisconnection(disconnectedHostID string) {
	slogging.Get().Info("Host %s disconnected from session %s, initiating session termination", disconnectedHostID, s.ID)

	// Update session state to terminating
	s.mu.Lock()
	s.State = SessionStateTerminating
	now := time.Now().UTC()
	s.TerminatedAt = &now
	s.mu.Unlock()

	// Send termination message to all remaining participants before closing connections
	s.broadcastSessionTermination("Host has disconnected")

	// Close all remaining client connections immediately
	s.mu.Lock()
	for client := range s.Clients {
		if client.UserID != disconnectedHostID { // Host already disconnected
			// Send WebSocket close message and close connection immediately
			if err := client.Conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Session terminated: host disconnected")); err != nil {
				slogging.Get().Debug("Failed to send close message to participant %s: %v", client.UserID, err)
			}
			// Close the underlying connection immediately
			if err := client.Conn.Close(); err != nil {
				slogging.Get().Debug("Failed to close connection for participant %s: %v", client.UserID, err)
			}
			// Close the Send channel using thread-safe helper
			client.closeClientChannel()
			slogging.Get().Debug("Closed connection for participant %s due to session termination", client.UserID)
		}
	}

	// Clear the clients map
	s.Clients = make(map[*WebSocketClient]bool)
	s.State = SessionStateTerminated
	s.mu.Unlock()

	// Remove the session from the hub immediately
	if s.Hub != nil {
		s.Hub.mu.Lock()
		delete(s.Hub.Diagrams, s.DiagramID)
		s.Hub.mu.Unlock()

		slogging.Get().Info("Session %s removed from hub after host disconnection", s.ID)

		// Record session end
		if GlobalPerformanceMonitor != nil {
			GlobalPerformanceMonitor.RecordSessionEnd(s.ID)
		}
	}

	slogging.Get().Info("Session %s terminated due to host departure", s.ID)
}

// broadcastSessionTermination sends termination messages to all participants
func (s *DiagramSession) broadcastSessionTermination(reason string) {
	s.mu.RLock()
	clients := make([]*WebSocketClient, 0, len(s.Clients))
	for client := range s.Clients {
		clients = append(clients, client)
	}
	s.mu.RUnlock()

	terminationMsg := ErrorMessage{
		MessageType: MessageTypeError,
		Error:       "session_terminated",
		Message:     fmt.Sprintf("Collaboration session has been terminated: %s", reason),
		Timestamp:   time.Now().UTC(),
	}

	msgBytes, err := json.Marshal(terminationMsg)
	if err != nil {
		slogging.Get().Error("Failed to marshal session termination message: %v", err)
		return
	}

	// Send to all clients
	for _, client := range clients {
		select {
		case client.Send <- msgBytes:
			slogging.Get().Debug("Sent session termination message to client %s", client.UserID)
		default:
			slogging.Get().Debug("Failed to send termination message to client %s (channel full)", client.UserID)
		}
	}

	// Give clients a brief moment to receive the termination message
	time.Sleep(100 * time.Millisecond)
}

// findClientByUserID finds a connected client by their user ID
func (s *DiagramSession) findClientByUserID(userID string) *WebSocketClient {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for client := range s.Clients {
		if client.UserID == userID {
			return client
		}
	}
	return nil
}

// validateAndEnforceIdentity validates that a User struct in a message matches the authenticated client.
// If the user data does not match, this is a security incident - the client is attempting to spoof
// their identity. This function logs the incident, removes the malicious client from the session,
// and returns false. Returns true if validation passes.
func (s *DiagramSession) validateAndEnforceIdentity(client *WebSocketClient, messageUser User, messageType string) bool {
	// Check if the user is trying to spoof their identity
	if messageUser.Id != "" && messageUser.Id != client.UserID {
		slogging.Get().Error("SECURITY: User identity spoofing detected - Session: %s, Authenticated User: %s (email: %s), Claimed User: %s (email: %s), Message Type: %s",
			s.ID, client.UserID, client.UserEmail, messageUser.Id, messageUser.Email, messageType)
		s.removeAndBlockClient(client, "Identity spoofing attempt detected")
		return false
	}
	if messageUser.Email != "" && string(messageUser.Email) != client.UserEmail {
		slogging.Get().Error("SECURITY: User identity spoofing detected - Session: %s, Authenticated User: %s (email: %s), Claimed User: %s (email: %s), Message Type: %s",
			s.ID, client.UserID, client.UserEmail, messageUser.Id, messageUser.Email, messageType)
		s.removeAndBlockClient(client, "Identity spoofing attempt detected")
		return false
	}
	if messageUser.Name != "" && messageUser.Name != client.UserName {
		slogging.Get().Error("SECURITY: User identity spoofing detected - Session: %s, Authenticated User: %s (email: %s, name: %s), Claimed Name: %s, Message Type: %s",
			s.ID, client.UserID, client.UserEmail, client.UserName, messageUser.Name, messageType)
		s.removeAndBlockClient(client, "Identity spoofing attempt detected")
		return false
	}
	return true
}

// removeAndBlockClient removes a client from the session and blocks them (same as host ejection)
func (s *DiagramSession) removeAndBlockClient(client *WebSocketClient, reason string) {
	slogging.Get().Info("Removing and blocking client %s from session %s: %s", client.UserID, s.ID, reason)

	// Remove from clients map
	s.mu.Lock()
	delete(s.Clients, client)
	s.mu.Unlock()

	// Send error message to client before disconnecting
	errorMsg := ErrorMessage{
		MessageType: MessageTypeError,
		Error:       "SECURITY_VIOLATION",
		Message:     reason,
		Timestamp:   time.Now(),
	}
	s.sendToClient(client, errorMsg)

	// Close the client connection using thread-safe helper
	client.closeClientChannel()

	// Broadcast participant left message to remaining clients
	leftMsg := ParticipantLeftMessage{
		MessageType: MessageTypeParticipantLeft,
		DepartedUser: User{
			Id:    client.UserID,
			Email: openapi_types.Email(client.UserEmail),
			Name:  client.UserName,
		},
	}
	s.broadcastMessage(leftMsg)

	// Update participants list (defer to avoid issues if ThreatModelStore not initialized)
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Debug("Error broadcasting participants update after removing client: %v", r)
		}
	}()
	s.broadcastParticipantsUpdate()
}

// findClientByUserEmail finds a connected client by their email address
func (s *DiagramSession) findClientByUserEmail(userEmail string) *WebSocketClient {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for client := range s.Clients {
		if client.UserEmail == userEmail {
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
	// TODO: Get IdP and groups from user context when WebSocket supports it
	role := GetUserRole(userID, "", []string{}, tm)
	return role
}

// broadcastParticipantsUpdate sends complete participant list to all clients
func (s *DiagramSession) broadcastParticipantsUpdate() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build participant list
	participants := make([]AsyncParticipant, 0)

	// Get threat model for permissions checking
	var tm *ThreatModel
	if s.ThreatModelID != "" {
		if threatModel, err := ThreatModelStore.Get(s.ThreatModelID); err == nil {
			tm = &threatModel
		}
	}

	// Track processed users to avoid duplicates
	processedUsers := make(map[string]bool)

	// Add active WebSocket clients
	for client := range s.Clients {
		if processedUsers[client.UserID] {
			continue
		}

		var permissions string
		if tm != nil {
			// Use email for permission check for backwards compatibility
			permissionCheckID := client.UserEmail
			if permissionCheckID == "" {
				permissionCheckID = client.UserID
			}
			perms := getSessionPermissionsForUser(permissionCheckID, tm)
			if perms == nil {
				// User is unauthorized, skip them
				slogging.Get().Debug("Skipping user %s (%s) - no permissions found for threat model %s", client.UserID, permissionCheckID, tm.Id)
				continue
			}
			permissions = string(*perms)
		} else {
			// No threat model, default to writer
			permissions = "writer"
		}

		participants = append(participants, AsyncParticipant{
			User: AsyncUser{
				UserID: client.UserID,
				Name:   client.UserName,
				Email:  client.UserEmail,
			},
			Permissions:  permissions,
			LastActivity: client.LastActivity,
		})
		processedUsers[client.UserID] = true
	}

	// Create and send the message
	msg := ParticipantsUpdateMessage{
		MessageType:      MessageTypeParticipantsUpdate,
		Participants:     participants,
		Host:             s.Host,
		CurrentPresenter: s.CurrentPresenter,
	}

	if msgBytes, err := json.Marshal(msg); err == nil {
		// Send to all clients (using broadcast channel)
		select {
		case s.Broadcast <- msgBytes:
		default:
			slogging.Get().Error("Failed to broadcast participants update: broadcast channel full")
		}
	} else {
		slogging.Get().Error("Failed to marshal participants update: %v", err)
	}
}

// sendParticipantsUpdateToClient sends participant list to a specific client
func (s *DiagramSession) sendParticipantsUpdateToClient(client *WebSocketClient) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build participant list
	participants := make([]AsyncParticipant, 0)

	// Get threat model for permissions checking
	var tm *ThreatModel
	if s.ThreatModelID != "" {
		if threatModel, err := ThreatModelStore.Get(s.ThreatModelID); err == nil {
			tm = &threatModel
		}
	}

	// Track processed users to avoid duplicates
	processedUsers := make(map[string]bool)

	// Add active WebSocket clients
	for c := range s.Clients {
		if processedUsers[c.UserName] {
			continue
		}

		var permissions string
		if tm != nil {
			// Use email for permission check for backwards compatibility
			permissionCheckID := c.UserEmail
			if permissionCheckID == "" {
				permissionCheckID = c.UserID
			}
			perms := getSessionPermissionsForUser(permissionCheckID, tm)
			if perms == nil {
				// User is unauthorized, skip them
				slogging.Get().Debug("Skipping user %s (%s) - no permissions found for threat model %s", c.UserID, permissionCheckID, tm.Id)
				continue
			}
			permissions = string(*perms)
		} else {
			// No threat model, default to writer
			permissions = "writer"
		}

		participants = append(participants, AsyncParticipant{
			User: AsyncUser{
				UserID: c.UserID,
				Name:   c.UserName,
				Email:  c.UserEmail,
			},
			Permissions:  permissions,
			LastActivity: c.LastActivity,
		})
		processedUsers[c.UserID] = true
	}

	// Create and send the message
	msg := ParticipantsUpdateMessage{
		MessageType:      MessageTypeParticipantsUpdate,
		Participants:     participants,
		Host:             s.Host,
		CurrentPresenter: s.CurrentPresenter,
	}

	if msgBytes, err := json.Marshal(msg); err == nil {
		// Send to specific client
		slogging.Get().Debug("Sending participants update message to client %s with %d participants", client.UserID, len(participants))
		select {
		case client.Send <- msgBytes:
			slogging.Get().Debug("Successfully queued participants update for client %s", client.UserID)
		default:
			slogging.Get().Error("Failed to send participants update to client %s: send channel full", client.UserID)
		}
	} else {
		slogging.Get().Error("Failed to marshal participants update for client: %v", err)
	}
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
		GlobalPerformanceMonitor.RecordAuthorizationDenied(s.ID, client.UserID, reason)
	}
}

// sendOperationRejected sends an operation_rejected message to the originating client
func (s *DiagramSession) sendOperationRejected(client *WebSocketClient, operationID string, sequenceNumber *uint64, reason string, message string, details *string, affectedCells []string, requiresResync bool) {
	rejectionMsg := OperationRejectedMessage{
		MessageType:    MessageTypeOperationRejected,
		OperationID:    operationID,
		SequenceNumber: sequenceNumber,
		Reason:         reason,
		Message:        message,
		Details:        details,
		AffectedCells:  affectedCells,
		RequiresResync: requiresResync,
		Timestamp:      time.Now().UTC(),
	}

	s.sendToClient(client, rejectionMsg)

	slogging.Get().Info("Sent operation_rejected to %s - Session: %s, OperationID: %s, Reason: %s, RequiresResync: %v, AffectedCells: %v",
		client.UserID, s.ID, operationID, reason, requiresResync, affectedCells)
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

	slogging.Get().Info("Sending state correction to %s for cells %v (reason: %s)", client.UserID, affectedCellIDs, reason)

	// Check user permission level for enhanced messaging
	// Use email for permission check for backwards compatibility
	permissionCheckID := client.UserEmail
	if permissionCheckID == "" {
		permissionCheckID = client.UserID
	}
	userRole := s.getUserRole(permissionCheckID)
	s.sendEnhancedStateCorrection(client, affectedCellIDs, reason, userRole)
}

// sendEnhancedStateCorrection sends enhanced state correction with update vector
func (s *DiagramSession) sendEnhancedStateCorrection(client *WebSocketClient, _ []string, reason string, userRole Role) {

	// Get current diagram state
	diagram, err := DiagramStore.Get(s.DiagramID)
	if err != nil {
		slogging.Get().Info("Error getting diagram for state correction: %v", err)
		return
	}

	// Get current update vector from diagram
	updateVector := int64(0)
	if diagram.UpdateVector != nil {
		updateVector = *diagram.UpdateVector
	}

	// Send the correction with update vector
	correctionMsg := StateCorrectionMessage{
		MessageType:  "state_correction",
		UpdateVector: &updateVector,
	}
	s.sendToClient(client, correctionMsg)

	// Enhanced logging based on reason and user role
	s.logEnhancedStateCorrection(client.UserID, reason, userRole)

	// Track correction frequency for potential sync issues
	s.trackCorrectionEvent(client.UserID, reason)

	// Record performance metrics
	if GlobalPerformanceMonitor != nil {
		GlobalPerformanceMonitor.RecordStateCorrection(s.ID, client.UserID, reason)
	}
}

// logEnhancedStateCorrection provides detailed logging for state corrections
func (s *DiagramSession) logEnhancedStateCorrection(userID string, reason string, userRole Role) {
	roleStr := string(userRole)

	switch reason {
	case "unauthorized_operation":
		slogging.Get().Warn("STATE CORRECTION [UNAUTHORIZED]: User %s (%s role) attempted unauthorized operation - resync required",
			userID, roleStr)

		// Enhanced security logging for unauthorized operations
		if userRole == RoleReader {
			slogging.Get().Error("SECURITY ALERT: Read-only user %s attempted to modify diagram %s", userID, s.DiagramID)
		}

	case "operation_failed":
		slogging.Get().Warn("STATE CORRECTION [OPERATION_FAILED]: User %s (%s role) operation failed - resync required",
			userID, roleStr)

	case "out_of_order_sequence", "duplicate_message", "message_gap":
		slogging.Get().Warn("STATE CORRECTION [SYNC_ISSUE]: User %s (%s role) sync issue (%s) - resync required",
			userID, roleStr, reason)

	default:
		slogging.Get().Warn("STATE CORRECTION [%s]: User %s (%s role) - resync required",
			strings.ToUpper(reason), userID, roleStr)
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
		slogging.Get().Warn("WARNING: User %s has received %d state corrections for reason '%s' - potential sync issue",
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
		slogging.Get().Warn("WARNING: User %s has experienced %d '%s' issues - may need resync",
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
		slogging.Get().Info("Cannot send resync recommendation: client %s not found", userID)
		return
	}

	// Send a resync response message to recommend the client resync via REST API
	resyncResponse := ResyncResponseMessage{
		MessageType:   MessageTypeResyncResponse,
		Method:        "rest_api",
		DiagramID:     s.DiagramID,
		ThreatModelID: s.ThreatModelID,
	}

	s.sendToClient(client, resyncResponse)
	slogging.Get().Info("Sent automatic resync recommendation to %s due to %s issues", userID, issueType)
}

// applyHistoryState applies a historical state to the diagram (for undo)
func (s *DiagramSession) applyHistoryState(state map[string]*DfdDiagram_Cells_Item) error {
	// Convert state map to slice for centralized update
	cells := make([]DfdDiagram_Cells_Item, 0, len(state))
	for _, cell := range state {
		cells = append(cells, *cell)
	}

	// Use centralized update function
	_, err := s.Hub.UpdateDiagramCells(s.DiagramID, cells, "websocket", "")
	if err != nil {
		return fmt.Errorf("failed to update diagram via centralized function: %w", err)
	}

	return nil
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
		slogging.Get().Info("Error marshaling broadcast message: %v", err)
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
			slogging.Get().Info("Failed to send message to client %s", client.UserID)
		}
	}
}

// sendToClient sends a message to a specific client
func (s *DiagramSession) sendToClient(client *WebSocketClient, message interface{}) {
	// Check if client is closing to prevent send on closed channel
	client.closingMu.RLock()
	if client.closing {
		client.closingMu.RUnlock()
		slogging.Get().Debug("Skipping send to client %s - client is closing", client.UserID)
		return
	}
	client.closingMu.RUnlock()

	msgBytes, err := json.Marshal(message)
	if err != nil {
		slogging.Get().Info("Error marshaling message: %v", err)
		return
	}

	// Use defer/recover as additional safety against panics
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Debug("Recovered from panic sending to client %s: %v", client.UserID, r)
		}
	}()

	// Double-check closing flag right before send to minimize race window
	client.closingMu.RLock()
	if client.closing {
		client.closingMu.RUnlock()
		slogging.Get().Debug("Client %s closing flag set just before send, dropping message", client.UserID)
		return
	}
	client.closingMu.RUnlock()

	select {
	case client.Send <- msgBytes:
		// Successfully sent
	default:
		slogging.Get().Info("Client send channel full, dropping message")
	}
}

// broadcastMessage broadcasts a message to all clients
func (s *DiagramSession) broadcastMessage(message interface{}) {
	msgBytes, err := json.Marshal(message)
	if err != nil {
		slogging.Get().Info("Error marshaling broadcast message: %v", err)
		return
	}

	s.Broadcast <- msgBytes
}

// broadcastToOthers broadcasts a message to all clients except the sender
func (s *DiagramSession) broadcastToOthers(sender *WebSocketClient, message interface{}) {
	slogging.Get().Info("[TRACE-BROADCAST] broadcastToOthers ENTRY - Session: %s, Sender: %s (%p), Message type: %T",
		s.ID, sender.UserID, sender, message)

	msgBytes, err := json.Marshal(message)
	if err != nil {
		slogging.Get().Error("[TRACE-BROADCAST] Error marshaling message: %v", err)
		return
	}

	slogging.Get().Info("[TRACE-BROADCAST] Message marshaled successfully - Size: %d bytes", len(msgBytes))

	s.mu.RLock()
	defer s.mu.RUnlock()

	totalClients := len(s.Clients)
	recipientCount := 0
	skippedSender := false

	slogging.Get().Info("[TRACE-BROADCAST] broadcastToOthers - Session: %s, Sender: %s (%p), Total clients: %d",
		s.ID, sender.UserID, sender, totalClients)

	for client := range s.Clients {
		slogging.Get().Debug("[TRACE-BROADCAST] Checking client - User: %s, Pointer: %p, Is sender? %v",
			client.UserID, client, client == sender)

		if client == sender {
			// This is the sender - skip them
			skippedSender = true
			slogging.Get().Info("[TRACE-BROADCAST]   ✗ Skipping sender - User: %s, Pointer: %p (matches sender pointer)",
				client.UserID, client)
		} else {
			// This is a recipient - send the message
			slogging.Get().Info("[TRACE-BROADCAST]   → Attempting to send to recipient - User: %s, Pointer: %p, Channel buffer: %d/%d",
				client.UserID, client, len(client.Send), cap(client.Send))

			select {
			case client.Send <- msgBytes:
				recipientCount++
				slogging.Get().Info("[TRACE-BROADCAST]     ✓✓✓ Message SUCCESSFULLY QUEUED to channel for %s ✓✓✓", client.UserID)
			default:
				slogging.Get().Error("[TRACE-BROADCAST]     ✗✗✗ Client send channel FULL - DROPPING MESSAGE for %s (channel: %d/%d) ✗✗✗",
					client.UserID, len(client.Send), cap(client.Send))
			}
		}
	}

	slogging.Get().Info("[TRACE-BROADCAST] Broadcast complete - Session: %s, Sender: %s, Recipients: %d, Skipped sender: %v",
		s.ID, sender.UserID, recipientCount, skippedSender)
}

// CellOperationProcessor processes cell operations with validation and conflict detection
type CellOperationProcessor struct {
	diagramStore DiagramStoreInterface
}

// NewCellOperationProcessor creates a new cell operation processor
func NewCellOperationProcessor(store DiagramStoreInterface) *CellOperationProcessor {
	return &CellOperationProcessor{
		diagramStore: store,
	}
}

// ProcessCellOperations processes a batch of cell operations with full validation
func (cop *CellOperationProcessor) ProcessCellOperations(diagramID string, operation CellPatchOperation) (*OperationValidationResult, error) {
	// Get current diagram state
	diagram, err := cop.diagramStore.Get(diagramID)
	if err != nil {
		return nil, fmt.Errorf("failed to get diagram %s: %w", diagramID, err)
	}

	// Build current state map for conflict detection using union type
	currentState := make(map[string]*DfdDiagram_Cells_Item)
	for i := range diagram.Cells {
		cellItem := &diagram.Cells[i]
		// Extract ID from union type
		var itemID string
		if node, err := cellItem.AsNode(); err == nil {
			itemID = node.Id.String()
		} else if edge, err := cellItem.AsEdge(); err == nil {
			itemID = edge.Id.String()
		}
		if itemID != "" {
			currentState[itemID] = cellItem
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
func (cop *CellOperationProcessor) processAndValidateCellOperations(diagram *DfdDiagram, currentState map[string]*DfdDiagram_Cells_Item, operation CellPatchOperation) *OperationValidationResult {
	result := &OperationValidationResult{
		Valid:         true,
		StateChanged:  false,
		CellsModified: make([]string, 0),
		PreviousState: make(map[string]*DfdDiagram_Cells_Item),
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

	// Check for duplicate cell IDs within this operation to prevent client bugs
	// from causing conflicts and disconnections
	seenCellIDs := make(map[string]bool)
	deduplicatedCells := make([]CellOperation, 0, len(operation.Cells))

	for _, cellOp := range operation.Cells {
		if seenCellIDs[cellOp.ID] {
			slogging.Get().Warn("Duplicate cell operation detected in single message - CellID: %s, Operation: %s",
				cellOp.ID, cellOp.Operation)
			continue // Skip duplicate operations
		}
		seenCellIDs[cellOp.ID] = true
		deduplicatedCells = append(deduplicatedCells, cellOp)
	}

	if len(deduplicatedCells) != len(operation.Cells) {
		slogging.Get().Info("Filtered %d duplicate cell operations from message",
			len(operation.Cells)-len(deduplicatedCells))
	}

	// Process each deduplicated cell operation
	for _, cellOp := range deduplicatedCells {
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
func (cop *CellOperationProcessor) validateAndProcessCellOperation(diagram *DfdDiagram, currentState map[string]*DfdDiagram_Cells_Item, cellOp CellOperation) *OperationValidationResult {
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

// normalizeCellData ensures consistent structure for cell data
// Converts flat X/Y/Width/Height properties to nested Position/Size structs
func normalizeCellData(cellItem *DfdDiagram_Cells_Item) {
	// Try to extract as Node
	if node, err := cellItem.AsNode(); err == nil {
		// Check if flat properties are set but Position is not
		if node.X != nil && node.Y != nil && node.Position == nil {
			node.Position = &struct {
				X float32 `json:"x"`
				Y float32 `json:"y"`
			}{
				X: *node.X,
				Y: *node.Y,
			}
			// Clear flat properties after moving to Position
			node.X = nil
			node.Y = nil

			// Update the cell item with normalized node
			_ = cellItem.FromNode(node)
		}

		// Check if flat Width/Height are set but Size is not
		if node.Width != nil && node.Height != nil && node.Size == nil {
			node.Size = &struct {
				Height float32 `json:"height"`
				Width  float32 `json:"width"`
			}{
				Height: *node.Height,
				Width:  *node.Width,
			}
			// Clear flat properties after moving to Size
			node.Width = nil
			node.Height = nil

			// Update the cell item with normalized node
			_ = cellItem.FromNode(node)
		}
	}
	// Edges don't have position/size normalization needs
}

// NormalizeDiagramCells normalizes all cells in a diagram
// This should be called for both REST API and WebSocket operations
func NormalizeDiagramCells(cells []DfdDiagram_Cells_Item) {
	for i := range cells {
		normalizeCellData(&cells[i])
	}
}

// validateAddOperation validates adding a new cell
// If the cell already exists, this operation is treated as idempotent and converted to an update
func (cop *CellOperationProcessor) validateAddOperation(diagram *DfdDiagram, currentState map[string]*DfdDiagram_Cells_Item, cellOp CellOperation) *OperationValidationResult {
	result := &OperationValidationResult{Valid: true}

	// Validate cell data first
	if cellOp.Data == nil {
		result.Valid = false
		result.Reason = "add_requires_cell_data"
		return result
	}

	// Normalize cell data to use Position/Size structs instead of flat properties
	normalizeCellData(cellOp.Data)

	// Check if cell already exists - if so, treat as update (idempotent add)
	if _, exists := currentState[cellOp.ID]; exists {
		slogging.Get().Debug("Add operation for existing cell - converting to update (idempotent) - CellID: %s", cellOp.ID)

		// Convert to update operation by finding and replacing the cell
		found := false
		for i := range diagram.Cells {
			cellItem := &diagram.Cells[i]
			// Extract ID from union type to find matching cell
			var itemID string
			if node, err := cellItem.AsNode(); err == nil {
				itemID = node.Id.String()
			} else if edge, err := cellItem.AsEdge(); err == nil {
				itemID = edge.Id.String()
			}

			if itemID == cellOp.ID {
				// Replace with new cell data (idempotent add acts as update)
				diagram.Cells[i] = *cellOp.Data
				found = true
				result.StateChanged = true
				break
			}
		}

		if !found {
			// This shouldn't happen if currentState is accurate, but handle defensively
			result.Valid = false
			result.Reason = "cell_not_found_in_diagram"
			result.ConflictDetected = true
			return result
		}

		return result
	}

	// Cell doesn't exist - perform normal add operation
	// cellOp.Data is already a DfdDiagram_Cells_Item union type (Node | Edge)
	// No conversion needed - directly append to diagram
	diagram.Cells = append(diagram.Cells, *cellOp.Data)
	result.StateChanged = true

	return result
}

// validateUpdateOperation validates updating an existing cell
func (cop *CellOperationProcessor) validateUpdateOperation(diagram *DfdDiagram, currentState map[string]*DfdDiagram_Cells_Item, cellOp CellOperation) *OperationValidationResult {
	result := &OperationValidationResult{Valid: true}

	// Check if cell exists
	_, exists := currentState[cellOp.ID]
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

	// Normalize cell data to use Position/Size structs instead of flat properties
	normalizeCellData(cellOp.Data)

	// Apply the update operation to diagram
	// cellOp.Data is already a DfdDiagram_Cells_Item union type (Node | Edge)
	found := false
	for i := range diagram.Cells {
		cellItem := &diagram.Cells[i]
		// Extract ID from union type to find matching cell
		var itemID string
		if node, err := cellItem.AsNode(); err == nil {
			itemID = node.Id.String()
		} else if edge, err := cellItem.AsEdge(); err == nil {
			itemID = edge.Id.String()
		}

		if itemID == cellOp.ID {
			// Replace with updated cell - no conversion needed
			diagram.Cells[i] = *cellOp.Data
			found = true
			result.StateChanged = true
			break
		}
	}

	if !found {
		result.Valid = false
		result.Reason = "cell_not_found_in_diagram"
		result.ConflictDetected = true
		return result
	}

	return result
}

// validateRemoveOperation validates removing a cell
func (cop *CellOperationProcessor) validateRemoveOperation(diagram *DfdDiagram, currentState map[string]*DfdDiagram_Cells_Item, cellOp CellOperation) *OperationValidationResult {
	result := &OperationValidationResult{Valid: true}

	// Check if cell exists
	if _, exists := currentState[cellOp.ID]; !exists {
		// Removing non-existent cell is idempotent - not an error
		result.StateChanged = false
		return result
	}

	// Apply the remove operation to diagram
	found := false
	for i := range diagram.Cells {
		cellItem := &diagram.Cells[i]
		// Extract ID from union type to find matching cell
		var itemID string
		if node, err := cellItem.AsNode(); err == nil {
			itemID = node.Id.String()
		} else if edge, err := cellItem.AsEdge(); err == nil {
			itemID = edge.Id.String()
		}

		if itemID == cellOp.ID {
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

	result.StateChanged = found

	return result
}

// validateCellData and detectCellChanges removed - no longer needed with union types

// OperationValidationResult represents the result of operation validation
type OperationValidationResult struct {
	Valid            bool
	Reason           string
	CorrectionNeeded bool
	ConflictDetected bool
	StateChanged     bool
	CellsModified    []string
	PreviousState    map[string]*DfdDiagram_Cells_Item
}

// processAndValidateCellOperations processes and validates cell operations
func (s *DiagramSession) processAndValidateCellOperations(diagram *DfdDiagram, currentState map[string]*DfdDiagram_Cells_Item, operation CellPatchOperation) OperationValidationResult {
	result := OperationValidationResult{
		Valid:         true,
		StateChanged:  false,
		CellsModified: make([]string, 0),
		PreviousState: make(map[string]*DfdDiagram_Cells_Item),
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

	// Check for duplicate cell IDs within this operation to prevent client bugs
	// from causing conflicts and disconnections
	seenCellIDs := make(map[string]bool)
	deduplicatedCells := make([]CellOperation, 0, len(operation.Cells))

	for _, cellOp := range operation.Cells {
		if seenCellIDs[cellOp.ID] {
			slogging.Get().Warn("Duplicate cell operation detected in single message - Session: %s, CellID: %s, Operation: %s",
				s.ID, cellOp.ID, cellOp.Operation)
			continue // Skip duplicate operations
		}
		seenCellIDs[cellOp.ID] = true
		deduplicatedCells = append(deduplicatedCells, cellOp)
	}

	if len(deduplicatedCells) != len(operation.Cells) {
		slogging.Get().Info("Filtered %d duplicate cell operations from message - Session: %s",
			len(operation.Cells)-len(deduplicatedCells), s.ID)
	}

	// Process each deduplicated cell operation
	for _, cellOp := range deduplicatedCells {
		// Log each cell operation before processing
		slogging.Get().Debug("Processing cell operation - Session: %s, CellID: %s, Operation: %s, CurrentStateHasCell: %v",
			s.ID, cellOp.ID, cellOp.Operation, currentState[cellOp.ID] != nil)

		cellResult := s.validateAndProcessCellOperation(diagram, currentState, cellOp)

		if !cellResult.Valid {
			result.Valid = false
			result.Reason = cellResult.Reason
			result.ConflictDetected = cellResult.ConflictDetected
			result.CorrectionNeeded = cellResult.CorrectionNeeded
			result.CellsModified = append(result.CellsModified, cellOp.ID)
			slogging.Get().Warn("Cell operation validation failed - Session: %s, CellID: %s, Operation: %s, Reason: %s",
				s.ID, cellOp.ID, cellOp.Operation, cellResult.Reason)
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
func (s *DiagramSession) validateAndProcessCellOperation(diagram *DfdDiagram, currentState map[string]*DfdDiagram_Cells_Item, cellOp CellOperation) OperationValidationResult {
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
func (s *DiagramSession) validateAddOperation(diagram *DfdDiagram, currentState map[string]*DfdDiagram_Cells_Item, cellOp CellOperation) OperationValidationResult {
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

	// Apply the add operation to diagram
	// cellOp.Data is already a DfdDiagram_Cells_Item union type (Node | Edge)
	// No conversion needed - directly append to diagram
	diagram.Cells = append(diagram.Cells, *cellOp.Data)
	result.StateChanged = true

	// Use centralized update function to save changes
	_, saveErr := s.Hub.UpdateDiagramCells(s.DiagramID, diagram.Cells, "websocket", "")
	if saveErr != nil {
		slogging.Get().Info("Failed to save diagram after add operation: %v", saveErr)
		result.Valid = false
		result.Reason = "save_failed"
		return result
	}

	return result
}

// validateUpdateOperation validates updating an existing cell
func (s *DiagramSession) validateUpdateOperation(diagram *DfdDiagram, currentState map[string]*DfdDiagram_Cells_Item, cellOp CellOperation) OperationValidationResult {
	result := OperationValidationResult{Valid: true}

	// Check if cell exists
	_, exists := currentState[cellOp.ID]
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

	// Apply the update operation to diagram
	// cellOp.Data is already a DfdDiagram_Cells_Item union type (Node | Edge)
	found := false
	for i := range diagram.Cells {
		cellItem := &diagram.Cells[i]
		// Extract ID from union type to find matching cell
		var itemID string
		if node, err := cellItem.AsNode(); err == nil {
			itemID = node.Id.String()
		} else if edge, err := cellItem.AsEdge(); err == nil {
			itemID = edge.Id.String()
		}

		if itemID == cellOp.ID {
			// Replace with updated cell - no conversion needed
			diagram.Cells[i] = *cellOp.Data
			found = true
			result.StateChanged = true
			break
		}
	}

	if !found {
		result.Valid = false
		result.Reason = "cell_not_found_in_diagram"
		result.ConflictDetected = true
		return result
	}

	// Use centralized update function to save changes
	_, saveErr := s.Hub.UpdateDiagramCells(s.DiagramID, diagram.Cells, "websocket", "")
	if saveErr != nil {
		slogging.Get().Info("Failed to save diagram after update operation: %v", saveErr)
		result.Valid = false
		result.Reason = "save_failed"
		return result
	}

	return result
}

// validateRemoveOperation validates removing a cell
func (s *DiagramSession) validateRemoveOperation(diagram *DfdDiagram, currentState map[string]*DfdDiagram_Cells_Item, cellOp CellOperation) OperationValidationResult {
	result := OperationValidationResult{Valid: true}

	// Check if cell exists
	if _, exists := currentState[cellOp.ID]; !exists {
		// Removing non-existent cell is idempotent - not an error
		result.StateChanged = false
		return result
	}

	// Apply the remove operation to diagram
	found := false
	for i := range diagram.Cells {
		cellItem := &diagram.Cells[i]
		// Extract ID from union type to find matching cell
		var itemID string
		if node, err := cellItem.AsNode(); err == nil {
			itemID = node.Id.String()
		} else if edge, err := cellItem.AsEdge(); err == nil {
			itemID = edge.Id.String()
		}

		if itemID == cellOp.ID {
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

	result.StateChanged = found

	if found {
		// Use centralized update function to save changes
		_, saveErr := s.Hub.UpdateDiagramCells(s.DiagramID, diagram.Cells, "websocket", "")
		if saveErr != nil {
			slogging.Get().Info("Failed to save diagram after remove operation: %v", saveErr)
			result.Valid = false
			result.Reason = "save_failed"
			return result
		}
	}

	return result
}

// addToHistory adds an operation to the history for conflict resolution
func (s *DiagramSession) addToHistory(msg DiagramOperationMessage, userID string, previousState, _ map[string]*DfdDiagram_Cells_Item) {
	if s.OperationHistory == nil {
		return
	}

	var sequenceNumber uint64
	if msg.SequenceNumber != nil {
		sequenceNumber = *msg.SequenceNumber
	}

	entry := &HistoryEntry{
		SequenceNumber: sequenceNumber,
		OperationID:    msg.OperationID,
		UserID:         userID,
		Timestamp:      time.Now().UTC(),
		Operation:      msg.Operation,
		PreviousState:  previousState,
	}

	// AddOperation handles its own mutex locking, so we don't lock here
	s.OperationHistory.AddOperation(entry)
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
		// Panic recovery
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in WebSocketClient.ReadPump() - User: %s, Session: %s, Error: %v, Stack: %s",
				c.UserID, c.Session.ID, r, debug.Stack())
		}

		// Log WebSocket disconnection
		if c.Session != nil && c.Hub != nil {
			slogging.LogWebSocketConnection("CONNECTION_CLOSED", c.Session.ID, c.UserID, c.Session.DiagramID, c.Hub.LoggingConfig)
		}
		if c.Session != nil {
			c.Session.Unregister <- c
		}
		if err := c.Conn.Close(); err != nil {
			slogging.Get().Info("Error closing connection: %v", err)
		}
	}()

	slogging.Get().Debug("WebSocketClient.ReadPump() started - User: %s, Session: %s", c.UserID, c.Session.ID)

	c.Conn.SetReadLimit(65536) // 64KB message limit
	// Set read timeout to 3x ping interval (90 seconds)
	readTimeout := 90 * time.Second
	if err := c.Conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		slogging.Get().Info("Error setting read deadline: %v", err)
		return
	}
	c.Conn.SetPongHandler(func(string) error {
		if err := c.Conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			slogging.Get().Info("Error setting read deadline in pong handler: %v", err)
		}
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			// Check for message size limit error (code 1009)
			if websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
				if c.Session != nil {
					slogging.Get().Info("WebSocket message too large in session %s for user %s (limit: 64KB): %v", c.Session.ID, c.UserID, err)
				} else {
					slogging.Get().Info("WebSocket message too large (no session) for user %s (limit: 64KB): %v", c.UserID, err)
				}
			} else if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				if c.Session != nil {
					slogging.Get().Info("WebSocket error in session %s for user %s: %v", c.Session.ID, c.UserID, err)
				} else {
					slogging.Get().Info("WebSocket error (no session) for user %s: %v", c.UserID, err)
				}
				// Log WebSocket error
				if c.Session != nil && c.Hub != nil {
					slogging.LogWebSocketError("UNEXPECTED_CLOSE", err.Error(), c.Session.ID, c.UserID, c.Hub.LoggingConfig)
				}
			}
			break
		}

		// Update last activity timestamp
		c.LastActivity = time.Now().UTC()

		// Log inbound WebSocket message
		if c.Session != nil && c.Hub != nil {
			slogging.LogWebSocketMessage(
				slogging.WSMessageInbound,
				c.Session.ID,
				c.UserName,
				"text",
				message,
				c.Hub.LoggingConfig,
			)
		}

		// Process message using enhanced message handler
		c.Session.ProcessMessage(c, message)
	}
}

// WritePump pumps messages from hub to WebSocket
func (c *WebSocketClient) WritePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		// Panic recovery
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in WebSocketClient.WritePump() - User: %s, Session: %s, Error: %v, Stack: %s",
				c.UserID, c.Session.ID, r, debug.Stack())
		}

		ticker.Stop()
		if err := c.Conn.Close(); err != nil {
			slogging.Get().Info("Error closing connection: %v", err)
		}
	}()

	slogging.Get().Debug("WebSocketClient.WritePump() started - User: %s, Session: %s", c.UserID, c.Session.ID)

	for {
		select {
		case message, ok := <-c.Send:
			slogging.Get().Info("[TRACE-BROADCAST] WritePump: Received message from channel - User: %s, OK: %v, Length: %d bytes",
				c.UserID, ok, len(message))

			if err := c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				slogging.Get().Error("[TRACE-BROADCAST] WritePump: Error setting write deadline: %v", err)
				return
			}
			if !ok {
				// Hub closed the channel
				slogging.Get().Info("[TRACE-BROADCAST] WritePump: Channel closed for user %s", c.UserID)
				if err := c.Conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
					slogging.Get().Info("Error writing close message: %v", err)
				}
				return
			}
			slogging.Get().Info("[TRACE-BROADCAST] WritePump: About to write message to WebSocket - User: %s, Length: %d", c.UserID, len(message))

			// Log outbound WebSocket message
			if c.Session != nil && c.Hub != nil {
				slogging.LogWebSocketMessage(
					slogging.WSMessageOutbound,
					c.Session.ID,
					c.UserName,
					"text",
					message,
					c.Hub.LoggingConfig,
				)
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				slogging.Get().Error("[TRACE-BROADCAST] WritePump: Error getting NextWriter for user %s: %v", c.UserID, err)
				return
			}
			bytesWritten, err := w.Write(message)
			if err != nil {
				slogging.Get().Error("[TRACE-BROADCAST] WritePump: Error writing message for user %s: %v", c.UserID, err)
				return
			}
			slogging.Get().Info("[TRACE-BROADCAST] WritePump: Wrote %d bytes to writer for user %s", bytesWritten, c.UserID)

			// Don't try to batch messages - it causes issues with JSON parsing
			// Each WebSocket message should be sent separately

			if err := w.Close(); err != nil {
				slogging.Get().Error("[TRACE-BROADCAST] WritePump: Error closing writer for user %s: %v", c.UserID, err)
				return
			}
			slogging.Get().Info("[TRACE-BROADCAST] ✓✓✓ WritePump: Message SUCCESSFULLY SENT to WebSocket for user %s ✓✓✓", c.UserID)
		case <-ticker.C:
			if err := c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				slogging.Get().Info("Error setting write deadline for ping: %v", err)
				return
			}
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
