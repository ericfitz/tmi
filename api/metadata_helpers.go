package api

import (
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// loadEntityMetadata loads metadata for any entity type from the database.
// The db parameter can be s.db.WithContext(ctx) or a transaction.
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: fetch all metadata entries for an entity type and ID, ordered by key (reads DB)
func loadEntityMetadata(db *gorm.DB, entityType, entityID string) ([]Metadata, error) {
	var metadataEntries []models.Metadata
	result := db.
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Order("key ASC").
		Find(&metadataEntries)

	if result.Error != nil {
		return nil, result.Error
	}

	metadata := make([]Metadata, 0, len(metadataEntries))
	for _, entry := range metadataEntries {
		metadata = append(metadata, Metadata{
			Key:   string(entry.Key),
			Value: string(entry.Value),
		})
	}

	return metadata, nil
}

// saveEntityMetadata saves metadata using upsert (OnConflict) without deleting existing entries.
// The db parameter can be s.db.WithContext(ctx) or a transaction.
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: upsert metadata entries for an entity, updating value and modified_at on conflict (reads DB)
func saveEntityMetadata(db *gorm.DB, entityType, entityID string, metadata []Metadata) error {
	if len(metadata) == 0 {
		return nil
	}

	for _, meta := range metadata {
		entry := models.Metadata{
			ID:         models.DBVarchar(uuid.New().String()),
			EntityType: models.DBVarchar(entityType),
			EntityID:   models.DBVarchar(entityID),
			Key:        models.DBVarchar(meta.Key),
			Value:      models.DBVarchar(meta.Value),
		}

		// Use Col()/ColumnName() so the Oracle GORM driver receives uppercase
		// column identifiers when emitting MERGE INTO. Without this, the
		// conflict-target columns are emitted lowercase and fail to match the
		// Oracle unique index. Matches the pattern already used in
		// group_repository.go for the same reason.
		dialect := db.Name()
		result := db.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				Col(dialect, "entity_type"),
				Col(dialect, "entity_id"),
				Col(dialect, "key"),
			},
			DoUpdates: clause.AssignmentColumns([]string{
				ColumnName(dialect, "value"),
				ColumnName(dialect, "modified_at"),
			}),
		}).Create(&entry)

		if result.Error != nil {
			return fmt.Errorf("failed to save %s metadata: %w", entityType, result.Error)
		}
	}

	return nil
}

// deleteAndSaveEntityMetadata deletes existing metadata then inserts new entries.
// Used by stores that need to replace all metadata atomically.
// The db parameter can be s.db.WithContext(ctx) or a transaction.
// SEM@22b222cb8680df2700e22f0e8538874669789920: atomically replace all metadata for an entity by deleting then reinserting (reads DB)
func deleteAndSaveEntityMetadata(db *gorm.DB, entityType, entityID string, metadata []Metadata) error {
	// Delete existing metadata
	if err := db.
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Delete(&models.Metadata{}).Error; err != nil {
		return fmt.Errorf("failed to delete existing %s metadata: %w", entityType, err)
	}

	// Insert new metadata
	return saveEntityMetadata(db, entityType, entityID, metadata)
}
