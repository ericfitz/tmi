package api

import (
	"encoding/json"
	"fmt"
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

	// Step 1: Check if it's already a valid internal_uuid
	if _, err := uuid.Parse(identifier); err == nil {
		result := tx.Where("internal_uuid = ?", identifier).First(&user)
		if result.Error == nil {
			return user.InternalUUID, nil
		}
	}

	// Step 2: Try as provider_user_id
	result := tx.Where("provider_user_id = ?", identifier).First(&user)
	if result.Error == nil {
		return user.InternalUUID, nil
	}

	// Step 3: Try as email
	result = tx.Where("email = ?", identifier).First(&user)
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
	result := tx.Where("provider = ? AND group_name = ?", provider, groupName).First(&group)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
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
		DoUpdates: clause.Assignments(map[string]interface{}{"last_used": time.Now().UTC(), "usage_count": gorm.Expr("usage_count + 1")}),
	}).Create(&group)

	if result.Error != nil {
		return "", fmt.Errorf("failed to ensure group exists: %w", result.Error)
	}

	// If the group was updated (not created), we need to fetch its UUID
	if group.InternalUUID == "" {
		var existingGroup models.Group
		if err := tx.Where("provider = ? AND group_name = ?", provider, groupName).First(&existingGroup).Error; err != nil {
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
	result := s.db.Preload("Owner").Preload("CreatedBy").First(&tm, "id = ?", id)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
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
		ProviderId:    derefString(tm.Owner.ProviderUserID),
		DisplayName:   tm.Owner.Name,
		Email:         openapi_types.Email(tm.Owner.Email),
	}

	// Create created_by User
	var createdBy *User
	if tm.CreatedByInternalUUID != "" {
		createdBy = &User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      tm.CreatedBy.Provider,
			ProviderId:    derefString(tm.CreatedBy.ProviderUserID),
			DisplayName:   tm.CreatedBy.Name,
			Email:         openapi_types.Email(tm.CreatedBy.Email),
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
		framework = "STRIDE"
	}

	return ThreatModel{
		Id:                   &tmUUID,
		Name:                 tm.Name,
		Description:          tm.Description,
		Owner:                owner,
		CreatedBy:            createdBy,
		ThreatModelFramework: framework,
		IssueUri:             tm.IssueURI,
		Status:               tm.Status,
		StatusUpdated:        tm.StatusUpdated,
		CreatedAt:            &tm.CreatedAt,
		ModifiedAt:           &tm.ModifiedAt,
		Authorization:        authorization,
		Metadata:             &metadata,
		Threats:              &threats,
		Diagrams:             diagrams,
	}, nil
}

