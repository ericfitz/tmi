package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

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
			return fmt.Errorf("survey not found: %s", response.SurveyId)
		}
		return fmt.Errorf("failed to get survey: %w", result.Error)
	}

	// Capture survey version at creation
	response.SurveyVersion = &template.Version

	// Snapshot the template's survey_json for rendering historical responses
	if len(template.SurveyJSON) > 0 {
		var surveyJSON map[string]interface{}
		if err := json.Unmarshal(template.SurveyJSON, &surveyJSON); err == nil {
			response.SurveyJson = &surveyJSON
		}
	}

	model, err := s.apiToModel(response, userInternalUUID)
	if err != nil {
		logger.Error("Failed to convert response to model: error=%v", err)
		return fmt.Errorf("failed to convert response: %w", err)
	}

	// Start transaction for creating response and access entries
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Create the response
	if err := tx.Create(&model).Error; err != nil {
		tx.Rollback()
		logger.Error("Failed to create survey response: error=%v", err)
		return fmt.Errorf("failed to create survey response: %w", err)
	}

	// Add owner access entry
	ownerAccess := models.SurveyResponseAccess{
		SurveyResponseID: model.ID,
		UserInternalUUID: &userInternalUUID,
		SubjectType:      "user",
		Role:             string(AuthorizationRoleOwner),
	}
	if err := tx.Create(&ownerAccess).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to create owner access: %w", err)
	}

	// Add Security Reviewers group if not confidential
	isConfidential := response.IsConfidential != nil && *response.IsConfidential
	if !isConfidential {
		// Get or create Security Reviewers group
		groupUUID, err := s.ensureSecurityReviewersGroup(tx)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to ensure security reviewers group: %w", err)
		}

		reviewersAccess := models.SurveyResponseAccess{
			SurveyResponseID:  model.ID,
			GroupInternalUUID: &groupUUID,
			SubjectType:       "group",
			Role:              string(AuthorizationRoleOwner), // Owner role for triage actions
		}
		if err := tx.Create(&reviewersAccess).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to create security reviewers access: %w", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Save metadata if provided
	if response.Metadata != nil && len(*response.Metadata) > 0 {
		if err := s.saveMetadata(ctx, response.Id.String(), *response.Metadata); err != nil {
			logger.Error("Failed to save metadata for survey response: id=%s, error=%v", response.Id, err)
			return fmt.Errorf("failed to save metadata: %w", err)
		}
	}

	// Update response with server-generated values
	response.CreatedAt = &model.CreatedAt
	response.ModifiedAt = &model.ModifiedAt

	// Load authorization entries so they're included in the response
	auth, err := s.loadAuthorization(ctx, response.Id.String())
	if err != nil {
		logger.Error("Failed to load authorization after create: id=%s, error=%v", response.Id, err)
		return fmt.Errorf("failed to load authorization: %w", err)
	}
	response.Authorization = &auth

	logger.Info("Survey response created: id=%s, survey_id=%s, owner=%s",
		response.Id, response.SurveyId, userInternalUUID)

	return nil
}

