package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/gin-gonic/gin"
)

// serverManagedFields are fields excluded from audit comparison and snapshots.
// Changes to only these fields do not generate audit entries.
var serverManagedFields = map[string]bool{
	"id":                  true,
	"created_at":          true,
	"modified_at":         true,
	"update_vector":       true,
	"image":               true,
	"image_update_vector": true,
}

// ExtractAuditActor extracts denormalized user information from the Gin context
// for recording in audit entries. Uses the same context keys set by JWT middleware.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: extract denormalized user identity from Gin context for audit entries (pure)
func ExtractAuditActor(c *gin.Context) InternalAuditActor {
	email := getContextString(c, "userEmail")
	providerID := getContextString(c, "userID")
	displayName := getContextString(c, "userDisplayName")
	provider := getContextString(c, "userIdP")

	return InternalAuditActor{
		Email:       email,
		Provider:    provider,
		ProviderID:  providerID,
		DisplayName: displayName,
	}
}

// getContextString safely extracts a string value from gin context
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: safely fetch a string value from the Gin context by key (pure)
func getContextString(c *gin.Context, key string) string {
	val, exists := c.Get(key)
	if !exists {
		return ""
	}
	s, ok := val.(string)
	if !ok {
		return ""
	}
	return s
}

// SerializeForAudit marshals an entity to JSON, stripping server-managed and
// bulky derived fields (like SVG images) from the snapshot.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: serialize an entity to JSON, stripping server-managed fields for audit snapshots (pure)
func SerializeForAudit(entity any) ([]byte, error) {
	data, err := json.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize entity for audit: %w", err)
	}

	// Parse and remove server-managed fields; if not an object, return as-is
	cleaned := stripServerManagedFields(data)
	return cleaned, nil
}

// stripServerManagedFields removes server-managed fields from JSON data.
// If data is not a JSON object, it is returned unchanged.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: remove server-managed fields from a JSON object, returning unchanged if not an object (pure)
func stripServerManagedFields(data []byte) []byte {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return data
	}

	for field := range serverManagedFields {
		delete(m, field)
	}

	cleaned, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return cleaned
}

// ShouldAudit returns true if the change between original and modified JSON
// includes changes to non-server-managed fields. Returns false if the only
// differences are in server-managed fields (id, timestamps, etc.).
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: report whether two JSON states differ on non-server-managed fields (pure)
func ShouldAudit(originalJSON, modifiedJSON []byte) bool {
	var original, modified map[string]any
	if err := json.Unmarshal(originalJSON, &original); err != nil {
		return true // can't parse, audit to be safe
	}
	if err := json.Unmarshal(modifiedJSON, &modified); err != nil {
		return true
	}

	for key := range original {
		if serverManagedFields[key] {
			continue
		}
		modVal, exists := modified[key]
		if !exists {
			return true // field was removed
		}
		origBytes, _ := json.Marshal(original[key])
		modBytes, _ := json.Marshal(modVal)
		if string(origBytes) != string(modBytes) {
			return true
		}
	}

	// Check for new fields in modified
	for key := range modified {
		if serverManagedFields[key] {
			continue
		}
		if _, exists := original[key]; !exists {
			return true
		}
	}

	return false
}

// GenerateChangeSummary produces a human-readable summary of changes between
// two JSON states. Format: "field1: 'old' -> 'new', field2: added, field3: removed"
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: compute a human-readable field-level change summary between two JSON states (pure)
func GenerateChangeSummary(originalJSON, modifiedJSON []byte) string {
	var original, modified map[string]any
	if err := json.Unmarshal(originalJSON, &original); err != nil {
		return "unable to parse original state"
	}
	if err := json.Unmarshal(modifiedJSON, &modified); err != nil {
		return "unable to parse modified state"
	}

	var changes []string

	// Collect all keys
	allKeys := make(map[string]bool)
	for k := range original {
		allKeys[k] = true
	}
	for k := range modified {
		allKeys[k] = true
	}

	// Sort keys for deterministic output
	sortedKeys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, key := range sortedKeys {
		if serverManagedFields[key] {
			continue
		}

		origVal, origExists := original[key]
		modVal, modExists := modified[key]

		if !origExists && modExists {
			changes = append(changes, fmt.Sprintf("%s: added", key))
			continue
		}
		if origExists && !modExists {
			changes = append(changes, fmt.Sprintf("%s: removed", key))
			continue
		}

		origBytes, _ := json.Marshal(origVal)
		modBytes, _ := json.Marshal(modVal)
		if string(origBytes) != string(modBytes) {
			origStr := truncateValue(string(origBytes), 50)
			modStr := truncateValue(string(modBytes), 50)
			changes = append(changes, fmt.Sprintf("%s: %s -> %s", key, origStr, modStr))
		}
	}

	if len(changes) == 0 {
		return "no significant changes"
	}
	return strings.Join(changes, ", ")
}

