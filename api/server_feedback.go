package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Usability Feedback Methods

// ListUsabilityFeedback lists usability feedback entries.
// SEM@cd6b617fb7aaaeb6491d79c87b09839f94b0fc3e: route list usability feedback requests to the feedback handler
func (s *Server) ListUsabilityFeedback(c *gin.Context, params ListUsabilityFeedbackParams) {
	s.usabilityFeedbackHandler.List(c)
}

// CreateUsabilityFeedback creates a usability feedback entry.
// SEM@cd6b617fb7aaaeb6491d79c87b09839f94b0fc3e: route create usability feedback requests to the feedback handler
func (s *Server) CreateUsabilityFeedback(c *gin.Context) {
	s.usabilityFeedbackHandler.Create(c)
}

// GetUsabilityFeedback retrieves a usability feedback entry by ID.
// SEM@cd6b617fb7aaaeb6491d79c87b09839f94b0fc3e: route fetch usability feedback by ID requests to the feedback handler
func (s *Server) GetUsabilityFeedback(c *gin.Context, id openapi_types.UUID) {
	s.usabilityFeedbackHandler.Get(c)
}

// Content Feedback Methods

// ListContentFeedback lists content feedback entries for a threat model.
// SEM@cd6b617fb7aaaeb6491d79c87b09839f94b0fc3e: route list content feedback requests for a threat model to the feedback handler
func (s *Server) ListContentFeedback(c *gin.Context, threatModelId ThreatModelId, params ListContentFeedbackParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.contentFeedbackHandler.List(c)
}

// CreateContentFeedback creates a content feedback entry for a threat model.
// SEM@cd6b617fb7aaaeb6491d79c87b09839f94b0fc3e: route create content feedback requests for a threat model to the feedback handler
func (s *Server) CreateContentFeedback(c *gin.Context, threatModelId ThreatModelId) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.contentFeedbackHandler.Create(c)
}

// GetContentFeedback retrieves a content feedback entry by ID within a threat model.
// SEM@cd6b617fb7aaaeb6491d79c87b09839f94b0fc3e: route content feedback fetch request to the content feedback handler
func (s *Server) GetContentFeedback(c *gin.Context, threatModelId ThreatModelId, feedbackId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "feedback_id", Value: feedbackId.String()})
	s.contentFeedbackHandler.Get(c)
}
