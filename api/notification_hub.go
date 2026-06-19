package api

import (
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gorilla/websocket"
)

// NotificationClient represents a client connected to the notification hub
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: represents a connected WebSocket notification subscriber with its user info and subscription
type NotificationClient struct {
	// Unique identifier for the client
	ID string

	// User information
	UserID    string
	UserEmail string
	UserName  string

	// WebSocket connection
	Conn *websocket.Conn

	// Send channel for messages
	Send chan []byte

	// Subscription preferences
	Subscription *NotificationSubscription

	// Hub reference
	Hub *NotificationHub

	// Connection metadata
	ConnectedAt time.Time
}

// NotificationHub manages all notification WebSocket connections
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: central broker that routes notification messages to subscribed WebSocket clients (mutates shared state)
type NotificationHub struct {
	// Registered clients by client ID
	clients map[string]*NotificationClient

	// User ID to client IDs mapping (one user can have multiple connections)
	userClients map[string]map[string]*NotificationClient

	// Channel for client registration
	register chan *NotificationClient

	// Channel for client unregistration
	unregister chan *NotificationClient

	// Channel for broadcasting messages
	broadcast chan *NotificationMessage

	// Mutex for thread-safe operations
	mu sync.RWMutex

	// Logger
	logger *slogging.Logger
}

// NewNotificationHub creates a new notification hub
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: build an empty NotificationHub ready to dispatch messages
func NewNotificationHub() *NotificationHub {
	return &NotificationHub{
		clients:     make(map[string]*NotificationClient),
		userClients: make(map[string]map[string]*NotificationClient),
		register:    make(chan *NotificationClient),
		unregister:  make(chan *NotificationClient),
		broadcast:   make(chan *NotificationMessage),
		logger:      slogging.Get(),
	}
}

// Run starts the notification hub
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: process client registration, unregistration, broadcasts, and periodic heartbeats in a loop (mutates shared state)
func (h *NotificationHub) Run() {
	ticker := time.NewTicker(30 * time.Second) // Heartbeat every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case client := <-h.register:
			h.registerClient(client)

		case client := <-h.unregister:
			h.unregisterClient(client)

		case message := <-h.broadcast:
			h.broadcastMessage(message)

		case <-ticker.C:
			h.sendHeartbeat()
		}
	}
}

// registerClient adds a new client to the hub
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: add a client to the hub and broadcast a user-joined event (mutates shared state)
func (h *NotificationHub) registerClient(client *NotificationClient) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.clients[client.ID] = client

	// Add to user clients mapping
	if h.userClients[client.UserID] == nil {
		h.userClients[client.UserID] = make(map[string]*NotificationClient)
	}
	h.userClients[client.UserID][client.ID] = client

	h.logger.Info("Notification client registered - client_id: %s, user_id: %s, user_email: %s",
		client.ID, client.UserID, client.UserEmail)

	// Send user joined notification
	joinNotification := &NotificationMessage{
		MessageType: NotificationUserJoined,
		UserID:      client.UserID,
		Timestamp:   time.Now().UTC(),
		Data: UserActivityData{
			UserEmail: client.UserEmail,
			UserName:  client.UserName,
		},
	}
	h.broadcast <- joinNotification
}

// unregisterClient removes a client from the hub
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: remove a client from the hub and broadcast a user-left event if no connections remain (mutates shared state)
func (h *NotificationHub) unregisterClient(client *NotificationClient) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[client.ID]; ok {
		delete(h.clients, client.ID)

		// Remove from user clients mapping
		if userClients, ok := h.userClients[client.UserID]; ok {
			delete(userClients, client.ID)
			if len(userClients) == 0 {
				delete(h.userClients, client.UserID)
			}
		}

		close(client.Send)

		h.logger.Info("Notification client unregistered - client_id: %s, user_id: %s",
			client.ID, client.UserID)

		// Send user left notification only if no other connections exist for this user
		if len(h.userClients[client.UserID]) == 0 {
			leaveNotification := &NotificationMessage{
				MessageType: NotificationUserLeft,
				UserID:      client.UserID,
				Timestamp:   time.Now().UTC(),
				Data: UserActivityData{
					UserEmail: client.UserEmail,
					UserName:  client.UserName,
				},
			}
			h.broadcast <- leaveNotification
		}
	}
}

// broadcastMessage sends a message to all eligible clients
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: dispatch a message to every eligible subscribed client, dropping slow ones (mutates shared state)
func (h *NotificationHub) broadcastMessage(message *NotificationMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	msgBytes, err := json.Marshal(message)
	if err != nil {
		h.logger.Error("Failed to marshal notification message: %v", err)
		return
	}

	// Send to all clients that are subscribed to this message type
	for _, client := range h.clients {
		if h.shouldReceiveMessage(client, message) {
			select {
			case client.Send <- msgBytes:
			default:
				// Client's send channel is full, close it
				h.logger.Warn("Client send channel full, closing connection - client_id: %s", client.ID)
				go func(c *NotificationClient) {
					h.unregister <- c
				}(client)
			}
		}
	}
}

