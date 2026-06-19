package api

import (
	"github.com/gin-gonic/gin"
)

// StartAuditPruner starts the background audit pruning goroutine.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: start the background audit trail pruning goroutine (mutates shared state)
func (s *Server) StartAuditPruner() {
	if s.auditPruner != nil {
		s.auditPruner.Start()
	}
}

// StopAuditPruner stops the background audit pruning goroutine.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: stop the background audit trail pruning goroutine (mutates shared state)
func (s *Server) StopAuditPruner() {
	if s.auditPruner != nil {
		s.auditPruner.Stop()
	}
}

// Audit trail handler delegation methods

// GetThreatModelAuditTrail lists audit entries for a threat model and all sub-objects.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delegate threat model audit trail listing to the audit handler (reads DB)
func (s *Server) GetThreatModelAuditTrail(c *gin.Context, threatModelId ThreatModelId, params GetThreatModelAuditTrailParams) {
	s.auditHandler.GetThreatModelAuditTrail(c, threatModelId, params)
}

// GetAuditEntry returns a single audit entry.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delegate single audit entry fetch to the audit handler (reads DB)
func (s *Server) GetAuditEntry(c *gin.Context, threatModelId ThreatModelId, entryId AuditEntryId) {
	s.auditHandler.GetAuditEntry(c, threatModelId, entryId)
}

// RollbackToVersion restores an entity to a previous version.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delegate entity rollback to a prior audit version to the audit handler (mutates shared state)
func (s *Server) RollbackToVersion(c *gin.Context, threatModelId ThreatModelId, entryId AuditEntryId) {
	s.auditHandler.RollbackToVersion(c, threatModelId, entryId)
}

// GetDiagramAuditTrail lists audit entries for a specific diagram.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delegate diagram audit trail listing to the audit handler (reads DB)
func (s *Server) GetDiagramAuditTrail(c *gin.Context, threatModelId ThreatModelId, diagramId DiagramId, params GetDiagramAuditTrailParams) {
	s.auditHandler.GetDiagramAuditTrail(c, threatModelId, diagramId, params)
}

// GetThreatAuditTrail lists audit entries for a specific threat.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delegate threat audit trail listing to the audit handler (reads DB)
func (s *Server) GetThreatAuditTrail(c *gin.Context, threatModelId ThreatModelId, threatId ThreatId, params GetThreatAuditTrailParams) {
	s.auditHandler.GetThreatAuditTrail(c, threatModelId, threatId, params)
}

// GetAssetAuditTrail lists audit entries for a specific asset.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delegate asset audit trail listing to the audit handler (reads DB)
func (s *Server) GetAssetAuditTrail(c *gin.Context, threatModelId ThreatModelId, assetId AssetId, params GetAssetAuditTrailParams) {
	s.auditHandler.GetAssetAuditTrail(c, threatModelId, assetId, params)
}

// GetDocumentAuditTrail lists audit entries for a specific document.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: fetch paginated audit entries for a specific document (reads DB)
func (s *Server) GetDocumentAuditTrail(c *gin.Context, threatModelId ThreatModelId, documentId DocumentId, params GetDocumentAuditTrailParams) {
	s.auditHandler.GetDocumentAuditTrail(c, threatModelId, documentId, params)
}

// GetNoteAuditTrail lists audit entries for a specific note.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: fetch paginated audit entries for a specific note (reads DB)
func (s *Server) GetNoteAuditTrail(c *gin.Context, threatModelId ThreatModelId, noteId NoteId, params GetNoteAuditTrailParams) {
	s.auditHandler.GetNoteAuditTrail(c, threatModelId, noteId, params)
}

// GetRepositoryAuditTrail lists audit entries for a specific repository.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: fetch paginated audit entries for a specific repository (reads DB)
func (s *Server) GetRepositoryAuditTrail(c *gin.Context, threatModelId ThreatModelId, repositoryId RepositoryId, params GetRepositoryAuditTrailParams) {
	s.auditHandler.GetRepositoryAuditTrail(c, threatModelId, repositoryId, params)
}

// Restore endpoints

// RestoreThreatModel restores a soft-deleted threat model and all its children.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: restore a soft-deleted threat model and all its children
func (s *Server) RestoreThreatModel(c *gin.Context, threatModelId ThreatModelId) {
	HandleRestoreThreatModel(c, threatModelId.String())
}

// RestoreDiagram restores a soft-deleted diagram.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: restore a soft-deleted diagram within a threat model
func (s *Server) RestoreDiagram(c *gin.Context, threatModelId ThreatModelId, diagramId DiagramId) {
	HandleRestoreDiagram(c, threatModelId.String(), diagramId.String())
}

// RestoreThreat restores a soft-deleted threat.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: restore a soft-deleted threat within a threat model
func (s *Server) RestoreThreat(c *gin.Context, threatModelId ThreatModelId, threatId ThreatId) {
	HandleRestoreThreat(c, threatModelId.String(), threatId.String())
}

// RestoreAsset restores a soft-deleted asset.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: restore a soft-deleted asset within a threat model
func (s *Server) RestoreAsset(c *gin.Context, threatModelId ThreatModelId, assetId AssetId) {
	HandleRestoreAsset(c, threatModelId.String(), assetId.String())
}

// RestoreDocument restores a soft-deleted document.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: restore a soft-deleted document within a threat model
func (s *Server) RestoreDocument(c *gin.Context, threatModelId ThreatModelId, documentId DocumentId) {
	HandleRestoreDocument(c, threatModelId.String(), documentId.String())
}

// RestoreNote restores a soft-deleted note.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: restore a soft-deleted note within a threat model
func (s *Server) RestoreNote(c *gin.Context, threatModelId ThreatModelId, noteId NoteId) {
	HandleRestoreNote(c, threatModelId.String(), noteId.String())
}

// RestoreRepository restores a soft-deleted repository.
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: restore a soft-deleted repository within a threat model
func (s *Server) RestoreRepository(c *gin.Context, threatModelId ThreatModelId, repositoryId RepositoryId) {
	HandleRestoreRepository(c, threatModelId.String(), repositoryId.String())
}
