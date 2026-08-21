package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/api/validation"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/uuidgen"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// metadataBatchSize caps how many rows CreateInBatches sends per round trip.
// 100 keeps each batch under Oracle's max-bind-variable limit (700 binds at 7
// columns/row) while collapsing the spec-capped 100-entry bulk request into a
// single statement instead of one per row. The ADB-verified precedent for the
// multi-row shape is api/system_audit_prune_oracle_test.go (1,100 rows through
// the identical CreateInBatches path, recorded PASS against live ADB).
//
// ORACLE VERSION FLOOR: gorm-oracle emits multi-row `VALUES (:1,...),(:8,...)`
// for plain batched creates, which Oracle only accepts from 23ai (the table
// value constructor); 19c/21c raise ORA-00933, which dberrors does not map, so
// on an older ADB every multi-entry BulkCreate/BulkReplace would 500. TMI's
// fleet and the terraform default (terraform/modules/database/oci) are 23ai —
// do not provision an older version (oracle-db-admin review of #666).
const metadataBatchSize = 100

// GormMetadataRepository implements MetadataRepository using GORM.
//
// No mutex: earlier versions serialized every metadata write in the process
// behind a sync.RWMutex, which protected nothing (GORM connections are
// concurrency-safe and the transactions below already provide atomicity) while
// turning every bulk write into a process-wide bottleneck. In a multi-replica
// deployment concurrent writers are in different processes anyway, so the
// mutex bought no additional safety there either (#666).
// SEM@0000000000000000000000000000000000000000: GORM-backed repository for entity metadata key-value pairs with cache and invalidation support
type GormMetadataRepository struct {
	db               *gorm.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
	logger           *slogging.Logger
}

// NewGormMetadataRepository creates a new GORM-backed metadata repository
// SEM@4b5601a9cbb59c0d9d34db8808624707ebd7501e: build a GormMetadataRepository with the given DB, cache, and invalidator (pure)
func NewGormMetadataRepository(db *gorm.DB, cache *CacheService, invalidator *CacheInvalidator) *GormMetadataRepository {
	return &GormMetadataRepository{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
		logger:           slogging.Get(),
	}
}

// validateEntityType checks if the entity type is supported.
// Delegates to the canonical validation.ValidEntityTypes list to ensure consistency
// with the GORM BeforeSave hook in models/hooks.go.
// SEM@4b5601a9cbb59c0d9d34db8808624707ebd7501e: validate that the entity type is in the canonical allowed list (pure)
func (r *GormMetadataRepository) validateEntityType(entityType string) error {
	return validation.ValidateEntityType(entityType)
}

// Create creates a new metadata entry
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: store a single metadata entry for an entity, rejecting duplicate keys (mutates shared state)
func (r *GormMetadataRepository) Create(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	r.logger.Debug("Creating metadata: %s=%s for %s:%s", metadata.Key, metadata.Value, entityType, entityID)

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return err
	}

	now := time.Now().UTC()

	model := models.Metadata{
		ID:         models.DBVarchar(uuidgen.MustNewForEntity(uuidgen.EntityTypeMetadata).String()),
		EntityType: models.DBVarchar(entityType),
		EntityID:   models.DBVarchar(entityID),
		Key:        models.DBVarchar(metadata.Key),
		Value:      models.DBVarchar(metadata.Value),
		CreatedAt:  now,
		ModifiedAt: now,
	}

	err := authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Create(&model)
		if result.Error != nil {
			classified := dberrors.Classify(result.Error)
			if errors.Is(classified, dberrors.ErrDuplicate) {
				return &MetadataConflictError{ConflictingKeys: []string{metadata.Key}}
			}
			return classified
		}
		return nil
	})

	if err != nil {
		r.logger.Error("Failed to create metadata in database: %v", err)
		return err
	}

	// Invalidate related caches
	if r.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "metadata",
			EntityID:      string(model.ID),
			ParentType:    entityType,
			ParentID:      entityID,
			OperationType: "create",
			Strategy:      InvalidateImmediately,
		}
		if invErr := r.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			r.logger.Error("Failed to invalidate caches after metadata creation: %v", invErr)
		}
	}

	r.logger.Debug("Successfully created metadata: %s=%s", metadata.Key, metadata.Value)
	return nil
}

