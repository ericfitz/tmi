// Package api provides storage and HTTP handlers for the TMI service.
package api

import (
	"net/http"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// AdminAuditMiddleware writes a system_audit_entries row for every successful
// /admin/* write that has a descriptor entry. Audit-write failure is logged
// at Error level and does NOT fail the admin write (per spec §5).
//
// Order: this middleware MUST run after StepUpMiddleware and after the
// JWT-validation middleware that populates userEmail/userIdP/userInternalUUID/
// userID/userDisplayName on the Gin context.
func AdminAuditMiddleware(repo SystemAuditRepository, redactor Redactor, descriptors []auditDescriptor) gin.HandlerFunc {
	descByKey := map[string]auditDescriptor{}
	for _, d := range descriptors {
		descByKey[d.Method+" "+d.PathTpl] = d
	}
	return func(c *gin.Context) {
		// Resolve the descriptor by method + OpenAPI-form path.
		openAPIPath := ginPathToOpenAPI(c.FullPath())
		desc, ok := descByKey[c.Request.Method+" "+openAPIPath]
		if !ok {
			c.Next()
			return
		}

		// Capture before-state and request body before the handler mutates state.
		oldVal := desc.OldValueFn(c)
		body, err := readBodyForCapture(c)
		if err != nil {
			slogging.Get().WithContext(c).Error("admin audit: failed to read request body: %v", err)
			c.Next()
			return
		}

		c.Next()

		// Only audit on 2xx outcomes — non-2xx means the write did not take effect.
		status := c.Writer.Status()
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			return
		}

		fieldPath := desc.FieldPathFn(c)
		newVal := desc.NewValueFn(c, body)
		summary := desc.SummaryFn(c)

		providerUserID, _, provider, _ := GetUserAuthFieldsForAccessCheck(c)

		entry := models.SystemAuditEntry{
			ActorEmail:       c.GetString("userEmail"),
			ActorProvider:    models.DBVarchar(provider),
			ActorProviderID:  providerUserID,
			ActorDisplayName: c.GetString("userDisplayName"),
			HTTPMethod:       models.DBVarchar(c.Request.Method),
			HTTPPath:         c.FullPath(),
			FieldPath:        fieldPath,
			OldValueRedacted: nullableTextFromString(redactor.Redact(fieldPath, oldVal)),
			NewValueRedacted: nullableTextFromString(redactor.Redact(fieldPath, newVal)),
			ChangeSummary:    nullableTextFromString(summary),
		}

		if err := repo.Create(c.Request.Context(), entry); err != nil {
			slogging.Get().WithContext(c).Error(
				"system audit write failed: %v (actor=%s field=%s method=%s)",
				err, entry.ActorEmail, fieldPath, c.Request.Method)
		}
	}
}

// NewAdminAuditMiddleware constructs AdminAuditMiddleware with the canonical
// descriptor set for all /admin/* write operations (#355).
// This is the preferred entry point for external callers (e.g. cmd/server/main.go)
// so that adminAuditDescriptors stays unexported.
func NewAdminAuditMiddleware(repo SystemAuditRepository, redactor Redactor, reader SystemSettingReader) gin.HandlerFunc {
	return AdminAuditMiddleware(repo, redactor, adminAuditDescriptors(reader))
}

// nullableTextFromString constructs a NullableDBText with Valid=true from a
// non-empty string. Empty strings still construct as Valid=true (the redactor
// emits an "<empty>" sentinel for empty inputs at the verbatim tier, so a
// truly empty value should not reach this helper — but if it does, we keep
// Valid=true so PostgreSQL and Oracle both round-trip the same way).
func nullableTextFromString(s string) models.NullableDBText {
	return models.NullableDBText{String: s, Valid: true}
}
