package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Survey Template Admin Handlers

// ListAdminSurveyTemplates returns a paginated list of all survey templates.
// GET /admin/survey_templates
func (s *Server) ListAdminSurveyTemplates(c *gin.Context, params ListAdminSurveyTemplatesParams) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Set defaults for pagination
	limit := 20
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}

	// Get status filter if provided
	var status *SurveyTemplateStatus
	if params.Status != nil {
		status = params.Status
	}

	items, total, err := GlobalSurveyTemplateStore.List(ctx, limit, offset, status)
	if err != nil {
		logger.Error("Failed to list survey templates: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to list survey templates",
		})
		return
	}

	c.JSON(http.StatusOK, ListSurveyTemplatesResponse{
		SurveyTemplates: items,
		Total:           total,
		Limit:           limit,
		Offset:          offset,
	})
}

// CreateAdminSurveyTemplate creates a new survey template.
// POST /admin/survey_templates
func (s *Server) CreateAdminSurveyTemplate(c *gin.Context) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Get user internal UUID from context
	userInternalUUID := ""
	if internalUUID, exists := c.Get("userInternalUUID"); exists {
		userInternalUUID, _ = internalUUID.(string)
	}
	if userInternalUUID == "" {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated",
		})
		return
	}

	var req SurveyTemplateBase
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Debug("Invalid request body: %v", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_input",
			ErrorDescription: "Invalid request body: " + err.Error(),
		})
		return
	}

	// Create the template struct
	template := &SurveyTemplate{
		Name:        req.Name,
		Description: req.Description,
		Version:     req.Version,
		Status:      req.Status,
		Questions:   req.Questions,
		Settings:    req.Settings,
	}

	// Create in store
	if err := GlobalSurveyTemplateStore.Create(ctx, template, userInternalUUID); err != nil {
		logger.Error("Failed to create survey template: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to create survey template",
		})
		return
	}

	c.JSON(http.StatusCreated, template)
}

// GetAdminSurveyTemplate returns a specific survey template.
// GET /admin/survey_templates/{template_id}
func (s *Server) GetAdminSurveyTemplate(c *gin.Context, templateId SurveyTemplateIdPathParam) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	template, err := GlobalSurveyTemplateStore.Get(ctx, templateId)
	if err != nil {
		logger.Error("Failed to get survey template: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey template",
		})
		return
	}

	if template == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey template not found",
		})
		return
	}

	c.JSON(http.StatusOK, template)
}

// UpdateAdminSurveyTemplate fully updates a survey template.
// PUT /admin/survey_templates/{template_id}
func (s *Server) UpdateAdminSurveyTemplate(c *gin.Context, templateId SurveyTemplateIdPathParam) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Check if template exists
	existing, err := GlobalSurveyTemplateStore.Get(ctx, templateId)
	if err != nil {
		logger.Error("Failed to get survey template: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey template",
		})
		return
	}

	if existing == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey template not found",
		})
		return
	}

	var req SurveyTemplateBase
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Debug("Invalid request body: %v", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_input",
			ErrorDescription: "Invalid request body: " + err.Error(),
		})
		return
	}

	// Build updated template
	template := &SurveyTemplate{
		Id:          &templateId,
		Name:        req.Name,
		Description: req.Description,
		Version:     req.Version,
		Status:      req.Status,
		Questions:   req.Questions,
		Settings:    req.Settings,
	}

	if err := GlobalSurveyTemplateStore.Update(ctx, template); err != nil {
		logger.Error("Failed to update survey template: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to update survey template",
		})
		return
	}

	// Get the updated template
	updated, err := GlobalSurveyTemplateStore.Get(ctx, templateId)
	if err != nil {
		logger.Error("Failed to get updated survey template: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get updated survey template",
		})
		return
	}

	c.JSON(http.StatusOK, updated)
}

