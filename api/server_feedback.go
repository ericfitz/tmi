package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Usability Feedback Methods

// ListUsabilityFeedback lists usability feedback entries.
func (s *Server) ListUsabilityFeedback(c *gin.Context, params ListUsabilityFeedbackParams) {
	s.usabilityFeedbackHandler.List(c)
}

// CreateUsabilityFeedback creates a usability feedback entry.
func (s *Server) CreateUsabilityFeedback(c *gin.Context) {
	s.usabilityFeedbackHandler.Create(c)
}

// GetUsabilityFeedback retrieves a usability feedback entry by ID.
func (s *Server) GetUsabilityFeedback(c *gin.Context, id openapi_types.UUID) {
	s.usabilityFeedbackHandler.Get(c)
}

// Content Feedback Methods

// ListContentFeedback lists content feedback entries for a threat model.
func (s *Server) ListContentFeedback(c *gin.Context, threatModelId ThreatModelId, params ListContentFeedbackParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.contentFeedbackHandler.List(c)
}

// CreateContentFeedback creates a content feedback entry for a threat model.
func (s *Server) CreateContentFeedback(c *gin.Context, threatModelId ThreatModelId) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.contentFeedbackHandler.Create(c)
}

// GetContentFeedback retrieves a content feedback entry by ID within a threat model.
func (s *Server) GetContentFeedback(c *gin.Context, threatModelId ThreatModelId, feedbackId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "feedback_id", Value: feedbackId.String()})
	s.contentFeedbackHandler.Get(c)
}
