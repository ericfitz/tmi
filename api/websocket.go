package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

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
	LoggingConfig logging.WebSocketLoggingConfig
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

	// Enhanced collaboration state
	// Host (user who created the session)
	Host string
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
}

// WebSocketMessage represents the legacy message format (kept for backward compatibility)
type WebSocketMessage struct {
	// Type of message (update, join, leave, session_ended)
	Event string `json:"event"`
	// User who sent the message (legacy format - new messages should use User object)
	UserID string `json:"user_id"`
	// Diagram operation
	Operation DiagramOperation `json:"operation,omitempty"`
	// Optional message text for events like session_ended
	Message string `json:"message,omitempty"`
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

		logging.Get().Warn("Rejected WebSocket connection from origin: %s", origin)
		return false
	},
}

// NewWebSocketHub creates a new WebSocket hub
func NewWebSocketHub(loggingConfig logging.WebSocketLoggingConfig) *WebSocketHub {
	return &WebSocketHub{
		Diagrams:      make(map[string]*DiagramSession),
		LoggingConfig: loggingConfig,
	}
}

// NewWebSocketHubForTests creates a WebSocket hub with default test configuration
func NewWebSocketHubForTests() *WebSocketHub {
	return NewWebSocketHub(logging.WebSocketLoggingConfig{
		Enabled:        false, // Disable logging in tests by default
		RedactTokens:   true,
		MaxMessageSize: 5 * 1024,
		OnlyDebugLevel: true,
	})
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
		Broadcast:     make(chan []byte),
		Register:      make(chan *WebSocketClient),
		Unregister:    make(chan *WebSocketClient),
		LastActivity:  time.Now().UTC(),
		CreatedAt:     time.Now().UTC(),
		Hub:           h, // Reference to the hub for cleanup

		// Enhanced collaboration state
		Host:               hostUserID,
		CurrentPresenter:   hostUserID, // Host starts as presenter
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

	logging.Get().Info("Created new session %s for diagram %s (host: %s, threat model: %s)",
		session.ID, diagramID, hostUserID, threatModelID)

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
		logging.Get().Info("Retrieved existing session %s for diagram %s (host: %s, state: %s)",
			session.ID, diagramID, session.Host, session.State)
		return session
	}

	session := &DiagramSession{
		ID:            uuid.New().String(),
		DiagramID:     diagramID,
		ThreatModelID: threatModelID,
		State:         SessionStateActive,
		Clients:       make(map[*WebSocketClient]bool),
		Broadcast:     make(chan []byte),
		Register:      make(chan *WebSocketClient),
		Unregister:    make(chan *WebSocketClient),
		LastActivity:  time.Now().UTC(),
		CreatedAt:     time.Now().UTC(),
		Hub:           h, // Reference to the hub for cleanup

		// Enhanced collaboration state
		Host:               hostUserID,
		CurrentPresenter:   hostUserID, // Host starts as presenter
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

	logging.Get().Info("Created new session %s for diagram %s (host: %s, threat model: %s)",
		session.ID, diagramID, hostUserID, threatModelID)

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
func convertClientToParticipant(client *WebSocketClient, session *DiagramSession, tm *ThreatModel) *Participant {
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
			UserId: client.UserID,
			Email:  client.UserEmail,
			Name:   client.UserName,
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
		logging.Get().Error("buildCollaborationSessionFromDiagramSession: Failed to parse diagram ID '%s': %v", diagramID, err)
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

	// Check if user has any access to the threat model (reader, writer, or resource owner)
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
		logging.Get().Debug(" ThreatModelStore is nil, denying WebSocket access for diagram %s", diagramID)
		return openapi_types.UUID{}
	}

	// Search through all threat models to find the one containing this diagram
	// Use a large limit to get all threat models (in practice we should have pagination)
	threatModels := ThreatModelStore.List(0, 1000, nil)
	logging.Get().Debug(" Searching for diagram %s in %d threat models", diagramID, len(threatModels))

	for _, tm := range threatModels {
		if tm.Diagrams != nil {
			logging.Get().Debug(" Checking threat model %s with %d diagrams", tm.Id.String(), len(*tm.Diagrams))
			for _, diagramUnion := range *tm.Diagrams {
				// Convert union type to DfdDiagram to get the ID
				if dfdDiag, err := diagramUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil {
					logging.Get().Debug(" Found diagram %s in threat model %s", dfdDiag.Id.String(), tm.Id.String())
					if dfdDiag.Id.String() == diagramID {
						logging.Get().Debug(" Match found! Diagram %s belongs to threat model %s", diagramID, tm.Id.String())
						return *tm.Id
					}
				} else {
					logging.Get().Debug(" Failed to convert diagram union to DfdDiagram: %v", err)
				}
			}
		} else {
			logging.Get().Debug(" Threat model %s has nil Diagrams", tm.Id.String())
		}
	}

	logging.Get().Debug(" Diagram %s not found in any threat model", diagramID)
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

// CleanupInactiveSessions removes sessions that are inactive or empty with grace period
func (h *WebSocketHub) CleanupInactiveSessions() {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now().UTC()
	inactivityTimeout := now.Add(-5 * time.Minute)
	emptySessionTimeout := now.Add(-1 * time.Minute) // 1-minute grace period for empty sessions

	terminatedSessionTimeout := now.Add(-15 * time.Second) // 15-second grace period for terminated sessions

	for diagramID, session := range h.Diagrams {
		session.mu.RLock()
		lastActivity := session.LastActivity
		createdAt := session.CreatedAt
		clientCount := len(session.Clients)
		sessionState := session.State
		terminatedAt := session.TerminatedAt
		session.mu.RUnlock()

		shouldCleanup := false
		cleanupReason := ""

		// Check if session is terminated and past grace period
		if sessionState == SessionStateTerminated && terminatedAt != nil && terminatedAt.Before(terminatedSessionTimeout) {
			shouldCleanup = true
			cleanupReason = "terminated session past grace period"
		} else if lastActivity.Before(inactivityTimeout) {
			// Check for long-term inactivity (15+ minutes)
			shouldCleanup = true
			cleanupReason = "inactive for 5+ minutes"
		} else if clientCount == 0 && createdAt.Before(emptySessionTimeout) {
			// Check for empty sessions past grace period (1+ minute with no clients)
			shouldCleanup = true
			cleanupReason = "empty session past 1-minute grace period"
		}

		if shouldCleanup {
			// Close session
			for client := range session.Clients {
				close(client.Send)
			}
			delete(h.Diagrams, diagramID)
			logging.Get().Info("Cleaned up session %s for diagram %s: %s", session.ID, diagramID, cleanupReason)

			// Record session cleanup
			if GlobalPerformanceMonitor != nil {
				GlobalPerformanceMonitor.RecordSessionEnd(session.ID)
			}
		}
	}
}

// CleanupEmptySessions performs immediate cleanup of empty sessions past grace period
func (h *WebSocketHub) CleanupEmptySessions() {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now().UTC()
	emptySessionTimeout := now.Add(-1 * time.Minute) // 1-minute grace period for empty sessions

	for diagramID, session := range h.Diagrams {
		session.mu.RLock()
		createdAt := session.CreatedAt
		clientCount := len(session.Clients)
		session.mu.RUnlock()

		// Only cleanup empty sessions past grace period
		if clientCount == 0 && createdAt.Before(emptySessionTimeout) {
			// Close session
			for client := range session.Clients {
				close(client.Send)
			}
			delete(h.Diagrams, diagramID)
			logging.Get().Info("Cleaned up empty session %s for diagram %s (triggered by user departure)", session.ID, diagramID)

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
		logging.Get().Info("No active collaboration sessions to clean up")
		return
	}

	logging.Get().Info("Cleaning up %d existing collaboration sessions from previous server run", sessionCount)

	// Close all sessions
	for diagramID, session := range h.Diagrams {
		// Close all client connections
		for client := range session.Clients {
			close(client.Send)
		}
		logging.Get().Debug("Cleaned up collaboration session for diagram %s", diagramID)
	}

	// Clear the sessions map
	h.Diagrams = make(map[string]*DiagramSession)
	logging.Get().Info("All collaboration sessions cleaned up successfully")
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
	// Defer cleanup when session terminates
	defer func() {
		// Remove session from hub when Run exits
		if s.Hub != nil {
			s.Hub.mu.Lock()
			delete(s.Hub.Diagrams, s.DiagramID)
			s.Hub.mu.Unlock()
			logging.Get().Info("Session %s removed from hub after termination", s.ID)
		}
	}()

	for {
		select {
		case client := <-s.Register:
			s.mu.Lock()
			s.Clients[client] = true
			s.LastActivity = time.Now().UTC()
			s.mu.Unlock()

			// Send initial state to the new client
			// First, send current presenter info
			s.mu.RLock()
			currentPresenter := s.CurrentPresenter
			s.mu.RUnlock()

			if currentPresenter != "" {
				presenterMsg := CurrentPresenterMessage{
					MessageType:      MessageTypeCurrentPresenter,
					CurrentPresenter: currentPresenter,
				}
				if msgBytes, err := json.Marshal(presenterMsg); err == nil {
					select {
					case client.Send <- msgBytes:
					default:
						logging.Get().Error("Failed to send current presenter to new client")
					}
				}
			}

			// Send participant list to the new client
			s.sendParticipantsUpdateToClient(client)

			// Notify other clients that someone joined
			msg := WebSocketMessage{
				Event:     "join",
				UserID:    client.UserID,
				Timestamp: time.Now().UTC(),
			}
			if msgBytes, err := json.Marshal(msg); err == nil {
				s.Broadcast <- msgBytes
			}

			// Broadcast updated participant list to all clients
			s.broadcastParticipantsUpdate()

		case client := <-s.Unregister:
			s.mu.Lock()
			if _, ok := s.Clients[client]; ok {
				delete(s.Clients, client)
				close(client.Send)
				s.LastActivity = time.Now().UTC()

				// Check if the leaving client was the current presenter
				wasPresenter := client.UserID == s.CurrentPresenter

				// Check if the leaving client was the host
				wasHost := client.UserID == s.Host

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
			msg := WebSocketMessage{
				Event:     "leave",
				UserID:    client.UserID,
				Timestamp: time.Now().UTC(),
			}
			if msgBytes, err := json.Marshal(msg); err == nil {
				s.Broadcast <- msgBytes
			}

			// Broadcast updated participant list to all remaining clients
			s.broadcastParticipantsUpdate()

			// Trigger cleanup of empty sessions after user departure
			if s.Hub != nil {
				go s.Hub.CleanupEmptySessions()
			}

		case message := <-s.Broadcast:
			// Log outgoing broadcast message
			logging.Get().Debug("[wsmsg] Broadcasting message - session_id=%s message_size=%d raw_message=%s client_count=%d",
				s.ID, len(message), string(message), len(s.Clients))
			s.mu.Lock()
			s.LastActivity = time.Now().UTC()
			clientCount := 0
			// Send to all clients
			for client := range s.Clients {
				select {
				case client.Send <- message:
					clientCount++
				default:
					close(client.Send)
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
			c.JSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "User not authenticated",
			})
			return "", "", "", fmt.Errorf("user not authenticated")
		}
	}

	userIDStr, ok := userID.(string)
	if !ok || userIDStr == "" {
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
	threatModelID, diagramID, userIDStr, err := h.validateWebSocketRequest(c)
	if err != nil {
		return
	}

	// Get user display name from context (optional, will use email as fallback)
	userNameStr := ""
	if userName, exists := c.Get("user_name"); exists {
		if name, ok := userName.(string); ok && name != "" {
			userNameStr = name
		}
	}

	// Get user email from context
	userEmailStr := ""
	if userEmail, exists := c.Get("user_email"); exists {
		if email, ok := userEmail.(string); ok && email != "" {
			userEmailStr = email
		}
	}

	// If no display name, use email as fallback
	if userNameStr == "" && userEmailStr != "" {
		userNameStr = userEmailStr
	}

	// If still no display name, use user ID as last resort
	if userNameStr == "" {
		userNameStr = userIDStr
	}

	// Upgrade to WebSocket first
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logging.Get().Info("Failed to upgrade connection: %v", err)
		return
	}

	// CRITICAL: Validate user has access to the diagram after upgrading
	// For backwards compatibility, use email for validation if userID lookup fails
	validationID := userIDStr
	if userEmailStr != "" {
		// TODO: Update validateWebSocketDiagramAccessDirect to use user ID instead of email
		validationID = userEmailStr
	}
	if !h.validateWebSocketDiagramAccessDirect(validationID, threatModelID, diagramID) {
		// Send error message before closing
		errorMsg := map[string]string{
			"error":   "unauthorized",
			"message": "You don't have sufficient permissions to collaborate on this diagram",
		}
		if msgBytes, err := json.Marshal(errorMsg); err == nil {
			if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
				logging.Get().Debug("Failed to send error message: %v", err)
			}
		}
		// Close the connection
		if err := conn.Close(); err != nil {
			logging.Get().Debug("Failed to close connection: %v", err)
		}
		logging.Get().Info("Disconnected user %s - no permissions for diagram %s", userIDStr, diagramID)
		return
	}

	// Get optional session_id from query parameters
	sessionID := c.Query("session_id")

	// Get or create session
	session := h.GetOrCreateSession(diagramID, threatModelID, userIDStr)

	// Log session state
	logging.Get().Info("WebSocket connection attempt - User: %s, Diagram: %s, Session: %s, Provided SessionID: %s",
		userIDStr, diagramID, session.ID, sessionID)

	// Check if the session is still active
	session.mu.RLock()
	sessionState := session.State
	sessionActualID := session.ID
	session.mu.RUnlock()

	// Validate session state and ID
	if sessionState != SessionStateActive {
		// Session has been terminated
		errorMsg := map[string]interface{}{
			"error":   "session_terminated",
			"message": "The collaboration session has been terminated",
			"reason":  "host_disconnected",
		}
		if msgBytes, err := json.Marshal(errorMsg); err == nil {
			if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
				logging.Get().Debug("Failed to send error message: %v", err)
			}
		}
		// Close the connection
		if err := conn.Close(); err != nil {
			logging.Get().Debug("Failed to close connection: %v", err)
		}
		logging.Get().Info("Rejected connection from user %s - session %s is terminated", userIDStr, sessionActualID)
		return
	}

	// If a session ID was provided, validate it matches
	if sessionID != "" && sessionID != sessionActualID {
		// Session ID mismatch - likely trying to reconnect to an old session
		errorMsg := map[string]interface{}{
			"error":          "session_invalid",
			"message":        "The collaboration session ID is invalid or expired",
			"new_session_id": sessionActualID,
		}
		if msgBytes, err := json.Marshal(errorMsg); err == nil {
			if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
				logging.Get().Debug("Failed to send error message: %v", err)
			}
		}
		// Close the connection
		if err := conn.Close(); err != nil {
			logging.Get().Debug("Failed to close connection: %v", err)
		}
		logging.Get().Info("Rejected connection from user %s - provided session ID %s does not match current session %s", userIDStr, sessionID, sessionActualID)
		return
	}

	// Create client
	client := &WebSocketClient{
		Hub:          h,
		Session:      session,
		Conn:         conn,
		UserID:       userIDStr,
		UserName:     userNameStr,
		UserEmail:    userEmailStr,
		Send:         make(chan []byte, 256),
		LastActivity: time.Now().UTC(),
	}

	// Register client
	session.Register <- client

	// Log WebSocket connection
	logging.LogWebSocketConnection("CONNECTION_ESTABLISHED", session.ID, userIDStr, diagramID, h.LoggingConfig)

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
	// Log raw incoming message with wsmsg component
	logging.Get().Debug("[wsmsg] Received WebSocket message - session_id=%s user_id=%s message_size=%d raw_message=%s",
		s.ID, client.UserID, len(message), string(message))
	// First try to parse as enhanced message format
	var baseMsg struct {
		MessageType string          `json:"message_type"`
		UserID      string          `json:"user_id"`
		Raw         json.RawMessage `json:"-"`
	}

	if err := json.Unmarshal(message, &baseMsg); err != nil {
		logging.Get().Info("Error parsing message: %v", err)
		return
	}

	// Log parsed message details
	logging.Get().Debug("[wsmsg] Parsed message - session_id=%s message_type=%s user_id=%s",
		s.ID, baseMsg.MessageType, baseMsg.UserID)

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
		logging.Get().Info("Error parsing diagram operation: %v", err)
		return
	}

	// Record message metrics
	if GlobalPerformanceMonitor != nil {
		GlobalPerformanceMonitor.RecordMessage(s.ID, len(message), 0)
	}

	// Validate message
	if msg.User.UserId != client.UserID {
		logging.Get().Info("User ID mismatch in diagram operation: %s != %s", msg.User.UserId, client.UserID)
		return
	}

	// Check authorization (this will be implemented in the authorization filtering task)
	// Use email for permission check for backwards compatibility
	permissionCheckID := client.UserEmail
	if permissionCheckID == "" {
		permissionCheckID = client.UserID
	}
	if !s.checkMutationPermission(permissionCheckID) {
		s.sendAuthorizationDenied(client, msg.OperationID, "insufficient_permissions")

		// Send enhanced state correction for affected cells
		affectedCellIDs := extractCellIDs(msg.Operation.Cells)
		s.sendStateCorrectionWithReason(client, affectedCellIDs, "unauthorized_operation")
		return
	}

	// Check for out-of-order message delivery if client has a sequence number
	if msg.SequenceNumber != nil {
		s.mu.Lock()
		lastSeq, exists := s.clientLastSequence[client.UserID]
		expectedSeq := lastSeq + 1

		if exists && *msg.SequenceNumber != expectedSeq {
			if *msg.SequenceNumber < expectedSeq {
				logging.Get().Info("Duplicate or old message from %s: expected %d, got %d",
					client.UserID, expectedSeq, *msg.SequenceNumber)
				s.trackPotentialSyncIssue(client.UserID, "duplicate_message")
			} else {
				logging.Get().Info("Message gap detected from %s: expected %d, got %d (gap of %d)",
					client.UserID, expectedSeq, *msg.SequenceNumber, *msg.SequenceNumber-expectedSeq)
				s.trackPotentialSyncIssue(client.UserID, "message_gap")
			}
		}

		// Update client's last sequence number
		s.clientLastSequence[client.UserID] = *msg.SequenceNumber
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
		logging.Get().Info("Failed to process cell operation: %v", err)
		return
	}

	if !result.Valid {
		logging.Get().Info("Operation %s validation failed: %s", msg.OperationID, result.Reason)

		if result.CorrectionNeeded {
			s.sendStateCorrection(client, result.CellsModified)
		}
		return
	}

	if !result.StateChanged {
		logging.Get().Info("Operation %s resulted in no state changes", msg.OperationID)

		// Record operation performance even for no-op operations
		if GlobalPerformanceMonitor != nil {
			perf := &OperationPerformance{
				OperationID:      msg.OperationID,
				UserID:           msg.User.UserId,
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
			UserID:           msg.User.UserId,
			StartTime:        startTime,
			TotalTime:        time.Since(startTime),
			CellCount:        len(msg.Operation.Cells),
			StateChanged:     result.StateChanged,
			ConflictDetected: !result.Valid,
		}
		GlobalPerformanceMonitor.RecordOperation(perf)
	}

	logging.Get().Info("Successfully applied operation %s from %s with sequence %d",
		msg.OperationID, msg.User.UserId, *msg.SequenceNumber)

	// Broadcast to all other clients (not the sender)
	s.broadcastToOthers(client, msg)
}

