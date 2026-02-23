package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ListTeams returns a paginated list of teams.
// Non-admins see only teams they are members of.
// GET /teams
func (s *Server) ListTeams(c *gin.Context, params ListTeamsParams) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	isAdmin, _ := IsUserAdministrator(c)

	limit := 20
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}

	if err := ValidatePaginationParams(params.Limit, params.Offset); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Convert ListTeamsParams to TeamFilters
	filters := &TeamFilters{
		Name:       params.Name,
		Transitive: params.Transitive,
	}
	if params.Status != nil {
		filters.Status = strings.Split(*params.Status, ",")
	}
	if params.MemberUserId != nil {
		s := params.MemberUserId.String()
		filters.MemberUserID = &s
	}
	if params.RelatedTo != nil {
		s := params.RelatedTo.String()
		filters.RelatedTo = &s
	}
	if params.Relationship != nil {
		s := string(*params.Relationship)
		filters.Relationship = &s
	}

	items, total, err := GlobalTeamStore.List(ctx, limit, offset, filters, userUUID, isAdmin)
	if err != nil {
		logger.Error("Failed to list teams: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Teams not found", "Failed to list teams"))
		return
	}

	c.JSON(http.StatusOK, ListTeamsResponse{
		Teams:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// CreateTeam creates a new team.
// Any authenticated user can create a team. The creator is auto-added as a member.
// POST /teams
func (s *Server) CreateTeam(c *gin.Context) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	var req TeamInput
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Debug("Invalid request body: %v", err)
		HandleRequestError(c, InvalidInputError("Invalid request body"))
		return
	}

	team := Team{
		Name:               req.Name,
		Description:        req.Description,
		EmailAddress:       req.EmailAddress,
		Members:            req.Members,
		RelatedTeams:       req.RelatedTeams,
		ResponsibleParties: req.ResponsibleParties,
		Status:             req.Status,
		Uri:                req.Uri,
	}

	result, err := GlobalTeamStore.Create(ctx, &team, userUUID)
	if err != nil {
		logger.Error("Failed to create team: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Team not found", "Failed to create team"))
		return
	}

	// Emit event
	if GlobalEventEmitter != nil {
		_ = GlobalEventEmitter.EmitEvent(ctx, EventPayload{
			EventType:    EventTeamCreated,
			ResourceID:   result.Id.String(),
			ResourceType: "team",
			OwnerID:      userUUID,
		})
	}

	c.JSON(http.StatusCreated, result)
}

// GetTeam retrieves a team by ID.
// Requires team membership or admin.
// GET /teams/{team_id}
func (s *Server) GetTeam(c *gin.Context, teamId openapi_types.UUID) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization check
	authorized, err := IsTeamMemberOrAdmin(ctx, teamId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check team authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		HandleRequestError(c, ForbiddenError("You must be a team member or administrator to view this team"))
		return
	}

	team, err := GlobalTeamStore.Get(ctx, teamId.String())
	if err != nil {
		logger.Error("Failed to get team: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Team not found", "Failed to get team"))
		return
	}

	c.JSON(http.StatusOK, team)
}

// UpdateTeam fully updates a team.
// Requires team membership or admin.
// PUT /teams/{team_id}
func (s *Server) UpdateTeam(c *gin.Context, teamId openapi_types.UUID) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization check
	authorized, err := IsTeamMemberOrAdmin(ctx, teamId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check team authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		HandleRequestError(c, ForbiddenError("You must be a team member or administrator to update this team"))
		return
	}

	var req TeamInput
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Debug("Invalid request body: %v", err)
		HandleRequestError(c, InvalidInputError("Invalid request body"))
		return
	}

	id := teamId
	team := Team{
		Id:                 &id,
		Name:               req.Name,
		Description:        req.Description,
		EmailAddress:       req.EmailAddress,
		Members:            req.Members,
		RelatedTeams:       req.RelatedTeams,
		ResponsibleParties: req.ResponsibleParties,
		Status:             req.Status,
		Uri:                req.Uri,
	}

	result, err := GlobalTeamStore.Update(ctx, teamId.String(), &team, userUUID)
	if err != nil {
		logger.Error("Failed to update team: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Team not found", "Failed to update team"))
		return
	}

	// Emit event
	if GlobalEventEmitter != nil {
		_ = GlobalEventEmitter.EmitEvent(ctx, EventPayload{
			EventType:    EventTeamUpdated,
			ResourceID:   teamId.String(),
			ResourceType: "team",
			OwnerID:      userUUID,
		})
	}

	c.JSON(http.StatusOK, result)
}

