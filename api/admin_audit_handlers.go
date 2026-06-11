package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// adminAuditPageLimit applies the AuditPageLimit parameter defaults/bounds.
func adminAuditPageLimit(p *AuditPageLimit) int {
	if p == nil {
		return 50
	}
	if *p < 1 {
		return 1
	}
	if *p > 100 {
		return 100
	}
	return *p
}

// ListSystemAuditEntries handles GET /admin/audit/system (#398).
func (s *Server) ListSystemAuditEntries(c *gin.Context, params ListSystemAuditEntriesParams) {
	logger := slogging.Get().WithContext(c)

	if s.systemAuditRepo == nil {
		logger.Error("systemAuditRepo is nil in ListSystemAuditEntries")
		HandleRequestError(c, ServerError("System audit repository not available"))
		return
	}

	var cursor *auditCursor
	if params.Cursor != nil {
		decoded, err := decodeAuditCursor(*params.Cursor)
		if err != nil {
			HandleRequestError(c, InvalidInputError("Invalid pagination cursor"))
			return
		}
		cursor = decoded
	}

	limit := adminAuditPageLimit(params.Limit)

	// AuditActorEmail is an alias for openapi_types.Email (a named string type),
	// so explicit conversion to string is required.
	var actorEmail *string
	if params.ActorEmail != nil {
		e := string(*params.ActorEmail)
		actorEmail = &e
	}

	var httpMethod *string
	if params.HttpMethod != nil {
		m := string(*params.HttpMethod)
		httpMethod = &m
	}

	var pathPrefix *string
	if params.PathPrefix != nil {
		pp := *params.PathPrefix
		pathPrefix = &pp
	}

	var fieldPath *string
	if params.FieldPath != nil {
		fp := *params.FieldPath
		fieldPath = &fp
	}

	var actorProvider *string
	if params.ActorProvider != nil {
		ap := *params.ActorProvider
		actorProvider = &ap
	}

	var createdAfter *time.Time
	if params.CreatedAfter != nil {
		ca := *params.CreatedAfter
		createdAfter = &ca
	}

	var createdBefore *time.Time
	if params.CreatedBefore != nil {
		cb := *params.CreatedBefore
		createdBefore = &cb
	}

	filter := SystemAuditFilter{
		ActorEmail:    actorEmail,
		ActorProvider: actorProvider,
		CreatedAfter:  createdAfter,
		CreatedBefore: createdBefore,
		HTTPMethod:    httpMethod,
		PathPrefix:    pathPrefix,
		FieldPath:     fieldPath,
		Limit:         limit,
		Cursor:        cursor,
	}

	rows, total, next, err := s.systemAuditRepo.List(c.Request.Context(), filter)
	if err != nil {
		logger.Error("Failed to list system audit entries: %v", err)
		HandleRequestError(c, ServerError("Failed to list system audit entries"))
		return
	}

	entries := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, systemAuditEntryToAPI(r))
	}
	writeAdminAuditJSON(c, logger, gin.H{
		"entries":     entries,
		"total":       total,
		"limit":       limit,
		"next_cursor": next,
	})
}

// GetSystemAuditEntry handles GET /admin/audit/system/{entry_id} (#398).
func (s *Server) GetSystemAuditEntry(c *gin.Context, entryId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	if s.systemAuditRepo == nil {
		logger.Error("systemAuditRepo is nil in GetSystemAuditEntry")
		HandleRequestError(c, ServerError("System audit repository not available"))
		return
	}

	row, err := s.systemAuditRepo.GetByID(c.Request.Context(), entryId.String())
	if err != nil {
		logger.Error("Failed to get system audit entry: %v", err)
		HandleRequestError(c, ServerError("Failed to get system audit entry"))
		return
	}
	if row == nil {
		HandleRequestError(c, NotFoundError("System audit entry not found"))
		return
	}
	writeAdminAuditJSON(c, logger, systemAuditEntryToAPI(*row))
}

