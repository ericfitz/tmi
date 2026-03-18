package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormThreatModelStore handles threat model database operations using GORM
type GormThreatModelStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

// NewGormThreatModelStore creates a new threat model GORM store
func NewGormThreatModelStore(database *gorm.DB) *GormThreatModelStore {
	return &GormThreatModelStore{
		db: database,
	}
}

// GetDB returns the underlying GORM database connection
func (s *GormThreatModelStore) GetDB() *gorm.DB {
	return s.db
}

// resolveUserIdentifierToUUID attempts to resolve a user identifier to an internal_uuid using GORM
func (s *GormThreatModelStore) resolveUserIdentifierToUUID(tx *gorm.DB, identifier string) (string, error) {
	var user models.User

	// Use map-based queries for cross-database compatibility (Oracle requires quoted lowercase column names)
	// Step 1: Check if it's already a valid internal_uuid
	if _, err := uuid.Parse(identifier); err == nil {
		result := tx.Where(map[string]any{"internal_uuid": identifier}).First(&user)
		if result.Error == nil {
			return user.InternalUUID, nil
		}
	}

	// Step 2: Try as provider_user_id
	result := tx.Where(map[string]any{"provider_user_id": identifier}).First(&user)
	if result.Error == nil {
		return user.InternalUUID, nil
	}

	// Step 3: Try as email
	result = tx.Where(map[string]any{"email": identifier}).First(&user)
	if result.Error == nil {
		return user.InternalUUID, nil
	}

	return "", fmt.Errorf("user not found with identifier: %s", identifier)
}

// resolveGroupToUUID attempts to resolve a group identifier to an internal_uuid using GORM
func (s *GormThreatModelStore) resolveGroupToUUID(tx *gorm.DB, groupName string, idp *string) (string, error) {
	provider := "*"
	if idp != nil && *idp != "" {
		provider = *idp
	}

	var group models.Group
	// Use struct-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
	result := tx.Where(&models.Group{Provider: provider, GroupName: groupName}).First(&group)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("group not found: %s@%s", groupName, provider)
		}
		return "", result.Error
	}

	return group.InternalUUID, nil
}

// ensureGroupExists creates a group entry if it doesn't exist and returns its internal_uuid using GORM
func (s *GormThreatModelStore) ensureGroupExists(tx *gorm.DB, groupName string, idp *string) (string, error) {
	provider := "*"
	if idp != nil && *idp != "" {
		provider = *idp
	}

	group := models.Group{
		Provider:   provider,
		GroupName:  groupName,
		Name:       &groupName,
		UsageCount: 1,
	}

	// Upsert: insert or update on conflict
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "provider"}, {Name: "group_name"}},
		DoUpdates: clause.Assignments(map[string]any{"last_used": time.Now().UTC(), "usage_count": gorm.Expr("usage_count + 1")}),
	}).Create(&group)

	if result.Error != nil {
		return "", fmt.Errorf("failed to ensure group exists: %w", result.Error)
	}

	// If the group was updated (not created), we need to fetch its UUID
	if group.InternalUUID == "" {
		var existingGroup models.Group
		// Use struct-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
		if err := tx.Where(&models.Group{Provider: provider, GroupName: groupName}).First(&existingGroup).Error; err != nil {
			return "", fmt.Errorf("failed to fetch group after upsert: %w", err)
		}
		return existingGroup.InternalUUID, nil
	}

	return group.InternalUUID, nil
}

// Get retrieves a threat model by ID using GORM
func (s *GormThreatModelStore) Get(id string) (ThreatModel, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("GormThreatModelStore.Get() called id=%s", id)

	// Validate UUID format
	if _, err := uuid.Parse(id); err != nil {
		logger.Error("Invalid UUID format id=%s error=%v", id, err)
		return ThreatModel{}, fmt.Errorf("invalid UUID format: %w", err)
	}

	var tm models.ThreatModel
	result := s.db.Preload("Owner").Preload("CreatedBy").Preload("SecurityReviewer").First(&tm, "id = ? AND deleted_at IS NULL", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ThreatModel{}, fmt.Errorf("threat model with ID %s not found", id)
		}
		return ThreatModel{}, fmt.Errorf("failed to get threat model: %w", result.Error)
	}

	// Convert GORM model to API model
	return s.convertToAPIModel(&tm)
}

