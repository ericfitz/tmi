package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// AuditHandler provides handlers for audit trail and rollback operations.
type AuditHandler struct {
	auditService AuditServiceInterface
}

// NewAuditHandler creates a new audit handler.
func NewAuditHandler(auditService AuditServiceInterface) *AuditHandler {
	return &AuditHandler{
		auditService: auditService,
	}
}

// GoneError creates a RequestError for resources that have been pruned/removed.
func GoneError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusGone,
		Code:    "gone",
		Message: message,
	}
}

// GetThreatModelAuditTrail lists audit entries for a threat model and all sub-objects.
func (h *AuditHandler) GetThreatModelAuditTrail(c *gin.Context, threatModelId ThreatModelId, params GetThreatModelAuditTrailParams) {
	logger := slogging.Get().WithContext(c)
	logger.Debug("[HANDLER] GetThreatModelAuditTrail called for TM: %s", threatModelId)

	if h.auditService == nil {
		logger.Error("[HANDLER] auditService is nil in GetThreatModelAuditTrail")
		HandleRequestError(c, ServerError("Audit service is not available"))
		return
	}

	// Validate authentication
	_, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Verify threat model exists and user has at least reader access
	_, err = h.validateThreatModelAccess(c, threatModelId.String())
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	limit, offset := parsePaginationParams(params.Limit, params.Offset)

	filters := buildAuditFilters(params.ObjectType, params.ChangeType, params.ActorEmail, params.After, params.Before)

	entries, total, err := h.auditService.GetThreatModelAuditTrail(c.Request.Context(), threatModelId.String(), offset, limit, filters)
	if err != nil {
		HandleRequestError(c, ServerError(fmt.Sprintf("Failed to get audit trail: %v", err)))
		return
	}

	apiEntries := toAPIAuditEntries(entries)
	if apiEntries == nil {
		apiEntries = []AuditEntry{}
	}

	c.JSON(http.StatusOK, ListAuditTrailResponse{
		AuditEntries: apiEntries,
		Total:        total,
		Limit:        limit,
		Offset:       offset,
	})
}

// GetAuditEntry returns a single audit entry.
func (h *AuditHandler) GetAuditEntry(c *gin.Context, threatModelId ThreatModelId, entryId AuditEntryId) {
	slogging.Get().WithContext(c).Debug("[HANDLER] GetAuditEntry called for entry: %s", entryId)

	_, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	_, err = h.validateThreatModelAccess(c, threatModelId.String())
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	entry, err := h.auditService.GetAuditEntry(c.Request.Context(), entryId.String())
	if err != nil {
		HandleRequestError(c, ServerError(fmt.Sprintf("Failed to get audit entry: %v", err)))
		return
	}
	if entry == nil {
		HandleRequestError(c, NotFoundError("Audit entry not found"))
		return
	}

	// Verify entry belongs to this threat model
	if entry.ThreatModelID != threatModelId.String() {
		HandleRequestError(c, NotFoundError("Audit entry not found in this threat model"))
		return
	}

	c.JSON(http.StatusOK, toAPIAuditEntry(*entry))
}

