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

func TestAccessCheck(t *testing.T) {
	tests := []struct {
		name         string
		principal    string
		requiredRole Role
		authData     AuthorizationData
		expected     bool
	}{
		{
			name:         "valid type - owner has access",
			principal:    "owner1",
			requiredRole: RoleReader,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: "owner1",
				Authorization: []Authorization{
					{Subject: "user1", Role: RoleReader},
				},
			},
			expected: true,
		},
		{
			name:         "valid type - owner has access for any required role",
			principal:    "owner1",
			requiredRole: RoleOwner,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: "owner1",
				Authorization: []Authorization{
					{Subject: "user1", Role: RoleReader},
				},
			},
			expected: true,
		},
		{
			name:         "valid type - user has exact required role",
			principal:    "user1",
			requiredRole: RoleReader,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: "owner1",
				Authorization: []Authorization{
					{Subject: "user1", Role: RoleReader},
				},
			},
			expected: true,
		},
		{
			name:         "valid type - user has higher role than required",
			principal:    "user1",
			requiredRole: RoleReader,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: "owner1",
				Authorization: []Authorization{
					{Subject: "user1", Role: RoleWriter},
				},
			},
			expected: true,
		},
		{
			name:         "valid type - user has lower role than required",
			principal:    "user1",
			requiredRole: RoleWriter,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: "owner1",
				Authorization: []Authorization{
					{Subject: "user1", Role: RoleReader},
				},
			},
			expected: false,
		},
		{
			name:         "valid type - principal not in authorization list",
			principal:    "user2",
			requiredRole: RoleReader,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: "owner1",
				Authorization: []Authorization{
					{Subject: "user1", Role: RoleReader},
				},
			},
			expected: false,
		},
		{
			name:         "invalid authorization type",
			principal:    "owner1",
			requiredRole: RoleReader,
			authData: AuthorizationData{
				Type:  "invalid-type",
				Owner: "owner1",
				Authorization: []Authorization{
					{Subject: "user1", Role: RoleReader},
				},
			},
			expected: false,
		},
		{
			name:         "empty authorization list",
			principal:    "user1",
			requiredRole: RoleReader,
			authData: AuthorizationData{
				Type:          AuthTypeTMI10,
				Owner:         "owner1",
				Authorization: []Authorization{},
			},
			expected: false,
		},
		{
			name:         "multiple users in authorization list",
			principal:    "user2",
			requiredRole: RoleWriter,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: "owner1",
				Authorization: []Authorization{
					{Subject: "user1", Role: RoleReader},
					{Subject: "user2", Role: RoleWriter},
					{Subject: "user3", Role: RoleOwner},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AccessCheck(tt.principal, tt.requiredRole, tt.authData)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasRequiredRole(t *testing.T) {
	tests := []struct {
		name         string
		userRole     Role
		requiredRole Role
		expected     bool
	}{
		{
			name:         "reader has reader access",
			userRole:     RoleReader,
			requiredRole: RoleReader,
			expected:     true,
		},
		{
			name:         "writer has reader access",
			userRole:     RoleWriter,
			requiredRole: RoleReader,
			expected:     true,
		},
		{
			name:         "owner has reader access",
			userRole:     RoleOwner,
			requiredRole: RoleReader,
			expected:     true,
		},
		{
			name:         "reader lacks writer access",
			userRole:     RoleReader,
			requiredRole: RoleWriter,
			expected:     false,
		},
		{
			name:         "writer has writer access",
			userRole:     RoleWriter,
			requiredRole: RoleWriter,
			expected:     true,
		},
		{
			name:         "owner has writer access",
			userRole:     RoleOwner,
			requiredRole: RoleWriter,
			expected:     true,
		},
		{
			name:         "reader lacks owner access",
			userRole:     RoleReader,
			requiredRole: RoleOwner,
			expected:     false,
		},
		{
			name:         "writer lacks owner access",
			userRole:     RoleWriter,
			requiredRole: RoleOwner,
			expected:     false,
		},
		{
			name:         "owner has owner access",
			userRole:     RoleOwner,
			requiredRole: RoleOwner,
			expected:     true,
		},
		{
			name:         "invalid user role",
			userRole:     "invalid",
			requiredRole: RoleReader,
			expected:     false,
		},
		{
			name:         "invalid required role",
			userRole:     RoleReader,
			requiredRole: "invalid",
			expected:     false,
		},
		{
			name:         "both roles invalid",
			userRole:     "invalid1",
			requiredRole: "invalid2",
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasRequiredRole(tt.userRole, tt.requiredRole)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractAuthData(t *testing.T) {
	// Save original test fixtures
	originalOwner := TestFixtures.Owner
	originalAuth := TestFixtures.ThreatModel.Authorization

	// Restore after test
	defer func() {
		TestFixtures.Owner = originalOwner
		TestFixtures.ThreatModel.Authorization = originalAuth
	}()

	tests := []struct {
		name          string
		setupFixtures func()
		resource      interface{}
		expectedOwner string
		expectedAuth  []Authorization
		expectedType  string
		expectError   bool
	}{
		{
			name: "valid test fixtures",
			setupFixtures: func() {
				TestFixtures.Owner = "testowner"
				TestFixtures.ThreatModel.Authorization = []Authorization{
					{Subject: "user1", Role: RoleReader},
					{Subject: "user2", Role: RoleWriter},
				}
			},
			resource:      "dummy",
			expectedOwner: "testowner",
			expectedAuth: []Authorization{
				{Subject: "user1", Role: RoleReader},
				{Subject: "user2", Role: RoleWriter},
			},
			expectedType: AuthTypeTMI10,
			expectError:  false,
		},
		{
			name: "empty test fixtures",
			setupFixtures: func() {
				TestFixtures.Owner = ""
				TestFixtures.ThreatModel.Authorization = nil
			},
			resource:    "dummy",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test fixtures
			tt.setupFixtures()

			// Test the function
			authData, err := ExtractAuthData(tt.resource)

			if tt.expectError {
				require.Error(t, err)
				reqErr, ok := err.(*RequestError)
				require.True(t, ok, "Expected RequestError")
				assert.Equal(t, http.StatusInternalServerError, reqErr.Status)
				assert.Equal(t, "server_error", reqErr.Code)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedType, authData.Type)
				assert.Equal(t, tt.expectedOwner, authData.Owner)
				assert.Equal(t, tt.expectedAuth, authData.Authorization)
			}
		})
	}
}

func TestCheckResourceAccess(t *testing.T) {
	// Save original test fixtures
	originalOwner := TestFixtures.Owner
	originalAuth := TestFixtures.ThreatModel.Authorization

	// Restore after test
	defer func() {
		TestFixtures.Owner = originalOwner
		TestFixtures.ThreatModel.Authorization = originalAuth
	}()

	tests := []struct {
		name           string
		setupFixtures  func()
		userName       string
		requiredRole   Role
		resource       interface{}
		expectedAccess bool
		expectError    bool
	}{
		{
			name: "owner has access",
			setupFixtures: func() {
				TestFixtures.Owner = "owner1"
				TestFixtures.ThreatModel.Authorization = []Authorization{
					{Subject: "user1", Role: RoleReader},
				}
			},
			userName:       "owner1",
			requiredRole:   RoleReader,
			resource:       "dummy",
			expectedAccess: true,
			expectError:    false,
		},
		{
			name: "user has sufficient role",
			setupFixtures: func() {
				TestFixtures.Owner = "owner1"
				TestFixtures.ThreatModel.Authorization = []Authorization{
					{Subject: "user1", Role: RoleWriter},
				}
			},
			userName:       "user1",
			requiredRole:   RoleReader,
			resource:       "dummy",
			expectedAccess: true,
			expectError:    false,
		},
		{
			name: "user lacks sufficient role",
			setupFixtures: func() {
				TestFixtures.Owner = "owner1"
				TestFixtures.ThreatModel.Authorization = []Authorization{
					{Subject: "user1", Role: RoleReader},
				}
			},
			userName:       "user1",
			requiredRole:   RoleWriter,
			resource:       "dummy",
			expectedAccess: false,
			expectError:    false,
		},
		{
			name: "user not in authorization list",
			setupFixtures: func() {
				TestFixtures.Owner = "owner1"
				TestFixtures.ThreatModel.Authorization = []Authorization{
					{Subject: "user1", Role: RoleReader},
				}
			},
			userName:       "user2",
			requiredRole:   RoleReader,
			resource:       "dummy",
			expectedAccess: false,
			expectError:    false,
		},
		{
			name: "extraction error",
			setupFixtures: func() {
				TestFixtures.Owner = ""
				TestFixtures.ThreatModel.Authorization = nil
			},
			userName:     "user1",
			requiredRole: RoleReader,
			resource:     "dummy",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test fixtures
			tt.setupFixtures()

			// Test the function
			hasAccess, err := CheckResourceAccess(tt.userName, tt.resource, tt.requiredRole)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedAccess, hasAccess)
			}
		})
	}
}

