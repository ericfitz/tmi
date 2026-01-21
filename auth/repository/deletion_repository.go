package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// GormDeletionRepository implements DeletionRepository using GORM
type GormDeletionRepository struct {
	db     *gorm.DB
	logger *slogging.Logger
}

// NewGormDeletionRepository creates a new GORM-backed deletion repository
func NewGormDeletionRepository(db *gorm.DB) *GormDeletionRepository {
	return &GormDeletionRepository{
		db:     db,
		logger: slogging.Get(),
	}
}

// DeleteUserAndData deletes a user and handles ownership transfer for threat models
func (r *GormDeletionRepository) DeleteUserAndData(ctx context.Context, userEmail string) (*DeletionResult, error) {
	result := &DeletionResult{
		UserEmail: userEmail,
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Get the user's internal_uuid
		var user models.User
		if err := tx.Where("email = ?", userEmail).First(&user).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("user not found: %s", userEmail)
			}
			return fmt.Errorf("failed to query user: %w", err)
		}

		// Get all threat models owned by user
		var threatModels []models.ThreatModel
		if err := tx.Where("owner_internal_uuid = ?", user.InternalUUID).Find(&threatModels).Error; err != nil {
			return fmt.Errorf("failed to query owned threat models: %w", err)
		}

		// Process each threat model
		for _, tm := range threatModels {
			// Find alternate owner (user with owner role in threat_model_access)
			var access models.ThreatModelAccess
			err := tx.Where(
				"threat_model_id = ? AND role = ? AND subject_type = ? AND user_internal_uuid != ?",
				tm.ID, "owner", "user", user.InternalUUID,
			).First(&access).Error

			if err == gorm.ErrRecordNotFound {
				// No alternate owner - delete threat model and all children
				// Must delete children first due to foreign key constraints
				if err := r.deleteThreatModelChildren(tx, tm.ID); err != nil {
					return fmt.Errorf("failed to delete threat model %s children: %w", tm.ID, err)
				}
				if err := tx.Delete(&tm).Error; err != nil {
					return fmt.Errorf("failed to delete threat model %s: %w", tm.ID, err)
				}
				result.ThreatModelsDeleted++
				r.logger.Debug("Deleted threat model %s (no alternate owner)", tm.ID)
			} else if err != nil {
				return fmt.Errorf("failed to find alternate owner for threat model %s: %w", tm.ID, err)
			} else {
				// Transfer ownership to alternate owner
				if err := tx.Model(&tm).Updates(map[string]interface{}{
					"owner_internal_uuid": access.UserInternalUUID,
					"modified_at":         time.Now().UTC(),
				}).Error; err != nil {
					return fmt.Errorf("failed to transfer ownership of threat model %s: %w", tm.ID, err)
				}

				// Remove deleting user's permissions
				if err := tx.Where(
					"threat_model_id = ? AND user_internal_uuid = ? AND subject_type = ?",
					tm.ID, user.InternalUUID, "user",
				).Delete(&models.ThreatModelAccess{}).Error; err != nil {
					return fmt.Errorf("failed to remove user permissions from threat model %s: %w", tm.ID, err)
				}

				result.ThreatModelsTransferred++
				r.logger.Debug("Transferred ownership of threat model %s to %v", tm.ID, access.UserInternalUUID)
			}
		}

		// Clean up any remaining permissions (reader/writer on other threat models)
		if err := tx.Where(
			"user_internal_uuid = ? AND subject_type = ?",
			user.InternalUUID, "user",
		).Delete(&models.ThreatModelAccess{}).Error; err != nil {
			return fmt.Errorf("failed to clean up remaining permissions: %w", err)
		}

		// Delete all user-related entities before deleting the user
		if err := r.deleteUserRelatedEntities(tx, user.InternalUUID); err != nil {
			return fmt.Errorf("failed to delete user-related entities: %w", err)
		}

		// Delete user record
		deleteResult := tx.Where("email = ?", userEmail).Delete(&models.User{})
		if deleteResult.Error != nil {
			return fmt.Errorf("failed to delete user: %w", deleteResult.Error)
		}
		if deleteResult.RowsAffected == 0 {
			return fmt.Errorf("user not found: %s", userEmail)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	r.logger.Info("User deleted successfully: email=%s, transferred=%d, deleted=%d",
		userEmail, result.ThreatModelsTransferred, result.ThreatModelsDeleted)

	return result, nil
}

