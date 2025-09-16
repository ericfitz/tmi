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
			Message: "Failed to serialize entity: " + err.Error(),
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

	// Fix the JSON to ensure consistent metadata and image field handling
	modifiedBytes = fixMetadataField(modifiedBytes, originalBytes)
	modifiedBytes = fixImageField(modifiedBytes, originalBytes)

	// Deserialize back into entity
	var modified T
	if err := json.Unmarshal(modifiedBytes, &modified); err != nil {
		return zero, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to deserialize patched entity: " + err.Error(),
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

// ConvertJSONPatchToCellOperations converts standard JSON Patch operations to CellPatchOperation format
// This enables code reuse between REST PATCH endpoints and WebSocket operations
func ConvertJSONPatchToCellOperations(operations []PatchOperation) (*CellPatchOperation, error) {
	// For now, this is a placeholder since the existing JSON Patch system
	// operates on the entire diagram structure, while our WebSocket system
	// operates on individual cells.
	//
	// In a full implementation, this would:
	// 1. Parse JSON Patch operations that target /cells/* paths
	// 2. Convert them to CellOperation format (add/update/remove)
	// 3. Group them into a CellPatchOperation
	//
	// For Phase 1, we're establishing the architecture for code reuse.
	// The shared CellOperationProcessor can be used by both systems.

	return &CellPatchOperation{
		Type:  "patch",
		Cells: []CellOperation{},
	}, nil
}

// ProcessDiagramCellOperations provides a shared interface for diagram cell operations
// This can be used by both REST PATCH handlers and WebSocket operations
func ProcessDiagramCellOperations(diagramID string, operations CellPatchOperation) (*OperationValidationResult, error) {
	processor := NewCellOperationProcessor(DiagramStore)
	return processor.ProcessCellOperations(diagramID, operations)
}

// fixMetadataField ensures that metadata fields are consistent between original and modified JSON.
// This fixes the issue where "metadata": [] becomes "metadata": null after JSON marshal/unmarshal.
func fixMetadataField(modifiedBytes, originalBytes []byte) []byte {
	// Parse original JSON to check metadata field
	var originalData map[string]interface{}
	if err := json.Unmarshal(originalBytes, &originalData); err != nil {
		return modifiedBytes // If we can't parse, return as-is
	}

	// Parse modified JSON
	var modifiedData map[string]interface{}
	if err := json.Unmarshal(modifiedBytes, &modifiedData); err != nil {
		return modifiedBytes // If we can't parse, return as-is
	}

	// Check if original had metadata as empty array and modified has null
	if originalMetadata, hasOriginal := originalData["metadata"]; hasOriginal {
		if modifiedMetadata, hasModified := modifiedData["metadata"]; hasModified {
			// If original was empty array and modified is null, restore empty array
			if originalArray, isArray := originalMetadata.([]interface{}); isArray && len(originalArray) == 0 {
				if modifiedMetadata == nil {
					modifiedData["metadata"] = []interface{}{}
				}
			}
		}
	}

	// Re-marshal the fixed data
	if fixedBytes, err := json.Marshal(modifiedData); err == nil {
		return fixedBytes
	}

	return modifiedBytes // If marshaling fails, return original
}

// fixImageField ensures that image fields are consistent between original and modified JSON.
// This fixes the issue where "image": {} becomes "image": null after JSON marshal/unmarshal
// when the original had a non-null image field.
func fixImageField(modifiedBytes, originalBytes []byte) []byte {
	// Parse original JSON to check image field
	var originalData map[string]interface{}
	if err := json.Unmarshal(originalBytes, &originalData); err != nil {
		return modifiedBytes // If we can't parse, return as-is
	}

	// Parse modified JSON
	var modifiedData map[string]interface{}
	if err := json.Unmarshal(modifiedBytes, &modifiedData); err != nil {
		return modifiedBytes // If we can't parse, return as-is
	}

	// Check if original had a non-null image field and modified has null
	if originalImage, hasOriginal := originalData["image"]; hasOriginal {
		if modifiedImage, hasModified := modifiedData["image"]; hasModified {
			// If original was a map and modified is null, restore empty map
			if originalMap, isMap := originalImage.(map[string]interface{}); isMap {
				if modifiedImage == nil {
					modifiedData["image"] = originalMap
				}
			}
		} else if originalImage != nil {
			// If original had image but modified doesn't, restore it
			modifiedData["image"] = originalImage
		}
	}

	// Re-marshal the fixed data
	if fixedBytes, err := json.Marshal(modifiedData); err == nil {
		return fixedBytes
	}

	return modifiedBytes // If marshaling fails, return original
}