// RollbackToVersion restores an entity to a previous version.
func (h *AuditHandler) RollbackToVersion(c *gin.Context, threatModelId ThreatModelId, entryId AuditEntryId) {
	slogging.Get().WithContext(c).Debug("[HANDLER] RollbackToVersion called for entry: %s", entryId)

	_, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Rollback requires writer role
	_, err = h.validateThreatModelWriteAccess(c, threatModelId.String())
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Get the audit entry
	entry, err := h.auditService.GetAuditEntry(c.Request.Context(), entryId.String())
	if err != nil {
		HandleRequestError(c, ServerError(fmt.Sprintf("Failed to get audit entry: %v", err)))
		return
	}
	if entry == nil {
		HandleRequestError(c, NotFoundError("Audit entry not found"))
		return
	}

	if entry.ThreatModelID != threatModelId.String() {
		HandleRequestError(c, NotFoundError("Audit entry not found in this threat model"))
		return
	}

	// Check if version snapshot is available
	if entry.Version == nil {
		HandleRequestError(c, GoneError("Version snapshot has been pruned; rollback is no longer available"))
		return
	}

	// Get the snapshot to restore
	snapshotData, err := h.auditService.GetSnapshot(c.Request.Context(), entryId.String())
	if err != nil {
		HandleRequestError(c, GoneError(fmt.Sprintf("Cannot retrieve version snapshot: %v", err)))
		return
	}

	// Perform rollback based on entity type
	restoredEntity, err := h.performRollback(c, entry, snapshotData)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Record the rollback as a new audit entry
	actor := ExtractAuditActor(c)
	rollbackSummary := fmt.Sprintf("Rolled back to version %d", *entry.Version)
	currentState, _ := json.Marshal(restoredEntity)

	rollbackErr := h.auditService.RecordMutation(c.Request.Context(), AuditParams{
		ThreatModelID: threatModelId.String(),
		ObjectType:    entry.ObjectType,
		ObjectID:      entry.ObjectID,
		ChangeType:    models.ChangeTypeRolledBack,
		Actor:         actor,
		PreviousState: nil, // the pre-rollback state was captured by performRollback
		CurrentState:  currentState,
		ChangeSummary: &rollbackSummary,
	})
	if rollbackErr != nil {
		slogging.Get().Error("failed to record rollback audit entry: %v", rollbackErr)
	}

	// Build rollback response
	rollbackEntry, _ := h.auditService.GetAuditEntry(c.Request.Context(), entryId.String())
	var rollbackAuditEntry AuditEntry
	if rollbackEntry != nil {
		rollbackAuditEntry = toAPIAuditEntry(*rollbackEntry)
	}

	c.JSON(http.StatusOK, RollbackResponse{
		RestoredEntity: &restoredEntity,
		AuditEntry:     rollbackAuditEntry,
	})
}

// performRollback restores an entity from a snapshot. Returns the restored entity as a generic map.
func (h *AuditHandler) performRollback(c *gin.Context, entry *AuditEntryResponse, snapshotData []byte) (map[string]interface{}, error) {
	ctx := c.Request.Context()

	// Deserialize snapshot as a generic map for the response
	var restoredMap map[string]interface{}
	if err := json.Unmarshal(snapshotData, &restoredMap); err != nil {
		return nil, ServerError(fmt.Sprintf("Failed to deserialize snapshot: %v", err))
	}

	switch entry.ObjectType {
	case models.ObjectTypeThreatModel:
		return restoredMap, h.rollbackThreatModel(ctx, entry, snapshotData)
	case models.ObjectTypeDiagram:
		return restoredMap, h.rollbackDiagram(ctx, entry, snapshotData)
	case models.ObjectTypeThreat:
		return restoredMap, h.rollbackThreat(ctx, entry, snapshotData)
	case models.ObjectTypeAsset:
		return restoredMap, h.rollbackAsset(ctx, entry, snapshotData)
	case models.ObjectTypeDocument:
		return restoredMap, h.rollbackDocument(ctx, entry, snapshotData)
	case models.ObjectTypeNote:
		return restoredMap, h.rollbackNote(ctx, entry, snapshotData)
	case models.ObjectTypeRepository:
		return restoredMap, h.rollbackRepository(ctx, entry, snapshotData)
	default:
		return nil, ServerError(fmt.Sprintf("Unsupported object type for rollback: %s", entry.ObjectType))
	}
}

func (h *AuditHandler) rollbackThreatModel(_ context.Context, entry *AuditEntryResponse, snapshotData []byte) error {
	var tm ThreatModel
	if err := json.Unmarshal(snapshotData, &tm); err != nil {
		return ServerError(fmt.Sprintf("Failed to deserialize threat model snapshot: %v", err))
	}

	if entry.ChangeType == models.ChangeTypeDeleted {
		// Try to restore the tombstone first; fall back to create if already hard-deleted
		if err := ThreatModelStore.Restore(entry.ObjectID); err == nil {
			return ThreatModelStore.Update(entry.ObjectID, tm)
		}
		_, err := ThreatModelStore.Create(tm, func(t ThreatModel, id string) ThreatModel { return t })
		return err
	}
	return ThreatModelStore.Update(entry.ObjectID, tm)
}

