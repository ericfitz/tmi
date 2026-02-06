package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/types"
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
	ListByOwner(ctx context.Context, ownerInternalUUID string, limit, offset int, status *SurveyResponseStatus) ([]SurveyResponseListItem, int, error)

	// State transition operations
	Submit(ctx context.Context, id uuid.UUID) error
	Approve(ctx context.Context, id uuid.UUID, reviewerInternalUUID string) error
	Return(ctx context.Context, id uuid.UUID, reviewerInternalUUID string, notes string) error

	// Authorization operations
	GetAuthorization(ctx context.Context, id uuid.UUID) ([]Authorization, error)
	UpdateAuthorization(ctx context.Context, id uuid.UUID, authorization []Authorization) error

	// Check access
	HasAccess(ctx context.Context, id uuid.UUID, userInternalUUID string, requiredRole AuthorizationRole) (bool, error)
}

// SurveyResponseFilters defines filter options for listing responses
type SurveyResponseFilters struct {
	Status     *SurveyResponseStatus
	TemplateID *uuid.UUID
	OwnerID    *string
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
		status := Draft
		response.Status = &status
	}

	// Validate template exists and get version
	var template models.SurveyTemplate
	result := s.db.WithContext(ctx).First(&template, "id = ?", response.TemplateId.String())
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return fmt.Errorf("template not found: %s", response.TemplateId)
		}
		return fmt.Errorf("failed to get template: %w", result.Error)
	}

	// Capture template version at creation
	response.TemplateVersion = &template.Version

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

	// Update response with server-generated values
	response.CreatedAt = &model.CreatedAt
	response.ModifiedAt = &model.ModifiedAt

	logger.Info("Survey response created: id=%s, template_id=%s, owner=%s",
		response.Id, response.TemplateId, userInternalUUID)

	return nil
}

// ensureSecurityReviewersGroup ensures the Security Reviewers group exists and returns its UUID
func (s *GormSurveyResponseStore) ensureSecurityReviewersGroup(tx *gorm.DB) (string, error) {
	var group models.Group
	result := tx.Where("group_name = ? AND provider = ?", SecurityReviewersGroup, "*").First(&group)

	if result.Error == nil {
		return group.InternalUUID, nil
	}

	if result.Error != gorm.ErrRecordNotFound {
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
		if result.Error == gorm.ErrRecordNotFound {
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
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("survey response not found: %s", response.Id)
		}
		return fmt.Errorf("failed to get current response: %w", err)
	}

	// Build update map (only updatable fields)
	updates := map[string]interface{}{
		"modified_at": time.Now().UTC(),
	}

	// Only update answers if provided
	if response.Answers != nil {
		answersJSON, err := json.Marshal(response.Answers)
		if err != nil {
			return fmt.Errorf("failed to marshal answers: %w", err)
		}
		updates["answers"] = answersJSON
	}

	// Note: status transitions should use dedicated methods (Submit, Approve, Return)
	// Note: is_confidential is immutable after creation
	// Note: template_id and template_version are immutable

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

	logger.Info("Survey response updated: id=%s", response.Id)

	return nil
}

// Delete removes a survey response by ID (only allowed for draft status)
func (s *GormSurveyResponseStore) Delete(ctx context.Context, id uuid.UUID) error {
	logger := slogging.Get()

	// Check if response exists and is in draft status
	var response models.SurveyResponse
	if err := s.db.WithContext(ctx).First(&response, "id = ?", id.String()).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("survey response not found: %s", id)
		}
		return fmt.Errorf("failed to get response: %w", err)
	}

	if response.Status != string(Draft) {
		return fmt.Errorf("can only delete draft responses, current status: %s", response.Status)
	}

	// Delete in transaction (access entries have CASCADE delete)
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	// Delete access entries first
	if err := tx.Where("survey_response_id = ?", id.String()).Delete(&models.SurveyResponseAccess{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete access entries: %w", err)
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
			query = query.Where("status = ?", string(*filters.Status))
		}
		if filters.TemplateID != nil {
			query = query.Where("template_id = ?", filters.TemplateID.String())
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
func (s *GormSurveyResponseStore) ListByOwner(ctx context.Context, ownerInternalUUID string, limit, offset int, status *SurveyResponseStatus) ([]SurveyResponseListItem, int, error) {
	filters := &SurveyResponseFilters{
		OwnerID: &ownerInternalUUID,
		Status:  status,
	}
	return s.List(ctx, limit, offset, filters)
}

// Submit transitions a response from draft to submitted status
func (s *GormSurveyResponseStore) Submit(ctx context.Context, id uuid.UUID) error {
	logger := slogging.Get()

	var response models.SurveyResponse
	if err := s.db.WithContext(ctx).First(&response, "id = ?", id.String()).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("survey response not found: %s", id)
		}
		return fmt.Errorf("failed to get response: %w", err)
	}

	// Validate state transition
	if response.Status != string(Draft) && response.Status != string(NeedsRevision) {
		return fmt.Errorf("invalid state transition: can only submit from draft or needs_revision, current: %s", response.Status)
	}

	now := time.Now().UTC()
	result := s.db.WithContext(ctx).
		Model(&models.SurveyResponse{}).
		Where("id = ?", id.String()).
		Updates(map[string]interface{}{
			"status":       string(Submitted),
			"submitted_at": now,
			"modified_at":  now,
		})

	if result.Error != nil {
		logger.Error("Failed to submit survey response: id=%s, error=%v", id, result.Error)
		return fmt.Errorf("failed to submit survey response: %w", result.Error)
	}

	logger.Info("Survey response submitted: id=%s", id)

	return nil
}

