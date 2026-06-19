package api

import (
	"time"
)

// NotificationMessageType represents the type of notification message
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: string enum of WebSocket notification event types (pure)
type NotificationMessageType string

const (
	// Threat model related notifications
	NotificationThreatModelCreated NotificationMessageType = "threat_model_created"
	NotificationThreatModelUpdated NotificationMessageType = "threat_model_updated"
	NotificationThreatModelDeleted NotificationMessageType = "threat_model_deleted"
	NotificationThreatModelShared  NotificationMessageType = "threat_model_shared"

	// Diagram collaboration notifications
	NotificationCollaborationStarted NotificationMessageType = "collaboration_started"
	NotificationCollaborationEnded   NotificationMessageType = "collaboration_ended"
	NotificationCollaborationInvite  NotificationMessageType = "collaboration_invite"

	// System notifications
	NotificationSystemAnnouncement NotificationMessageType = "system_announcement"
	NotificationSystemMaintenance  NotificationMessageType = "system_maintenance"
	NotificationSystemUpdate       NotificationMessageType = "system_update"

	// User activity notifications
	NotificationUserJoined NotificationMessageType = "user_joined"
	NotificationUserLeft   NotificationMessageType = "user_left"

	// Keep-alive
	NotificationHeartbeat NotificationMessageType = "heartbeat"
)

// NotificationMessage is the base structure for all notification messages
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: envelope for all WebSocket notification messages with type, actor, and payload (pure)
type NotificationMessage struct {
	MessageType NotificationMessageType `json:"message_type"`
	UserID      string                  `json:"user_id"` // internal_uuid of user who triggered the event
	Timestamp   time.Time               `json:"timestamp"`
	Data        any                     `json:"data,omitempty"` // Type-specific data
}

// ThreatModelNotificationData contains data for threat model notifications
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: payload for threat model lifecycle notifications (pure)
type ThreatModelNotificationData struct {
	ThreatModelID   string `json:"threat_model_id"`
	ThreatModelName string `json:"threat_model_name"`
	Action          string `json:"action"` // created, updated, deleted
}

// ThreatModelShareData contains data for threat model sharing notifications
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: payload for threat model sharing notifications including recipient and role (pure)
type ThreatModelShareData struct {
	ThreatModelID   string `json:"threat_model_id"`
	ThreatModelName string `json:"threat_model_name"`
	SharedWithEmail string `json:"shared_with_email"`
	Role            string `json:"role"` // reader, writer, owner
}

// CollaborationNotificationData contains data for collaboration notifications
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: payload for diagram collaboration session start/end notifications (pure)
type CollaborationNotificationData struct {
	DiagramID       string `json:"diagram_id"`
	DiagramName     string `json:"diagram_name,omitempty"`
	ThreatModelID   string `json:"threat_model_id"`
	ThreatModelName string `json:"threat_model_name,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
}

// CollaborationInviteData contains data for collaboration invitations
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: payload for collaboration invitation notifications including inviter and role (pure)
type CollaborationInviteData struct {
	DiagramID       string `json:"diagram_id"`
	DiagramName     string `json:"diagram_name,omitempty"`
	ThreatModelID   string `json:"threat_model_id"`
	ThreatModelName string `json:"threat_model_name,omitempty"`
	InviterEmail    string `json:"inviter_email"`
	Role            string `json:"role"` // viewer, writer
}

// SystemNotificationData contains data for system notifications
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: payload for system-wide announcements with severity and optional action URL (pure)
type SystemNotificationData struct {
	Severity       string `json:"severity"` // info, warning, error, critical
	Message        string `json:"message"`
	ActionRequired bool   `json:"action_required"`
	ActionURL      string `json:"action_url,omitempty"`
}

// UserActivityData contains data for user activity notifications
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: payload for user join/leave activity notifications (pure)
type UserActivityData struct {
	UserEmail string `json:"user_email"`
	UserName  string `json:"user_name,omitempty"`
}

// NotificationSubscription represents a user's notification preferences
// SEM@66b1e1515b82356913c8625edc8616772c3c70d3: user's notification preferences including subscribed types and resource filters (pure)
type NotificationSubscription struct {
	UserID             string                    `json:"user_id"`
	SubscribedTypes    []NotificationMessageType `json:"subscribed_types"`
	ThreatModelFilters []string                  `json:"threat_model_filters,omitempty"` // Specific threat model IDs to filter
	DiagramFilters     []string                  `json:"diagram_filters,omitempty"`      // Specific diagram IDs to filter
}