func (h *AuditHandler) rollbackDiagram(_ context.Context, entry *AuditEntryResponse, snapshotData []byte) error {
	var diagram DfdDiagram
	if err := json.Unmarshal(snapshotData, &diagram); err != nil {
		return ServerError(fmt.Sprintf("Failed to deserialize diagram snapshot: %v", err))
	}

	if entry.ChangeType == models.ChangeTypeDeleted {
		if err := DiagramStore.Restore(entry.ObjectID); err == nil {
			return DiagramStore.Update(entry.ObjectID, diagram)
		}
		_, err := DiagramStore.CreateWithThreatModel(diagram, entry.ThreatModelID, func(d DfdDiagram, id string) DfdDiagram { return d })
		return err
	}
	return DiagramStore.Update(entry.ObjectID, diagram)
}

func (h *AuditHandler) rollbackThreat(ctx context.Context, entry *AuditEntryResponse, snapshotData []byte) error {
	var threat Threat
	if err := json.Unmarshal(snapshotData, &threat); err != nil {
		return ServerError(fmt.Sprintf("Failed to deserialize threat snapshot: %v", err))
	}

	if entry.ChangeType == models.ChangeTypeDeleted {
		if err := GlobalThreatStore.Restore(ctx, entry.ObjectID); err == nil {
			return GlobalThreatStore.Update(ctx, &threat)
		}
		return GlobalThreatStore.Create(ctx, &threat)
	}
	return GlobalThreatStore.Update(ctx, &threat)
}

func (h *AuditHandler) rollbackAsset(ctx context.Context, entry *AuditEntryResponse, snapshotData []byte) error {
	var asset Asset
	if err := json.Unmarshal(snapshotData, &asset); err != nil {
		return ServerError(fmt.Sprintf("Failed to deserialize asset snapshot: %v", err))
	}

	if entry.ChangeType == models.ChangeTypeDeleted {
		if err := GlobalAssetStore.Restore(ctx, entry.ObjectID); err == nil {
			return GlobalAssetStore.Update(ctx, &asset, entry.ThreatModelID)
		}
		return GlobalAssetStore.Create(ctx, &asset, entry.ThreatModelID)
	}
	return GlobalAssetStore.Update(ctx, &asset, entry.ThreatModelID)
}

func (h *AuditHandler) rollbackDocument(ctx context.Context, entry *AuditEntryResponse, snapshotData []byte) error {
	var doc Document
	if err := json.Unmarshal(snapshotData, &doc); err != nil {
		return ServerError(fmt.Sprintf("Failed to deserialize document snapshot: %v", err))
	}

	if entry.ChangeType == models.ChangeTypeDeleted {
		if err := GlobalDocumentStore.Restore(ctx, entry.ObjectID); err == nil {
			return GlobalDocumentStore.Update(ctx, &doc, entry.ThreatModelID)
		}
		return GlobalDocumentStore.Create(ctx, &doc, entry.ThreatModelID)
	}
	return GlobalDocumentStore.Update(ctx, &doc, entry.ThreatModelID)
}

func (h *AuditHandler) rollbackNote(ctx context.Context, entry *AuditEntryResponse, snapshotData []byte) error {
	var note Note
	if err := json.Unmarshal(snapshotData, &note); err != nil {
		return ServerError(fmt.Sprintf("Failed to deserialize note snapshot: %v", err))
	}

	if entry.ChangeType == models.ChangeTypeDeleted {
		if err := GlobalNoteStore.Restore(ctx, entry.ObjectID); err == nil {
			return GlobalNoteStore.Update(ctx, &note, entry.ThreatModelID)
		}
		return GlobalNoteStore.Create(ctx, &note, entry.ThreatModelID)
	}
	return GlobalNoteStore.Update(ctx, &note, entry.ThreatModelID)
}

func (h *AuditHandler) rollbackRepository(ctx context.Context, entry *AuditEntryResponse, snapshotData []byte) error {
	var repo Repository
	if err := json.Unmarshal(snapshotData, &repo); err != nil {
		return ServerError(fmt.Sprintf("Failed to deserialize repository snapshot: %v", err))
	}

	if entry.ChangeType == models.ChangeTypeDeleted {
		if err := GlobalRepositoryStore.Restore(ctx, entry.ObjectID); err == nil {
			return GlobalRepositoryStore.Update(ctx, &repo, entry.ThreatModelID)
		}
		return GlobalRepositoryStore.Create(ctx, &repo, entry.ThreatModelID)
	}
	return GlobalRepositoryStore.Update(ctx, &repo, entry.ThreatModelID)
}