// convertToAPIModel converts a GORM ThreatModel to the API ThreatModel
func (s *GormThreatModelStore) convertToAPIModel(tm *models.ThreatModel) (ThreatModel, error) {
	tmUUID, _ := uuid.Parse(tm.ID)

	// Create owner User
	owner := User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      tm.Owner.Provider,
		ProviderId:    strFromPtr(tm.Owner.ProviderUserID),
		DisplayName:   tm.Owner.Name,
		Email:         openapi_types.Email(tm.Owner.Email),
	}

	// Create created_by User
	var createdBy *User
	if tm.CreatedByInternalUUID != "" {
		createdBy = &User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      tm.CreatedBy.Provider,
			ProviderId:    strFromPtr(tm.CreatedBy.ProviderUserID),
			DisplayName:   tm.CreatedBy.Name,
			Email:         openapi_types.Email(tm.CreatedBy.Email),
		}
	}

	// Create security_reviewer User (if assigned)
	var securityReviewer *User
	if tm.SecurityReviewerInternalUUID != nil && *tm.SecurityReviewerInternalUUID != "" && tm.SecurityReviewer != nil {
		securityReviewer = &User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      tm.SecurityReviewer.Provider,
			ProviderId:    strFromPtr(tm.SecurityReviewer.ProviderUserID),
			DisplayName:   tm.SecurityReviewer.Name,
			Email:         openapi_types.Email(tm.SecurityReviewer.Email),
		}
	}

	// Load authorization
	authorization, err := s.loadAuthorization(tm.ID)
	if err != nil {
		return ThreatModel{}, fmt.Errorf("failed to load authorization: %w", err)
	}

	// Load metadata
	metadata, err := s.loadMetadata(tm.ID)
	if err != nil {
		return ThreatModel{}, fmt.Errorf("failed to load metadata: %w", err)
	}

	// Load threats
	threats, err := s.loadThreats(tm.ID)
	if err != nil {
		return ThreatModel{}, fmt.Errorf("failed to load threats: %w", err)
	}

	// Load diagrams dynamically from DiagramStore
	diagrams, err := s.loadDiagramsDynamically(tm.ID)
	if err != nil {
		return ThreatModel{}, fmt.Errorf("failed to load diagrams: %w", err)
	}

	// Set default framework
	framework := tm.ThreatModelFramework
	if framework == "" {
		framework = DefaultThreatModelFramework
	}

	// Convert alias array
	var alias *[]string
	if len(tm.Alias) > 0 {
		aliasSlice := []string(tm.Alias)
		alias = &aliasSlice
	}

	isConfidential := bool(tm.IsConfidential)

	// Convert project_id if present
	var projectID *openapi_types.UUID
	if tm.ProjectID != nil && *tm.ProjectID != "" {
		pid, err := uuid.Parse(*tm.ProjectID)
		if err == nil {
			projectID = &pid
		}
	}

	return ThreatModel{
		Id:                   &tmUUID,
		Name:                 tm.Name,
		Description:          tm.Description,
		Owner:                owner,
		CreatedBy:            createdBy,
		SecurityReviewer:     securityReviewer,
		ThreatModelFramework: framework,
		IssueUri:             tm.IssueURI,
		IsConfidential:       &isConfidential,
		Status:               tm.Status,
		StatusUpdated:        tm.StatusUpdated,
		CreatedAt:            &tm.CreatedAt,
		ModifiedAt:           &tm.ModifiedAt,
		Authorization:        authorization,
		Metadata:             &metadata,
		Threats:              &threats,
		Diagrams:             diagrams,
		Alias:                alias,
		ProjectId:            projectID,
	}, nil
}

// List returns filtered and paginated threat models using GORM
func (s *GormThreatModelStore) List(offset, limit int, filter func(ThreatModel) bool) []ThreatModel {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var results []ThreatModel

	var tmModels []models.ThreatModel
	result := s.db.Where("deleted_at IS NULL").Preload("Owner").Preload("CreatedBy").Preload("SecurityReviewer").Order("created_at DESC").Find(&tmModels)
	if result.Error != nil {
		return results
	}

	for _, tm := range tmModels {
		apiTM, err := s.convertToAPIModel(&tm)
		if err != nil {
			continue
		}

		// Apply filter if provided
		if filter == nil || filter(apiTM) {
			results = append(results, apiTM)
		}
	}

	// Apply pagination
	if offset >= len(results) {
		return []ThreatModel{}
	}

	end := offset + limit
	if end > len(results) || limit <= 0 {
		end = len(results)
	}

	return results[offset:end]
}

// ListWithCounts returns filtered and paginated threat models with count information using GORM
// Returns the paginated slice and the total count (before pagination)
func (s *GormThreatModelStore) ListWithCounts(offset, limit int, filter func(ThreatModel) bool, filters *ThreatModelFilters) ([]ThreatModelWithCounts, int) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var results []ThreatModelWithCounts

	// Build query with database-level filters
	query := s.db.Model(&models.ThreatModel{}).Where("threat_models.deleted_at IS NULL")

	// Check if include_deleted is requested via filters
	if filters != nil && filters.IncludeDeleted {
		query = s.db.Model(&models.ThreatModel{})
	}

	// Apply database-level filters if provided
	if filters != nil {
		if filters.Name != nil && *filters.Name != "" {
			query = query.Where("LOWER(threat_models.name) LIKE LOWER(?)", "%"+*filters.Name+"%")
		}
		if filters.Description != nil && *filters.Description != "" {
			query = query.Where("LOWER(threat_models.description) LIKE LOWER(?)", "%"+*filters.Description+"%")
		}
		if filters.IssueUri != nil && *filters.IssueUri != "" {
			query = query.Where("LOWER(threat_models.issue_uri) LIKE LOWER(?)", "%"+*filters.IssueUri+"%")
		}
		if filters.Owner != nil && *filters.Owner != "" {
			// Join with users table to filter by owner email or display name
			query = query.Joins("LEFT JOIN users AS owner_filter ON threat_models.owner_internal_uuid = owner_filter.internal_uuid").
				Where("LOWER(owner_filter.email) LIKE LOWER(?) OR LOWER(owner_filter.name) LIKE LOWER(?)",
					"%"+*filters.Owner+"%", "%"+*filters.Owner+"%")
		}
		if filters.CreatedAfter != nil {
			query = query.Where("threat_models.created_at >= ?", *filters.CreatedAfter)
		}
		if filters.CreatedBefore != nil {
			query = query.Where("threat_models.created_at <= ?", *filters.CreatedBefore)
		}
		if filters.ModifiedAfter != nil {
			query = query.Where("threat_models.modified_at >= ?", *filters.ModifiedAfter)
		}
		if filters.ModifiedBefore != nil {
			query = query.Where("threat_models.modified_at <= ?", *filters.ModifiedBefore)
		}
		if len(filters.Status) > 0 {
			lowered := make([]string, len(filters.Status))
			for i, s := range filters.Status {
				lowered[i] = strings.ToLower(s)
			}
			query = query.Where("LOWER(threat_models.status) IN ?", lowered)
		}
		if filters.StatusUpdatedAfter != nil {
			query = query.Where("threat_models.status_updated >= ?", *filters.StatusUpdatedAfter)
		}
		if filters.StatusUpdatedBefore != nil {
			query = query.Where("threat_models.status_updated <= ?", *filters.StatusUpdatedBefore)
		}
	}

	var tmModels []models.ThreatModel
	result := query.Preload("Owner").Preload("CreatedBy").Preload("SecurityReviewer").Order("threat_models.created_at DESC").Find(&tmModels)
	if result.Error != nil {
		return results, 0
	}

	for _, tm := range tmModels {
		apiTM, err := s.convertToAPIModel(&tm)
		if err != nil {
			continue
		}

		// Apply authorization filter if provided (this is still done in-memory for access control)
		if filter == nil || filter(apiTM) {
			results = append(results, ThreatModelWithCounts{
				ThreatModel:   apiTM,
				DocumentCount: s.calculateCount("documents", tm.ID),
				SourceCount:   s.calculateCount("repositories", tm.ID),
				DiagramCount:  s.calculateCount("diagrams", tm.ID),
				ThreatCount:   s.calculateCount("threats", tm.ID),
				NoteCount:     s.calculateCount("notes", tm.ID),
				AssetCount:    s.calculateCount("assets", tm.ID),
			})
		}
	}

	// Store total count before pagination
	total := len(results)

	// Apply pagination
	if offset >= total {
		return []ThreatModelWithCounts{}, total
	}

	end := offset + limit
	if end > total || limit <= 0 {
		end = total
	}

	return results[offset:end], total
}

