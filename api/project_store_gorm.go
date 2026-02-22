package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ProjectFilters defines filtering criteria for listing projects
type ProjectFilters struct {
	Name         *string
	Status       []string
	TeamID       *string
	RelatedTo    *string
	Relationship *string
	Transitive   *bool
}

// ProjectStoreInterface defines the store interface for projects
type ProjectStoreInterface interface {
	Create(ctx context.Context, project *Project, userInternalUUID string) (*Project, error)
	Get(ctx context.Context, id string) (*Project, error)
	Update(ctx context.Context, id string, project *Project, userInternalUUID string) (*Project, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, limit, offset int, filters *ProjectFilters, userInternalUUID string, isAdmin bool) ([]ProjectListItem, int, error)
	GetTeamID(ctx context.Context, projectID string) (string, error)
	HasThreatModels(ctx context.Context, projectID string) (bool, error)
}

// GormProjectStore implements ProjectStoreInterface using GORM
type GormProjectStore struct {
	db *gorm.DB
}

// NewGormProjectStore creates a new GORM-backed project store
func NewGormProjectStore(db *gorm.DB) *GormProjectStore {
	return &GormProjectStore{db: db}
}

// Create creates a new project
func (s *GormProjectStore) Create(ctx context.Context, project *Project, userInternalUUID string) (*Project, error) {
	logger := slogging.Get()

	// Generate ID if not provided
	if project.Id == nil {
		id := uuid.New()
		project.Id = &id
	}

	// Validate team_id exists
	var teamCount int64
	if err := s.db.WithContext(ctx).Model(&models.TeamRecord{}).
		Where(map[string]any{"id": project.TeamId.String()}).
		Count(&teamCount).Error; err != nil {
		logger.Error("failed to check team existence: %v", err)
		return nil, ServerError("failed to validate team")
	}
	if teamCount == 0 {
		return nil, InvalidInputError(fmt.Sprintf("team not found: %s", project.TeamId))
	}

	// Validate relationships if provided
	if project.RelatedProjects != nil {
		if err := s.validateProjectRelationships(ctx, project.Id.String(), *project.RelatedProjects); err != nil {
			return nil, err
		}
	}

	// Build the project record
	record := models.ProjectRecord{
		ID:                    project.Id.String(),
		Name:                  project.Name,
		Description:           project.Description,
		TeamID:                project.TeamId.String(),
		URI:                   project.Uri,
		Status:                project.Status,
		CreatedByInternalUUID: userInternalUUID,
	}

	// Begin transaction
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, ServerError(fmt.Sprintf("failed to begin transaction: %v", tx.Error))
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Create the project record
	if err := tx.Create(&record).Error; err != nil {
		tx.Rollback()
		logger.Error("failed to create project: %v", err)
		return nil, ServerError("failed to create project")
	}

	// Save responsible parties
	if project.ResponsibleParties != nil {
		if err := s.saveResponsibleParties(tx, record.ID, *project.ResponsibleParties); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	// Save relationships
	if project.RelatedProjects != nil {
		if err := s.saveRelationships(tx, record.ID, *project.RelatedProjects); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		return nil, ServerError(fmt.Sprintf("failed to commit transaction: %v", err))
	}

	// Save metadata outside the transaction (uses its own transaction internally)
	if project.Metadata != nil && len(*project.Metadata) > 0 {
		if err := saveEntityMetadata(s.db.WithContext(ctx), "project", record.ID, *project.Metadata); err != nil {
			logger.Error("failed to save metadata for project: id=%s, error=%v", record.ID, err)
			return nil, ServerError("failed to save metadata")
		}
	}

	logger.Info("project created: id=%s, name=%s, team_id=%s", record.ID, record.Name, record.TeamID)

	// Return the full project via Get
	return s.Get(ctx, record.ID)
}

// Get retrieves a project by ID
func (s *GormProjectStore) Get(ctx context.Context, id string) (*Project, error) {
	logger := slogging.Get()

	var record models.ProjectRecord
	result := s.db.WithContext(ctx).
		Preload("Team").
		Preload("CreatedBy").
		Preload("ModifiedBy").
		Preload("ReviewedBy").
		First(&record, map[string]any{"id": id})

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			logger.Debug("project not found: id=%s", id)
			return nil, NotFoundError(fmt.Sprintf("project not found: %s", id))
		}
		logger.Error("failed to get project: id=%s, error=%v", id, result.Error)
		return nil, ServerError("failed to get project")
	}

	// Load responsible parties
	responsibleParties, err := s.loadResponsibleParties(ctx, id)
	if err != nil {
		logger.Error("failed to load responsible parties: id=%s, error=%v", id, err)
		return nil, ServerError("failed to load responsible parties")
	}

	// Load relationships
	relationships, err := s.loadRelationships(ctx, id)
	if err != nil {
		logger.Error("failed to load relationships: id=%s, error=%v", id, err)
		return nil, ServerError("failed to load relationships")
	}

	// Load metadata
	metadata, err := loadEntityMetadata(s.db.WithContext(ctx), "project", id)
	if err != nil {
		logger.Error("failed to load metadata: id=%s, error=%v", id, err)
		return nil, ServerError("failed to load metadata")
	}

	// Convert to API type
	project := s.recordToAPI(&record, responsibleParties, relationships, metadata)

	logger.Debug("retrieved project: id=%s, name=%s", id, record.Name)

	return project, nil
}

