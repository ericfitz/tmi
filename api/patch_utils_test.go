package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PatchTestEntity represents a test entity for patch operations
type PatchTestEntity struct {
	ID            string          `json:"id,omitempty"`
	Name          string          `json:"name"`
	Description   *string         `json:"description,omitempty"`
	Owner         string          `json:"owner"`
	Authorization []Authorization `json:"authorization"`
	CreatedAt     time.Time       `json:"created_at"`
	ModifiedAt    time.Time       `json:"modified_at"`
}

func TestApplyPatchOperations(t *testing.T) {
	now := time.Now().UTC()
	original := PatchTestEntity{
		ID:          "test-id",
		Name:        "original name",
		Description: stringPtr("original description"),
		Owner:       "original-owner",
		Authorization: []Authorization{
			{
				PrincipalType: AuthorizationPrincipalTypeUser,
				Provider:      "test",
				ProviderId:    "user1",
				Role:          RoleReader,
			},
		},
		CreatedAt:  now,
		ModifiedAt: now,
	}

	tests := []struct {
		name        string
		operations  []PatchOperation
		expectError bool
		errorCode   string
		validator   func(t *testing.T, result PatchTestEntity)
	}{
		{
			name: "replace name",
			operations: []PatchOperation{
				{Op: "replace", Path: "/name", Value: "new name"},
			},
			expectError: false,
			validator: func(t *testing.T, result PatchTestEntity) {
				assert.Equal(t, "new name", result.Name)
				assert.Equal(t, original.Owner, result.Owner) // Unchanged
			},
		},
		{
			name: "add description",
			operations: []PatchOperation{
				{Op: "add", Path: "/description", Value: "new description"},
			},
			expectError: false,
			validator: func(t *testing.T, result PatchTestEntity) {
				require.NotNil(t, result.Description)
				assert.Equal(t, "new description", *result.Description)
			},
		},
		{
			name: "remove description",
			operations: []PatchOperation{
				{Op: "remove", Path: "/description"},
			},
			expectError: false,
			validator: func(t *testing.T, result PatchTestEntity) {
				assert.Nil(t, result.Description)
			},
		},
		{
			name: "replace owner",
			operations: []PatchOperation{
				{Op: "replace", Path: "/owner", Value: "new-owner"},
			},
			expectError: false,
			validator: func(t *testing.T, result PatchTestEntity) {
				assert.Equal(t, "new-owner", result.Owner)
			},
		},
		{
			name: "replace authorization",
			operations: []PatchOperation{
				{Op: "replace", Path: "/authorization", Value: []interface{}{
					map[string]interface{}{"subject": "user2", "role": "writer"},
					map[string]interface{}{"subject": "user3", "role": "owner"},
				}},
			},
			expectError: false,
			validator: func(t *testing.T, result PatchTestEntity) {
				assert.Len(t, result.Authorization, 2)
				assert.Equal(t, "user2", result.Authorization[0].ProviderId)
				assert.Equal(t, RoleWriter, result.Authorization[0].Role)
				assert.Equal(t, "user3", result.Authorization[1].ProviderId)
				assert.Equal(t, RoleOwner, result.Authorization[1].Role)
			},
		},
		{
			name: "multiple operations",
			operations: []PatchOperation{
				{Op: "replace", Path: "/name", Value: "multi name"},
				{Op: "replace", Path: "/owner", Value: "multi-owner"},
				{Op: "add", Path: "/description", Value: "multi description"},
			},
			expectError: false,
			validator: func(t *testing.T, result PatchTestEntity) {
				assert.Equal(t, "multi name", result.Name)
				assert.Equal(t, "multi-owner", result.Owner)
				require.NotNil(t, result.Description)
				assert.Equal(t, "multi description", *result.Description)
			},
		},
		{
			name: "invalid operation type",
			operations: []PatchOperation{
				{Op: "invalid_op", Path: "/name", Value: "value"},
			},
			expectError: true,
			errorCode:   "patch_failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ApplyPatchOperations(original, tt.operations)

			if tt.expectError {
				require.Error(t, err)
				reqErr, ok := err.(*RequestError)
				require.True(t, ok, "Expected RequestError")
				assert.Equal(t, tt.errorCode, reqErr.Code)
			} else {
				require.NoError(t, err)
				if tt.validator != nil {
					tt.validator(t, result)
				}
			}
		})
	}
}