// shouldReceiveMessage checks if a client should receive a specific message
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: check whether a client's subscription includes a given message type and filters (pure)
func (h *NotificationHub) shouldReceiveMessage(client *NotificationClient, message *NotificationMessage) bool {
	// Everyone gets heartbeats
	if message.MessageType == NotificationHeartbeat {
		return true
	}

	// Check if client has a subscription
	if client.Subscription == nil {
		// No subscription means receive all messages (default behavior)
		return true
	}

	// Check if message type is in subscribed types
	subscribed := slices.Contains(client.Subscription.SubscribedTypes, message.MessageType)

	if !subscribed {
		return false
	}

	// Apply filters based on message type
	switch message.MessageType {
	case NotificationThreatModelCreated, NotificationThreatModelUpdated, NotificationThreatModelDeleted, NotificationThreatModelShared:
		// Check threat model filters
		if len(client.Subscription.ThreatModelFilters) > 0 {
			if data, ok := message.Data.(ThreatModelNotificationData); ok {
				return slices.Contains(client.Subscription.ThreatModelFilters, data.ThreatModelID)
			}
			if data, ok := message.Data.(ThreatModelShareData); ok {
				return slices.Contains(client.Subscription.ThreatModelFilters, data.ThreatModelID)
			}
		}

	case NotificationCollaborationStarted, NotificationCollaborationEnded, NotificationCollaborationInvite:
		// Check diagram filters
		if len(client.Subscription.DiagramFilters) > 0 {
			if data, ok := message.Data.(CollaborationNotificationData); ok {
				return slices.Contains(client.Subscription.DiagramFilters, data.DiagramID)
			}
			if data, ok := message.Data.(CollaborationInviteData); ok {
				return slices.Contains(client.Subscription.DiagramFilters, data.DiagramID)
			}
		}
	}

	return true
}

// sendHeartbeat sends a heartbeat message to all connected clients
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: broadcast a heartbeat message to all connected notification clients (mutates shared state)
func (h *NotificationHub) sendHeartbeat() {
	heartbeat := &NotificationMessage{
		MessageType: NotificationHeartbeat,
		UserID:      "system",
		Timestamp:   time.Now().UTC(),
	}
	h.broadcastMessage(heartbeat)
}

// BroadcastThreatModelEvent broadcasts a threat model event to all connected clients
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: dispatch a threat model lifecycle event to all subscribed notification clients
func (h *NotificationHub) BroadcastThreatModelEvent(eventType NotificationMessageType, userID string, tmID, tmName, action string) {
	notification := &NotificationMessage{
		MessageType: eventType,
		UserID:      userID,
		Timestamp:   time.Now().UTC(),
		Data: ThreatModelNotificationData{
			ThreatModelID:   tmID,
			ThreatModelName: tmName,
			Action:          action,
		},
	}
	h.broadcast <- notification
}

// BroadcastCollaborationEvent broadcasts a collaboration event to all connected clients
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: dispatch a diagram collaboration event to all subscribed notification clients
func (h *NotificationHub) BroadcastCollaborationEvent(eventType NotificationMessageType, userID, diagramID, diagramName, tmID, tmName, sessionID string) {
	notification := &NotificationMessage{
		MessageType: eventType,
		UserID:      userID,
		Timestamp:   time.Now().UTC(),
		Data: CollaborationNotificationData{
			DiagramID:       diagramID,
			DiagramName:     diagramName,
			ThreatModelID:   tmID,
			ThreatModelName: tmName,
			SessionID:       sessionID,
		},
	}
	h.broadcast <- notification
}

// BroadcastSystemNotification broadcasts a system notification to all connected clients
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: dispatch a system-level announcement to all connected notification clients
func (h *NotificationHub) BroadcastSystemNotification(severity, message string, actionRequired bool, actionURL string) {
	notification := &NotificationMessage{
		MessageType: NotificationSystemAnnouncement,
		UserID:      "system",
		Timestamp:   time.Now().UTC(),
		Data: SystemNotificationData{
			Severity:       severity,
			Message:        message,
			ActionRequired: actionRequired,
			ActionURL:      actionURL,
		},
	}
	h.broadcast <- notification
}

// GetConnectedUsers returns a list of currently connected user IDs
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: list user IDs that have at least one active notification connection (reads DB)
func (h *NotificationHub) GetConnectedUsers() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	users := make([]string, 0, len(h.userClients))
	for userID := range h.userClients {
		users = append(users, userID)
	}
	return users
}

// GetConnectionCount returns the total number of active connections
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: return the total number of active notification client connections (pure)
func (h *NotificationHub) GetConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.clients)
}
