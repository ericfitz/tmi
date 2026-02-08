package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// Survey status constants (free-form strings matching ThreatModel pattern)
const (
	SurveyStatusActive   = "active"
	SurveyStatusInactive = "inactive"
	SurveyStatusArchived = "archived"
)

// Survey response status constants
const (
	ResponseStatusDraft          = "draft"
	ResponseStatusSubmitted      = "submitted"
	ResponseStatusNeedsRevision  = "needs_revision"
	ResponseStatusReadyForReview = "ready_for_review"
	ResponseStatusReviewCreated  = "review_created"
)

// Survey Admin Handlers

// ListAdminSurveys returns a paginated list of all surveys.
// GET /admin/surveys
func (s *Server) ListAdminSurveys(c *gin.Context, params ListAdminSurveysParams) {
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
	var status *string
	if params.Status != nil {
		status = params.Status
	}

	items, total, err := GlobalSurveyStore.List(ctx, limit, offset, status)
	if err != nil {
		logger.Error("Failed to list surveys: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to list surveys",
		})
		return
	}

	c.JSON(http.StatusOK, ListSurveysResponse{
		Surveys: items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	})
}

// CreateAdminSurvey creates a new survey.
// POST /admin/surveys
func (s *Server) CreateAdminSurvey(c *gin.Context) {
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

	var req SurveyBase
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Debug("Invalid request body: %v", err)
		HandleRequestError(c, InvalidInputError("Invalid request body"))
		return
	}

	// Validate survey_json has a pages array
	if err := validateSurveyJSON(req.SurveyJson); err != nil {
		HandleRequestError(c, InvalidInputError(err.Error()))
		return
	}

	// Create the survey struct
	survey := &Survey{
		Name:        req.Name,
		Description: req.Description,
		Version:     req.Version,
		Status:      req.Status,
		SurveyJson:  req.SurveyJson,
		Settings:    req.Settings,
	}

	// Create in store
	if err := GlobalSurveyStore.Create(ctx, survey, userInternalUUID); err != nil {
		if isDuplicateConstraintError(err) {
			c.JSON(http.StatusConflict, Error{
				Error:            "conflict",
				ErrorDescription: "A survey with this name and version already exists",
			})
			return
		}
		logger.Error("Failed to create survey: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to create survey",
		})
		return
	}

	// Emit webhook event
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:    EventSurveyCreated,
			ResourceID:   survey.Id.String(),
			ResourceType: "survey",
			OwnerID:      userInternalUUID,
			Data: map[string]interface{}{
				"name":        survey.Name,
				"description": survey.Description,
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, payload)
	}

	c.JSON(http.StatusCreated, survey)
}

// GetAdminSurvey returns a specific survey.
// GET /admin/surveys/{survey_id}
func (s *Server) GetAdminSurvey(c *gin.Context, surveyId SurveyId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	survey, err := GlobalSurveyStore.Get(ctx, surveyId)
	if err != nil {
		logger.Error("Failed to get survey: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey",
		})
		return
	}

	if survey == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey not found",
		})
		return
	}

	c.JSON(http.StatusOK, survey)
}

