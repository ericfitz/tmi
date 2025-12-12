package api

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// GroupDatabaseStore implements GroupStore using PostgreSQL
type GroupDatabaseStore struct {
	db          *sql.DB
	authService *auth.Service
}

// NewGroupDatabaseStore creates a new database-backed group store
func NewGroupDatabaseStore(db *sql.DB, authService *auth.Service) *GroupDatabaseStore {
	return &GroupDatabaseStore{
		db:          db,
		authService: authService,
	}
}

// List returns groups with optional filtering and pagination
func (s *GroupDatabaseStore) List(ctx context.Context, filter GroupFilter) ([]Group, error) {
	logger := slogging.Get()

	// Build query with filters
	query := `SELECT internal_uuid, provider, group_name, name, description,
	          first_used, last_used, usage_count FROM groups WHERE 1=1`
	args := []interface{}{}
	argPos := 1

	// Apply filters
	if filter.Provider != "" {
		query += fmt.Sprintf(" AND provider = $%d", argPos)
		args = append(args, filter.Provider)
		argPos++
	}

	if filter.GroupName != "" {
		query += fmt.Sprintf(" AND LOWER(group_name) LIKE LOWER($%d)", argPos)
		args = append(args, "%"+filter.GroupName+"%")
		argPos++
	}

	if filter.UsedInAuthorizations != nil {
		// Subquery to check if group is used in threat_model_access
		if *filter.UsedInAuthorizations {
			query += ` AND EXISTS (SELECT 1 FROM threat_model_access WHERE group_internal_uuid = groups.internal_uuid)`
		} else {
			query += ` AND NOT EXISTS (SELECT 1 FROM threat_model_access WHERE group_internal_uuid = groups.internal_uuid)`
		}
	}

	// Apply sorting
	sortBy := "last_used"
	if filter.SortBy != "" {
		switch filter.SortBy {
		case "group_name", "first_used", "last_used", "usage_count":
			sortBy = filter.SortBy
		default:
			logger.Warn("Invalid sort_by value: %s, using default: last_used", filter.SortBy)
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
			logger.Warn("Invalid sort_order value: %s, using default: DESC", filter.SortOrder)
		}
	}

	query += fmt.Sprintf(" ORDER BY %s %s", sortBy, sortOrder)

	// Apply pagination
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argPos)
		args = append(args, filter.Limit)
		argPos++
	}

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argPos)
		args = append(args, filter.Offset)
		// argPos++ // Last use of argPos, no need to increment
	}

	// Execute query
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query groups: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			logger.Error("Failed to close rows: %v", cerr)
		}
	}()

	// Scan results
	groups := []Group{}
	for rows.Next() {
		var group Group
		var name, description sql.NullString

		err := rows.Scan(
			&group.InternalUUID,
			&group.Provider,
			&group.GroupName,
			&name,
			&description,
			&group.FirstUsed,
			&group.LastUsed,
			&group.UsageCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan group: %w", err)
		}

		if name.Valid {
			group.Name = name.String
		}
		if description.Valid {
			group.Description = description.String
		}

		groups = append(groups, group)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating groups: %w", err)
	}

	return groups, nil
}

// Get retrieves a group by internal UUID
func (s *GroupDatabaseStore) Get(ctx context.Context, internalUUID uuid.UUID) (*Group, error) {
	query := `SELECT internal_uuid, provider, group_name, name, description,
	          first_used, last_used, usage_count FROM groups WHERE internal_uuid = $1`

	var group Group
	var name, description sql.NullString

	err := s.db.QueryRowContext(ctx, query, internalUUID).Scan(
		&group.InternalUUID,
		&group.Provider,
		&group.GroupName,
		&name,
		&description,
		&group.FirstUsed,
		&group.LastUsed,
		&group.UsageCount,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("group not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get group: %w", err)
	}

	if name.Valid {
		group.Name = name.String
	}
	if description.Valid {
		group.Description = description.String
	}

	return &group, nil
}

// GetByProviderAndName retrieves a group by provider and group_name
func (s *GroupDatabaseStore) GetByProviderAndName(ctx context.Context, provider string, groupName string) (*Group, error) {
	query := `SELECT internal_uuid, provider, group_name, name, description,
	          first_used, last_used, usage_count FROM groups WHERE provider = $1 AND group_name = $2`

	var group Group
	var name, description sql.NullString

	err := s.db.QueryRowContext(ctx, query, provider, groupName).Scan(
		&group.InternalUUID,
		&group.Provider,
		&group.GroupName,
		&name,
		&description,
		&group.FirstUsed,
		&group.LastUsed,
		&group.UsageCount,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("group not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get group: %w", err)
	}

	if name.Valid {
		group.Name = name.String
	}
	if description.Valid {
		group.Description = description.String
	}

	return &group, nil
}