// Get retrieves a specific metadata entry by key
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: fetch a metadata entry by key, using the cache when available (reads DB)
func (r *GormMetadataRepository) Get(ctx context.Context, entityType, entityID, key string) (*Metadata, error) {
	r.logger.Debug("Getting metadata: %s for %s:%s", key, entityType, entityID)

	// Try cache first
	if r.cache != nil {
		metadataList, err := r.cache.GetCachedMetadata(ctx, entityType, entityID)
		if err != nil {
			r.logger.Error("Cache error when getting metadata %s:%s: %v", entityType, entityID, err)
		} else if metadataList != nil {
			for _, meta := range metadataList {
				if meta.Key == key {
					r.logger.Debug("Cache hit for metadata: %s", key)
					return &meta, nil
				}
			}
			return nil, ErrMetadataNotFound
		}
	}

	// Cache miss - get from database
	r.logger.Debug("Cache miss for metadata %s, querying database", key)

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return nil, err
	}

	var model models.Metadata
	result := r.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ? AND key = ?", entityType, entityID, key).
		First(&model)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrMetadataNotFound
		}
		r.logger.Error("Failed to get metadata from database: %v", result.Error)
		return nil, dberrors.Classify(result.Error)
	}

	metadata := &Metadata{
		Key:   string(model.Key),
		Value: string(model.Value),
	}

	r.logger.Debug("Successfully retrieved metadata: %s=%s", metadata.Key, metadata.Value)
	return metadata, nil
}

// Update updates an existing metadata entry
// SEM@c65573c7e7d2c1566c489a62f575cb72550438f9: update a metadata entry's value, setting modified_at explicitly (mutates shared state)
func (r *GormMetadataRepository) Update(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	r.logger.Debug("Updating metadata: %s=%s for %s:%s", metadata.Key, metadata.Value, entityType, entityID)

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return err
	}

	err := authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Skip hooks to avoid validation errors on empty model struct.
		// Entity type is already validated above.
		//
		// modified_at explicitly: SkipHooks suppresses GORM's autoUpdateTime
		// injector, so the old comment here ("handled automatically by GORM's
		// autoUpdateTime tag") was false and metadata.modified_at sat frozen at
		// created_at for the life of every row on both dialects. This is the
		// third instance of the same defect; #610 fixed the document and
		// repository stores and missed this one.
		//
		// INVARIANT: the lowercase key is safe ONLY while SkipHooks is set —
		// see the fuller note in document_store_gorm.go for why re-enabling
		// hooks would raise ORA-00957 on Oracle but not PostgreSQL.
		result := tx.Session(&gorm.Session{SkipHooks: true}).Model(&models.Metadata{}).
			Where("entity_type = ? AND entity_id = ? AND key = ?", entityType, entityID, metadata.Key).
			Updates(AssignmentMap(tx.Name(), map[string]any{
				"value":       metadata.Value,
				"modified_at": time.Now().UTC(),
			}))

		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}

		if result.RowsAffected == 0 {
			return ErrMetadataNotFound
		}

		return nil
	})

	if err != nil {
		r.logger.Error("Failed to update metadata in database: %v", err)
		return err
	}

	// Invalidate related caches
	if r.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "metadata",
			EntityID:      fmt.Sprintf("%s:%s:%s", entityType, entityID, metadata.Key),
			ParentType:    entityType,
			ParentID:      entityID,
			OperationType: "update",
			Strategy:      InvalidateImmediately,
		}
		if invErr := r.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			r.logger.Error("Failed to invalidate caches after metadata update: %v", invErr)
		}
	}

	r.logger.Debug("Successfully updated metadata: %s=%s", metadata.Key, metadata.Value)
	return nil
}

// Delete removes a metadata entry
// SEM@4b5601a9cbb59c0d9d34db8808624707ebd7501e: delete a metadata entry by key and invalidate related caches (mutates shared state)
func (r *GormMetadataRepository) Delete(ctx context.Context, entityType, entityID, key string) error {
	r.logger.Debug("Deleting metadata: %s for %s:%s", key, entityType, entityID)

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return err
	}

	err := authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.
			Where("entity_type = ? AND entity_id = ? AND key = ?", entityType, entityID, key).
			Delete(&models.Metadata{})

		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}

		if result.RowsAffected == 0 {
			return ErrMetadataNotFound
		}

		return nil
	})

	if err != nil {
		r.logger.Error("Failed to delete metadata from database: %v", err)
		return err
	}

	// Invalidate related caches
	if r.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "metadata",
			EntityID:      fmt.Sprintf("%s:%s:%s", entityType, entityID, key),
			ParentType:    entityType,
			ParentID:      entityID,
			OperationType: "delete",
			Strategy:      InvalidateImmediately,
		}
		if invErr := r.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			r.logger.Error("Failed to invalidate caches after metadata deletion: %v", invErr)
		}
	}

	r.logger.Debug("Successfully deleted metadata: %s", key)
	return nil
}