// UpdateAdminSurvey fully updates a survey.
// PUT /admin/surveys/{survey_id}
func (s *Server) UpdateAdminSurvey(c *gin.Context, surveyId SurveyId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Check if survey exists
	existing, err := GlobalSurveyStore.Get(ctx, surveyId)
	if err != nil {
		logger.Error("Failed to get survey: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey",
		})
		return
	}

	if existing == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey not found",
		})
		return
	}

	var req SurveyBase
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Debug("Invalid request body: %v", err)
		HandleRequestError(c, InvalidInputError("Invalid request body"))
		return
	}

	// Reject updates to archived surveys
	if existing.Status != nil && *existing.Status == SurveyStatusArchived {
		c.JSON(http.StatusConflict, Error{
			Error:            "conflict",
			ErrorDescription: "Cannot update an archived survey",
		})
		return
	}

	// Validate survey_json has a pages array
	if err := validateSurveyJSON(req.SurveyJson); err != nil {
		HandleRequestError(c, InvalidInputError(err.Error()))
		return
	}

	// Build updated survey
	survey := &Survey{
		Id:          &surveyId,
		Name:        req.Name,
		Description: req.Description,
		Version:     req.Version,
		Status:      req.Status,
		SurveyJson:  req.SurveyJson,
		Settings:    req.Settings,
	}

	if err := GlobalSurveyStore.Update(ctx, survey); err != nil {
		if isDuplicateConstraintError(err) {
			c.JSON(http.StatusConflict, Error{
				Error:            "conflict",
				ErrorDescription: "A survey with this name and version already exists",
			})
			return
		}
		logger.Error("Failed to update survey: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to update survey",
		})
		return
	}

	// Get the updated survey
	updated, err := GlobalSurveyStore.Get(ctx, surveyId)
	if err != nil {
		logger.Error("Failed to get updated survey: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get updated survey",
		})
		return
	}

	// Emit webhook event
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:    EventSurveyUpdated,
			ResourceID:   surveyId.String(),
			ResourceType: "survey",
			Data: map[string]interface{}{
				"name":        updated.Name,
				"description": updated.Description,
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, payload)
	}

	c.JSON(http.StatusOK, updated)
}

// PatchAdminSurvey partially updates a survey.
// PATCH /admin/surveys/{survey_id}
func (s *Server) PatchAdminSurvey(c *gin.Context, surveyId SurveyId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Check if survey exists
	existing, err := GlobalSurveyStore.Get(ctx, surveyId)
	if err != nil {
		logger.Error("Failed to get survey: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey",
		})
		return
	}

	if existing == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey not found",
		})
		return
	}

	// Reject updates to archived surveys
	if existing.Status != nil && *existing.Status == SurveyStatusArchived {
		c.JSON(http.StatusConflict, Error{
			Error:            "conflict",
			ErrorDescription: "Cannot update an archived survey",
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

	// Apply patch operations to the existing survey
	patched, err := ApplyPatchOperations(*existing, operations)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Ensure ID is preserved
	patched.Id = &surveyId

	if err := GlobalSurveyStore.Update(ctx, &patched); err != nil {
		logger.Error("Failed to update survey: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to update survey",
		})
		return
	}

	// Get the updated survey
	updated, err := GlobalSurveyStore.Get(ctx, surveyId)
	if err != nil {
		logger.Error("Failed to get updated survey: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get updated survey",
		})
		return
	}

	// Emit webhook event
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:    EventSurveyUpdated,
			ResourceID:   surveyId.String(),
			ResourceType: "survey",
			Data: map[string]interface{}{
				"name":        updated.Name,
				"description": updated.Description,
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, payload)
	}

	c.JSON(http.StatusOK, updated)
}

// DeleteAdminSurvey deletes a survey.
// DELETE /admin/surveys/{survey_id}
func (s *Server) DeleteAdminSurvey(c *gin.Context, surveyId SurveyId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Check if survey exists
	existing, err := GlobalSurveyStore.Get(ctx, surveyId)
	if err != nil {
		logger.Error("Failed to get survey: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey",
		})
		return
	}

	if existing == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey not found",
		})
		return
	}

	// Check if survey has responses
	hasResponses, err := GlobalSurveyStore.HasResponses(ctx, surveyId)
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
			ErrorDescription: "Cannot delete survey with existing responses",
		})
		return
	}

	if err := GlobalSurveyStore.Delete(ctx, surveyId); err != nil {
		logger.Error("Failed to delete survey: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to delete survey",
		})
		return
	}

	// Emit webhook event
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:    EventSurveyDeleted,
			ResourceID:   surveyId.String(),
			ResourceType: "survey",
			Data: map[string]interface{}{
				"name": existing.Name,
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, payload)
	}

	c.Status(http.StatusNoContent)
}

// Survey Intake Handlers (Developer-facing)

// ListIntakeSurveys returns a list of active surveys.
// GET /intake/surveys
func (s *Server) ListIntakeSurveys(c *gin.Context, params ListIntakeSurveysParams) {
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

	items, total, err := GlobalSurveyStore.ListActive(ctx, limit, offset)
	if err != nil {
		logger.Error("Failed to list active surveys: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to list surveys",
		})
		return
	}

	c.JSON(http.StatusOK, ListSurveysResponse{
		Surveys: items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	})
}