// PatchAdminSurveyTemplate partially updates a survey template.
// PATCH /admin/survey_templates/{template_id}
func (s *Server) PatchAdminSurveyTemplate(c *gin.Context, templateId SurveyTemplateIdPathParam) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Check if template exists
	existing, err := GlobalSurveyTemplateStore.Get(ctx, templateId)
	if err != nil {
		logger.Error("Failed to get survey template: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey template",
		})
		return
	}

	if existing == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey template not found",
		})
		return
	}

	// Parse JSON Patch operations
	operations, err := ParsePatchRequest(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Validate patch operations against prohibited fields
	prohibitedPaths := []string{
		"/id", "/created_at", "/modified_at", "/created_by",
	}

	for _, op := range operations {
		for _, prohibitedPath := range prohibitedPaths {
			if op.Path == prohibitedPath {
				fieldName := strings.TrimPrefix(prohibitedPath, "/")
				HandleRequestError(c, InvalidInputError(fmt.Sprintf(
					"Field '%s' is not allowed in PATCH requests",
					fieldName)))
				return
			}
		}
	}

	// Apply patch operations to the existing template
	// Convert existing template to JSON
	existingJSON, err := json.Marshal(existing)
	if err != nil {
		logger.Error("Failed to marshal existing template: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to process patch",
		})
		return
	}

	// Apply patches
	patchedJSON, err := ApplyPatchOperations(existingJSON, operations)
	if err != nil {
		HandleRequestError(c, InvalidInputError("Failed to apply patch: "+err.Error()))
		return
	}

	// Unmarshal back to template
	var patched SurveyTemplate
	if err := json.Unmarshal(patchedJSON, &patched); err != nil {
		logger.Error("Failed to unmarshal patched template: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to process patch",
		})
		return
	}

	// Ensure ID is preserved
	patched.Id = &templateId

	if err := GlobalSurveyTemplateStore.Update(ctx, &patched); err != nil {
		logger.Error("Failed to update survey template: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to update survey template",
		})
		return
	}

	// Get the updated template
	updated, err := GlobalSurveyTemplateStore.Get(ctx, templateId)
	if err != nil {
		logger.Error("Failed to get updated survey template: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get updated survey template",
		})
		return
	}

	c.JSON(http.StatusOK, updated)
}

// DeleteAdminSurveyTemplate deletes a survey template.
// DELETE /admin/survey_templates/{template_id}
func (s *Server) DeleteAdminSurveyTemplate(c *gin.Context, templateId SurveyTemplateIdPathParam) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Check if template exists
	existing, err := GlobalSurveyTemplateStore.Get(ctx, templateId)
	if err != nil {
		logger.Error("Failed to get survey template: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey template",
		})
		return
	}

	if existing == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey template not found",
		})
		return
	}

	// Check if template has responses
	hasResponses, err := GlobalSurveyTemplateStore.HasResponses(ctx, templateId)
	if err != nil {
		logger.Error("Failed to check for responses: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to check for responses",
		})
		return
	}

	if hasResponses {
		c.JSON(http.StatusConflict, Error{
			Error:            "conflict",
			ErrorDescription: "Cannot delete template with existing responses",
		})
		return
	}

	if err := GlobalSurveyTemplateStore.Delete(ctx, templateId); err != nil {
		logger.Error("Failed to delete survey template: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to delete survey template",
		})
		return
	}

	c.Status(http.StatusNoContent)
}

// Survey Intake Handlers (Developer-facing)

// ListIntakeTemplates returns a list of active survey templates.
// GET /intake/templates
func (s *Server) ListIntakeTemplates(c *gin.Context, params ListIntakeTemplatesParams) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Set defaults for pagination
	limit := 20
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}

	items, total, err := GlobalSurveyTemplateStore.ListActive(ctx, limit, offset)
	if err != nil {
		logger.Error("Failed to list active survey templates: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to list survey templates",
		})
		return
	}

	c.JSON(http.StatusOK, ListSurveyTemplatesResponse{
		SurveyTemplates: items,
		Total:           total,
		Limit:           limit,
		Offset:          offset,
	})
}