// Update updates an existing project
func (s *GormProjectStore) Update(ctx context.Context, id string, project *Project, userInternalUUID string) (*Project, error) {
	logger := slogging.Get()

	// Check that the project exists
	var existing models.ProjectRecord
	if err := s.db.WithContext(ctx).First(&existing, map[string]any{"id": id}).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, NotFoundError(fmt.Sprintf("project not found: %s", id))
		}
		return nil, ServerError("failed to get project")
	}

	// Validate team_id if changed
	newTeamID := project.TeamId.String()
	if newTeamID != existing.TeamID {
		var teamCount int64
		if err := s.db.WithContext(ctx).Model(&models.TeamRecord{}).
			Where(map[string]any{"id": newTeamID}).
			Count(&teamCount).Error; err != nil {
			logger.Error("failed to check team existence: %v", err)
			return nil, ServerError("failed to validate team")
		}
		if teamCount == 0 {
			return nil, InvalidInputError(fmt.Sprintf("team not found: %s", project.TeamId))
		}
	}

	// Validate relationships if provided
	if project.RelatedProjects != nil {
		if err := s.validateProjectRelationships(ctx, id, *project.RelatedProjects); err != nil {
			return nil, err
		}
	}

	// Begin transaction
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, ServerError(fmt.Sprintf("failed to begin transaction: %v", tx.Error))
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Update project record fields using map for cross-DB compatibility
	updates := map[string]any{
		"name":                      project.Name,
		"team_id":                   newTeamID,
		"modified_by_internal_uuid": userInternalUUID,
	}
	if project.Description != nil {
		updates["description"] = *project.Description
	}
	if project.Uri != nil {
		updates["uri"] = *project.Uri
	}
	if project.Status != nil {
		updates["status"] = *project.Status
	}

	if err := tx.Model(&models.ProjectRecord{}).
		Where(map[string]any{"id": id}).
		Updates(updates).Error; err != nil {
		tx.Rollback()
		logger.Error("failed to update project: id=%s, error=%v", id, err)
		return nil, ServerError("failed to update project")
	}

	// Delete and recreate responsible parties
	if project.ResponsibleParties != nil {
		if err := tx.Where(map[string]any{"project_id": id}).
			Delete(&models.ProjectResponsiblePartyRecord{}).Error; err != nil {
			tx.Rollback()
			return nil, ServerError("failed to delete responsible parties")
		}
		if err := s.saveResponsibleParties(tx, id, *project.ResponsibleParties); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	// Delete and recreate relationships
	if project.RelatedProjects != nil {
		if err := tx.Where("project_id = ? OR related_project_id = ?", id, id).
			Delete(&models.ProjectRelationshipRecord{}).Error; err != nil {
			tx.Rollback()
			return nil, ServerError("failed to delete relationships")
		}
		if err := s.saveRelationships(tx, id, *project.RelatedProjects); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		return nil, ServerError(fmt.Sprintf("failed to commit transaction: %v", err))
	}

	// Save metadata outside the transaction
	if project.Metadata != nil && len(*project.Metadata) > 0 {
		if err := saveEntityMetadata(s.db.WithContext(ctx), "project", id, *project.Metadata); err != nil {
			logger.Error("failed to save metadata for project: id=%s, error=%v", id, err)
			return nil, ServerError("failed to save metadata")
		}
	}

	logger.Info("project updated: id=%s", id)

	// Return the full project via Get
	return s.Get(ctx, id)
}