// DeleteGroupAndData deletes a group by internal UUID and handles threat model cleanup
// Uses internal_uuid for precise identification to avoid issues with duplicate group_names
func (r *GormDeletionRepository) DeleteGroupAndData(ctx context.Context, internalUUID string) (*GroupDeletionResult, error) {
	result := &GroupDeletionResult{}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Get group by internal_uuid
		var group models.Group
		if err := tx.Where("internal_uuid = ?", internalUUID).First(&group).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("group not found: %s", internalUUID)
			}
			return fmt.Errorf("failed to query group: %w", err)
		}

		// Store group_name for result
		result.GroupName = group.GroupName

		// Validate not deleting "everyone" group
		if group.GroupName == "everyone" {
			return fmt.Errorf("cannot delete protected group: everyone")
		}

		// Get all threat models owned by this group
		var threatModels []models.ThreatModel
		if err := tx.Where("owner_internal_uuid = ?", group.InternalUUID).Find(&threatModels).Error; err != nil {
			return fmt.Errorf("failed to query owned threat models: %w", err)
		}

		// Process each threat model
		for _, tm := range threatModels {
			// Check if there are other user owners (not group owners)
			var count int64
			if err := tx.Model(&models.ThreatModelAccess{}).Where(
				"threat_model_id = ? AND role = ? AND subject_type = ?",
				tm.ID, "owner", "user",
			).Count(&count).Error; err != nil {
				return fmt.Errorf("failed to check for alternate owners for threat model %s: %w", tm.ID, err)
			}

			if count == 0 {
				// No user owners - delete threat model and all children
				// Must delete children first due to foreign key constraints
				if err := r.deleteThreatModelChildren(tx, tm.ID); err != nil {
					return fmt.Errorf("failed to delete threat model %s children: %w", tm.ID, err)
				}
				if err := tx.Delete(&tm).Error; err != nil {
					return fmt.Errorf("failed to delete threat model %s: %w", tm.ID, err)
				}
				result.ThreatModelsDeleted++
				r.logger.Debug("Deleted threat model %s (no user owners)", tm.ID)
			} else {
				// Has user owners - just remove group from access, keep threat model
				result.ThreatModelsRetained++
				r.logger.Debug("Retaining threat model %s (has user owners)", tm.ID)
			}
		}

		// Clean up any remaining permissions (reader/writer/owner on any threat models)
		if err := tx.Where(
			"group_internal_uuid = ? AND subject_type = ?",
			group.InternalUUID, "group",
		).Delete(&models.ThreatModelAccess{}).Error; err != nil {
			return fmt.Errorf("failed to clean up group permissions: %w", err)
		}

		// Delete group record (cascades to administrators via FK)
		// Use the exact internal_uuid we looked up to ensure correct deletion
		deleteResult := tx.Where("internal_uuid = ?", internalUUID).Delete(&models.Group{})
		if deleteResult.Error != nil {
			return fmt.Errorf("failed to delete group: %w", deleteResult.Error)
		}
		if deleteResult.RowsAffected == 0 {
			return fmt.Errorf("group not found: %s", internalUUID)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	r.logger.Info("Group deleted successfully: internal_uuid=%s, group_name=%s, deleted=%d, retained=%d",
		internalUUID, result.GroupName, result.ThreatModelsDeleted, result.ThreatModelsRetained)

	return result, nil
}

