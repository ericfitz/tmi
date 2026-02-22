package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ListProjects returns a paginated list of projects.
// Non-admins see only projects from teams they are members of.
// GET /projects
func (s *Server) ListProjects(c *gin.Context, params ListProjectsParams) {
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

	// Convert ListProjectsParams to ProjectFilters
	filters := &ProjectFilters{
		Name:       params.Name,
		Transitive: params.Transitive,
	}
	if params.Status != nil {
		filters.Status = strings.Split(*params.Status, ",")
	}
	if params.TeamId != nil {
		s := params.TeamId.String()
		filters.TeamID = &s
	}
	if params.RelatedTo != nil {
		s := params.RelatedTo.String()
		filters.RelatedTo = &s
	}
	if params.Relationship != nil {
		s := string(*params.Relationship)
		filters.Relationship = &s
	}

	items, total, err := GlobalProjectStore.List(ctx, limit, offset, filters, userUUID, isAdmin)
	if err != nil {
		logger.Error("Failed to list projects: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Projects not found", "Failed to list projects"))
		return
	}

	c.JSON(http.StatusOK, ListProjectsResponse{
		Projects: items,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	})
}

// CreateProject creates a new project.
// Requires membership in the referenced team.
// POST /projects
func (s *Server) CreateProject(c *gin.Context) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	var req ProjectInput
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Debug("Invalid request body: %v", err)
		HandleRequestError(c, InvalidInputError("Invalid request body"))
		return
	}

	// Check team membership before creating project
	authorized, err := IsTeamMemberOrAdmin(ctx, req.TeamId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check team authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		HandleRequestError(c, ForbiddenError("You must be a member of the team to create a project in it"))
		return
	}

	project := Project{
		Name:               req.Name,
		Description:        req.Description,
		TeamId:             req.TeamId,
		RelatedProjects:    req.RelatedProjects,
		ResponsibleParties: req.ResponsibleParties,
		Status:             req.Status,
		Uri:                req.Uri,
	}

	result, err := GlobalProjectStore.Create(ctx, &project, userUUID)
	if err != nil {
		logger.Error("Failed to create project: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Project not found", "Failed to create project"))
		return
	}

	// Emit event
	if GlobalEventEmitter != nil {
		_ = GlobalEventEmitter.EmitEvent(ctx, EventPayload{
			EventType:    EventProjectCreated,
			ResourceID:   result.Id.String(),
			ResourceType: "project",
			OwnerID:      userUUID,
		})
	}

	c.JSON(http.StatusCreated, result)
}

// GetProject retrieves a project by ID.
// Requires team membership or admin.
// GET /projects/{project_id}
func (s *Server) GetProject(c *gin.Context, projectId openapi_types.UUID) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization check via team membership
	authorized, err := IsProjectTeamMemberOrAdmin(ctx, projectId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check project authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		HandleRequestError(c, ForbiddenError("You must be a team member or administrator to view this project"))
		return
	}

	project, err := GlobalProjectStore.Get(ctx, projectId.String())
	if err != nil {
		logger.Error("Failed to get project: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Project not found", "Failed to get project"))
		return
	}

	c.JSON(http.StatusOK, project)
}

// UpdateProject fully updates a project.
// Requires team membership or admin.
// PUT /projects/{project_id}
func (s *Server) UpdateProject(c *gin.Context, projectId openapi_types.UUID) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization check via team membership
	authorized, err := IsProjectTeamMemberOrAdmin(ctx, projectId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check project authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		HandleRequestError(c, ForbiddenError("You must be a team member or administrator to update this project"))
		return
	}

	var req ProjectInput
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Debug("Invalid request body: %v", err)
		HandleRequestError(c, InvalidInputError("Invalid request body"))
		return
	}

	id := projectId
	project := Project{
		Id:                 &id,
		Name:               req.Name,
		Description:        req.Description,
		TeamId:             req.TeamId,
		RelatedProjects:    req.RelatedProjects,
		ResponsibleParties: req.ResponsibleParties,
		Status:             req.Status,
		Uri:                req.Uri,
	}

	result, err := GlobalProjectStore.Update(ctx, projectId.String(), &project, userUUID)
	if err != nil {
		logger.Error("Failed to update project: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Project not found", "Failed to update project"))
		return
	}

	// Emit event
	if GlobalEventEmitter != nil {
		_ = GlobalEventEmitter.EmitEvent(ctx, EventPayload{
			EventType:    EventProjectUpdated,
			ResourceID:   projectId.String(),
			ResourceType: "project",
			OwnerID:      userUUID,
		})
	}

	c.JSON(http.StatusOK, result)
}

