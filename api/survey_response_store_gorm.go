package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// splitCommaValues splits a comma-separated string into trimmed, non-empty values.
func splitCommaValues(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// SurveyResponseStore defines the interface for survey response operations
type SurveyResponseStore interface {
	// CRUD operations
	Create(ctx context.Context, response *SurveyResponse, userInternalUUID string) error
	Get(ctx context.Context, id uuid.UUID) (*SurveyResponse, error)
	Update(ctx context.Context, response *SurveyResponse) error
	Delete(ctx context.Context, id uuid.UUID) error

	// List operations with pagination and filtering
	List(ctx context.Context, limit, offset int, filters *SurveyResponseFilters) ([]SurveyResponseListItem, int, error)

	// List responses for a specific owner
	ListByOwner(ctx context.Context, ownerInternalUUID string, limit, offset int, status *string) ([]SurveyResponseListItem, int, error)

	// State transition
	UpdateStatus(ctx context.Context, id uuid.UUID, newStatus string, reviewerInternalUUID *string, revisionNotes *string) error

	// Authorization operations
	GetAuthorization(ctx context.Context, id uuid.UUID) ([]Authorization, error)
	UpdateAuthorization(ctx context.Context, id uuid.UUID, authorization []Authorization) error

	// Check access
	HasAccess(ctx context.Context, id uuid.UUID, userInternalUUID string, requiredRole AuthorizationRole) (bool, error)

	// SetCreatedThreatModel atomically sets created_threat_model_id and transitions
	// status to review_created. Returns an error if the response is not in
	// ready_for_review status (optimistic concurrency guard).
	SetCreatedThreatModel(ctx context.Context, id uuid.UUID, threatModelID string) error
}

// SurveyResponseFilters defines filter options for listing responses
type SurveyResponseFilters struct {
	Status   *string
	SurveyID *uuid.UUID
	OwnerID  *string
}

// GormSurveyResponseStore implements SurveyResponseStore using GORM
type GormSurveyResponseStore struct {
	db *gorm.DB
}

// NewGormSurveyResponseStore creates a new GORM-backed survey response store
func NewGormSurveyResponseStore(db *gorm.DB) *GormSurveyResponseStore {
	return &GormSurveyResponseStore{db: db}
}

// Create creates a new survey response
func (s *GormSurveyResponseStore) Create(ctx context.Context, response *SurveyResponse, userInternalUUID string) error {
	logger := slogging.Get()

	// Generate ID if not provided
	if response.Id == nil {
		id := uuid.New()
		response.Id = &id
	}

	// Set default status if not provided
	if response.Status == nil {
		status := ResponseStatusDraft
		response.Status = &status
	}

	// Validate survey exists and get version
	var template models.SurveyTemplate
	result := s.db.WithContext(ctx).First(&template, "id = ?", response.SurveyId.String())
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrSurveyNotFound
		}
		return dberrors.Classify(result.Error)
	}

	// Capture survey version at creation
	response.SurveyVersion = &template.Version

	// Snapshot the template's survey_json for rendering historical responses
	if len(template.SurveyJSON) > 0 {
		var surveyJSON map[string]any
		if err := json.Unmarshal(template.SurveyJSON, &surveyJSON); err == nil {
			response.SurveyJson = &surveyJSON
		}
	}

	model, err := s.apiToModel(response, userInternalUUID)
	if err != nil {
		logger.Error("Failed to convert response to model: error=%v", err)
		return fmt.Errorf("failed to convert response: %w", err)
	}

	// Start transaction (with retry on transient errors)
	err = authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Create the response
		if err := tx.Create(&model).Error; err != nil {
			logger.Error("Failed to create survey response: error=%v", err)
			return dberrors.Classify(err)
		}

		// Add owner access entry
		ownerAccess := models.SurveyResponseAccess{
			SurveyResponseID: model.ID,
			UserInternalUUID: &userInternalUUID,
			SubjectType:      "user",
			Role:             string(AuthorizationRoleOwner),
		}
		if err := tx.Create(&ownerAccess).Error; err != nil {
			return dberrors.Classify(err)
		}

		// Add Security Reviewers group if not confidential, or Confidential Project Reviewers if confidential
		isConfidential := response.IsConfidential != nil && *response.IsConfidential
		if !isConfidential {
			groupUUID, err := s.ensureSecurityReviewersGroup(tx)
			if err != nil {
				return dberrors.Classify(err)
			}
			reviewersAccess := models.SurveyResponseAccess{
				SurveyResponseID:  model.ID,
				GroupInternalUUID: &groupUUID,
				SubjectType:       "group",
				Role:              string(AuthorizationRoleOwner),
			}
			if err := tx.Create(&reviewersAccess).Error; err != nil {
				return dberrors.Classify(err)
			}
		} else {
			groupUUID, err := s.ensureConfidentialProjectReviewersGroup(tx)
			if err != nil {
				return dberrors.Classify(err)
			}
			reviewersAccess := models.SurveyResponseAccess{
				SurveyResponseID:  model.ID,
				GroupInternalUUID: &groupUUID,
				SubjectType:       "group",
				Role:              string(AuthorizationRoleOwner),
			}
			if err := tx.Create(&reviewersAccess).Error; err != nil {
				return dberrors.Classify(err)
			}
		}

		// Add TMI Automation group with writer role
		automationGroupUUID, err := s.ensureTMIAutomationGroup(tx)
		if err != nil {
			return dberrors.Classify(err)
		}
		automationAccess := models.SurveyResponseAccess{
			SurveyResponseID:  model.ID,
			GroupInternalUUID: &automationGroupUUID,
			SubjectType:       "group",
			Role:              string(AuthorizationRoleWriter),
		}
		if err := tx.Create(&automationAccess).Error; err != nil {
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Save metadata if provided
	if response.Metadata != nil && len(*response.Metadata) > 0 {
		if err := s.saveMetadata(ctx, response.Id.String(), *response.Metadata); err != nil {
			logger.Error("Failed to save metadata for survey response: id=%s, error=%v", response.Id, err)
			return dberrors.Classify(err)
		}
	}

	// Update response with server-generated values
	response.CreatedAt = &model.CreatedAt
	response.ModifiedAt = &model.ModifiedAt

	// Load authorization entries so they're included in the response
	auth, err := s.loadAuthorization(ctx, response.Id.String())
	if err != nil {
		logger.Error("Failed to load authorization after create: id=%s, error=%v", response.Id, err)
		return dberrors.Classify(err)
	}
	response.Authorization = &auth

	logger.Info("Survey response created: id=%s, survey_id=%s, owner=%s",
		response.Id, response.SurveyId, userInternalUUID)

	return nil
}

