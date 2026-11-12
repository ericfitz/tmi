package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/api/validation"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/errcode"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormDocumentRepository implements DocumentStore using GORM
type GormDocumentRepository struct {
	db               *gorm.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
	mutex            sync.RWMutex
}

// NewGormDocumentRepository creates a new GORM-backed document store with optional caching
func NewGormDocumentRepository(db *gorm.DB, cache *CacheService, invalidator *CacheInvalidator) *GormDocumentRepository {
	return &GormDocumentRepository{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// Create creates a new document
func (s *GormDocumentRepository) Create(ctx context.Context, document *Document, threatModelID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Creating document: %s in threat model: %s", document.Name, threatModelID)

	// Generate ID if not provided
	if document.Id == nil {
		id := uuid.New()
		document.Id = &id
	}

	// Microseconds: the resolution TIMESTAMP(6) and timestamptz store, so the
	// value mirrored onto the response matches a later GET (#1150).
	now := time.Now().UTC().Truncate(time.Microsecond)

	model := models.Document{
		ID:            models.DBVarchar(document.Id.String()),
		ThreatModelID: models.DBVarchar(threatModelID),
		Name:          models.DBVarchar(document.Name),
		URI:           models.DBText(document.Uri),
		Description:   models.NewNullableDBText(document.Description),
		CreatedAt:     now,
		ModifiedAt:    now,
	}
	if document.IncludeInReport != nil {
		model.IncludeInReport = models.DBBool(*document.IncludeInReport)
	}
	if document.TimmyEnabled != nil {
		model.TimmyEnabled = models.DBBool(*document.TimmyEnabled)
	}

	// Map access tracking fields from API type to GORM model
	if document.AccessStatus != nil {
		status := string(*document.AccessStatus)
		model.AccessStatus = models.NewNullableDBVarchar(&status)
	}
	if document.ContentSource != nil {
		model.ContentSource = models.NewNullableDBVarchar(document.ContentSource)
	}

	// READ COMMITTED: insert-only on freshly keyed rows, so SERIALIZABLE only adds false ORA-08177 (#906, ADR 2026-09-17).
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		alias, err := AllocateNextAlias(ctx, tx, threatModelID, "document")
		if err != nil {
			return fmt.Errorf("allocate document alias: %w", err)
		}
		model.Alias = alias
		// Mirror the allocated alias back into the API-level argument so
		// callers (handlers serializing the response) see the assigned value.
		document.Alias = &alias
		if err := tx.Create(&model).Error; err != nil {
			return dberrors.Classify(err)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		logger.Error("Failed to create document in database: %v", err)
		return err
	}

	// Update API object with timestamps from database
	document.CreatedAt = &model.CreatedAt
	document.ModifiedAt = &model.ModifiedAt

	// Save metadata if present
	if document.Metadata != nil && len(*document.Metadata) > 0 {
		if err := s.saveMetadata(ctx, document.Id.String(), *document.Metadata); err != nil {
			logger.Error("Failed to save document metadata: %v", err)
			// Don't fail the request if metadata saving fails
		}
	}

	// Cache the new document
	if s.cache != nil {
		if cacheErr := s.cache.CacheDocument(ctx, document); cacheErr != nil {
			logger.Error("Failed to cache new document: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "document",
			EntityID:      document.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threatModelID,
			OperationType: "create",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after document creation: %v", invErr)
		}
	}

	logger.Debug("Successfully created document: %s", document.Id)
	return nil
}

// Get retrieves a document by ID
func (s *GormDocumentRepository) Get(ctx context.Context, id string) (*Document, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Getting document: %s", id)

	// Try cache first
	if s.cache != nil {
		document, err := s.cache.GetCachedDocument(ctx, id)
		if err != nil {
			logger.Error("Cache error when getting document %s: %v", id, err)
		} else if document != nil {
			logger.Debug("Cache hit for document: %s", id)
			return document, nil
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for document %s, querying database", id)

	var model models.Document
	result := s.db.WithContext(ctx).First(&model, "id = ? AND deleted_at IS NULL", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrDocumentNotFound
		}
		logger.Error("Failed to get document from database: %v", result.Error)
		return nil, dberrors.Classify(result.Error)
	}

	document := s.modelToAPI(&model)

	// Load metadata
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		logger.Error("Failed to load metadata for document %s: %v", id, err)
		metadata = []Metadata{}
	}
	document.Metadata = &metadata

	// Cache the result
	if s.cache != nil {
		if cacheErr := s.cache.CacheDocument(ctx, document); cacheErr != nil {
			logger.Error("Failed to cache document after database fetch: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved document: %s", id)
	return document, nil
}

// update runs the document content write inside one retryable transaction,
// CAS-guarded first when expectedVersion is non-nil (#594).
func (s *GormDocumentRepository) update(ctx context.Context, document *Document, threatModelID string, expectedVersion *int) (int, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Updating document: %s", document.Id)

	// DOCUMENTS.NAME and DOCUMENTS.URI are NOT NULL, and the SkipHooks session
	// below disables any hook fallback. Oracle binds '' as NULL, so an empty
	// value that PostgreSQL would store raises ORA-01407 — an unclassified 500
	// (#714). Reject as client input so both engines answer 400. Backstop for
	// callers that bypass the handler/schema checks (validateURI skips "").
	if err := validation.ValidateNonEmpty("name", document.Name); err != nil {
		return 0, &RequestError{Status: http.StatusBadRequest, Code: errcode.InvalidInput, Message: err.Error()}
	}
	if err := validation.ValidateURI("uri", document.Uri); err != nil {
		return 0, &RequestError{Status: http.StatusBadRequest, Code: errcode.InvalidInput, Message: err.Error()}
	}

	// modified_at explicitly: the SkipHooks session below suppresses GORM's
	// autoUpdateTime tag, so without this the column stays at created_at for
	// the life of the row. Pre-dates #610 — the same measurement that found it
	// on repositories found it here.
	//
	// The lowercase key itself is fine: GORM's LookUpField falls back to running
	// it through the naming strategy, which yields MODIFIED_AT on Oracle, and
	// the emitted SQL uses field.DBName. So this does NOT risk ORA-00904, and
	// rewriting it via AssignmentMap "to fix the casing" would be treating a
	// non-problem. (That fallback is a GORM-version dependency — on a release
	// without it the raw key is emitted verbatim and ORA-00904 is real.)
	//
	// INVARIANT: this key is safe ONLY while that session sets SkipHooks. The
	// autoUpdateTime injector is guarded by `!stmt.SkipHooks`, and its dedup
	// check tests value[field.Name] and value[field.DBName] — "ModifiedAt" and
	// "MODIFIED_AT" on Oracle. A lowercase key matches neither, so re-enabling
	// hooks would append a SECOND assignment to the same column: ORA-00957
	// (duplicate column name), which is what the comment previously here warned
	// about. PostgreSQL's DBName is literally "modified_at", so its guard fires
	// and PG stays green — the breakage would be Oracle-only. If you need the
	// hooks back, drop this key or route it through AssignmentMap first.
	// Truncated to microseconds — see the note in repository_store_gorm.go.
	now := time.Now().UTC().Truncate(time.Microsecond)
	// description is a NullableDBText column; route through the typed
	// constructor so Value() normalizes an empty string to NULL (#700).
	updates := map[string]any{
		"modified_at": now,
		"name":        document.Name,
		"uri":         document.Uri,
		"description": models.NewNullableDBText(document.Description),
	}
	if document.IncludeInReport != nil {
		updates["include_in_report"] = models.DBBool(*document.IncludeInReport)
	} else {
		updates["include_in_report"] = models.DBBool(false)
	}
	if document.TimmyEnabled != nil {
		updates["timmy_enabled"] = models.DBBool(*document.TimmyEnabled)
	} else {
		updates["timmy_enabled"] = models.DBBool(false)
	}

	// Skip hooks to avoid validation errors on empty model struct.
	// Document fields are already validated via OpenAPI middleware before reaching here.
	var newVersion int
	var stored models.Document
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if expectedVersion != nil {
			v, casErr := CheckAndBumpVersion(ctx, tx, models.Document{}.TableName(), document.Id.String(), *expectedVersion)
			if casErr != nil {
				return casErr
			}
			newVersion = v
		}

		result := tx.Session(&gorm.Session{SkipHooks: true}).Model(&models.Document{}).
			Where("id = ? AND threat_model_id = ?", document.Id.String(), threatModelID).
			// Map condition (clause.Eq) rather than extending the string Expr:
			// gorm-oracle's #392 UPDATE heuristic would classify a combined
			// "deleted_at ... null" Expr as soft-delete-only and fail with
			// gorm.ErrMissingWhereClause. DeletedAt is *time.Time, so nothing
			// scopes tombstones out implicitly (#669).
			Where(map[string]any{ColumnName(tx.Name(), "deleted_at"): nil}).
			Updates(AssignmentMap(tx.Name(), updates))
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrDocumentNotFound
		}
		// Read back the timestamps the row now holds: the caller's struct has
		// no created_at (nil on PUT), and it is both the response body and the
		// cached copy (#1150). A plain tx (no SkipHooks session) with a
		// two-column Select, as in the note and asset stores.
		if err := tx.Select("created_at", "modified_at").
			First(&stored, "id = ?", document.Id.String()).Error; err != nil {
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, ErrDocumentNotFound) {
			logger.Error("Failed to update document in database: %v", err)
		}
		return 0, err
	}

	// Mirror the stored timestamps onto the struct before it is cached — see
	// the matching note in repository_store_gorm.go.
	document.CreatedAt = &stored.CreatedAt
	document.ModifiedAt = &stored.ModifiedAt

	// Update metadata if present
	if document.Metadata != nil {
		if err := s.updateMetadata(ctx, document.Id.String(), *document.Metadata); err != nil {
			logger.Error("Failed to update document metadata: %v", err)
		}
	}

	// Update cache
	if s.cache != nil {
		if cacheErr := s.cache.CacheDocument(ctx, document); cacheErr != nil {
			logger.Error("Failed to update document cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "document",
			EntityID:      document.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threatModelID,
			OperationType: "update",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after document update: %v", invErr)
		}
	}

	logger.Debug("Successfully updated document: %s", document.Id)
	return newVersion, nil
}

// Update updates an existing document
func (s *GormDocumentRepository) Update(ctx context.Context, document *Document, threatModelID string) error {
	_, err := s.update(ctx, document, threatModelID, nil)
	return err
}

// UpdateWithVersion updates a document guarded by a same-transaction
// optimistic-lock CAS (#594).
func (s *GormDocumentRepository) UpdateWithVersion(ctx context.Context, document *Document, threatModelID string, expectedVersion int) (int, error) {
	return s.update(ctx, document, threatModelID, &expectedVersion)
}

// Delete soft-deletes a document by setting deleted_at
func (s *GormDocumentRepository) Delete(ctx context.Context, id string) error {
	return s.SoftDelete(ctx, id)
}

// hardDeleteDocument permanently removes a document and its metadata from the database
func (s *GormDocumentRepository) hardDeleteDocument(ctx context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting document: %s", id)

	// Get threat model ID for cache invalidation
	var model models.Document
	if err := s.db.WithContext(ctx).Select("threat_model_id").First(&model, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrDocumentNotFound
		}
		logger.Error("Failed to get threat model ID for document %s: %v", id, err)
		return dberrors.Classify(err)
	}

	// Delete from database (with retry)
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Metadata has no FK to its entity: delete it in the same transaction
		// (before the row, matching the tombstone purge's lock order) so a
		// reused ID cannot inherit it (#947). Not-found rolls this back.
		if err := tx.Where("entity_type = ? AND entity_id = ?", "document", id).
			Delete(&models.Metadata{}).Error; err != nil {
			return dberrors.Classify(err)
		}
		result := tx.Delete(&models.Document{}, "id = ?", id)
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrDocumentNotFound
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, ErrDocumentNotFound) {
			logger.Error("Failed to delete document from database: %v", err)
		}
		return err
	}

	// Remove from cache
	if s.cache != nil {
		if cacheErr := s.cache.InvalidateEntity(ctx, "document", id); cacheErr != nil {
			logger.Error("Failed to remove document from cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "document",
			EntityID:      id,
			ParentType:    "threat_model",
			ParentID:      string(model.ThreatModelID),
			OperationType: "delete",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after document deletion: %v", invErr)
		}
	}

	logger.Debug("Successfully deleted document: %s", id)
	return nil
}

// List retrieves documents for a threat model with pagination
func (s *GormDocumentRepository) List(ctx context.Context, threatModelID string, offset, limit int) ([]Document, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Listing documents for threat model %s (offset: %d, limit: %d)", threatModelID, offset, limit)

	// Try cache first
	var documents []Document
	if s.cache != nil {
		err := s.cache.GetCachedList(ctx, "documents", threatModelID, offset, limit, &documents)
		if err == nil && documents != nil {
			logger.Debug("Cache hit for document list %s [%d:%d]", threatModelID, offset, limit)
			return documents, nil
		}
		if err != nil {
			logger.Error("Cache error when getting document list: %v", err)
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for document list, querying database")

	var modelList []models.Document
	query := s.db.WithContext(ctx)
	if includeDeletedFromContext(ctx) {
		query = query.Where("threat_model_id = ?", threatModelID)
	} else {
		query = query.Where("threat_model_id = ? AND deleted_at IS NULL", threatModelID)
	}
	result := query.
		Order("created_at DESC, id ASC").
		Limit(limit).
		Offset(offset).
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to query documents from database: %v", result.Error)
		return nil, dberrors.Classify(result.Error)
	}

	documents = make([]Document, 0, len(modelList))
	for _, model := range modelList {
		doc := s.modelToAPI(&model)

		// Load metadata for this document
		metadata, metaErr := s.loadMetadata(ctx, string(model.ID))
		if metaErr != nil {
			logger.Error("Failed to load metadata for document %s: %v", string(model.ID), metaErr)
			metadata = []Metadata{}
		}
		doc.Metadata = &metadata

		documents = append(documents, *doc)
	}

	// Cache the result
	if s.cache != nil {
		if cacheErr := s.cache.CacheList(ctx, "documents", threatModelID, offset, limit, documents); cacheErr != nil {
			logger.Error("Failed to cache document list: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved %d documents", len(documents))
	return documents, nil
}

// ListByAccessStatus returns documents matching the given access status across all threat models.
func (s *GormDocumentRepository) ListByAccessStatus(ctx context.Context, status string, limit int) ([]Document, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var dbDocs []models.Document
	result := s.db.WithContext(ctx).
		Where("access_status = ? AND deleted_at IS NULL", status).
		Limit(limit).
		Find(&dbDocs)
	if result.Error != nil {
		return nil, result.Error
	}

	docs := make([]Document, 0, len(dbDocs))
	for _, d := range dbDocs {
		doc := s.modelToAPI(&d)
		docs = append(docs, *doc)
	}
	return docs, nil
}

// BulkCreate creates multiple documents in a single transaction
func (s *GormDocumentRepository) BulkCreate(ctx context.Context, documents []Document, threatModelID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Bulk creating %d documents", len(documents))

	if len(documents) == 0 {
		return nil
	}

	// Microseconds: the resolution TIMESTAMP(6) and timestamptz store, so the
	// value mirrored onto the response matches a later GET (#1150).
	now := time.Now().UTC().Truncate(time.Microsecond)

	// READ COMMITTED: insert-only on freshly keyed rows, so SERIALIZABLE only adds false ORA-08177 (#906, ADR 2026-09-17).
	return authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		for i := range documents {
			document := &documents[i]

			// Generate ID if not provided
			if document.Id == nil {
				id := uuid.New()
				document.Id = &id
			}

			alias, err := AllocateNextAlias(ctx, tx, threatModelID, "document")
			if err != nil {
				return fmt.Errorf("allocate document alias: %w", err)
			}

			model := models.Document{
				ID:            models.DBVarchar(document.Id.String()),
				ThreatModelID: models.DBVarchar(threatModelID),
				Name:          models.DBVarchar(document.Name),
				URI:           models.DBText(document.Uri),
				Description:   models.NewNullableDBText(document.Description),
				CreatedAt:     now,
				ModifiedAt:    now,
				Alias:         alias,
			}
			if document.IncludeInReport != nil {
				model.IncludeInReport = models.DBBool(*document.IncludeInReport)
			}
			if document.TimmyEnabled != nil {
				model.TimmyEnabled = models.DBBool(*document.TimmyEnabled)
			}

			if err := tx.Create(&model).Error; err != nil {
				logger.Error("Failed to bulk create document %d: %v", i, err)
				return dberrors.Classify(err)
			}
			// Mirror the allocated alias and stored timestamps back into the
			// API-level slice (#1150).
			document.Alias = &alias
			document.CreatedAt = &model.CreatedAt
			document.ModifiedAt = &model.ModifiedAt
		}

		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

// Patch applies JSON patch operations to a document
func (s *GormDocumentRepository) Patch(ctx context.Context, id string, operations []PatchOperation) (*Document, error) {
	document, _, err := s.patch(ctx, id, operations, nil)
	return document, err
}

// PatchWithVersion applies JSON patch operations to a document guarded by a
// same-transaction optimistic-lock CAS (#594).
func (s *GormDocumentRepository) PatchWithVersion(ctx context.Context, id string, operations []PatchOperation, expectedVersion int) (*Document, int, error) {
	return s.patch(ctx, id, operations, &expectedVersion)
}

// patch runs patch-operation application then the content write, CAS-guarded
// first when expectedVersion is non-nil (#594).
func (s *GormDocumentRepository) patch(ctx context.Context, id string, operations []PatchOperation, expectedVersion *int) (*Document, int, error) {
	logger := slogging.Get()
	logger.Debug("Patching document %s with %d operations", id, len(operations))

	// Get current document
	document, err := s.Get(ctx, id)
	if err != nil {
		return nil, 0, err
	}

	// Apply the operations with the generic RFC 6902 applier (#1143). A
	// malformed or inapplicable patch is client input and surfaces as a 400
	// RequestError (#611).
	patched, err := applyDocumentPatch(*document, operations)
	if err != nil {
		logger.Error("Failed to apply patch to document %s: %v", id, err)
		return nil, 0, err
	}
	*document = patched

	// Get threat model ID for update
	threatModelID, err := s.getDocumentThreatModelID(ctx, id)
	if err != nil {
		return nil, 0, err
	}

	// Update the document
	newVersion, err := s.update(ctx, document, threatModelID, expectedVersion)
	if err != nil {
		return nil, 0, err
	}

	return document, newVersion, nil
}

// Count returns the total number of documents for a threat model
func (s *GormDocumentRepository) Count(ctx context.Context, threatModelID string) (int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Counting documents for threat model %s", threatModelID)

	var count int64
	query := s.db.WithContext(ctx).Model(&models.Document{})
	if includeDeletedFromContext(ctx) {
		query = query.Where("threat_model_id = ?", threatModelID)
	} else {
		query = query.Where("threat_model_id = ? AND deleted_at IS NULL", threatModelID)
	}
	result := query.Count(&count)

	if result.Error != nil {
		logger.Error("Failed to count documents: %v", result.Error)
		return 0, dberrors.Classify(result.Error)
	}

	return int(count), nil
}

// InvalidateCache removes document-related cache entries
func (s *GormDocumentRepository) InvalidateCache(ctx context.Context, id string) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.InvalidateEntity(ctx, "document", id)
}

// WarmCache preloads documents for a threat model into cache
func (s *GormDocumentRepository) WarmCache(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Warming cache for threat model documents: %s", threatModelID)

	if s.cache == nil {
		return nil
	}

	// Load first page of documents
	documents, err := s.List(ctx, threatModelID, 0, 50)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	logger.Debug("Warmed cache with %d documents for threat model %s", len(documents), threatModelID)
	return nil
}

// UpdateAccessStatus sets the access tracking fields on a document.
func (s *GormDocumentRepository) UpdateAccessStatus(ctx context.Context, id string, accessStatus string, contentSource string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Both columns are NullableDBVarchar; route through the typed constructor
	// so Value() normalizes an empty string to NULL (#700) instead of writing
	// "" verbatim, which the raw strings previously here would do.
	updates := map[string]interface{}{
		"access_status":  models.NewNullableDBVarchar(&accessStatus),
		"content_source": models.NewNullableDBVarchar(&contentSource),
	}
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Table(models.Document{}.TableName()).Where("id = ?", id).
			Updates(AssignmentMap(tx.Name(), updates))
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Invalidate cache so subsequent GETs reflect the updated fields
	if s.cache != nil {
		_ = s.cache.InvalidateEntity(ctx, "document", id)
	}
	return nil
}

// UpdateAccessStatusWithDiagnostics sets access tracking fields on a document.
// See DocumentStore.UpdateAccessStatusWithDiagnostics.
//
// Uses Table("documents") (not Model(&models.Document{})) to avoid triggering
// the Document.BeforeSave hook, which validates Name/URI on the empty struct and
// would produce false "cannot be empty" errors for map-based updates — same
// pattern as UpdateAccessStatus.
func (s *GormDocumentRepository) UpdateAccessStatusWithDiagnostics(
	ctx context.Context,
	id string,
	accessStatus string,
	contentSource string,
	reasonCode string,
	reasonDetail string,
) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// access_status is a NullableDBVarchar column; route it through the typed
	// constructor so Value() normalizes an empty string to NULL (#700) rather
	// than writing "" verbatim, which a raw string in the map would do.
	updates := map[string]interface{}{
		"access_status":            models.NewNullableDBVarchar(&accessStatus),
		"access_status_updated_at": time.Now().UTC().Truncate(time.Microsecond),
	}
	if contentSource != "" {
		updates["content_source"] = contentSource
	}
	if reasonCode == "" {
		updates["access_reason_code"] = nil
		updates["access_reason_detail"] = nil
	} else {
		updates["access_reason_code"] = reasonCode
		if reasonDetail == "" {
			updates["access_reason_detail"] = nil
		} else {
			updates["access_reason_detail"] = reasonDetail
		}
	}
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Table(models.Document{}.TableName()).Where("id = ?", id).
			Updates(AssignmentMap(tx.Name(), updates))
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Invalidate cache so subsequent GETs reflect the updated fields
	if s.cache != nil {
		_ = s.cache.InvalidateEntity(ctx, "document", id)
	}
	return nil
}

// GetAccessReason returns the diagnostic fields for a document.
// See DocumentStore.GetAccessReason.
func (s *GormDocumentRepository) GetAccessReason(
	ctx context.Context, id string,
) (reasonCode string, reasonDetail string, updatedAt *time.Time, err error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var row struct {
		AccessReasonCode      *string
		AccessReasonDetail    *string
		AccessStatusUpdatedAt *time.Time
	}
	result := s.db.WithContext(ctx).
		Table(models.Document{}.TableName()).
		Select("access_reason_code, access_reason_detail, access_status_updated_at").
		Where("id = ? AND deleted_at IS NULL", id).
		Take(&row)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "", "", nil, ErrDocumentNotFound
		}
		return "", "", nil, dberrors.Classify(result.Error)
	}

	if row.AccessReasonCode != nil {
		reasonCode = *row.AccessReasonCode
	}
	if row.AccessReasonDetail != nil {
		reasonDetail = *row.AccessReasonDetail
	}
	return reasonCode, reasonDetail, row.AccessStatusUpdatedAt, nil
}

// GetThreatModelID returns the parent threat model ID for the given document.
// See DocumentRepository.GetThreatModelID.
func (s *GormDocumentRepository) GetThreatModelID(ctx context.Context, id string) (string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var row struct {
		ThreatModelID string
	}
	result := s.db.WithContext(ctx).
		Table(models.Document{}.TableName()).
		Select("threat_model_id").
		Where("id = ? AND deleted_at IS NULL", id).
		Take(&row)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "", ErrDocumentNotFound
		}
		return "", dberrors.Classify(result.Error)
	}
	return row.ThreatModelID, nil
}