// calculateCount counts records in a table for a threat model using GORM
func (s *GormThreatModelStore) calculateCount(tableName, threatModelID string) int {
	var count int64
	s.db.Table(tableName).Where("threat_model_id = ? AND deleted_at IS NULL", threatModelID).Count(&count)
	return int(count)
}

// Create adds a new threat model using GORM
func (s *GormThreatModelStore) Create(item ThreatModel, idSetter func(ThreatModel, string) ThreatModel) (ThreatModel, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Begin transaction
	tx := s.db.Begin()
	if tx.Error != nil {
		return item, fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Generate ID if not set
	id := uuid.New().String()
	if idSetter != nil {
		item = idSetter(item, id)
	}

	// Resolve owner identifier to internal_uuid
	ownerUUID, err := s.resolveUserIdentifierToUUID(tx, item.Owner.ProviderId)
	if err != nil {
		tx.Rollback()
		return item, fmt.Errorf("failed to resolve owner identifier %s: %w", item.Owner.ProviderId, err)
	}

	// Resolve created_by identifier to internal_uuid
	createdByUUID, err := s.resolveUserIdentifierToUUID(tx, item.CreatedBy.ProviderId)
	if err != nil {
		tx.Rollback()
		return item, fmt.Errorf("failed to resolve created_by identifier %s: %w", item.CreatedBy.ProviderId, err)
	}

	// Resolve security_reviewer identifier to internal_uuid (if provided)
	var securityReviewerUUID *string
	if item.SecurityReviewer != nil && item.SecurityReviewer.ProviderId != "" {
		srUUID, err := s.resolveUserIdentifierToUUID(tx, item.SecurityReviewer.ProviderId)
		if err != nil {
			tx.Rollback()
			return item, fmt.Errorf("failed to resolve security_reviewer: %w", err)
		}
		securityReviewerUUID = &srUUID
	}

	// Get framework value
	framework := item.ThreatModelFramework
	if framework == "" {
		framework = DefaultThreatModelFramework
	}

	// Set status_updated if status is provided
	var statusUpdated *time.Time
	if item.Status != nil && len(*item.Status) > 0 {
		now := time.Now().UTC()
		statusUpdated = &now
	}

	// Convert alias array if provided
	var aliasArray models.StringArray
	if item.Alias != nil && len(*item.Alias) > 0 {
		aliasArray = models.StringArray(*item.Alias)
	}

	// Create GORM model
	isConfidential := models.DBBool(false)
	if item.IsConfidential != nil {
		isConfidential = models.DBBool(*item.IsConfidential)
	}

	// Convert project_id if present
	var projectID *string
	if item.ProjectId != nil {
		s := item.ProjectId.String()
		projectID = &s
	}

	tm := models.ThreatModel{
		ID:                           id,
		Name:                         item.Name,
		Description:                  item.Description,
		OwnerInternalUUID:            ownerUUID,
		CreatedByInternalUUID:        createdByUUID,
		SecurityReviewerInternalUUID: securityReviewerUUID,
		ThreatModelFramework:         framework,
		IssueURI:                     item.IssueUri,
		IsConfidential:               isConfidential,
		Status:                       item.Status,
		StatusUpdated:                statusUpdated,
		Alias:                        aliasArray,
		ProjectID:                    projectID,
	}

	// Set timestamps
	if item.CreatedAt != nil {
		tm.CreatedAt = *item.CreatedAt
	}
	if item.ModifiedAt != nil {
		tm.ModifiedAt = *item.ModifiedAt
	}

	// Insert threat model
	if err := tx.Create(&tm).Error; err != nil {
		tx.Rollback()
		return item, fmt.Errorf("failed to insert threat model: %w", err)
	}

	// Insert authorization entries
	if err := s.saveAuthorizationTx(tx, id, item.Authorization); err != nil {
		tx.Rollback()
		return item, fmt.Errorf("failed to save authorization: %w", err)
	}

	// Insert metadata if present
	if item.Metadata != nil && len(*item.Metadata) > 0 {
		if err := s.saveMetadataTx(tx, id, *item.Metadata); err != nil {
			tx.Rollback()
			return item, fmt.Errorf("failed to save metadata: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return item, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return item, nil
}

// Update modifies an existing threat model using GORM
func (s *GormThreatModelStore) Update(id string, item ThreatModel) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Begin transaction
	tx := s.db.Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Get current threat model
	var existingTM models.ThreatModel
	if err := tx.First(&existingTM, "id = ?", id).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("threat model with ID %s not found", id)
		}
		return fmt.Errorf("failed to get current threat model: %w", err)
	}

	// Check if status changed
	statusChanged := false
	switch {
	case item.Status == nil && existingTM.Status != nil:
		statusChanged = true
	case item.Status != nil && existingTM.Status == nil:
		statusChanged = true
	case item.Status != nil && existingTM.Status != nil && *item.Status != *existingTM.Status:
		statusChanged = true
	}

	// Set status_updated if status changed
	var statusUpdated *time.Time
	if statusChanged {
		now := time.Now().UTC()
		statusUpdated = &now
	}

	// Resolve owner identifier to internal_uuid
	ownerUUID, err := s.resolveUserIdentifierToUUID(tx, item.Owner.ProviderId)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to resolve owner identifier %s: %w", item.Owner.ProviderId, err)
	}

	// Resolve created_by identifier to internal_uuid
	var createdByUUID string
	if item.CreatedBy != nil {
		createdByUUID, err = s.resolveUserIdentifierToUUID(tx, item.CreatedBy.ProviderId)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to resolve created_by identifier %s: %w", item.CreatedBy.ProviderId, err)
		}
	} else {
		createdByUUID = ownerUUID
	}

	// Resolve security_reviewer identifier to internal_uuid (if provided)
	var securityReviewerUUID *string
	if item.SecurityReviewer != nil && item.SecurityReviewer.ProviderId != "" {
		srUUID, err := s.resolveUserIdentifierToUUID(tx, item.SecurityReviewer.ProviderId)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to resolve security_reviewer: %w", err)
		}
		securityReviewerUUID = &srUUID
	}

	// Get framework value
	framework := item.ThreatModelFramework
	if framework == "" {
		framework = DefaultThreatModelFramework
	}

	// Convert alias array if provided
	var aliasValue any
	if item.Alias != nil {
		aliasValue = models.StringArray(*item.Alias)
	}

	// Convert project_id for update
	var updateProjectID *string
	if item.ProjectId != nil {
		s := item.ProjectId.String()
		updateProjectID = &s
	}

	// Update threat model
	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	updates := map[string]any{
		"name":                            item.Name,
		"description":                     item.Description,
		"owner_internal_uuid":             ownerUUID,
		"created_by_internal_uuid":        createdByUUID,
		"security_reviewer_internal_uuid": securityReviewerUUID,
		"threat_model_framework":          framework,
		"issue_uri":                       item.IssueUri,
		"status":                          item.Status,
		"project_id":                      updateProjectID,
	}
	if statusUpdated != nil {
		updates["status_updated"] = statusUpdated
	}
	if aliasValue != nil {
		updates["alias"] = aliasValue
	}

	result := tx.Model(&models.ThreatModel{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update threat model: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		tx.Rollback()
		return fmt.Errorf("threat model with ID %s not found", id)
	}

	// Update authorization
	if err := s.updateAuthorizationTx(tx, id, item.Authorization); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update authorization: %w", err)
	}

	// Update metadata
	if item.Metadata != nil {
		if err := s.updateMetadataTx(tx, id, *item.Metadata); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update metadata: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Delete soft-deletes a threat model and all its children.
// Use HardDelete for permanent removal (e.g., tombstone cleanup).
func (s *GormThreatModelStore) Delete(id string) error {
	return s.SoftDelete(id)
}

// Count returns the total number of threat models using GORM
func (s *GormThreatModelStore) Count() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var count int64
	s.db.Model(&models.ThreatModel{}).Count(&count)
	return int(count)
}

