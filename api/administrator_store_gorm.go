package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormAdministratorStore implements AdministratorStore using GORM
type GormAdministratorStore struct {
	db *gorm.DB
}

// NewGormAdministratorStore creates a new GORM-backed administrator store
func NewGormAdministratorStore(db *gorm.DB) *GormAdministratorStore {
	return &GormAdministratorStore{db: db}
}

// Create adds a new administrator entry with upsert support
func (s *GormAdministratorStore) Create(ctx context.Context, admin DBAdministrator) error {
	logger := slogging.Get()

	// Generate ID if not set
	id := admin.ID
	if id == uuid.Nil {
		id = uuid.New()
	}

	model := s.dbAdminToModel(admin)
	model.ID = id.String()

	// Use different conflict columns based on subject_type
	var conflictColumns []clause.Column
	if admin.SubjectType == "user" {
		conflictColumns = []clause.Column{{Name: "user_internal_uuid"}, {Name: "subject_type"}}
	} else {
		conflictColumns = []clause.Column{{Name: "group_internal_uuid"}, {Name: "subject_type"}, {Name: "provider"}}
	}

	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: conflictColumns,
		DoUpdates: clause.AssignmentColumns([]string{
			"granted_at",
			"granted_by_internal_uuid",
			"notes",
			"provider",
		}),
	}).Create(&model)

	if result.Error != nil {
		logger.Error("Failed to create administrator entry: type=%s, provider=%s, user_uuid=%v, group_uuid=%v, error=%v",
			admin.SubjectType, admin.Provider, admin.UserInternalUUID, admin.GroupInternalUUID, result.Error)
		return fmt.Errorf("failed to create administrator: %w", result.Error)
	}

	logger.Info("Administrator created: type=%s, provider=%s, user_uuid=%v, group_uuid=%v",
		admin.SubjectType, admin.Provider, admin.UserInternalUUID, admin.GroupInternalUUID)

	return nil
}

// Delete removes an administrator entry by ID
func (s *GormAdministratorStore) Delete(ctx context.Context, id uuid.UUID) error {
	logger := slogging.Get()

	result := s.db.WithContext(ctx).Delete(&models.Administrator{}, "id = ?", id.String())

	if result.Error != nil {
		logger.Error("Failed to delete administrator entry: id=%s, error=%v", id, result.Error)
		return fmt.Errorf("failed to delete administrator: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("administrator not found: id=%s", id)
	}

	logger.Info("Administrator deleted: id=%s", id)

	return nil
}

// List returns all administrator entries
func (s *GormAdministratorStore) List(ctx context.Context) ([]DBAdministrator, error) {
	logger := slogging.Get()

	var modelList []models.Administrator
	result := s.db.WithContext(ctx).Order("granted_at DESC").Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to list administrators: %v", result.Error)
		return nil, fmt.Errorf("failed to list administrators: %w", result.Error)
	}

	administrators := make([]DBAdministrator, len(modelList))
	for i, model := range modelList {
		administrators[i] = s.modelToDBAdmin(model)
	}

	logger.Debug("Listed %d administrators", len(administrators))

	return administrators, nil
}

// IsAdmin checks if a user or any of their groups is an administrator
func (s *GormAdministratorStore) IsAdmin(ctx context.Context, userUUID *uuid.UUID, provider string, groupUUIDs []uuid.UUID) (bool, error) {
	logger := slogging.Get()

	// Build query to check user or group admin status
	query := s.db.WithContext(ctx).Model(&models.Administrator{}).
		Where("provider = ?", provider)

	// Build the OR conditions for user and groups
	if userUUID != nil && len(groupUUIDs) > 0 {
		groupUUIDStrings := make([]string, len(groupUUIDs))
		for i, g := range groupUUIDs {
			groupUUIDStrings[i] = g.String()
		}
		query = query.Where(
			"(subject_type = 'user' AND user_internal_uuid = ?) OR (subject_type = 'group' AND group_internal_uuid IN ?)",
			userUUID.String(), groupUUIDStrings,
		)
	} else if userUUID != nil {
		query = query.Where("subject_type = 'user' AND user_internal_uuid = ?", userUUID.String())
	} else if len(groupUUIDs) > 0 {
		groupUUIDStrings := make([]string, len(groupUUIDs))
		for i, g := range groupUUIDs {
			groupUUIDStrings[i] = g.String()
		}
		query = query.Where("subject_type = 'group' AND group_internal_uuid IN ?", groupUUIDStrings)
	} else {
		// No user or groups to check
		return false, nil
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		logger.Error("Failed to check admin status for user_uuid=%v, provider=%s, groups=%v: %v",
			userUUID, provider, groupUUIDs, err)
		return false, fmt.Errorf("failed to check admin status: %w", err)
	}

	isAdmin := count > 0
	logger.Debug("Admin check: user_uuid=%v, provider=%s, groups=%v, is_admin=%t",
		userUUID, provider, groupUUIDs, isAdmin)

	return isAdmin, nil
}