// Delete removes a project by ID
func (s *GormProjectStore) Delete(ctx context.Context, id string) error {
	logger := slogging.Get()

	// Check that the project exists
	var existing models.ProjectRecord
	if err := s.db.WithContext(ctx).First(&existing, map[string]any{"id": id}).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return NotFoundError(fmt.Sprintf("project not found: %s", id))
		}
		return ServerError("failed to get project")
	}

	// Check if the project has threat models
	hasTM, err := s.HasThreatModels(ctx, id)
	if err != nil {
		return ServerError("failed to check threat model references")
	}
	if hasTM {
		return ConflictError("cannot delete project: it has associated threat models")
	}

	// Begin transaction
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return ServerError(fmt.Sprintf("failed to begin transaction: %v", tx.Error))
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Delete relationships (both directions)
	if err := tx.Where("project_id = ? OR related_project_id = ?", id, id).
		Delete(&models.ProjectRelationshipRecord{}).Error; err != nil {
		tx.Rollback()
		return ServerError("failed to delete relationships")
	}

	// Delete responsible parties
	if err := tx.Where(map[string]any{"project_id": id}).
		Delete(&models.ProjectResponsiblePartyRecord{}).Error; err != nil {
		tx.Rollback()
		return ServerError("failed to delete responsible parties")
	}

	// Delete associated metadata
	if err := tx.Where("entity_type = ? AND entity_id = ?", "project", id).
		Delete(&models.Metadata{}).Error; err != nil {
		tx.Rollback()
		return ServerError("failed to delete metadata")
	}

	// Delete the project record
	if err := tx.Delete(&models.ProjectRecord{}, map[string]any{"id": id}).Error; err != nil {
		tx.Rollback()
		return ServerError("failed to delete project")
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		return ServerError(fmt.Sprintf("failed to commit transaction: %v", err))
	}

	logger.Info("project deleted: id=%s", id)

	return nil
}