// processPresenterRequest handles presenter mode requests
func (s *DiagramSession) processPresenterRequest(client *WebSocketClient, message []byte) {
	var msg PresenterRequestMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		logging.Get().Info("Error parsing presenter request: %v", err)
		return
	}

	// Validate user ID matches client
	if msg.User.UserId != client.UserID {
		logging.Get().Info("User ID mismatch in presenter request: %s != %s", msg.User.UserId, client.UserID)
		return
	}

	s.mu.RLock()
	currentPresenter := s.CurrentPresenter
	host := s.Host
	s.mu.RUnlock()

	// If user is already the presenter, ignore
	if msg.User.UserId == currentPresenter {
		logging.Get().Info("User %s is already the presenter", msg.User.UserId)
		return
	}

	// If user is the host, automatically grant presenter mode
	if msg.User.UserId == host {
		s.mu.Lock()
		s.CurrentPresenter = msg.User.UserId
		s.mu.Unlock()

		// Broadcast new presenter to all clients
		broadcastMsg := CurrentPresenterMessage{
			MessageType:      "current_presenter",
			CurrentPresenter: msg.User.UserId,
		}
		s.broadcastMessage(broadcastMsg)
		logging.Get().Info("Host %s became presenter in session %s", msg.User.UserId, s.ID)

		// Also broadcast updated participant list since presenter has changed
		s.broadcastParticipantsUpdate()
		return
	}

	// For non-hosts, notify the host of the presenter request
	// The host can then use change_presenter to grant or send presenter_denied to deny
	hostClient := s.findClientByUserID(host)
	if hostClient != nil {
		// Forward the request to the host for approval
		s.sendToClient(hostClient, msg)
		logging.Get().Info("Forwarded presenter request from %s to host %s in session %s", msg.User.UserId, host, s.ID)
	} else {
		logging.Get().Info("Host %s not connected, cannot process presenter request from %s", host, msg.User.UserId)

		// Send denial to requester since host is not available
		deniedMsg := PresenterDeniedMessage{
			MessageType: "presenter_denied",
			User: User{
				UserId: "system",
				Email:  "system@tmi",
				Name:   "System",
			},
			TargetUser: msg.User.UserId,
		}
		s.sendToClient(client, deniedMsg)
	}
}

