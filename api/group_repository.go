package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/api/validation"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ptrOrNil returns a pointer to the string if non-empty, nil otherwise
func ptrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// GormGroupRepository implements GroupRepository using GORM for cross-database support
type GormGroupRepository struct {
	db     *gorm.DB
	logger *slogging.Logger
}

// NewGormGroupRepository creates a new GORM-backed group repository
func NewGormGroupRepository(db *gorm.DB) *GormGroupRepository {
	return &GormGroupRepository{
		db:     db,
		logger: slogging.Get(),
	}
}

// List returns groups with optional filtering and pagination
func (r *GormGroupRepository) List(ctx context.Context, filter GroupFilter) ([]Group, error) {
	query := r.db.WithContext(ctx).Model(&models.Group{})

	// Apply filters
	// Use map-based queries for cross-database compatibility (Oracle requires quoted lowercase column names)
	if filter.Provider != "" {
		query = query.Where(ColumnMap(query.Name(), map[string]any{"provider": filter.Provider}))
	}

	if filter.GroupName != "" {
		// Use LOWER() with Col() for cross-database case-insensitive search
		// Col() ensures proper column name casing (uppercase for Oracle)
		query = query.Where(
			clause.Expr{SQL: "LOWER(?) LIKE LOWER(?)",
				Vars: []any{Col(r.db.Name(), "group_name"), "%" + filter.GroupName + "%"}},
		)
	}

	if filter.UsedInAuthorizations != nil {
		if *filter.UsedInAuthorizations {
			query = query.Where("EXISTS (SELECT 1 FROM threat_model_access WHERE group_internal_uuid = groups.internal_uuid)")
		} else {
			query = query.Where("NOT EXISTS (SELECT 1 FROM threat_model_access WHERE group_internal_uuid = groups.internal_uuid)")
		}
	}

	if filter.BuiltIn != nil {
		if *filter.BuiltIn {
			query = query.Where(
				clause.Expr{SQL: "? IN ?",
					Vars: []any{Col(r.db.Name(), "internal_uuid"), validation.BuiltInGroupUUIDs}},
			)
		} else {
			query = query.Where(
				clause.Expr{SQL: "? NOT IN ?",
					Vars: []any{Col(r.db.Name(), "internal_uuid"), validation.BuiltInGroupUUIDs}},
			)
		}
	}

	// Apply sorting
	sortBy := GroupSortByLastUsed
	if filter.SortBy != "" {
		switch filter.SortBy {
		case GroupSortByGroupName, GroupSortByFirstUsed, GroupSortByLastUsed, GroupSortByUsageCount:
			sortBy = filter.SortBy
		default:
			r.logger.Warn("Invalid sort_by value: %s, using default: last_used", filter.SortBy)
		}
	}

	sortOrder := SortDirectionDESC
	if filter.SortOrder != "" {
		switch strings.ToUpper(filter.SortOrder) {
		case SortDirectionASC:
			sortOrder = SortDirectionASC
		case SortDirectionDESC:
			sortOrder = SortDirectionDESC
		default:
			r.logger.Warn("Invalid sort_order value: %s, using default: DESC", filter.SortOrder)
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
		return nil, dberrors.Classify(err)
	}

	// Convert to API type
	groups := make([]Group, 0, len(gormGroups))
	for _, gg := range gormGroups {
		groups = append(groups, r.convertToGroup(&gg))
	}

	return groups, nil
}

// Get retrieves a group by internal UUID
func (r *GormGroupRepository) Get(ctx context.Context, internalUUID uuid.UUID) (*Group, error) {
	var gormGroup models.Group
	result := r.db.WithContext(ctx).Where("internal_uuid = ?", internalUUID.String()).First(&gormGroup)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrGroupNotFound
		}
		return nil, dberrors.Classify(result.Error)
	}

	group := r.convertToGroup(&gormGroup)
	return &group, nil
}

// GetByProviderAndName retrieves a group by provider and group_name
func (r *GormGroupRepository) GetByProviderAndName(ctx context.Context, provider string, groupName string) (*Group, error) {
	var gormGroup models.Group
	// Use struct-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
	result := r.db.WithContext(ctx).
		Where(&models.Group{Provider: models.DBVarchar(provider), GroupName: models.DBVarchar(groupName)}).
		First(&gormGroup)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrGroupNotFound
		}
		return nil, dberrors.Classify(result.Error)
	}

	group := r.convertToGroup(&gormGroup)
	return &group, nil
}