// List retrieves all metadata for an entity
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: list all metadata entries for an entity, using the cache when available (reads DB)
func (r *GormMetadataRepository) List(ctx context.Context, entityType, entityID string) ([]Metadata, error) {
	r.logger.Debug("Listing metadata for %s:%s", entityType, entityID)

	// Try cache first
	if r.cache != nil {
		metadataList, err := r.cache.GetCachedMetadata(ctx, entityType, entityID)
		if err != nil {
			r.logger.Error("Cache error when getting metadata list %s:%s: %v", entityType, entityID, err)
		} else if metadataList != nil {
			r.logger.Debug("Cache hit for metadata list %s:%s", entityType, entityID)
			return metadataList, nil
		}
	}

	// Cache miss - get from database
	r.logger.Debug("Cache miss for metadata list, querying database")

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return nil, err
	}

	var modelList []models.Metadata
	result := r.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Order("key ASC").
		Find(&modelList)

	if result.Error != nil {
		r.logger.Error("Failed to query metadata from database: %v", result.Error)
		return nil, dberrors.Classify(result.Error)
	}

	metadataList := make([]Metadata, 0, len(modelList))
	for _, model := range modelList {
		metadataList = append(metadataList, Metadata{
			Key:   string(model.Key),
			Value: string(model.Value),
		})
	}

	// Cache the result
	if r.cache != nil {
		if cacheErr := r.cache.CacheMetadata(ctx, entityType, entityID, metadataList); cacheErr != nil {
			r.logger.Error("Failed to cache metadata list: %v", cacheErr)
		}
	}

	r.logger.Debug("Successfully retrieved %d metadata entries", len(metadataList))
	return metadataList, nil
}

// Post creates a new metadata entry using POST semantics
// SEM@4b5601a9cbb59c0d9d34db8808624707ebd7501e: create a metadata entry using POST semantics, delegating to Create (mutates shared state)
func (r *GormMetadataRepository) Post(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	r.logger.Debug("Posting metadata: %s=%s for %s:%s", metadata.Key, metadata.Value, entityType, entityID)

	// POST semantics: create regardless of existing keys, let the database handle conflicts
	return r.Create(ctx, entityType, entityID, metadata)
}

