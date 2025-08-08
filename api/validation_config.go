package api

import (
	"strings"
)

// FieldErrorRegistry provides contextual error messages for prohibited fields
type FieldErrorRegistry struct {
	messages map[string]string
}

// GetFieldErrorMessage returns a contextual error message for a prohibited field
func (r *FieldErrorRegistry) GetMessage(field, operation string) string {
	// Try operation-specific message first (convert operation to lowercase)
	operationKey := strings.ToLower(operation)
	if msg, exists := r.messages[field+"_"+operationKey]; exists {
		return msg
	}
	
	// Fall back to general field message
	if msg, exists := r.messages[field]; exists {
		return msg
	}
	
	// Default message
	return "This field cannot be set directly."
}

// Global field error registry
var fieldErrorRegistry = &FieldErrorRegistry{
	messages: map[string]string{
		// Owner field messages
		"owner_post": "The owner field is set automatically to the authenticated user during creation.",
		"owner_put":  "Owner can only be changed by the current owner.",
		"owner":      "The owner field is managed automatically by the system.",
		
		// ID and timestamp messages
		"id":          "The ID is read-only and set by the server.",
		"created_at":  "The creation timestamp is set automatically by the server.",
		"modified_at": "The modification timestamp is updated automatically by the server.",
		"created_by":  "The created_by field is set automatically to the authenticated user.",
		
		// Count fields
		"document_count": "Count fields are calculated automatically and cannot be set directly.",
		"source_count":   "Count fields are calculated automatically and cannot be set directly.",
		"diagram_count":  "Count fields are calculated automatically and cannot be set directly.",
		"threat_count":   "Count fields are calculated automatically and cannot be set directly.",
		
		// Sub-entity collections
		"diagrams":   "Diagrams should be managed through the dedicated diagrams endpoints.",
		"documents":  "Documents should be managed through the dedicated documents endpoints.",
		"threats":    "Threats should be managed through the dedicated threats endpoints.",
		"sourceCode": "Source code should be managed through the dedicated source endpoints.",
	},
}

// GetFieldErrorMessage is the global function to get error messages
func GetFieldErrorMessage(field, operation string) string {
	return fieldErrorRegistry.GetMessage(field, operation)
}

// ValidationConfigs defines validation rules for each endpoint
var ValidationConfigs = map[string]ValidationConfig{
	// Threat Model endpoints
	"threat_model_create": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at", "created_by", "owner",
			"document_count", "source_count", "diagram_count", "threat_count",
			"diagrams", "documents", "threats", "sourceCode",
		},
		CustomValidators: []ValidatorFunc{ValidateAuthorizationEntriesFunc},
		Operation:        "POST",
	},
	
	"threat_model_update": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at", "created_by",
			"document_count", "source_count", "diagram_count", "threat_count",
			"diagrams", "documents", "threats", "sourceCode",
		},
		CustomValidators: []ValidatorFunc{ValidateAuthorizationEntriesFunc},
		AllowOwnerField:  true,
		Operation:        "PUT",
	},
	
	// Diagram endpoints
	"diagram_create": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: []ValidatorFunc{ValidateDiagramType},
		Operation:        "POST",
	},
	
	"diagram_update": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: []ValidatorFunc{ValidateDiagramType},
		Operation:        "PUT",
	},
	
	// Document endpoints
	"document_create": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: []ValidatorFunc{ValidateUUIDFields},
		Operation:        "POST",
	},
	
	"document_update": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: []ValidatorFunc{ValidateUUIDFields},
		Operation:        "PUT",
	},
	
	// Source endpoints
	"source_create": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: []ValidatorFunc{ValidateUUIDFields},
		Operation:        "POST",
	},
	
	"source_update": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: []ValidatorFunc{ValidateUUIDFields},
		Operation:        "PUT",
	},
	
	// Threat endpoints
	"threat_create": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: []ValidatorFunc{ValidateUUIDFields},
		Operation:        "POST",
	},
	
	"threat_update": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: []ValidatorFunc{ValidateUUIDFields},
		Operation:        "PUT",
	},
	
	// Metadata endpoints (minimal validation)
	"metadata_create": {
		ProhibitedFields: []string{},
		CustomValidators: []ValidatorFunc{},
		Operation:        "POST",
	},
	
	"metadata_update": {
		ProhibitedFields: []string{},
		CustomValidators: []ValidatorFunc{},
		Operation:        "PUT",
	},
	
	// Cell endpoints
	"cell_create": {
		ProhibitedFields: []string{
			"id",
		},
		CustomValidators: []ValidatorFunc{ValidateUUIDFields},
		Operation:        "POST",
	},
	
	"cell_update": {
		ProhibitedFields: []string{
			"id",
		},
		CustomValidators: []ValidatorFunc{ValidateUUIDFields},
		Operation:        "PUT",
	},
	
	// Batch operations
	"batch_patch": {
		ProhibitedFields: []string{},
		CustomValidators: []ValidatorFunc{},
		Operation:        "PATCH",
	},
	
	"batch_delete": {
		ProhibitedFields: []string{},
		CustomValidators: []ValidatorFunc{},
		Operation:        "DELETE",
	},
}

// GetValidationConfig returns the validation config for an endpoint
func GetValidationConfig(endpoint string) (ValidationConfig, bool) {
	config, exists := ValidationConfigs[endpoint]
	return config, exists
}

// Common validator functions as variables to avoid redeclaration

// ValidateAuthorizationEntriesFunc validates authorization array
var ValidateAuthorizationEntriesFunc ValidatorFunc = ValidateAuthorizationEntriesFromStruct

// ValidateUUIDFieldsFunc validates UUID format for ID fields
var ValidateUUIDFieldsFunc ValidatorFunc = func(data interface{}) error {
	return ValidateUUIDFields(data)
}

// ValidateDiagramTypeFunc validates diagram type field
var ValidateDiagramTypeFunc ValidatorFunc = func(data interface{}) error {
	return ValidateDiagramType(data)
}