// PatchProject partially updates a project via JSON Patch.
// Requires team membership or admin.
// PATCH /projects/{project_id}
func (s *Server) PatchProject(c *gin.Context, projectId openapi_types.UUID) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization check via team membership
	authorized, err := IsProjectTeamMemberOrAdmin(ctx, projectId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check project authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		HandleRequestError(c, ForbiddenError("You must be a team member or administrator to update this project"))
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

	// Get existing project
	existing, err := GlobalProjectStore.Get(ctx, projectId.String())
	if err != nil {
		logger.Error("Failed to get project for patch: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Project not found", "Failed to get project"))
		return
	}

	// Apply patch operations
	patched, err := ApplyPatchOperations(*existing, operations)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Save via Update
	result, err := GlobalProjectStore.Update(ctx, projectId.String(), &patched, userUUID)
	if err != nil {
		logger.Error("Failed to patch project: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Project not found", "Failed to patch project"))
		return
	}

	// Emit event
	if GlobalEventEmitter != nil {
		_ = GlobalEventEmitter.EmitEvent(ctx, EventPayload{
			EventType:    EventProjectUpdated,
			ResourceID:   projectId.String(),
			ResourceType: "project",
			OwnerID:      userUUID,
		})
	}

	c.JSON(http.StatusOK, result)
}

// DeleteProject deletes a project.
// Requires team owner/creator role or admin. Returns 409 if threat models reference it.
// DELETE /projects/{project_id}
func (s *Server) DeleteProject(c *gin.Context, projectId openapi_types.UUID) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization check - need project's team, then check owner/admin
	teamID, err := GetProjectTeamID(ctx, projectId.String())
	if err != nil {
		logger.Error("Failed to get project team: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Project not found", "Failed to get project"))
		return
	}

	authorized, err := IsTeamOwnerOrAdmin(ctx, teamID, userUUID, c)
	if err != nil {
		logger.Error("Failed to check project authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		HandleRequestError(c, ForbiddenError("You must be the team creator or an administrator to delete this project"))
		return
	}

	err = GlobalProjectStore.Delete(ctx, projectId.String())
	if err != nil {
		logger.Error("Failed to delete project: %v", err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Project not found", "Failed to delete project"))
		return
	}

	// Emit event
	if GlobalEventEmitter != nil {
		_ = GlobalEventEmitter.EmitEvent(ctx, EventPayload{
			EventType:    EventProjectDeleted,
			ResourceID:   projectId.String(),
			ResourceType: "project",
			OwnerID:      userUUID,
		})
	}

	c.Status(http.StatusNoContent)
}

// Project Metadata Methods - delegate to generic handler

func (s *Server) GetProjectMetadata(c *gin.Context, projectId openapi_types.UUID) {
	s.projectMetadata.List(c)
}

func (s *Server) CreateProjectMetadata(c *gin.Context, projectId openapi_types.UUID) {
	s.projectMetadata.Create(c)
}

func (s *Server) BulkCreateProjectMetadata(c *gin.Context, projectId openapi_types.UUID) {
	s.projectMetadata.BulkCreate(c)
}

func (s *Server) BulkReplaceProjectMetadata(c *gin.Context, projectId openapi_types.UUID) {
	s.projectMetadata.BulkReplace(c)
}

func (s *Server) BulkUpsertProjectMetadata(c *gin.Context, projectId openapi_types.UUID) {
	s.projectMetadata.BulkUpsert(c)
}

func (s *Server) DeleteProjectMetadata(c *gin.Context, projectId openapi_types.UUID, key string) {
	s.projectMetadata.Delete(c)
}

func (s *Server) UpdateProjectMetadata(c *gin.Context, projectId openapi_types.UUID, key string) {
	s.projectMetadata.Update(c)
}

// projectExistsFunc is a helper for metadata handler entity existence checks
func projectExistsFunc(ctx context.Context, projectID openapi_types.UUID) error {
	project, err := GlobalProjectStore.Get(ctx, projectID.String())
	if err != nil {
		return err
	}
	if project == nil {
		return NotFoundError("project not found")
	}
	return nil
}