// ensureBuiltInGroup is the SAVEPOINT-protected create-then-recover-on-conflict
// helper shared by the three ensure*Group methods below. The race-recovery
// (re-fetch after a duplicate-key INSERT failure) must be wrapped in a
// SAVEPOINT on PostgreSQL: without it, an SQLSTATE 23505 from the INSERT
// aborts the outer BEGIN/COMMIT block and the follow-up SELECT returns
// "current transaction is aborted" rather than the existing row. Oracle has
// statement-level rollback semantics so this is purely a PG correctness fix —
// safe to run on Oracle (RollbackTo is a no-op on a clean savepoint).
//
// In practice the seed in api/seed/seed.go pre-populates these groups, so the
// race window only opens for fresh databases that haven't been seeded yet.
func (s *GormSurveyResponseStore) ensureBuiltInGroup(tx *gorm.DB, groupKey, groupDisplay, groupUUID string) (string, error) {
	var group models.Group
	result := tx.Where("group_name = ? AND provider = ?", groupKey, BuiltInProvider).First(&group)
	if result.Error == nil {
		return group.InternalUUID, nil
	}
	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return "", dberrors.Classify(result.Error)
	}

	// Use a savepoint so a duplicate-key INSERT failure does not abort the outer transaction on PG.
	const sp = "ensure_builtin_group"
	if err := tx.SavePoint(sp).Error; err != nil {
		return "", dberrors.Classify(err)
	}

	group = models.Group{
		InternalUUID: groupUUID,
		Provider:     BuiltInProvider,
		GroupName:    groupKey,
		Name:         &groupDisplay,
		UsageCount:   1,
	}

	if err := tx.Create(&group).Error; err != nil {
		// Roll back to savepoint so the outer transaction stays usable on PG.
		if rbErr := tx.RollbackTo(sp).Error; rbErr != nil {
			return "", dberrors.Classify(rbErr)
		}
		// Race recovery: re-fetch the row that another transaction must have created.
		var existingGroup models.Group
		if fetchErr := tx.Where("group_name = ? AND provider = ?", groupKey, BuiltInProvider).First(&existingGroup).Error; fetchErr == nil {
			return existingGroup.InternalUUID, nil
		}
		return "", dberrors.Classify(err)
	}

	return group.InternalUUID, nil
}

