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
				// No alternate owner - delete threat model (cascades to children)
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

		// Delete user record (cascades to user_providers)
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

// DeleteGroupAndData deletes a group and handles threat model cleanup
func (r *GormDeletionRepository) DeleteGroupAndData(ctx context.Context, groupName string) (*GroupDeletionResult, error) {
	// Validate not deleting "everyone" group
	if groupName == "everyone" {
		return nil, fmt.Errorf("cannot delete protected group: everyone")
	}

	result := &GroupDeletionResult{
		GroupName: groupName,
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Get group internal_uuid (provider is always "*" for TMI-managed groups)
		var group models.Group
		if err := tx.Where("provider = ? AND group_name = ?", "*", groupName).First(&group).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("group not found: %s", groupName)
			}
			return fmt.Errorf("failed to query group: %w", err)
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
				// No user owners - delete threat model (cascades to children)
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
		deleteResult := tx.Delete(&group)
		if deleteResult.Error != nil {
			return fmt.Errorf("failed to delete group: %w", deleteResult.Error)
		}
		if deleteResult.RowsAffected == 0 {
			return fmt.Errorf("group not found: %s", groupName)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	r.logger.Info("Group deleted successfully: group_name=%s, deleted=%d, retained=%d",
		groupName, result.ThreatModelsDeleted, result.ThreatModelsRetained)

	return result, nil
}
