// Package validation provides cross-database validation for TMI models.
// These validators replace PostgreSQL CHECK constraints to enable
// consistent validation across all supported databases.
package validation

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Valid enum values for various fields
var (
	// ValidThreatModelFrameworks are the allowed threat modeling frameworks
	ValidThreatModelFrameworks = []string{"CIA", "STRIDE", "LINDDUN", "DIE", "PLOT4ai"}

	// ValidDiagramTypes are the allowed diagram types
	ValidDiagramTypes = []string{"DFD-1.0.0"}

	// ValidAssetTypes are the allowed asset types
	ValidAssetTypes = []string{"data", "hardware", "software", "infrastructure", "service", "personnel"}

	// ValidRepositoryTypes are the allowed repository types
	ValidRepositoryTypes = []string{"git", "svn", "mercurial", "other"}

	// ValidRoles are the allowed access roles
	ValidRoles = []string{"owner", "writer", "reader"}

	// ValidSubjectTypes are the allowed subject types for access control
	ValidSubjectTypes = []string{"user", "group"}

	// ValidWebhookStatuses are the allowed webhook subscription statuses
	ValidWebhookStatuses = []string{"pending_verification", "active", "pending_delete"}

	// ValidWebhookDeliveryStatuses are the allowed webhook delivery statuses
	ValidWebhookDeliveryStatuses = []string{"pending", "delivered", "failed"}

	// ValidWebhookPatternTypes are the allowed webhook URL deny list pattern types
	ValidWebhookPatternTypes = []string{"glob", "regex"}

	// ValidEntityTypes are the allowed metadata entity types
	ValidEntityTypes = []string{"threat_model", "threat", "diagram", "document", "repository", "cell", "note", "asset"}

	// EveryonePseudoGroupUUID is the reserved UUID for the "everyone" pseudo-group
	EveryonePseudoGroupUUID = "00000000-0000-0000-0000-000000000000"
)

// Regex patterns for validation
var (
	// metadataKeyPattern validates metadata keys: alphanumeric, underscore, hyphen only
	metadataKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// ValidationError represents a validation failure
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// NewValidationError creates a new validation error
func NewValidationError(field, message string) *ValidationError {
	return &ValidationError{Field: field, Message: message}
}

// --- Enum Validators ---

// ValidateEnum checks if a value is in an allowed list
func ValidateEnum(field, value string, allowed []string) error {
	for _, v := range allowed {
		if value == v {
			return nil
		}
	}
	return NewValidationError(field, fmt.Sprintf("must be one of: %s", strings.Join(allowed, ", ")))
}

// ValidateEnumPtr validates an optional enum field
func ValidateEnumPtr(field string, value *string, allowed []string) error {
	if value == nil {
		return nil
	}
	return ValidateEnum(field, *value, allowed)
}

// ValidateThreatModelFramework validates the threat model framework field
func ValidateThreatModelFramework(framework string) error {
	return ValidateEnum("threat_model_framework", framework, ValidThreatModelFrameworks)
}

// ValidateDiagramType validates the diagram type field
func ValidateDiagramType(diagramType string) error {
	return ValidateEnum("type", diagramType, ValidDiagramTypes)
}

// ValidateAssetType validates the asset type field
func ValidateAssetType(assetType string) error {
	return ValidateEnum("type", assetType, ValidAssetTypes)
}

// ValidateRepositoryType validates the repository type field
func ValidateRepositoryType(repoType string) error {
	return ValidateEnum("type", repoType, ValidRepositoryTypes)
}

// ValidateRole validates the access role field
func ValidateRole(role string) error {
	return ValidateEnum("role", role, ValidRoles)
}

// ValidateSubjectType validates the subject type field
func ValidateSubjectType(subjectType string) error {
	return ValidateEnum("subject_type", subjectType, ValidSubjectTypes)
}

// ValidateWebhookStatus validates the webhook subscription status field
func ValidateWebhookStatus(status string) error {
	return ValidateEnum("status", status, ValidWebhookStatuses)
}

// ValidateWebhookDeliveryStatus validates the webhook delivery status field
func ValidateWebhookDeliveryStatus(status string) error {
	return ValidateEnum("status", status, ValidWebhookDeliveryStatuses)
}

// ValidateWebhookPatternType validates the webhook URL deny list pattern type
func ValidateWebhookPatternType(patternType string) error {
	return ValidateEnum("pattern_type", patternType, ValidWebhookPatternTypes)
}

// ValidateEntityType validates the metadata entity type field
func ValidateEntityType(entityType string) error {
	return ValidateEnum("entity_type", entityType, ValidEntityTypes)
}

// --- String Validators ---

// ValidateNonEmpty checks that a string is not empty after trimming
func ValidateNonEmpty(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return NewValidationError(field, "cannot be empty")
	}
	return nil
}