// ensureSecurityReviewersGroup ensures the Security Reviewers group exists and returns its UUID
func (s *GormSurveyResponseStore) ensureSecurityReviewersGroup(tx *gorm.DB) (string, error) {
	return s.ensureBuiltInGroup(tx, SecurityReviewersGroup, "Security Reviewers", SecurityReviewersGroupUUID)
}

// ensureConfidentialProjectReviewersGroup ensures the confidential-project-reviewers group exists and returns its UUID.
func (s *GormSurveyResponseStore) ensureConfidentialProjectReviewersGroup(tx *gorm.DB) (string, error) {
	return s.ensureBuiltInGroup(tx, ConfidentialProjectReviewersGroup, "Confidential Project Reviewers", ConfidentialProjectReviewersGroupUUID)
}

// ensureTMIAutomationGroup ensures the tmi-automation group exists and returns its UUID.
func (s *GormSurveyResponseStore) ensureTMIAutomationGroup(tx *gorm.DB) (string, error) {
	return s.ensureBuiltInGroup(tx, TMIAutomationGroup, "TMI Automation", TMIAutomationGroupUUID)
}

// Get retrieves a survey response by ID
func (s *GormSurveyResponseStore) Get(ctx context.Context, id uuid.UUID) (*SurveyResponse, error) {
	logger := slogging.Get()

	var model models.SurveyResponse
	result := s.db.WithContext(ctx).
		Preload("Owner").
		Preload("ReviewedBy").
		Preload("Template").
		First(&model, "id = ?", id.String())

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			logger.Debug("Survey response not found: id=%s", id)
			return nil, nil
		}
		logger.Error("Failed to get survey response: id=%s, error=%v", id, result.Error)
		return nil, dberrors.Classify(result.Error)
	}

	response, err := s.modelToAPI(&model)
	if err != nil {
		logger.Error("Failed to convert model to API: id=%s, error=%v", id, err)
		return nil, fmt.Errorf("failed to convert response: %w", err)
	}

	// Load authorization
	auth, err := s.loadAuthorization(ctx, id.String())
	if err != nil {
		logger.Error("Failed to load authorization: id=%s, error=%v", id, err)
		return nil, dberrors.Classify(err)
	}
	response.Authorization = &auth

	// Load metadata
	metadata, err := s.loadMetadata(ctx, id.String())
	if err != nil {
		logger.Error("Failed to load metadata: id=%s, error=%v", id, err)
		return nil, dberrors.Classify(err)
	}
	if len(metadata) > 0 {
		response.Metadata = &metadata
	}

	logger.Debug("Retrieved survey response: id=%s, status=%s", response.Id, *response.Status)

	return response, nil
}