// List retrieves projects with pagination and optional filters
func (s *GormProjectStore) List(ctx context.Context, limit, offset int, filters *ProjectFilters, userInternalUUID string, isAdmin bool) ([]ProjectListItem, int, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.ProjectRecord{}).
		Joins("LEFT JOIN " + models.TeamRecord{}.TableName() + " ON " + models.TeamRecord{}.TableName() + ".id = " + models.ProjectRecord{}.TableName() + ".team_id")

	// If not admin, filter by team membership
	if !isAdmin {
		teamMembersTable := models.TeamMemberRecord{}.TableName()
		query = query.Where(
			models.ProjectRecord{}.TableName()+".team_id IN (SELECT team_id FROM "+teamMembersTable+" WHERE user_internal_uuid = ?)",
			userInternalUUID,
		)
	}

	// Apply filters
	if filters != nil {
		if filters.Name != nil && *filters.Name != "" {
			query = query.Where(
				"LOWER("+models.ProjectRecord{}.TableName()+".name) LIKE ?",
				"%"+strings.ToLower(*filters.Name)+"%",
			)
		}

		if len(filters.Status) > 0 {
			query = query.Where(models.ProjectRecord{}.TableName()+".status IN ?", filters.Status)
		}

		if filters.TeamID != nil && *filters.TeamID != "" {
			query = query.Where(map[string]any{models.ProjectRecord{}.TableName() + ".team_id": *filters.TeamID})
		}

		// Apply related_to filter
		if filters.RelatedTo != nil && *filters.RelatedTo != "" {
			query = s.applyRelatedToFilter(query, *filters.RelatedTo, filters.Relationship, filters.Transitive)
		}
	}

	// Get total count before pagination
	var total int64
	countQuery := query.Session(&gorm.Session{})
	if err := countQuery.Count(&total).Error; err != nil {
		logger.Error("failed to count projects: error=%v", err)
		return nil, 0, ServerError("failed to count projects")
	}

	// Select specific columns and apply pagination
	projectsTable := models.ProjectRecord{}.TableName()
	teamsTable := models.TeamRecord{}.TableName()
	var results []struct {
		ID          string  `gorm:"column:id"`
		Name        string  `gorm:"column:name"`
		Description *string `gorm:"column:description"`
		Status      *string `gorm:"column:status"`
		TeamID      string  `gorm:"column:team_id"`
		TeamName    string  `gorm:"column:team_name"`
		CreatedAt   string  `gorm:"column:created_at"`
		ModifiedAt  string  `gorm:"column:modified_at"`
	}

	if err := query.
		Select(
			projectsTable+".id",
			projectsTable+".name",
			projectsTable+".description",
			projectsTable+".status",
			projectsTable+".team_id",
			teamsTable+".name AS team_name",
			projectsTable+".created_at",
			projectsTable+".modified_at",
		).
		Order(projectsTable + ".created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&results).Error; err != nil {
		logger.Error("failed to list projects: error=%v", err)
		return nil, 0, ServerError("failed to list projects")
	}

	// Convert to API list items
	items := make([]ProjectListItem, 0, len(results))
	for _, r := range results {
		projectID, err := uuid.Parse(r.ID)
		if err != nil {
			logger.Error("failed to parse project ID: %v", err)
			continue
		}
		teamID, err := uuid.Parse(r.TeamID)
		if err != nil {
			logger.Error("failed to parse team ID: %v", err)
			continue
		}

		item := ProjectListItem{
			Id:          projectID,
			Name:        r.Name,
			Description: r.Description,
			Status:      r.Status,
			TeamId:      teamID,
		}

		if r.TeamName != "" {
			teamName := r.TeamName
			item.TeamName = &teamName
		}

		// Parse timestamps
		if createdAt, parseErr := parseTimestamp(r.CreatedAt); parseErr == nil {
			item.CreatedAt = createdAt
		}
		if r.ModifiedAt != "" {
			if modifiedAt, parseErr := parseTimestamp(r.ModifiedAt); parseErr == nil {
				item.ModifiedAt = &modifiedAt
			}
		}

		items = append(items, item)
	}

	logger.Debug("listed %d projects (total: %d, limit: %d, offset: %d)",
		len(items), total, limit, offset)

	return items, int(total), nil
}

// GetTeamID returns the team_id for a given project
func (s *GormProjectStore) GetTeamID(ctx context.Context, projectID string) (string, error) {
	logger := slogging.Get()

	var record models.ProjectRecord
	result := s.db.WithContext(ctx).
		Select("team_id").
		First(&record, map[string]any{"id": projectID})

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "", NotFoundError(fmt.Sprintf("project not found: %s", projectID))
		}
		logger.Error("failed to get team_id for project: id=%s, error=%v", projectID, result.Error)
		return "", ServerError("failed to get project team")
	}

	return record.TeamID, nil
}

// HasThreatModels checks if a project has any associated threat models
func (s *GormProjectStore) HasThreatModels(ctx context.Context, projectID string) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.ThreatModel{}).
		Where(map[string]any{"project_id": projectID}).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to count threat models for project %s: %w", projectID, err)
	}
	return count > 0, nil
}