// PatchTeam partially updates a team via JSON Patch.
// Requires team membership or admin.
// PATCH /teams/{team_id}
func (s *Server) PatchTeam(c *gin.Context, teamId openapi_types.UUID) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization check
	authorized, err := IsTeamMemberOrAdmin(ctx, teamId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check team authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		HandleRequestError(c, ForbiddenError("You must be a team member or administrator to update this team"))
		return
	}

	// Parse JSON Patch operations from request body
	operations, err := ParsePatchRequest(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Validate prohibited fields
	prohibitedPaths := []string{"/id", "/created_at", "/modified_at", "/created_by", "/modified_by"}
	for _, op := range operations {
		for _, p := range prohibitedPaths {
			if op.Path == p {
				HandleRequestError(c, InvalidInputError("Field '"+strings.TrimPrefix(p, "/")+"' is not allowed in PATCH requests"))
				return
			}
		}
	}

	// Get existing team
	existing, err := GlobalTeamStore.Get(ctx, teamId.String())
	if err != nil {
		logger.Error("Failed to get team for patch: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Team not found", "Failed to get team"))
		return
	}

	// Apply patch operations
	patched, err := ApplyPatchOperations(*existing, operations)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Save via Update
	result, err := GlobalTeamStore.Update(ctx, teamId.String(), &patched, userUUID)
	if err != nil {
		logger.Error("Failed to patch team: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Team not found", "Failed to patch team"))
		return
	}

	// Emit event
	if GlobalEventEmitter != nil {
		_ = GlobalEventEmitter.EmitEvent(ctx, EventPayload{
			EventType:    EventTeamUpdated,
			ResourceID:   teamId.String(),
			ResourceType: "team",
			OwnerID:      userUUID,
		})
	}

	c.JSON(http.StatusOK, result)
}

// DeleteTeam deletes a team.
// Requires owner role or admin. Returns 409 if team has projects.
// DELETE /teams/{team_id}
func (s *Server) DeleteTeam(c *gin.Context, teamId openapi_types.UUID) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Owner/admin check (stricter than member check)
	authorized, err := IsTeamOwnerOrAdmin(ctx, teamId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check team authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		HandleRequestError(c, ForbiddenError("You must be the team creator or an administrator to delete this team"))
		return
	}

	err = GlobalTeamStore.Delete(ctx, teamId.String())
	if err != nil {
		logger.Error("Failed to delete team: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Team not found", "Failed to delete team"))
		return
	}

	// Emit event
	if GlobalEventEmitter != nil {
		_ = GlobalEventEmitter.EmitEvent(ctx, EventPayload{
			EventType:    EventTeamDeleted,
			ResourceID:   teamId.String(),
			ResourceType: "team",
			OwnerID:      userUUID,
		})
	}

	c.Status(http.StatusNoContent)
}

// Team Metadata Methods - delegate to generic handler

func (s *Server) GetTeamMetadata(c *gin.Context, teamId openapi_types.UUID) {
	s.teamMetadata.List(c)
}

func (s *Server) CreateTeamMetadata(c *gin.Context, teamId openapi_types.UUID) {
	s.teamMetadata.Create(c)
}

func (s *Server) BulkCreateTeamMetadata(c *gin.Context, teamId openapi_types.UUID) {
	s.teamMetadata.BulkCreate(c)
}

func (s *Server) BulkReplaceTeamMetadata(c *gin.Context, teamId openapi_types.UUID) {
	s.teamMetadata.BulkReplace(c)
}

func (s *Server) BulkUpsertTeamMetadata(c *gin.Context, teamId openapi_types.UUID) {
	s.teamMetadata.BulkUpsert(c)
}

func (s *Server) DeleteTeamMetadata(c *gin.Context, teamId openapi_types.UUID, key string) {
	s.teamMetadata.Delete(c)
}

func (s *Server) UpdateTeamMetadata(c *gin.Context, teamId openapi_types.UUID, key string) {
	s.teamMetadata.Update(c)
}

// teamExistsFunc is a helper for metadata handler entity existence checks
func teamExistsFunc(ctx context.Context, teamID openapi_types.UUID) error {
	team, err := GlobalTeamStore.Get(ctx, teamID.String())
	if err != nil {
		return err
	}
	if team == nil {
		return NotFoundError("team not found")
	}
	return nil
}