// ValidateLength checks string length constraints
func ValidateLength(field, value string, min, max int) error {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < min {
		return NewValidationError(field, fmt.Sprintf("must be at least %d characters", min))
	}
	if len(value) > max {
		return NewValidationError(field, fmt.Sprintf("must be at most %d characters", max))
	}
	return nil
}

// ValidateStatusLength validates optional status field length (max 128)
func ValidateStatusLength(status *string) error {
	if status == nil {
		return nil
	}
	if len(*status) > 128 {
		return NewValidationError("status", "must be at most 128 characters")
	}
	return nil
}

// --- Metadata Validators ---

// ValidateMetadataKey validates a metadata key
func ValidateMetadataKey(key string) error {
	trimmed := strings.TrimSpace(key)
	if len(trimmed) == 0 {
		return NewValidationError("key", "cannot be empty")
	}
	if len(key) > 128 {
		return NewValidationError("key", "must be at most 128 characters")
	}
	if !metadataKeyPattern.MatchString(key) {
		return NewValidationError("key", "must contain only alphanumeric characters, underscores, and hyphens")
	}
	return nil
}

// ValidateMetadataValue validates a metadata value
func ValidateMetadataValue(value string) error {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) == 0 {
		return NewValidationError("value", "cannot be empty")
	}
	if len(value) > 65535 {
		return NewValidationError("value", "must be at most 65535 characters")
	}
	return nil
}

// --- Numeric Validators ---

// ValidateScore validates a threat score (0.0 to 10.0)
func ValidateScore(score *float64) error {
	if score == nil {
		return nil
	}
	if *score < 0.0 || *score > 10.0 {
		return NewValidationError("score", "must be between 0.0 and 10.0")
	}
	return nil
}

// --- XOR Constraint Validators ---

// ValidateSubjectXOR validates the XOR constraint for subject_type/user_internal_uuid/group_internal_uuid
// Used by ThreatModelAccess and Administrator models
func ValidateSubjectXOR(subjectType string, userUUID, groupUUID *string) error {
	switch subjectType {
	case "user":
		if userUUID == nil || *userUUID == "" {
			return NewValidationError("user_internal_uuid", "required when subject_type is 'user'")
		}
		if groupUUID != nil && *groupUUID != "" {
			return NewValidationError("group_internal_uuid", "must be empty when subject_type is 'user'")
		}
	case "group":
		if groupUUID == nil || *groupUUID == "" {
			return NewValidationError("group_internal_uuid", "required when subject_type is 'group'")
		}
		if userUUID != nil && *userUUID != "" {
			return NewValidationError("user_internal_uuid", "must be empty when subject_type is 'group'")
		}
	default:
		return NewValidationError("subject_type", "must be 'user' or 'group'")
	}
	return nil
}

// --- Group Protection Validators ---

// ValidateNotEveryoneGroup checks that operations don't target the protected "everyone" group
func ValidateNotEveryoneGroup(groupUUID string) error {
	if groupUUID == EveryonePseudoGroupUUID {
		return errors.New("cannot modify the 'everyone' pseudo-group")
	}
	return nil
}

// ValidateNotEveryoneGroupMember checks that members aren't added to "everyone" group
func ValidateNotEveryoneGroupMember(groupUUID string) error {
	if groupUUID == EveryonePseudoGroupUUID {
		return errors.New("cannot add members to the 'everyone' pseudo-group")
	}
	return nil
}

// --- URI/URL Validators ---

// ValidateURI validates that a URI is not empty
func ValidateURI(field, uri string) error {
	if strings.TrimSpace(uri) == "" {
		return NewValidationError(field, "URI cannot be empty")
	}
	return nil
}

// ValidateWebSocketURL validates that a WebSocket URL is not empty
func ValidateWebSocketURL(url string) error {
	if strings.TrimSpace(url) == "" {
		return NewValidationError("websocket_url", "cannot be empty")
	}
	return nil
}

// --- Timestamp Validators ---

// ValidateExpiresAfterCreated validates that expires_at is after created_at (if both set)
// Note: This is typically validated in the store layer with actual time values