// Update updates an existing survey response
func (s *GormSurveyResponseStore) Update(ctx context.Context, response *SurveyResponse) error {
	logger := slogging.Get()

	if response.Id == nil {
		return fmt.Errorf("response ID is required for update")
	}

	// Get current response to preserve immutable fields
	var current models.SurveyResponse
	if err := s.db.WithContext(ctx).First(&current, "id = ?", response.Id.String()).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSurveyResponseNotFound
		}
		return dberrors.Classify(err)
	}

	// Build update map (all updatable fields included unconditionally)
	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	updates := map[string]any{}

	// Marshal answers unconditionally; nil means clear the field
	if response.Answers != nil {
		answersJSON, err := json.Marshal(response.Answers)
		if err != nil {
			return fmt.Errorf("failed to marshal answers: %w", err)
		}
		updates["answers"] = answersJSON
	} else {
		updates["answers"] = nil
	}

	// Marshal ui_state unconditionally; nil means clear the field
	if response.UiState != nil {
		uiStateJSON, err := json.Marshal(response.UiState)
		if err != nil {
			return fmt.Errorf("failed to marshal ui_state: %w", err)
		}
		updates["ui_state"] = uiStateJSON
	} else {
		updates["ui_state"] = nil
	}

	// Convert project_id unconditionally; nil means clear the field
	if response.ProjectId != nil {
		s := response.ProjectId.String()
		updates["project_id"] = &s
	} else {
		updates["project_id"] = nil
	}

	// Convert linked_threat_model_id unconditionally; nil means clear the field
	if response.LinkedThreatModelId != nil {
		s := response.LinkedThreatModelId.String()
		updates["linked_threat_model_id"] = &s
	} else {
		updates["linked_threat_model_id"] = nil
	}

	// Note: status transitions should use UpdateStatus method
	// Note: is_confidential is immutable after creation
	// Note: survey_id and survey_version are immutable
	// Note: survey_json is immutable (set at creation from template snapshot)

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.
			Model(&models.SurveyResponse{}).
			Where("id = ?", response.Id.String()).
			Updates(updates)
		if result.Error != nil {
			logger.Error("Failed to update survey response: id=%s, error=%v", response.Id, result.Error)
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			logger.Debug("Survey response not found for update: id=%s", response.Id)
			return ErrSurveyResponseNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Save metadata if provided
	if response.Metadata != nil && len(*response.Metadata) > 0 {
		if err := s.saveMetadata(ctx, response.Id.String(), *response.Metadata); err != nil {
			logger.Error("Failed to save metadata for survey response: id=%s, error=%v", response.Id, err)
			return dberrors.Classify(err)
		}
	}

	logger.Info("Survey response updated: id=%s", response.Id)

	return nil
}

// Delete removes a survey response by ID
func (s *GormSurveyResponseStore) Delete(ctx context.Context, id uuid.UUID) error {
	logger := slogging.Get()

	// Check if response exists
	var response models.SurveyResponse
	if err := s.db.WithContext(ctx).First(&response, "id = ?", id.String()).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSurveyResponseNotFound
		}
		return dberrors.Classify(err)
	}

	// Delete in transaction (with retry on transient errors) — must remove all
	// dependent rows before the response.
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Where("survey_response_id = ?", id.String()).Delete(&models.TriageNote{}).Error; err != nil {
			return dberrors.Classify(err)
		}
		if err := tx.Where("survey_response_id = ?", id.String()).Delete(&models.SurveyResponseAccess{}).Error; err != nil {
			return dberrors.Classify(err)
		}
		if err := tx.Where("entity_type = ? AND entity_id = ?", "survey_response", id.String()).Delete(&models.Metadata{}).Error; err != nil {
			return dberrors.Classify(err)
		}
		if err := tx.Where("response_id = ?", id.String()).Delete(&models.SurveyAnswer{}).Error; err != nil {
			return dberrors.Classify(err)
		}
		if err := tx.Delete(&models.SurveyResponse{}, "id = ?", id.String()).Error; err != nil {
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	logger.Info("Survey response deleted: id=%s", id)

	return nil
}

// List retrieves survey responses with pagination and optional filters
func (s *GormSurveyResponseStore) List(ctx context.Context, limit, offset int, filters *SurveyResponseFilters) ([]SurveyResponseListItem, int, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.SurveyResponse{}).
		Where("owner_internal_uuid IS NOT NULL")

	// Apply filters
	if filters != nil {
		if filters.Status != nil {
			statuses := splitCommaValues(*filters.Status)
			if len(statuses) == 1 {
				query = query.Where("status = ?", statuses[0])
			} else {
				query = query.Where("status IN ?", statuses)
			}
		}
		if filters.SurveyID != nil {
			query = query.Where("template_id = ?", filters.SurveyID.String())
		}
		if filters.OwnerID != nil {
			query = query.Where("owner_internal_uuid = ?", *filters.OwnerID)
		}
	}

	// Get total count
	var total int64
	if err := query.Count(&total).Error; err != nil {
		logger.Error("Failed to count survey responses: error=%v", err)
		return nil, 0, dberrors.Classify(err)
	}

	// Get responses with pagination
	var modelList []models.SurveyResponse
	result := query.
		Preload("Owner").
		Preload("Template").
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to list survey responses: error=%v", result.Error)
		return nil, 0, dberrors.Classify(result.Error)
	}

	items := make([]SurveyResponseListItem, len(modelList))
	for i, model := range modelList {
		items[i] = s.modelToListItem(&model)
	}

	logger.Debug("Listed %d survey responses (total: %d, limit: %d, offset: %d)",
		len(items), total, limit, offset)

	return items, int(total), nil
}