// loadAuthorization loads authorization entries for a threat model using GORM
// Note: Using batch lookups instead of Preload for Oracle compatibility
func (s *GormThreatModelStore) loadAuthorization(threatModelID string) ([]Authorization, error) {
	logger := slogging.Get()
	var accessEntries []models.ThreatModelAccess
	result := s.db.Where("threat_model_id = ?", threatModelID).
		Order("role DESC").
		Find(&accessEntries)
	if result.Error != nil {
		return nil, result.Error
	}

	logger.Debug("[GORM-STORE] loadAuthorization: Found %d access entries for threat model %s", len(accessEntries), threatModelID)

	// Collect unique UUIDs for batch resolution
	userUUIDSet := make(map[string]bool)
	groupUUIDSet := make(map[string]bool)
	for _, entry := range accessEntries {
		if entry.SubjectType == string(AddGroupMemberRequestSubjectTypeUser) && entry.UserInternalUUID != nil {
			userUUIDSet[*entry.UserInternalUUID] = true
		} else if entry.SubjectType == string(AddGroupMemberRequestSubjectTypeGroup) && entry.GroupInternalUUID != nil {
			groupUUIDSet[*entry.GroupInternalUUID] = true
		}
	}

	userUUIDs := make([]string, 0, len(userUUIDSet))
	for uuid := range userUUIDSet {
		userUUIDs = append(userUUIDs, uuid)
	}
	groupUUIDs := make([]string, 0, len(groupUUIDSet))
	for uuid := range groupUUIDSet {
		groupUUIDs = append(groupUUIDs, uuid)
	}

	userMap, groupMap := s.resolveUsersAndGroupsBatch(userUUIDs, groupUUIDs)

	// Build authorization entries from maps
	authorization := []Authorization{}
	for i, entry := range accessEntries {
		role := AuthorizationRole(entry.Role)
		logger.Debug("[GORM-STORE] loadAuthorization: Entry %d - SubjectType=%s, UserUUID=%v, GroupUUID=%v, Role=%s",
			i, entry.SubjectType, entry.UserInternalUUID, entry.GroupInternalUUID, entry.Role)

		if entry.SubjectType == string(AddGroupMemberRequestSubjectTypeUser) && entry.UserInternalUUID != nil {
			if user, ok := userMap[*entry.UserInternalUUID]; ok {
				auth := Authorization{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      user.Provider,
					ProviderId:    strFromPtr(user.ProviderUserID),
					DisplayName:   &user.Name,
					Email:         (*openapi_types.Email)(&user.Email),
					Role:          role,
				}
				authorization = append(authorization, auth)
			}
		} else if entry.SubjectType == string(AddGroupMemberRequestSubjectTypeGroup) && entry.GroupInternalUUID != nil {
			if group, ok := groupMap[*entry.GroupInternalUUID]; ok {
				auth := Authorization{
					PrincipalType: AuthorizationPrincipalTypeGroup,
					Provider:      group.Provider,
					ProviderId:    group.GroupName,
					DisplayName:   group.Name,
					Role:          role,
				}
				authorization = append(authorization, auth)
			}
		}
	}

	return authorization, nil
}

