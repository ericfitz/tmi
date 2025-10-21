package api

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// DocumentStore defines the interface for document operations with caching support
// Note: Documents do not support PATCH operations per the implementation plan
type DocumentStore interface {
	// CRUD operations (no PATCH support)
	Create(ctx context.Context, document *Document, threatModelID string) error
	Get(ctx context.Context, id string) (*Document, error)
	Update(ctx context.Context, document *Document, threatModelID string) error
	Delete(ctx context.Context, id string) error

	// List operations with pagination
	List(ctx context.Context, threatModelID string, offset, limit int) ([]Document, error)

	// Bulk operations
	BulkCreate(ctx context.Context, documents []Document, threatModelID string) error

	// Cache management
	InvalidateCache(ctx context.Context, id string) error
	WarmCache(ctx context.Context, threatModelID string) error
}

// ExtendedDocument includes database fields not in the API model
type ExtendedDocument struct {
	Document
	ThreatModelId uuid.UUID `json:"threat_model_id"`
	CreatedAt     time.Time `json:"created_at"`
	ModifiedAt    time.Time `json:"modified_at"`
}

// DatabaseDocumentStore implements DocumentStore with database persistence and Redis caching
type DatabaseDocumentStore struct {
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewDatabaseDocumentStore creates a new database-backed document store with caching
func NewDatabaseDocumentStore(db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *DatabaseDocumentStore {
	return &DatabaseDocumentStore{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// documentToExtended converts a Document to ExtendedDocument
func documentToExtended(doc *Document, threatModelID string, createdAt, modifiedAt time.Time) *ExtendedDocument {
	tmID, _ := uuid.Parse(threatModelID)
	return &ExtendedDocument{
		Document:      *doc,
		ThreatModelId: tmID,
		CreatedAt:     createdAt,
		ModifiedAt:    modifiedAt,
	}
}

// extendedToDocument converts an ExtendedDocument to Document
func extendedToDocument(extDoc *ExtendedDocument) *Document {
	return &extDoc.Document
}

// Create creates a new document with write-through caching
func (s *DatabaseDocumentStore) Create(ctx context.Context, document *Document, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Creating document: %s in threat model: %s", document.Name, threatModelID)

	// Generate ID if not provided
	if document.Id == nil {
		id := uuid.New()
		document.Id = &id
	}

	// Parse threat model ID
	tmID, err := uuid.Parse(threatModelID)
	if err != nil {
		logger.Error("Invalid threat model ID: %s", threatModelID)
		return fmt.Errorf("invalid threat model ID: %w", err)
	}

	// Set timestamps
	now := time.Now().UTC()

	// Insert into database
	query := `
		INSERT INTO documents (
			id, threat_model_id, name, uri, description, created_at, modified_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		)
	`

	_, err = s.db.ExecContext(ctx, query,
		document.Id,
		tmID,
		document.Name,
		document.Uri,
		document.Description,
		now,
		now,
	)

	if err != nil {
		logger.Error("Failed to create document in database: %v", err)
		return fmt.Errorf("failed to create document: %w", err)
	}

	// Cache the new document
	if s.cache != nil {
		extDoc := documentToExtended(document, threatModelID, now, now)
		if cacheErr := s.cache.CacheDocument(ctx, &extDoc.Document); cacheErr != nil {
			logger.Error("Failed to cache new document: %v", cacheErr)
			// Don't fail the request if caching fails
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

// Get retrieves a document by ID with cache-first strategy
func (s *DatabaseDocumentStore) Get(ctx context.Context, id string) (*Document, error) {
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

	query := `
		SELECT id, threat_model_id, name, uri, description, created_at, modified_at
		FROM documents
		WHERE id = $1
	`

	var extDoc ExtendedDocument
	var description sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&extDoc.Id,
		&extDoc.ThreatModelId,
		&extDoc.Name,
		&extDoc.Uri,
		&description,
		&extDoc.CreatedAt,
		&extDoc.ModifiedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("document not found: %s", id)
		}
		logger.Error("Failed to get document from database: %v", err)
		return nil, fmt.Errorf("failed to get document: %w", err)
	}

	// Handle nullable fields
	if description.Valid {
		extDoc.Description = &description.String
	}

	document := extendedToDocument(&extDoc)

	// Load metadata
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		logger.Error("Failed to load metadata for document %s: %v", id, err)
		// Don't fail the request if metadata loading fails, just set empty metadata
		metadata = []Metadata{}
	}
	document.Metadata = &metadata

	// Cache the result for future requests
	if s.cache != nil {
		if cacheErr := s.cache.CacheDocument(ctx, document); cacheErr != nil {
			logger.Error("Failed to cache document after database fetch: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved document: %s", id)
	return document, nil
}

// Update updates an existing document with write-through caching
func (s *DatabaseDocumentStore) Update(ctx context.Context, document *Document, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Updating document: %s", document.Id)

	// Parse threat model ID
	tmID, err := uuid.Parse(threatModelID)
	if err != nil {
		logger.Error("Invalid threat model ID: %s", threatModelID)
		return fmt.Errorf("invalid threat model ID: %w", err)
	}

	// Update timestamp
	now := time.Now().UTC()

	query := `
		UPDATE documents SET
			name = $2, uri= $3, description = $4, modified_at = $5
		WHERE id = $1 AND threat_model_id = $6
	`

	result, err := s.db.ExecContext(ctx, query,
		document.Id,
		document.Name,
		document.Uri,
		document.Description,
		now,
		tmID,
	)

	if err != nil {
		logger.Error("Failed to update document in database: %v", err)
		return fmt.Errorf("failed to update document: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected: %v", err)
		return fmt.Errorf("failed to verify update: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("document not found: %s", document.Id)
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
	return nil
}

// Delete removes a document and invalidates related caches
func (s *DatabaseDocumentStore) Delete(ctx context.Context, id string) error {
	logger := slogging.Get()
	logger.Debug("Deleting document: %s", id)

	// Get the threat model ID from database for cache invalidation
	// We need this since the Document struct doesn't contain the threat_model_id field
	var threatModelID uuid.UUID
	query := `SELECT threat_model_id FROM documents WHERE id = $1`
	err := s.db.QueryRowContext(ctx, query, id).Scan(&threatModelID)
	if err != nil {
		logger.Error("Failed to get threat model ID for document %s: %v", id, err)
		return fmt.Errorf("failed to get document parent: %w", err)
	}

	// Delete from database
	deleteQuery := `DELETE FROM documents WHERE id = $1`
	result, err := s.db.ExecContext(ctx, deleteQuery, id)
	if err != nil {
		logger.Error("Failed to delete document from database: %v", err)
		return fmt.Errorf("failed to delete document: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected: %v", err)
		return fmt.Errorf("failed to verify deletion: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("document not found: %s", id)
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
			ParentID:      threatModelID.String(),
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

// List retrieves documents for a threat model with pagination and caching
func (s *DatabaseDocumentStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Document, error) {
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

	query := `
		SELECT id, threat_model_id, name, uri, description, created_at, modified_at
		FROM documents
		WHERE threat_model_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.QueryContext(ctx, query, threatModelID, limit, offset)
	if err != nil {
		logger.Error("Failed to query documents from database: %v", err)
		return nil, fmt.Errorf("failed to list documents: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	documents = make([]Document, 0)
	for rows.Next() {
		var extDoc ExtendedDocument
		var description sql.NullString

		err := rows.Scan(
			&extDoc.Id,
			&extDoc.ThreatModelId,
			&extDoc.Name,
			&extDoc.Uri,
			&description,
			&extDoc.CreatedAt,
			&extDoc.ModifiedAt,
		)

		if err != nil {
			logger.Error("Failed to scan document row: %v", err)
			return nil, fmt.Errorf("failed to scan document: %w", err)
		}

		// Handle nullable fields
		if description.Valid {
			extDoc.Description = &description.String
		}

		document := extendedToDocument(&extDoc)

		// Load metadata for this document
		metadata, metaErr := s.loadMetadata(ctx, extDoc.Id.String())
		if metaErr != nil {
			logger.Error("Failed to load metadata for document %s: %v", extDoc.Id.String(), metaErr)
			// Don't fail the request if metadata loading fails, just set empty metadata
			metadata = []Metadata{}
		}
		document.Metadata = &metadata

		documents = append(documents, *document)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating document rows: %v", err)
		return nil, fmt.Errorf("error iterating documents: %w", err)
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

// BulkCreate creates multiple documents in a single transaction
func (s *DatabaseDocumentStore) BulkCreate(ctx context.Context, documents []Document, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Bulk creating %d documents", len(documents))

	if len(documents) == 0 {
		return nil
	}

	// Parse threat model ID
	tmID, err := uuid.Parse(threatModelID)
	if err != nil {
		logger.Error("Invalid threat model ID: %s", threatModelID)
		return fmt.Errorf("invalid threat model ID: %w", err)
	}

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		logger.Error("Failed to begin transaction: %v", err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logger.Error("Failed to rollback transaction: %v", rollbackErr)
			}
		}
	}()

	query := `
		INSERT INTO documents (
			id, threat_model_id, name, uri, description, created_at, modified_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		)
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		logger.Error("Failed to prepare bulk insert statement: %v", err)
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			logger.Error("Failed to close statement: %v", closeErr)
		}
	}()

	now := time.Now().UTC()

	for i := range documents {
		document := &documents[i]

		// Generate ID if not provided
		if document.Id == nil {
			id := uuid.New()
			document.Id = &id
		}

		_, err = stmt.ExecContext(ctx,
			document.Id,
			tmID,
			document.Name,
			document.Uri,
			document.Description,
			now,
			now,
		)

		if err != nil {
			logger.Error("Failed to execute bulk insert for document %d: %v", i, err)
			return fmt.Errorf("failed to insert document %d: %w", i, err)
		}
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		logger.Error("Failed to commit bulk create transaction: %v", err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		if invErr := s.cacheInvalidator.InvalidateAllRelatedCaches(ctx, threatModelID); invErr != nil {
			logger.Error("Failed to invalidate caches after bulk document creation: %v", invErr)
		}
	}

	logger.Debug("Successfully bulk created %d documents", len(documents))
	return nil
}

// InvalidateCache removes document-related cache entries
func (s *DatabaseDocumentStore) InvalidateCache(ctx context.Context, id string) error {
	if s.cache == nil {
		return nil
	}

	return s.cache.InvalidateEntity(ctx, "document", id)
}

// WarmCache preloads documents for a threat model into cache
func (s *DatabaseDocumentStore) WarmCache(ctx context.Context, threatModelID string) error {
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

	// Individual documents are already cached by List(), so we're done
	logger.Debug("Warmed cache with %d documents for threat model %s", len(documents), threatModelID)
	return nil
}

// loadMetadata loads metadata for a document from the metadata table
func (s *DatabaseDocumentStore) loadMetadata(ctx context.Context, documentID string) ([]Metadata, error) {
	query := `
		SELECT key, value 
		FROM metadata 
		WHERE entity_type = 'document' AND entity_id = $1
		ORDER BY key ASC
	`

	rows, err := s.db.QueryContext(ctx, query, documentID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Error closing rows, but don't fail the operation
			_ = err
		}
	}()

	var metadata []Metadata
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		metadata = append(metadata, Metadata{
			Key:   key,
			Value: value,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating metadata: %w", err)
	}

	return metadata, nil
}
