package api

import (
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// AuditLogger provides standardized audit logging for admin operations
type AuditLogger struct {
	logger *slogging.Logger
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger() *AuditLogger {
	return &AuditLogger{
		logger: slogging.Get(),
	}
}

// AuditContext contains the actor information for audit logs
type AuditContext struct {
	ActorUserID string
	ActorEmail  string
}

// ExtractAuditContext extracts actor information from the Gin context
func ExtractAuditContext(c *gin.Context) *AuditContext {
	return &AuditContext{
		ActorUserID: c.GetString("userInternalUUID"),
		ActorEmail:  c.GetString("userEmail"),
	}
}

// LogAction logs an audit event with standardized format
func (a *AuditLogger) LogAction(ctx *AuditContext, action string, details map[string]interface{}) {
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
func (a *AuditLogger) LogCreate(ctx *AuditContext, entityType string, entityID string, details map[string]interface{}) {
	if details == nil {
		details = make(map[string]interface{})
	}
	details["entity_type"] = entityType
	details["entity_id"] = entityID

	a.LogAction(ctx, fmt.Sprintf("%s created", entityType), details)
}

// LogUpdate logs an entity update event
func (a *AuditLogger) LogUpdate(ctx *AuditContext, entityType string, entityID string, changes []string) {
	details := map[string]interface{}{
		"entity_type": entityType,
		"entity_id":   entityID,
		"changes":     fmt.Sprintf("[%s]", joinStrings(changes, ", ")),
	}

	a.LogAction(ctx, fmt.Sprintf("%s updated", entityType), details)
}

// LogDelete logs an entity deletion event
func (a *AuditLogger) LogDelete(ctx *AuditContext, entityType string, entityID string, details map[string]interface{}) {
	if details == nil {
		details = make(map[string]interface{})
	}
	details["entity_type"] = entityType
	details["entity_id"] = entityID

	a.LogAction(ctx, fmt.Sprintf("%s deleted", entityType), details)
}

// LogUserDeletion logs a user deletion event with transfer and deletion counts
func (a *AuditLogger) LogUserDeletion(ctx *AuditContext, provider string, providerUserID string, email string, transferred int, deleted int) {
	details := map[string]interface{}{
		"provider":         provider,
		"provider_user_id": providerUserID,
		"email":            email,
		"transferred":      transferred,
		"deleted":          deleted,
	}

	a.LogAction(ctx, "Admin user deletion", details)
}

// LogGroupMemberAdded logs a group member addition event
func (a *AuditLogger) LogGroupMemberAdded(ctx *AuditContext, groupUUID string, userUUID string, userEmail string) {
	details := map[string]interface{}{
		"group_uuid": groupUUID,
		"user_uuid":  userUUID,
		"user_email": userEmail,
	}

	a.LogAction(ctx, "Group member added", details)
}

// LogGroupMemberRemoved logs a group member removal event
func (a *AuditLogger) LogGroupMemberRemoved(ctx *AuditContext, groupUUID string, userUUID string) {
	details := map[string]interface{}{
		"group_uuid": groupUUID,
		"user_uuid":  userUUID,
	}

	a.LogAction(ctx, "Group member removed", details)
}

// joinStrings is a helper to join string slices
func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}