// ListByOwner retrieves survey responses for a specific owner
func (s *GormSurveyResponseStore) ListByOwner(ctx context.Context, ownerInternalUUID string, limit, offset int, status *string) ([]SurveyResponseListItem, int, error) {
	filters := &SurveyResponseFilters{
		OwnerID: &ownerInternalUUID,
		Status:  status,
	}
	return s.List(ctx, limit, offset, filters)
}

// UpdateStatus transitions a response to a new status with validation
func (s *GormSurveyResponseStore) UpdateStatus(ctx context.Context, id uuid.UUID, newStatus string, reviewerInternalUUID *string, revisionNotes *string) error {
	logger := slogging.Get()

	var response models.SurveyResponse
	if err := s.db.WithContext(ctx).First(&response, "id = ?", id.String()).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSurveyResponseNotFound
		}
		return dberrors.Classify(err)
	}

	currentStatus := response.Status

	// Validate that the new status is a known status value
	validStatuses := map[string]bool{
		ResponseStatusDraft:          true,
		ResponseStatusSubmitted:      true,
		ResponseStatusNeedsRevision:  true,
		ResponseStatusReadyForReview: true,
		ResponseStatusReviewCreated:  true,
	}
	if !validStatuses[newStatus] {
		return fmt.Errorf("invalid status value: %s", newStatus)
	}

	// Require revision_notes when transitioning to needs_revision
	if newStatus == ResponseStatusNeedsRevision && (revisionNotes == nil || *revisionNotes == "") {
		return fmt.Errorf("revision_notes required when transitioning to needs_revision")
	}

	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	now := time.Now().UTC()
	updates := map[string]any{
		"status": newStatus,
	}

	switch newStatus {
	case ResponseStatusSubmitted:
		updates["submitted_at"] = now
		updates["ui_state"] = nil // Clear UI state on submission
	case ResponseStatusReadyForReview:
		updates["reviewed_at"] = now
		if reviewerInternalUUID != nil {
			updates["reviewed_by_internal_uuid"] = *reviewerInternalUUID
		}
	case ResponseStatusNeedsRevision:
		updates["revision_notes"] = *revisionNotes
		updates["reviewed_at"] = now
		if reviewerInternalUUID != nil {
			updates["reviewed_by_internal_uuid"] = *reviewerInternalUUID
		}
	}

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.
			Model(&models.SurveyResponse{}).
			Where("id = ?", id.String()).
			Updates(updates)
		if result.Error != nil {
			logger.Error("Failed to update survey response status: id=%s, from=%s, to=%s, error=%v",
				id, currentStatus, newStatus, result.Error)
			return dberrors.Classify(result.Error)
		}
		return nil
	})
	if err != nil {
		return err
	}

	logger.Info("Survey response status updated: id=%s, from=%s, to=%s", id, currentStatus, newStatus)

	return nil
}

// GetAuthorization retrieves authorization entries for a response
func (s *GormSurveyResponseStore) GetAuthorization(ctx context.Context, id uuid.UUID) ([]Authorization, error) {
	return s.loadAuthorization(ctx, id.String())
}