// Create creates a new group (primarily for provider-independent groups)
func (r *GormGroupRepository) Create(ctx context.Context, group Group) error {
	// Set default values if not provided
	if group.InternalUUID == uuid.Nil {
		group.InternalUUID = uuid.New()
	}
	if group.FirstUsed.IsZero() {
		group.FirstUsed = time.Now().UTC().Truncate(time.Microsecond)
	}
	if group.LastUsed.IsZero() {
		group.LastUsed = time.Now().UTC().Truncate(time.Microsecond)
	}
	if group.UsageCount == 0 {
		group.UsageCount = 1
	}

	gormGroup := r.convertFromGroup(&group)

	// READ COMMITTED: insert-only on freshly keyed rows, so SERIALIZABLE only adds false ORA-08177 (#906, ADR 2026-09-17).
	return authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Create(gormGroup)
		if result.Error != nil {
			classified := dberrors.Classify(result.Error)
			if errors.Is(classified, dberrors.ErrDuplicate) {
				return ErrGroupDuplicate
			}
			return classified
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

// Update updates group metadata (name, description)
func (r *GormGroupRepository) Update(ctx context.Context, group Group) error {
	// last_used: the caller's value (the handler returns it), zero falls back to
	// now. Keyed "LastUsed" (Go field name): Group.LastUsed is autoUpdateTime and
	// hooks run here, so a lowercase key would not suppress gorm-oracle's
	// injection and LAST_USED would be assigned twice (ORA-00957) (#1155).
	lastUsed := group.LastUsed.UTC().Truncate(time.Microsecond)
	if group.LastUsed.IsZero() {
		lastUsed = time.Now().UTC().Truncate(time.Microsecond)
	}
	return authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Model(&models.Group{}).
			Where("internal_uuid = ?", group.InternalUUID.String()).
			Updates(map[string]any{
				"name":        ptrOrNil(group.Name),
				"description": ptrOrNil(group.Description),
				"LastUsed":    lastUsed,
			})

		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}

		if result.RowsAffected == 0 {
			return ErrGroupNotFound
		}

		return nil
	})
}

// Count returns total count of groups matching the filter
func (r *GormGroupRepository) Count(ctx context.Context, filter GroupFilter) (int, error) {
	query := r.db.WithContext(ctx).Model(&models.Group{})

	// Apply same filters as List (excluding pagination and sorting)
	// Use map-based queries for cross-database compatibility (Oracle requires quoted lowercase column names)
	if filter.Provider != "" {
		query = query.Where(ColumnMap(query.Name(), map[string]any{"provider": filter.Provider}))
	}

	if filter.GroupName != "" {
		// Use LOWER() with Col() for cross-database case-insensitive search
		// Col() ensures proper column name casing (uppercase for Oracle)
		query = query.Where(
			clause.Expr{SQL: "LOWER(?) LIKE LOWER(?)",
				Vars: []any{Col(r.db.Name(), "group_name"), "%" + filter.GroupName + "%"}},
		)
	}

	if filter.UsedInAuthorizations != nil {
		if *filter.UsedInAuthorizations {
			query = query.Where("EXISTS (SELECT 1 FROM threat_model_access WHERE group_internal_uuid = groups.internal_uuid)")
		} else {
			query = query.Where("NOT EXISTS (SELECT 1 FROM threat_model_access WHERE group_internal_uuid = groups.internal_uuid)")
		}
	}

	if filter.BuiltIn != nil {
		if *filter.BuiltIn {
			query = query.Where(
				clause.Expr{SQL: "? IN ?",
					Vars: []any{Col(r.db.Name(), "internal_uuid"), validation.BuiltInGroupUUIDs}},
			)
		} else {
			query = query.Where(
				clause.Expr{SQL: "? NOT IN ?",
					Vars: []any{Col(r.db.Name(), "internal_uuid"), validation.BuiltInGroupUUIDs}},
			)
		}
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, dberrors.Classify(err)
	}

	return int(count), nil
}