// processChangePresenter handles host changing presenter
func (s *DiagramSession) processChangePresenter(client *WebSocketClient, message []byte) {
	var msg ChangePresenterMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		logging.Get().Info("Error parsing change presenter: %v", err)
		return
	}

	// Only host can change presenter
	s.mu.RLock()
	host := s.Host
	s.mu.RUnlock()

	if client.UserID != host {
		logging.Get().Info("Non-host attempted to change presenter: %s", client.UserID)
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
	logging.Get().Info("Host %s changed presenter to %s in session %s", client.UserID, msg.NewPresenter, s.ID)

	// Also broadcast updated participant list since presenter has changed
	s.broadcastParticipantsUpdate()
}

// processPresenterDenied handles host denying presenter requests
func (s *DiagramSession) processPresenterDenied(client *WebSocketClient, message []byte) {
	var msg PresenterDeniedMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		logging.Get().Info("Error parsing presenter denied: %v", err)
		return
	}

	// Only host can deny presenter requests
	s.mu.RLock()
	host := s.Host
	s.mu.RUnlock()

	if client.UserID != host {
		logging.Get().Info("Non-host attempted to deny presenter request: %s", client.UserID)
		return
	}

	// Validate user ID matches client (sender should be host)
	if msg.User.UserId != client.UserID {
		logging.Get().Info("User ID mismatch in presenter denied: %s != %s", msg.User.UserId, client.UserID)
		return
	}

	// Find the target user to send the denial
	targetClient := s.findClientByUserID(msg.TargetUser)
	if targetClient != nil {
		s.sendToClient(targetClient, msg)
		logging.Get().Info("Host %s denied presenter request from %s in session %s", msg.User.UserId, msg.TargetUser, s.ID)
	} else {
		logging.Get().Info("Target user %s not found for presenter denial in session %s", msg.TargetUser, s.ID)
	}
}