// BulkCreate creates multiple metadata entries in a single transaction
// SEM@0000000000000000000000000000000000000000: batch-insert multiple metadata entries in one transaction, rejecting any duplicate keys (mutates shared state)
func (r *GormMetadataRepository) BulkCreate(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	r.logger.Debug("Bulk creating %d metadata entries", len(metadata))

	if len(metadata) == 0 {
		return nil
	}

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return err
	}

	now := time.Now().UTC()

	// keys is also the fallback conflict set: if a duplicate-key error surfaces
	// from the batch insert but the existing-keys probe below can't name the
	// culprit (a phantom read of an uncommitted concurrent winner), report all
	// requested keys rather than none.
	keys := make([]string, len(metadata))
	entries := make([]models.Metadata, len(metadata))
	for i, meta := range metadata {
		keys[i] = meta.Key
		entries[i] = models.Metadata{
			ID:         models.DBVarchar(uuidgen.MustNewForEntity(uuidgen.EntityTypeMetadata).String()),
			EntityType: models.DBVarchar(entityType),
			EntityID:   models.DBVarchar(entityID),
			Key:        models.DBVarchar(meta.Key),
			Value:      models.DBVarchar(meta.Value),
			CreatedAt:  now,
			ModifiedAt: now,
		}
	}

	return authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Check for existing keys (create-only semantics)
		var existingKeys []string
		if err := tx.Model(&models.Metadata{}).
			Where("entity_type = ? AND entity_id = ? AND key IN ?", entityType, entityID, keys).
			Pluck("key", &existingKeys).Error; err != nil {
			r.logger.Error("Failed to check existing keys: %v", err)
			return dberrors.Classify(err)
		}

		if len(existingKeys) > 0 {
			return &MetadataConflictError{ConflictingKeys: existingKeys}
		}

		// Insert new entries in batches (no upsert)
		if err := tx.CreateInBatches(&entries, metadataBatchSize).Error; err != nil {
			classified := dberrors.Classify(err)
			if errors.Is(classified, dberrors.ErrDuplicate) {
				// Lost a create race against a concurrent writer between the
				// probe above and this insert. Re-run the same probe inside
				// this tx to name the actual conflicting key(s); if nothing
				// comes back (e.g. the winner hasn't committed yet on this
				// isolation level), fall back to naming every requested key
				// rather than swallowing the conflict.
				var raceKeys []string
				if probeErr := tx.Model(&models.Metadata{}).
					Where("entity_type = ? AND entity_id = ? AND key IN ?", entityType, entityID, keys).
					Pluck("key", &raceKeys).Error; probeErr != nil {
					r.logger.Error("Failed to probe conflicting keys after bulk create race: %v", probeErr)
				}
				if len(raceKeys) == 0 {
					raceKeys = keys
				}
				return &MetadataConflictError{ConflictingKeys: raceKeys}
			}
			r.logger.Error("Failed to bulk create metadata: %v", err)
			return classified
		}

		// Invalidate related caches
		if r.cacheInvalidator != nil {
			event := InvalidationEvent{
				EntityType:    "metadata",
				EntityID:      fmt.Sprintf("%s:%s", entityType, entityID),
				ParentType:    entityType,
				ParentID:      entityID,
				OperationType: "create",
				Strategy:      InvalidateImmediately,
			}
			if invErr := r.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
				r.logger.Error("Failed to invalidate caches after bulk metadata creation: %v", invErr)
			}
		}

		r.logger.Debug("Successfully bulk created %d metadata entries", len(metadata))
		return nil
	})
}

// BulkUpdate upserts multiple metadata entries in a single transaction.
// Keys present in the request are created or updated; keys not present are left untouched.
// This implements PATCH (merge/upsert) semantics.
// SEM@0000000000000000000000000000000000000000: batch-upsert multiple metadata entries in one transaction using PATCH merge semantics (mutates shared state)
func (r *GormMetadataRepository) BulkUpdate(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	r.logger.Debug("Bulk upserting %d metadata entries", len(metadata))

	if len(metadata) == 0 {
		return nil
	}

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return err
	}

	now := time.Now().UTC()

	entries := make([]models.Metadata, len(metadata))
	for i, meta := range metadata {
		entries[i] = models.Metadata{
			ID:         models.DBVarchar(uuidgen.MustNewForEntity(uuidgen.EntityTypeMetadata).String()),
			EntityType: models.DBVarchar(entityType),
			EntityID:   models.DBVarchar(entityID),
			Key:        models.DBVarchar(meta.Key),
			Value:      models.DBVarchar(meta.Value),
			CreatedAt:  now,
			ModifiedAt: now,
		}
	}

	return authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Use Col()/ColumnName() so the Oracle GORM driver receives
		// uppercase column identifiers when emitting MERGE INTO.
		//
		// gorm-oracle folds the whole batch into one
		// `MERGE INTO ... USING (SELECT ... UNION ALL ...)` — verified by
		// DryRun against the real dialector at v1.1.3 (oracle-db-admin review
		// of #666; models.Metadata has no default-DB-value fields, so the
		// PL/SQL FORALL branch is never taken).
		//
		// INVARIANT (owned by the bulk handlers, metadata_handlers.go): the
		// batch must not contain two entries with the same key. A duplicated
		// key in one MERGE source raises ORA-30926 on Oracle — unmapped in
		// dberrors, so it would surface as a 500 — where the old per-row
		// upsert silently applied last-write-wins. All three bulk handlers
		// reject duplicate keys with a 400 before reaching this repository.
		dialect := tx.Name()
		result := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				Col(dialect, "entity_type"),
				Col(dialect, "entity_id"),
				Col(dialect, "key"),
			},
			DoUpdates: clause.AssignmentColumns([]string{
				ColumnName(dialect, "value"),
				ColumnName(dialect, "modified_at"),
			}),
		}).CreateInBatches(&entries, metadataBatchSize)

		if result.Error != nil {
			r.logger.Error("Failed to bulk upsert metadata: %v", result.Error)
			return dberrors.Classify(result.Error)
		}

		// Invalidate related caches
		if r.cacheInvalidator != nil {
			event := InvalidationEvent{
				EntityType:    "metadata",
				EntityID:      fmt.Sprintf("%s:%s", entityType, entityID),
				ParentType:    entityType,
				ParentID:      entityID,
				OperationType: "update",
				Strategy:      InvalidateImmediately,
			}
			if invErr := r.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
				r.logger.Error("Failed to invalidate caches after bulk metadata upsert: %v", invErr)
			}
		}

		r.logger.Debug("Successfully bulk upserted %d metadata entries", len(metadata))
		return nil
	})
}