// GetIntakeTemplate returns a specific active survey template for filling.
// GET /intake/templates/{template_id}
func (s *Server) GetIntakeTemplate(c *gin.Context, templateId SurveyTemplateIdPathParam) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	template, err := GlobalSurveyTemplateStore.Get(ctx, templateId)
	if err != nil {
		logger.Error("Failed to get survey template: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey template",
		})
		return
	}

	if template == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey template not found",
		})
		return
	}

	// Check if template is active (intake endpoints only show active templates)
	if template.Status == nil || *template.Status != SurveyTemplateStatusActive {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey template not found or not active",
		})
		return
	}

	c.JSON(http.StatusOK, template)
}

// ListIntakeResponses returns the current user's survey responses.
// GET /intake/responses
func (s *Server) ListIntakeResponses(c *gin.Context, params ListIntakeResponsesParams) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Get the current user's internal UUID from context
	userInternalUUID, exists := c.Get("userInternalUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated",
		})
		return
	}

	// Set defaults for pagination
	limit := 20
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}

	items, total, err := GlobalSurveyResponseStore.ListByOwner(ctx, userInternalUUID.(string), limit, offset, params.Status)
	if err != nil {
		logger.Error("Failed to list survey responses: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to list survey responses",
		})
		return
	}

	c.JSON(http.StatusOK, ListSurveyResponsesResponse{
		SurveyResponses: items,
		Total:           total,
		Limit:           limit,
		Offset:          offset,
	})
}

// CreateIntakeResponse creates a new survey response in draft status.
// POST /intake/responses
func (s *Server) CreateIntakeResponse(c *gin.Context) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Get the current user's internal UUID from context
	userInternalUUID, exists := c.Get("userInternalUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated",
		})
		return
	}

	var req SurveyResponseCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Debug("Invalid request body: %v", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_input",
			ErrorDescription: "Invalid request body: " + err.Error(),
		})
		return
	}

	// Create the response struct
	response := &SurveyResponse{
		TemplateId:          req.TemplateId,
		IsConfidential:      req.IsConfidential,
		LinkedThreatModelId: req.LinkedThreatModelId,
	}

	// Convert answers if provided
	if req.Answers != nil {
		// Need to convert between the answer types
		answers := make(map[string]SurveyResponse_Answers_AdditionalProperties)
		for k, v := range *req.Answers {
			// The types are compatible, just copy the union data
			answers[k] = SurveyResponse_Answers_AdditionalProperties(v)
		}
		response.Answers = &answers
	}

	// Create in store
	if err := GlobalSurveyResponseStore.Create(ctx, response, userInternalUUID.(string)); err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "template not found") {
			c.JSON(http.StatusBadRequest, Error{
				Error:            "invalid_input",
				ErrorDescription: "Template not found: " + response.TemplateId.String(),
			})
			return
		}
		logger.Error("Failed to create survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to create survey response",
		})
		return
	}

	c.JSON(http.StatusCreated, response)
}

// GetIntakeResponse returns a specific survey response.
// GET /intake/responses/{response_id}
func (s *Server) GetIntakeResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Get the current user's internal UUID from context
	userInternalUUID, exists := c.Get("userInternalUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated",
		})
		return
	}

	response, err := GlobalSurveyResponseStore.Get(ctx, responseId)
	if err != nil {
		logger.Error("Failed to get survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey response",
		})
		return
	}

	if response == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey response not found",
		})
		return
	}

	// Check access
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, responseId, userInternalUUID.(string), AuthorizationRoleReader)
	if err != nil {
		logger.Error("Failed to check access: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to check access",
		})
		return
	}

	if !hasAccess {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Access denied",
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// UpdateIntakeResponse fully updates a survey response.
// PUT /intake/responses/{response_id}
func (s *Server) UpdateIntakeResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Get the current user's internal UUID from context
	userInternalUUID, exists := c.Get("userInternalUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated",
		})
		return
	}

	// Check if response exists
	existing, err := GlobalSurveyResponseStore.Get(ctx, responseId)
	if err != nil {
		logger.Error("Failed to get survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey response",
		})
		return
	}

	if existing == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey response not found",
		})
		return
	}

	// Check write access
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, responseId, userInternalUUID.(string), AuthorizationRoleWriter)
	if err != nil {
		logger.Error("Failed to check access: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to check access",
		})
		return
	}

	if !hasAccess {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Access denied",
		})
		return
	}

	// Only allow updates in draft or needs_revision status
	if existing.Status != nil && *existing.Status != Draft && *existing.Status != NeedsRevision {
		c.JSON(http.StatusConflict, Error{
			Error:            "conflict",
			ErrorDescription: "Can only update responses in draft or needs_revision status",
		})
		return
	}

	var req SurveyResponseBase
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Debug("Invalid request body: %v", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_input",
			ErrorDescription: "Invalid request body: " + err.Error(),
		})
		return
	}

	// Build updated response (preserving immutable fields)
	response := &SurveyResponse{
		Id:                  &responseId,
		LinkedThreatModelId: req.LinkedThreatModelId,
	}

	// Convert answers if provided
	if req.Answers != nil {
		answers := make(map[string]SurveyResponse_Answers_AdditionalProperties)
		for k, v := range *req.Answers {
			answers[k] = SurveyResponse_Answers_AdditionalProperties(v)
		}
		response.Answers = &answers
	}

	if err := GlobalSurveyResponseStore.Update(ctx, response); err != nil {
		logger.Error("Failed to update survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to update survey response",
		})
		return
	}

	// Get the updated response
	updated, err := GlobalSurveyResponseStore.Get(ctx, responseId)
	if err != nil {
		logger.Error("Failed to get updated survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get updated survey response",
		})
		return
	}

	c.JSON(http.StatusOK, updated)
}