// resolveUsersAndGroupsBatch loads users and groups by internal UUIDs in batch.
// Returns lookup maps keyed by internal_uuid. Oracle-compatible (chunks IN clauses at 999).
func (s *GormThreatModelStore) resolveUsersAndGroupsBatch(userUUIDs, groupUUIDs []string) (map[string]models.User, map[string]models.Group) {
	userMap := make(map[string]models.User, len(userUUIDs))
	groupMap := make(map[string]models.Group, len(groupUUIDs))

	if len(userUUIDs) > 0 {
		for _, chunk := range chunkStrings(userUUIDs, 999) {
			var users []models.User
			s.db.Where("internal_uuid IN ?", chunk).Find(&users)
			for _, u := range users {
				userMap[u.InternalUUID] = u
			}
		}
	}

	if len(groupUUIDs) > 0 {
		for _, chunk := range chunkStrings(groupUUIDs, 999) {
			var groups []models.Group
			s.db.Where("internal_uuid IN ?", chunk).Find(&groups)
			for _, g := range groups {
				groupMap[g.InternalUUID] = g
			}
		}
	}

	return userMap, groupMap
}

// chunkStrings splits a slice into chunks of at most size n.
func chunkStrings(s []string, n int) [][]string {
	if len(s) <= n {
		return [][]string{s}
	}
	var chunks [][]string
	for i := 0; i < len(s); i += n {
		end := i + n
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}

// loadMetadata loads metadata for a threat model using GORM
func (s *GormThreatModelStore) loadMetadata(threatModelID string) ([]Metadata, error) {
	return loadEntityMetadata(s.db, "threat_model", threatModelID)
}

// loadThreats loads threats for a threat model using GORM
func (s *GormThreatModelStore) loadThreats(threatModelID string) ([]Threat, error) {
	var threatModels []models.Threat
	result := s.db.Where("threat_model_id = ?", threatModelID).Find(&threatModels)
	if result.Error != nil {
		return nil, result.Error
	}

	// Initialize as empty slice
	threats := []Threat{}

	for _, tm := range threatModels {
		threatUUID, _ := uuid.Parse(tm.ID)
		threatModelUUID, _ := uuid.Parse(threatModelID)

		// Convert diagram_id, cell_id, and asset_id
		var diagramID, cellID, assetID *uuid.UUID
		if tm.DiagramID != nil {
			if id, err := uuid.Parse(*tm.DiagramID); err == nil {
				diagramID = &id
			}
		}
		if tm.CellID != nil {
			if id, err := uuid.Parse(*tm.CellID); err == nil {
				cellID = &id
			}
		}
		if tm.AssetID != nil {
			if id, err := uuid.Parse(*tm.AssetID); err == nil {
				assetID = &id
			}
		}

		// Convert score from float64 to float32
		var scoreFloat32 *float32
		if tm.Score != nil {
			score32 := float32(*tm.Score)
			scoreFloat32 = &score32
		}

		// Load threat metadata
		threatMetadata, _ := s.loadThreatMetadata(tm.ID)
		metadata := &threatMetadata

		// Convert mitigated from OracleBool to *bool
		mitigatedBool := tm.Mitigated.Bool()
		mitigated := &mitigatedBool

		threats = append(threats, Threat{
			Id:            &threatUUID,
			Name:          tm.Name,
			Description:   tm.Description,
			Severity:      tm.Severity,
			Mitigation:    tm.Mitigation,
			DiagramId:     diagramID,
			CellId:        cellID,
			AssetId:       assetID,
			Priority:      tm.Priority,
			Mitigated:     mitigated,
			Status:        tm.Status,
			ThreatType:    []string(tm.ThreatType),
			Score:         scoreFloat32,
			IssueUri:      tm.IssueURI,
			Metadata:      metadata,
			CreatedAt:     &tm.CreatedAt,
			ModifiedAt:    &tm.ModifiedAt,
			ThreatModelId: &threatModelUUID,
		})
	}

	return threats, nil
}

// loadThreatMetadata loads metadata for a threat using GORM
func (s *GormThreatModelStore) loadThreatMetadata(threatID string) ([]Metadata, error) {
	var metadataEntries []models.Metadata
	result := s.db.Where("entity_type = ? AND entity_id = ?", "threat", threatID).Order("key ASC").Find(&metadataEntries)
	if result.Error != nil {
		return nil, result.Error
	}

	var metadata []Metadata
	for _, entry := range metadataEntries {
		metadata = append(metadata, Metadata{
			Key:   entry.Key,
			Value: entry.Value,
		})
	}

	return metadata, nil
}

// loadDiagramsDynamically loads diagrams using the DiagramStore for single source of truth
func (s *GormThreatModelStore) loadDiagramsDynamically(threatModelID string) (*[]Diagram, error) {
	var diagramIDs []string
	result := s.db.Model(&models.Diagram{}).
		Where("threat_model_id = ?", threatModelID).
		Order("created_at").
		Pluck("id", &diagramIDs)
	if result.Error != nil {
		return nil, result.Error
	}

	if len(diagramIDs) == 0 {
		emptySlice := []Diagram{}
		return &emptySlice, nil
	}

	// Load each diagram from the DiagramStore
	diagrams := []Diagram{}
	for _, diagramID := range diagramIDs {
		diagram, err := DiagramStore.Get(diagramID)
		if err != nil {
			continue
		}

		// Ensure backward compatibility
		if diagram.Image == nil {
			diagram.Image = &struct {
				Svg          *[]byte `json:"svg,omitempty"`
				UpdateVector *int64  `json:"update_vector,omitempty"`
			}{}
		}

		var diagramUnion Diagram
		if err := diagramUnion.FromDfdDiagram(diagram); err != nil {
			continue
		}
		diagrams = append(diagrams, diagramUnion)
	}

	return &diagrams, nil
}

// saveAuthorizationTx saves authorization entries within a transaction using GORM
func (s *GormThreatModelStore) saveAuthorizationTx(tx *gorm.DB, threatModelID string, authorization []Authorization) error {
	logger := slogging.Get()
	logger.Debug("[GORM-STORE] saveAuthorizationTx: Called with %d entries for threat model %s", len(authorization), threatModelID)

	if len(authorization) == 0 {
		return nil
	}

	for _, auth := range authorization {
		subjectTypeStr := string(AddGroupMemberRequestSubjectTypeUser)
		if auth.PrincipalType == AuthorizationPrincipalTypeGroup {
			subjectTypeStr = string(AddGroupMemberRequestSubjectTypeGroup)
		}

		var userUUID, groupUUID *string

		switch subjectTypeStr {
		case string(AddGroupMemberRequestSubjectTypeUser):
			identifier := auth.ProviderId
			if identifier == "" && auth.Email != nil {
				identifier = string(*auth.Email)
			}

			resolvedUUID, err := s.resolveUserIdentifierToUUID(tx, identifier)
			if err != nil {
				logger.Debug("Could not resolve user identifier %s to internal_uuid: %v", identifier, err)
				userUUID = &identifier
			} else {
				userUUID = &resolvedUUID
			}
		case string(AddGroupMemberRequestSubjectTypeGroup):
			if auth.ProviderId == EveryonePseudoGroup {
				everyoneUUID := EveryonePseudoGroupUUID
				groupUUID = &everyoneUUID
			} else {
				resolvedUUID, err := s.resolveGroupToUUID(tx, auth.ProviderId, &auth.Provider)
				if err != nil {
					logger.Debug("Could not resolve group identifier %s to internal_uuid: %v", auth.ProviderId, err)
					newGroupUUID, err := s.ensureGroupExists(tx, auth.ProviderId, &auth.Provider)
					if err != nil {
						return fmt.Errorf("failed to ensure group exists: %w", err)
					}
					groupUUID = &newGroupUUID
				} else {
					groupUUID = &resolvedUUID
				}
			}
		}

		// Create access entry
		access := models.ThreatModelAccess{
			ThreatModelID:     threatModelID,
			UserInternalUUID:  userUUID,
			GroupInternalUUID: groupUUID,
			SubjectType:       subjectTypeStr,
			Role:              string(auth.Role),
		}

		// For Oracle compatibility, use simple Create
		// The BeforeCreate hook will generate a new ID
		// If we need upsert behavior, we should delete existing entries first
		// (which is done in updateAuthorizationTx before calling this function)
		result := tx.Create(&access)

		if result.Error != nil {
			logger.Error("[GORM-STORE] saveAuthorizationTx: Failed to insert authorization entry: %v", result.Error)
			return result.Error
		}
	}

	logger.Debug("[GORM-STORE] saveAuthorizationTx: Successfully saved all %d entries", len(authorization))
	return nil
}

// saveMetadataTx saves metadata entries within a transaction using GORM
func (s *GormThreatModelStore) saveMetadataTx(tx *gorm.DB, threatModelID string, metadata []Metadata) error {
	return saveEntityMetadata(tx, "threat_model", threatModelID, metadata)
}

// updateAuthorizationTx updates authorization entries within a transaction using GORM
func (s *GormThreatModelStore) updateAuthorizationTx(tx *gorm.DB, threatModelID string, authorization []Authorization) error {
	logger := slogging.Get()
	logger.Debug("[GORM-STORE] updateAuthorizationTx: Deleting existing authorization for threat model %s", threatModelID)

	// Delete existing authorization
	result := tx.Where("threat_model_id = ?", threatModelID).Delete(&models.ThreatModelAccess{})
	if result.Error != nil {
		return result.Error
	}
	logger.Debug("[GORM-STORE] updateAuthorizationTx: Deleted %d existing entries", result.RowsAffected)

	// Insert new authorization
	logger.Debug("[GORM-STORE] updateAuthorizationTx: Inserting %d new entries", len(authorization))
	return s.saveAuthorizationTx(tx, threatModelID, authorization)
}

// updateMetadataTx updates metadata entries within a transaction using GORM
func (s *GormThreatModelStore) updateMetadataTx(tx *gorm.DB, threatModelID string, metadata []Metadata) error {
	return deleteAndSaveEntityMetadata(tx, "threat_model", threatModelID, metadata)
}

// GormDiagramStore handles diagram database operations using GORM
type GormDiagramStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

// NewGormDiagramStore creates a new diagram GORM store
func NewGormDiagramStore(database *gorm.DB) *GormDiagramStore {
	return &GormDiagramStore{
		db: database,
	}
}

// Get retrieves a diagram by ID using GORM
func (s *GormDiagramStore) Get(id string) (DfdDiagram, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var diagram models.Diagram
	result := s.db.First(&diagram, "id = ? AND deleted_at IS NULL", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return DfdDiagram{}, fmt.Errorf("diagram with ID %s not found", id)
		}
		return DfdDiagram{}, fmt.Errorf("failed to get diagram: %w", result.Error)
	}

	return s.convertToAPIDiagram(&diagram)
}