// ensureSecurityReviewersGroup ensures the Security Reviewers group exists and returns its UUID
func (s *GormSurveyResponseStore) ensureSecurityReviewersGroup(tx *gorm.DB) (string, error) {
	var group models.Group
	result := tx.Where("group_name = ? AND provider = ?", SecurityReviewersGroup, "*").First(&group)

	if result.Error == nil {
		return group.InternalUUID, nil
	}

	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return "", result.Error
	}

	// Create the group
	groupName := "Security Reviewers"
	group = models.Group{
		InternalUUID: SecurityReviewersGroupUUID,
		Provider:     "*",
		GroupName:    SecurityReviewersGroup,
		Name:         &groupName,
		UsageCount:   1,
	}

	if err := tx.Create(&group).Error; err != nil {
		// Handle race condition - another transaction may have created it
		var existingGroup models.Group
		if tx.Where("group_name = ? AND provider = ?", SecurityReviewersGroup, "*").First(&existingGroup).Error == nil {
			return existingGroup.InternalUUID, nil
		}
		return "", err
	}

	return group.InternalUUID, nil
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
		return nil, fmt.Errorf("failed to get survey response: %w", result.Error)
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
		return nil, fmt.Errorf("failed to load authorization: %w", err)
	}
	response.Authorization = &auth

	// Load metadata
	metadata, err := s.loadMetadata(ctx, id.String())
	if err != nil {
		logger.Error("Failed to load metadata: id=%s, error=%v", id, err)
		return nil, fmt.Errorf("failed to load metadata: %w", err)
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
			return fmt.Errorf("survey response not found: %s", response.Id)
		}
		return fmt.Errorf("failed to get current response: %w", err)
	}

	// Build update map (only updatable fields)
	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	updates := map[string]interface{}{}

	// Only update answers if provided
	if response.Answers != nil {
		answersJSON, err := json.Marshal(response.Answers)
		if err != nil {
			return fmt.Errorf("failed to marshal answers: %w", err)
		}
		updates["answers"] = answersJSON
	}

	// Update ui_state if provided
	if response.UiState != nil {
		uiStateJSON, err := json.Marshal(response.UiState)
		if err != nil {
			return fmt.Errorf("failed to marshal ui_state: %w", err)
		}
		updates["ui_state"] = uiStateJSON
	}

	// Note: status transitions should use UpdateStatus method
	// Note: is_confidential is immutable after creation
	// Note: survey_id and survey_version are immutable
	// Note: survey_json is immutable (set at creation from template snapshot)

	result := s.db.WithContext(ctx).
		Model(&models.SurveyResponse{}).
		Where("id = ?", response.Id.String()).
		Updates(updates)

	if result.Error != nil {
		logger.Error("Failed to update survey response: id=%s, error=%v", response.Id, result.Error)
		return fmt.Errorf("failed to update survey response: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		logger.Debug("Survey response not found for update: id=%s", response.Id)
		return fmt.Errorf("survey response not found: %s", response.Id)
	}

	// Save metadata if provided
	if response.Metadata != nil && len(*response.Metadata) > 0 {
		if err := s.saveMetadata(ctx, response.Id.String(), *response.Metadata); err != nil {
			logger.Error("Failed to save metadata for survey response: id=%s, error=%v", response.Id, err)
			return fmt.Errorf("failed to save metadata: %w", err)
		}
	}

	logger.Info("Survey response updated: id=%s", response.Id)

	return nil
}