// List returns filtered and paginated threat models using GORM
func (s *GormThreatModelStore) List(offset, limit int, filter func(ThreatModel) bool) []ThreatModel {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var results []ThreatModel

	var tmModels []models.ThreatModel
	result := s.db.Preload("Owner").Preload("CreatedBy").Order("created_at DESC").Find(&tmModels)
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
func (s *GormThreatModelStore) ListWithCounts(offset, limit int, filter func(ThreatModel) bool) []ThreatModelWithCounts {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var results []ThreatModelWithCounts

	var tmModels []models.ThreatModel
	result := s.db.Preload("Owner").Preload("CreatedBy").Order("created_at DESC").Find(&tmModels)
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

	// Apply pagination
	if offset >= len(results) {
		return []ThreatModelWithCounts{}
	}

	end := offset + limit
	if end > len(results) || limit <= 0 {
		end = len(results)
	}

	return results[offset:end]
}

// calculateCount counts records in a table for a threat model using GORM
func (s *GormThreatModelStore) calculateCount(tableName, threatModelID string) int {
	var count int64
	s.db.Table(tableName).Where("threat_model_id = ?", threatModelID).Count(&count)
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

	// Get framework value
	framework := item.ThreatModelFramework
	if framework == "" {
		framework = "STRIDE"
	}

	// Set status_updated if status is provided
	var statusUpdated *time.Time
	if item.Status != nil && len(*item.Status) > 0 {
		now := time.Now().UTC()
		statusUpdated = &now
	}

	// Create GORM model
	tm := models.ThreatModel{
		ID:                    id,
		Name:                  item.Name,
		Description:           item.Description,
		OwnerInternalUUID:     ownerUUID,
		CreatedByInternalUUID: createdByUUID,
		ThreatModelFramework:  framework,
		IssueURI:              item.IssueUri,
		Status:                item.Status,
		StatusUpdated:         statusUpdated,
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
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("threat model with ID %s not found", id)
		}
		return fmt.Errorf("failed to get current threat model: %w", err)
	}

	// Check if status changed
	statusChanged := false
	if item.Status == nil && existingTM.Status != nil {
		statusChanged = true
	} else if item.Status != nil && existingTM.Status == nil {
		statusChanged = true
	} else if item.Status != nil && existingTM.Status != nil && *item.Status != *existingTM.Status {
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

	// Get framework value
	framework := item.ThreatModelFramework
	if framework == "" {
		framework = "STRIDE"
	}

	// Update threat model
	updates := map[string]interface{}{
		"name":                     item.Name,
		"description":              item.Description,
		"owner_internal_uuid":      ownerUUID,
		"created_by_internal_uuid": createdByUUID,
		"threat_model_framework":   framework,
		"issue_uri":                item.IssueUri,
		"status":                   item.Status,
		"modified_at":              item.ModifiedAt,
	}
	if statusUpdated != nil {
		updates["status_updated"] = statusUpdated
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

// Delete removes a threat model using GORM
func (s *GormThreatModelStore) Delete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	result := s.db.Delete(&models.ThreatModel{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete threat model: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("threat model with ID %s not found", id)
	}

	return nil
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
func (s *GormThreatModelStore) loadAuthorization(threatModelID string) ([]Authorization, error) {
	var accessEntries []models.ThreatModelAccess
	result := s.db.Preload("User").Preload("Group").
		Where("threat_model_id = ?", threatModelID).
		Order("role DESC").
		Find(&accessEntries)
	if result.Error != nil {
		return nil, result.Error
	}

	// Initialize as empty slice
	authorization := []Authorization{}

	for _, entry := range accessEntries {
		role := AuthorizationRole(entry.Role)

		if entry.SubjectType == "user" && entry.User != nil {
			auth := Authorization{
				PrincipalType: AuthorizationPrincipalTypeUser,
				Provider:      entry.User.Provider,
				ProviderId:    derefString(entry.User.ProviderUserID),
				DisplayName:   &entry.User.Name,
				Email:         (*openapi_types.Email)(&entry.User.Email),
				Role:          role,
			}
			authorization = append(authorization, auth)
		} else if entry.SubjectType == "group" && entry.Group != nil {
			auth := Authorization{
				PrincipalType: AuthorizationPrincipalTypeGroup,
				Provider:      entry.Group.Provider,
				ProviderId:    entry.Group.GroupName,
				DisplayName:   entry.Group.Name,
				Role:          role,
			}
			authorization = append(authorization, auth)
		}
	}

	return authorization, nil
}

// loadMetadata loads metadata for a threat model using GORM
func (s *GormThreatModelStore) loadMetadata(threatModelID string) ([]Metadata, error) {
	var metadataEntries []models.Metadata
	result := s.db.Where("entity_type = ? AND entity_id = ?", "threat_model", threatModelID).Find(&metadataEntries)
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

		// Convert mitigated from bool to *bool
		mitigated := &tm.Mitigated

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
		subjectTypeStr := "user"
		if auth.PrincipalType == AuthorizationPrincipalTypeGroup {
			subjectTypeStr = "group"
		}

		var userUUID, groupUUID *string

		switch subjectTypeStr {
		case "user":
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
		case "group":
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

		// Upsert: insert or update on conflict
		var conflictColumns []clause.Column
		if subjectTypeStr == "user" {
			conflictColumns = []clause.Column{{Name: "threat_model_id"}, {Name: "user_internal_uuid"}, {Name: "subject_type"}}
		} else {
			conflictColumns = []clause.Column{{Name: "threat_model_id"}, {Name: "group_internal_uuid"}, {Name: "subject_type"}}
		}

		result := tx.Clauses(clause.OnConflict{
			Columns:   conflictColumns,
			DoUpdates: clause.AssignmentColumns([]string{"role", "modified_at"}),
		}).Create(&access)

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
	if len(metadata) == 0 {
		return nil
	}

	for _, meta := range metadata {
		entry := models.Metadata{
			ID:         uuid.New().String(),
			EntityType: "threat_model",
			EntityID:   threatModelID,
			Key:        meta.Key,
			Value:      meta.Value,
		}

		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "entity_type"}, {Name: "entity_id"}, {Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value", "modified_at"}),
		}).Create(&entry)

		if result.Error != nil {
			return result.Error
		}
	}

	return nil
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
	// Delete existing metadata
	result := tx.Where("entity_type = ? AND entity_id = ?", "threat_model", threatModelID).Delete(&models.Metadata{})
	if result.Error != nil {
		return result.Error
	}

	// Insert new metadata
	return s.saveMetadataTx(tx, threatModelID, metadata)
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
	result := s.db.First(&diagram, "id = ?", id)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return DfdDiagram{}, fmt.Errorf("diagram with ID %s not found", id)
		}
		return DfdDiagram{}, fmt.Errorf("failed to get diagram: %w", result.Error)
	}

	return s.convertToAPIDiagram(&diagram)
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
	if diagram.SVGImage != nil {
		svgBytes := []byte(*diagram.SVGImage)
		imagePtr = &struct {
			Svg          *[]byte `json:"svg,omitempty"`
			UpdateVector *int64  `json:"update_vector,omitempty"`
		}{
			Svg:          &svgBytes,
			UpdateVector: diagram.ImageUpdateVector,
		}
	}

	return DfdDiagram{
		Id:           &diagramUUID,
		Name:         diagram.Name,
		Description:  diagram.Description,
		Type:         diagType,
		Cells:        cells,
		Metadata:     &metadata,
		Image:        imagePtr,
		UpdateVector: &diagram.UpdateVector,
		CreatedAt:    &diagram.CreatedAt,
		ModifiedAt:   &diagram.ModifiedAt,
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
		Cells:             cellsJSON,
		SVGImage:          svgImage,
		ImageUpdateVector: imageUpdateVector,
		UpdateVector:      updateVector,
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

	updates := map[string]interface{}{
		"name":                item.Name,
		"description":         item.Description,
		"type":                diagType,
		"cells":               cellsJSON,
		"svg_image":           svgImage,
		"image_update_vector": imageUpdateVector,
		"update_vector":       updateVector,
		"modified_at":         item.ModifiedAt,
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

// Delete removes a diagram using GORM
func (s *GormDiagramStore) Delete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	result := s.db.Delete(&models.Diagram{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete diagram: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("diagram with ID %s not found", id)
	}

	return nil
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
	var metadataEntries []models.Metadata
	result := s.db.Where("entity_type = ? AND entity_id = ?", entityType, entityID).Order("key ASC").Find(&metadataEntries)
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

// saveMetadata saves metadata for a diagram using GORM
func (s *GormDiagramStore) saveMetadata(diagramID string, metadata []Metadata) error {
	if len(metadata) == 0 {
		return nil
	}

	for _, meta := range metadata {
		entry := models.Metadata{
			ID:         uuid.New().String(),
			EntityType: "diagram",
			EntityID:   diagramID,
			Key:        meta.Key,
			Value:      meta.Value,
		}

		result := s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "entity_type"}, {Name: "entity_id"}, {Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value", "modified_at"}),
		}).Create(&entry)

		if result.Error != nil {
			return fmt.Errorf("failed to save diagram metadata: %w", result.Error)
		}
	}

	return nil
}

// updateMetadata updates metadata for a diagram using GORM
func (s *GormDiagramStore) updateMetadata(diagramID string, metadata []Metadata) error {
	// Delete existing metadata
	result := s.db.Where("entity_type = ? AND entity_id = ?", "diagram", diagramID).Delete(&models.Metadata{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete existing diagram metadata: %w", result.Error)
	}

	// Insert new metadata
	return s.saveMetadata(diagramID, metadata)
}

// Helper function to dereference string pointer
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