// processPresenterCursor handles cursor position updates
func (s *DiagramSession) processPresenterCursor(client *WebSocketClient, message []byte) {
	var msg PresenterCursorMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		logging.Get().Info("Error parsing presenter cursor: %v", err)
		return
	}

	// Validate user ID matches client
	if msg.User.UserId != client.UserID {
		logging.Get().Info("User ID mismatch in presenter cursor: %s != %s", msg.User.UserId, client.UserID)
		return
	}

	// Only current presenter can send cursor updates
	s.mu.RLock()
	currentPresenter := s.CurrentPresenter
	s.mu.RUnlock()

	if client.UserID != currentPresenter {
		logging.Get().Info("Non-presenter attempted to send cursor: %s", client.UserID)
		return
	}

	// Broadcast cursor to all other clients
	s.broadcastToOthers(client, msg)
}

// processPresenterSelection handles selection updates
func (s *DiagramSession) processPresenterSelection(client *WebSocketClient, message []byte) {
	var msg PresenterSelectionMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		logging.Get().Info("Error parsing presenter selection: %v", err)
		return
	}

	// Validate user ID matches client
	if msg.User.UserId != client.UserID {
		logging.Get().Info("User ID mismatch in presenter selection: %s != %s", msg.User.UserId, client.UserID)
		return
	}

	// Only current presenter can send selection updates
	s.mu.RLock()
	currentPresenter := s.CurrentPresenter
	s.mu.RUnlock()

	if client.UserID != currentPresenter {
		logging.Get().Info("Non-presenter attempted to send selection: %s", client.UserID)
		return
	}

	// Broadcast selection to all other clients
	s.broadcastToOthers(client, msg)
}