// BulkReplace replaces all metadata for an entity atomically.
// All existing metadata is deleted, then the provided entries are inserted.
// An empty metadata slice clears all metadata for the entity.
// This implements PUT (full replace) semantics.
// SEM@0000000000000000000000000000000000000000: atomically replace all metadata for an entity with a batch-inserted new set using PUT semantics (mutates shared state)
func (r *GormMetadataRepository) BulkReplace(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	r.logger.Debug("Bulk replacing metadata for %s:%s with %d entries", entityType, entityID, len(metadata))

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return err
	}

	now := time.Now().UTC()

	entries := make([]models.Metadata, len(metadata))
	for i, meta := range metadata {
		entries[i] = models.Metadata{
			ID:         models.DBVarchar(uuidgen.MustNewForEntity(uuidgen.EntityTypeMetadata).String()),
			EntityType: models.DBVarchar(entityType),
			EntityID:   models.DBVarchar(entityID),
			Key:        models.DBVarchar(meta.Key),
			Value:      models.DBVarchar(meta.Value),
			CreatedAt:  now,
			ModifiedAt: now,
		}
	}

	// READ COMMITTED, not the #451 SERIALIZABLE default — the codebase's first
	// per-site opt-down (#449's escape hatch), approved on #783. Under
	// SERIALIZABLE a back-to-back same-entity replace exhausts the retry
	// wrapper with a FALSE ORA-08177: the metadata blocks (and the hot
	// right-edge leaf blocks of its timestamp-leading indexes) carry Oracle's
	// default INITRANS 1, so the second transaction recycles the first one's
	// ITL slot and the rows become unprovable — time-independent, so retrying
	// cannot help, and the client sees a 503 on two quick PUTs. Oracle never
	// raises ORA-08177 outside serializable/read-only transactions.
	// Correctness is preserved: BulkReplace reads nothing it acts upon (pure
	// DELETE-then-INSERT), so there is no lost-update hazard and
	// last-writer-wins still holds on both engines; a key colliding with the
	// new set surfaces as ORA-00001 -> ErrDuplicate, which the bulk handlers
	// route to the documented 409 via StoreErrorToRequestError. The narrow
	// caveat: a concurrent writer's non-colliding key inserted in the
	// DELETE->INSERT window — one round trip, tens of milliseconds at ADB
	// latency, down from ~100 sequential inserts pre-#666 — survives the
	// replace instead of conflicting. The durable root-cause fix (INITRANS +
	// index work) stays tracked on #783.
	return authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Delete all existing metadata for this entity
		if err := tx.Where("entity_type = ? AND entity_id = ?", entityType, entityID).
			Delete(&models.Metadata{}).Error; err != nil {
			r.logger.Error("Failed to delete existing metadata: %v", err)
			return dberrors.Classify(err)
		}

		// Insert new entries in batches
		if len(entries) > 0 {
			if err := tx.CreateInBatches(&entries, metadataBatchSize).Error; err != nil {
				r.logger.Error("Failed to insert metadata during replace: %v", err)
				return dberrors.Classify(err)
			}
		}

		// Invalidate related caches
		if r.cacheInvalidator != nil {
			event := InvalidationEvent{
				EntityType:    "metadata",
				EntityID:      fmt.Sprintf("%s:%s", entityType, entityID),
				ParentType:    entityType,
				ParentID:      entityID,
				OperationType: "replace",
				Strategy:      InvalidateImmediately,
			}
			if invErr := r.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
				r.logger.Error("Failed to invalidate caches after bulk metadata replace: %v", invErr)
			}
		}

		r.logger.Debug("Successfully bulk replaced metadata for %s:%s with %d entries", entityType, entityID, len(metadata))
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

