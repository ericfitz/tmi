package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDuplicateSubjects(t *testing.T) {
	tests := []struct {
		name        string
		authList    []Authorization
		expectError bool
		duplicate   string
	}{
		{
			name: "no duplicates",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "user2", Role: RoleWriter},
				{Subject: "user3", Role: RoleOwner},
			},
			expectError: false,
		},
		{
			name:        "empty list",
			authList:    []Authorization{},
			expectError: false,
		},
		{
			name: "single entry",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
			},
			expectError: false,
		},
		{
			name: "duplicate subjects",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "user2", Role: RoleWriter},
				{Subject: "user1", Role: RoleOwner}, // Duplicate
			},
			expectError: true,
			duplicate:   "user1",
		},
		{
			name: "multiple duplicates - first found",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "user2", Role: RoleWriter},
				{Subject: "user1", Role: RoleOwner},  // First duplicate
				{Subject: "user2", Role: RoleReader}, // Second duplicate
			},
			expectError: true,
			duplicate:   "user1", // Should find first duplicate
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDuplicateSubjects(tt.authList)

			if tt.expectError {
				require.Error(t, err)
				reqErr, ok := err.(*RequestError)
				require.True(t, ok, "Expected RequestError")
				assert.Equal(t, http.StatusBadRequest, reqErr.Status)
				assert.Equal(t, "invalid_input", reqErr.Code)
				assert.Contains(t, reqErr.Message, tt.duplicate)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateOwnerNotInAuthList(t *testing.T) {
	tests := []struct {
		name        string
		owner       string
		authList    []Authorization
		expectError bool
	}{
		{
			name:  "owner not in auth list",
			owner: "owner1",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "user2", Role: RoleWriter},
			},
			expectError: false,
		},
		{
			name:        "empty auth list",
			owner:       "owner1",
			authList:    []Authorization{},
			expectError: false,
		},
		{
			name:  "owner in auth list",
			owner: "owner1",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "owner1", Role: RoleWriter}, // Owner duplicate
			},
			expectError: true,
		},
		{
			name:  "owner in auth list with owner role",
			owner: "owner1",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "owner1", Role: RoleOwner}, // Owner duplicate
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOwnerNotInAuthList(tt.owner, tt.authList)

			if tt.expectError {
				require.Error(t, err)
				reqErr, ok := err.(*RequestError)
				require.True(t, ok, "Expected RequestError")
				assert.Equal(t, http.StatusBadRequest, reqErr.Status)
				assert.Equal(t, "invalid_input", reqErr.Code)
				assert.Contains(t, reqErr.Message, tt.owner)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestApplyOwnershipTransferRule(t *testing.T) {
	tests := []struct {
		name          string
		authList      []Authorization
		originalOwner string
		newOwner      string
		expected      []Authorization
	}{
		{
			name: "no ownership change",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "user2", Role: RoleWriter},
			},
			originalOwner: "owner1",
			newOwner:      "owner1", // Same owner
			expected: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "user2", Role: RoleWriter},
			},
		},
		{
			name: "ownership change - original owner not in list",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "user2", Role: RoleWriter},
			},
			originalOwner: "oldowner",
			newOwner:      "newowner",
			expected: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "user2", Role: RoleWriter},
				{Subject: "oldowner", Role: RoleOwner}, // Added
			},
		},
		{
			name: "ownership change - original owner in list with different role",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "oldowner", Role: RoleWriter}, // Will be updated
			},
			originalOwner: "oldowner",
			newOwner:      "newowner",
			expected: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "oldowner", Role: RoleOwner}, // Role updated
			},
		},
		{
			name: "ownership change - original owner already has owner role",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "oldowner", Role: RoleOwner}, // Already owner
			},
			originalOwner: "oldowner",
			newOwner:      "newowner",
			expected: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "oldowner", Role: RoleOwner}, // Unchanged
			},
		},
		{
			name:          "empty auth list",
			authList:      []Authorization{},
			originalOwner: "oldowner",
			newOwner:      "newowner",
			expected: []Authorization{
				{Subject: "oldowner", Role: RoleOwner}, // Added
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ApplyOwnershipTransferRule(tt.authList, tt.originalOwner, tt.newOwner)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractOwnershipChangesFromOperations(t *testing.T) {
	tests := []struct {
		name            string
		operations      []PatchOperation
		expectedOwner   string
		expectedAuth    []Authorization
		expectedOwnerCh bool
		expectedAuthCh  bool
	}{
		{
			name: "no ownership operations",
			operations: []PatchOperation{
				{Op: "replace", Path: "/name", Value: "new name"},
				{Op: "add", Path: "/description", Value: "new description"},
			},
			expectedOwner:   "",
			expectedAuth:    nil,
			expectedOwnerCh: false,
			expectedAuthCh:  false,
		},
		{
			name: "owner change only",
			operations: []PatchOperation{
				{Op: "replace", Path: "/owner", Value: "newowner"},
				{Op: "replace", Path: "/name", Value: "new name"},
			},
			expectedOwner:   "newowner",
			expectedAuth:    nil,
			expectedOwnerCh: true,
			expectedAuthCh:  false,
		},
		{
			name: "authorization change only",
			operations: []PatchOperation{
				{Op: "replace", Path: "/authorization", Value: []interface{}{
					map[string]interface{}{"subject": "user1", "role": "reader"},
					map[string]interface{}{"subject": "user2", "role": "writer"},
				}},
			},
			expectedOwner: "",
			expectedAuth: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "user2", Role: RoleWriter},
			},
			expectedOwnerCh: false,
			expectedAuthCh:  true,
		},
		{
			name: "both owner and authorization changes",
			operations: []PatchOperation{
				{Op: "replace", Path: "/owner", Value: "newowner"},
				{Op: "replace", Path: "/authorization", Value: []interface{}{
					map[string]interface{}{"subject": "user1", "role": "reader"},
				}},
			},
			expectedOwner: "newowner",
			expectedAuth: []Authorization{
				{Subject: "user1", Role: RoleReader},
			},
			expectedOwnerCh: true,
			expectedAuthCh:  true,
		},
		{
			name: "add operations",
			operations: []PatchOperation{
				{Op: "add", Path: "/owner", Value: "addedowner"},
				{Op: "add", Path: "/authorization", Value: []interface{}{
					map[string]interface{}{"subject": "user1", "role": "owner"},
				}},
			},
			expectedOwner: "addedowner",
			expectedAuth: []Authorization{
				{Subject: "user1", Role: RoleOwner},
			},
			expectedOwnerCh: true,
			expectedAuthCh:  true,
		},
		{
			name: "empty owner value ignored",
			operations: []PatchOperation{
				{Op: "replace", Path: "/owner", Value: ""},
			},
			expectedOwner:   "",
			expectedAuth:    nil,
			expectedOwnerCh: false,
			expectedAuthCh:  false,
		},
		{
			name: "invalid owner type ignored",
			operations: []PatchOperation{
				{Op: "replace", Path: "/owner", Value: 123},
			},
			expectedOwner:   "",
			expectedAuth:    nil,
			expectedOwnerCh: false,
			expectedAuthCh:  false,
		},
		{
			name: "invalid authorization type ignored",
			operations: []PatchOperation{
				{Op: "replace", Path: "/authorization", Value: "invalid"},
			},
			expectedOwner:   "",
			expectedAuth:    nil,
			expectedOwnerCh: false,
			expectedAuthCh:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, auth, hasOwnerCh, hasAuthCh := ExtractOwnershipChangesFromOperations(tt.operations)

			assert.Equal(t, tt.expectedOwner, owner)
			assert.Equal(t, tt.expectedAuth, auth)
			assert.Equal(t, tt.expectedOwnerCh, hasOwnerCh)
			assert.Equal(t, tt.expectedAuthCh, hasAuthCh)
		})
	}
}

