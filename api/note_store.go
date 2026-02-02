package api

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// NoteStore defines the interface for note operations with caching support
type NoteStore interface {
	// CRUD operations
	Create(ctx context.Context, note *Note, threatModelID string) error
	Get(ctx context.Context, id string) (*Note, error)
	Update(ctx context.Context, note *Note, threatModelID string) error
	Delete(ctx context.Context, id string) error
	Patch(ctx context.Context, id string, operations []PatchOperation) (*Note, error)

	// List operations with pagination
	List(ctx context.Context, threatModelID string, offset, limit int) ([]Note, error)
	// Count returns total number of notes for a threat model
	Count(ctx context.Context, threatModelID string) (int, error)

	// Cache management
	InvalidateCache(ctx context.Context, id string) error
	WarmCache(ctx context.Context, threatModelID string) error
}

// ExtendedNote includes database fields not in the API model
type ExtendedNote struct {
	Note
	ThreatModelId uuid.UUID `json:"threat_model_id"`
	CreatedAt     time.Time `json:"created_at"`
	ModifiedAt    time.Time `json:"modified_at"`
}

// DatabaseNoteStore implements NoteStore with database persistence and Redis caching
type DatabaseNoteStore struct {
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewDatabaseNoteStore creates a new database-backed note store with caching
func NewDatabaseNoteStore(db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *DatabaseNoteStore {
	return &DatabaseNoteStore{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// noteToExtended converts a Note to ExtendedNote
func noteToExtended(note *Note, threatModelID string, createdAt, modifiedAt time.Time) *ExtendedNote {
	tmID, _ := uuid.Parse(threatModelID)
	return &ExtendedNote{
		Note:          *note,
		ThreatModelId: tmID,
		CreatedAt:     createdAt,
		ModifiedAt:    modifiedAt,
	}
}

// extendedToNote converts an ExtendedNote to Note
func extendedToNote(extNote *ExtendedNote) *Note {
	return &extNote.Note
}

// Create creates a new note with write-through caching
func (s *DatabaseNoteStore) Create(ctx context.Context, note *Note, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Creating note: %s in threat model: %s", note.Name, threatModelID)

	// Generate ID if not provided
	if note.Id == nil {
		id := uuid.New()
		note.Id = &id
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
		INSERT INTO notes (
			id, threat_model_id, name, content, description, created_at, modified_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		)
	`

	_, err = s.db.ExecContext(ctx, query,
		note.Id,
		tmID,
		note.Name,
		note.Content,
		note.Description,
		now,
		now,
	)

	if err != nil {
		logger.Error("Failed to create note in database: %v", err)
		return fmt.Errorf("failed to create note: %w", err)
	}

	// Cache the new note
	if s.cache != nil {
		extNote := noteToExtended(note, threatModelID, now, now)
		if cacheErr := s.cache.CacheNote(ctx, &extNote.Note); cacheErr != nil {
			logger.Error("Failed to cache new note: %v", cacheErr)
			// Don't fail the request if caching fails
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "note",
			EntityID:      note.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threatModelID,
			OperationType: "create",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after note creation: %v", invErr)
		}
	}

	logger.Debug("Successfully created note: %s", note.Id)
	return nil
}

// Get retrieves a note by ID with cache-first strategy
func (s *DatabaseNoteStore) Get(ctx context.Context, id string) (*Note, error) {
	logger := slogging.Get()
	logger.Debug("Getting note: %s", id)

	// Try cache first
	if s.cache != nil {
		note, err := s.cache.GetCachedNote(ctx, id)
		if err != nil {
			logger.Error("Cache error when getting note %s: %v", id, err)
		} else if note != nil {
			logger.Debug("Cache hit for note: %s", id)
			return note, nil
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for note %s, querying database", id)

	query := `
		SELECT id, threat_model_id, name, content, description, created_at, modified_at
		FROM notes
		WHERE id = $1
	`

	var extNote ExtendedNote
	var description sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&extNote.Id,
		&extNote.ThreatModelId,
		&extNote.Name,
		&extNote.Content,
		&description,
		&extNote.CreatedAt,
		&extNote.ModifiedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("note not found: %s", id)
		}
		logger.Error("Failed to get note from database: %v", err)
		return nil, fmt.Errorf("failed to get note: %w", err)
	}

	// Handle nullable fields
	if description.Valid {
		extNote.Description = &description.String
	}

	note := extendedToNote(&extNote)

	// Load metadata
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		logger.Error("Failed to load metadata for note %s: %v", id, err)
		// Don't fail the request if metadata loading fails, just set empty metadata
		metadata = []Metadata{}
	}
	note.Metadata = &metadata

	// Cache the result for future requests
	if s.cache != nil {
		if cacheErr := s.cache.CacheNote(ctx, note); cacheErr != nil {
			logger.Error("Failed to cache note after database fetch: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved note: %s", id)
	return note, nil
}

// Update updates an existing note with write-through caching
func (s *DatabaseNoteStore) Update(ctx context.Context, note *Note, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Updating note: %s", note.Id)

	// Parse threat model ID
	tmID, err := uuid.Parse(threatModelID)
	if err != nil {
		logger.Error("Invalid threat model ID: %s", threatModelID)
		return fmt.Errorf("invalid threat model ID: %w", err)
	}

	// Update timestamp
	now := time.Now().UTC()

	query := `
		UPDATE notes SET
			name = $2, content = $3, description = $4, modified_at = $5
		WHERE id = $1 AND threat_model_id = $6
	`

	result, err := s.db.ExecContext(ctx, query,
		note.Id,
		note.Name,
		note.Content,
		note.Description,
		now,
		tmID,
	)

	if err != nil {
		logger.Error("Failed to update note in database: %v", err)
		return fmt.Errorf("failed to update note: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected: %v", err)
		return fmt.Errorf("failed to verify update: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("note not found: %s", note.Id)
	}

	// Update cache
	if s.cache != nil {
		if cacheErr := s.cache.CacheNote(ctx, note); cacheErr != nil {
			logger.Error("Failed to update note cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "note",
			EntityID:      note.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threatModelID,
			OperationType: "update",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after note update: %v", invErr)
		}
	}

	logger.Debug("Successfully updated note: %s", note.Id)
	return nil
}

// Delete removes a note and invalidates related caches
func (s *DatabaseNoteStore) Delete(ctx context.Context, id string) error {
	logger := slogging.Get()
	logger.Debug("Deleting note: %s", id)

	// Get the threat model ID from database for cache invalidation
	var threatModelID uuid.UUID
	query := `SELECT threat_model_id FROM notes WHERE id = $1`
	err := s.db.QueryRowContext(ctx, query, id).Scan(&threatModelID)
	if err != nil {
		logger.Error("Failed to get threat model ID for note %s: %v", id, err)
		return fmt.Errorf("failed to get note parent: %w", err)
	}

	// Delete from database
	deleteQuery := `DELETE FROM notes WHERE id = $1`
	result, err := s.db.ExecContext(ctx, deleteQuery, id)
	if err != nil {
		logger.Error("Failed to delete note from database: %v", err)
		return fmt.Errorf("failed to delete note: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected: %v", err)
		return fmt.Errorf("failed to verify deletion: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("note not found: %s", id)
	}

	// Remove from cache
	if s.cache != nil {
		if cacheErr := s.cache.InvalidateEntity(ctx, "note", id); cacheErr != nil {
			logger.Error("Failed to remove note from cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "note",
			EntityID:      id,
			ParentType:    "threat_model",
			ParentID:      threatModelID.String(),
			OperationType: "delete",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after note deletion: %v", invErr)
		}
	}

	logger.Debug("Successfully deleted note: %s", id)
	return nil
}

// List retrieves notes for a threat model with pagination and caching
func (s *DatabaseNoteStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Note, error) {
	logger := slogging.Get()
	logger.Debug("Listing notes for threat model %s (offset: %d, limit: %d)", threatModelID, offset, limit)

	// Try cache first
	var notes []Note
	if s.cache != nil {
		err := s.cache.GetCachedList(ctx, "notes", threatModelID, offset, limit, &notes)
		if err == nil && notes != nil {
			logger.Debug("Cache hit for note list %s [%d:%d]", threatModelID, offset, limit)
			return notes, nil
		}
		if err != nil {
			logger.Error("Cache error when getting note list: %v", err)
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for note list, querying database")

	query := `
		SELECT id, threat_model_id, name, content, description, created_at, modified_at
		FROM notes
		WHERE threat_model_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.QueryContext(ctx, query, threatModelID, limit, offset)
	if err != nil {
		logger.Error("Failed to query notes from database: %v", err)
		return nil, fmt.Errorf("failed to list notes: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	notes = make([]Note, 0)
	for rows.Next() {
		var extNote ExtendedNote
		var description sql.NullString

		err := rows.Scan(
			&extNote.Id,
			&extNote.ThreatModelId,
			&extNote.Name,
			&extNote.Content,
			&description,
			&extNote.CreatedAt,
			&extNote.ModifiedAt,
		)

		if err != nil {
			logger.Error("Failed to scan note row: %v", err)
			return nil, fmt.Errorf("failed to scan note: %w", err)
		}

		// Handle nullable fields
		if description.Valid {
			extNote.Description = &description.String
		}

		note := extendedToNote(&extNote)

		// Load metadata for this note
		metadata, metaErr := s.loadMetadata(ctx, extNote.Id.String())
		if metaErr != nil {
			logger.Error("Failed to load metadata for note %s: %v", extNote.Id.String(), metaErr)
			// Don't fail the request if metadata loading fails, just set empty metadata
			metadata = []Metadata{}
		}
		note.Metadata = &metadata

		notes = append(notes, *note)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating note rows: %v", err)
		return nil, fmt.Errorf("error iterating notes: %w", err)
	}

	// Cache the result
	if s.cache != nil {
		if cacheErr := s.cache.CacheList(ctx, "notes", threatModelID, offset, limit, notes); cacheErr != nil {
			logger.Error("Failed to cache note list: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved %d notes", len(notes))
	return notes, nil
}

// Count returns the total number of notes for a threat model
func (s *DatabaseNoteStore) Count(ctx context.Context, threatModelID string) (int, error) {
	logger := slogging.Get()
	logger.Debug("Counting notes for threat model %s", threatModelID)

	query := `SELECT COUNT(*) FROM notes WHERE threat_model_id = $1`
	var count int
	err := s.db.QueryRowContext(ctx, query, threatModelID).Scan(&count)
	if err != nil {
		logger.Error("Failed to count notes: %v", err)
		return 0, fmt.Errorf("failed to count notes: %w", err)
	}

	return count, nil
}

// InvalidateCache removes note-related cache entries
func (s *DatabaseNoteStore) InvalidateCache(ctx context.Context, id string) error {
	if s.cache == nil {
		return nil
	}

	return s.cache.InvalidateEntity(ctx, "note", id)
}

// WarmCache preloads notes for a threat model into cache
func (s *DatabaseNoteStore) WarmCache(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Warming cache for threat model notes: %s", threatModelID)

	if s.cache == nil {
		return nil
	}

	// Load first page of notes
	notes, err := s.List(ctx, threatModelID, 0, 50)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	// Individual notes are already cached by List(), so we're done
	logger.Debug("Warmed cache with %d notes for threat model %s", len(notes), threatModelID)
	return nil
}

// loadMetadata loads metadata for a note from the metadata table
func (s *DatabaseNoteStore) loadMetadata(ctx context.Context, noteID string) ([]Metadata, error) {
	query := `
		SELECT key, value
		FROM metadata
		WHERE entity_type = 'note' AND entity_id = $1
		ORDER BY key ASC
	`

	rows, err := s.db.QueryContext(ctx, query, noteID)
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

// Patch applies JSON patch operations to a note
func (s *DatabaseNoteStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Note, error) {
	logger := slogging.Get()
	logger.Debug("Patching note %s with %d operations", id, len(operations))

	// Get current note
	note, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply patch operations
	for _, op := range operations {
		if err := s.applyPatchOperation(note, op); err != nil {
			logger.Error("Failed to apply patch operation %s to note %s: %v", op.Op, id, err)
			return nil, fmt.Errorf("failed to apply patch operation: %w", err)
		}
	}

	// Get threat model ID for update
	threatModelID, err := s.getNoteThreatModelID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get threat model ID: %w", err)
	}

	// Update the note
	if err := s.Update(ctx, note, threatModelID); err != nil {
		return nil, err
	}

	return note, nil
}

// applyPatchOperation applies a single patch operation to a note
func (s *DatabaseNoteStore) applyPatchOperation(note *Note, op PatchOperation) error {
	switch op.Path {
	case "/name":
		if op.Op == "replace" {
			if name, ok := op.Value.(string); ok {
				note.Name = name
			} else {
				return fmt.Errorf("invalid value type for name: expected string")
			}
		}
	case "/content":
		if op.Op == "replace" {
			if content, ok := op.Value.(string); ok {
				note.Content = content
			} else {
				return fmt.Errorf("invalid value type for content: expected string")
			}
		}
	case "/description":
		switch op.Op {
		case "replace", "add":
			if desc, ok := op.Value.(string); ok {
				note.Description = &desc
			} else {
				return fmt.Errorf("invalid value type for description: expected string")
			}
		case "remove":
			note.Description = nil
		}
	default:
		return fmt.Errorf("unsupported patch path: %s", op.Path)
	}
	return nil
}

// getNoteThreatModelID retrieves the threat model ID for a note
func (s *DatabaseNoteStore) getNoteThreatModelID(ctx context.Context, noteID string) (string, error) {
	query := `SELECT threat_model_id FROM notes WHERE id = $1`
	var threatModelID string
	err := s.db.QueryRowContext(ctx, query, noteID).Scan(&threatModelID)
	if err != nil {
		return "", fmt.Errorf("failed to get threat model ID for note: %w", err)
	}
	return threatModelID, nil
}