// EnrichGroups adds related data to groups (usage in authorizations/admin grants)
func (r *GormGroupRepository) EnrichGroups(ctx context.Context, groups []Group) ([]Group, error) {
	if len(groups) == 0 {
		return groups, nil
	}

	enriched := make([]Group, len(groups))
	copy(enriched, groups)

	for i := range enriched {
		group := &enriched[i]

		// Check if used in threat_model_access
		var usedInAuth bool
		err := r.db.WithContext(ctx).Raw(
			"SELECT EXISTS(SELECT 1 FROM threat_model_access WHERE group_internal_uuid = ?)",
			group.InternalUUID.String(),
		).Scan(&usedInAuth).Error
		if err != nil {
			r.logger.Warn("Failed to check authorization usage for group %s: %v", group.InternalUUID, err)
		} else {
			group.UsedInAuthorizations = usedInAuth
		}

		// Check if group is a member of the Administrators group
		var usedInAdmin bool
		err = r.db.WithContext(ctx).Raw(
			"SELECT EXISTS(SELECT 1 FROM group_members WHERE group_internal_uuid = ? AND subject_type = 'group' AND member_group_internal_uuid = ?)",
			AdministratorsGroupUUID, group.InternalUUID.String(),
		).Scan(&usedInAdmin).Error
		if err != nil {
			r.logger.Warn("Failed to check admin grant usage for group %s: %v", group.InternalUUID, err)
		} else {
			group.UsedInAdminGrants = usedInAdmin
		}

		// Note: MemberCount would require querying the IdP - leave as 0 for now
	}

	return enriched, nil
}

// GetGroupsForProvider returns all groups for a specific provider (for UI autocomplete)
func (r *GormGroupRepository) GetGroupsForProvider(ctx context.Context, provider string) ([]Group, error) {
	filter := GroupFilter{
		Provider:  provider,
		SortBy:    GroupSortByLastUsed,
		SortOrder: SortDirectionDESC,
		Limit:     500, // Reasonable limit for autocomplete
	}
	return r.List(ctx, filter)
}

// UpsertGroup creates or updates a group (used during JWT group sync)
// This is a concrete method not on the GroupRepository interface — kept for future JWT group sync use.
func (r *GormGroupRepository) UpsertGroup(ctx context.Context, group Group) error {
	gormGroup := r.convertFromGroup(&group)

	// Use Col() for column names to handle Oracle uppercase naming
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{Col(r.db.Name(), "provider"), Col(r.db.Name(), "group_name")},
		DoUpdates: clause.AssignmentColumns([]string{ColumnName(r.db.Name(), "last_used"), ColumnName(r.db.Name(), "usage_count")}),
	}).Create(gormGroup)

	if result.Error == nil {
		return nil
	}

	classified := dberrors.Classify(result.Error)
	if !errors.Is(classified, dberrors.ErrDuplicate) {
		return classified
	}

	// #704: uniq_groups_provider_group_name backs the OnConflict target, but
	// a concurrent first-time upsert for the same (provider, group_name) can
	// still lose the create race here (reachable on Oracle, whose MERGE
	// lacks the atomicity PostgreSQL's ON CONFLICT has for the same conflict
	// target). IsRetryable does not retry ErrDuplicate, so confirm the
	// winner's row exists instead of surfacing an error the caller (JWT
	// group sync) can't act on.
	//
	// Guard against an empty group name: the struct-based query below omits
	// zero-value fields, so an empty GroupName would silently drop that
	// predicate and match an arbitrary group for this provider (#502). The
	// Create itself would normally fail before this branch is reached, but
	// the guard costs nothing and keeps the failure mode "duplicate" instead
	// of "wrong group".
	if gormGroup.GroupName == "" {
		return classified
	}
	var existing models.Group
	if err := r.db.WithContext(ctx).
		Where(&models.Group{Provider: gormGroup.Provider, GroupName: gormGroup.GroupName}).
		First(&existing).Error; err != nil {
		return dberrors.Classify(err)
	}
	return nil
}

// convertToGroup converts a GORM Group model to API Group
func (r *GormGroupRepository) convertToGroup(gg *models.Group) Group {
	internalUUID, _ := uuid.Parse(string(gg.InternalUUID))

	return Group{
		InternalUUID: internalUUID,
		Provider:     string(gg.Provider),
		GroupName:    string(gg.GroupName),
		Name:         strFromPtr(gg.Name.Ptr()),
		Description:  strFromPtr(gg.Description.Ptr()),
		FirstUsed:    gg.FirstUsed,
		LastUsed:     gg.LastUsed,
		UsageCount:   gg.UsageCount,
		IsBuiltin:    validation.IsBuiltInGroup(string(gg.InternalUUID)),
	}
}

// convertFromGroup converts an API Group to GORM Group model
func (r *GormGroupRepository) convertFromGroup(g *Group) *models.Group {
	return &models.Group{
		InternalUUID: models.DBVarchar(g.InternalUUID.String()),
		Provider:     models.DBVarchar(g.Provider),
		GroupName:    models.DBVarchar(g.GroupName),
		Name:         models.NewNullableDBVarchar(ptrOrNil(g.Name)),
		Description:  models.NewNullableDBText(ptrOrNil(g.Description)),
		FirstUsed:    g.FirstUsed,
		LastUsed:     g.LastUsed,
		UsageCount:   g.UsageCount,
	}
}
