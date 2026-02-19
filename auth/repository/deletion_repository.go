package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/api/validation"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
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
			if errors.Is(err, gorm.ErrRecordNotFound) {
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

			if errors.Is(err, gorm.ErrRecordNotFound) {
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
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("group not found: %s", internalUUID)
			}
			return fmt.Errorf("failed to query group: %w", err)
		}

		// Store group_name for result
		result.GroupName = group.GroupName

		// Validate not deleting built-in groups (everyone, security-reviewers, administrators)
		if validation.IsBuiltInGroup(group.InternalUUID) {
			return fmt.Errorf("cannot delete built-in group %q", group.GroupName)
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
		// Pass the populated group struct so GORM BeforeDelete hook can check built-in status
		deleteResult := tx.Delete(&group)
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
	// 1. Delete user preferences
	if err := tx.Where("user_internal_uuid = ?", userInternalUUID).Delete(&models.UserPreference{}).Error; err != nil {
		return fmt.Errorf("failed to delete user preferences: %w", err)
	}

	// 2. Delete client credentials owned by user
	if err := tx.Where("owner_uuid = ?", userInternalUUID).Delete(&models.ClientCredential{}).Error; err != nil {
		return fmt.Errorf("failed to delete client credentials: %w", err)
	}

	// 3. Delete refresh tokens for user
	if err := tx.Where("user_internal_uuid = ?", userInternalUUID).Delete(&models.RefreshTokenRecord{}).Error; err != nil {
		return fmt.Errorf("failed to delete refresh tokens: %w", err)
	}

	// 4. Delete webhook subscriptions owned by user (and their deliveries)
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

	// 5. Delete webhook quota for user
	if err := tx.Where("owner_id = ?", userInternalUUID).Delete(&models.WebhookQuota{}).Error; err != nil {
		return fmt.Errorf("failed to delete webhook quota: %w", err)
	}

	// 6. Delete group memberships (includes Administrators group membership)
	if err := tx.Where("user_internal_uuid = ?", userInternalUUID).Delete(&models.GroupMember{}).Error; err != nil {
		return fmt.Errorf("failed to delete group memberships: %w", err)
	}

	// 8. Delete user API quota
	if err := tx.Where("user_internal_uuid = ?", userInternalUUID).Delete(&models.UserAPIQuota{}).Error; err != nil {
		return fmt.Errorf("failed to delete user API quota: %w", err)
	}

	// 9. Delete addon invocation quota
	if err := tx.Where("owner_internal_uuid = ?", userInternalUUID).Delete(&models.AddonInvocationQuota{}).Error; err != nil {
		return fmt.Errorf("failed to delete addon invocation quota: %w", err)
	}

	// 10. Delete session participants (for any collaboration sessions they joined)
	if err := tx.Where("user_internal_uuid = ?", userInternalUUID).Delete(&models.SessionParticipant{}).Error; err != nil {
		return fmt.Errorf("failed to delete session participants: %w", err)
	}

	// 11. SET NULL on triage notes created/modified by this user
	if err := tx.Model(&models.TriageNote{}).
		Where("created_by_internal_uuid = ?", userInternalUUID).
		Update("created_by_internal_uuid", nil).Error; err != nil {
		return fmt.Errorf("failed to nullify triage note created_by: %w", err)
	}
	if err := tx.Model(&models.TriageNote{}).
		Where("modified_by_internal_uuid = ?", userInternalUUID).
		Update("modified_by_internal_uuid", nil).Error; err != nil {
		return fmt.Errorf("failed to nullify triage note modified_by: %w", err)
	}

	// 12. SET NULL on threat model security_reviewer where deleted user was the reviewer
	if err := tx.Model(&models.ThreatModel{}).
		Where("security_reviewer_internal_uuid = ?", userInternalUUID).
		Update("security_reviewer_internal_uuid", nil).Error; err != nil {
		return fmt.Errorf("failed to nullify threat model security_reviewer: %w", err)
	}

	// 13. Handle survey response access:
	//     - DELETE records where user is the grantee (they can no longer use the access)
	//     - SET NULL on granted_by where user granted access to others
	if err := tx.Where("user_internal_uuid = ? AND subject_type = ?",
		userInternalUUID, "user").Delete(&models.SurveyResponseAccess{}).Error; err != nil {
		return fmt.Errorf("failed to delete survey response access for user: %w", err)
	}
	if err := tx.Model(&models.SurveyResponseAccess{}).
		Where("granted_by_internal_uuid = ?", userInternalUUID).
		Update("granted_by_internal_uuid", nil).Error; err != nil {
		return fmt.Errorf("failed to nullify survey response access granted_by: %w", err)
	}

	// 14. Handle survey responses:
	//     - SET NULL on reviewed_by where deleted user was reviewer
	//     - Ensure Security Reviewers group has owner access (even for confidential responses)
	//     - SET NULL on owner for responses owned by deleted user
	if err := tx.Model(&models.SurveyResponse{}).
		Where("reviewed_by_internal_uuid = ?", userInternalUUID).
		Update("reviewed_by_internal_uuid", nil).Error; err != nil {
		return fmt.Errorf("failed to nullify survey response reviewed_by: %w", err)
	}

	// Find responses owned by the deleted user and ensure Security Reviewers access
	var ownedResponses []models.SurveyResponse
	if err := tx.Where("owner_internal_uuid = ?", userInternalUUID).
		Find(&ownedResponses).Error; err != nil {
		return fmt.Errorf("failed to find owned survey responses: %w", err)
	}

	if len(ownedResponses) > 0 {
		groupUUID, err := ensureSecurityReviewersGroupForDeletion(tx)
		if err != nil {
			return fmt.Errorf("failed to ensure security reviewers group: %w", err)
		}

		for _, resp := range ownedResponses {
			// Check if Security Reviewers already has access to this response
			var count int64
			if err := tx.Model(&models.SurveyResponseAccess{}).Where(
				"survey_response_id = ? AND group_internal_uuid = ? AND subject_type = ?",
				resp.ID, groupUUID, "group",
			).Count(&count).Error; err != nil {
				return fmt.Errorf("failed to check security reviewers access: %w", err)
			}

			if count == 0 {
				access := models.SurveyResponseAccess{
					SurveyResponseID:  resp.ID,
					GroupInternalUUID: &groupUUID,
					SubjectType:       "group",
					Role:              "owner",
				}
				if err := tx.Create(&access).Error; err != nil {
					return fmt.Errorf("failed to add security reviewers access to survey response %s: %w", resp.ID, err)
				}
				r.logger.Debug("Added Security Reviewers access to survey response %s (owner being deleted)", resp.ID)
			}
		}
	}

	// SET NULL on survey response owner
	if err := tx.Model(&models.SurveyResponse{}).
		Where("owner_internal_uuid = ?", userInternalUUID).
		Update("owner_internal_uuid", nil).Error; err != nil {
		return fmt.Errorf("failed to nullify survey response owner: %w", err)
	}

	return nil
}