// deleteThreatModelChildren deletes all child entities of a threat model
// This must be called before deleting the threat model itself due to FK constraints
func (r *GormDeletionRepository) deleteThreatModelChildren(tx *gorm.DB, threatModelID string) error {
	// Delete in order to respect foreign key constraints
	// Each entity type may have metadata, so delete metadata before the entity

	// 1. Delete collaboration sessions and participants
	if err := r.deleteCollaborationSessions(tx, threatModelID); err != nil {
		return err
	}

	// 2. Delete threats and their metadata
	if err := r.deleteEntitiesWithMetadata(tx, threatModelID, "threat", &[]models.Threat{}); err != nil {
		return err
	}

	// 3. Delete diagrams and their metadata
	if err := r.deleteEntitiesWithMetadata(tx, threatModelID, "diagram", &[]models.Diagram{}); err != nil {
		return err
	}

	// 4. Delete assets and their metadata
	if err := r.deleteEntitiesWithMetadata(tx, threatModelID, "asset", &[]models.Asset{}); err != nil {
		return err
	}

	// 5. Delete documents and their metadata
	if err := r.deleteEntitiesWithMetadata(tx, threatModelID, "document", &[]models.Document{}); err != nil {
		return err
	}

	// 6. Delete notes and their metadata
	if err := r.deleteEntitiesWithMetadata(tx, threatModelID, "note", &[]models.Note{}); err != nil {
		return err
	}

	// 7. Delete repositories and their metadata
	if err := r.deleteEntitiesWithMetadata(tx, threatModelID, "repository", &[]models.Repository{}); err != nil {
		return err
	}

	// 8. Delete threat model access records
	if err := tx.Where("threat_model_id = ?", threatModelID).Delete(&models.ThreatModelAccess{}).Error; err != nil {
		return fmt.Errorf("failed to delete threat model access records: %w", err)
	}

	// 9. Delete webhook subscriptions and deliveries scoped to this threat model
	if err := r.deleteWebhookSubscriptions(tx, threatModelID); err != nil {
		return err
	}

	// 10. Delete addons scoped to this threat model
	if err := tx.Where("threat_model_id = ?", threatModelID).Delete(&models.Addon{}).Error; err != nil {
		return fmt.Errorf("failed to delete addons: %w", err)
	}

	// 11. Delete threat model metadata (last, after all children are gone)
	if err := tx.Where("entity_type = ? AND entity_id = ?", "threat_model", threatModelID).Delete(&models.Metadata{}).Error; err != nil {
		return fmt.Errorf("failed to delete threat model metadata: %w", err)
	}

	return nil
}

