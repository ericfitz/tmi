package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"gorm.io/gorm"
)

// maxTeamRelationshipDepth is the maximum depth for cycle detection and transitive queries
const maxTeamRelationshipDepth = 10

// TeamFilters defines filtering criteria for listing teams
type TeamFilters struct {
	Name         *string
	Status       []string
	MemberUserID *string
	RelatedTo    *string
	Relationship *string
	Transitive   *bool
}

// TeamStoreInterface defines the store interface for teams
type TeamStoreInterface interface {
	Create(ctx context.Context, team *Team, userInternalUUID string) (*Team, error)
	Get(ctx context.Context, id string) (*Team, error)
	Update(ctx context.Context, id string, team *Team, userInternalUUID string) (*Team, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, limit, offset int, filters *TeamFilters, userInternalUUID string, isAdmin bool) ([]TeamListItem, int, error)
	IsMember(ctx context.Context, teamID string, userInternalUUID string) (bool, error)
	HasProjects(ctx context.Context, teamID string) (bool, error)
}

// GormTeamStore implements TeamStoreInterface using GORM for database persistence
type GormTeamStore struct {
	db *gorm.DB
}

// NewGormTeamStore creates a new GORM-backed team store
func NewGormTeamStore(db *gorm.DB) *GormTeamStore {
	return &GormTeamStore{db: db}
}

// uuidToString converts an openapi_types.UUID to its string representation
func uuidToString(id openapi_types.UUID) string {
	return id.String()
}

// stringToUUID converts a string to an openapi_types.UUID
func stringToUUID(s string) openapi_types.UUID {
	return uuid.MustParse(s)
}