// UpdateAuthorization updates authorization entries for a response
func (s *GormSurveyResponseStore) UpdateAuthorization(ctx context.Context, id uuid.UUID, authorization []Authorization) error {
	logger := slogging.Get()

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// T14 (#354): serialize concurrent ACL writes by row-locking the
		// parent survey response. See updateAuthorizationTx in
		// database_store_gorm.go for the equivalent guard on threat models.
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&models.SurveyResponse{}, "id = ?", id.String()).Error; err != nil {
			return dberrors.Classify(fmt.Errorf("acquiring row lock on survey response %s: %w", id, err))
		}

		// Delete existing entries
		if err := tx.Where("survey_response_id = ?", id.String()).Delete(&models.SurveyResponseAccess{}).Error; err != nil {
			return dberrors.Classify(err)
		}

		// Create new entries
		for _, auth := range authorization {
			access := models.SurveyResponseAccess{
				SurveyResponseID: id.String(),
				SubjectType:      string(auth.PrincipalType),
				Role:             string(auth.Role),
			}

			switch auth.PrincipalType {
			case AuthorizationPrincipalTypeUser:
				userUUID, err := s.resolveUserToUUID(tx, auth.ProviderId, auth.Provider)
				if err != nil {
					return err
				}
				access.UserInternalUUID = &userUUID
			case AuthorizationPrincipalTypeGroup:
				groupUUID, err := s.resolveGroupToUUID(tx, auth.ProviderId, &auth.Provider)
				if err != nil {
					return err
				}
				access.GroupInternalUUID = &groupUUID
			}

			if err := tx.Create(&access).Error; err != nil {
				return dberrors.Classify(err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	logger.Info("Updated authorization for survey response: id=%s, entries=%d", id, len(authorization))

	return nil
}

// HasAccess checks if a user has the required access to a response
func (s *GormSurveyResponseStore) HasAccess(ctx context.Context, id uuid.UUID, userInternalUUID string, requiredRole AuthorizationRole) (bool, error) {
	// Get user's groups
	var userGroups []models.GroupMember
	if err := s.db.WithContext(ctx).Where("user_internal_uuid = ?", userInternalUUID).Find(&userGroups).Error; err != nil {
		return false, dberrors.Classify(err)
	}

	groupUUIDs := make([]string, len(userGroups))
	for i, g := range userGroups {
		groupUUIDs[i] = g.GroupInternalUUID
	}

	// Check user's direct access and group access
	var count int64
	query := s.db.WithContext(ctx).Model(&models.SurveyResponseAccess{}).
		Where("survey_response_id = ?", id.String())

	if len(groupUUIDs) > 0 {
		query = query.Where("(user_internal_uuid = ? OR group_internal_uuid IN ?)", userInternalUUID, groupUUIDs)
	} else {
		query = query.Where("user_internal_uuid = ?", userInternalUUID)
	}

	// Role hierarchy: owner > writer > reader
	switch requiredRole {
	case AuthorizationRoleReader:
		query = query.Where("role IN ?", []string{string(AuthorizationRoleReader), string(AuthorizationRoleWriter), string(AuthorizationRoleOwner)})
	case AuthorizationRoleWriter:
		query = query.Where("role IN ?", []string{string(AuthorizationRoleWriter), string(AuthorizationRoleOwner)})
	case AuthorizationRoleOwner:
		query = query.Where("role = ?", string(AuthorizationRoleOwner))
	}

	if err := query.Count(&count).Error; err != nil {
		return false, dberrors.Classify(err)
	}

	return count > 0, nil
}

// SetCreatedThreatModel atomically sets created_threat_model_id and transitions
// status to review_created with an optimistic concurrency guard.
//
// Idempotent on retry: if the optimistic-concurrency UPDATE matches zero rows,
// re-read the row and treat it as success when the row already shows
// (status=review_created, created_threat_model_id=requested). This handles the
// commit-ack-loss case where a transient ADB error fires after the server-side
// commit but before the client receives the ack — the original update is
// already persisted; the retry's WHERE predicate fails because status has moved
// past ready_for_review. Without this check, the retry surfaced a misleading
// "not in ready_for_review status" error even though the operation succeeded.
func (s *GormSurveyResponseStore) SetCreatedThreatModel(ctx context.Context, id uuid.UUID, threatModelID string) error {
	return authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.
			Model(&models.SurveyResponse{}).
			Where("id = ? AND status = ?", id.String(), ResponseStatusReadyForReview).
			Updates(map[string]any{
				"status":                  ResponseStatusReviewCreated,
				"created_threat_model_id": threatModelID,
			})
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			// Distinguish retry-success-on-second-attempt from genuine
			// precondition failure: read the row and check whether it
			// already reflects the requested state.
			var existing models.SurveyResponse
			if err := tx.Select("status", "created_threat_model_id").
				Where("id = ?", id.String()).
				First(&existing).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("survey response %s does not exist", id)
				}
				return dberrors.Classify(err)
			}
			if existing.Status == ResponseStatusReviewCreated &&
				existing.CreatedThreatModelID != nil &&
				*existing.CreatedThreatModelID == threatModelID {
				// Already transitioned to the requested state — treat as success.
				return nil
			}
			return fmt.Errorf("survey response %s is not in ready_for_review status", id)
		}
		return nil
	})
}