// GetByPrincipal retrieves administrator entries by user or group UUID
func (s *GormAdministratorStore) GetByPrincipal(ctx context.Context, userUUID *uuid.UUID, groupUUID *uuid.UUID, provider string) ([]DBAdministrator, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Where("provider = ?", provider)

	if userUUID != nil && groupUUID != nil {
		query = query.Where("user_internal_uuid = ? OR group_internal_uuid = ?", userUUID.String(), groupUUID.String())
	} else if userUUID != nil {
		query = query.Where("user_internal_uuid = ?", userUUID.String())
	} else if groupUUID != nil {
		query = query.Where("group_internal_uuid = ?", groupUUID.String())
	}

	var modelList []models.Administrator
	result := query.Order("granted_at DESC").Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to get administrators by principal: user_uuid=%v, group_uuid=%v, provider=%s, error=%v",
			userUUID, groupUUID, provider, result.Error)
		return nil, fmt.Errorf("failed to get administrators by principal: %w", result.Error)
	}

	administrators := make([]DBAdministrator, len(modelList))
	for i, model := range modelList {
		administrators[i] = s.modelToDBAdmin(model)
	}

	logger.Debug("Found %d administrators for user_uuid=%v, group_uuid=%v, provider=%s",
		len(administrators), userUUID, groupUUID, provider)

	return administrators, nil
}

// GetGroupUUIDsByNames looks up group UUIDs from group names for a given provider
func (s *GormAdministratorStore) GetGroupUUIDsByNames(ctx context.Context, provider string, groupNames []string) ([]uuid.UUID, error) {
	if len(groupNames) == 0 {
		return []uuid.UUID{}, nil
	}

	logger := slogging.Get()

	var groups []models.Group
	// Use map-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
	// Map keys are converted to proper column names by GORM's schema layer
	result := s.db.WithContext(ctx).
		Where(map[string]interface{}{"provider": provider, "group_name": groupNames}).
		Find(&groups)

	if result.Error != nil {
		logger.Error("Failed to look up group UUIDs: provider=%s, group_names=%v, error=%v",
			provider, groupNames, result.Error)
		return nil, fmt.Errorf("failed to look up group UUIDs: %w", result.Error)
	}

	groupUUIDs := make([]uuid.UUID, 0, len(groups))
	for _, g := range groups {
		if groupUUID, err := uuid.Parse(g.InternalUUID); err == nil {
			groupUUIDs = append(groupUUIDs, groupUUID)
		}
	}

	logger.Debug("Looked up %d group UUIDs from %d group names for provider %s",
		len(groupUUIDs), len(groupNames), provider)

	return groupUUIDs, nil
}

// Get retrieves a single administrator grant by ID
func (s *GormAdministratorStore) Get(ctx context.Context, id uuid.UUID) (*DBAdministrator, error) {
	logger := slogging.Get()

	var model models.Administrator
	result := s.db.WithContext(ctx).First(&model, "id = ?", id.String())

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			logger.Debug("Administrator not found: id=%s", id)
			return nil, fmt.Errorf("administrator not found: id=%s", id)
		}
		logger.Error("Failed to get administrator: id=%s, error=%v", id, result.Error)
		return nil, fmt.Errorf("failed to get administrator: %w", result.Error)
	}

	admin := s.modelToDBAdmin(model)
	logger.Debug("Retrieved administrator: id=%s", id)

	return &admin, nil
}