func TestConvertInterfaceToAuthList(t *testing.T) {
	tests := []struct {
		name     string
		input    []interface{}
		expected []Authorization
	}{
		{
			name:     "empty list",
			input:    []interface{}{},
			expected: []Authorization{},
		},
		{
			name: "valid authorization entries",
			input: []interface{}{
				map[string]interface{}{"subject": "user1", "role": "reader"},
				map[string]interface{}{"subject": "user2", "role": "writer"},
				map[string]interface{}{"subject": "user3", "role": "owner"},
			},
			expected: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "user2", Role: RoleWriter},
				{Subject: "user3", Role: RoleOwner},
			},
		},
		{
			name: "partial entries",
			input: []interface{}{
				map[string]interface{}{"subject": "user1"}, // No role
				map[string]interface{}{"role": "reader"},   // No subject
			},
			expected: []Authorization{
				{Subject: "user1", Role: ""},
				{Subject: "", Role: RoleReader},
			},
		},
		{
			name: "invalid entries ignored",
			input: []interface{}{
				"invalid string", // Will be ignored
				123,              // Will be ignored
				map[string]interface{}{"subject": "user1", "role": "reader"}, // Valid
			},
			expected: []Authorization{
				{Subject: "user1", Role: RoleReader}, // Only valid map processed
			},
		},
		{
			name: "invalid subject/role types",
			input: []interface{}{
				map[string]interface{}{"subject": 123, "role": "reader"}, // Invalid subject type -> empty subject
				map[string]interface{}{"subject": "user1", "role": 456},  // Invalid role type -> empty role
			},
			expected: []Authorization{
				{Subject: "", Role: RoleReader}, // Subject was not string, role was
				{Subject: "user1", Role: ""},    // Subject was string, role was not
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertInterfaceToAuthList(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateAuthorizationEntries(t *testing.T) {
	tests := []struct {
		name        string
		authList    []Authorization
		expectError bool
	}{
		{
			name: "valid entries",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "user2", Role: RoleWriter},
			},
			expectError: false,
		},
		{
			name:        "empty list",
			authList:    []Authorization{},
			expectError: false,
		},
		{
			name: "empty subject",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "", Role: RoleWriter}, // Empty subject
			},
			expectError: true,
		},
		{
			name: "multiple empty subjects",
			authList: []Authorization{
				{Subject: "", Role: RoleReader}, // First empty
				{Subject: "user1", Role: RoleWriter},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAuthorizationEntries(tt.authList)

			if tt.expectError {
				require.Error(t, err)
				reqErr, ok := err.(*RequestError)
				require.True(t, ok, "Expected RequestError")
				assert.Equal(t, http.StatusBadRequest, reqErr.Status)
				assert.Equal(t, "invalid_input", reqErr.Code)
				assert.Contains(t, reqErr.Message, "subject cannot be empty")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateAuthorizationEntriesWithFormat(t *testing.T) {
	tests := []struct {
		name        string
		authList    []Authorization
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid entries",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "user2", Role: RoleWriter},
				{Subject: "user3", Role: RoleOwner},
			},
			expectError: false,
		},
		{
			name:        "empty list",
			authList:    []Authorization{},
			expectError: false,
		},
		{
			name: "empty subject",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "", Role: RoleWriter}, // Empty subject
			},
			expectError: true,
			errorMsg:    "Authorization subject at index 1 cannot be empty",
		},
		{
			name: "subject too long",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: string(make([]byte, 256)), Role: RoleWriter}, // 256 chars
			},
			expectError: true,
			errorMsg:    "exceeds maximum length of 255 characters",
		},
		{
			name: "invalid role",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "user2", Role: "invalid_role"}, // Invalid role
			},
			expectError: true,
			errorMsg:    "Invalid role 'invalid_role' for subject 'user2'",
		},
		{
			name: "first entry has error",
			authList: []Authorization{
				{Subject: "", Role: RoleReader}, // First entry error
			},
			expectError: true,
			errorMsg:    "Authorization subject at index 0 cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAuthorizationEntriesWithFormat(tt.authList)

			if tt.expectError {
				require.Error(t, err)
				reqErr, ok := err.(*RequestError)
				require.True(t, ok, "Expected RequestError")
				assert.Equal(t, http.StatusBadRequest, reqErr.Status)
				assert.Equal(t, "invalid_input", reqErr.Code)
				assert.Contains(t, reqErr.Message, tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
