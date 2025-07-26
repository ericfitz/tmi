package api

import (
	"encoding/json"
	"net/http"

	jsonpatch "github.com/evanphx/json-patch"
)

// ApplyPatchOperations applies JSON Patch operations to an entity and returns the modified entity
func ApplyPatchOperations[T any](original T, operations []PatchOperation) (T, error) {
	var zero T

	// Convert operations to RFC6902 JSON Patch format
	patchBytes, err := convertOperationsToJSONPatch(operations)
	if err != nil {
		return zero, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_format",
			Message: "Failed to convert patch operations: " + err.Error(),
		}
	}

	// Convert entity to JSON
	originalBytes, err := json.Marshal(original)
	if err != nil {
		return zero, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to serialize entity",
		}
	}

	// Create patch object
	patch, err := jsonpatch.DecodePatch(patchBytes)
	if err != nil {
		return zero, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_patch",
			Message: "Invalid JSON Patch: " + err.Error(),
		}
	}

	// Apply patch
	modifiedBytes, err := patch.Apply(originalBytes)
	if err != nil {
		return zero, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "patch_failed",
			Message: "Failed to apply patch: " + err.Error(),
		}
	}

	// Deserialize back into entity
	var modified T
	if err := json.Unmarshal(modifiedBytes, &modified); err != nil {
		return zero, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to deserialize patched entity",
		}
	}

	return modified, nil
}

// ValidatePatchAuthorization validates that the user has permission to perform the patch operations
func ValidatePatchAuthorization(operations []PatchOperation, userRole Role) error {
	// Check if any operations modify owner or authorization fields
	ownerChanging, authChanging := CheckOwnershipChanges(operations)

	// Only owners can modify ownership or authorization
	if (ownerChanging || authChanging) && userRole != RoleOwner {
		return &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "Only the owner can change ownership or authorization",
		}
	}

	return nil
}

// CheckOwnershipChanges analyzes patch operations to determine if owner or authorization fields are being modified
func CheckOwnershipChanges(operations []PatchOperation) (ownerChanging, authChanging bool) {
	for _, op := range operations {
		if op.Op == "replace" || op.Op == "add" || op.Op == "remove" {
			switch op.Path {
			case "/owner":
				ownerChanging = true
			case "/authorization":
				authChanging = true
			default:
				// Check for authorization array operations like /authorization/0, /authorization/-
				if len(op.Path) > 14 && op.Path[:14] == "/authorization" {
					authChanging = true
				}
			}
		}
	}
	return ownerChanging, authChanging
}

// PreserveCriticalFields preserves critical fields that shouldn't change during patching
func PreserveCriticalFields[T any](modified, original T, preserveFields func(T, T) T) T {
	return preserveFields(modified, original)
}

// ValidatePatchedEntity validates that the patched entity meets business rules
func ValidatePatchedEntity[T any](original, patched T, userName string, validator func(T, T, string) error) error {
	if validator == nil {
		return nil
	}

	if err := validator(original, patched, userName); err != nil {
		return &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "validation_failed",
			Message: err.Error(),
		}
	}

	return nil
}
