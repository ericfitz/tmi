package api

import (
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/ericfitz/tmi/internal/logging"
)

// This file demonstrates how to migrate existing validation patterns to the new unified framework
// It shows before/after examples for common validation scenarios

// BEFORE: Original validation pattern from diagram_metadata_handlers.go
func CreateDirectDiagramMetadataOld(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("CreateDirectDiagramMetadata - creating new metadata entry")

	// Extract diagram ID from URL
	diagramID := c.Param("id")
	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}

	// Validate diagram ID format
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse request body using generic function
	metadata, err := ParseRequestBody[Metadata](c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Manual validation of required fields
	if metadata.Key == "" {
		HandleRequestError(c, InvalidInputError("Metadata key is required"))
		return
	}
	if metadata.Value == "" {
		HandleRequestError(c, InvalidInputError("Metadata value is required"))
		return
	}

	// Continue with business logic...
	logger.Debug("Creating metadata key '%s' for diagram %s (user: %s)", metadata.Key, diagramID, userName)
}

// AFTER: Using the new unified validation framework
func CreateDirectDiagramMetadataNew(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("CreateDirectDiagramMetadata - creating new metadata entry")

	// Extract and validate diagram ID (unchanged - URL parameter validation is separate)
	diagramID := c.Param("id")
	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user (unchanged)
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Use new unified validation - replaces ParseRequestBody + manual validation
	metadata, err := ValidateAndParseRequest[Metadata](c, ValidationConfigs["metadata_create"])
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Continue with business logic - validation is complete!
	logger.Debug("Creating metadata key '%s' for diagram %s (user: %s)", metadata.Key, diagramID, userName)
}

// Example for a more complex endpoint: Threat Model Creation

// BEFORE: Complex two-phase validation from threat_model_handlers.go
func CreateThreatModelOld(c *gin.Context) {
	// Phase 1: Parse raw JSON to check prohibited fields
	var rawRequest map[string]interface{}
	if err := c.ShouldBindJSON(&rawRequest); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid JSON format"))
		return
	}

	// Check for prohibited fields
	prohibitedFields := []string{
		"document_count", "source_count", "diagram_count", "threat_count",
		"id", "created_at", "modified_at", "created_by", "owner",
		"diagrams", "documents", "threats", "sourceCode",
	}

	for _, field := range prohibitedFields {
		if _, exists := rawRequest[field]; exists {
			HandleRequestError(c, InvalidInputError(fmt.Sprintf(
				"Field '%s' is not allowed in POST requests. %s",
				field, getFieldErrorMessage(field))))
			return
		}
	}

	// Phase 2: Re-marshal and unmarshal into typed struct
	var request CreateThreatModelRequest
	rawJSON, err := json.Marshal(rawRequest)
	if err != nil {
		HandleRequestError(c, InvalidInputError("Failed to process request"))
		return
	}
	if err := json.Unmarshal(rawJSON, &request); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request format"))
		return
	}

	// Manual validation of required fields
	if request.Name == "" {
		HandleRequestError(c, InvalidInputError("Field 'name' is required"))
		return
	}

	// Custom validation
	if err := ValidateAuthorizationEntriesWithFormat(request.Authorization); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Continue with business logic...
}

// AFTER: Using unified validation framework  
func CreateThreatModelNew(c *gin.Context) {
	// Single call replaces all the validation above!
	request, err := ValidateAndParseRequest[CreateThreatModelRequest](c, ValidationConfigs["threat_model_create"])
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Continue with business logic - all validation is complete!
	// - Prohibited fields checked with contextual error messages
	// - Required fields validated automatically from binding tags  
	// - Custom validators (authorization) run automatically
	// - Consistent error message format
	_ = request // Use the validated request for business logic
}

// Migration Benefits Summary:
// 
// 1. CODE REDUCTION:
//    - Old metadata handler: ~25 lines of validation code
//    - New metadata handler: ~3 lines of validation code
//    - Old threat model handler: ~45 lines of validation code  
//    - New threat model handler: ~3 lines of validation code
//
// 2. CONSISTENCY:
//    - All endpoints use same validation approach
//    - Consistent error message format
//    - Centralized prohibited field checking
//
// 3. MAINTAINABILITY:
//    - Add new prohibited fields in one place (ValidationConfigs)
//    - Update error messages in one place (FieldErrorRegistry)
//    - Custom validators reusable across endpoints
//
// 4. RELIABILITY:
//    - No more manual required field validation (uses binding tags)
//    - No more duplicate validation logic
//    - Comprehensive test coverage for validation framework

// Required struct definitions for examples
type CreateThreatModelRequest struct {
	Name                 string          `json:"name" binding:"required"`
	Description          *string         `json:"description,omitempty"`
	ThreatModelFramework string          `json:"threat_model_framework,omitempty"`
	IssueUrl             *string         `json:"issue_url,omitempty"`
	Authorization        []Authorization `json:"authorization,omitempty"`
}