// processResyncRequest handles client resync requests
func (s *DiagramSession) processResyncRequest(client *WebSocketClient, message []byte) {
	var msg ResyncRequestMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		logging.Get().Info("Error parsing resync request: %v", err)
		return
	}

	// Validate user ID matches client
	if msg.User.UserId != client.UserID {
		logging.Get().Info("User ID mismatch in resync request: %s != %s", msg.User.UserId, client.UserID)
		return
	}

	logging.Get().Info("Client %s requested resync for diagram %s", client.UserID, s.DiagramID)

	// According to the plan, we use REST API for resync for simplicity
	// Send a message telling the client to use the REST endpoint for resync
	resyncResponse := ResyncResponseMessage{
		MessageType: "resync_response",
		User: User{
			UserId: "system",
			Email:  "system@tmi",
			Name:   "System",
		},
		TargetUser:    msg.User.UserId,
		Method:        "rest_api",
		DiagramID:     s.DiagramID,
		ThreatModelID: s.ThreatModelID,
	}

	s.sendToClient(client, resyncResponse)
	logging.Get().Info("Sent resync response to %s for diagram %s", msg.User.UserId, s.DiagramID)

	// Record performance metrics
	if GlobalPerformanceMonitor != nil {
		GlobalPerformanceMonitor.RecordResyncRequest(s.ID, msg.User.UserId)
	}
}

// processUndoRequest handles undo requests
func (s *DiagramSession) processUndoRequest(client *WebSocketClient, message []byte) {
	var msg UndoRequestMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		logging.Get().Info("Error parsing undo request: %v", err)
		return
	}

	// Validate user ID matches client
	if msg.User.UserId != client.UserID {
		logging.Get().Info("User ID mismatch in undo request: %s != %s", msg.User.UserId, client.UserID)
		return
	}

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
		logging.Get().Info("No operations to undo for user %s", client.UserID)
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
		logging.Get().Info("Failed to get undo operation for user %s", client.UserID)
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
		logging.Get().Info("Failed to apply undo state: %v", err)
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
	logging.Get().Info("Processed undo request from %s, reverted to sequence %d", client.UserID, entry.SequenceNumber-1)
}