// GetPickerDispatch returns picker metadata + owner UUID for poller dispatch.
// See DocumentStore.GetPickerDispatch.
func (s *GormDocumentRepository) GetPickerDispatch(
	ctx context.Context, id string,
) (*PickerMetadata, string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var row struct {
		PickerProviderID  *string
		PickerFileID      *string
		PickerMimeType    *string
		OwnerInternalUUID string
	}
	result := s.db.WithContext(ctx).
		Table(models.Document{}.TableName()).
		Select("documents.picker_provider_id, documents.picker_file_id, documents.picker_mime_type, threat_models.owner_internal_uuid AS owner_internal_uuid").
		Joins("INNER JOIN threat_models ON documents.threat_model_id = threat_models.id").
		Where("documents.id = ? AND documents.deleted_at IS NULL", id).
		Take(&row)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, "", ErrDocumentNotFound
		}
		return nil, "", dberrors.Classify(result.Error)
	}

	var picker *PickerMetadata
	if row.PickerProviderID != nil && row.PickerFileID != nil && row.PickerMimeType != nil {
		picker = &PickerMetadata{
			ProviderID: row.PickerProviderID,
			FileID:     row.PickerFileID,
			MimeType:   row.PickerMimeType,
		}
	}
	return picker, row.OwnerInternalUUID, nil
}