// GetIntakeSurvey returns a specific active survey for filling.
// GET /intake/surveys/{survey_id}
func (s *Server) GetIntakeSurvey(c *gin.Context, surveyId SurveyId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	survey, err := GlobalSurveyStore.Get(ctx, surveyId)
	if err != nil {
		logger.Error("Failed to get survey: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get survey",
		})
		return
	}

	if survey == nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey not found",
		})
		return
	}

	// Check if survey is active (intake endpoints only show active surveys)
	if survey.Status == nil || *survey.Status != SurveyStatusActive {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Survey not found or not active",
		})
		return
	}

	c.JSON(http.StatusOK, survey)
}

// ListIntakeSurveyResponses returns the current user's survey responses.
// GET /intake/survey_responses
func (s *Server) ListIntakeSurveyResponses(c *gin.Context, params ListIntakeSurveyResponsesParams) {
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

// CreateIntakeSurveyResponse creates a new survey response in draft status.
// POST /intake/survey_responses
func (s *Server) CreateIntakeSurveyResponse(c *gin.Context) {
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

	req, parseErr := ParseRequestBody[SurveyResponseCreateRequest](c)
	if parseErr != nil {
		HandleRequestError(c, parseErr)
		return
	}

	// Create the response struct
	response := &SurveyResponse{
		SurveyId:            req.SurveyId,
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
		if strings.Contains(errMsg, "survey not found") {
			c.JSON(http.StatusBadRequest, Error{
				Error:            "invalid_input",
				ErrorDescription: "Survey not found: " + response.SurveyId.String(),
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

	// Emit webhook event
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:    EventSurveyResponseCreated,
			ResourceID:   response.Id.String(),
			ResourceType: "survey_response",
			OwnerID:      userInternalUUID.(string),
			Data: map[string]interface{}{
				"survey_id": response.SurveyId.String(),
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, payload)
	}

	c.JSON(http.StatusCreated, response)
}

// GetIntakeSurveyResponse returns a specific survey response.
// GET /intake/survey_responses/{response_id}
func (s *Server) GetIntakeSurveyResponse(c *gin.Context, surveyResponseId SurveyResponseId) {
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

	response, err := GlobalSurveyResponseStore.Get(ctx, surveyResponseId)
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
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userInternalUUID.(string), AuthorizationRoleReader)
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

// UpdateIntakeSurveyResponse fully updates a survey response.
// PUT /intake/survey_responses/{response_id}
func (s *Server) UpdateIntakeSurveyResponse(c *gin.Context, surveyResponseId SurveyResponseId) {
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
	existing, err := GlobalSurveyResponseStore.Get(ctx, surveyResponseId)
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
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userInternalUUID.(string), AuthorizationRoleWriter)
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
	if existing.Status != nil && *existing.Status != ResponseStatusDraft && *existing.Status != ResponseStatusNeedsRevision {
		c.JSON(http.StatusConflict, Error{
			Error:            "conflict",
			ErrorDescription: "Can only update responses in draft or needs_revision status",
		})
		return
	}

	var req SurveyResponseBase
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Debug("Invalid request body: %v", err)
		HandleRequestError(c, InvalidInputError("Invalid request body"))
		return
	}

	// Build updated response (preserving immutable fields)
	response := &SurveyResponse{
		Id:                  &surveyResponseId,
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
	updated, err := GlobalSurveyResponseStore.Get(ctx, surveyResponseId)
	if err != nil {
		logger.Error("Failed to get updated survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get updated survey response",
		})
		return
	}

	// Emit webhook event
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:    EventSurveyResponseUpdated,
			ResourceID:   surveyResponseId.String(),
			ResourceType: "survey_response",
			Data: map[string]interface{}{
				"survey_id": updated.SurveyId.String(),
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, payload)
	}

	c.JSON(http.StatusOK, updated)
}

// PatchIntakeSurveyResponse partially updates a survey response.
// PATCH /intake/survey_responses/{response_id}
// Supports status transitions: draft->submitted, needs_revision->submitted
func (s *Server) PatchIntakeSurveyResponse(c *gin.Context, surveyResponseId SurveyResponseId) {
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
	existing, err := GlobalSurveyResponseStore.Get(ctx, surveyResponseId)
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
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userInternalUUID.(string), AuthorizationRoleWriter)
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
	if existing.Status != nil && *existing.Status != ResponseStatusDraft && *existing.Status != ResponseStatusNeedsRevision {
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
		"/id", "/created_at", "/modified_at", "/owner", "/survey_id",
		"/survey_version", "/is_confidential", "/survey_json",
	}

	// Check for status change in operations
	hasStatusChange := false
	for _, op := range operations {
		if op.Path == "/status" {
			hasStatusChange = true
		}
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
	patched, err := ApplyPatchOperations(*existing, operations)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	patched.Id = &surveyResponseId

	// Handle status transition if status was changed
	if hasStatusChange && patched.Status != nil && *patched.Status != *existing.Status {
		newStatus := *patched.Status
		// Intake users can only transition to submitted
		if newStatus != ResponseStatusSubmitted {
			c.JSON(http.StatusConflict, Error{
				Error:            "conflict",
				ErrorDescription: fmt.Sprintf("Invalid status transition from %s to %s", *existing.Status, newStatus),
			})
			return
		}
		if err := GlobalSurveyResponseStore.UpdateStatus(ctx, surveyResponseId, newStatus, nil, nil); err != nil {
			if strings.Contains(err.Error(), "invalid state transition") {
				c.JSON(http.StatusConflict, Error{
					Error:            "conflict",
					ErrorDescription: err.Error(),
				})
				return
			}
			logger.Error("Failed to update survey response status: %v", err)
			c.JSON(http.StatusInternalServerError, Error{
				Error:            "server_error",
				ErrorDescription: "Failed to update survey response status",
			})
			return
		}
	}

	// Update non-status fields
	if err := GlobalSurveyResponseStore.Update(ctx, &patched); err != nil {
		logger.Error("Failed to update survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to update survey response",
		})
		return
	}

	updated, err := GlobalSurveyResponseStore.Get(ctx, surveyResponseId)
	if err != nil {
		logger.Error("Failed to get updated survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get updated survey response",
		})
		return
	}

	// Emit webhook event
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:    EventSurveyResponseUpdated,
			ResourceID:   surveyResponseId.String(),
			ResourceType: "survey_response",
			Data: map[string]interface{}{
				"survey_id": updated.SurveyId.String(),
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, payload)
	}

	c.JSON(http.StatusOK, updated)
}