// Approve transitions a response from submitted to ready_for_review status
func (s *GormSurveyResponseStore) Approve(ctx context.Context, id uuid.UUID, reviewerInternalUUID string) error {
	logger := slogging.Get()

	var response models.SurveyResponse
	if err := s.db.WithContext(ctx).First(&response, "id = ?", id.String()).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("survey response not found: %s", id)
		}
		return fmt.Errorf("failed to get response: %w", err)
	}

	// Validate state transition
	if response.Status != string(Submitted) {
		return fmt.Errorf("invalid state transition: can only approve from submitted, current: %s", response.Status)
	}

	now := time.Now().UTC()
	result := s.db.WithContext(ctx).
		Model(&models.SurveyResponse{}).
		Where("id = ?", id.String()).
		Updates(map[string]interface{}{
			"status":                    string(ReadyForReview),
			"reviewed_at":               now,
			"reviewed_by_internal_uuid": reviewerInternalUUID,
			"modified_at":               now,
		})

	if result.Error != nil {
		logger.Error("Failed to approve survey response: id=%s, error=%v", id, result.Error)
		return fmt.Errorf("failed to approve survey response: %w", result.Error)
	}

	logger.Info("Survey response approved: id=%s, reviewer=%s", id, reviewerInternalUUID)

	return nil
}

// Return transitions a response back to needs_revision status with notes
func (s *GormSurveyResponseStore) Return(ctx context.Context, id uuid.UUID, reviewerInternalUUID string, notes string) error {
	logger := slogging.Get()

	var response models.SurveyResponse
	if err := s.db.WithContext(ctx).First(&response, "id = ?", id.String()).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("survey response not found: %s", id)
		}
		return fmt.Errorf("failed to get response: %w", err)
	}

	// Validate state transition
	if response.Status != string(Submitted) && response.Status != string(ReadyForReview) {
		return fmt.Errorf("invalid state transition: can only return from submitted or ready_for_review, current: %s", response.Status)
	}

	now := time.Now().UTC()
	result := s.db.WithContext(ctx).
		Model(&models.SurveyResponse{}).
		Where("id = ?", id.String()).
		Updates(map[string]interface{}{
			"status":                    string(NeedsRevision),
			"revision_notes":            notes,
			"reviewed_at":               now,
			"reviewed_by_internal_uuid": reviewerInternalUUID,
			"modified_at":               now,
		})

	if result.Error != nil {
		logger.Error("Failed to return survey response: id=%s, error=%v", id, result.Error)
		return fmt.Errorf("failed to return survey response: %w", result.Error)
	}

	logger.Info("Survey response returned for revision: id=%s, reviewer=%s", id, reviewerInternalUUID)

	return nil
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
		if result.Error == gorm.ErrRecordNotFound {
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
		if result.Error == gorm.ErrRecordNotFound {
			return "", fmt.Errorf("group not found: %s@%s", groupName, p)
		}
		return "", result.Error
	}
	return group.InternalUUID, nil
}

// apiToModel converts an API SurveyResponse to a database model
func (s *GormSurveyResponseStore) apiToModel(response *SurveyResponse, ownerInternalUUID string) (*models.SurveyResponse, error) {
	model := &models.SurveyResponse{
		TemplateID:        response.TemplateId.String(),
		OwnerInternalUUID: ownerInternalUUID,
	}

	if response.Id != nil {
		model.ID = response.Id.String()
	}

	if response.TemplateVersion != nil {
		model.TemplateVersion = *response.TemplateVersion
	}

	if response.Status != nil {
		model.Status = string(*response.Status)
	} else {
		model.Status = string(Draft)
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

	templateID, err := uuid.Parse(model.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("invalid template ID: %w", err)
	}

	response := &SurveyResponse{
		Id:              &id,
		TemplateId:      templateID,
		TemplateVersion: &model.TemplateVersion,
		CreatedAt:       &model.CreatedAt,
		ModifiedAt:      &model.ModifiedAt,
		SubmittedAt:     model.SubmittedAt,
		ReviewedAt:      model.ReviewedAt,
		RevisionNotes:   model.RevisionNotes,
	}

	// Convert status
	status := SurveyResponseStatus(model.Status)
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
	if model.Owner.InternalUUID != "" {
		response.Owner = s.userModelToAPI(&model.Owner)
	}

	// Convert reviewed_by
	if model.ReviewedBy != nil && model.ReviewedBy.InternalUUID != "" {
		response.ReviewedBy = s.userModelToAPI(model.ReviewedBy)
	}

	return response, nil
}

// modelToListItem converts a database model to an API SurveyResponseListItem
func (s *GormSurveyResponseStore) modelToListItem(model *models.SurveyResponse) SurveyResponseListItem {
	id, _ := uuid.Parse(model.ID)
	templateID, _ := uuid.Parse(model.TemplateID)

	item := SurveyResponseListItem{
		Id:              id,
		TemplateId:      templateID,
		TemplateVersion: &model.TemplateVersion,
		Status:          SurveyResponseStatus(model.Status),
		CreatedAt:       model.CreatedAt,
		SubmittedAt:     model.SubmittedAt,
	}

	// Get template name (pointer field)
	if model.Template.Name != "" {
		item.TemplateName = &model.Template.Name
	}

	// Convert owner (required field)
	if model.Owner.InternalUUID != "" {
		ownerPtr := s.userModelToAPI(&model.Owner)
		if ownerPtr != nil {
			item.Owner = *ownerPtr
		}
	}

	return item
}

// userModelToAPI converts a database User model to an API User
func (s *GormSurveyResponseStore) userModelToAPI(model *models.User) *User {
	email := types.Email(model.Email)
	return &User{
		PrincipalType: UserPrincipalType(AuthorizationPrincipalTypeUser),
		Provider:      model.Provider,
		ProviderId:    model.Email,
		DisplayName:   model.Name,
		Email:         email,
	}
}