// deleteEntitiesWithMetadata finds entities by threat_model_id, deletes their metadata, then deletes the entities
func (r *GormDeletionRepository) deleteEntitiesWithMetadata(tx *gorm.DB, threatModelID, entityType string, entities interface{}) error {
	// Find all entities for this threat model
	if err := tx.Where("threat_model_id = ?", threatModelID).Find(entities).Error; err != nil {
		return fmt.Errorf("failed to find %ss: %w", entityType, err)
	}

	// Delete metadata for each entity using type switch
	switch e := entities.(type) {
	case *[]models.Threat:
		for _, entity := range *e {
			if err := tx.Where("entity_type = ? AND entity_id = ?", entityType, entity.ID).Delete(&models.Metadata{}).Error; err != nil {
				return fmt.Errorf("failed to delete %s metadata: %w", entityType, err)
			}
		}
		if err := tx.Where("threat_model_id = ?", threatModelID).Delete(&models.Threat{}).Error; err != nil {
			return fmt.Errorf("failed to delete %ss: %w", entityType, err)
		}
	case *[]models.Diagram:
		for _, entity := range *e {
			if err := tx.Where("entity_type = ? AND entity_id = ?", entityType, entity.ID).Delete(&models.Metadata{}).Error; err != nil {
				return fmt.Errorf("failed to delete %s metadata: %w", entityType, err)
			}
		}
		if err := tx.Where("threat_model_id = ?", threatModelID).Delete(&models.Diagram{}).Error; err != nil {
			return fmt.Errorf("failed to delete %ss: %w", entityType, err)
		}
	case *[]models.Asset:
		for _, entity := range *e {
			if err := tx.Where("entity_type = ? AND entity_id = ?", entityType, entity.ID).Delete(&models.Metadata{}).Error; err != nil {
				return fmt.Errorf("failed to delete %s metadata: %w", entityType, err)
			}
		}
		if err := tx.Where("threat_model_id = ?", threatModelID).Delete(&models.Asset{}).Error; err != nil {
			return fmt.Errorf("failed to delete %ss: %w", entityType, err)
		}
	case *[]models.Document:
		for _, entity := range *e {
			if err := tx.Where("entity_type = ? AND entity_id = ?", entityType, entity.ID).Delete(&models.Metadata{}).Error; err != nil {
				return fmt.Errorf("failed to delete %s metadata: %w", entityType, err)
			}
		}
		if err := tx.Where("threat_model_id = ?", threatModelID).Delete(&models.Document{}).Error; err != nil {
			return fmt.Errorf("failed to delete %ss: %w", entityType, err)
		}
	case *[]models.Note:
		for _, entity := range *e {
			if err := tx.Where("entity_type = ? AND entity_id = ?", entityType, entity.ID).Delete(&models.Metadata{}).Error; err != nil {
				return fmt.Errorf("failed to delete %s metadata: %w", entityType, err)
			}
		}
		if err := tx.Where("threat_model_id = ?", threatModelID).Delete(&models.Note{}).Error; err != nil {
			return fmt.Errorf("failed to delete %ss: %w", entityType, err)
		}
	case *[]models.Repository:
		for _, entity := range *e {
			if err := tx.Where("entity_type = ? AND entity_id = ?", entityType, entity.ID).Delete(&models.Metadata{}).Error; err != nil {
				return fmt.Errorf("failed to delete %s metadata: %w", entityType, err)
			}
		}
		if err := tx.Where("threat_model_id = ?", threatModelID).Delete(&models.Repository{}).Error; err != nil {
			return fmt.Errorf("failed to delete %ss: %w", entityType, err)
		}
	}

	return nil
}

// deleteCollaborationSessions deletes collaboration sessions and their participants
func (r *GormDeletionRepository) deleteCollaborationSessions(tx *gorm.DB, threatModelID string) error {
	var sessions []models.CollaborationSession
	if err := tx.Where("threat_model_id = ?", threatModelID).Find(&sessions).Error; err != nil {
		return fmt.Errorf("failed to find collaboration sessions: %w", err)
	}
	for _, session := range sessions {
		if err := tx.Where("session_id = ?", session.ID).Delete(&models.SessionParticipant{}).Error; err != nil {
			return fmt.Errorf("failed to delete session participants: %w", err)
		}
	}
	if err := tx.Where("threat_model_id = ?", threatModelID).Delete(&models.CollaborationSession{}).Error; err != nil {
		return fmt.Errorf("failed to delete collaboration sessions: %w", err)
	}
	return nil
}

// deleteWebhookSubscriptions deletes webhook subscriptions and their deliveries
func (r *GormDeletionRepository) deleteWebhookSubscriptions(tx *gorm.DB, threatModelID string) error {
	var webhooks []models.WebhookSubscription
	if err := tx.Where("threat_model_id = ?", threatModelID).Find(&webhooks).Error; err != nil {
		return fmt.Errorf("failed to find webhook subscriptions: %w", err)
	}
	for _, webhook := range webhooks {
		if err := tx.Where("subscription_id = ?", webhook.ID).Delete(&models.WebhookDelivery{}).Error; err != nil {
			return fmt.Errorf("failed to delete webhook deliveries: %w", err)
		}
	}
	if err := tx.Where("threat_model_id = ?", threatModelID).Delete(&models.WebhookSubscription{}).Error; err != nil {
		return fmt.Errorf("failed to delete webhook subscriptions: %w", err)
	}
	return nil
}