// ListFiltered retrieves administrator grants with optional filtering
func (s *GormAdministratorStore) ListFiltered(ctx context.Context, filter AdminFilter) ([]DBAdministrator, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.Administrator{})

	// Apply filters
	if filter.Provider != "" {
		query = query.Where("provider = ?", filter.Provider)
	}
	if filter.UserID != nil {
		query = query.Where("user_internal_uuid = ?", filter.UserID.String())
	}
	if filter.GroupID != nil {
		query = query.Where("group_internal_uuid = ?", filter.GroupID.String())
	}

	// Apply pagination
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	var modelList []models.Administrator
	result := query.Order("granted_at DESC").
		Limit(limit).
		Offset(filter.Offset).
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to list administrators with filter: %v", result.Error)
		return nil, fmt.Errorf("failed to list administrators: %w", result.Error)
	}

	administrators := make([]DBAdministrator, len(modelList))
	for i, model := range modelList {
		administrators[i] = s.modelToDBAdmin(model)
	}

	logger.Debug("Listed %d administrators with filter", len(administrators))

	return administrators, nil
}

// CountFiltered returns the total count of administrators matching the filter (without pagination)
func (s *GormAdministratorStore) CountFiltered(ctx context.Context, filter AdminFilter) (int, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.Administrator{})

	// Apply filters (same as ListFiltered, but no pagination)
	if filter.Provider != "" {
		query = query.Where("provider = ?", filter.Provider)
	}
	if filter.UserID != nil {
		query = query.Where("user_internal_uuid = ?", filter.UserID.String())
	}
	if filter.GroupID != nil {
		query = query.Where("group_internal_uuid = ?", filter.GroupID.String())
	}

	var count int64
	result := query.Count(&count)

	if result.Error != nil {
		logger.Error("Failed to count administrators with filter: %v", result.Error)
		return 0, fmt.Errorf("failed to count administrators: %w", result.Error)
	}

	logger.Debug("Counted %d administrators with filter", count)

	return int(count), nil
}

// HasAnyAdministrators returns true if at least one administrator grant exists
func (s *GormAdministratorStore) HasAnyAdministrators(ctx context.Context) (bool, error) {
	logger := slogging.Get()

	var count int64
	result := s.db.WithContext(ctx).Model(&models.Administrator{}).Limit(1).Count(&count)

	if result.Error != nil {
		logger.Error("Failed to check if any administrators exist: %v", result.Error)
		return false, fmt.Errorf("failed to check administrators: %w", result.Error)
	}

	hasAdmins := count > 0
	logger.Debug("HasAnyAdministrators: %t", hasAdmins)

	return hasAdmins, nil
}

// GetUserDetails retrieves email and name for an internal_uuid
func (s *GormAdministratorStore) GetUserDetails(ctx context.Context, userID uuid.UUID) (email string, name string, err error) {
	logger := slogging.Get()

	var user models.User
	result := s.db.WithContext(ctx).
		Select("email", "name").
		First(&user, "internal_uuid = ?", userID.String())

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			logger.Debug("User not found for details lookup: internal_uuid=%s", userID)
			return "", "", nil
		}
		logger.Error("Failed to get user details: internal_uuid=%s, error=%v", userID, result.Error)
		return "", "", fmt.Errorf("failed to get user details: %w", result.Error)
	}

	return user.Email, user.Name, nil
}

// GetGroupName retrieves name for a group_id
func (s *GormAdministratorStore) GetGroupName(ctx context.Context, groupID uuid.UUID, provider string) (string, error) {
	logger := slogging.Get()

	var group models.Group
	// Use map-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
	result := s.db.WithContext(ctx).
		Where(map[string]interface{}{"internal_uuid": groupID.String(), "provider": provider}).
		First(&group)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			logger.Debug("Group not found for name lookup: id=%s, provider=%s", groupID, provider)
			return "", nil
		}
		logger.Error("Failed to get group name: id=%s, provider=%s, error=%v", groupID, provider, result.Error)
		return "", fmt.Errorf("failed to get group name: %w", result.Error)
	}

	return group.GroupName, nil
}

// EnrichAdministrators adds user_email, user_name, and group_name to administrator records
func (s *GormAdministratorStore) EnrichAdministrators(ctx context.Context, admins []DBAdministrator) ([]DBAdministrator, error) {
	logger := slogging.Get()

	enriched := make([]DBAdministrator, len(admins))
	for i, admin := range admins {
		enriched[i] = admin

		// Enrich user-based grants with email and name
		if admin.UserInternalUUID != nil {
			email, name, err := s.GetUserDetails(ctx, *admin.UserInternalUUID)
			if err != nil {
				logger.Warn("Failed to enrich user details for admin %s: %v", admin.ID, err)
			}
			enriched[i].UserEmail = email
			enriched[i].UserName = name
		}

		// Enrich group-based grants with group name
		if admin.GroupInternalUUID != nil {
			groupName, err := s.GetGroupName(ctx, *admin.GroupInternalUUID, admin.Provider)
			if err != nil {
				logger.Warn("Failed to enrich group name for admin %s: %v", admin.ID, err)
			}
			enriched[i].GroupName = groupName
		}
	}

	logger.Debug("Enriched %d administrator records", len(enriched))

	return enriched, nil
}

