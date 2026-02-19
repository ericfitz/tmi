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
			if doc.Uri == "" {
				return InvalidInputError("Document URI is required")
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
			if doc.Uri == "" {
				return InvalidInputError("Document URI is required")
			}
			return nil
		}),
		Operation: "PUT",
	},

	// Note endpoints
	"note_create": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: append(CommonValidators.GetValidators([]string{
			"uuid_fields", "note_markdown", "string_length",
		}), func(data interface{}) error {
			// Validate required fields for notes
			note, ok := data.(*Note)
			if !ok {
				return InvalidInputError("Invalid data type for note validation")
			}
			if note.Name == "" {
				return InvalidInputError("Note name is required")
			}
			if note.Content == "" {
				return InvalidInputError("Note content is required")
			}
			return nil
		}),
		Operation: "POST",
	},

	"note_update": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: append(CommonValidators.GetValidators([]string{
			"uuid_fields", "note_markdown", "string_length",
		}), func(data interface{}) error {
			// Validate required fields for notes
			note, ok := data.(*Note)
			if !ok {
				return InvalidInputError("Invalid data type for note validation")
			}
			if note.Name == "" {
				return InvalidInputError("Note name is required")
			}
			if note.Content == "" {
				return InvalidInputError("Note content is required")
			}
			return nil
		}),
		Operation: "PUT",
	},

	// Triage Note endpoints (append-only, create only)
	"triage_note_create": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at", "created_by", "modified_by",
		},
		CustomValidators: append(CommonValidators.GetValidators([]string{
			"triage_note_markdown", "string_length",
		}), func(data interface{}) error {
			note, ok := data.(*TriageNote)
			if !ok {
				return InvalidInputError("Invalid data type for triage note validation")
			}
			if note.Name == "" {
				return InvalidInputError("Triage note name is required")
			}
			if note.Content == "" {
				return InvalidInputError("Triage note content is required")
			}
			return nil
		}),
		Operation: "POST",
	},

	// Repository endpoints
	"repository_create": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: append(CommonValidators.GetValidators([]string{
			"uuid_fields", "url_format", "no_html_injection", "string_length",
		}), func(data interface{}) error {
			// Validate required fields for repositories
			repository, ok := data.(*Repository)
			if !ok {
				return InvalidInputError("Invalid data type for repository validation")
			}
			if repository.Uri == "" {
				return InvalidInputError("Repository URI is required")
			}
			return nil
		}),
		Operation: "POST",
	},

	"repository_update": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: append(CommonValidators.GetValidators([]string{
			"uuid_fields", "url_format", "no_html_injection", "string_length",
		}), func(data interface{}) error {
			// Validate required fields for repositories
			repository, ok := data.(*Repository)
			if !ok {
				return InvalidInputError("Invalid data type for repository validation")
			}
			if repository.Uri == "" {
				return InvalidInputError("Repository URI is required")
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
			"uuid_fields", "threat_severity", "no_html_injection", "string_length", "score_precision",
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
			"uuid_fields", "threat_severity", "no_html_injection", "string_length", "score_precision",
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

	// Asset validation configurations
	"asset_create": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: append(CommonValidators.GetValidators([]string{
			"uuid_fields", "no_html_injection", "string_length",
		}), func(data interface{}) error {
			// Validate required fields for assets
			asset, ok := data.(*Asset)
			if !ok {
				return InvalidInputError("Invalid data type for asset validation")
			}
			if asset.Name == "" {
				return InvalidInputError("Asset name is required")
			}
			if asset.Type == "" {
				return InvalidInputError("Asset type is required")
			}
			// Validate asset type enum
			validTypes := map[AssetType]bool{
				"data": true, "hardware": true, "software": true,
				"infrastructure": true, "service": true, "personnel": true,
			}
			if !validTypes[asset.Type] {
				return InvalidInputError("Invalid asset type, must be one of: data, hardware, software, infrastructure, service, personnel")
			}
			// Validate array field lengths
			if asset.Classification != nil && len(*asset.Classification) > 50 {
				return InvalidInputError("Asset classification array exceeds maximum of 50 items")
			}
			// Validate string field lengths
			if asset.Sensitivity != nil && len(*asset.Sensitivity) > 128 {
				return InvalidInputError("Asset sensitivity exceeds maximum of 128 characters")
			}
			return nil
		}),
		Operation: "POST",
	},

	"asset_update": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: append(CommonValidators.GetValidators([]string{
			"uuid_fields", "no_html_injection", "string_length",
		}), func(data interface{}) error {
			// Validate required fields for assets
			asset, ok := data.(*Asset)
			if !ok {
				return InvalidInputError("Invalid data type for asset validation")
			}
			if asset.Name == "" {
				return InvalidInputError("Asset name is required")
			}
			if asset.Type == "" {
				return InvalidInputError("Asset type is required")
			}
			// Validate asset type enum
			validTypes := map[AssetType]bool{
				"data": true, "hardware": true, "software": true,
				"infrastructure": true, "service": true, "personnel": true,
			}
			if !validTypes[asset.Type] {
				return InvalidInputError("Invalid asset type, must be one of: data, hardware, software, infrastructure, service, personnel")
			}
			// Validate array field lengths
			if asset.Classification != nil && len(*asset.Classification) > 50 {
				return InvalidInputError("Asset classification array exceeds maximum of 50 items")
			}
			// Validate string field lengths
			if asset.Sensitivity != nil && len(*asset.Sensitivity) > 128 {
				return InvalidInputError("Asset sensitivity exceeds maximum of 128 characters")
			}
			return nil
		}),
		Operation: "PUT",
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

	// PATCH validation configs for simple resources
	// PATCH operations use JSON Patch (RFC 6902) with PatchOperation arrays
	// Validation is done on individual patch operations, not the whole resource
	"asset_patch": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: CommonValidators.GetValidators([]string{
			"no_html_injection", "string_length",
		}),
		Operation: "PATCH",
	},

	"document_patch": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: CommonValidators.GetValidators([]string{
			"no_html_injection", "string_length",
		}),
		Operation: "PATCH",
	},

	"note_patch": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: CommonValidators.GetValidators([]string{
			"no_html_injection", "string_length",
		}),
		Operation: "PATCH",
	},

	"repository_patch": {
		ProhibitedFields: []string{
			"id", "created_at", "modified_at",
		},
		CustomValidators: CommonValidators.GetValidators([]string{
			"no_html_injection", "string_length",
		}),
		Operation: "PATCH",
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
var ValidateUUIDFieldsFunc ValidatorFunc = ValidateUUIDFieldsFromStruct

// ValidateDiagramTypeFunc validates diagram type field
var ValidateDiagramTypeFunc ValidatorFunc = ValidateDiagramType
