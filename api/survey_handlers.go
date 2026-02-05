package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Survey Template Admin Handlers

// ListAdminSurveyTemplates returns a paginated list of all survey templates.
// GET /admin/survey_templates
func (s *Server) ListAdminSurveyTemplates(c *gin.Context, params ListAdminSurveyTemplatesParams) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Survey template listing not yet implemented",
	})
}

// CreateAdminSurveyTemplate creates a new survey template.
// POST /admin/survey_templates
func (s *Server) CreateAdminSurveyTemplate(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Survey template creation not yet implemented",
	})
}

// GetAdminSurveyTemplate returns a specific survey template.
// GET /admin/survey_templates/{template_id}
func (s *Server) GetAdminSurveyTemplate(c *gin.Context, templateId SurveyTemplateIdPathParam) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Survey template retrieval not yet implemented",
	})
}

// UpdateAdminSurveyTemplate fully updates a survey template.
// PUT /admin/survey_templates/{template_id}
func (s *Server) UpdateAdminSurveyTemplate(c *gin.Context, templateId SurveyTemplateIdPathParam) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Survey template update not yet implemented",
	})
}

// PatchAdminSurveyTemplate partially updates a survey template.
// PATCH /admin/survey_templates/{template_id}
func (s *Server) PatchAdminSurveyTemplate(c *gin.Context, templateId SurveyTemplateIdPathParam) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Survey template patch not yet implemented",
	})
}

// DeleteAdminSurveyTemplate deletes a survey template.
// DELETE /admin/survey_templates/{template_id}
func (s *Server) DeleteAdminSurveyTemplate(c *gin.Context, templateId SurveyTemplateIdPathParam) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Survey template deletion not yet implemented",
	})
}

// Survey Intake Handlers (Developer-facing)

// ListIntakeTemplates returns a list of active survey templates.
// GET /intake/templates
func (s *Server) ListIntakeTemplates(c *gin.Context, params ListIntakeTemplatesParams) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Intake template listing not yet implemented",
	})
}

// GetIntakeTemplate returns a specific active survey template for filling.
// GET /intake/templates/{template_id}
func (s *Server) GetIntakeTemplate(c *gin.Context, templateId SurveyTemplateIdPathParam) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Intake template retrieval not yet implemented",
	})
}

// ListIntakeResponses returns the current user's survey responses.
// GET /intake/responses
func (s *Server) ListIntakeResponses(c *gin.Context, params ListIntakeResponsesParams) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Intake response listing not yet implemented",
	})
}

// CreateIntakeResponse creates a new survey response in draft status.
// POST /intake/responses
func (s *Server) CreateIntakeResponse(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Intake response creation not yet implemented",
	})
}

// GetIntakeResponse returns a specific survey response.
// GET /intake/responses/{response_id}
func (s *Server) GetIntakeResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Intake response retrieval not yet implemented",
	})
}

// UpdateIntakeResponse fully updates a survey response.
// PUT /intake/responses/{response_id}
func (s *Server) UpdateIntakeResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Intake response update not yet implemented",
	})
}

// PatchIntakeResponse partially updates a survey response.
// PATCH /intake/responses/{response_id}
func (s *Server) PatchIntakeResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Intake response patch not yet implemented",
	})
}

// DeleteIntakeResponse deletes a draft survey response.
// DELETE /intake/responses/{response_id}
func (s *Server) DeleteIntakeResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Intake response deletion not yet implemented",
	})
}

// SubmitIntakeResponse submits a survey response for review.
// POST /intake/responses/{response_id}/submit
func (s *Server) SubmitIntakeResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Intake response submission not yet implemented",
	})
}

// Survey Triage Handlers (Security Engineer-facing)

// ListTriageSurveyResponses returns survey responses for triage.
// GET /triage/surveys/responses
func (s *Server) ListTriageSurveyResponses(c *gin.Context, params ListTriageSurveyResponsesParams) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Triage response listing not yet implemented",
	})
}

// GetTriageSurveyResponse returns a specific survey response for triage.
// GET /triage/surveys/responses/{response_id}
func (s *Server) GetTriageSurveyResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Triage response retrieval not yet implemented",
	})
}

// ApproveTriageSurveyResponse approves a survey response.
// POST /triage/surveys/responses/{response_id}/approve
func (s *Server) ApproveTriageSurveyResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Triage response approval not yet implemented",
	})
}

// ReturnTriageSurveyResponse returns a survey response for revision.
// POST /triage/surveys/responses/{response_id}/return
func (s *Server) ReturnTriageSurveyResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Triage response return not yet implemented",
	})
}

// CreateThreatModelFromSurveyResponse creates a threat model from an approved survey response.
// POST /triage/surveys/responses/{response_id}/create_threat_model
func (s *Server) CreateThreatModelFromSurveyResponse(c *gin.Context, responseId SurveyResponseIdPathParam) {
	c.JSON(http.StatusNotImplemented, Error{
		Error:            "not_implemented",
		ErrorDescription: "Threat model creation from survey not yet implemented",
	})
}