// deleteUserRelatedEntities deletes all entities that reference the user
// This must be called before deleting the user record due to FK constraints
func (r *GormDeletionRepository) deleteUserRelatedEntities(tx *gorm.DB, userInternalUUID string) error {
	// 1. Delete client credentials owned by user
	if err := tx.Where("owner_uuid = ?", userInternalUUID).Delete(&models.ClientCredential{}).Error; err != nil {
		return fmt.Errorf("failed to delete client credentials: %w", err)
	}

	// 2. Delete refresh tokens for user
	if err := tx.Where("user_internal_uuid = ?", userInternalUUID).Delete(&models.RefreshTokenRecord{}).Error; err != nil {
		return fmt.Errorf("failed to delete refresh tokens: %w", err)
	}

	// 3. Delete webhook subscriptions owned by user (and their deliveries)
	// Note: Threat-model-scoped webhooks were already deleted with the threat model
	var webhooks []models.WebhookSubscription
	if err := tx.Where("owner_internal_uuid = ?", userInternalUUID).Find(&webhooks).Error; err != nil {
		return fmt.Errorf("failed to find user webhook subscriptions: %w", err)
	}
	for _, webhook := range webhooks {
		// Delete addons associated with this webhook first
		if err := tx.Where("webhook_id = ?", webhook.ID).Delete(&models.Addon{}).Error; err != nil {
			return fmt.Errorf("failed to delete webhook addons: %w", err)
		}
		// Delete deliveries
		if err := tx.Where("subscription_id = ?", webhook.ID).Delete(&models.WebhookDelivery{}).Error; err != nil {
			return fmt.Errorf("failed to delete user webhook deliveries: %w", err)
		}
	}
	if err := tx.Where("owner_internal_uuid = ?", userInternalUUID).Delete(&models.WebhookSubscription{}).Error; err != nil {
		return fmt.Errorf("failed to delete user webhook subscriptions: %w", err)
	}

	// 4. Delete webhook quota for user
	if err := tx.Where("owner_id = ?", userInternalUUID).Delete(&models.WebhookQuota{}).Error; err != nil {
		return fmt.Errorf("failed to delete webhook quota: %w", err)
	}

	// 5. Delete administrator record for user (if they were an admin)
	if err := tx.Where("user_internal_uuid = ? AND subject_type = ?", userInternalUUID, "user").Delete(&models.Administrator{}).Error; err != nil {
		return fmt.Errorf("failed to delete administrator record: %w", err)
	}

	// 6. Delete group memberships
	if err := tx.Where("user_internal_uuid = ?", userInternalUUID).Delete(&models.GroupMember{}).Error; err != nil {
		return fmt.Errorf("failed to delete group memberships: %w", err)
	}

	// 7. Delete user API quota
	if err := tx.Where("user_internal_uuid = ?", userInternalUUID).Delete(&models.UserAPIQuota{}).Error; err != nil {
		return fmt.Errorf("failed to delete user API quota: %w", err)
	}

	// 8. Delete addon invocation quota
	if err := tx.Where("owner_internal_uuid = ?", userInternalUUID).Delete(&models.AddonInvocationQuota{}).Error; err != nil {
		return fmt.Errorf("failed to delete addon invocation quota: %w", err)
	}

	// 9. Delete session participants (for any collaboration sessions they joined)
	if err := tx.Where("user_internal_uuid = ?", userInternalUUID).Delete(&models.SessionParticipant{}).Error; err != nil {
		return fmt.Errorf("failed to delete session participants: %w", err)
	}

	return nil
}