// SetPickerMetadata persists picker_provider_id, picker_file_id, and
// picker_mime_type on the document. Resets access_status to 'unknown' so
// the poller will validate.
//
// Non-atomic with Create — see DocumentStore.SetPickerMetadata for the
// failure-handling contract.
//
// See DocumentStore.SetPickerMetadata.
func (s *GormDocumentRepository) SetPickerMetadata(
	ctx context.Context, id string, providerID, fileID, mimeType string,
) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// These picker/content_source columns are NullableDBVarchar; route
	// through the typed constructor so Value() normalizes an empty string
	// to NULL (#700) instead of writing "" verbatim.
	updates := map[string]interface{}{
		"picker_provider_id":       models.NewNullableDBVarchar(&providerID),
		"picker_file_id":           models.NewNullableDBVarchar(&fileID),
		"picker_mime_type":         models.NewNullableDBVarchar(&mimeType),
		"access_status":            AccessStatusUnknown,
		"content_source":           models.NewNullableDBVarchar(&providerID),
		"access_status_updated_at": time.Now().UTC().Truncate(time.Microsecond),
	}
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Table(models.Document{}.TableName()).
			Where("id = ? AND deleted_at IS NULL", id).
			Updates(AssignmentMap(tx.Name(), updates))
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrDocumentNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}
	if s.cache != nil {
		_ = s.cache.InvalidateEntity(ctx, "document", id)
	}
	return nil
}