// processRedoRequest handles redo requests
func (s *DiagramSession) processRedoRequest(client *WebSocketClient, message []byte) {
	var msg RedoRequestMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		logging.Get().Info("Error parsing redo request: %v", err)
		return
	}

	// Validate user ID matches client
	if msg.User.UserId != client.UserID {
		logging.Get().Info("User ID mismatch in redo request: %s != %s", msg.User.UserId, client.UserID)
		return
	}

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
		logging.Get().Info("No operations to redo for user %s", client.UserID)
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
		logging.Get().Info("Failed to get redo operation for user %s", client.UserID)
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
		logging.Get().Info("Failed to apply redo operation: %v", err)
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
	logging.Get().Info("Processed redo request from %s, restored to sequence %d", client.UserID, entry.SequenceNumber)
}

// processLegacyMessage handles backward compatibility with old message format
func (s *DiagramSession) processLegacyMessage(client *WebSocketClient, message []byte) {
	// Parse legacy message format
	var clientMsg struct {
		Operation json.RawMessage `json:"operation"`
	}
	if err := json.Unmarshal(message, &clientMsg); err != nil {
		logging.Get().Info("Error parsing legacy WebSocket message: %v", err)
		return
	}

	// Validate message size
	if len(clientMsg.Operation) > 1024*50 { // 50KB limit
		logging.Get().Info("Operation too large (%d bytes), ignoring", len(clientMsg.Operation))
		return
	}

	// Create server message
	msg := WebSocketMessage{
		Event:     "update",
		UserID:    client.UserID,
		Timestamp: time.Now().UTC(),
	}

	// Unmarshal operation
	var op DiagramOperation
	if err := json.Unmarshal(clientMsg.Operation, &op); err != nil {
		logging.Get().Info("Error parsing operation: %v", err)
		return
	}

	// Validate operation
	if err := validateDiagramOperation(op); err != nil {
		logging.Get().Info("Invalid diagram operation: %v", err)
		return
	}

	msg.Operation = op

	// Apply operation to the diagram
	if err := applyDiagramOperation(s.DiagramID, op); err != nil {
		logging.Get().Info("Error applying operation to diagram: %v", err)
		// Still broadcast the operation to maintain consistency
	}

	// Marshal and broadcast
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		logging.Get().Info("Error marshaling message: %v", err)
		return
	}

	s.Broadcast <- msgBytes
}

// Helper methods

// handlePresenterDisconnection handles when the current presenter leaves the session
func (s *DiagramSession) handlePresenterDisconnection(disconnectedUserID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	logging.Get().Info("Presenter %s disconnected from session %s, reassigning presenter", disconnectedUserID, s.ID)

	// Reset presenter according to the plan:
	// 1. First try to set presenter back to host
	// 2. If host has also left, set presenter to first remaining user with write permissions

	var newPresenter string

	// Check if host is still connected
	managerConnected := false
	for client := range s.Clients {
		if client.UserID == s.Host {
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
			logging.Get().Info("Failed to get threat model %s for presenter reassignment: %v", s.ThreatModelID, err)
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
			CurrentPresenter: newPresenter,
		}

		// Release the lock before broadcasting to avoid deadlock
		s.mu.Unlock()
		s.broadcastMessage(broadcastMsg)
		s.mu.Lock()

		logging.Get().Info("Set new presenter to %s in session %s after %s disconnected", newPresenter, s.ID, disconnectedUserID)
	} else {
		// No suitable presenter found, clear presenter
		s.CurrentPresenter = ""
		logging.Get().Info("No suitable presenter found for session %s after %s disconnected", s.ID, disconnectedUserID)
	}
}