const (
	securityReviewersGroupName = "security-reviewers"
	securityReviewersGroupUUID = "00000000-0000-0000-0000-000000000001"
)

// ensureSecurityReviewersGroupForDeletion ensures the Security Reviewers group exists
// and returns its internal UUID. This is a standalone version of the function in
// survey_response_store_gorm.go, duplicated here to avoid a cross-package dependency
// from auth/repository to api.
func ensureSecurityReviewersGroupForDeletion(tx *gorm.DB) (string, error) {
	var group models.Group
	result := tx.Where("group_name = ? AND provider = ?", securityReviewersGroupName, "*").First(&group)

	if result.Error == nil {
		return group.InternalUUID, nil
	}

	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return "", result.Error
	}

	// Create the group
	groupName := "Security Reviewers"
	group = models.Group{
		InternalUUID: securityReviewersGroupUUID,
		Provider:     "*",
		GroupName:    securityReviewersGroupName,
		Name:         &groupName,
		UsageCount:   1,
	}

	if err := tx.Create(&group).Error; err != nil {
		// Handle race condition - another transaction may have created it
		var existingGroup models.Group
		if tx.Where("group_name = ? AND provider = ?", securityReviewersGroupName, "*").First(&existingGroup).Error == nil {
			return existingGroup.InternalUUID, nil
		}
		return "", err
	}

	return group.InternalUUID, nil
}

