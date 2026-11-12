package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/errcode"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Global notification hub instance
var notificationHub *NotificationHub

// InitNotificationHub initializes the global notification hub
func InitNotificationHub() {
	if notificationHub == nil {
		notificationHub = NewNotificationHub()
		go notificationHub.Run()
		slogging.Get().Info("Notification hub initialized and running")
	}
}

// GetNotificationHub returns the global notification hub instance
func GetNotificationHub() *NotificationHub {
	return notificationHub
}

// HandleNotificationWebSocket handles WebSocket connections for notifications
func (s *Server) HandleNotificationWebSocket(c *gin.Context) {
	logger := slogging.Get()

	// Get user information from JWT context
	userEmailInterface, exists := c.Get("userEmail")
	if !exists {
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "User not authenticated")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            ErrorError(errcode.Unauthorized),
			ErrorDescription: "User not authenticated",
		})
		return
	}

	userEmail, ok := userEmailInterface.(string)
	if !ok {
		slogging.Get().WithContext(c).Error("Notification WebSocket: Invalid user context - userEmail is not a string (type: %T, value: %v)", userEmailInterface, userEmailInterface)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            ErrorError(errcode.ServerError),
			ErrorDescription: "Invalid user context",
		})
		return
	}

	// For now, use userEmail as both email and ID
	userID := userEmail

	// Identity and groups for authorizing delivery of threat model scoped events
	providerUserID, internalUUID, provider, groups := GetUserAuthFieldsForAccessCheck(c)
	resolvedUser := ResolvedUser{
		InternalUUID: internalUUID,
		Provider:     provider,
		ProviderID:   providerUserID,
		Email:        userEmail,
		DisplayName:  c.GetString("userDisplayName"),
	}

	// Upgrade HTTP connection to WebSocket
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	conn, err := withOriginCheck(c, upgrader).Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Error("Failed to upgrade HTTP connection to WebSocket for user %s: %v", userEmail, err)
		HandleRequestError(c, WithDetailCode(ServerError("Failed to upgrade connection"), errcode.DetailWebsocketUpgradeFailed))
		return
	}

	// Create new notification client
	client := &NotificationClient{
		ID:           uuid.New().String(),
		UserID:       userID,
		UserEmail:    userEmail,
		UserName:     userEmail,
		User:         resolvedUser,
		Groups:       groups,
		Conn:         conn,
		Send:         make(chan []byte, 256),
		Subscription: nil, // Start with no subscription (receives all messages it is authorized for)
		Hub:          notificationHub,
		ConnectedAt:  time.Now().UTC(),
	}

	// Register client with the hub
	notificationHub.register <- client

	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump()

	logger.Info("WebSocket notification connection established - user: %s", userEmail)
}

// readPump handles incoming messages from the client
func (c *NotificationClient) readPump() {
	defer func() {
		c.Hub.unregister <- c
		_ = c.Conn.Close()
	}()

	_ = c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		_ = c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slogging.Get().Error("WebSocket error: %v", err)
			}
			break
		}

		// Handle incoming messages (subscription updates, etc.)
		var msg map[string]any
		if err := json.Unmarshal(message, &msg); err != nil {
			slogging.Get().Warn("Invalid message from client %s: %v", c.ID, err)
			continue
		}

		// Handle subscription updates
		if msgType, ok := msg["message_type"].(string); ok && msgType == "update_subscription" {
			c.handleSubscriptionUpdate(msg)
		}
	}
}

// writePump handles sending messages to the client
func (c *NotificationClient) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// The hub closed the channel
				_ = c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			// Add queued messages to the current websocket message
			n := len(c.Send)
			for range n {
				_, _ = w.Write([]byte{'\n'})
				_, _ = w.Write(<-c.Send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleSubscriptionUpdate processes subscription update messages from the client
func (c *NotificationClient) handleSubscriptionUpdate(msg map[string]any) {
	logger := slogging.Get()

	// Extract subscription data
	if data, ok := msg["data"].(map[string]any); ok {
		subscription := &NotificationSubscription{
			UserID: c.UserID,
		}

		// Extract subscribed types
		if types, ok := data["subscribed_types"].([]any); ok {
			for _, t := range types {
				if typeStr, ok := t.(string); ok {
					subscription.SubscribedTypes = append(subscription.SubscribedTypes, NotificationMessageType(typeStr))
				}
			}
		}

		// Extract threat model filters
		if tmFilters, ok := data["threat_model_filters"].([]any); ok {
			for _, f := range tmFilters {
				if filterStr, ok := f.(string); ok {
					subscription.ThreatModelFilters = append(subscription.ThreatModelFilters, filterStr)
				}
			}
		}

		// Extract diagram filters
		if dgFilters, ok := data["diagram_filters"].([]any); ok {
			for _, f := range dgFilters {
				if filterStr, ok := f.(string); ok {
					subscription.DiagramFilters = append(subscription.DiagramFilters, filterStr)
				}
			}
		}

		// Update client subscription
		c.Subscription = subscription
		logger.Info("Updated subscription for client %s - types: %d, tm_filters: %d, diagram_filters: %d",
			c.ID, len(subscription.SubscribedTypes), len(subscription.ThreatModelFilters), len(subscription.DiagramFilters))

		// Send confirmation
		confirmMsg := NotificationMessage{
			MessageType: "subscription_updated",
			UserID:      "system",
			Timestamp:   time.Now().UTC(),
			Data: map[string]any{
				"message": "Subscription updated successfully",
			},
		}
		if msgBytes, err := json.Marshal(confirmMsg); err == nil {
			c.Send <- msgBytes
		}
	}
}

// BroadcastThreatModelCreated notifies connected clients that can read the threat model that it was created
func BroadcastThreatModelCreated(userID string, tm ThreatModel) {
	if notificationHub != nil {
		notificationHub.BroadcastThreatModelEvent(NotificationThreatModelCreated, userID, tm, "created")
	}
}

// BroadcastThreatModelUpdated notifies connected clients that can read the threat model that it was updated
func BroadcastThreatModelUpdated(userID string, tm ThreatModel) {
	if notificationHub != nil {
		notificationHub.BroadcastThreatModelEvent(NotificationThreatModelUpdated, userID, tm, "updated")
	}
}

// BroadcastThreatModelDeleted notifies connected clients that can read the threat model that it was deleted
func BroadcastThreatModelDeleted(userID string, tm ThreatModel) {
	if notificationHub != nil {
		notificationHub.BroadcastThreatModelEvent(NotificationThreatModelDeleted, userID, tm, "deleted")
	}
}

// BroadcastCollaborationStarted notifies clients that can read the threat model about a new collaboration session
func BroadcastCollaborationStarted(userID, diagramID, diagramName string, tm ThreatModel, sessionID string) {
	if notificationHub != nil {
		notificationHub.BroadcastCollaborationEvent(NotificationCollaborationStarted, userID, diagramID, diagramName, tm, sessionID)
	}
}

// BroadcastSystemAnnouncement sends a system-wide announcement
func BroadcastSystemAnnouncement(message string, severity string, actionRequired bool, actionURL string) {
	if notificationHub != nil {
		notificationHub.BroadcastSystemNotification(severity, message, actionRequired, actionURL)
	}
}