// truncateValue truncates a string to maxLen, adding "..." if truncated
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: truncate a string to a max length with ellipsis if needed (pure)
func truncateValue(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// ComputeReverseDiff computes a JSON Merge Patch (RFC 7396) that transforms
// the 'after' state back to the 'before' state. This is the reverse diff
// stored in version snapshots for space efficiency.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: compute a JSON Merge Patch that reverts the after state back to before (pure)
func ComputeReverseDiff(before, after []byte) ([]byte, error) {
	// CreateMergePatch(original, modified) returns a patch that transforms original -> modified.
	// We want the reverse: a patch that transforms after -> before.
	// So we pass (after, before) to get the reverse diff.
	patch, err := jsonpatch.CreateMergePatch(after, before)
	if err != nil {
		return nil, fmt.Errorf("failed to compute reverse diff: %w", err)
	}
	return patch, nil
}

// ApplyDiff applies a JSON Merge Patch (RFC 7396) to a state,
// producing a new state.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: apply a JSON Merge Patch to a state and return the resulting state (pure)
func ApplyDiff(state, diff []byte) ([]byte, error) {
	result, err := jsonpatch.MergePatch(state, diff)
	if err != nil {
		return nil, fmt.Errorf("failed to apply diff: %w", err)
	}
	return result, nil
}

// RecordAuditCreate records a "created" audit entry for a newly created entity.
// Call after the entity has been successfully created in the store.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: record a created audit entry for a newly created entity (mutates shared state)
func RecordAuditCreate(c *gin.Context, threatModelID, objectType, objectID string, entity any) {
	if GlobalAuditDebouncer == nil {
		return
	}

	postState, err := SerializeForAudit(entity)
	if err != nil {
		slogging.Get().WithContext(c).Error("failed to serialize entity for audit: %v", err)
		return
	}

	summary := "created"
	GlobalAuditDebouncer.RecordOrBuffer(c.Request.Context(), AuditParams{
		ThreatModelID: threatModelID,
		ObjectType:    objectType,
		ObjectID:      objectID,
		ChangeType:    "created",
		Actor:         ExtractAuditActor(c),
		PreviousState: nil,
		CurrentState:  postState,
		ChangeSummary: &summary,
	}, false)
}

// RecordAuditUpdate records an "updated" or "patched" audit entry for a modified entity.
// preState should be obtained via SerializeForAudit before the mutation.
// Call after the entity has been successfully updated in the store.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: record an updated or patched audit entry when non-server fields change (mutates shared state)
func RecordAuditUpdate(c *gin.Context, changeType, threatModelID, objectType, objectID string, preState []byte, entity any) {
	if GlobalAuditDebouncer == nil {
		return
	}

	postState, err := SerializeForAudit(entity)
	if err != nil {
		slogging.Get().WithContext(c).Error("failed to serialize entity for audit: %v", err)
		return
	}

	if !ShouldAudit(preState, postState) {
		return
	}

	summary := GenerateChangeSummary(preState, postState)
	GlobalAuditDebouncer.RecordOrBuffer(c.Request.Context(), AuditParams{
		ThreatModelID: threatModelID,
		ObjectType:    objectType,
		ObjectID:      objectID,
		ChangeType:    changeType,
		Actor:         ExtractAuditActor(c),
		PreviousState: preState,
		CurrentState:  postState,
		ChangeSummary: &summary,
	}, false)
}

// RecordAuditDelete records a "deleted" audit entry for a deleted entity.
// preState should be obtained via SerializeForAudit before the deletion.
// Call after the entity has been successfully deleted from the store.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: record a deleted audit entry for a removed entity (mutates shared state)
func RecordAuditDelete(c *gin.Context, threatModelID, objectType, objectID string, preState []byte) {
	if GlobalAuditDebouncer == nil {
		return
	}

	summary := "deleted"
	GlobalAuditDebouncer.RecordOrBuffer(c.Request.Context(), AuditParams{
		ThreatModelID: threatModelID,
		ObjectType:    objectType,
		ObjectID:      objectID,
		ChangeType:    "deleted",
		Actor:         ExtractAuditActor(c),
		PreviousState: preState,
		CurrentState:  nil,
		ChangeSummary: &summary,
	}, false)
}
