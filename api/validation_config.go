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
		"created_at":  "Creation timestamp is read-only and set by the server.",
		"modified_at": "Modification timestamp is managed automatically by the server.",
		"created_by":  "The creator field is read-only and set during creation.",

		// Sub-entity collections
		"diagrams":   "Diagrams must be managed via the /threat_models/:threat_model_id/diagrams sub-entity endpoints.",
		"documents":  "Documents must be managed via the /threat_models/:threat_model_id/documents sub-entity endpoints.",
		"threats":    "Threats must be managed via the /threat_models/:threat_model_id/threats sub-entity endpoints.",
		"sourceCode": "Source code entries must be managed via the /threat_models/:threat_model_id/sources sub-entity endpoints.",
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
			"diagrams", "documents", "threats", "sourceCode",
		},
		CustomValidators: CommonValidators.GetValidators([]string{
			"authorization", "email_format", "no_html_injection", "string_length",
		}),
		Operation: "POST",
	},

	"threat_model_update": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at", "created_by",
			"diagrams", "documents", "threats", "sourceCode",
		},
		CustomValidators: CommonValidators.GetValidators([]string{
			"authorization", "email_format", "no_html_injection", "string_length",
		}),
		AllowOwnerField: true,
		Operation:       "PUT",
	},

	// Diagram endpoints
	"diagram_create": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: CommonValidators.GetValidators([]string{
			"diagram_type", "no_html_injection", "string_length",
		}),
		Operation: "POST",
	},

	"diagram_update": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: CommonValidators.GetValidators([]string{
			"diagram_type", "no_html_injection", "string_length",
		}),
		Operation: "PUT",
	},

	// Document endpoints
	"document_create": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: append(CommonValidators.GetValidators([]string{
			"uuid_fields", "url_format", "no_html_injection", "string_length",
		}), func(data interface{}) error {
			// Validate required fields for documents
			doc, ok := data.(*Document)
			if !ok {
				return InvalidInputError("Invalid data type for document validation")
			}
			if doc.Name == "" {
				return InvalidInputError("Document name is required")
			}
			if doc.Url == "" {
				return InvalidInputError("Document URL is required")
			}
			return nil
		}),
		Operation: "POST",
	},

	"document_update": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: append(CommonValidators.GetValidators([]string{
			"uuid_fields", "url_format", "no_html_injection", "string_length",
		}), func(data interface{}) error {
			// Validate required fields for documents
			doc, ok := data.(*Document)
			if !ok {
				return InvalidInputError("Invalid data type for document validation")
			}
			if doc.Name == "" {
				return InvalidInputError("Document name is required")
			}
			if doc.Url == "" {
				return InvalidInputError("Document URL is required")
			}
			return nil
		}),
		Operation: "PUT",
	},

	// Source endpoints
	"source_create": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: append(CommonValidators.GetValidators([]string{
			"uuid_fields", "url_format", "no_html_injection", "string_length",
		}), func(data interface{}) error {
			// Validate required fields for sources
			source, ok := data.(*Source)
			if !ok {
				return InvalidInputError("Invalid data type for source validation")
			}
			if source.Url == "" {
				return InvalidInputError("Source URL is required")
			}
			return nil
		}),
		Operation: "POST",
	},

	"source_update": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: append(CommonValidators.GetValidators([]string{
			"uuid_fields", "url_format", "no_html_injection", "string_length",
		}), func(data interface{}) error {
			// Validate required fields for sources
			source, ok := data.(*Source)
			if !ok {
				return InvalidInputError("Invalid data type for source validation")
			}
			if source.Url == "" {
				return InvalidInputError("Source URL is required")
			}
			return nil
		}),
		Operation: "PUT",
	},

	// Threat endpoints
	"threat_create": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: append(CommonValidators.GetValidators([]string{
			"uuid_fields", "threat_severity", "no_html_injection", "string_length",
		}), func(data interface{}) error {
			// Validate required fields for threats
			threat, ok := data.(*Threat)
			if !ok {
				return InvalidInputError("Invalid data type for threat validation")
			}
			if threat.Name == "" {
				return InvalidInputError("Threat name is required")
			}
			return nil
		}),
		Operation: "POST",
	},

	"threat_update": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: append(CommonValidators.GetValidators([]string{
			"uuid_fields", "threat_severity", "no_html_injection", "string_length",
		}), func(data interface{}) error {
			// Validate required fields for threats
			threat, ok := data.(*Threat)
			if !ok {
				return InvalidInputError("Invalid data type for threat validation")
			}
			if threat.Name == "" {
				return InvalidInputError("Threat name is required")
			}
			return nil
		}),
		Operation: "PUT",
	},

	// Metadata endpoints
	"metadata_create": {
		ProhibitedFields: []string{},
		CustomValidators: CommonValidators.GetValidators([]string{
			"metadata_key", "no_html_injection", "string_length",
		}),
		Operation: "POST",
	},

	"metadata_update": {
		ProhibitedFields: []string{},
		CustomValidators: CommonValidators.GetValidators([]string{
			"metadata_key", "no_html_injection", "string_length",
		}),
		Operation: "PUT",
	},

	// Cell endpoints
	"cell_create": {
		ProhibitedFields: []string{
			"id",
		},
		CustomValidators: []ValidatorFunc{ValidateUUIDFieldsFromStruct},
		Operation:        "POST",
	},

	"cell_update": {
		ProhibitedFields: []string{
			"id",
		},
		CustomValidators: []ValidatorFunc{ValidateUUIDFieldsFromStruct},
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
	return ValidateUUIDFieldsFromStruct(data)
}

// ValidateDiagramTypeFunc validates diagram type field
var ValidateDiagramTypeFunc ValidatorFunc = func(data interface{}) error {
	return ValidateDiagramType(data)
}