func TestPermissionResolution(t *testing.T) {
	tests := []struct {
		name                 string
		principal            string
		owner                string
		authList             []Authorization
		expectedReaderAccess bool
		expectedWriterAccess bool
		expectedOwnerAccess  bool
		description          string
	}{
		{
			name:      "owner in auth list with reader role - owner field wins",
			principal: "owner1",
			owner:     "owner1",
			authList: []Authorization{
				{Subject: "owner1", Role: RoleReader}, // Lower permission in auth list
			},
			expectedReaderAccess: true, // Owner always has reader access
			expectedWriterAccess: true, // Owner always has writer access
			expectedOwnerAccess:  true, // Owner always has owner access
			description:          "Owner field takes absolute precedence over auth list",
		},
		{
			name:      "user in auth list with multiple roles - highest permission wins",
			principal: "user1",
			owner:     "owner",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader}, // Lower permission
				{Subject: "user1", Role: RoleWriter}, // Higher permission should win
			},
			expectedReaderAccess: true,  // Writer includes reader access
			expectedWriterAccess: true,  // User should get writer access
			expectedOwnerAccess:  false, // User is not owner
			description:          "Multiple roles: RoleWriter > RoleReader, so user gets RoleWriter",
		},
		{
			name:      "user with reader then owner roles - owner wins",
			principal: "user1",
			owner:     "owner",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader}, // Lower permission
				{Subject: "user1", Role: RoleOwner},  // Higher permission should win
			},
			expectedReaderAccess: true, // Owner includes reader access
			expectedWriterAccess: true, // Owner includes writer access
			expectedOwnerAccess:  true, // User should get owner access
			description:          "Multiple roles: RoleOwner > RoleReader, so user gets RoleOwner",
		},
		{
			name:      "user with writer then owner roles - owner wins",
			principal: "user1",
			owner:     "owner",
			authList: []Authorization{
				{Subject: "user1", Role: RoleWriter}, // Lower permission
				{Subject: "user1", Role: RoleOwner},  // Higher permission should win
			},
			expectedReaderAccess: true, // Owner includes reader access
			expectedWriterAccess: true, // Owner includes writer access
			expectedOwnerAccess:  true, // User should get owner access
			description:          "Multiple roles: RoleOwner > RoleWriter, so user gets RoleOwner",
		},
		{
			name:      "user with single writer role",
			principal: "user1",
			owner:     "owner",
			authList: []Authorization{
				{Subject: "user1", Role: RoleWriter},
			},
			expectedReaderAccess: true,  // Writer includes reader access
			expectedWriterAccess: true,  // Writer can write
			expectedOwnerAccess:  false, // Writer cannot own
			description:          "Single role: writer includes reader permissions",
		},
		{
			name:      "user with single reader role",
			principal: "user1",
			owner:     "owner",
			authList: []Authorization{
				{Subject: "user1", Role: RoleReader},
			},
			expectedReaderAccess: true,  // Reader can read
			expectedWriterAccess: false, // Reader cannot write
			expectedOwnerAccess:  false, // Reader cannot own
			description:          "Single role: reader has most limited permissions",
		},
		{
			name:      "user not in auth list has no permissions",
			principal: "user1",
			owner:     "owner",
			authList: []Authorization{
				{Subject: "user2", Role: RoleOwner},
			},
			expectedReaderAccess: false, // Not found in auth list
			expectedWriterAccess: false, // Not found in auth list
			expectedOwnerAccess:  false, // Not found in auth list
			description:          "Users not in auth list have no access",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authData := AuthorizationData{
				Type:          AuthTypeTMI10,
				Owner:         tt.owner,
				Authorization: tt.authList,
			}

			// Test reader access
			readerAccess := AccessCheck(tt.principal, RoleReader, authData)
			assert.Equal(t, tt.expectedReaderAccess, readerAccess,
				"Reader access mismatch: %s", tt.description)

			// Test writer access
			writerAccess := AccessCheck(tt.principal, RoleWriter, authData)
			assert.Equal(t, tt.expectedWriterAccess, writerAccess,
				"Writer access mismatch: %s", tt.description)

			// Test owner access
			ownerAccess := AccessCheck(tt.principal, RoleOwner, authData)
			assert.Equal(t, tt.expectedOwnerAccess, ownerAccess,
				"Owner access mismatch: %s", tt.description)
		})
	}
}

