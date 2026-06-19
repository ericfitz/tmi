package models

import (
	"time"

	"gorm.io/gorm"
)

// Extraction job status values. The monolith actively writes only
// StatusQueued (at publish time) and the terminal StatusCompleted /
// StatusFailed (when a result lands). The intermediate values exist for
// forward-compatibility and are not written in Plan 3.
const (
	ExtractionStatusQueued         = "queued"
	ExtractionStatusExtracting     = "extracting"
	ExtractionStatusChunkEmbedding = "chunk_embedding"
	ExtractionStatusCompleted      = "completed"
	ExtractionStatusFailed         = "failed"
)

// ExtractionJob is the monolith's internal record of one async extraction
// job. The result-consumer is the sole writer of terminal states; the
// publish-side callers only insert the initial queued row (idempotently).
// Components (workers) never touch this table. document_ref is indexed but
// has no database-level foreign key, so a document deleted mid-job does not
// cause a constraint violation; the result-consumer tolerates the missing row.
// SEM@d8b4a7f6b4c480a8020df9e796e1deabb7f0fdb7: GORM model tracking the status lifecycle of one async extraction job (pure)
type ExtractionJob struct {
	JobID DBVarchar `gorm:"column:job_id;primaryKey;not null;size:36"`
	// DocumentRef is the document being extracted. NOT NULL with no DB-level FK.
	// On the bare-upsert-insert path (a terminal result arriving with no prior
	// queued row) the real ref is unknown and the non-empty sentinel
	// "__unknown__" (unknownDocumentRef in api/extraction_job_store.go) is
	// written instead — an empty string is indistinguishable from NULL on
	// Oracle and would violate NOT NULL (ORA-01400). Any query that filters on
	// document_ref must exclude the sentinel.
	DocumentRef DBVarchar         `gorm:"column:document_ref;size:36;not null;index:idx_extraction_jobs_doc"`
	Status      DBVarchar         `gorm:"column:status;size:32;not null;default:queued"`
	ReasonCode  NullableDBVarchar `gorm:"column:reason_code;size:64"`
	Stage       NullableDBVarchar `gorm:"column:stage;size:32"`
	Attempts    int32             `gorm:"column:attempts;not null;default:0"`
	CreatedAt   time.Time         `gorm:"column:created_at;not null;autoCreateTime"`
	UpdatedAt   time.Time         `gorm:"column:updated_at;not null;autoUpdateTime"`
	CompletedAt *time.Time        `gorm:"column:completed_at"`
}

// TableName returns the prefixed table name.
// SEM@d8b4a7f6b4c480a8020df9e796e1deabb7f0fdb7: return the prefixed database table name for extraction jobs (pure)
func (ExtractionJob) TableName() string {
	return tableName("extraction_jobs")
}

// BeforeCreate is a gorm hook required to satisfy the gorm.DB interface.
// JobID is always supplied by the caller (it is the envelope job_id) so it
// is never generated here.
// SEM@d8b4a7f6b4c480a8020df9e796e1deabb7f0fdb7: GORM hook that no-ops before insert since job ID is always caller-supplied (pure)
func (j *ExtractionJob) BeforeCreate(_ *gorm.DB) error {
	return nil
}