func TestValidatePatchAuthorization(t *testing.T) {
	tests := []struct {
		name        string
		operations  []PatchOperation
		userRole    Role
		expectError bool
	}{
		{
			name: "owner can change owner",
			operations: []PatchOperation{
				{Op: "replace", Path: "/owner", Value: "new-owner"},
			},
			userRole:    RoleOwner,
			expectError: false,
		},
		{
			name: "owner can change authorization",
			operations: []PatchOperation{
				{Op: "replace", Path: "/authorization", Value: []interface{}{}},
			},
			userRole:    RoleOwner,
			expectError: false,
		},
		{
			name: "owner can change both",
			operations: []PatchOperation{
				{Op: "replace", Path: "/owner", Value: "new-owner"},
				{Op: "replace", Path: "/authorization", Value: []interface{}{}},
			},
			userRole:    RoleOwner,
			expectError: false,
		},
		{
			name: "writer cannot change owner",
			operations: []PatchOperation{
				{Op: "replace", Path: "/owner", Value: "new-owner"},
			},
			userRole:    RoleWriter,
			expectError: true,
		},
		{
			name: "writer cannot change authorization",
			operations: []PatchOperation{
				{Op: "replace", Path: "/authorization", Value: []interface{}{}},
			},
			userRole:    RoleWriter,
			expectError: true,
		},
		{
			name: "reader cannot change owner",
			operations: []PatchOperation{
				{Op: "replace", Path: "/owner", Value: "new-owner"},
			},
			userRole:    RoleReader,
			expectError: true,
		},
		{
			name: "reader cannot change authorization",
			operations: []PatchOperation{
				{Op: "replace", Path: "/authorization", Value: []interface{}{}},
			},
			userRole:    RoleReader,
			expectError: true,
		},
		{
			name: "any role can change other fields",
			operations: []PatchOperation{
				{Op: "replace", Path: "/name", Value: "new name"},
				{Op: "replace", Path: "/description", Value: "new description"},
			},
			userRole:    RoleReader,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePatchAuthorization(tt.operations, tt.userRole)

			if tt.expectError {
				require.Error(t, err)
				reqErr, ok := err.(*RequestError)
				require.True(t, ok, "Expected RequestError")
				assert.Equal(t, http.StatusForbidden, reqErr.Status)
				assert.Equal(t, "forbidden", reqErr.Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckOwnershipChanges(t *testing.T) {
	tests := []struct {
		name          string
		operations    []PatchOperation
		expectedOwner bool
		expectedAuth  bool
	}{
		{
			name: "no ownership changes",
			operations: []PatchOperation{
				{Op: "replace", Path: "/name", Value: "new name"},
				{Op: "replace", Path: "/description", Value: "new description"},
			},
			expectedOwner: false,
			expectedAuth:  false,
		},
		{
			name: "owner change only",
			operations: []PatchOperation{
				{Op: "replace", Path: "/owner", Value: "new-owner"},
				{Op: "replace", Path: "/name", Value: "new name"},
			},
			expectedOwner: true,
			expectedAuth:  false,
		},
		{
			name: "authorization change only",
			operations: []PatchOperation{
				{Op: "replace", Path: "/authorization", Value: []interface{}{}},
			},
			expectedOwner: false,
			expectedAuth:  true,
		},
		{
			name: "both owner and authorization changes",
			operations: []PatchOperation{
				{Op: "replace", Path: "/owner", Value: "new-owner"},
				{Op: "replace", Path: "/authorization", Value: []interface{}{}},
			},
			expectedOwner: true,
			expectedAuth:  true,
		},
		{
			name: "add operations",
			operations: []PatchOperation{
				{Op: "add", Path: "/owner", Value: "new-owner"},
				{Op: "add", Path: "/authorization", Value: []interface{}{}},
			},
			expectedOwner: true,
			expectedAuth:  true,
		},
		{
			name: "remove operations",
			operations: []PatchOperation{
				{Op: "remove", Path: "/owner"},
				{Op: "remove", Path: "/authorization"},
			},
			expectedOwner: true,
			expectedAuth:  true,
		},
		{
			name: "authorization array element operations",
			operations: []PatchOperation{
				{Op: "replace", Path: "/authorization/0", Value: map[string]interface{}{"subject": "user1", "role": "reader"}},
				{Op: "add", Path: "/authorization/-", Value: map[string]interface{}{"subject": "user2", "role": "writer"}},
			},
			expectedOwner: false,
			expectedAuth:  true,
		},
		{
			name: "mixed authorization operations",
			operations: []PatchOperation{
				{Op: "replace", Path: "/authorization", Value: []interface{}{}},
				{Op: "add", Path: "/authorization/0/role", Value: "owner"},
			},
			expectedOwner: false,
			expectedAuth:  true,
		},
		{
			name: "test operation (should not trigger changes)",
			operations: []PatchOperation{
				{Op: "test", Path: "/owner", Value: "current-owner"},
				{Op: "test", Path: "/authorization", Value: []interface{}{}},
			},
			expectedOwner: false,
			expectedAuth:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ownerChanging, authChanging := CheckOwnershipChanges(tt.operations)
			assert.Equal(t, tt.expectedOwner, ownerChanging)
			assert.Equal(t, tt.expectedAuth, authChanging)
		})
	}
}

func TestPreserveCriticalFields(t *testing.T) {
	now := time.Now().UTC()
	earlier := now.Add(-1 * time.Hour)

	original := PatchTestEntity{
		ID:         "original-id",
		Name:       "original name",
		Owner:      "original-owner",
		CreatedAt:  earlier,
		ModifiedAt: earlier,
	}

	modified := PatchTestEntity{
		ID:         "modified-id", // Should be preserved
		Name:       "modified name",
		Owner:      "modified-owner",
		CreatedAt:  now, // Should be preserved
		ModifiedAt: now,
	}

	preserveFunc := func(modified, original PatchTestEntity) PatchTestEntity {
		modified.ID = original.ID
		modified.CreatedAt = original.CreatedAt
		return modified
	}

	result := PreserveCriticalFields(modified, original, preserveFunc)

	assert.Equal(t, original.ID, result.ID)                 // Preserved
	assert.Equal(t, original.CreatedAt, result.CreatedAt)   // Preserved
	assert.Equal(t, modified.Name, result.Name)             // Not preserved
	assert.Equal(t, modified.Owner, result.Owner)           // Not preserved
	assert.Equal(t, modified.ModifiedAt, result.ModifiedAt) // Not preserved
}

func TestValidatePatchedEntity(t *testing.T) {
	original := PatchTestEntity{
		Name:  "original",
		Owner: "original-owner",
	}

	patched := PatchTestEntity{
		Name:  "patched",
		Owner: "patched-owner",
	}

	tests := []struct {
		name        string
		validator   func(PatchTestEntity, PatchTestEntity, string) error
		userName    string
		expectError bool
		errorCode   string
	}{
		{
			name:        "nil validator",
			validator:   nil,
			userName:    "user",
			expectError: false,
		},
		{
			name: "passing validator",
			validator: func(original, patched PatchTestEntity, userName string) error {
				return nil
			},
			userName:    "user",
			expectError: false,
		},
		{
			name: "failing validator",
			validator: func(original, patched PatchTestEntity, userName string) error {
				return assert.AnError
			},
			userName:    "user",
			expectError: true,
			errorCode:   "validation_failed",
		},
		{
			name: "validator with business logic",
			validator: func(original, patched PatchTestEntity, userName string) error {
				if patched.Name == "" {
					return assert.AnError
				}
				return nil
			},
			userName:    "user",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePatchedEntity(original, patched, tt.userName, tt.validator)

			if tt.expectError {
				require.Error(t, err)
				reqErr, ok := err.(*RequestError)
				require.True(t, ok, "Expected RequestError")
				assert.Equal(t, http.StatusBadRequest, reqErr.Status)
				assert.Equal(t, tt.errorCode, reqErr.Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Test integration scenario
func TestPatchWorkflow(t *testing.T) {
	now := time.Now().UTC()
	original := PatchTestEntity{
		ID:    "test-id",
		Name:  "original name",
		Owner: "original-owner",
		Authorization: []Authorization{
			{
				PrincipalType: AuthorizationPrincipalTypeUser,
				Provider:      "test",
				ProviderId:    "user1",
				Role:          RoleReader,
			},
		},
		CreatedAt:  now,
		ModifiedAt: now,
	}

	operations := []PatchOperation{
		{Op: "replace", Path: "/name", Value: "patched name"},
		{Op: "replace", Path: "/owner", Value: "new-owner"},
	}

	// Step 1: Validate authorization (as owner)
	err := ValidatePatchAuthorization(operations, RoleOwner)
	require.NoError(t, err)

	// Step 2: Apply patch operations
	modified, err := ApplyPatchOperations(original, operations)
	require.NoError(t, err)

	// Step 3: Preserve critical fields
	preserveFunc := func(modified, original PatchTestEntity) PatchTestEntity {
		modified.ID = original.ID
		modified.CreatedAt = original.CreatedAt
		return modified
	}
	modified = PreserveCriticalFields(modified, original, preserveFunc)

	// Step 4: Validate patched entity
	validator := func(original, patched PatchTestEntity, userName string) error {
		if patched.Name == "" {
			return assert.AnError
		}
		return nil
	}
	err = ValidatePatchedEntity(original, modified, "test-user", validator)
	require.NoError(t, err)

	// Verify final result
	assert.Equal(t, original.ID, modified.ID)               // Preserved
	assert.Equal(t, original.CreatedAt, modified.CreatedAt) // Preserved
	assert.Equal(t, "patched name", modified.Name)          // Modified
	assert.Equal(t, "new-owner", modified.Owner)            // Modified
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func strPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}
