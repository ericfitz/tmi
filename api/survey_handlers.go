package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// getUserUUID safely extracts the userInternalUUID from the Gin context.
// Returns the UUID string and true if successful, or an empty string and false if
// the value is missing or not a string. On failure, it writes an appropriate
// error response to the Gin context.
func getUserUUID(c *gin.Context) (string, bool) {
	val, exists := c.Get("userInternalUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated",
		})
		return "", false
	}
	uuid, ok := val.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Invalid user context",
		})
		return "", false
	}
	return uuid, true
}

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

// Mapped field name constants for survey-to-threat-model field dispatch.
const (
	tmFieldName         = "name"
	tmFieldDescription  = "description"
	tmFieldIssueURI     = "issue_uri"
	tmFieldAssets       = "assets"
	tmFieldDocuments    = "documents"
	tmFieldRepositories = "repositories"
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
	if errMsg := StrictJSONBind(c, &req); errMsg != "" {
		logger.Debug("Invalid request body: %s", errMsg)
		HandleRequestError(c, InvalidInputError(errMsg))
		return
	}

	// Validate survey_json has a pages array
	if err := validateSurveyJSON(req.SurveyJson); err != nil {
		HandleRequestError(c, InvalidInputError(err.Error()))
		return
	}

	// Sanitize text fields (defense-in-depth)
	req.Name = SanitizePlainText(req.Name)
	req.Description = SanitizeOptionalString(req.Description)

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
			EventType:  EventSurveyCreated,
			ObjectID:   survey.Id.String(),
			ObjectType: "survey",
			OwnerID:    userInternalUUID,
			Data: map[string]any{
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
	if errMsg := StrictJSONBind(c, &req); errMsg != "" {
		logger.Debug("Invalid request body: %s", errMsg)
		HandleRequestError(c, InvalidInputError(errMsg))
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

	// Sanitize text fields (defense-in-depth)
	req.Name = SanitizePlainText(req.Name)
	req.Description = SanitizeOptionalString(req.Description)

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
			EventType:  EventSurveyUpdated,
			ObjectID:   surveyId.String(),
			ObjectType: "survey",
			Data: map[string]any{
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

	// Sanitize text fields on the patched result (defense-in-depth)
	patched.Name = SanitizePlainText(patched.Name)
	patched.Description = SanitizeOptionalString(patched.Description)

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
			EventType:  EventSurveyUpdated,
			ObjectID:   surveyId.String(),
			ObjectType: "survey",
			Data: map[string]any{
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
			EventType:  EventSurveyDeleted,
			ObjectID:   surveyId.String(),
			ObjectType: "survey",
			Data: map[string]any{
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
	userUUID, ok := getUserUUID(c)
	if !ok {
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

	items, total, err := GlobalSurveyResponseStore.ListByOwner(ctx, userUUID, limit, offset, params.Status)
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
	userUUID, ok := getUserUUID(c)
	if !ok {
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

	// Copy answers if provided
	if req.Answers != nil {
		response.Answers = req.Answers
	}

	// Create in store
	if err := GlobalSurveyResponseStore.Create(ctx, response, userUUID); err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "survey not found") {
			c.JSON(http.StatusBadRequest, Error{
				Error:            "invalid_input",
				ErrorDescription: "Survey not found: " + response.SurveyId.String(),
			})
			return
		}
		if isForeignKeyConstraintError(err) {
			logger.Warn("Foreign key constraint violation creating survey response: %v", err)
			c.JSON(http.StatusBadRequest, Error{
				Error:            "invalid_input",
				ErrorDescription: "Referenced resource not found (e.g. linked_threat_model_id does not exist)",
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

	extractSurveyAnswers(ctx, response)

	// Emit webhook event
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:  EventSurveyResponseCreated,
			ObjectID:   response.Id.String(),
			ObjectType: "survey_response",
			OwnerID:    userUUID,
			Data: map[string]any{
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
	userUUID, ok := getUserUUID(c)
	if !ok {
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
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userUUID, AuthorizationRoleReader)
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
	userUUID, ok := getUserUUID(c)
	if !ok {
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
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userUUID, AuthorizationRoleWriter)
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
	if errMsg := StrictJSONBind(c, &req); errMsg != "" {
		logger.Debug("Invalid request body: %s", errMsg)
		HandleRequestError(c, InvalidInputError(errMsg))
		return
	}

	// Build updated response (preserving immutable fields)
	response := &SurveyResponse{
		Id:                  &surveyResponseId,
		LinkedThreatModelId: req.LinkedThreatModelId,
	}

	// Copy answers if provided
	if req.Answers != nil {
		response.Answers = req.Answers
	}

	if err := GlobalSurveyResponseStore.Update(ctx, response); err != nil {
		if isForeignKeyConstraintError(err) {
			logger.Warn("Foreign key constraint violation updating survey response: %v", err)
			c.JSON(http.StatusBadRequest, Error{
				Error:            "invalid_input",
				ErrorDescription: "Referenced resource not found (e.g. linked_threat_model_id does not exist)",
			})
			return
		}
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

	extractSurveyAnswers(ctx, updated)

	// Emit webhook event
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:  EventSurveyResponseUpdated,
			ObjectID:   surveyResponseId.String(),
			ObjectType: "survey_response",
			Data: map[string]any{
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
	userUUID, ok := getUserUUID(c)
	if !ok {
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
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userUUID, AuthorizationRoleWriter)
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
		if op.Path == PatchPathStatus {
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
		if err := GlobalSurveyResponseStore.UpdateStatus(ctx, surveyResponseId, newStatus, nil, nil); err != nil {
			if strings.Contains(err.Error(), "invalid status value") || strings.Contains(err.Error(), "revision_notes required") {
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

	extractSurveyAnswers(ctx, updated)

	// Emit webhook event
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:  EventSurveyResponseUpdated,
			ObjectID:   surveyResponseId.String(),
			ObjectType: "survey_response",
			Data: map[string]any{
				"survey_id": updated.SurveyId.String(),
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, payload)
	}

	c.JSON(http.StatusOK, updated)
}

// DeleteIntakeSurveyResponse deletes a survey response.
// DELETE /intake/survey_responses/{response_id}
func (s *Server) DeleteIntakeSurveyResponse(c *gin.Context, surveyResponseId SurveyResponseId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Get the current user's internal UUID from context
	userUUID, ok := getUserUUID(c)
	if !ok {
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
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userUUID, AuthorizationRoleOwner)
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
			EventType:  EventSurveyResponseDeleted,
			ObjectID:   surveyResponseId.String(),
			ObjectType: "survey_response",
			Data: map[string]any{
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
	userUUID, ok := getUserUUID(c)
	if !ok {
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
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userUUID, AuthorizationRoleReader)
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
	userUUID, ok := getUserUUID(c)
	if !ok {
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
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userUUID, AuthorizationRoleOwner)
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
		if op.Path == PatchPathStatus {
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
		reviewerUUID := userUUID
		if err := GlobalSurveyResponseStore.UpdateStatus(ctx, surveyResponseId, newStatus, &reviewerUUID, patched.RevisionNotes); err != nil {
			if strings.Contains(err.Error(), "invalid status value") || strings.Contains(err.Error(), "revision_notes required") {
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

	extractSurveyAnswers(ctx, updated)

	// Emit webhook event
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:  EventSurveyResponseUpdated,
			ObjectID:   surveyResponseId.String(),
			ObjectType: "survey_response",
			Data: map[string]any{
				"survey_id": updated.SurveyId.String(),
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, payload)
	}

	c.JSON(http.StatusOK, updated)
}

// mappedAnswerResult holds the processed results of mapping survey answers
// to threat model fields.
type mappedAnswerResult struct {
	name         *string
	description  *string
	issueURI     *string
	metadata     []Metadata
	assets       []any
	documents    []any
	repositories []any
}

// buildThreatModelName returns the TM name from the mapped name field, or
// constructs a fallback from template name, project name, and current date.
func buildThreatModelName(mappedName *string, templateName, projectName string) string {
	if mappedName != nil && *mappedName != "" {
		return *mappedName
	}

	date := time.Now().UTC().Format("2006-01-02")
	if projectName != "" {
		return fmt.Sprintf("%s: %s - %s", templateName, projectName, date)
	}
	return fmt.Sprintf("%s - %s", templateName, date)
}

// processMappedAnswers iterates all answer rows and dispatches them to the
// appropriate TM field based on mapsToTmField. Unmapped answers become metadata.
func processMappedAnswers(answers []SurveyAnswerRow) mappedAnswerResult {
	logger := slogging.Get()
	var result mappedAnswerResult

	for _, row := range answers {
		if row.MapsToTmField == nil {
			// Unmapped answer -> metadata
			result.metadata = append(result.metadata, Metadata{
				Key:   row.QuestionName,
				Value: flattenAndSanitize(row.AnswerValue),
			})
			continue
		}

		field := *row.MapsToTmField
		switch {
		case field == tmFieldName:
			val := flattenAndSanitize(row.AnswerValue)
			result.name = &val

		case field == tmFieldDescription:
			val := flattenAndSanitize(row.AnswerValue)
			result.description = &val

		case field == tmFieldIssueURI:
			val := flattenAndSanitize(row.AnswerValue)
			result.issueURI = &val

		case strings.HasPrefix(field, "metadata."):
			key := strings.TrimPrefix(field, "metadata.")
			result.metadata = append(result.metadata, Metadata{
				Key:   SanitizePlainText(key),
				Value: flattenAndSanitize(row.AnswerValue),
			})

		case field == tmFieldAssets || field == tmFieldDocuments || field == tmFieldRepositories:
			items, fallback := parseCollectionAnswer(field, row.AnswerValue)
			result.metadata = append(result.metadata, fallback...)
			switch field {
			case tmFieldAssets:
				result.assets = append(result.assets, items...)
			case tmFieldDocuments:
				result.documents = append(result.documents, items...)
			case tmFieldRepositories:
				result.repositories = append(result.repositories, items...)
			}

		default:
			logger.Warn("unrecognized mapsToTmField %q on question %q, falling back to metadata", field, row.QuestionName)
			result.metadata = append(result.metadata, Metadata{
				Key:   field,
				Value: flattenAndSanitize(row.AnswerValue),
			})
		}
	}

	return result
}

// createThreatModelFromResponse builds and creates a ThreatModel from a survey
// response's answers, mapping fields according to mapsToTmField directives.
func createThreatModelFromResponse(ctx context.Context, response *SurveyResponse) (*ThreatModel, error) {
	logger := slogging.Get()

	// Step 1: Get all answers
	answers, err := GlobalSurveyAnswerStore.GetAnswers(ctx, response.Id.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get answers for response %s: %w", response.Id.String(), err)
	}

	// Step 2: Process mapped fields
	mapped := processMappedAnswers(answers)

	// Step 3: Build TM name
	var templateName string
	if GlobalSurveyStore != nil {
		survey, err := GlobalSurveyStore.Get(ctx, response.SurveyId)
		switch {
		case err != nil:
			logger.Warn("failed to load survey template %s for TM name fallback: %v", response.SurveyId.String(), err)
			templateName = "Survey"
		case survey == nil:
			logger.Warn("survey template %s not found for TM name fallback", response.SurveyId.String())
			templateName = "Survey"
		default:
			templateName = survey.Name
		}
	}

	var projectName string
	if response.ProjectId != nil && GlobalProjectStore != nil {
		project, err := GlobalProjectStore.Get(ctx, response.ProjectId.String())
		if err != nil {
			logger.Warn("failed to load project %s for TM name fallback: %v", response.ProjectId.String(), err)
		} else if project != nil {
			projectName = project.Name
		}
	}

	tmName := buildThreatModelName(mapped.name, templateName, projectName)

	// Step 4: Build metadata
	metadata := &mapped.metadata
	if err := SanitizeMetadataSlice(metadata); err != nil {
		logger.Warn("metadata sanitization warning: %v", err)
	}

	// Filter out metadata entries with empty values (e.g., unanswered survey questions)
	// to prevent "value: cannot be empty" validation errors during persistence.
	filtered := make([]Metadata, 0, len(*metadata))
	for _, m := range *metadata {
		if strings.TrimSpace(m.Value) != "" {
			filtered = append(filtered, m)
		}
	}
	metadata = &filtered

	// Step 5: Build owner and authorization
	owner := *response.Owner
	authorizations := []Authorization{
		{
			PrincipalType: AuthorizationPrincipalTypeUser,
			Provider:      owner.Provider,
			ProviderId:    owner.ProviderId,
			Role:          RoleOwner,
		},
	}

	// Set security reviewer from the person who reviewed the response
	var securityReviewer *User
	if response.ReviewedBy != nil {
		securityReviewer = response.ReviewedBy
	}

	// Apply security reviewer rule (auto-add to authorization)
	authorizations = ApplySecurityReviewerRule(authorizations, securityReviewer)

	// Step 6: Copy confidentiality
	isConfidential := response.IsConfidential

	// Step 7: Build and create threat model
	now := time.Now().UTC()
	emptyThreats := []Threat{}

	tm := ThreatModel{
		Name:             tmName,
		Description:      mapped.description,
		IssueUri:         mapped.issueURI,
		IsConfidential:   isConfidential,
		SecurityReviewer: securityReviewer,
		CreatedAt:        &now,
		ModifiedAt:       &now,
		Owner:            owner,
		CreatedBy:        &owner,
		Authorization:    &authorizations,
		Metadata:         metadata,
		Threats:          &emptyThreats,
	}

	// Copy project reference if set
	if response.ProjectId != nil {
		tm.ProjectId = response.ProjectId
	}

	idSetter := func(tm ThreatModel, id string) ThreatModel {
		uuid, _ := ParseUUID(id)
		tm.Id = &uuid
		return tm
	}

	createdTM, err := ThreatModelStore.Create(tm, idSetter)
	if err != nil {
		return nil, fmt.Errorf("failed to create threat model: %w", err)
	}

	// Step 8: Create sub-resources (non-fatal failures)
	tmID := createdTM.Id.String()

	for _, item := range mapped.assets {
		asset, ok := item.(Asset)
		if !ok {
			logger.Warn("skipping non-Asset item in mapped assets for TM %s", tmID)
			continue
		}
		if err := GlobalAssetStore.Create(ctx, &asset, tmID); err != nil {
			logger.Warn("failed to create asset %q for TM %s: %v", asset.Name, tmID, err)
		}
	}

	for _, item := range mapped.documents {
		doc, ok := item.(Document)
		if !ok {
			logger.Warn("skipping non-Document item in mapped documents for TM %s", tmID)
			continue
		}
		if err := GlobalDocumentStore.Create(ctx, &doc, tmID); err != nil {
			logger.Warn("failed to create document %q for TM %s: %v", doc.Name, tmID, err)
		}
	}

	for _, item := range mapped.repositories {
		repo, ok := item.(Repository)
		if !ok {
			logger.Warn("skipping non-Repository item in mapped repositories for TM %s", tmID)
			continue
		}
		repoName := ""
		if repo.Name != nil {
			repoName = *repo.Name
		}
		if err := GlobalRepositoryStore.Create(ctx, &repo, tmID); err != nil {
			logger.Warn("failed to create repository %q for TM %s: %v", repoName, tmID, err)
		}
	}

	return &createdTM, nil
}

// CreateThreatModelFromSurveyResponse creates a threat model from an approved survey response.
// POST /triage/survey_responses/{response_id}/create_threat_model
func (s *Server) CreateThreatModelFromSurveyResponse(c *gin.Context, surveyResponseId SurveyResponseId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Step 1: Load survey response
	response, err := GlobalSurveyResponseStore.Get(ctx, surveyResponseId)
	if err != nil {
		logger.WithContext(c).Error("failed to get survey response %s: %v", surveyResponseId.String(), err)
		HandleRequestError(c, NotFoundError("Survey response not found"))
		return
	}

	// Step 2: Extract user identity
	userInternalUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Step 3: Check access
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userInternalUUID, AuthorizationRoleOwner)
	if err != nil {
		logger.WithContext(c).Error("failed to check access for survey response %s: %v", surveyResponseId.String(), err)
		HandleRequestError(c, ServerError("Failed to check access"))
		return
	}
	if !hasAccess {
		HandleRequestError(c, ForbiddenError("Insufficient permissions"))
		return
	}

	// Step 4: Validate preconditions
	if response.Owner == nil {
		logger.WithContext(c).Error("survey response %s has nil owner", surveyResponseId.String())
		HandleRequestError(c, ServerError("Survey response owner not found"))
		return
	}

	if response.Status == nil || *response.Status != ResponseStatusReadyForReview {
		currentStatus := string(ComponentHealthStatusUnknown)
		if response.Status != nil {
			currentStatus = *response.Status
		}
		c.JSON(http.StatusConflict, Error{
			Error:            "conflict",
			ErrorDescription: fmt.Sprintf("Survey response must be in '%s' status to create a threat model (current: '%s')", ResponseStatusReadyForReview, currentStatus),
		})
		return
	}

	if response.CreatedThreatModelId != nil {
		c.JSON(http.StatusConflict, Error{
			Error:            "conflict",
			ErrorDescription: fmt.Sprintf("A threat model has already been created from this survey response (threat_model_id: %s)", response.CreatedThreatModelId.String()),
		})
		return
	}

	// Step 5: Create threat model from response
	createdTM, err := createThreatModelFromResponse(ctx, response)
	if err != nil {
		logger.WithContext(c).Error("failed to create threat model from survey response %s: %v", surveyResponseId.String(), err)
		HandleRequestError(c, ServerError("Failed to create threat model"))
		return
	}

	// Step 6: Update survey response with created threat model ID
	if err := GlobalSurveyResponseStore.SetCreatedThreatModel(ctx, surveyResponseId, createdTM.Id.String()); err != nil {
		logger.WithContext(c).Error("failed to update survey response %s after TM creation: %v", surveyResponseId.String(), err)
	}

	// Step 7: Record audit
	RecordAuditCreate(c, createdTM.Id.String(), "threat_model", createdTM.Id.String(), createdTM)

	// Step 8: Broadcast notification
	BroadcastThreatModelCreated(userEmail, createdTM.Id.String(), createdTM.Name)

	// Step 9: Emit webhooks
	if GlobalEventEmitter != nil {
		tmPayload := EventPayload{
			EventType:     EventThreatModelCreated,
			ThreatModelID: createdTM.Id.String(),
			ObjectID:      createdTM.Id.String(),
			ObjectType:    "threat_model",
			OwnerID:       GetOwnerInternalUUID(ctx, createdTM.Owner.Provider, createdTM.Owner.ProviderId),
			Data: map[string]any{
				"name":        createdTM.Name,
				"description": createdTM.Description,
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, tmPayload)

		responsePayload := EventPayload{
			EventType:  EventSurveyResponseUpdated,
			ObjectID:   surveyResponseId.String(),
			ObjectType: "survey_response",
			Data: map[string]any{
				"survey_id": response.SurveyId.String(),
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, responsePayload)
	}

	// Step 10: Return 201
	c.Header("Location", "/threat_models/"+createdTM.Id.String())
	c.JSON(http.StatusCreated, CreateThreatModelFromSurveyResponse{
		ThreatModelId:    *createdTM.Id,
		SurveyResponseId: surveyResponseId,
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

// extractSurveyAnswers extracts answers from a survey response into the survey_answers table.
// This is non-fatal: errors are logged but do not fail the response save.
func extractSurveyAnswers(ctx context.Context, response *SurveyResponse) {
	if GlobalSurveyAnswerStore == nil {
		return
	}
	if response == nil || response.Id == nil || response.SurveyJson == nil {
		return
	}
	logger := slogging.Get()

	answersMap := make(map[string]any)
	if response.Answers != nil {
		answersMap = *response.Answers
	}

	status := "draft"
	if response.Status != nil {
		status = *response.Status
	}

	if err := GlobalSurveyAnswerStore.ExtractAndSave(ctx, response.Id.String(), *response.SurveyJson, answersMap, status); err != nil {
		logger.Warn("failed to extract survey answers for response %s: %v", response.Id.String(), err)
	}
}

// validateSurveyJSON validates that survey_json is a non-null object containing a pages array
// and that no two questions share the same mapsToTmField annotation.
func validateSurveyJSON(surveyJSON map[string]any) error {
	if surveyJSON == nil {
		return fmt.Errorf("survey_json is required")
	}
	pages, ok := surveyJSON["pages"]
	if !ok {
		return fmt.Errorf("survey_json must contain a 'pages' field")
	}
	if _, ok := pages.([]any); !ok {
		return fmt.Errorf("survey_json 'pages' must be an array")
	}

	// Validate no duplicate mapsToTmField annotations
	if _, err := ExtractQuestions(surveyJSON, nil); err != nil {
		return err
	}

	return nil
}