// DeleteIntakeSurveyResponse deletes a draft survey response.
// DELETE /intake/survey_responses/{response_id}
func (s *Server) DeleteIntakeSurveyResponse(c *gin.Context, surveyResponseId SurveyResponseId) {
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
	existing, err := GlobalSurveyResponseStore.Get(ctx, surveyResponseId)
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
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userInternalUUID.(string), AuthorizationRoleOwner)
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

	if err := GlobalSurveyResponseStore.Delete(ctx, surveyResponseId); err != nil {
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

	// Emit webhook event
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:    EventSurveyResponseDeleted,
			ResourceID:   surveyResponseId.String(),
			ResourceType: "survey_response",
			Data: map[string]interface{}{
				"survey_id": existing.SurveyId.String(),
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, payload)
	}

	c.Status(http.StatusNoContent)
}

// Survey Triage Handlers (Security Engineer-facing)

// ListTriageSurveyResponses returns survey responses for triage.
// GET /triage/survey_responses
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
	if params.Status != nil || params.SurveyId != nil {
		filters = &SurveyResponseFilters{
			Status:   params.Status,
			SurveyID: params.SurveyId,
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
// GET /triage/survey_responses/{response_id}
func (s *Server) GetTriageSurveyResponse(c *gin.Context, surveyResponseId SurveyResponseId) {
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

	response, err := GlobalSurveyResponseStore.Get(ctx, surveyResponseId)
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
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userInternalUUID.(string), AuthorizationRoleReader)
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

// PatchTriageSurveyResponse partially updates a survey response for triage.
// PATCH /triage/survey_responses/{response_id}
// Supports status transitions: submitted->ready_for_review, submitted->needs_revision, ready_for_review->needs_revision
func (s *Server) PatchTriageSurveyResponse(c *gin.Context, surveyResponseId SurveyResponseId) {
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
	existing, err := GlobalSurveyResponseStore.Get(ctx, surveyResponseId)
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
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userInternalUUID.(string), AuthorizationRoleOwner)
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

	// Parse JSON Patch operations
	operations, err := ParsePatchRequest(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Validate patch operations against prohibited fields
	prohibitedPaths := []string{
		"/id", "/created_at", "/modified_at", "/owner", "/survey_id",
		"/survey_version", "/is_confidential", "/survey_json", "/ui_state",
	}

	hasStatusChange := false
	for _, op := range operations {
		if op.Path == "/status" {
			hasStatusChange = true
		}
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
	patched, err := ApplyPatchOperations(*existing, operations)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Handle status transition if status was changed
	if hasStatusChange && patched.Status != nil && *patched.Status != *existing.Status {
		newStatus := *patched.Status
		// Triage users can transition to ready_for_review or needs_revision
		if newStatus != ResponseStatusReadyForReview && newStatus != ResponseStatusNeedsRevision {
			c.JSON(http.StatusConflict, Error{
				Error:            "conflict",
				ErrorDescription: fmt.Sprintf("Invalid status transition from %s to %s", *existing.Status, newStatus),
			})
			return
		}

		reviewerUUID := userInternalUUID.(string)
		if err := GlobalSurveyResponseStore.UpdateStatus(ctx, surveyResponseId, newStatus, &reviewerUUID, patched.RevisionNotes); err != nil {
			if strings.Contains(err.Error(), "invalid state transition") || strings.Contains(err.Error(), "revision_notes required") {
				c.JSON(http.StatusConflict, Error{
					Error:            "conflict",
					ErrorDescription: err.Error(),
				})
				return
			}
			logger.Error("Failed to update survey response status: %v", err)
			c.JSON(http.StatusInternalServerError, Error{
				Error:            "server_error",
				ErrorDescription: "Failed to update survey response status",
			})
			return
		}
	}

	updated, err := GlobalSurveyResponseStore.Get(ctx, surveyResponseId)
	if err != nil {
		logger.Error("Failed to get updated survey response: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to get updated survey response",
		})
		return
	}

	// Emit webhook event
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:    EventSurveyResponseUpdated,
			ResourceID:   surveyResponseId.String(),
			ResourceType: "survey_response",
			Data: map[string]interface{}{
				"survey_id": updated.SurveyId.String(),
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, payload)
	}

	c.JSON(http.StatusOK, updated)
}

// CreateThreatModelFromSurveyResponse creates a threat model from an approved survey response.
// POST /triage/survey_responses/{response_id}/create_threat_model
func (s *Server) CreateThreatModelFromSurveyResponse(c *gin.Context, surveyResponseId SurveyResponseId) {
	// This handler will need integration with the ThreatModel store
	// For now, return not implemented as it requires complex integration
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Threat model creation from survey not yet implemented",
	})
}

// isDuplicateConstraintError checks if an error is a database unique constraint violation.
// Covers PostgreSQL, SQLite, SQL Server, and Oracle error messages.
func isDuplicateConstraintError(err error) bool {
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "duplicate key") ||
		strings.Contains(errMsg, "unique constraint") ||
		strings.Contains(errMsg, "ora-00001")
}

// validateSurveyJSON validates that survey_json is a non-null object containing a pages array
func validateSurveyJSON(surveyJSON map[string]interface{}) error {
	if surveyJSON == nil {
		return fmt.Errorf("survey_json is required")
	}
	pages, ok := surveyJSON["pages"]
	if !ok {
		return fmt.Errorf("survey_json must contain a 'pages' field")
	}
	if _, ok := pages.([]interface{}); !ok {
		return fmt.Errorf("survey_json 'pages' must be an array")
	}
	return nil
}