// validateProjectRelationships validates relationship constraints
func (s *GormProjectStore) validateProjectRelationships(ctx context.Context, projectID string, relationships []RelatedProject) error {
	for _, rel := range relationships {
		relatedID := rel.RelatedProjectId.String()

		// Self-reference check
		if relatedID == projectID {
			return InvalidInputError("a project cannot have a relationship with itself")
		}

		// Verify the related project exists
		var count int64
		if err := s.db.WithContext(ctx).Model(&models.ProjectRecord{}).
			Where(map[string]any{"id": relatedID}).
			Count(&count).Error; err != nil {
			return ServerError("failed to validate related project")
		}
		if count == 0 {
			return InvalidInputError(fmt.Sprintf("related project not found: %s", relatedID))
		}

		// Cycle detection for directional relationships
		if directionalRelationships[string(rel.Relationship)] {
			if err := s.detectProjectCycle(ctx, projectID, relatedID, string(rel.Relationship)); err != nil {
				return err
			}
		}
	}
	return nil
}

// directionalRelationships lists relationship types that form directed graphs
var directionalRelationships = map[string]bool{
	string(RelationshipTypeParent):       true,
	string(RelationshipTypeChild):        true,
	string(RelationshipTypeDependency):   true,
	string(RelationshipTypeDependent):    true,
	string(RelationshipTypeSupersedes):   true,
	string(RelationshipTypeSupersededBy): true,
}

// inverseRelationship maps a directional relationship to its inverse
var inverseRelationship = map[string]string{
	string(RelationshipTypeParent):       string(RelationshipTypeChild),
	string(RelationshipTypeChild):        string(RelationshipTypeParent),
	string(RelationshipTypeDependency):   string(RelationshipTypeDependent),
	string(RelationshipTypeDependent):    string(RelationshipTypeDependency),
	string(RelationshipTypeSupersedes):   string(RelationshipTypeSupersededBy),
	string(RelationshipTypeSupersededBy): string(RelationshipTypeSupersedes),
}

// getInverseRelationship returns the inverse of a directional relationship, or the original if no inverse exists
func getInverseRelationship(rel string) string {
	if inv, ok := inverseRelationship[rel]; ok {
		return inv
	}
	return rel
}

// detectProjectCycle detects cycles in directional project relationships using BFS
func (s *GormProjectStore) detectProjectCycle(ctx context.Context, sourceID, targetID, relationship string) error {
	// For a directional relationship (e.g., sourceID is parent of targetID),
	// we need to check that targetID does not already reach sourceID
	// through the same relationship direction.
	// This prevents: A -> B -> C -> A cycles.

	const maxDepth = 10
	visited := map[string]bool{sourceID: true}
	queue := []string{targetID}

	for depth := 0; depth < maxDepth && len(queue) > 0; depth++ {
		var nextQueue []string
		for _, currentID := range queue {
			if visited[currentID] {
				return InvalidInputError(fmt.Sprintf(
					"adding relationship '%s' from %s to %s would create a cycle",
					relationship, sourceID, targetID,
				))
			}
			visited[currentID] = true

			// Find all projects that currentID points to via the same relationship type
			var rels []models.ProjectRelationshipRecord
			if err := s.db.WithContext(ctx).
				Where(map[string]any{"project_id": currentID, "relationship": relationship}).
				Find(&rels).Error; err != nil {
				return ServerError("failed to check for relationship cycles")
			}
			for _, r := range rels {
				if !visited[r.RelatedProjectID] {
					nextQueue = append(nextQueue, r.RelatedProjectID)
				}
			}
		}
		queue = nextQueue
	}

	return nil
}

// saveResponsibleParties saves responsible party records for a project
func (s *GormProjectStore) saveResponsibleParties(tx *gorm.DB, projectID string, parties []ResponsibleParty) error {
	logger := slogging.Get()

	for _, party := range parties {
		record := models.ProjectResponsiblePartyRecord{
			ProjectID:        projectID,
			UserInternalUUID: party.UserId.String(),
			CustomRole:       party.CustomRole,
		}
		if party.Role != nil {
			record.Role = string(*party.Role)
		} else {
			record.Role = string(TeamMemberRoleEngineer) // default role
		}

		// Verify the user exists
		var userCount int64
		if err := tx.Model(&models.User{}).
			Where(map[string]any{"internal_uuid": party.UserId.String()}).
			Count(&userCount).Error; err != nil {
			logger.Error("failed to verify user for responsible party: %v", err)
			return ServerError("failed to validate responsible party user")
		}
		if userCount == 0 {
			return InvalidInputError(fmt.Sprintf("user not found for responsible party: %s", party.UserId))
		}

		if err := tx.Create(&record).Error; err != nil {
			logger.Error("failed to create responsible party: %v", err)
			return ServerError("failed to create responsible party")
		}
	}

	return nil
}