// GetThreatModelID returns the threat model ID for a given diagram
func (s *GormDiagramStore) GetThreatModelID(diagramID string) (string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var diagram models.Diagram
	result := s.db.Select("threat_model_id").First(&diagram, "id = ? AND deleted_at IS NULL", diagramID)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("diagram with ID %s not found", diagramID)
		}
		return "", fmt.Errorf("failed to get diagram: %w", result.Error)
	}

	return diagram.ThreatModelID, nil
}

// convertToAPIDiagram converts a GORM Diagram to the API DfdDiagram
func (s *GormDiagramStore) convertToAPIDiagram(diagram *models.Diagram) (DfdDiagram, error) {
	diagramUUID, _ := uuid.Parse(diagram.ID)

	// Parse cells JSON
	var cells []DfdDiagram_Cells_Item
	if diagram.Cells != nil {
		if err := json.Unmarshal(diagram.Cells, &cells); err != nil {
			return DfdDiagram{}, fmt.Errorf("failed to parse cells JSON: %w", err)
		}
	}

	// Parse color palette JSON
	var colorPalette *[]ColorPaletteEntry
	if diagram.ColorPalette != nil {
		var cp []ColorPaletteEntry
		if err := json.Unmarshal(diagram.ColorPalette, &cp); err != nil {
			return DfdDiagram{}, fmt.Errorf("failed to parse color_palette JSON: %w", err)
		}
		if len(cp) > 0 {
			colorPalette = &cp
		}
	}

	// Load diagram metadata
	metadata, err := s.loadMetadata("diagram", diagram.ID)
	if err != nil {
		return DfdDiagram{}, fmt.Errorf("failed to load diagram metadata: %w", err)
	}

	// Convert type to enum
	diagType := DfdDiagramTypeDFD100
	if diagram.Type != nil && *diagram.Type != "" {
		diagType = DfdDiagramType(*diagram.Type)
	}

	// Handle image
	var imagePtr *struct {
		Svg          *[]byte `json:"svg,omitempty"`
		UpdateVector *int64  `json:"update_vector,omitempty"`
	}
	if diagram.SVGImage.Valid {
		svgBytes := []byte(diagram.SVGImage.String)
		imagePtr = &struct {
			Svg          *[]byte `json:"svg,omitempty"`
			UpdateVector *int64  `json:"update_vector,omitempty"`
		}{
			Svg:          &svgBytes,
			UpdateVector: diagram.ImageUpdateVector,
		}
	}

	includeInReport := diagram.IncludeInReport.Bool()
	timmyEnabled := diagram.TimmyEnabled.Bool()

	return DfdDiagram{
		Id:              &diagramUUID,
		Name:            diagram.Name,
		Description:     diagram.Description,
		Type:            diagType,
		Cells:           cells,
		ColorPalette:    colorPalette,
		Metadata:        &metadata,
		Image:           imagePtr,
		UpdateVector:    &diagram.UpdateVector,
		IncludeInReport: &includeInReport,
		TimmyEnabled:    &timmyEnabled,
		CreatedAt:       &diagram.CreatedAt,
		ModifiedAt:      &diagram.ModifiedAt,
	}, nil
}