// Create creates a new group (primarily for provider-independent groups)
func (s *GroupDatabaseStore) Create(ctx context.Context, group Group) error {
	query := `INSERT INTO groups (internal_uuid, provider, group_name, name, description, first_used, last_used, usage_count)
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

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

	_, err := s.db.ExecContext(ctx, query,
		group.InternalUUID,
		group.Provider,
		group.GroupName,
		nullString(group.Name),
		nullString(group.Description),
		group.FirstUsed,
		group.LastUsed,
		group.UsageCount,
	)
	if err != nil {
		// Check for duplicate key violation
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return fmt.Errorf("group already exists for provider")
		}
		return fmt.Errorf("failed to create group: %w", err)
	}

	return nil
}

// Update updates group metadata (name, description)
func (s *GroupDatabaseStore) Update(ctx context.Context, group Group) error {
	query := `UPDATE groups SET name = $1, description = $2, last_used = $3
	          WHERE internal_uuid = $4`

	result, err := s.db.ExecContext(ctx, query,
		nullString(group.Name),
		nullString(group.Description),
		time.Now().UTC(),
		group.InternalUUID,
	)
	if err != nil {
		return fmt.Errorf("failed to update group: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("group not found")
	}

	return nil
}

// Delete deletes a TMI-managed group by group_name (provider is always "*")
// Delegates to auth service for proper cleanup of threat models and relationships
func (s *GroupDatabaseStore) Delete(ctx context.Context, groupName string) (*GroupDeletionStats, error) {
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
func (s *GroupDatabaseStore) Count(ctx context.Context, filter GroupFilter) (int, error) {
	query := `SELECT COUNT(*) FROM groups WHERE 1=1`
	args := []interface{}{}
	argPos := 1

	// Apply same filters as List (excluding pagination)
	if filter.Provider != "" {
		query += fmt.Sprintf(" AND provider = $%d", argPos)
		args = append(args, filter.Provider)
		argPos++
	}

	if filter.GroupName != "" {
		query += fmt.Sprintf(" AND LOWER(group_name) LIKE LOWER($%d)", argPos)
		args = append(args, "%"+filter.GroupName+"%")
		// argPos++ // Last use of argPos before non-parameterized query
	}

	if filter.UsedInAuthorizations != nil {
		if *filter.UsedInAuthorizations {
			query += ` AND EXISTS (SELECT 1 FROM threat_model_access WHERE group_internal_uuid = groups.internal_uuid)`
		} else {
			query += ` AND NOT EXISTS (SELECT 1 FROM threat_model_access WHERE group_internal_uuid = groups.internal_uuid)`
		}
	}

	var count int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count groups: %w", err)
	}

	return count, nil
}

// EnrichGroups adds related data to groups (usage in authorizations/admin grants)
func (s *GroupDatabaseStore) EnrichGroups(ctx context.Context, groups []Group) ([]Group, error) {
	logger := slogging.Get()

	if len(groups) == 0 {
		return groups, nil
	}

	enriched := make([]Group, len(groups))
	copy(enriched, groups)

	for i := range enriched {
		group := &enriched[i]

		// Check if used in threat_model_access
		authQuery := `SELECT EXISTS(SELECT 1 FROM threat_model_access WHERE group_internal_uuid = $1)`
		var usedInAuth bool
		err := s.db.QueryRowContext(ctx, authQuery, group.InternalUUID).Scan(&usedInAuth)
		if err != nil {
			logger.Warn("Failed to check authorization usage for group %s: %v", group.InternalUUID, err)
		} else {
			group.UsedInAuthorizations = usedInAuth
		}

		// Check if used in administrators table
		adminQuery := `SELECT EXISTS(SELECT 1 FROM administrators WHERE group_internal_uuid = $1)`
		var usedInAdmin bool
		err = s.db.QueryRowContext(ctx, adminQuery, group.InternalUUID).Scan(&usedInAdmin)
		if err != nil {
			logger.Warn("Failed to check admin grant usage for group %s: %v", group.InternalUUID, err)
		} else {
			group.UsedInAdminGrants = usedInAdmin
		}

		// Note: MemberCount would require querying the IdP - leave as 0 for now
	}

	return enriched, nil
}

// GetGroupsForProvider returns all groups for a specific provider (for UI autocomplete)
func (s *GroupDatabaseStore) GetGroupsForProvider(ctx context.Context, provider string) ([]Group, error) {
	filter := GroupFilter{
		Provider:  provider,
		SortBy:    "last_used",
		SortOrder: "DESC",
		Limit:     500, // Reasonable limit for autocomplete
	}
	return s.List(ctx, filter)
}

// nullString returns a sql.NullString for optional string fields
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}
