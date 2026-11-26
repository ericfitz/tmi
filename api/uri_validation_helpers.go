package api

import (
	"fmt"
	"strings"
)

// validateURI validates a required URI field. Returns nil if validator is nil or URI is empty.
func validateURI(validator *URIValidator, fieldName, uri string) error {
	if validator == nil || uri == "" {
		return nil
	}
	if err := validator.Validate(uri); err != nil {
		return InvalidInputError(fmt.Sprintf("invalid %s: %s", fieldName, err.Error()))
	}
	return nil
}

// validateOptionalURI validates an optional (*string) URI field. Returns nil if validator is nil, pointer is nil, or string is empty.
func validateOptionalURI(validator *URIValidator, fieldName string, uri *string) error {
	if validator == nil || uri == nil || *uri == "" {
		return nil
	}
	return validateURI(validator, fieldName, *uri)
}

// validateOptionalReferenceURI validates an optional URI field that the server
// stores but never fetches. It checks scheme and (when configured) allowlist
// only — DNS resolution and private-IP blocking are skipped because there is
// no SSRF surface for fields the server doesn't dereference.
func validateOptionalReferenceURI(validator *URIValidator, fieldName string, uri *string) error {
	if validator == nil || uri == nil || *uri == "" {
		return nil
	}
	if err := validator.ValidateReference(*uri); err != nil {
		return InvalidInputError(fmt.Sprintf("invalid %s: %s", fieldName, err.Error()))
	}
	return nil
}

// ValidateURIPatchOperations validates URI values in JSON Patch operations.
// Only "replace" and "add" operations for the specified paths are validated.
// Returns nil if validator is nil.
func ValidateURIPatchOperations(validator *URIValidator, operations []PatchOperation, uriPaths []string) error {
	return validateURIPatchOperationsWith(validator, operations, uriPaths, (*URIValidator).Validate)
}

// ValidateReferenceURIPatchOperations is the reference-mode counterpart of
// ValidateURIPatchOperations: it validates URI values in JSON Patch operations
// using ValidateReference (no DNS/IP checks). Use for stored-only fields like
// issue_uri.
func ValidateReferenceURIPatchOperations(validator *URIValidator, operations []PatchOperation, uriPaths []string) error {
	return validateURIPatchOperationsWith(validator, operations, uriPaths, (*URIValidator).ValidateReference)
}

func validateURIPatchOperationsWith(validator *URIValidator, operations []PatchOperation, uriPaths []string, check func(*URIValidator, string) error) error {
	if validator == nil {
		return nil
	}

	uriPathSet := make(map[string]bool, len(uriPaths))
	for _, p := range uriPaths {
		uriPathSet[p] = true
	}

	for _, op := range operations {
		if op.Op != string(Replace) && op.Op != string(Add) {
			continue
		}
		if !uriPathSet[op.Path] {
			continue
		}

		uriStr, ok := op.Value.(string)
		if !ok || uriStr == "" {
			continue
		}

		fieldName := strings.TrimPrefix(op.Path, "/")

		if err := check(validator, uriStr); err != nil {
			return InvalidInputError(fmt.Sprintf("invalid %s: %s", fieldName, err.Error()))
		}
	}

	return nil
}

// NormalizeOptionalURI normalizes an optional URI field: nil, empty and
// whitespace-only values become nil (field absent), and any other value is
// returned with surrounding whitespace trimmed. Apply it after sanitizing and
// before validating, so an empty string is never stored or echoed (it is not a
// valid format: uri value) and a padded value does not leak its padding.
func NormalizeOptionalURI(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// NormalizeURIPatchOperations applies the NormalizeOptionalURI rule to JSON
// Patch operations: for "replace" and "add" on the given paths, a string value
// that is blank becomes null (clearing the field) and any other string value is
// trimmed. Non-string values are left for the applier to reject.
func NormalizeURIPatchOperations(operations []PatchOperation, uriPaths []string) {
	pathSet := make(map[string]bool, len(uriPaths))
	for _, p := range uriPaths {
		pathSet[p] = true
	}
	for i := range operations {
		op := &operations[i]
		if (op.Op != string(Replace) && op.Op != string(Add)) || !pathSet[op.Path] {
			continue
		}
		str, ok := op.Value.(string)
		if !ok {
			continue
		}
		if normalized := NormalizeOptionalURI(&str); normalized == nil {
			op.Value = nil
		} else {
			op.Value = *normalized
		}
	}
}