// PatchIntakeResponse partially updates a survey response.
// PATCH /intake/responses/{response_id}
func (s *Server) PatchIntakeResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Get the current user's internal UUID from context
	userInternalUUID, exists := c.Get("userInternalUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated",
		})
		return
	}

	// Check if response exists
	existing, err := GlobalSurveyResponseStore.Get(ctx, responseId)
	if err != nil {
		logger.Error("Failed to get survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey response",
		})
		return
	}

	if existing == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey response not found",
		})
		return
	}

	// Check write access
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, responseId, userInternalUUID.(string), AuthorizationRoleWriter)
	if err != nil {
		logger.Error("Failed to check access: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to check access",
		})
		return
	}

	if !hasAccess {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Access denied",
		})
		return
	}

	// Only allow updates in draft or needs_revision status
	if existing.Status != nil && *existing.Status != Draft && *existing.Status != NeedsRevision {
		c.JSON(http.StatusConflict, Error{
			Error:            "conflict",
			ErrorDescription: "Can only update responses in draft or needs_revision status",
		})
		return
	}

	// Parse JSON Patch operations
	operations, err := ParsePatchRequest(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Validate patch operations against prohibited fields
	prohibitedPaths := []string{
		"/id", "/created_at", "/modified_at", "/owner", "/template_id",
		"/template_version", "/is_confidential", "/status",
	}

	for _, op := range operations {
		for _, prohibitedPath := range prohibitedPaths {
			if op.Path == prohibitedPath {
				fieldName := strings.TrimPrefix(prohibitedPath, "/")
				HandleRequestError(c, InvalidInputError(fmt.Sprintf(
					"Field '%s' is not allowed in PATCH requests",
					fieldName)))
				return
			}
		}
	}

	// Apply patch operations
	existingJSON, err := json.Marshal(existing)
	if err != nil {
		logger.Error("Failed to marshal existing response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to process patch",
		})
		return
	}

	patchedJSON, err := ApplyPatchOperations(existingJSON, operations)
	if err != nil {
		HandleRequestError(c, InvalidInputError("Failed to apply patch: "+err.Error()))
		return
	}

	var patched SurveyResponse
	if err := json.Unmarshal(patchedJSON, &patched); err != nil {
		logger.Error("Failed to unmarshal patched response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to process patch",
		})
		return
	}

	patched.Id = &responseId

	if err := GlobalSurveyResponseStore.Update(ctx, &patched); err != nil {
		logger.Error("Failed to update survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to update survey response",
		})
		return
	}

	updated, err := GlobalSurveyResponseStore.Get(ctx, responseId)
	if err != nil {
		logger.Error("Failed to get updated survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get updated survey response",
		})
		return
	}

	c.JSON(http.StatusOK, updated)
}