// handleHostDisconnection handles when the host leaves
// This method broadcasts session termination messages and prepares for session cleanup
func (s *DiagramSession) handleHostDisconnection(disconnectedHostID string) {
	logging.Get().Info("Host %s disconnected from session %s, initiating session termination", disconnectedHostID, s.ID)

	// Update session state to terminating
	s.mu.Lock()
	s.State = SessionStateTerminating
	now := time.Now().UTC()
	s.TerminatedAt = &now
	s.mu.Unlock()

	// Broadcast session termination notification to all remaining participants
	s.broadcastSessionTermination(disconnectedHostID)

	// Give clients a brief moment to process the termination message
	time.Sleep(100 * time.Millisecond)

	// Close all remaining client connections gracefully
	s.mu.Lock()
	for client := range s.Clients {
		if client.UserID != disconnectedHostID { // Host already disconnected
			close(client.Send)
			logging.Get().Debug("Closed connection for participant %s due to session termination", client.UserID)
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

		logging.Get().Info("Session %s removed from hub after host disconnection", s.ID)

		// Record session end
		if GlobalPerformanceMonitor != nil {
			GlobalPerformanceMonitor.RecordSessionEnd(s.ID)
		}
	}

	logging.Get().Info("Session %s terminated due to host departure", s.ID)
}

// broadcastSessionTermination sends termination messages to all participants
func (s *DiagramSession) broadcastSessionTermination(hostID string) {
	// Create session termination message
	terminationMsg := WebSocketMessage{
		Event:     "session_ended",
		UserID:    hostID,
		Timestamp: time.Now().UTC(),
		Message:   "Session ended: host has left",
	}

	// Broadcast to all clients
	if msgBytes, err := json.Marshal(terminationMsg); err == nil {
		s.mu.RLock()
		for client := range s.Clients {
			if client.UserID != hostID { // Don't send to the disconnected host
				select {
				case client.Send <- msgBytes:
					logging.Get().Debug("Sent session termination message to %s", client.UserID)
				default:
					logging.Get().Warn("Failed to send session termination message to %s (channel full)", client.UserID)
				}
			}
		}
		s.mu.RUnlock()
	} else {
		logging.Get().Error("Failed to marshal session termination message: %v", err)
	}

	// Also send a final leave message for the host
	leaveMsg := WebSocketMessage{
		Event:     "leave",
		UserID:    hostID,
		Timestamp: time.Now().UTC(),
	}

	if msgBytes, err := json.Marshal(leaveMsg); err == nil {
		s.mu.RLock()
		for client := range s.Clients {
			if client.UserID != hostID {
				select {
				case client.Send <- msgBytes:
				default:
					// Ignore if channel is full - termination is more important
				}
			}
		}
		s.mu.RUnlock()
	}
}

// cleanupSession removes this session from the hub when it terminates
func (s *DiagramSession) cleanupSession() {
	if s.Hub != nil {
		s.Hub.mu.Lock()
		delete(s.Hub.Diagrams, s.DiagramID)
		s.Hub.mu.Unlock()

		logging.Get().Info("Session %s removed from hub after termination", s.ID)

		// Record session end
		if GlobalPerformanceMonitor != nil {
			GlobalPerformanceMonitor.RecordSessionEnd(s.ID)
		}
	}
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
			logging.Get().Error("Failed to broadcast participants update: broadcast channel full")
		}
	} else {
		logging.Get().Error("Failed to marshal participants update: %v", err)
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
		select {
		case client.Send <- msgBytes:
		default:
			logging.Get().Error("Failed to send participants update to client %s: send channel full", client.UserID)
		}
	} else {
		logging.Get().Error("Failed to marshal participants update for client: %v", err)
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

// sendStateCorrection sends the current state of specified cells to correct client state
func (s *DiagramSession) sendStateCorrection(client *WebSocketClient, affectedCellIDs []string) {
	s.sendStateCorrectionWithReason(client, affectedCellIDs, "operation_failed")
}

// sendStateCorrectionWithReason sends state correction with detailed logging and reason tracking
func (s *DiagramSession) sendStateCorrectionWithReason(client *WebSocketClient, affectedCellIDs []string, reason string) {
	if len(affectedCellIDs) == 0 {
		return
	}

	logging.Get().Info("Sending state correction to %s for cells %v (reason: %s)", client.UserID, affectedCellIDs, reason)

	// Check user permission level for enhanced messaging
	// Use email for permission check for backwards compatibility
	permissionCheckID := client.UserEmail
	if permissionCheckID == "" {
		permissionCheckID = client.UserID
	}
	userRole := s.getUserRole(permissionCheckID)
	s.sendEnhancedStateCorrection(client, affectedCellIDs, reason, userRole)
}

// sendEnhancedStateCorrection sends enhanced state correction with role-specific messaging
func (s *DiagramSession) sendEnhancedStateCorrection(client *WebSocketClient, affectedCellIDs []string, reason string, userRole Role) {

	// Get current diagram state
	diagram, err := DiagramStore.Get(s.DiagramID)
	if err != nil {
		logging.Get().Info("Error getting diagram for state correction: %v", err)
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
		s.logEnhancedStateCorrection(client.UserID, reason, userRole, correctionsSent, len(affectedCellIDs)-correctionsSent, totalCells)

		// Track correction frequency for potential sync issues
		s.trackCorrectionEvent(client.UserID, reason)

		// Record performance metrics
		if GlobalPerformanceMonitor != nil {
			GlobalPerformanceMonitor.RecordStateCorrection(s.ID, client.UserID, reason, len(correctionCells))
		}
	}
}

// logEnhancedStateCorrection provides detailed logging for state corrections
func (s *DiagramSession) logEnhancedStateCorrection(userID string, reason string, userRole Role, correctionsSent, deletionsSent, totalCells int) {
	roleStr := string(userRole)

	switch reason {
	case "unauthorized_operation":
		logging.Get().Warn("STATE CORRECTION [UNAUTHORIZED]: User %s (%s role) attempted unauthorized operation - sent %d cell corrections, %d deletions (total cells: %d)",
			userID, roleStr, correctionsSent, deletionsSent, totalCells)

		// Enhanced security logging for unauthorized operations
		if userRole == RoleReader {
			logging.Get().Error("SECURITY ALERT: Read-only user %s attempted to modify diagram %s", userID, s.DiagramID)
		}

	case "operation_failed":
		logging.Get().Warn("STATE CORRECTION [OPERATION_FAILED]: User %s (%s role) operation failed - sent %d cell corrections, %d deletions (total cells: %d)",
			userID, roleStr, correctionsSent, deletionsSent, totalCells)

	case "out_of_order_sequence", "duplicate_message", "message_gap":
		logging.Get().Warn("STATE CORRECTION [SYNC_ISSUE]: User %s (%s role) sync issue (%s) - sent %d cell corrections, %d deletions (total cells: %d)",
			userID, roleStr, reason, correctionsSent, deletionsSent, totalCells)

	default:
		logging.Get().Warn("STATE CORRECTION [%s]: User %s (%s role) - sent %d cell corrections, %d deletions (total cells: %d)",
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
		logging.Get().Warn("WARNING: User %s has received %d state corrections for reason '%s' - potential sync issue",
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
		logging.Get().Warn("WARNING: User %s has experienced %d '%s' issues - may need resync",
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
		logging.Get().Info("Cannot send resync recommendation: client %s not found", userID)
		return
	}

	// Send a resync response message to recommend the client resync via REST API
	resyncResponse := ResyncResponseMessage{
		MessageType: "resync_response",
		User: User{
			UserId: "system",
			Email:  "system@tmi",
			Name:   "System",
		},
		TargetUser:    userID,
		Method:        "rest_api",
		DiagramID:     s.DiagramID,
		ThreatModelID: s.ThreatModelID,
	}

	s.sendToClient(client, resyncResponse)
	logging.Get().Info("Sent automatic resync recommendation to %s due to %s issues", userID, issueType)
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
			logging.Get().Info("Warning: failed to convert cell %s: %v", cell.Id.String(), err)
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
		logging.Get().Info("Error marshaling broadcast message: %v", err)
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
			logging.Get().Info("Failed to send message to client %s", client.UserID)
		}
	}
}

// sendToClient sends a message to a specific client
func (s *DiagramSession) sendToClient(client *WebSocketClient, message interface{}) {
	msgBytes, err := json.Marshal(message)
	if err != nil {
		logging.Get().Info("Error marshaling message: %v", err)
		return
	}

	select {
	case client.Send <- msgBytes:
	default:
		logging.Get().Info("Client send channel full, dropping message")
	}
}

// broadcastMessage broadcasts a message to all clients
func (s *DiagramSession) broadcastMessage(message interface{}) {
	msgBytes, err := json.Marshal(message)
	if err != nil {
		logging.Get().Info("Error marshaling broadcast message: %v", err)
		return
	}

	s.Broadcast <- msgBytes
}

// broadcastToOthers broadcasts a message to all clients except the sender
func (s *DiagramSession) broadcastToOthers(sender *WebSocketClient, message interface{}) {
	msgBytes, err := json.Marshal(message)
	if err != nil {
		logging.Get().Info("Error marshaling message: %v", err)
		return
	}

	s.mu.RLock()
	for client := range s.Clients {
		if client != sender {
			select {
			case client.Send <- msgBytes:
			default:
				logging.Get().Info("Client send channel full, dropping message")
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
		logging.Get().Info("Failed to get diagram %s: %v", s.DiagramID, err)
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
		logging.Get().Info("Operation %s validation failed: %s", msg.OperationID, result.Reason)

		if result.CorrectionNeeded {
			s.sendStateCorrection(client, result.CellsModified)
		}

		return false
	}

	if !result.StateChanged {
		logging.Get().Info("Operation %s resulted in no state changes", msg.OperationID)
		return false // Don't broadcast no-op operations
	}

	// Update operation history
	s.addToHistory(msg, result.PreviousState, currentState)

	logging.Get().Info("Successfully applied operation %s from %s with sequence %d",
		msg.OperationID, msg.User.UserId, *msg.SequenceNumber)

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
		logging.Get().Info("Failed to save diagram after add operation: %v", err)
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
		logging.Get().Info("Failed to save diagram after update operation: %v", err)
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
			logging.Get().Info("Failed to save diagram after remove operation: %v", err)
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
		UserID:         msg.User.UserId,
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
		// Log WebSocket disconnection
		if c.Session != nil && c.Hub != nil {
			logging.LogWebSocketConnection("CONNECTION_CLOSED", c.Session.ID, c.UserID, c.Session.DiagramID, c.Hub.LoggingConfig)
		}
		c.Session.Unregister <- c
		if err := c.Conn.Close(); err != nil {
			logging.Get().Info("Error closing connection: %v", err)
		}
	}()

	c.Conn.SetReadLimit(4096) // 4KB message limit
	if err := c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		logging.Get().Info("Error setting read deadline: %v", err)
		return
	}
	c.Conn.SetPongHandler(func(string) error {
		if err := c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			logging.Get().Info("Error setting read deadline in pong handler: %v", err)
		}
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logging.Get().Info("WebSocket error: %v", err)
				// Log WebSocket error
				if c.Session != nil && c.Hub != nil {
					logging.LogWebSocketError("UNEXPECTED_CLOSE", err.Error(), c.Session.ID, c.UserID, c.Hub.LoggingConfig)
				}
			}
			break
		}

		// Update last activity timestamp
		c.LastActivity = time.Now().UTC()

		// Log inbound WebSocket message
		if c.Session != nil && c.Hub != nil {
			logging.LogWebSocketMessage(
				logging.WSMessageInbound,
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
			logging.Get().Info("Error closing connection: %v", err)
		}
	}()

	for {
		select {
		case message, ok := <-c.Send:
			if err := c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				logging.Get().Info("Error setting write deadline: %v", err)
				return
			}
			if !ok {
				// Hub closed the channel
				if err := c.Conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
					logging.Get().Info("Error writing close message: %v", err)
				}
				return
			}

			// Log outbound WebSocket message
			if c.Session != nil && c.Hub != nil {
				logging.LogWebSocketMessage(
					logging.WSMessageOutbound,
					c.Session.ID,
					c.UserName,
					"text",
					message,
					c.Hub.LoggingConfig,
				)
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if _, err := w.Write(message); err != nil {
				logging.Get().Info("Error writing message: %v", err)
				return
			}

			// Don't try to batch messages - it causes issues with JSON parsing
			// Each WebSocket message should be sent separately

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				logging.Get().Info("Error setting write deadline for ping: %v", err)
				return
			}
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