// saveRelationships saves relationship records for a project
func (s *GormProjectStore) saveRelationships(tx *gorm.DB, projectID string, relationships []RelatedProject) error {
	logger := slogging.Get()

	for _, rel := range relationships {
		record := models.ProjectRelationshipRecord{
			ProjectID:          projectID,
			RelatedProjectID:   rel.RelatedProjectId.String(),
			Relationship:       string(rel.Relationship),
			CustomRelationship: rel.CustomRelationship,
		}

		if err := tx.Create(&record).Error; err != nil {
			logger.Error("failed to create project relationship: %v", err)
			return ServerError("failed to create project relationship")
		}
	}

	return nil
}

// loadResponsibleParties loads responsible parties for a project
func (s *GormProjectStore) loadResponsibleParties(ctx context.Context, projectID string) ([]ResponsibleParty, error) {
	var records []models.ProjectResponsiblePartyRecord
	if err := s.db.WithContext(ctx).
		Preload("User").
		Where(map[string]any{"project_id": projectID}).
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to load responsible parties: %w", err)
	}

	parties := make([]ResponsibleParty, 0, len(records))
	for _, r := range records {
		userID, err := uuid.Parse(r.UserInternalUUID)
		if err != nil {
			continue
		}

		role := TeamMemberRole(r.Role)
		party := ResponsibleParty{
			UserId:     userID,
			Role:       &role,
			CustomRole: r.CustomRole,
		}

		// Resolve user details
		if r.User.InternalUUID != "" {
			party.User = userModelToAPI(&r.User)
		}

		parties = append(parties, party)
	}

	return parties, nil
}

// loadRelationships loads relationships for a project
func (s *GormProjectStore) loadRelationships(ctx context.Context, projectID string) ([]RelatedProject, error) {
	var records []models.ProjectRelationshipRecord
	if err := s.db.WithContext(ctx).
		Where(map[string]any{"project_id": projectID}).
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to load relationships: %w", err)
	}

	relationships := make([]RelatedProject, 0, len(records))
	for _, r := range records {
		relatedID, err := uuid.Parse(r.RelatedProjectID)
		if err != nil {
			continue
		}

		rel := RelatedProject{
			RelatedProjectId:   relatedID,
			Relationship:       RelationshipType(r.Relationship),
			CustomRelationship: r.CustomRelationship,
		}
		relationships = append(relationships, rel)
	}

	return relationships, nil
}

// recordToAPI converts a ProjectRecord and associated data to an API Project type
func (s *GormProjectStore) recordToAPI(record *models.ProjectRecord, responsibleParties []ResponsibleParty, relationships []RelatedProject, metadata []Metadata) *Project {
	projectID := uuid.MustParse(record.ID)
	teamID := uuid.MustParse(record.TeamID)

	project := &Project{
		Id:          &projectID,
		Name:        record.Name,
		Description: record.Description,
		TeamId:      teamID,
		Uri:         record.URI,
		Status:      record.Status,
		CreatedAt:   &record.CreatedAt,
		ModifiedAt:  &record.ModifiedAt,
		ReviewedAt:  record.ReviewedAt,
	}

	// Set team reference
	if record.Team.ID != "" {
		project.Team = s.teamRecordToTeamRef(&record.Team)
	}

	// Set user references
	if record.CreatedBy.InternalUUID != "" {
		project.CreatedBy = userModelToAPI(&record.CreatedBy)
	}
	if record.ModifiedBy != nil && record.ModifiedBy.InternalUUID != "" {
		project.ModifiedBy = userModelToAPI(record.ModifiedBy)
	}
	if record.ReviewedBy != nil && record.ReviewedBy.InternalUUID != "" {
		project.ReviewedBy = userModelToAPI(record.ReviewedBy)
	}

	// Set responsible parties
	if len(responsibleParties) > 0 {
		project.ResponsibleParties = &responsibleParties
	}

	// Set relationships
	if len(relationships) > 0 {
		project.RelatedProjects = &relationships
	}

	// Set metadata
	if len(metadata) > 0 {
		project.Metadata = &metadata
	}

	return project
}

