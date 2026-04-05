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

// ValidateURIPatchOperations validates URI values in JSON Patch operations.
// Only "replace" and "add" operations for the specified paths are validated.
// Returns nil if validator is nil.
func ValidateURIPatchOperations(validator *URIValidator, operations []PatchOperation, uriPaths []string) error {
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

		// Strip leading "/" from path for use as the field name in the error
		fieldName := strings.TrimPrefix(op.Path, "/")

		if err := validator.Validate(uriStr); err != nil {
			return InvalidInputError(fmt.Sprintf("invalid %s: %s", fieldName, err.Error()))
		}
	}

	return nil
}