// modelToDBAdmin converts a GORM model to the DBAdministrator type
func (s *GormAdministratorStore) modelToDBAdmin(model models.Administrator) DBAdministrator {
	admin := DBAdministrator{
		SubjectType: model.SubjectType,
		Provider:    model.Provider,
		GrantedAt:   model.GrantedAt,
	}

	if id, err := uuid.Parse(model.ID); err == nil {
		admin.ID = id
	}

	if model.UserInternalUUID != nil {
		if userUUID, err := uuid.Parse(*model.UserInternalUUID); err == nil {
			admin.UserInternalUUID = &userUUID
		}
	}

	if model.GroupInternalUUID != nil {
		if groupUUID, err := uuid.Parse(*model.GroupInternalUUID); err == nil {
			admin.GroupInternalUUID = &groupUUID
		}
	}

	if model.GrantedByInternalUUID != nil {
		if grantedBy, err := uuid.Parse(*model.GrantedByInternalUUID); err == nil {
			admin.GrantedBy = &grantedBy
		}
	}

	if model.Notes != nil {
		admin.Notes = *model.Notes
	}

	return admin
}

// dbAdminToModel converts a DBAdministrator to a GORM model
func (s *GormAdministratorStore) dbAdminToModel(admin DBAdministrator) models.Administrator {
	model := models.Administrator{
		ID:          admin.ID.String(),
		SubjectType: admin.SubjectType,
		Provider:    admin.Provider,
		GrantedAt:   admin.GrantedAt,
	}

	if admin.UserInternalUUID != nil {
		uuidStr := admin.UserInternalUUID.String()
		model.UserInternalUUID = &uuidStr
	}

	if admin.GroupInternalUUID != nil {
		uuidStr := admin.GroupInternalUUID.String()
		model.GroupInternalUUID = &uuidStr
	}

	if admin.GrantedBy != nil {
		uuidStr := admin.GrantedBy.String()
		model.GrantedByInternalUUID = &uuidStr
	}

	if admin.Notes != "" {
		model.Notes = &admin.Notes
	}

	return model
}

// GormAdminCheckerAdapter adapts GormAdministratorStore to the auth.AdminChecker interface
type GormAdminCheckerAdapter struct {
	store *GormAdministratorStore
}

// NewGormAdminCheckerAdapter creates a new adapter for the auth.AdminChecker interface
func NewGormAdminCheckerAdapter(store *GormAdministratorStore) *GormAdminCheckerAdapter {
	return &GormAdminCheckerAdapter{store: store}
}

// IsAdmin checks if a user is an administrator (implements auth.AdminChecker)
func (a *GormAdminCheckerAdapter) IsAdmin(ctx context.Context, userInternalUUID *string, provider string, groupUUIDs []string) (bool, error) {
	// Convert string UUID to uuid.UUID pointer
	var userUUID *uuid.UUID
	if userInternalUUID != nil && *userInternalUUID != "" {
		parsed, err := uuid.Parse(*userInternalUUID)
		if err != nil {
			return false, fmt.Errorf("invalid user UUID: %w", err)
		}
		userUUID = &parsed
	}

	// Convert string UUIDs to uuid.UUID slice
	uuids := make([]uuid.UUID, 0, len(groupUUIDs))
	for _, uuidStr := range groupUUIDs {
		parsed, err := uuid.Parse(uuidStr)
		if err != nil {
			return false, fmt.Errorf("invalid group UUID %s: %w", uuidStr, err)
		}
		uuids = append(uuids, parsed)
	}

	return a.store.IsAdmin(ctx, userUUID, provider, uuids)
}

// GetGroupUUIDsByNames converts group names to UUIDs (implements auth.AdminChecker)
func (a *GormAdminCheckerAdapter) GetGroupUUIDsByNames(ctx context.Context, provider string, groupNames []string) ([]string, error) {
	uuids, err := a.store.GetGroupUUIDsByNames(ctx, provider, groupNames)
	if err != nil {
		return nil, err
	}

	// Convert uuid.UUID slice to string slice
	result := make([]string, len(uuids))
	for i, u := range uuids {
		result[i] = u.String()
	}
	return result, nil
}
