package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormGroupStore implements GroupStore using GORM for cross-database support
type GormGroupStore struct {
	db          *gorm.DB
	authService *auth.Service
	logger      *slogging.Logger
}

// NewGormGroupStore creates a new GORM-backed group store
func NewGormGroupStore(db *gorm.DB, authService *auth.Service) *GormGroupStore {
	return &GormGroupStore{
		db:          db,
		authService: authService,
		logger:      slogging.Get(),
	}
}

// List returns groups with optional filtering and pagination
func (s *GormGroupStore) List(ctx context.Context, filter GroupFilter) ([]Group, error) {
	query := s.db.WithContext(ctx).Model(&models.Group{})

	// Apply filters
	// Use map-based queries for cross-database compatibility (Oracle requires quoted lowercase column names)
	if filter.Provider != "" {
		query = query.Where(map[string]interface{}{"provider": filter.Provider})
	}

	if filter.GroupName != "" {
		// Use LOWER() with clause.Column for cross-database case-insensitive search
		// clause.Column ensures proper quoting of column names (required for Oracle)
		query = query.Where(
			clause.Expr{SQL: "LOWER(?) LIKE LOWER(?)",
				Vars: []interface{}{clause.Column{Name: "group_name"}, "%" + filter.GroupName + "%"}},
		)
	}

	if filter.UsedInAuthorizations != nil {
		if *filter.UsedInAuthorizations {
			query = query.Where("EXISTS (SELECT 1 FROM threat_model_access WHERE group_internal_uuid = groups.internal_uuid)")
		} else {
			query = query.Where("NOT EXISTS (SELECT 1 FROM threat_model_access WHERE group_internal_uuid = groups.internal_uuid)")
		}
	}

	// Apply sorting
	sortBy := "last_used"
	if filter.SortBy != "" {
		switch filter.SortBy {
		case "group_name", "first_used", "last_used", "usage_count":
			sortBy = filter.SortBy
		default:
			s.logger.Warn("Invalid sort_by value: %s, using default: last_used", filter.SortBy)
		}
	}

	sortOrder := "DESC"
	if filter.SortOrder != "" {
		switch strings.ToUpper(filter.SortOrder) {
		case "ASC":
			sortOrder = "ASC"
		case "DESC":
			sortOrder = "DESC"
		default:
			s.logger.Warn("Invalid sort_order value: %s, using default: DESC", filter.SortOrder)
		}
	}

	query = query.Order(fmt.Sprintf("%s %s", sortBy, sortOrder))

	// Apply pagination
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}

	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	// Execute query
	var gormGroups []models.Group
	if err := query.Find(&gormGroups).Error; err != nil {
		return nil, fmt.Errorf("failed to query groups: %w", err)
	}

	// Convert to API type
	groups := make([]Group, 0, len(gormGroups))
	for _, gg := range gormGroups {
		groups = append(groups, s.convertToGroup(&gg))
	}

	return groups, nil
}

// Get retrieves a group by internal UUID
func (s *GormGroupStore) Get(ctx context.Context, internalUUID uuid.UUID) (*Group, error) {
	var gormGroup models.Group
	result := s.db.WithContext(ctx).Where("internal_uuid = ?", internalUUID.String()).First(&gormGroup)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("group not found")
		}
		return nil, fmt.Errorf("failed to get group: %w", result.Error)
	}

	group := s.convertToGroup(&gormGroup)
	return &group, nil
}

// GetByProviderAndName retrieves a group by provider and group_name
func (s *GormGroupStore) GetByProviderAndName(ctx context.Context, provider string, groupName string) (*Group, error) {
	var gormGroup models.Group
	// Use struct-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
	result := s.db.WithContext(ctx).
		Where(&models.Group{Provider: provider, GroupName: groupName}).
		First(&gormGroup)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("group not found")
		}
		return nil, fmt.Errorf("failed to get group: %w", result.Error)
	}

	group := s.convertToGroup(&gormGroup)
	return &group, nil
}

// Create creates a new group (primarily for provider-independent groups)
func (s *GormGroupStore) Create(ctx context.Context, group Group) error {
	// Set default values if not provided
	if group.InternalUUID == uuid.Nil {
		group.InternalUUID = uuid.New()
	}
	if group.FirstUsed.IsZero() {
		group.FirstUsed = time.Now().UTC()
	}
	if group.LastUsed.IsZero() {
		group.LastUsed = time.Now().UTC()
	}
	if group.UsageCount == 0 {
		group.UsageCount = 1
	}

	gormGroup := s.convertFromGroup(&group)

	result := s.db.WithContext(ctx).Create(gormGroup)
	if result.Error != nil {
		// Check for duplicate key violation
		errStr := result.Error.Error()
		if strings.Contains(errStr, "duplicate key") ||
			strings.Contains(errStr, "unique constraint") ||
			strings.Contains(errStr, "UNIQUE constraint") ||
			strings.Contains(errStr, "ORA-00001") { // Oracle unique constraint
			return fmt.Errorf("group already exists for provider")
		}
		return fmt.Errorf("failed to create group: %w", result.Error)
	}

	return nil
}