// Delete removes a survey response by ID (only allowed for draft status)
func (s *GormSurveyResponseStore) Delete(ctx context.Context, id uuid.UUID) error {
	logger := slogging.Get()

	// Check if response exists and is in draft status
	var response models.SurveyResponse
	if err := s.db.WithContext(ctx).First(&response, "id = ?", id.String()).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("survey response not found: %s", id)
		}
		return fmt.Errorf("failed to get response: %w", err)
	}

	if response.Status != ResponseStatusDraft {
		return fmt.Errorf("can only delete draft responses, current status: %s", response.Status)
	}

	// Delete in transaction - must remove all dependent rows before the response
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	// Delete triage notes (FK constraint: fk_triage_notes_survey_response)
	if err := tx.Where("survey_response_id = ?", id.String()).Delete(&models.TriageNote{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete triage notes: %w", err)
	}

	// Delete access entries
	if err := tx.Where("survey_response_id = ?", id.String()).Delete(&models.SurveyResponseAccess{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete access entries: %w", err)
	}

	// Delete associated metadata (no FK, but clean up orphaned rows)
	if err := tx.Where("entity_type = ? AND entity_id = ?", "survey_response", id.String()).Delete(&models.Metadata{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete metadata: %w", err)
	}

	// Delete the response
	if err := tx.Delete(&models.SurveyResponse{}, "id = ?", id.String()).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete survey response: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	logger.Info("Survey response deleted: id=%s", id)

	return nil
}

// List retrieves survey responses with pagination and optional filters
func (s *GormSurveyResponseStore) List(ctx context.Context, limit, offset int, filters *SurveyResponseFilters) ([]SurveyResponseListItem, int, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.SurveyResponse{})

	// Apply filters
	if filters != nil {
		if filters.Status != nil {
			query = query.Where("status = ?", *filters.Status)
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
		return nil, 0, fmt.Errorf("failed to count survey responses: %w", err)
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
		return nil, 0, fmt.Errorf("failed to list survey responses: %w", result.Error)
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
			return fmt.Errorf("survey response not found: %s", id)
		}
		return fmt.Errorf("failed to get response: %w", err)
	}

	currentStatus := response.Status

	// Validate state transition
	if !isValidStatusTransition(currentStatus, newStatus) {
		return fmt.Errorf("invalid state transition from %s to %s", currentStatus, newStatus)
	}

	// Require revision_notes when transitioning to needs_revision
	if newStatus == ResponseStatusNeedsRevision && (revisionNotes == nil || *revisionNotes == "") {
		return fmt.Errorf("revision_notes required when transitioning to needs_revision")
	}

	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	now := time.Now().UTC()
	updates := map[string]interface{}{
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

	result := s.db.WithContext(ctx).
		Model(&models.SurveyResponse{}).
		Where("id = ?", id.String()).
		Updates(updates)

	if result.Error != nil {
		logger.Error("Failed to update survey response status: id=%s, from=%s, to=%s, error=%v",
			id, currentStatus, newStatus, result.Error)
		return fmt.Errorf("failed to update survey response status: %w", result.Error)
	}

	logger.Info("Survey response status updated: id=%s, from=%s, to=%s", id, currentStatus, newStatus)

	return nil
}

// isValidStatusTransition checks if a status transition is allowed
func isValidStatusTransition(from, to string) bool {
	validTransitions := map[string][]string{
		ResponseStatusDraft:          {ResponseStatusSubmitted},
		ResponseStatusSubmitted:      {ResponseStatusReadyForReview, ResponseStatusNeedsRevision},
		ResponseStatusNeedsRevision:  {ResponseStatusSubmitted},
		ResponseStatusReadyForReview: {ResponseStatusNeedsRevision, ResponseStatusReviewCreated},
	}

	allowed, exists := validTransitions[from]
	if !exists {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// GetAuthorization retrieves authorization entries for a response
func (s *GormSurveyResponseStore) GetAuthorization(ctx context.Context, id uuid.UUID) ([]Authorization, error) {
	return s.loadAuthorization(ctx, id.String())
}

// UpdateAuthorization updates authorization entries for a response
func (s *GormSurveyResponseStore) UpdateAuthorization(ctx context.Context, id uuid.UUID, authorization []Authorization) error {
	logger := slogging.Get()

	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Delete existing entries
	if err := tx.Where("survey_response_id = ?", id.String()).Delete(&models.SurveyResponseAccess{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete existing access entries: %w", err)
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
			// Resolve user to internal UUID
			userUUID, err := s.resolveUserToUUID(tx, auth.ProviderId, auth.Provider)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to resolve user: %w", err)
			}
			access.UserInternalUUID = &userUUID
		case AuthorizationPrincipalTypeGroup:
			// Resolve group to internal UUID
			groupUUID, err := s.resolveGroupToUUID(tx, auth.ProviderId, &auth.Provider)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to resolve group: %w", err)
			}
			access.GroupInternalUUID = &groupUUID
		}

		if err := tx.Create(&access).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to create access entry: %w", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	logger.Info("Updated authorization for survey response: id=%s, entries=%d", id, len(authorization))

	return nil
}

// HasAccess checks if a user has the required access to a response
func (s *GormSurveyResponseStore) HasAccess(ctx context.Context, id uuid.UUID, userInternalUUID string, requiredRole AuthorizationRole) (bool, error) {
	// Get user's groups
	var userGroups []models.GroupMember
	if err := s.db.WithContext(ctx).Where("user_internal_uuid = ?", userInternalUUID).Find(&userGroups).Error; err != nil {
		return false, fmt.Errorf("failed to get user groups: %w", err)
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
		return false, fmt.Errorf("failed to check access: %w", err)
	}

	return count > 0, nil
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
		return nil, result.Error
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
			return "", fmt.Errorf("user not found: %s@%s", providerUserID, provider)
		}
		return "", result.Error
	}
	return user.InternalUUID, nil
}

// resolveGroupToUUID resolves a group identifier to internal UUID
func (s *GormSurveyResponseStore) resolveGroupToUUID(tx *gorm.DB, groupName string, provider *string) (string, error) {
	p := "*"
	if provider != nil && *provider != "" {
		p = *provider
	}

	var group models.Group
	result := tx.Where("provider = ? AND group_name = ?", p, groupName).First(&group)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("group not found: %s@%s", groupName, p)
		}
		return "", result.Error
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
		var answers map[string]SurveyResponse_Answers_AdditionalProperties
		if err := json.Unmarshal(model.Answers, &answers); err != nil {
			return nil, fmt.Errorf("failed to unmarshal answers: %w", err)
		}
		response.Answers = &answers
	}

	// Convert ui_state from JSON
	if len(model.UIState) > 0 {
		var uiState map[string]interface{}
		if err := json.Unmarshal(model.UIState, &uiState); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ui_state: %w", err)
		}
		response.UiState = &uiState
	}

	// Convert survey_json from JSON (template snapshot)
	if len(model.SurveyJSON) > 0 {
		var surveyJSON map[string]interface{}
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
