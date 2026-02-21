package api

import (
	"time"
)

// NotificationMessageType represents the type of notification message
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
type NotificationMessage struct {
	MessageType NotificationMessageType `json:"message_type"`
	UserID      string                  `json:"user_id"` // internal_uuid of user who triggered the event
	Timestamp   time.Time               `json:"timestamp"`
	Data        any                     `json:"data,omitempty"` // Type-specific data
}

// ThreatModelNotificationData contains data for threat model notifications
type ThreatModelNotificationData struct {
	ThreatModelID   string `json:"threat_model_id"`
	ThreatModelName string `json:"threat_model_name"`
	Action          string `json:"action"` // created, updated, deleted
}

// ThreatModelShareData contains data for threat model sharing notifications
type ThreatModelShareData struct {
	ThreatModelID   string `json:"threat_model_id"`
	ThreatModelName string `json:"threat_model_name"`
	SharedWithEmail string `json:"shared_with_email"`
	Role            string `json:"role"` // reader, writer, owner
}

// CollaborationNotificationData contains data for collaboration notifications
type CollaborationNotificationData struct {
	DiagramID       string `json:"diagram_id"`
	DiagramName     string `json:"diagram_name,omitempty"`
	ThreatModelID   string `json:"threat_model_id"`
	ThreatModelName string `json:"threat_model_name,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
}

// CollaborationInviteData contains data for collaboration invitations
type CollaborationInviteData struct {
	DiagramID       string `json:"diagram_id"`
	DiagramName     string `json:"diagram_name,omitempty"`
	ThreatModelID   string `json:"threat_model_id"`
	ThreatModelName string `json:"threat_model_name,omitempty"`
	InviterEmail    string `json:"inviter_email"`
	Role            string `json:"role"` // viewer, writer
}

// SystemNotificationData contains data for system notifications
type SystemNotificationData struct {
	Severity       string `json:"severity"` // info, warning, error, critical
	Message        string `json:"message"`
	ActionRequired bool   `json:"action_required"`
	ActionURL      string `json:"action_url,omitempty"`
}

// UserActivityData contains data for user activity notifications
type UserActivityData struct {
	UserEmail string `json:"user_email"`
	UserName  string `json:"user_name,omitempty"`
}

// NotificationSubscription represents a user's notification preferences
type NotificationSubscription struct {
	UserID             string                    `json:"user_id"`
	SubscribedTypes    []NotificationMessageType `json:"subscribed_types"`
	ThreatModelFilters []string                  `json:"threat_model_filters,omitempty"` // Specific threat model IDs to filter
	DiagramFilters     []string                  `json:"diagram_filters,omitempty"`      // Specific diagram IDs to filter
}