// ClearPickerMetadataForOwner nulls picker metadata and access-diagnostic
// fields on all documents owned by the given user (via threat-model owner)
// that were picker-registered under the given provider.
// See DocumentStore.ClearPickerMetadataForOwner.
func (s *GormDocumentRepository) ClearPickerMetadataForOwner(
	ctx context.Context, ownerInternalUUID, providerID string,
) (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	updates := map[string]interface{}{
		"picker_provider_id":       nil,
		"picker_file_id":           nil,
		"picker_mime_type":         nil,
		"access_status":            AccessStatusUnknown,
		"access_reason_code":       nil,
		"access_reason_detail":     nil,
		"access_status_updated_at": time.Now().UTC().Truncate(time.Microsecond),
	}
	var rowsAffected int64
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Table(models.Document{}.TableName()).
			Where("picker_provider_id = ? AND threat_model_id IN (?)",
				providerID,
				tx.Table(models.ThreatModel{}.TableName()).Select("id").Where("owner_internal_uuid = ?", ownerInternalUUID),
			).
			Updates(AssignmentMap(tx.Name(), updates))
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		rowsAffected = result.RowsAffected
		return nil
	})
	if err != nil {
		return 0, err
	}
	// Cache invalidation: we don't know which document ids were affected,
	// so we skip targeted invalidation here — list/listing caches will naturally
	// reload after un-link.
	return rowsAffected, nil
}