// Create creates a new team, auto-adding the creator as a member with engineering_lead role
func (s *GormTeamStore) Create(ctx context.Context, team *Team, userInternalUUID string) (*Team, error) {
	logger := slogging.Get()
	logger.Debug("Creating team: %s", team.Name)

	// Validate relationships before starting transaction
	if team.RelatedTeams != nil {
		for _, rel := range *team.RelatedTeams {
			teamID := ""
			if team.Id != nil {
				teamID = uuidToString(*team.Id)
			}
			if err := s.validateRelationship(ctx, teamID, uuidToString(rel.RelatedTeamId), string(rel.Relationship)); err != nil {
				return nil, err
			}
		}
	}

	// Generate ID if not provided
	if team.Id == nil {
		id := uuid.New()
		team.Id = &id
	}
	teamID := uuidToString(*team.Id)

	// Begin transaction
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Create the team record
		record := &models.TeamRecord{
			ID:                    teamID,
			Name:                  team.Name,
			Description:           team.Description,
			URI:                   team.Uri,
			Status:                team.Status,
			CreatedByInternalUUID: userInternalUUID,
		}

		if team.EmailAddress != nil {
			emailStr := string(*team.EmailAddress)
			record.EmailAddress = &emailStr
		}

		if err := tx.Create(record).Error; err != nil {
			logger.Error("Failed to create team record: %v", err)
			return fmt.Errorf("failed to create team: %w", err)
		}

		// Auto-add creator as member with engineering_lead role
		creatorRole := TeamMemberRoleEngineeringLead
		creatorMember := &models.TeamMemberRecord{
			TeamID:           teamID,
			UserInternalUUID: userInternalUUID,
			Role:             string(creatorRole),
		}
		if err := tx.Create(creatorMember).Error; err != nil {
			logger.Error("Failed to add creator as team member: %v", err)
			return fmt.Errorf("failed to add creator as team member: %w", err)
		}

		// Save additional members (if any)
		if team.Members != nil {
			for _, member := range *team.Members {
				memberUUID := uuidToString(member.UserId)
				// Skip if this is the creator (already added)
				if memberUUID == userInternalUUID {
					continue
				}
				role := "engineer"
				if member.Role != nil {
					role = string(*member.Role)
				}
				rec := &models.TeamMemberRecord{
					TeamID:           teamID,
					UserInternalUUID: memberUUID,
					Role:             role,
					CustomRole:       member.CustomRole,
				}
				if err := tx.Create(rec).Error; err != nil {
					logger.Error("Failed to add team member %s: %v", memberUUID, err)
					return fmt.Errorf("failed to add team member: %w", err)
				}
			}
		}

		// Save responsible parties
		if team.ResponsibleParties != nil {
			for _, rp := range *team.ResponsibleParties {
				rpUUID := uuidToString(rp.UserId)
				role := ""
				if rp.Role != nil {
					role = string(*rp.Role)
				}
				rec := &models.TeamResponsiblePartyRecord{
					TeamID:           teamID,
					UserInternalUUID: rpUUID,
					Role:             role,
					CustomRole:       rp.CustomRole,
				}
				if err := tx.Create(rec).Error; err != nil {
					logger.Error("Failed to add responsible party %s: %v", rpUUID, err)
					return fmt.Errorf("failed to add responsible party: %w", err)
				}
			}
		}

		// Save relationships
		if team.RelatedTeams != nil {
			for _, rel := range *team.RelatedTeams {
				rec := &models.TeamRelationshipRecord{
					TeamID:             teamID,
					RelatedTeamID:      uuidToString(rel.RelatedTeamId),
					Relationship:       string(rel.Relationship),
					CustomRelationship: rel.CustomRelationship,
				}
				if err := tx.Create(rec).Error; err != nil {
					logger.Error("Failed to add team relationship: %v", err)
					return fmt.Errorf("failed to add team relationship: %w", err)
				}
			}
		}

		// Save metadata
		if team.Metadata != nil && len(*team.Metadata) > 0 {
			if err := saveEntityMetadata(tx, "team", teamID, *team.Metadata); err != nil {
				logger.Error("Failed to save team metadata: %v", err)
				return fmt.Errorf("failed to save team metadata: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Return full team via Get
	return s.Get(ctx, teamID)
}

// Get retrieves a team by ID with all associated data
func (s *GormTeamStore) Get(ctx context.Context, id string) (*Team, error) {
	logger := slogging.Get()
	logger.Debug("Getting team: %s", id)

	// Load the team record with user preloads
	var record models.TeamRecord
	result := s.db.WithContext(ctx).
		Preload("CreatedBy").
		Preload("ModifiedBy").
		Preload("ReviewedBy").
		First(&record, "id = ?", id)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, NotFoundError(fmt.Sprintf("team not found: %s", id))
		}
		logger.Error("Failed to get team from database: %v", result.Error)
		return nil, fmt.Errorf("failed to get team: %w", result.Error)
	}

	// Load members with user preload
	var memberRecords []models.TeamMemberRecord
	if err := s.db.WithContext(ctx).
		Preload("User").
		Where(map[string]any{"team_id": id}).
		Find(&memberRecords).Error; err != nil {
		logger.Error("Failed to load team members: %v", err)
		return nil, fmt.Errorf("failed to load team members: %w", err)
	}

	// Load responsible parties with user preload
	var rpRecords []models.TeamResponsiblePartyRecord
	if err := s.db.WithContext(ctx).
		Preload("User").
		Where(map[string]any{"team_id": id}).
		Find(&rpRecords).Error; err != nil {
		logger.Error("Failed to load team responsible parties: %v", err)
		return nil, fmt.Errorf("failed to load team responsible parties: %w", err)
	}

	// Load relationships
	var relRecords []models.TeamRelationshipRecord
	if err := s.db.WithContext(ctx).
		Where(map[string]any{"team_id": id}).
		Find(&relRecords).Error; err != nil {
		logger.Error("Failed to load team relationships: %v", err)
		return nil, fmt.Errorf("failed to load team relationships: %w", err)
	}

	// Load metadata
	metadata, err := loadEntityMetadata(s.db.WithContext(ctx), "team", id)
	if err != nil {
		logger.Error("Failed to load team metadata: %v", err)
		metadata = []Metadata{}
	}

	// Convert to API type
	team := s.recordToAPI(&record, memberRecords, rpRecords, relRecords, metadata)

	logger.Debug("Successfully retrieved team: %s", id)
	return team, nil
}

// Update updates an existing team, replacing members, responsible parties, and relationships
func (s *GormTeamStore) Update(ctx context.Context, id string, team *Team, userInternalUUID string) (*Team, error) {
	logger := slogging.Get()
	logger.Debug("Updating team: %s", id)

	// Validate relationships before starting transaction
	if team.RelatedTeams != nil {
		for _, rel := range *team.RelatedTeams {
			if err := s.validateRelationship(ctx, id, uuidToString(rel.RelatedTeamId), string(rel.Relationship)); err != nil {
				return nil, err
			}
		}
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Verify team exists
		var existing models.TeamRecord
		if err := tx.First(&existing, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NotFoundError(fmt.Sprintf("team not found: %s", id))
			}
			return fmt.Errorf("failed to find team: %w", err)
		}

		// Update team record fields
		updates := map[string]any{
			"name":                      team.Name,
			"description":               team.Description,
			"uri":                       team.Uri,
			"status":                    team.Status,
			"modified_by_internal_uuid": &userInternalUUID,
		}

		if team.EmailAddress != nil {
			emailStr := string(*team.EmailAddress)
			updates["email_address"] = &emailStr
		} else {
			updates["email_address"] = nil
		}

		if team.ReviewedAt != nil {
			updates["reviewed_at"] = team.ReviewedAt
			updates["reviewed_by_internal_uuid"] = &userInternalUUID
		}

		if err := tx.Model(&models.TeamRecord{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			logger.Error("Failed to update team record: %v", err)
			return fmt.Errorf("failed to update team: %w", err)
		}

		// Replace members: delete all then recreate
		if err := tx.Where(map[string]any{"team_id": id}).Delete(&models.TeamMemberRecord{}).Error; err != nil {
			logger.Error("Failed to delete team members: %v", err)
			return fmt.Errorf("failed to delete team members: %w", err)
		}
		if team.Members != nil {
			for _, member := range *team.Members {
				role := "engineer"
				if member.Role != nil {
					role = string(*member.Role)
				}
				rec := &models.TeamMemberRecord{
					TeamID:           id,
					UserInternalUUID: uuidToString(member.UserId),
					Role:             role,
					CustomRole:       member.CustomRole,
				}
				if err := tx.Create(rec).Error; err != nil {
					logger.Error("Failed to create team member: %v", err)
					return fmt.Errorf("failed to create team member: %w", err)
				}
			}
		}

		// Replace responsible parties: delete all then recreate
		if err := tx.Where(map[string]any{"team_id": id}).Delete(&models.TeamResponsiblePartyRecord{}).Error; err != nil {
			logger.Error("Failed to delete team responsible parties: %v", err)
			return fmt.Errorf("failed to delete team responsible parties: %w", err)
		}
		if team.ResponsibleParties != nil {
			for _, rp := range *team.ResponsibleParties {
				role := ""
				if rp.Role != nil {
					role = string(*rp.Role)
				}
				rec := &models.TeamResponsiblePartyRecord{
					TeamID:           id,
					UserInternalUUID: uuidToString(rp.UserId),
					Role:             role,
					CustomRole:       rp.CustomRole,
				}
				if err := tx.Create(rec).Error; err != nil {
					logger.Error("Failed to create responsible party: %v", err)
					return fmt.Errorf("failed to create responsible party: %w", err)
				}
			}
		}

		// Replace relationships: delete all then recreate
		if err := tx.Where(map[string]any{"team_id": id}).Delete(&models.TeamRelationshipRecord{}).Error; err != nil {
			logger.Error("Failed to delete team relationships: %v", err)
			return fmt.Errorf("failed to delete team relationships: %w", err)
		}
		if team.RelatedTeams != nil {
			for _, rel := range *team.RelatedTeams {
				rec := &models.TeamRelationshipRecord{
					TeamID:             id,
					RelatedTeamID:      uuidToString(rel.RelatedTeamId),
					Relationship:       string(rel.Relationship),
					CustomRelationship: rel.CustomRelationship,
				}
				if err := tx.Create(rec).Error; err != nil {
					logger.Error("Failed to create team relationship: %v", err)
					return fmt.Errorf("failed to create team relationship: %w", err)
				}
			}
		}

		// Replace metadata
		if team.Metadata != nil {
			if err := deleteAndSaveEntityMetadata(tx, "team", id, *team.Metadata); err != nil {
				logger.Error("Failed to save team metadata: %v", err)
				return fmt.Errorf("failed to save team metadata: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Return full team via Get
	return s.Get(ctx, id)
}

// Delete removes a team and all associated data
func (s *GormTeamStore) Delete(ctx context.Context, id string) error {
	logger := slogging.Get()
	logger.Debug("Deleting team: %s", id)

	// Check if team has associated projects
	hasProjects, err := s.HasProjects(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to check team projects: %w", err)
	}
	if hasProjects {
		return ConflictError("cannot delete team: team has associated projects")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Verify team exists
		var existing models.TeamRecord
		if err := tx.First(&existing, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NotFoundError(fmt.Sprintf("team not found: %s", id))
			}
			return fmt.Errorf("failed to find team: %w", err)
		}

		// Delete in reverse dependency order:
		// 1. Relationships (both directions)
		if err := tx.Where(map[string]any{"team_id": id}).Delete(&models.TeamRelationshipRecord{}).Error; err != nil {
			logger.Error("Failed to delete team relationships (as source): %v", err)
			return fmt.Errorf("failed to delete team relationships: %w", err)
		}
		if err := tx.Where(map[string]any{"related_team_id": id}).Delete(&models.TeamRelationshipRecord{}).Error; err != nil {
			logger.Error("Failed to delete team relationships (as target): %v", err)
			return fmt.Errorf("failed to delete team relationships: %w", err)
		}

		// 2. Responsible parties
		if err := tx.Where(map[string]any{"team_id": id}).Delete(&models.TeamResponsiblePartyRecord{}).Error; err != nil {
			logger.Error("Failed to delete team responsible parties: %v", err)
			return fmt.Errorf("failed to delete team responsible parties: %w", err)
		}

		// 3. Members
		if err := tx.Where(map[string]any{"team_id": id}).Delete(&models.TeamMemberRecord{}).Error; err != nil {
			logger.Error("Failed to delete team members: %v", err)
			return fmt.Errorf("failed to delete team members: %w", err)
		}

		// 4. Metadata
		if err := tx.Where("entity_type = ? AND entity_id = ?", "team", id).Delete(&models.Metadata{}).Error; err != nil {
			logger.Error("Failed to delete team metadata: %v", err)
			return fmt.Errorf("failed to delete team metadata: %w", err)
		}

		// 5. Team record itself
		if err := tx.Delete(&models.TeamRecord{}, "id = ?", id).Error; err != nil {
			logger.Error("Failed to delete team record: %v", err)
			return fmt.Errorf("failed to delete team: %w", err)
		}

		logger.Debug("Successfully deleted team: %s", id)
		return nil
	})
}

// List retrieves teams with filtering, pagination, and access control
func (s *GormTeamStore) List(ctx context.Context, limit, offset int, filters *TeamFilters, userInternalUUID string, isAdmin bool) ([]TeamListItem, int, error) {
	logger := slogging.Get()
	logger.Debug("Listing teams (limit=%d, offset=%d, isAdmin=%t)", limit, offset, isAdmin)

	teamTable := models.TeamRecord{}.TableName()
	memberTable := models.TeamMemberRecord{}.TableName()

	query := s.db.WithContext(ctx).Table(teamTable)

	// Access control: non-admin users can only see teams they belong to
	if !isAdmin {
		query = query.Where(
			fmt.Sprintf("EXISTS (SELECT 1 FROM %s WHERE %s.team_id = %s.id AND %s.user_internal_uuid = ?)",
				memberTable, memberTable, teamTable, memberTable),
			userInternalUUID,
		)
	}

	// Apply name filter (case-insensitive partial match)
	if filters != nil && filters.Name != nil && *filters.Name != "" {
		namePattern := "%" + strings.ToLower(*filters.Name) + "%"
		query = query.Where(fmt.Sprintf("LOWER(%s.name) LIKE ?", teamTable), namePattern)
	}

	// Apply status filter (exact match, supports multiple values)
	if filters != nil && len(filters.Status) > 0 {
		query = query.Where(fmt.Sprintf("%s.status IN ?", teamTable), filters.Status)
	}

	// Apply member_user_id filter
	if filters != nil && filters.MemberUserID != nil && *filters.MemberUserID != "" {
		query = query.Where(
			fmt.Sprintf("EXISTS (SELECT 1 FROM %s WHERE %s.team_id = %s.id AND %s.user_internal_uuid = ?)",
				memberTable, memberTable, teamTable, memberTable),
			*filters.MemberUserID,
		)
	}

	// Apply related_to + relationship filter (optionally transitive)
	if filters != nil && filters.RelatedTo != nil && *filters.RelatedTo != "" {
		relatedTeamIDs, err := s.resolveRelatedTeamIDs(ctx, filters)
		if err != nil {
			logger.Error("Failed to resolve related team IDs: %v", err)
			return nil, 0, fmt.Errorf("failed to resolve related teams: %w", err)
		}
		if len(relatedTeamIDs) > 0 {
			query = query.Where(fmt.Sprintf("%s.id IN ?", teamTable), relatedTeamIDs)
		} else {
			// No matching related teams found; return empty result
			return []TeamListItem{}, 0, nil
		}
	}

	// Get total count before pagination
	var totalCount int64
	countQuery := query.Session(&gorm.Session{})
	if err := countQuery.Count(&totalCount).Error; err != nil {
		logger.Error("Failed to count teams: %v", err)
		return nil, 0, fmt.Errorf("failed to count teams: %w", err)
	}

	// Apply pagination and ordering
	query = query.Order(fmt.Sprintf("%s.created_at DESC", teamTable))
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	// Execute query
	var records []models.TeamRecord
	if err := query.Find(&records).Error; err != nil {
		logger.Error("Failed to list teams: %v", err)
		return nil, 0, fmt.Errorf("failed to list teams: %w", err)
	}

	// Convert to TeamListItem with counts
	items := make([]TeamListItem, 0, len(records))
	for _, rec := range records {
		// Get member count
		var memberCount int64
		s.db.WithContext(ctx).Model(&models.TeamMemberRecord{}).
			Where(map[string]any{"team_id": rec.ID}).
			Count(&memberCount)
		mc := int(memberCount)

		// Get project count
		var projectCount int64
		s.db.WithContext(ctx).Model(&models.ProjectRecord{}).
			Where(map[string]any{"team_id": rec.ID}).
			Count(&projectCount)
		pc := int(projectCount)

		item := TeamListItem{
			Id:           stringToUUID(rec.ID),
			Name:         rec.Name,
			Description:  rec.Description,
			Status:       rec.Status,
			CreatedAt:    rec.CreatedAt,
			MemberCount:  &mc,
			ProjectCount: &pc,
		}
		if !rec.ModifiedAt.IsZero() {
			item.ModifiedAt = &rec.ModifiedAt
		}

		items = append(items, item)
	}

	logger.Debug("Successfully listed %d teams (total: %d)", len(items), totalCount)
	return items, int(totalCount), nil
}

// IsMember checks if a user is a member of a team
func (s *GormTeamStore) IsMember(ctx context.Context, teamID string, userInternalUUID string) (bool, error) {
	logger := slogging.Get()
	logger.Debug("Checking membership: team=%s, user=%s", teamID, userInternalUUID)

	var count int64
	result := s.db.WithContext(ctx).Model(&models.TeamMemberRecord{}).
		Where(map[string]any{"team_id": teamID, "user_internal_uuid": userInternalUUID}).
		Count(&count)

	if result.Error != nil {
		logger.Error("Failed to check team membership: %v", result.Error)
		return false, fmt.Errorf("failed to check team membership: %w", result.Error)
	}

	return count > 0, nil
}

// HasProjects checks if a team has any associated projects
func (s *GormTeamStore) HasProjects(ctx context.Context, teamID string) (bool, error) {
	logger := slogging.Get()
	logger.Debug("Checking projects for team: %s", teamID)

	var count int64
	result := s.db.WithContext(ctx).Model(&models.ProjectRecord{}).
		Where(map[string]any{"team_id": teamID}).
		Count(&count)

	if result.Error != nil {
		logger.Error("Failed to check team projects: %v", result.Error)
		return false, fmt.Errorf("failed to check team projects: %w", result.Error)
	}

	return count > 0, nil
}

// validateRelationship validates a relationship between two teams
func (s *GormTeamStore) validateRelationship(ctx context.Context, teamID, relatedTeamID, relationship string) error {
	// Self-reference check
	if teamID != "" && teamID == relatedTeamID {
		return InvalidInputError("a team cannot have a relationship with itself")
	}

	// Verify the related team exists
	if relatedTeamID != "" {
		var count int64
		s.db.WithContext(ctx).Model(&models.TeamRecord{}).
			Where("id = ?", relatedTeamID).
			Count(&count)
		if count == 0 {
			return InvalidInputError(fmt.Sprintf("related team not found: %s", relatedTeamID))
		}
	}

	// Cycle detection for directional relationships
	if teamID != "" && directionalRelationships[relationship] {
		if err := s.detectCycle(ctx, teamID, relatedTeamID, relationship); err != nil {
			return err
		}
	}

	return nil
}

// detectCycle checks if adding a relationship would create a cycle in the relationship graph.
// For "parent": adding A->parent->B means B is A's parent. Check if A is already an ancestor of B.
// For "child": adding A->child->B means B is A's child. Check if B is already an ancestor of A.
// For "supersedes": adding A->supersedes->B. Check if B already supersedes A (directly or transitively).
// For "superseded_by": adding A->superseded_by->B. Check if A already supersedes B.
func (s *GormTeamStore) detectCycle(ctx context.Context, teamID, relatedTeamID, relationship string) error {
	logger := slogging.Get()

	// Determine the traversal direction based on relationship type
	// We need to check if following the chain from relatedTeamID leads back to teamID
	var traverseRelationship string
	switch relationship {
	case string(RelationshipTypeParent):
		// A->parent->B: B is A's parent. Traverse B's parents to check if A is found.
		traverseRelationship = string(RelationshipTypeParent)
	case string(RelationshipTypeChild):
		// A->child->B: B is A's child. Traverse B's children to check if A is found.
		traverseRelationship = string(RelationshipTypeChild)
	case string(RelationshipTypeSupersedes):
		// A->supersedes->B: traverse B's supersedes chain.
		traverseRelationship = string(RelationshipTypeSupersedes)
	case string(RelationshipTypeSupersededBy):
		// A->superseded_by->B: traverse B's superseded_by chain.
		traverseRelationship = string(RelationshipTypeSupersededBy)
	default:
		return nil
	}

	// Iteratively traverse the relationship chain from relatedTeamID
	visited := map[string]bool{teamID: true}
	frontier := []string{relatedTeamID}

	for depth := 0; depth < maxTeamRelationshipDepth && len(frontier) > 0; depth++ {
		var nextFrontier []string

		for _, currentID := range frontier {
			if visited[currentID] {
				return InvalidInputError(fmt.Sprintf(
					"adding %s relationship from team %s to team %s would create a cycle",
					relationship, teamID, relatedTeamID,
				))
			}
			visited[currentID] = true

			// Find all teams related to currentID via the traversal relationship
			var relIDs []string
			s.db.WithContext(ctx).Model(&models.TeamRelationshipRecord{}).
				Where(map[string]any{"team_id": currentID, "relationship": traverseRelationship}).
				Pluck("related_team_id", &relIDs)

			nextFrontier = append(nextFrontier, relIDs...)
		}

		frontier = nextFrontier
	}

	logger.Debug("No cycle detected for %s relationship: %s -> %s", relationship, teamID, relatedTeamID)
	return nil
}

// resolveRelatedTeamIDs resolves the set of team IDs matching a related_to + relationship filter,
// optionally following transitive relationships.
func (s *GormTeamStore) resolveRelatedTeamIDs(ctx context.Context, filters *TeamFilters) ([]string, error) {
	if filters.RelatedTo == nil || *filters.RelatedTo == "" {
		return nil, nil
	}

	relatedTo := *filters.RelatedTo
	relationship := ""
	if filters.Relationship != nil {
		relationship = *filters.Relationship
	}

	transitive := false
	if filters.Transitive != nil {
		transitive = *filters.Transitive
	}

	// If transitive and directional, follow the chain
	if transitive && relationship != "" && directionalRelationships[relationship] {
		return s.resolveTransitiveRelatedTeams(ctx, relatedTo, relationship)
	}

	// Non-transitive: simple query for directly related teams
	query := s.db.WithContext(ctx).Model(&models.TeamRelationshipRecord{}).
		Where(map[string]any{"related_team_id": relatedTo})

	if relationship != "" {
		// Find teams that have the given relationship TO the relatedTo team.
		// If A has relationship "parent" to B, and we're looking for teams related to B
		// with relationship "parent", we want teams where related_team_id=B AND relationship matches.
		// But the inverse: looking for teams where team_id=relatedTo and collecting related_team_id
		// OR where related_team_id=relatedTo and collecting team_id.

		// Teams that point TO relatedTo with the given relationship
		var teamIDs1 []string
		s.db.WithContext(ctx).Model(&models.TeamRelationshipRecord{}).
			Where(map[string]any{"related_team_id": relatedTo, "relationship": relationship}).
			Pluck("team_id", &teamIDs1)

		// Teams that relatedTo points TO with the inverse relationship
		var teamIDs2 []string
		if inv, ok := inverseRelationship[relationship]; ok {
			s.db.WithContext(ctx).Model(&models.TeamRelationshipRecord{}).
				Where(map[string]any{"team_id": relatedTo, "relationship": inv}).
				Pluck("related_team_id", &teamIDs2)
		}

		// Also check: teams where team_id=relatedTo with the given relationship
		var teamIDs3 []string
		s.db.WithContext(ctx).Model(&models.TeamRelationshipRecord{}).
			Where(map[string]any{"team_id": relatedTo, "relationship": relationship}).
			Pluck("related_team_id", &teamIDs3)

		// Deduplicate
		seen := make(map[string]bool)
		var result []string
		for _, ids := range [][]string{teamIDs1, teamIDs2, teamIDs3} {
			for _, id := range ids {
				if !seen[id] {
					seen[id] = true
					result = append(result, id)
				}
			}
		}
		return result, nil
	}

	// No specific relationship filter: get all related teams in both directions
	var teamIDs1 []string
	query.Pluck("team_id", &teamIDs1)

	var teamIDs2 []string
	s.db.WithContext(ctx).Model(&models.TeamRelationshipRecord{}).
		Where(map[string]any{"team_id": relatedTo}).
		Pluck("related_team_id", &teamIDs2)

	// Deduplicate
	seen := make(map[string]bool)
	var result []string
	for _, ids := range [][]string{teamIDs1, teamIDs2} {
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				result = append(result, id)
			}
		}
	}
	return result, nil
}

// resolveTransitiveRelatedTeams follows a directional relationship chain iteratively up to maxTeamRelationshipDepth
func (s *GormTeamStore) resolveTransitiveRelatedTeams(ctx context.Context, startTeamID string, relationship string) ([]string, error) {
	logger := slogging.Get()
	logger.Debug("Resolving transitive %s relationships from team %s", relationship, startTeamID)

	visited := map[string]bool{startTeamID: true}
	frontier := []string{startTeamID}
	var allRelated []string

	for depth := 0; depth < maxTeamRelationshipDepth && len(frontier) > 0; depth++ {
		var nextFrontier []string

		for _, currentID := range frontier {
			// Find teams related via the specified relationship from currentID
			var relIDs []string
			s.db.WithContext(ctx).Model(&models.TeamRelationshipRecord{}).
				Where(map[string]any{"team_id": currentID, "relationship": relationship}).
				Pluck("related_team_id", &relIDs)

			for _, relID := range relIDs {
				if !visited[relID] {
					visited[relID] = true
					allRelated = append(allRelated, relID)
					nextFrontier = append(nextFrontier, relID)
				}
			}
		}

		frontier = nextFrontier
	}

	logger.Debug("Found %d transitive related teams", len(allRelated))
	return allRelated, nil
}

// recordToAPI converts database records to the API Team type
func (s *GormTeamStore) recordToAPI(
	record *models.TeamRecord,
	members []models.TeamMemberRecord,
	rps []models.TeamResponsiblePartyRecord,
	rels []models.TeamRelationshipRecord,
	metadata []Metadata,
) *Team {
	teamID := stringToUUID(record.ID)

	team := &Team{
		Id:        &teamID,
		Name:      record.Name,
		CreatedAt: &record.CreatedAt,
	}

	// Optional fields
	if record.Description != nil {
		team.Description = record.Description
	}
	if record.URI != nil {
		team.Uri = record.URI
	}
	if record.Status != nil {
		team.Status = record.Status
	}
	if record.EmailAddress != nil {
		email := openapi_types.Email(*record.EmailAddress)
		team.EmailAddress = &email
	}
	if !record.ModifiedAt.IsZero() {
		team.ModifiedAt = &record.ModifiedAt
	}
	if record.ReviewedAt != nil {
		team.ReviewedAt = record.ReviewedAt
	}

	// Convert CreatedBy user - null out if user no longer exists
	if record.CreatedByInternalUUID != "" && record.CreatedBy.InternalUUID != "" {
		team.CreatedBy = userModelToAPI(&record.CreatedBy)
	}

	// Convert ModifiedBy user - null out if user no longer exists
	if record.ModifiedBy != nil && record.ModifiedBy.InternalUUID != "" {
		team.ModifiedBy = userModelToAPI(record.ModifiedBy)
	}

	// Convert ReviewedBy user - null out if user no longer exists
	if record.ReviewedBy != nil && record.ReviewedBy.InternalUUID != "" {
		team.ReviewedBy = userModelToAPI(record.ReviewedBy)
	}

	// Convert members
	apiMembers := make([]TeamMember, 0, len(members))
	for _, m := range members {
		member := TeamMember{
			UserId: stringToUUID(m.UserInternalUUID),
		}
		role := TeamMemberRole(m.Role)
		member.Role = &role
		if m.CustomRole != nil {
			member.CustomRole = m.CustomRole
		}
		// Populate user details if available
		if m.User.InternalUUID != "" {
			member.User = userModelToAPI(&m.User)
		}
		apiMembers = append(apiMembers, member)
	}
	team.Members = &apiMembers

	// Convert responsible parties
	apiRPs := make([]ResponsibleParty, 0, len(rps))
	for _, rp := range rps {
		party := ResponsibleParty{
			UserId: stringToUUID(rp.UserInternalUUID),
		}
		role := TeamMemberRole(rp.Role)
		party.Role = &role
		if rp.CustomRole != nil {
			party.CustomRole = rp.CustomRole
		}
		// Populate user details if available
		if rp.User.InternalUUID != "" {
			party.User = userModelToAPI(&rp.User)
		}
		apiRPs = append(apiRPs, party)
	}
	team.ResponsibleParties = &apiRPs

	// Convert relationships
	apiRels := make([]RelatedTeam, 0, len(rels))
	for _, rel := range rels {
		related := RelatedTeam{
			RelatedTeamId:      stringToUUID(rel.RelatedTeamID),
			Relationship:       RelationshipType(rel.Relationship),
			CustomRelationship: rel.CustomRelationship,
		}
		apiRels = append(apiRels, related)
	}
	team.RelatedTeams = &apiRels

	// Set metadata
	team.Metadata = &metadata

	return team
}