// List returns all diagrams (not used in current implementation)
func (s *GormDiagramStore) List(offset, limit int, filter func(DfdDiagram) bool) []DfdDiagram {
	return []DfdDiagram{}
}

// CreateWithThreatModel adds a new diagram with a specific threat model ID using GORM
func (s *GormDiagramStore) CreateWithThreatModel(item DfdDiagram, threatModelID string, idSetter func(DfdDiagram, string) DfdDiagram) (DfdDiagram, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Generate ID if not set
	id := uuid.New().String()
	if idSetter != nil {
		item = idSetter(item, id)
	}

	// Serialize cells to JSON
	cellsJSON, err := json.Marshal(item.Cells)
	if err != nil {
		return item, fmt.Errorf("failed to marshal cells: %w", err)
	}

	// Serialize color palette to JSON
	var colorPaletteJSON models.JSONRaw
	if item.ColorPalette != nil && len(*item.ColorPalette) > 0 {
		cpJSON, err := json.Marshal(item.ColorPalette)
		if err != nil {
			return item, fmt.Errorf("failed to marshal color_palette: %w", err)
		}
		colorPaletteJSON = models.JSONRaw(cpJSON)
	}

	// Handle image
	var svgImage *string
	var imageUpdateVector *int64
	if item.Image != nil && item.Image.Svg != nil {
		svgStr := string(*item.Image.Svg)
		svgImage = &svgStr
		imageUpdateVector = item.Image.UpdateVector
	}

	// Get update_vector
	updateVector := int64(0)
	if item.UpdateVector != nil {
		updateVector = *item.UpdateVector
	}

	// Get diagram type
	var diagType *string
	if item.Type != "" {
		t := string(item.Type)
		diagType = &t
	}

	diagram := models.Diagram{
		ID:                id,
		ThreatModelID:     threatModelID,
		Name:              item.Name,
		Description:       item.Description,
		Type:              diagType,
		Cells:             models.JSONRaw(cellsJSON),
		ColorPalette:      colorPaletteJSON,
		SVGImage:          models.NewNullableDBText(svgImage),
		ImageUpdateVector: imageUpdateVector,
		UpdateVector:      updateVector,
	}
	if item.IncludeInReport != nil {
		diagram.IncludeInReport = models.DBBool(*item.IncludeInReport)
	}
	if item.TimmyEnabled != nil {
		diagram.TimmyEnabled = models.DBBool(*item.TimmyEnabled)
	}

	// Set timestamps
	if item.CreatedAt != nil {
		diagram.CreatedAt = *item.CreatedAt
	}
	if item.ModifiedAt != nil {
		diagram.ModifiedAt = *item.ModifiedAt
	}

	// Insert diagram
	if err := s.db.Create(&diagram).Error; err != nil {
		return item, fmt.Errorf("failed to insert diagram: %w", err)
	}

	// Save metadata if present
	if item.Metadata != nil && len(*item.Metadata) > 0 {
		if err := s.saveMetadata(id, *item.Metadata); err != nil {
			return item, fmt.Errorf("failed to save diagram metadata: %w", err)
		}
	}

	return item, nil
}

