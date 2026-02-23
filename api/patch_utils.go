package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
)

// ApplyPatchOperations applies JSON Patch operations to an entity and returns the modified entity
func ApplyPatchOperations[T any](original T, operations []PatchOperation) (T, error) {
	var zero T

	// Preprocess operations to handle special cases like base64 SVG decoding
	processedOperations, err := preprocessPatchOperations(operations)
	if err != nil {
		return zero, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_format",
			Message: "Failed to preprocess patch operations: " + err.Error(),
		}
	}

	// Convert entity to JSON (needed for replace-to-add promotion and validation)
	originalBytes, err := json.Marshal(original)
	if err != nil {
		return zero, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to serialize entity: " + err.Error(),
		}
	}

	// Promote "replace" to "add" for paths that don't exist in the serialized JSON.
	// This handles optional fields omitted by omitempty tags, making the API more
	// forgiving for clients that use "replace" on valid but currently-unset fields.
	processedOperations = promoteReplaceToAdd(originalBytes, processedOperations)

	// Convert operations to RFC6902 JSON Patch format
	patchBytes, err := convertOperationsToJSONPatch(processedOperations)
	if err != nil {
		return zero, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_format",
			Message: "Failed to convert patch operations: " + err.Error(),
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

	// Validate that replace operations target existing paths (workaround for
	// evanphx/json-patch v1 bug where partialDoc.get() returns nil error for
	// missing keys, causing replace to silently add fields instead of failing
	// per RFC 6902 Section 4.3). After promotion, only replace ops on paths
	// that exist in the JSON remain, so this validates nested path correctness.
	if err := validateReplacePaths(originalBytes, processedOperations); err != nil {
		return zero, err
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
	modifiedBytes = fixOwnerField(modifiedBytes, originalBytes)

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
		if op.Op == string(Replace) || op.Op == string(Add) || op.Op == string(Remove) {
			switch op.Path {
			case "/owner":
				ownerChanging = true
			case "/authorization":
				authChanging = true
			default:
				// Check for authorization array operations like /authorization/0, /authorization/-
				if len(op.Path) > 15 && op.Path[:15] == "/authorization/" {
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

// promoteReplaceToAdd checks each "replace" operation against the serialized JSON document.
// If the target path does not exist (typically because an optional field is nil and was omitted
// by omitempty), the operation is promoted to "add" so that the patch succeeds. This makes the
// API more forgiving for clients that use "replace" on fields that are valid in the schema but
// currently unset.
func promoteReplaceToAdd(originalBytes []byte, operations []PatchOperation) []PatchOperation {
	var doc any
	if err := json.Unmarshal(originalBytes, &doc); err != nil {
		return operations // let downstream handle malformed JSON
	}

	promoted := make([]PatchOperation, len(operations))
	for i, op := range operations {
		promoted[i] = op
		if op.Op != "replace" || op.Path == "" {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(op.Path, "/"), "/")
		if !pathExistsInDoc(doc, parts) {
			promoted[i].Op = string(Add)
		}
	}
	return promoted
}

// validateReplacePaths checks that all "replace" operations target paths that exist in the
// original document. This is a workaround for evanphx/json-patch v1 which silently adds
// fields instead of returning an error when replacing a nonexistent path (violating RFC 6902).
func validateReplacePaths(originalBytes []byte, operations []PatchOperation) error {
	var doc any
	if err := json.Unmarshal(originalBytes, &doc); err != nil {
		return nil //nolint:nilerr // let the library handle malformed JSON
	}

	for _, op := range operations {
		if op.Op != "replace" || op.Path == "" {
			continue
		}

		parts := strings.Split(strings.TrimPrefix(op.Path, "/"), "/")
		if !pathExistsInDoc(doc, parts) {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "patch_failed",
				Message: fmt.Sprintf("Replace operation target path does not exist: %s", op.Path),
			}
		}
	}
	return nil
}

// pathExistsInDoc walks a JSON document to verify that the given path segments exist.
func pathExistsInDoc(doc any, parts []string) bool {
	if len(parts) == 0 {
		return true
	}

	key := parts[0]
	remaining := parts[1:]

	switch d := doc.(type) {
	case map[string]any:
		val, ok := d[key]
		if !ok {
			return false
		}
		return pathExistsInDoc(val, remaining)
	case []any:
		// Array index — parse as int
		idx, err := strconv.Atoi(key)
		if err != nil {
			return false
		}
		if idx < 0 || idx >= len(d) {
			return false
		}
		return pathExistsInDoc(d[idx], remaining)
	default:
		// Scalar value — can't descend further
		return false
	}
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
	var originalData map[string]any
	if err := json.Unmarshal(originalBytes, &originalData); err != nil {
		return modifiedBytes // If we can't parse, return as-is
	}

	// Parse modified JSON
	var modifiedData map[string]any
	if err := json.Unmarshal(modifiedBytes, &modifiedData); err != nil {
		return modifiedBytes // If we can't parse, return as-is
	}

	// Check if original had metadata as empty array and modified has null
	if originalMetadata, hasOriginal := originalData["metadata"]; hasOriginal {
		if modifiedMetadata, hasModified := modifiedData["metadata"]; hasModified {
			// If original was empty array and modified is null, restore empty array
			if originalArray, isArray := originalMetadata.([]any); isArray && len(originalArray) == 0 {
				if modifiedMetadata == nil {
					modifiedData["metadata"] = []any{}
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
	var originalData map[string]any
	if err := json.Unmarshal(originalBytes, &originalData); err != nil {
		return modifiedBytes // If we can't parse, return as-is
	}

	// Parse modified JSON
	var modifiedData map[string]any
	if err := json.Unmarshal(modifiedBytes, &modifiedData); err != nil {
		return modifiedBytes // If we can't parse, return as-is
	}

	// Check if original had a non-null image field and modified has null
	if originalImage, hasOriginal := originalData["image"]; hasOriginal {
		if modifiedImage, hasModified := modifiedData["image"]; hasModified {
			// If original was a map and modified is null, restore empty map
			if originalMap, isMap := originalImage.(map[string]any); isMap {
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

// fixOwnerField ensures that owner fields are properly structured as User objects.
// This fixes the issue where PATCH operations set owner to a string value, but the
// ThreatModel struct expects a User object.
func fixOwnerField(modifiedBytes, _ []byte) []byte {
	// Parse modified JSON
	var modifiedData map[string]any
	if err := json.Unmarshal(modifiedBytes, &modifiedData); err != nil {
		return modifiedBytes // If we can't parse, return as-is
	}

	// Check if owner field exists and is a string
	if ownerValue, hasOwner := modifiedData["owner"]; hasOwner {
		if ownerStr, isString := ownerValue.(string); isString {
			// Convert string to User object structure
			// The string is typically an email/provider_id
			// Ensure email is valid format - if no '@' is present, append @example.com
			email := ownerStr
			if !strings.Contains(ownerStr, "@") {
				email = ownerStr + "@example.com"
			}
			modifiedData["owner"] = map[string]any{
				"principal_type": "user",
				"provider":       "tmi", // Default to tmi provider for now
				"provider_id":    ownerStr,
				"display_name":   ownerStr, // Use email as display name initially
				"email":          email,    // Email is required in User struct and must be valid format
			}
		}
	}

	// Re-marshal the fixed data
	if fixedBytes, err := json.Marshal(modifiedData); err == nil {
		return fixedBytes
	}

	return modifiedBytes // If marshaling fails, return original
}