// modelToAPI converts a GORM Document model to the API Document type
func (s *GormDocumentRepository) modelToAPI(model *models.Document) *Document {
	id, _ := uuid.Parse(string(model.ID))
	includeInReport := model.IncludeInReport.Bool()
	timmyEnabled := model.TimmyEnabled.Bool()
	alias := model.Alias
	doc := &Document{
		Id:              &id,
		Name:            string(model.Name),
		Uri:             string(model.URI),
		Description:     model.Description.Ptr(),
		IncludeInReport: &includeInReport,
		TimmyEnabled:    &timmyEnabled,
		Alias:           &alias,
	}

	// Include timestamps
	if !model.CreatedAt.IsZero() {
		doc.CreatedAt = &model.CreatedAt
	}
	if !model.ModifiedAt.IsZero() {
		doc.ModifiedAt = &model.ModifiedAt
	}

	// Map access tracking fields
	if model.AccessStatus.Valid {
		status := DocumentAccessStatus(model.AccessStatus.String)
		doc.AccessStatus = &status
	}
	if model.ContentSource.Valid {
		src := model.ContentSource.String
		doc.ContentSource = &src
	}

	return doc
}

// loadMetadata loads metadata for a document
func (s *GormDocumentRepository) loadMetadata(ctx context.Context, documentID string) ([]Metadata, error) {
	return loadEntityMetadata(s.db.WithContext(ctx), "document", documentID)
}