// Update updates group metadata (name, description)
func (s *GormGroupStore) Update(ctx context.Context, group Group) error {
	result := s.db.WithContext(ctx).Model(&models.Group{}).
		Where("internal_uuid = ?", group.InternalUUID.String()).
		Updates(map[string]interface{}{
			"name":        ptrOrNil(group.Name),
			"description": ptrOrNil(group.Description),
			"last_used":   time.Now().UTC(),
		})

	if result.Error != nil {
		return fmt.Errorf("failed to update group: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("group not found")
	}

	return nil
}

// Delete deletes a TMI-managed group by group_name (provider is always "*")
// Delegates to auth service for proper cleanup of threat models and relationships
func (s *GormGroupStore) Delete(ctx context.Context, groupName string) (*GroupDeletionStats, error) {
	// Delegate to auth service which handles transaction and cleanup
	result, err := s.authService.DeleteGroupAndData(ctx, groupName)
	if err != nil {
		return nil, fmt.Errorf("failed to delete group: %w", err)
	}

	return &GroupDeletionStats{
		ThreatModelsDeleted:  result.ThreatModelsDeleted,
		ThreatModelsRetained: result.ThreatModelsRetained,
		GroupName:            result.GroupName,
	}, nil
}

// Count returns total count of groups matching the filter
func (s *GormGroupStore) Count(ctx context.Context, filter GroupFilter) (int, error) {
	query := s.db.WithContext(ctx).Model(&models.Group{})

	// Apply same filters as List (excluding pagination and sorting)
	// Use map-based queries for cross-database compatibility (Oracle requires quoted lowercase column names)
	if filter.Provider != "" {
		query = query.Where(map[string]interface{}{"provider": filter.Provider})
	}

	if filter.GroupName != "" {
		// Use LOWER() with clause.Column for cross-database case-insensitive search
		// clause.Column ensures proper quoting of column names (required for Oracle)
		query = query.Where(
			clause.Expr{SQL: "LOWER(?) LIKE LOWER(?)",
				Vars: []interface{}{clause.Column{Name: "group_name"}, "%" + filter.GroupName + "%"}},
		)
	}

	if filter.UsedInAuthorizations != nil {
		if *filter.UsedInAuthorizations {
			query = query.Where("EXISTS (SELECT 1 FROM threat_model_access WHERE group_internal_uuid = groups.internal_uuid)")
		} else {
			query = query.Where("NOT EXISTS (SELECT 1 FROM threat_model_access WHERE group_internal_uuid = groups.internal_uuid)")
		}
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count groups: %w", err)
	}

	return int(count), nil
}

// EnrichGroups adds related data to groups (usage in authorizations/admin grants)
func (s *GormGroupStore) EnrichGroups(ctx context.Context, groups []Group) ([]Group, error) {
	if len(groups) == 0 {
		return groups, nil
	}

	enriched := make([]Group, len(groups))
	copy(enriched, groups)

	for i := range enriched {
		group := &enriched[i]

		// Check if used in threat_model_access
		var usedInAuth bool
		err := s.db.WithContext(ctx).Raw(
			"SELECT EXISTS(SELECT 1 FROM threat_model_access WHERE group_internal_uuid = ?)",
			group.InternalUUID.String(),
		).Scan(&usedInAuth).Error
		if err != nil {
			s.logger.Warn("Failed to check authorization usage for group %s: %v", group.InternalUUID, err)
		} else {
			group.UsedInAuthorizations = usedInAuth
		}

		// Check if used in administrators table
		var usedInAdmin bool
		err = s.db.WithContext(ctx).Raw(
			"SELECT EXISTS(SELECT 1 FROM administrators WHERE group_internal_uuid = ?)",
			group.InternalUUID.String(),
		).Scan(&usedInAdmin).Error
		if err != nil {
			s.logger.Warn("Failed to check admin grant usage for group %s: %v", group.InternalUUID, err)
		} else {
			group.UsedInAdminGrants = usedInAdmin
		}

		// Note: MemberCount would require querying the IdP - leave as 0 for now
	}

	return enriched, nil
}

// GetGroupsForProvider returns all groups for a specific provider (for UI autocomplete)
func (s *GormGroupStore) GetGroupsForProvider(ctx context.Context, provider string) ([]Group, error) {
	filter := GroupFilter{
		Provider:  provider,
		SortBy:    "last_used",
		SortOrder: "DESC",
		Limit:     500, // Reasonable limit for autocomplete
	}
	return s.List(ctx, filter)
}

// UpsertGroup creates or updates a group (used during JWT group sync)
func (s *GormGroupStore) UpsertGroup(ctx context.Context, group Group) error {
	gormGroup := s.convertFromGroup(&group)

	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "provider"}, {Name: "group_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_used", "usage_count"}),
	}).Create(gormGroup)

	if result.Error != nil {
		return fmt.Errorf("failed to upsert group: %w", result.Error)
	}

	return nil
}

// convertToGroup converts a GORM Group model to API Group
func (s *GormGroupStore) convertToGroup(gg *models.Group) Group {
	internalUUID, _ := uuid.Parse(gg.InternalUUID)

	return Group{
		InternalUUID: internalUUID,
		Provider:     gg.Provider,
		GroupName:    gg.GroupName,
		Name:         derefString(gg.Name),
		Description:  derefString(gg.Description),
		FirstUsed:    gg.FirstUsed,
		LastUsed:     gg.LastUsed,
		UsageCount:   gg.UsageCount,
	}
}

// convertFromGroup converts an API Group to GORM Group model
func (s *GormGroupStore) convertFromGroup(g *Group) *models.Group {
	return &models.Group{
		InternalUUID: g.InternalUUID.String(),
		Provider:     g.Provider,
		GroupName:    g.GroupName,
		Name:         ptrOrNil(g.Name),
		Description:  ptrOrNil(g.Description),
		FirstUsed:    g.FirstUsed,
		LastUsed:     g.LastUsed,
		UsageCount:   g.UsageCount,
	}
}

// ptrOrNil returns a pointer to the string if non-empty, nil otherwise
func ptrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