// loadAuthorization loads authorization entries for a response
func (s *GormSurveyResponseStore) loadAuthorization(ctx context.Context, responseID string) ([]Authorization, error) {
	var accessEntries []models.SurveyResponseAccess
	result := s.db.WithContext(ctx).
		Preload("User").
		Preload("Group").
		Where("survey_response_id = ?", responseID).
		Find(&accessEntries)

	if result.Error != nil {
		return nil, dberrors.Classify(result.Error)
	}

	authorization := make([]Authorization, 0, len(accessEntries))
	for _, entry := range accessEntries {
		auth := Authorization{
			Role: AuthorizationRole(entry.Role),
		}

		if entry.SubjectType == "user" && entry.User != nil {
			auth.PrincipalType = AuthorizationPrincipalTypeUser
			auth.Provider = entry.User.Provider
			if entry.User.ProviderUserID != nil {
				auth.ProviderId = *entry.User.ProviderUserID
			}
		} else if entry.SubjectType == "group" && entry.Group != nil {
			auth.PrincipalType = AuthorizationPrincipalTypeGroup
			auth.Provider = entry.Group.Provider
			auth.ProviderId = entry.Group.GroupName
		}

		authorization = append(authorization, auth)
	}

	return authorization, nil
}

// resolveUserToUUID resolves a user identifier to internal UUID
func (s *GormSurveyResponseStore) resolveUserToUUID(tx *gorm.DB, providerUserID, provider string) (string, error) {
	var user models.User
	result := tx.Where("provider = ? AND provider_user_id = ?", provider, providerUserID).First(&user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("%s@%s: %w", providerUserID, provider, ErrUserNotFound)
		}
		return "", dberrors.Classify(result.Error)
	}
	return user.InternalUUID, nil
}

// resolveGroupToUUID resolves a group identifier to internal UUID
func (s *GormSurveyResponseStore) resolveGroupToUUID(tx *gorm.DB, groupName string, provider *string) (string, error) {
	p := BuiltInProvider
	if provider != nil && *provider != "" {
		p = *provider
	}

	var group models.Group
	result := tx.Where("provider = ? AND group_name = ?", p, groupName).First(&group)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("%s@%s: %w", groupName, p, ErrGroupNotFound)
		}
		return "", dberrors.Classify(result.Error)
	}
	return group.InternalUUID, nil
}

// apiToModel converts an API SurveyResponse to a database model
func (s *GormSurveyResponseStore) apiToModel(response *SurveyResponse, ownerInternalUUID string) (*models.SurveyResponse, error) {
	model := &models.SurveyResponse{
		TemplateID:        response.SurveyId.String(),
		OwnerInternalUUID: &ownerInternalUUID,
	}

	if response.Id != nil {
		model.ID = response.Id.String()
	}

	if response.SurveyVersion != nil {
		model.TemplateVersion = *response.SurveyVersion
	}

	if response.Status != nil {
		model.Status = *response.Status
	} else {
		model.Status = ResponseStatusDraft
	}

	if response.IsConfidential != nil {
		model.IsConfidential = models.DBBool(*response.IsConfidential)
	}

	// Convert answers to JSON
	if response.Answers != nil {
		answersJSON, err := json.Marshal(response.Answers)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal answers: %w", err)
		}
		model.Answers = answersJSON
	}

	// Convert ui_state to JSON
	if response.UiState != nil {
		uiStateJSON, err := json.Marshal(response.UiState)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal ui_state: %w", err)
		}
		model.UIState = uiStateJSON
	}

	// Convert survey_json to JSON (snapshot from template)
	if response.SurveyJson != nil {
		surveyJSON, err := json.Marshal(response.SurveyJson)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal survey_json: %w", err)
		}
		model.SurveyJSON = surveyJSON
	}

	if response.LinkedThreatModelId != nil {
		idStr := response.LinkedThreatModelId.String()
		model.LinkedThreatModelID = &idStr
	}

	if response.RevisionNotes != nil {
		model.RevisionNotes = response.RevisionNotes
	}

	if response.ProjectId != nil {
		s := response.ProjectId.String()
		model.ProjectID = &s
	}

	return model, nil
}