// teamRecordToTeamRef converts a TeamRecord to an API Team reference (minimal)
func (s *GormProjectStore) teamRecordToTeamRef(record *models.TeamRecord) *Team {
	teamID := uuid.MustParse(record.ID)
	return &Team{
		Id:          &teamID,
		Name:        record.Name,
		Description: record.Description,
	}
}

// applyRelatedToFilter applies the related_to, relationship, and transitive filters to a query
func (s *GormProjectStore) applyRelatedToFilter(query *gorm.DB, relatedTo string, relationship *string, transitive *bool) *gorm.DB {
	projectRelsTable := models.ProjectRelationshipRecord{}.TableName()
	projectsTable := models.ProjectRecord{}.TableName()

	// Determine if we need transitive traversal
	isTransitive := transitive != nil && *transitive && relationship != nil

	if isTransitive {
		// Transitive: follow parent/child chains
		relatedIDs := s.collectTransitiveRelatedIDs(query.Statement.Context, relatedTo, *relationship)
		if len(relatedIDs) == 0 {
			// No related projects found; return impossible condition
			return query.Where("1 = 0")
		}
		return query.Where(projectsTable+".id IN ?", relatedIDs)
	}

	// Non-transitive: direct relationships only
	return query.Where(
		projectsTable+".id IN (SELECT CASE WHEN project_id = ? THEN related_project_id ELSE project_id END FROM "+projectRelsTable+" WHERE "+
			func() string {
				if relationship != nil && *relationship != "" {
					return "(project_id = ? AND relationship = ?) OR (related_project_id = ? AND relationship = ?)"
				}
				return "project_id = ? OR related_project_id = ?"
			}()+")",
		func() []any {
			args := []any{relatedTo}
			if relationship != nil && *relationship != "" {
				args = append(args, relatedTo, *relationship, relatedTo, getInverseRelationship(*relationship))
			} else {
				args = append(args, relatedTo, relatedTo)
			}
			return args
		}()...,
	)
}

// collectTransitiveRelatedIDs follows relationship chains transitively (BFS, max depth 10)
func (s *GormProjectStore) collectTransitiveRelatedIDs(ctx context.Context, startID, relationship string) []string {
	const maxDepth = 10
	visited := map[string]bool{startID: true}
	result := []string{}
	queue := []string{startID}

	for depth := 0; depth < maxDepth && len(queue) > 0; depth++ {
		var nextQueue []string
		for _, currentID := range queue {
			// Find direct relationships from currentID
			var rels []models.ProjectRelationshipRecord

			// Forward direction: currentID has the relationship to related projects
			if err := s.db.WithContext(ctx).
				Where(map[string]any{"project_id": currentID, "relationship": relationship}).
				Find(&rels).Error; err != nil {
				continue
			}
			for _, r := range rels {
				if !visited[r.RelatedProjectID] {
					visited[r.RelatedProjectID] = true
					result = append(result, r.RelatedProjectID)
					nextQueue = append(nextQueue, r.RelatedProjectID)
				}
			}

			// Reverse direction: other projects have the inverse relationship to currentID
			var reverseRels []models.ProjectRelationshipRecord
			inverse := getInverseRelationship(relationship)
			if err := s.db.WithContext(ctx).
				Where(map[string]any{"related_project_id": currentID, "relationship": inverse}).
				Find(&reverseRels).Error; err != nil {
				continue
			}
			for _, r := range reverseRels {
				if !visited[r.ProjectID] {
					visited[r.ProjectID] = true
					result = append(result, r.ProjectID)
					nextQueue = append(nextQueue, r.ProjectID)
				}
			}
		}
		queue = nextQueue
	}

	return result
}

// parseTimestamp parses a timestamp string in common formats
func parseTimestamp(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z",
		"2006-01-02T15:04:05.999999999Z",
		"2006-01-02 15:04:05",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse timestamp: %s", s)
}