// TransferOwnership transfers all owned threat models and survey responses
// from sourceUserUUID to targetUserUUID within a single transaction.
// The source user is downgraded to "writer" role on all transferred items.
func (r *GormDeletionRepository) TransferOwnership(ctx context.Context, sourceUserUUID, targetUserUUID string) (*TransferResult, error) {
	result := &TransferResult{}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Validate target user exists
		var targetUser models.User
		if err := tx.Where("internal_uuid = ?", targetUserUUID).First(&targetUser).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return fmt.Errorf("failed to query target user: %w", err)
		}

		// Validate source user exists
		var sourceUser models.User
		if err := tx.Where("internal_uuid = ?", sourceUserUUID).First(&sourceUser).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return fmt.Errorf("failed to query source user: %w", err)
		}

		// Transfer threat models
		var threatModels []models.ThreatModel
		if err := tx.Where("owner_internal_uuid = ?", sourceUserUUID).Find(&threatModels).Error; err != nil {
			return fmt.Errorf("failed to query owned threat models: %w", err)
		}

		for _, tm := range threatModels {
			// Update threat model ownership
			if err := tx.Model(&tm).Updates(map[string]interface{}{
				"owner_internal_uuid": targetUserUUID,
				"modified_at":         time.Now().UTC(),
			}).Error; err != nil {
				return fmt.Errorf("failed to transfer ownership of threat model %s: %w", tm.ID, err)
			}

			// Upsert target user as owner in threat_model_access
			if err := r.upsertThreatModelAccess(tx, tm.ID, targetUserUUID, "owner"); err != nil {
				return fmt.Errorf("failed to grant owner access on threat model %s: %w", tm.ID, err)
			}

			// Downgrade source user to writer in threat_model_access
			if err := r.upsertThreatModelAccess(tx, tm.ID, sourceUserUUID, "writer"); err != nil {
				return fmt.Errorf("failed to downgrade access on threat model %s: %w", tm.ID, err)
			}

			result.ThreatModelIDs = append(result.ThreatModelIDs, tm.ID)
			r.logger.Debug("Transferred ownership of threat model %s from %s to %s", tm.ID, sourceUserUUID, targetUserUUID)
		}

		// Transfer survey responses
		var surveyResponses []models.SurveyResponse
		if err := tx.Where("owner_internal_uuid = ?", sourceUserUUID).Find(&surveyResponses).Error; err != nil {
			return fmt.Errorf("failed to query owned survey responses: %w", err)
		}

		for _, sr := range surveyResponses {
			// Update survey response ownership
			if err := tx.Model(&sr).Updates(map[string]interface{}{
				"owner_internal_uuid": targetUserUUID,
				"modified_at":         time.Now().UTC(),
			}).Error; err != nil {
				return fmt.Errorf("failed to transfer ownership of survey response %s: %w", sr.ID, err)
			}

			// Upsert target user as owner in survey_response_access
			if err := r.upsertSurveyResponseAccess(tx, sr.ID, targetUserUUID, "owner"); err != nil {
				return fmt.Errorf("failed to grant owner access on survey response %s: %w", sr.ID, err)
			}

			// Downgrade source user to writer in survey_response_access
			if err := r.upsertSurveyResponseAccess(tx, sr.ID, sourceUserUUID, "writer"); err != nil {
				return fmt.Errorf("failed to downgrade access on survey response %s: %w", sr.ID, err)
			}

			result.SurveyResponseIDs = append(result.SurveyResponseIDs, sr.ID)
			r.logger.Debug("Transferred ownership of survey response %s from %s to %s", sr.ID, sourceUserUUID, targetUserUUID)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	r.logger.Info("Ownership transferred: source=%s, target=%s, threat_models=%d, survey_responses=%d",
		sourceUserUUID, targetUserUUID, len(result.ThreatModelIDs), len(result.SurveyResponseIDs))

	return result, nil
}

// upsertThreatModelAccess ensures a user has the specified role on a threat model.
// If the user already has an access record, the role is updated. Otherwise, a new record is created.
func (r *GormDeletionRepository) upsertThreatModelAccess(tx *gorm.DB, threatModelID, userUUID, role string) error {
	var existing models.ThreatModelAccess
	err := tx.Where(
		"threat_model_id = ? AND user_internal_uuid = ? AND subject_type = ?",
		threatModelID, userUUID, "user",
	).First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create new access record
		access := models.ThreatModelAccess{
			ID:               uuid.New().String(),
			ThreatModelID:    threatModelID,
			UserInternalUUID: &userUUID,
			SubjectType:      "user",
			Role:             role,
		}
		return tx.Create(&access).Error
	} else if err != nil {
		return err
	}

	// Update existing record's role
	if existing.Role != role {
		return tx.Model(&existing).Update("role", role).Error
	}
	return nil
}

// upsertSurveyResponseAccess ensures a user has the specified role on a survey response.
// If the user already has an access record, the role is updated. Otherwise, a new record is created.
func (r *GormDeletionRepository) upsertSurveyResponseAccess(tx *gorm.DB, surveyResponseID, userUUID, role string) error {
	var existing models.SurveyResponseAccess
	err := tx.Where(
		"survey_response_id = ? AND user_internal_uuid = ? AND subject_type = ?",
		surveyResponseID, userUUID, "user",
	).First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create new access record
		access := models.SurveyResponseAccess{
			ID:               uuid.New().String(),
			SurveyResponseID: surveyResponseID,
			UserInternalUUID: &userUUID,
			SubjectType:      "user",
			Role:             role,
		}
		return tx.Create(&access).Error
	} else if err != nil {
		return err
	}

	// Update existing record's role
	if existing.Role != role {
		return tx.Model(&existing).Update("role", role).Error
	}
	return nil
}