// BulkDelete deletes multiple metadata entries by key in a single statement
// SEM@0000000000000000000000000000000000000000: delete multiple metadata entries by key in one statement and invalidate caches (mutates shared state)
func (r *GormMetadataRepository) BulkDelete(ctx context.Context, entityType, entityID string, keys []string) error {
	r.logger.Debug("Bulk deleting %d metadata keys", len(keys))

	if len(keys) == 0 {
		return nil
	}

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return err
	}

	return authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Single statement for all keys; the handler caps keys at 100, well
		// under Oracle's 1000-expression IN-list limit.
		if err := tx.Where("entity_type = ? AND entity_id = ? AND key IN ?", entityType, entityID, keys).
			Delete(&models.Metadata{}).Error; err != nil {
			r.logger.Error("Failed to bulk delete metadata keys: %v", err)
			return dberrors.Classify(err)
		}

		// Invalidate related caches
		if r.cacheInvalidator != nil {
			event := InvalidationEvent{
				EntityType:    "metadata",
				EntityID:      fmt.Sprintf("%s:%s", entityType, entityID),
				ParentType:    entityType,
				ParentID:      entityID,
				OperationType: "delete",
				Strategy:      InvalidateImmediately,
			}
			if invErr := r.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
				r.logger.Error("Failed to invalidate caches after bulk metadata deletion: %v", invErr)
			}
		}

		r.logger.Debug("Successfully bulk deleted %d metadata keys", len(keys))
		return nil
	})
}

// GetByKey retrieves all metadata entries with a specific key across all entities
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: fetch all metadata entries with a given key across all entities (reads DB)
func (r *GormMetadataRepository) GetByKey(ctx context.Context, key string) ([]Metadata, error) {
	r.logger.Debug("Getting metadata by key: %s", key)

	var modelList []models.Metadata
	result := r.db.WithContext(ctx).
		Where("key = ?", key).
		Order("entity_type, entity_id").
		Find(&modelList)

	if result.Error != nil {
		r.logger.Error("Failed to query metadata by key from database: %v", result.Error)
		return nil, dberrors.Classify(result.Error)
	}

	metadataList := make([]Metadata, 0, len(modelList))
	for _, model := range modelList {
		metadataList = append(metadataList, Metadata{
			Key:   string(model.Key),
			Value: string(model.Value),
		})
	}

	r.logger.Debug("Successfully retrieved %d metadata entries with key %s", len(metadataList), key)
	return metadataList, nil
}

// ListKeys retrieves all metadata keys for an entity
// SEM@4b5601a9cbb59c0d9d34db8808624707ebd7501e: list all distinct metadata keys for an entity (reads DB)
func (r *GormMetadataRepository) ListKeys(ctx context.Context, entityType, entityID string) ([]string, error) {
	r.logger.Debug("Listing metadata keys for %s:%s", entityType, entityID)

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return nil, err
	}

	// Parse entity ID
	_, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("invalid entity ID: %w", err)
	}

	var keys []string
	result := r.db.WithContext(ctx).Model(&models.Metadata{}).
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Order("key ASC").
		Distinct().
		Pluck("key", &keys)

	if result.Error != nil {
		r.logger.Error("Failed to query metadata keys from database: %v", result.Error)
		return nil, dberrors.Classify(result.Error)
	}

	r.logger.Debug("Successfully retrieved %d metadata keys", len(keys))
	return keys, nil
}

// InvalidateCache removes metadata-related cache entries
// SEM@4b5601a9cbb59c0d9d34db8808624707ebd7501e: remove cached metadata for an entity from the cache store (mutates shared state)
func (r *GormMetadataRepository) InvalidateCache(ctx context.Context, entityType, entityID string) error {
	if r.cache == nil {
		return nil
	}
	return r.cache.InvalidateMetadata(ctx, entityType, entityID)
}

// WarmCache preloads metadata for an entity into cache
// SEM@4b5601a9cbb59c0d9d34db8808624707ebd7501e: preload an entity's metadata into the cache by listing it (reads DB)
func (r *GormMetadataRepository) WarmCache(ctx context.Context, entityType, entityID string) error {
	r.logger.Debug("Warming cache for %s:%s metadata", entityType, entityID)

	if r.cache == nil {
		return nil
	}

	// Load metadata for the entity
	_, err := r.List(ctx, entityType, entityID)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	r.logger.Debug("Warmed cache for %s:%s metadata", entityType, entityID)
	return nil
}