// Sub-resource audit trail handlers - delegate to TM-level query with object type filter

// GetDiagramAuditTrail lists audit entries for a specific diagram.
func (h *AuditHandler) GetDiagramAuditTrail(c *gin.Context, threatModelId ThreatModelId, diagramId DiagramId, params GetDiagramAuditTrailParams) {
	h.getSubResourceAuditTrail(c, threatModelId, models.ObjectTypeDiagram, diagramId.String(), params.Limit, params.Offset)
}

// GetThreatAuditTrail lists audit entries for a specific threat.
func (h *AuditHandler) GetThreatAuditTrail(c *gin.Context, threatModelId ThreatModelId, threatId ThreatId, params GetThreatAuditTrailParams) {
	h.getSubResourceAuditTrail(c, threatModelId, models.ObjectTypeThreat, threatId.String(), params.Limit, params.Offset)
}

// GetAssetAuditTrail lists audit entries for a specific asset.
func (h *AuditHandler) GetAssetAuditTrail(c *gin.Context, threatModelId ThreatModelId, assetId AssetId, params GetAssetAuditTrailParams) {
	h.getSubResourceAuditTrail(c, threatModelId, models.ObjectTypeAsset, assetId.String(), params.Limit, params.Offset)
}

// GetDocumentAuditTrail lists audit entries for a specific document.
func (h *AuditHandler) GetDocumentAuditTrail(c *gin.Context, threatModelId ThreatModelId, documentId DocumentId, params GetDocumentAuditTrailParams) {
	h.getSubResourceAuditTrail(c, threatModelId, models.ObjectTypeDocument, documentId.String(), params.Limit, params.Offset)
}

// GetNoteAuditTrail lists audit entries for a specific note.
func (h *AuditHandler) GetNoteAuditTrail(c *gin.Context, threatModelId ThreatModelId, noteId NoteId, params GetNoteAuditTrailParams) {
	h.getSubResourceAuditTrail(c, threatModelId, models.ObjectTypeNote, noteId.String(), params.Limit, params.Offset)
}

// GetRepositoryAuditTrail lists audit entries for a specific repository.
func (h *AuditHandler) GetRepositoryAuditTrail(c *gin.Context, threatModelId ThreatModelId, repositoryId RepositoryId, params GetRepositoryAuditTrailParams) {
	h.getSubResourceAuditTrail(c, threatModelId, models.ObjectTypeRepository, repositoryId.String(), params.Limit, params.Offset)
}

// getSubResourceAuditTrail is the shared implementation for sub-resource audit trails.
func (h *AuditHandler) getSubResourceAuditTrail(c *gin.Context, threatModelId ThreatModelId, objectType string, objectID string, limitParam *PaginationLimit, offsetParam *PaginationOffset) {
	logger := slogging.Get().WithContext(c)
	logger.Debug("[HANDLER] getSubResourceAuditTrail called for %s: %s (TM: %s)", objectType, objectID, threatModelId)

	if h.auditService == nil {
		logger.Error("[HANDLER] auditService is nil in getSubResourceAuditTrail")
		HandleRequestError(c, ServerError("Audit service is not available"))
		return
	}

	_, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	_, err = h.validateThreatModelAccess(c, threatModelId.String())
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	limit, offset := parsePaginationParams(limitParam, offsetParam)

	entries, total, err := h.auditService.GetObjectAuditTrail(c.Request.Context(), objectType, objectID, offset, limit)
	if err != nil {
		HandleRequestError(c, ServerError(fmt.Sprintf("Failed to get audit trail: %v", err)))
		return
	}

	apiEntries := toAPIAuditEntries(entries)
	if apiEntries == nil {
		apiEntries = []AuditEntry{}
	}

	c.JSON(http.StatusOK, ListAuditTrailResponse{
		AuditEntries: apiEntries,
		Total:        total,
		Limit:        limit,
		Offset:       offset,
	})
}