// DeleteIntakeResponse deletes a draft survey response.
// DELETE /intake/responses/{response_id}
func (s *Server) DeleteIntakeResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Get the current user's internal UUID from context
	userInternalUUID, exists := c.Get("userInternalUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated",
		})
		return
	}

	// Check if response exists
	existing, err := GlobalSurveyResponseStore.Get(ctx, responseId)
	if err != nil {
		logger.Error("Failed to get survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey response",
		})
		return
	}

	if existing == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey response not found",
		})
		return
	}

	// Check owner access (only owner can delete)
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, responseId, userInternalUUID.(string), AuthorizationRoleOwner)
	if err != nil {
		logger.Error("Failed to check access: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to check access",
		})
		return
	}

	if !hasAccess {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Access denied",
		})
		return
	}

	if err := GlobalSurveyResponseStore.Delete(ctx, responseId); err != nil {
		// Check if it's a status error
		if strings.Contains(err.Error(), "can only delete draft") {
			c.JSON(http.StatusConflict, Error{
				Error:            "conflict",
				ErrorDescription: err.Error(),
			})
			return
		}
		logger.Error("Failed to delete survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to delete survey response",
		})
		return
	}

	c.Status(http.StatusNoContent)
}

// SubmitIntakeResponse submits a survey response for review.
// POST /intake/responses/{response_id}/submit
func (s *Server) SubmitIntakeResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Get the current user's internal UUID from context
	userInternalUUID, exists := c.Get("userInternalUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated",
		})
		return
	}

	// Check if response exists
	existing, err := GlobalSurveyResponseStore.Get(ctx, responseId)
	if err != nil {
		logger.Error("Failed to get survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey response",
		})
		return
	}

	if existing == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey response not found",
		})
		return
	}

	// Check write access (owner or writer can submit)
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, responseId, userInternalUUID.(string), AuthorizationRoleWriter)
	if err != nil {
		logger.Error("Failed to check access: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to check access",
		})
		return
	}

	if !hasAccess {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Access denied",
		})
		return
	}

	if err := GlobalSurveyResponseStore.Submit(ctx, responseId); err != nil {
		if strings.Contains(err.Error(), "invalid state transition") {
			c.JSON(http.StatusConflict, Error{
				Error:            "conflict",
				ErrorDescription: err.Error(),
			})
			return
		}
		logger.Error("Failed to submit survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to submit survey response",
		})
		return
	}

	// Get the updated response
	updated, err := GlobalSurveyResponseStore.Get(ctx, responseId)
	if err != nil {
		logger.Error("Failed to get updated survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get updated survey response",
		})
		return
	}

	c.JSON(http.StatusOK, updated)
}

// Survey Triage Handlers (Security Engineer-facing)

// ListTriageSurveyResponses returns survey responses for triage.
// GET /triage/surveys/responses
func (s *Server) ListTriageSurveyResponses(c *gin.Context, params ListTriageSurveyResponsesParams) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Get the current user's internal UUID from context
	userInternalUUID, exists := c.Get("userInternalUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated",
		})
		return
	}

	// TODO: Check if user is in Security Reviewers group
	_ = userInternalUUID

	// Set defaults for pagination
	limit := 20
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}

	// Build filters
	var filters *SurveyResponseFilters
	if params.Status != nil || params.TemplateId != nil {
		filters = &SurveyResponseFilters{
			Status:     params.Status,
			TemplateID: params.TemplateId,
		}
	}

	items, total, err := GlobalSurveyResponseStore.List(ctx, limit, offset, filters)
	if err != nil {
		logger.Error("Failed to list survey responses: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to list survey responses",
		})
		return
	}

	c.JSON(http.StatusOK, ListSurveyResponsesResponse{
		SurveyResponses: items,
		Total:           total,
		Limit:           limit,
		Offset:          offset,
	})
}