func TestIsHigherRole(t *testing.T) {
	tests := []struct {
		name     string
		role1    Role
		role2    Role
		expected bool
	}{
		{
			name:     "owner higher than writer",
			role1:    RoleOwner,
			role2:    RoleWriter,
			expected: true,
		},
		{
			name:     "owner higher than reader",
			role1:    RoleOwner,
			role2:    RoleReader,
			expected: true,
		},
		{
			name:     "writer higher than reader",
			role1:    RoleWriter,
			role2:    RoleReader,
			expected: true,
		},
		{
			name:     "writer not higher than owner",
			role1:    RoleWriter,
			role2:    RoleOwner,
			expected: false,
		},
		{
			name:     "reader not higher than writer",
			role1:    RoleReader,
			role2:    RoleWriter,
			expected: false,
		},
		{
			name:     "reader not higher than owner",
			role1:    RoleReader,
			role2:    RoleOwner,
			expected: false,
		},
		{
			name:     "same role not higher",
			role1:    RoleWriter,
			role2:    RoleWriter,
			expected: false,
		},
		{
			name:     "invalid role1",
			role1:    "invalid",
			role2:    RoleReader,
			expected: false,
		},
		{
			name:     "invalid role2",
			role1:    RoleReader,
			role2:    "invalid",
			expected: false,
		},
		{
			name:     "both roles invalid",
			role1:    "invalid1",
			role2:    "invalid2",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isHigherRole(tt.role1, tt.role2)
			assert.Equal(t, tt.expected, result)
		})
	}
}