// modelToAPI converts a database model to an API SurveyResponse
func (s *GormSurveyResponseStore) modelToAPI(model *models.SurveyResponse) (*SurveyResponse, error) {
	id, err := uuid.Parse(model.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid response ID: %w", err)
	}

	surveyID, err := uuid.Parse(model.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("invalid survey ID: %w", err)
	}

	response := &SurveyResponse{
		Id:            &id,
		SurveyId:      surveyID,
		SurveyVersion: &model.TemplateVersion,
		CreatedAt:     &model.CreatedAt,
		ModifiedAt:    &model.ModifiedAt,
		SubmittedAt:   model.SubmittedAt,
		ReviewedAt:    model.ReviewedAt,
		RevisionNotes: model.RevisionNotes,
	}

	// Convert status
	status := model.Status
	response.Status = &status

	// Convert is_confidential
	isConf := bool(model.IsConfidential)
	response.IsConfidential = &isConf

	// Convert answers from JSON
	if len(model.Answers) > 0 {
		var answers map[string]any
		if err := json.Unmarshal(model.Answers, &answers); err != nil {
			return nil, fmt.Errorf("failed to unmarshal answers: %w", err)
		}
		response.Answers = &answers
	}

	// Convert ui_state from JSON
	if len(model.UIState) > 0 {
		var uiState map[string]any
		if err := json.Unmarshal(model.UIState, &uiState); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ui_state: %w", err)
		}
		response.UiState = &uiState
	}

	// Convert survey_json from JSON (template snapshot)
	if len(model.SurveyJSON) > 0 {
		var surveyJSON map[string]any
		if err := json.Unmarshal(model.SurveyJSON, &surveyJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal survey_json: %w", err)
		}
		response.SurveyJson = &surveyJSON
	}

	// Convert linked threat model ID
	if model.LinkedThreatModelID != nil {
		linkedID, err := uuid.Parse(*model.LinkedThreatModelID)
		if err == nil {
			response.LinkedThreatModelId = &linkedID
		}
	}

	// Convert created threat model ID
	if model.CreatedThreatModelID != nil {
		createdID, err := uuid.Parse(*model.CreatedThreatModelID)
		if err == nil {
			response.CreatedThreatModelId = &createdID
		}
	}

	// Convert owner
	if model.Owner != nil && model.Owner.InternalUUID != "" {
		response.Owner = userModelToAPI(model.Owner)
	}

	// Convert reviewed_by
	if model.ReviewedBy != nil && model.ReviewedBy.InternalUUID != "" {
		response.ReviewedBy = userModelToAPI(model.ReviewedBy)
	}

	// Convert project_id
	if model.ProjectID != nil && *model.ProjectID != "" {
		pid, err := uuid.Parse(*model.ProjectID)
		if err == nil {
			response.ProjectId = &pid
		}
	}

	return response, nil
}

// modelToListItem converts a database model to an API SurveyResponseListItem
func (s *GormSurveyResponseStore) modelToListItem(model *models.SurveyResponse) SurveyResponseListItem {
	id, _ := uuid.Parse(model.ID)
	surveyID, _ := uuid.Parse(model.TemplateID)

	item := SurveyResponseListItem{
		Id:            &id,
		SurveyId:      surveyID,
		SurveyVersion: &model.TemplateVersion,
		Status:        model.Status,
		CreatedAt:     model.CreatedAt,
		ModifiedAt:    &model.ModifiedAt,
		SubmittedAt:   model.SubmittedAt,
	}

	// Get survey name (pointer field)
	if model.Template.Name != "" {
		item.SurveyName = &model.Template.Name
	}

	// Convert owner (nullable)
	if model.Owner != nil && model.Owner.InternalUUID != "" {
		item.Owner = userModelToAPI(model.Owner)
	}

	return item
}

// loadMetadata loads metadata for a survey response
func (s *GormSurveyResponseStore) loadMetadata(ctx context.Context, responseID string) ([]Metadata, error) {
	return loadEntityMetadata(s.db.WithContext(ctx), "survey_response", responseID)
}

// saveMetadata saves metadata for a survey response
func (s *GormSurveyResponseStore) saveMetadata(ctx context.Context, responseID string, metadata []Metadata) error {
	return saveEntityMetadata(s.db.WithContext(ctx), "survey_response", responseID, metadata)
}

// userModelToAPI converts a database User model to an API User