// Create adds a new diagram using GORM (maintains backward compatibility)
func (s *GormDiagramStore) Create(item DfdDiagram, idSetter func(DfdDiagram, string) DfdDiagram) (DfdDiagram, error) {
	return s.CreateWithThreatModel(item, uuid.Nil.String(), idSetter)
}

// Update modifies an existing diagram using GORM
func (s *GormDiagramStore) Update(id string, item DfdDiagram) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Serialize cells to JSON
	cellsJSON, err := json.Marshal(item.Cells)
	if err != nil {
		return fmt.Errorf("failed to marshal cells: %w", err)
	}

	// Handle image
	var svgImage *string
	var imageUpdateVector *int64
	if item.Image != nil && item.Image.Svg != nil {
		svgStr := string(*item.Image.Svg)
		svgImage = &svgStr
		imageUpdateVector = item.Image.UpdateVector
	}

	// Get update_vector
	updateVector := int64(0)
	if item.UpdateVector != nil {
		updateVector = *item.UpdateVector
	}

	// Get diagram type
	var diagType *string
	if item.Type != "" {
		t := string(item.Type)
		diagType = &t
	}

	// Serialize color palette to JSON
	var colorPaletteJSON models.JSONRaw
	if item.ColorPalette != nil && len(*item.ColorPalette) > 0 {
		cpJSON, err := json.Marshal(item.ColorPalette)
		if err != nil {
			return fmt.Errorf("failed to marshal color_palette: %w", err)
		}
		colorPaletteJSON = models.JSONRaw(cpJSON)
	}

	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	updates := map[string]any{
		"name":                item.Name,
		"description":         item.Description,
		"type":                diagType,
		"cells":               models.JSONRaw(cellsJSON),
		"color_palette":       colorPaletteJSON,
		"svg_image":           svgImage,
		"image_update_vector": imageUpdateVector,
		"update_vector":       updateVector,
	}
	if item.IncludeInReport != nil {
		updates["include_in_report"] = models.DBBool(*item.IncludeInReport)
	}
	if item.TimmyEnabled != nil {
		updates["timmy_enabled"] = models.DBBool(*item.TimmyEnabled)
	}

	result := s.db.Model(&models.Diagram{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("failed to update diagram: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("diagram with ID %s not found", id)
	}

	// Update metadata if present
	if item.Metadata != nil {
		if err := s.updateMetadata(id, *item.Metadata); err != nil {
			return fmt.Errorf("failed to update diagram metadata: %w", err)
		}
	}

	return nil
}

// Delete removes a diagram using GORM.
// Uses a transaction to nullify diagram references on related threats before deleting,
// avoiding foreign key constraint violations from fk_threats_diagram.
// Delete soft-deletes a diagram.
// Use HardDelete for permanent removal (e.g., tombstone cleanup).
func (s *GormDiagramStore) Delete(id string) error {
	return s.SoftDelete(id)
}

// hardDeleteDiagram permanently removes a diagram with FK cleanup
func (s *GormDiagramStore) hardDeleteDiagram(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.db.Transaction(func(tx *gorm.DB) error {
		// 1. Nullify diagram_id and cell_id on threats referencing this diagram
		if err := tx.Model(&models.Threat{}).
			Where("diagram_id = ?", id).
			Updates(map[string]interface{}{
				"diagram_id": nil,
				"cell_id":    nil,
			}).Error; err != nil {
			return fmt.Errorf("failed to nullify diagram references on threats: %w", err)
		}

		// 2. Delete diagram metadata
		if err := tx.Where("entity_type = 'diagram' AND entity_id = ?", id).
			Delete(&models.Metadata{}).Error; err != nil {
			return fmt.Errorf("failed to delete diagram metadata: %w", err)
		}

		// 3. Delete the diagram
		result := tx.Delete(&models.Diagram{}, "id = ?", id)
		if result.Error != nil {
			return fmt.Errorf("failed to delete diagram: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("diagram with ID %s not found", id)
		}

		return nil
	})
}

// Count returns the total number of diagrams using GORM
func (s *GormDiagramStore) Count() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var count int64
	s.db.Model(&models.Diagram{}).Count(&count)
	return int(count)
}

// loadMetadata loads metadata for a diagram using GORM
func (s *GormDiagramStore) loadMetadata(entityType, entityID string) ([]Metadata, error) {
	return loadEntityMetadata(s.db, entityType, entityID)
}

// saveMetadata saves metadata for a diagram using GORM
func (s *GormDiagramStore) saveMetadata(diagramID string, metadata []Metadata) error {
	return saveEntityMetadata(s.db, "diagram", diagramID, metadata)
}

// updateMetadata updates metadata for a diagram using GORM
func (s *GormDiagramStore) updateMetadata(diagramID string, metadata []Metadata) error {
	return deleteAndSaveEntityMetadata(s.db, "diagram", diagramID, metadata)
}
