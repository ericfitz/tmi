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

	// Identity and group claims used to authorize delivery of threat model scoped events
	User   ResolvedUser
	Groups []string

	// Hub reference
	Hub *NotificationHub

	// Connection metadata
	ConnectedAt time.Time
}

// NotificationHub manages all notification WebSocket connections
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

// registerClient adds a new client to the hub. It runs on the Run goroutine, so it
// delivers the user-joined event directly: sending it to the broadcast channel,
// which only Run drains, would deadlock the hub.
func (h *NotificationHub) registerClient(client *NotificationClient) {
	h.mu.Lock()
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
	h.mu.Unlock()

	h.broadcastMessage(joinNotification)
}

// unregisterClient removes a client from the hub. Like registerClient it runs on
// the Run goroutine and delivers the user-left event directly.
func (h *NotificationHub) unregisterClient(client *NotificationClient) {
	h.mu.Lock()
	if _, ok := h.clients[client.ID]; !ok {
		h.mu.Unlock()
		return
	}
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
	lastConnection := len(h.userClients[client.UserID]) == 0
	h.mu.Unlock()

	if lastConnection {
		h.broadcastMessage(&NotificationMessage{
			MessageType: NotificationUserLeft,
			UserID:      client.UserID,
			Timestamp:   time.Now().UTC(),
			Data: UserActivityData{
				UserEmail: client.UserEmail,
				UserName:  client.UserName,
			},
		})
	}
}

// broadcastMessage sends a message to all eligible clients
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

// isAuthorizedForMessage reports whether a client may see a message at all,
// independent of its subscription. Threat model and collaboration events go only
// to clients with reader access to the threat model, under the authorization
// captured when the event was emitted; an event without that snapshot, or of an
// unrecognized type, is delivered to no one.
func isAuthorizedForMessage(client *NotificationClient, message *NotificationMessage) bool {
	switch message.MessageType {
	case NotificationHeartbeat,
		NotificationSystemAnnouncement, NotificationSystemMaintenance, NotificationSystemUpdate,
		NotificationUserJoined, NotificationUserLeft:
		return true

	case NotificationThreatModelCreated, NotificationThreatModelUpdated, NotificationThreatModelDeleted, NotificationThreatModelShared,
		NotificationCollaborationStarted, NotificationCollaborationEnded, NotificationCollaborationInvite:
		if message.authz == nil {
			return false
		}
		return AccessCheckWithGroups(client.User, client.Groups, RoleReader, *message.authz)

	default:
		return false
	}
}

// shouldReceiveMessage checks if a client should receive a specific message
func (h *NotificationHub) shouldReceiveMessage(client *NotificationClient, message *NotificationMessage) bool {
	if !isAuthorizedForMessage(client, message) {
		return false
	}

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
func (h *NotificationHub) sendHeartbeat() {
	heartbeat := &NotificationMessage{
		MessageType: NotificationHeartbeat,
		UserID:      "system",
		Timestamp:   time.Now().UTC(),
	}
	h.broadcastMessage(heartbeat)
}

// newThreatModelNotification builds a threat model event carrying a snapshot of the
// threat model's authorization, so delivery can be limited to its readers even
// after the threat model is deleted.
func newThreatModelNotification(eventType NotificationMessageType, userID string, tm ThreatModel, action string) *NotificationMessage {
	authz := threatModelAuthorizationData(tm)
	tmID := ""
	if tm.Id != nil {
		tmID = tm.Id.String()
	}
	return &NotificationMessage{
		MessageType: eventType,
		UserID:      userID,
		Timestamp:   time.Now().UTC(),
		Data: ThreatModelNotificationData{
			ThreatModelID:   tmID,
			ThreatModelName: tm.Name,
			Action:          action,
		},
		authz: &authz,
	}
}

// newCollaborationNotification builds a diagram collaboration event carrying a
// snapshot of the parent threat model's authorization.
func newCollaborationNotification(eventType NotificationMessageType, userID, diagramID, diagramName string, tm ThreatModel, sessionID string) *NotificationMessage {
	authz := threatModelAuthorizationData(tm)
	tmID := ""
	if tm.Id != nil {
		tmID = tm.Id.String()
	}
	return &NotificationMessage{
		MessageType: eventType,
		UserID:      userID,
		Timestamp:   time.Now().UTC(),
		Data: CollaborationNotificationData{
			DiagramID:       diagramID,
			DiagramName:     diagramName,
			ThreatModelID:   tmID,
			ThreatModelName: tm.Name,
			SessionID:       sessionID,
		},
		authz: &authz,
	}
}

// BroadcastThreatModelEvent broadcasts a threat model event to clients with read access to it
func (h *NotificationHub) BroadcastThreatModelEvent(eventType NotificationMessageType, userID string, tm ThreatModel, action string) {
	h.broadcast <- newThreatModelNotification(eventType, userID, tm, action)
}

// BroadcastCollaborationEvent broadcasts a collaboration event to clients with read access to the threat model
func (h *NotificationHub) BroadcastCollaborationEvent(eventType NotificationMessageType, userID, diagramID, diagramName string, tm ThreatModel, sessionID string) {
	h.broadcast <- newCollaborationNotification(eventType, userID, diagramID, diagramName, tm, sessionID)
}

// BroadcastSystemNotification broadcasts a system notification to all connected clients
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
func (h *NotificationHub) GetConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.clients)
}
