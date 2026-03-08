package api

import (
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// HandleRestoreThreatModel restores a soft-deleted threat model and all its children.
func HandleRestoreThreatModel(c *gin.Context, threatModelId string) {
	logger := slogging.Get().WithContext(c)

	if _, err := ParseUUID(threatModelId); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Verify entity is actually deleted
	tm, err := ThreatModelStore.GetIncludingDeleted(threatModelId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}
	if tm.DeletedAt == nil {
		HandleRequestError(c, ConflictError("Threat model is not deleted"))
		return
	}

	// Capture pre-restore state for audit
	preState, _ := SerializeForAudit(tm)

	// Restore (cascades to all children)
	if err := ThreatModelStore.Restore(threatModelId); err != nil {
		logger.Error("Failed to restore threat model %s: %v", threatModelId, err)
		HandleRequestError(c, ServerError("Failed to restore threat model"))
		return
	}

	// Fetch the restored entity
	restored, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		logger.Error("Failed to get restored threat model %s: %v", threatModelId, err)
		HandleRequestError(c, ServerError("Failed to retrieve restored threat model"))
		return
	}

	// Record audit entry
	RecordAuditUpdate(c, models.ChangeTypeRestored, threatModelId, "threat_model", threatModelId, preState, restored)

	logger.Info("Restored threat model %s and all children", threatModelId)
	c.JSON(http.StatusOK, restored)
}

// restoreSubEntity is a helper for restoring sub-entities within a threat model.
// It checks that the parent threat model is not deleted before allowing the restore.
func restoreSubEntity(c *gin.Context, threatModelId, entityId, entityType string, restoreFn func() error, getFn func() (any, error)) {
	logger := slogging.Get().WithContext(c)

	if _, err := ParseUUID(threatModelId); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}
	if _, err := ParseUUID(entityId); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid "+entityType+" ID format, must be a valid UUID"))
		return
	}

	// Check that the parent threat model is not deleted
	tm, err := ThreatModelStore.GetIncludingDeleted(threatModelId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}
	if tm.DeletedAt != nil {
		HandleRequestError(c, ConflictError("Cannot restore "+entityType+": parent threat model is deleted. Restore the threat model first."))
		return
	}

	// Restore the entity
	if err := restoreFn(); err != nil {
		// Distinguish "not found or not deleted" from other errors
		if isNotFoundOrNotDeleted(err) {
			HandleRequestError(c, ConflictError("Entity is not in a deleted state or does not exist"))
			return
		}
		logger.Error("Failed to restore %s %s: %v", entityType, entityId, err)
		HandleRequestError(c, ServerError("Failed to restore "+entityType))
		return
	}

	// Fetch the restored entity
	restored, err := getFn()
	if err != nil {
		logger.Error("Failed to get restored %s %s: %v", entityType, entityId, err)
		HandleRequestError(c, ServerError("Failed to retrieve restored "+entityType))
		return
	}

	// Record audit entry
	RecordAuditUpdate(c, models.ChangeTypeRestored, threatModelId, entityType, entityId, nil, restored)

	logger.Info("Restored %s %s in threat model %s", entityType, entityId, threatModelId)
	c.JSON(http.StatusOK, restored)
}

// isNotFoundOrNotDeleted checks if an error indicates the entity was not found or not deleted.
func isNotFoundOrNotDeleted(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "not found") || strings.Contains(msg, "not deleted")
}

// RestoreDiagram restores a soft-deleted diagram.
func HandleRestoreDiagram(c *gin.Context, threatModelId, diagramId string) {
	ctx := c.Request.Context()
	restoreSubEntity(c, threatModelId, diagramId, "diagram",
		func() error { return DiagramStore.Restore(diagramId) },
		func() (any, error) {
			d, err := DiagramStore.Get(diagramId)
			if err != nil {
				return nil, err
			}
			_ = ctx // diagram store Get doesn't take context
			return d, nil
		},
	)
}

// RestoreThreat restores a soft-deleted threat.
func HandleRestoreThreat(c *gin.Context, threatModelId, threatId string) {
	ctx := c.Request.Context()
	restoreSubEntity(c, threatModelId, threatId, "threat",
		func() error { return GlobalThreatStore.Restore(ctx, threatId) },
		func() (any, error) { return GlobalThreatStore.Get(ctx, threatId) },
	)
}

// RestoreAsset restores a soft-deleted asset.
func HandleRestoreAsset(c *gin.Context, threatModelId, assetId string) {
	ctx := c.Request.Context()
	restoreSubEntity(c, threatModelId, assetId, "asset",
		func() error { return GlobalAssetStore.Restore(ctx, assetId) },
		func() (any, error) { return GlobalAssetStore.Get(ctx, assetId) },
	)
}

// RestoreDocument restores a soft-deleted document.
func HandleRestoreDocument(c *gin.Context, threatModelId, documentId string) {
	ctx := c.Request.Context()
	restoreSubEntity(c, threatModelId, documentId, "document",
		func() error { return GlobalDocumentStore.Restore(ctx, documentId) },
		func() (any, error) { return GlobalDocumentStore.Get(ctx, documentId) },
	)
}

// RestoreNote restores a soft-deleted note.
func HandleRestoreNote(c *gin.Context, threatModelId, noteId string) {
	ctx := c.Request.Context()
	restoreSubEntity(c, threatModelId, noteId, "note",
		func() error { return GlobalNoteStore.Restore(ctx, noteId) },
		func() (any, error) { return GlobalNoteStore.Get(ctx, noteId) },
	)
}

// RestoreRepository restores a soft-deleted repository.
func HandleRestoreRepository(c *gin.Context, threatModelId, repositoryId string) {
	ctx := c.Request.Context()
	restoreSubEntity(c, threatModelId, repositoryId, "repository",
		func() error { return GlobalRepositoryStore.Restore(ctx, repositoryId) },
		func() (any, error) { return GlobalRepositoryStore.Get(ctx, repositoryId) },
	)
}