// GetTriageSurveyResponse returns a specific survey response for triage.
// GET /triage/surveys/responses/{response_id}
func (s *Server) GetTriageSurveyResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Get the current user's internal UUID from context
	userInternalUUID, exists := c.Get("userInternalUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated",
		})
		return
	}

	response, err := GlobalSurveyResponseStore.Get(ctx, responseId)
	if err != nil {
		logger.Error("Failed to get survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey response",
		})
		return
	}

	if response == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey response not found",
		})
		return
	}

	// Check access (Security Reviewers have owner role for triage access)
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, responseId, userInternalUUID.(string), AuthorizationRoleReader)
	if err != nil {
		logger.Error("Failed to check access: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to check access",
		})
		return
	}

	if !hasAccess {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Access denied",
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// ApproveTriageSurveyResponse approves a survey response.
// POST /triage/surveys/responses/{response_id}/approve
func (s *Server) ApproveTriageSurveyResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Get the current user's internal UUID from context
	userInternalUUID, exists := c.Get("userInternalUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated",
		})
		return
	}

	// Check if response exists
	existing, err := GlobalSurveyResponseStore.Get(ctx, responseId)
	if err != nil {
		logger.Error("Failed to get survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey response",
		})
		return
	}

	if existing == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey response not found",
		})
		return
	}

	// Check owner access (Security Reviewers have owner role for triage actions)
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, responseId, userInternalUUID.(string), AuthorizationRoleOwner)
	if err != nil {
		logger.Error("Failed to check access: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to check access",
		})
		return
	}

	if !hasAccess {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Access denied",
		})
		return
	}

	if err := GlobalSurveyResponseStore.Approve(ctx, responseId, userInternalUUID.(string)); err != nil {
		if strings.Contains(err.Error(), "invalid state transition") {
			c.JSON(http.StatusConflict, Error{
				Error:            "conflict",
				ErrorDescription: err.Error(),
			})
			return
		}
		logger.Error("Failed to approve survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to approve survey response",
		})
		return
	}

	// Get the updated response
	updated, err := GlobalSurveyResponseStore.Get(ctx, responseId)
	if err != nil {
		logger.Error("Failed to get updated survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get updated survey response",
		})
		return
	}

	c.JSON(http.StatusOK, updated)
}

// ReturnTriageSurveyResponse returns a survey response for revision.
// POST /triage/surveys/responses/{response_id}/return
func (s *Server) ReturnTriageSurveyResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Get the current user's internal UUID from context
	userInternalUUID, exists := c.Get("userInternalUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated",
		})
		return
	}

	// Check if response exists
	existing, err := GlobalSurveyResponseStore.Get(ctx, responseId)
	if err != nil {
		logger.Error("Failed to get survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey response",
		})
		return
	}

	if existing == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey response not found",
		})
		return
	}

	// Check owner access (Security Reviewers have owner role for triage actions)
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, responseId, userInternalUUID.(string), AuthorizationRoleOwner)
	if err != nil {
		logger.Error("Failed to check access: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to check access",
		})
		return
	}

	if !hasAccess {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Access denied",
		})
		return
	}

	// Parse request body for revision notes
	var req struct {
		Notes string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// Notes are optional, so don't error on missing body
		req.Notes = ""
	}

	if err := GlobalSurveyResponseStore.Return(ctx, responseId, userInternalUUID.(string), req.Notes); err != nil {
		if strings.Contains(err.Error(), "invalid state transition") {
			c.JSON(http.StatusConflict, Error{
				Error:            "conflict",
				ErrorDescription: err.Error(),
			})
			return
		}
		logger.Error("Failed to return survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to return survey response",
		})
		return
	}

	// Get the updated response
	updated, err := GlobalSurveyResponseStore.Get(ctx, responseId)
	if err != nil {
		logger.Error("Failed to get updated survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get updated survey response",
		})
		return
	}

	c.JSON(http.StatusOK, updated)
}

// CreateThreatModelFromSurveyResponse creates a threat model from an approved survey response.
// POST /triage/surveys/responses/{response_id}/create_threat_model
func (s *Server) CreateThreatModelFromSurveyResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	// This handler will need integration with the ThreatModel store
	// For now, return not implemented as it requires complex integration
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Threat model creation from survey not yet implemented",
	})
}

// Helper function to parse UUID path parameter
func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}