// saveMetadata saves metadata for a document
func (s *GormDocumentRepository) saveMetadata(ctx context.Context, documentID string, metadata []Metadata) error {
	return saveEntityMetadata(s.db.WithContext(ctx), "document", documentID, metadata)
}

// updateMetadata updates metadata for a document
func (s *GormDocumentRepository) updateMetadata(ctx context.Context, documentID string, metadata []Metadata) error {
	// One transaction so a failed insert rolls back the delete instead of
	// committing a bare metadata wipe (#670).
	return authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		return deleteAndSaveEntityMetadata(tx, "document", documentID, metadata)
	})
}

// documentPatchablePaths are the document fields a JSON Patch may change:
// exactly the columns update writes. Anything else (metadata, access status,
// alias, timestamps, ...) is rejected rather than silently dropped on save.
var documentPatchablePaths = map[string]bool{
	PatchPathName:        true,
	PatchPathURI:         true,
	PatchPathDescription: true,
	"/include_in_report": true,
	"/timmy_enabled":     true,
}

// applyDocumentPatch applies RFC 6902 operations to a document via the generic
// applier, then re-checks what the per-field code used to, because update runs
// with SkipHooks and so skips Document.BeforeSave: name and uri must be
// non-empty (NOT NULL columns; PostgreSQL stores ” but Oracle raises
// ORA-01407, #614). Unset flags get the schema default (true). Only the
// patchable fields are taken from the patched value; every other field is kept
// from the loaded row. Errors are 400 RequestErrors.
func applyDocumentPatch(document Document, operations []PatchOperation) (Document, error) {
	patched, err := applyEntityPatch(document, operations, "document", documentPatchablePaths, "document_update")
	if err != nil {
		return Document{}, err
	}
	if err := validation.ValidateNonEmpty("name", patched.Name); err != nil {
		return Document{}, InvalidInputError(err.Error())
	}
	if err := validation.ValidateURI("uri", patched.Uri); err != nil {
		return Document{}, InvalidInputError(err.Error())
	}
	result := document
	result.Name = patched.Name
	result.Uri = patched.Uri
	result.Description = patched.Description
	result.IncludeInReport = patched.IncludeInReport
	result.TimmyEnabled = patched.TimmyEnabled
	defaultTrue(&result.IncludeInReport)
	defaultTrue(&result.TimmyEnabled)
	return result, nil
}

// getDocumentThreatModelID retrieves the threat model ID for a document
func (s *GormDocumentRepository) getDocumentThreatModelID(ctx context.Context, documentID string) (string, error) {
	var model models.Document
	err := s.db.WithContext(ctx).Select("threat_model_id").First(&model, "id = ?", documentID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrDocumentNotFound
		}
		return "", dberrors.Classify(err)
	}
	return string(model.ThreatModelID), nil
}
