package api

import (
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// AuditLogger provides standardized audit logging for admin operations
// SEM@a5548be4c61d9f98ed2f3edd998abd909cd5f4ab: structured audit logger for recording admin actions with actor context (pure)
type AuditLogger struct {
	logger *slogging.Logger
}

// NewAuditLogger creates a new audit logger
// SEM@a5548be4c61d9f98ed2f3edd998abd909cd5f4ab: build an AuditLogger backed by the global structured logger (pure)
func NewAuditLogger() *AuditLogger {
	return &AuditLogger{
		logger: slogging.Get(),
	}
}

// AuditContext contains the actor information for audit logs
// SEM@a5548be4c61d9f98ed2f3edd998abd909cd5f4ab: actor identity (user ID and email) extracted for audit log entries (pure)
type AuditContext struct {
	ActorUserID string
	ActorEmail  string
}

// ExtractAuditContext extracts actor information from the Gin context
// SEM@a5548be4c61d9f98ed2f3edd998abd909cd5f4ab: extract the authenticated actor's identity from a Gin request context (pure)
func ExtractAuditContext(c *gin.Context) *AuditContext {
	return &AuditContext{
		ActorUserID: c.GetString("userInternalUUID"),
		ActorEmail:  c.GetString("userEmail"),
	}
}

// LogAction logs an audit event with standardized format
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: emit a structured audit log entry for an action with actor and detail fields (pure)
func (a *AuditLogger) LogAction(ctx *AuditContext, action string, details map[string]any) {
	// Build details string from map
	detailParts := make([]string, 0, len(details))
	for key, value := range details {
		detailParts = append(detailParts, fmt.Sprintf("%s=%v", key, value))
	}

	detailsStr := ""
	if len(detailParts) > 0 {
		detailsStr = ", " + joinStrings(detailParts, ", ")
	}

	a.logger.Info("[AUDIT] %s, actor=%s (email=%s)%s",
		action, ctx.ActorUserID, ctx.ActorEmail, detailsStr)
}

// LogCreate logs an entity creation event
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: emit an audit log entry recording creation of a named entity (pure)
func (a *AuditLogger) LogCreate(ctx *AuditContext, entityType string, entityID string, details map[string]any) {
	if details == nil {
		details = make(map[string]any)
	}
	details["entity_type"] = entityType
	details["entity_id"] = entityID

	a.LogAction(ctx, fmt.Sprintf("%s created", entityType), details)
}

// LogUpdate logs an entity update event
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: emit an audit log entry recording which fields of an entity were updated (pure)
func (a *AuditLogger) LogUpdate(ctx *AuditContext, entityType string, entityID string, changes []string) {
	details := map[string]any{
		"entity_type": entityType,
		"entity_id":   entityID,
		"changes":     fmt.Sprintf("[%s]", joinStrings(changes, ", ")),
	}

	a.LogAction(ctx, fmt.Sprintf("%s updated", entityType), details)
}

// LogDelete logs an entity deletion event
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: emit an audit log entry recording deletion of a named entity (pure)
func (a *AuditLogger) LogDelete(ctx *AuditContext, entityType string, entityID string, details map[string]any) {
	if details == nil {
		details = make(map[string]any)
	}
	details["entity_type"] = entityType
	details["entity_id"] = entityID

	a.LogAction(ctx, fmt.Sprintf("%s deleted", entityType), details)
}

// LogUserDeletion logs a user deletion event with transfer and deletion counts
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: emit an audit log entry for an admin user deletion with transfer and deletion counts (pure)
func (a *AuditLogger) LogUserDeletion(ctx *AuditContext, provider string, providerUserID string, email string, transferred int, deleted int) {
	details := map[string]any{
		"provider":         provider,
		"provider_user_id": providerUserID,
		"email":            email,
		"transferred":      transferred,
		"deleted":          deleted,
	}

	a.LogAction(ctx, "Admin user deletion", details)
}

// LogGroupMemberAdded logs a group member addition event
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: emit an audit log entry recording a user being added to a group (pure)
func (a *AuditLogger) LogGroupMemberAdded(ctx *AuditContext, groupUUID string, userUUID string, userEmail string) {
	details := map[string]any{
		"group_uuid": groupUUID,
		"user_uuid":  userUUID,
		"user_email": userEmail,
	}

	a.LogAction(ctx, "Group member added", details)
}

// LogGroupMemberRemoved logs a group member removal event
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: emit an audit log entry recording a user being removed from a group (pure)
func (a *AuditLogger) LogGroupMemberRemoved(ctx *AuditContext, groupUUID string, userUUID string) {
	details := map[string]any{
		"group_uuid": groupUUID,
		"user_uuid":  userUUID,
	}

	a.LogAction(ctx, "Group member removed", details)
}

// joinStrings is a helper to join string slices
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: join a string slice with a separator into a single string (pure)
func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	var result strings.Builder
	result.WriteString(parts[0])
	for i := 1; i < len(parts); i++ {
		result.WriteString(sep + parts[i])
	}
	return result.String()
}