// validateThreatModelAccess verifies the user has reader access to the threat model.
func (h *AuditHandler) validateThreatModelAccess(c *gin.Context, threatModelID string) (*ThreatModel, error) {
	tm, err := ThreatModelStore.Get(threatModelID)
	if err != nil {
		return nil, NotFoundError("Threat model not found")
	}

	userEmail, _, _, _ := ValidateAuthenticatedUser(c)
	hasAccess, err := CheckResourceAccessFromContext(c, userEmail, tm, RoleReader)
	if err != nil {
		return nil, ServerError(fmt.Sprintf("Failed to check access: %v", err))
	}
	if !hasAccess {
		return nil, ForbiddenError("Insufficient permissions to access this threat model")
	}

	return &tm, nil
}

// validateThreatModelWriteAccess verifies the user has writer access to the threat model.
func (h *AuditHandler) validateThreatModelWriteAccess(c *gin.Context, threatModelID string) (*ThreatModel, error) {
	tm, err := ThreatModelStore.Get(threatModelID)
	if err != nil {
		return nil, NotFoundError("Threat model not found")
	}

	userEmail, _, _, _ := ValidateAuthenticatedUser(c)
	hasAccess, err := CheckResourceAccessFromContext(c, userEmail, tm, RoleWriter)
	if err != nil {
		return nil, ServerError(fmt.Sprintf("Failed to check access: %v", err))
	}
	if !hasAccess {
		return nil, ForbiddenError("Insufficient permissions to modify this threat model")
	}

	return &tm, nil
}

// parsePaginationParams extracts limit and offset with defaults.
func parsePaginationParams(limitParam *PaginationLimit, offsetParam *PaginationOffset) (int, int) {
	limit := 20
	offset := 0
	if limitParam != nil {
		limit = *limitParam
	}
	if offsetParam != nil {
		offset = *offsetParam
	}
	if limit <= 0 || limit > 1000 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// buildAuditFilters creates AuditFilters from query parameters.
// Always returns a non-nil AuditFilters struct. When no query parameters are provided,
// returns an empty filter struct (all fields nil), which means "no filtering" — the
// audit trail query returns all entries for the threat model. Empty string values from
// query parameters are treated as absent (no filter for that field).
func buildAuditFilters(objectType *GetThreatModelAuditTrailParamsObjectType, changeType *GetThreatModelAuditTrailParamsChangeType, actorEmail *AuditActorEmail, after *AuditAfter, before *AuditBefore) *AuditFilters {
	filters := &AuditFilters{}

	if objectType != nil {
		s := string(*objectType)
		if s != "" {
			filters.ObjectType = &s
		}
	}
	if changeType != nil {
		s := string(*changeType)
		if s != "" {
			filters.ChangeType = &s
		}
	}
	if actorEmail != nil {
		s := string(*actorEmail)
		if s != "" {
			filters.ActorEmail = &s
		}
	}
	if after != nil {
		t := *after
		filters.After = &t
	}
	if before != nil {
		t := *before
		filters.Before = &t
	}

	return filters
}

// toAPIAuditEntry converts an AuditEntryResponse to the generated API type.
func toAPIAuditEntry(entry AuditEntryResponse) AuditEntry {
	return AuditEntry{
		Id:            parseUUID(entry.ID),
		ThreatModelId: parseUUID(entry.ThreatModelID),
		ObjectType:    AuditEntryObjectType(entry.ObjectType),
		ObjectId:      parseUUID(entry.ObjectID),
		Version:       entry.Version,
		ChangeType:    AuditEntryChangeType(entry.ChangeType),
		Actor: AuditActor{
			Email:       openapi_types.Email(entry.Actor.Email),
			Provider:    entry.Actor.Provider,
			ProviderId:  entry.Actor.ProviderID,
			DisplayName: entry.Actor.DisplayName,
		},
		ChangeSummary: entry.ChangeSummary,
		CreatedAt:     entry.CreatedAt,
	}
}

// toAPIAuditEntries converts a slice of AuditEntryResponse to generated API types.
func toAPIAuditEntries(entries []AuditEntryResponse) []AuditEntry {
	result := make([]AuditEntry, len(entries))
	for i, e := range entries {
		result[i] = toAPIAuditEntry(e)
	}
	return result
}

// parseUUID parses a UUID string, returning a zero UUID on error.
func parseUUID(s string) openapi_types.UUID {
	return ParseUUIDOrNil(s)
}