// ListAdminThreatModelAuditEntries handles GET /admin/audit/threat_models (#398).
func (s *Server) ListAdminThreatModelAuditEntries(c *gin.Context, params ListAdminThreatModelAuditEntriesParams) {
	logger := slogging.Get().WithContext(c)

	if GlobalAuditService == nil {
		logger.Error("GlobalAuditService is nil in ListAdminThreatModelAuditEntries")
		HandleRequestError(c, ServerError("Audit service not available"))
		return
	}

	var cursor *auditCursor
	if params.Cursor != nil {
		decoded, err := decodeAuditCursor(*params.Cursor)
		if err != nil {
			HandleRequestError(c, InvalidInputError("Invalid pagination cursor"))
			return
		}
		cursor = decoded
	}

	limit := adminAuditPageLimit(params.Limit)

	var actorEmail *string
	if params.ActorEmail != nil {
		e := string(*params.ActorEmail)
		actorEmail = &e
	}

	var actorProvider *string
	if params.ActorProvider != nil {
		ap := *params.ActorProvider
		actorProvider = &ap
	}

	var createdAfter *time.Time
	if params.CreatedAfter != nil {
		ca := *params.CreatedAfter
		createdAfter = &ca
	}

	var createdBefore *time.Time
	if params.CreatedBefore != nil {
		cb := *params.CreatedBefore
		createdBefore = &cb
	}

	var changeType *string
	if params.ChangeType != nil {
		ct := string(*params.ChangeType)
		changeType = &ct
	}

	var objectType *string
	if params.ObjectType != nil {
		ot := string(*params.ObjectType)
		objectType = &ot
	}

	filters := &AuditFilters{
		ActorEmail:    actorEmail,
		ActorProvider: actorProvider,
		After:         createdAfter,
		Before:        createdBefore,
		ChangeType:    changeType,
		ObjectType:    objectType,
	}
	if params.ThreatModelId != nil {
		tm := params.ThreatModelId.String()
		filters.ThreatModelID = &tm
	}

	rows, total, next, err := GlobalAuditService.ListAuditEntriesAdmin(c.Request.Context(), limit, cursor, filters)
	if err != nil {
		logger.Error("Failed to list audit entries: %v", err)
		HandleRequestError(c, ServerError("Failed to list audit entries"))
		return
	}

	// Convert []AuditEntryResponse to []AuditEntry (generated type) for wire serialization
	apiEntries := toAPIAuditEntries(rows)
	writeAdminAuditJSON(c, logger, gin.H{
		"entries":     apiEntries,
		"total":       total,
		"limit":       limit,
		"next_cursor": next,
	})
}

// GetAdminThreatModelAuditEntry handles GET /admin/audit/threat_models/{entry_id} (#398).
func (s *Server) GetAdminThreatModelAuditEntry(c *gin.Context, entryId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	if GlobalAuditService == nil {
		logger.Error("GlobalAuditService is nil in GetAdminThreatModelAuditEntry")
		HandleRequestError(c, ServerError("Audit service not available"))
		return
	}

	entry, err := GlobalAuditService.GetAuditEntry(c.Request.Context(), entryId.String())
	if err != nil {
		logger.Error("Failed to get audit entry: %v", err)
		HandleRequestError(c, ServerError("Failed to get audit entry"))
		return
	}
	if entry == nil {
		HandleRequestError(c, NotFoundError("Audit entry not found"))
		return
	}
	writeAdminAuditJSON(c, logger, toAPIAuditEntry(*entry))
}

// writeAdminAuditJSON marshals explicitly so serialization errors return 500
// instead of a silent empty 200 (same rationale as ListAdminUsers).
func writeAdminAuditJSON(c *gin.Context, logger *slogging.ContextLogger, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("Failed to marshal admin audit response: %v", err)
		HandleRequestError(c, ServerError("Failed to serialize response"))
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

// systemAuditEntryToAPI converts a models.SystemAuditEntry to the API wire shape.
// NullableDBText fields are marshaled as JSON string or null via MarshalJSON.
func systemAuditEntryToAPI(e models.SystemAuditEntry) gin.H {
	return gin.H{
		"id": string(e.ID),
		"actor": gin.H{
			"email":        string(e.ActorEmail),
			"provider":     string(e.ActorProvider),
			"provider_id":  string(e.ActorProviderID),
			"display_name": string(e.ActorDisplayName),
		},
		"http_method":        string(e.HTTPMethod),
		"http_path":          string(e.HTTPPath),
		"field_path":         string(e.FieldPath),
		"old_value_redacted": e.OldValueRedacted.Ptr(),
		"new_value_redacted": e.NewValueRedacted.Ptr(),
		"change_summary":     e.ChangeSummary.Ptr(),
		"created_at":         e.CreatedAt.UTC(),
	}
}
