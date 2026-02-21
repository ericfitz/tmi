package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Phase 1: Tests for untested authorization utility functions
// Focus: Functions that control who can access what -- bugs here = security bugs
// =============================================================================

func TestValidateSparseAuthorizationEntries(t *testing.T) {
	validEmail := openapi_types.Email("alice@example.com")
	emptyEmail := openapi_types.Email("")

	tests := []struct {
		name        string
		authList    []Authorization
		expectError bool
		errorCode   string
		errorMsg    string
	}{
		{
			name:        "empty list is valid",
			authList:    []Authorization{},
			expectError: false,
		},
		{
			name: "valid entry with provider and provider_id",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "user123",
					Role:          RoleReader,
				},
			},
			expectError: false,
		},
		{
			name: "valid entry with provider and email (no provider_id)",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					Email:         &validEmail,
					Role:          RoleWriter,
				},
			},
			expectError: false,
		},
		{
			name: "valid entry with both provider_id and email",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "google",
					ProviderId:    "google-uid-123",
					Email:         &validEmail,
					Role:          RoleOwner,
				},
			},
			expectError: false,
		},
		{
			name: "valid group entry",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeGroup,
					Provider:      "*",
					ProviderId:    "editors",
					Role:          RoleWriter,
				},
			},
			expectError: false,
		},
		{
			name: "missing provider",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "",
					ProviderId:    "user123",
					Role:          RoleReader,
				},
			},
			expectError: true,
			errorCode:   "validation_failed",
			errorMsg:    "'provider' is required",
		},
		{
			name: "missing both provider_id and email",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					Role:          RoleReader,
				},
			},
			expectError: true,
			errorCode:   "validation_failed",
			errorMsg:    "either 'provider_id' or 'email' must be provided",
		},
		{
			name: "empty email pointer does not count as identifier",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					Email:         &emptyEmail,
					Role:          RoleReader,
				},
			},
			expectError: true,
			errorCode:   "validation_failed",
			errorMsg:    "either 'provider_id' or 'email' must be provided",
		},
		{
			name: "nil email pointer does not count as identifier",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					Email:         nil,
					Role:          RoleReader,
				},
			},
			expectError: true,
			errorCode:   "validation_failed",
			errorMsg:    "either 'provider_id' or 'email' must be provided",
		},
		{
			name: "invalid role",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "user123",
					Role:          "superadmin",
				},
			},
			expectError: true,
			errorCode:   "validation_failed",
			errorMsg:    "invalid role 'superadmin'",
		},
		{
			name: "empty string role is invalid",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "user123",
					Role:          "",
				},
			},
			expectError: true,
			errorCode:   "validation_failed",
			errorMsg:    "invalid role ''",
		},
		{
			name: "display_name present is rejected",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "user123",
					Role:          RoleReader,
					DisplayName:   strPtr("Alice User"),
				},
			},
			expectError: true,
			errorCode:   "validation_failed",
			errorMsg:    "'display_name' cannot be provided in requests",
		},
		{
			name: "empty display_name is allowed (not considered provided)",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "user123",
					Role:          RoleReader,
					DisplayName:   strPtr(""),
				},
			},
			expectError: false,
		},
		{
			name: "nil display_name is allowed",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "user123",
					Role:          RoleReader,
					DisplayName:   nil,
				},
			},
			expectError: false,
		},
		{
			name: "error reported on correct index",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "user1",
					Role:          RoleReader,
				},
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "user2",
					Role:          RoleWriter,
				},
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "", // error at index 2
					ProviderId:    "user3",
					Role:          RoleReader,
				},
			},
			expectError: true,
			errorCode:   "validation_failed",
			errorMsg:    "index 2",
		},
		{
			name: "multiple valid entries",
			authList: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "user1",
					Role:          RoleReader,
				},
				{
					PrincipalType: AuthorizationPrincipalTypeGroup,
					Provider:      "*",
					ProviderId:    "editors",
					Role:          RoleWriter,
				},
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "google",
					Email:         &validEmail,
					Role:          RoleOwner,
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSparseAuthorizationEntries(tt.authList)

			if tt.expectError {
				require.Error(t, err)
				var reqErr *RequestError
				require.True(t, errors.As(err, &reqErr), "Expected RequestError, got %T", err)
				assert.Equal(t, http.StatusBadRequest, reqErr.Status)
				assert.Equal(t, tt.errorCode, reqErr.Code)
				assert.Contains(t, reqErr.Message, tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStripResponseOnlyAuthFields(t *testing.T) {
	tests := []struct {
		name    string
		input   []Authorization
		checkFn func(t *testing.T, result []Authorization)
	}{
		{
			name:  "empty list returns empty list",
			input: []Authorization{},
			checkFn: func(t *testing.T, result []Authorization) {
				assert.Len(t, result, 0)
			},
		},
		{
			name: "strips display_name from entries",
			input: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "user1",
					Role:          RoleReader,
					DisplayName:   strPtr("Alice User"),
				},
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "user2",
					Role:          RoleWriter,
					DisplayName:   strPtr("Bob User"),
				},
			},
			checkFn: func(t *testing.T, result []Authorization) {
				require.Len(t, result, 2)
				assert.Nil(t, result[0].DisplayName, "display_name should be stripped")
				assert.Nil(t, result[1].DisplayName, "display_name should be stripped")
			},
		},
		{
			name: "preserves all other fields",
			input: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "google",
					ProviderId:    "google-uid-123",
					Role:          RoleOwner,
					DisplayName:   strPtr("Power User"),
				},
			},
			checkFn: func(t *testing.T, result []Authorization) {
				require.Len(t, result, 1)
				assert.Equal(t, AuthorizationPrincipalTypeUser, result[0].PrincipalType)
				assert.Equal(t, "google", result[0].Provider)
				assert.Equal(t, "google-uid-123", result[0].ProviderId)
				assert.Equal(t, RoleOwner, result[0].Role)
				assert.Nil(t, result[0].DisplayName)
			},
		},
		{
			name: "already nil display_name stays nil",
			input: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "user1",
					Role:          RoleReader,
					DisplayName:   nil,
				},
			},
			checkFn: func(t *testing.T, result []Authorization) {
				require.Len(t, result, 1)
				assert.Nil(t, result[0].DisplayName)
			},
		},
		{
			name: "returns new slice - does not modify original",
			input: []Authorization{
				{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "user1",
					Role:          RoleReader,
					DisplayName:   strPtr("Original Name"),
				},
			},
			checkFn: func(t *testing.T, result []Authorization) {
				// The original input should have its DisplayName intact
				// since we check the result, not the input
				require.Len(t, result, 1)
				assert.Nil(t, result[0].DisplayName)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Copy input to verify it's not modified
			originalInput := make([]Authorization, len(tt.input))
			copy(originalInput, tt.input)

			result := StripResponseOnlyAuthFields(tt.input)
			tt.checkFn(t, result)

			// Verify original input was not modified
			for i := range originalInput {
				if originalInput[i].DisplayName != nil {
					assert.NotNil(t, tt.input[i].DisplayName,
						"original input should not be modified by StripResponseOnlyAuthFields")
				}
			}
		})
	}
}

func TestDeduplicateAuthorizationList(t *testing.T) {
	email1 := openapi_types.Email("alice@example.com")
	email2 := openapi_types.Email("bob@example.com")

	tests := []struct {
		name     string
		input    []Authorization
		expected []Authorization
	}{
		{
			name:     "nil list",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty list",
			input:    []Authorization{},
			expected: []Authorization{},
		},
		{
			name: "single entry returned as-is",
			input: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "user1", Role: RoleReader},
			},
			expected: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "user1", Role: RoleReader},
			},
		},
		{
			name: "no duplicates - all entries preserved in order",
			input: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "user1", Role: RoleReader},
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "user2", Role: RoleWriter},
				{PrincipalType: AuthorizationPrincipalTypeGroup, Provider: "*", ProviderId: "editors", Role: RoleWriter},
			},
			expected: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "user1", Role: RoleReader},
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "user2", Role: RoleWriter},
				{PrincipalType: AuthorizationPrincipalTypeGroup, Provider: "*", ProviderId: "editors", Role: RoleWriter},
			},
		},
		{
			name: "duplicate user by provider_id - last occurrence wins",
			input: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "user1", Role: RoleReader},
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "user2", Role: RoleWriter},
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "user1", Role: RoleOwner}, // duplicate, should win
			},
			expected: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "user2", Role: RoleWriter},
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "user1", Role: RoleOwner}, // kept - last occurrence
			},
		},
		{
			name: "duplicate user by email (no provider_id) - last occurrence wins",
			input: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", Email: &email1, Role: RoleReader},
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", Email: &email2, Role: RoleWriter},
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", Email: &email1, Role: RoleOwner}, // duplicate, should win
			},
			expected: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", Email: &email2, Role: RoleWriter},
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", Email: &email1, Role: RoleOwner}, // kept
			},
		},
		{
			name: "duplicate group - last occurrence wins",
			input: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeGroup, Provider: "*", ProviderId: "editors", Role: RoleReader},
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "user1", Role: RoleWriter},
				{PrincipalType: AuthorizationPrincipalTypeGroup, Provider: "*", ProviderId: "editors", Role: RoleOwner}, // duplicate, should win
			},
			expected: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "user1", Role: RoleWriter},
				{PrincipalType: AuthorizationPrincipalTypeGroup, Provider: "*", ProviderId: "editors", Role: RoleOwner}, // kept
			},
		},
		{
			name: "different providers same provider_id are NOT duplicates",
			input: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "google", ProviderId: "user1", Role: RoleReader},
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "okta", ProviderId: "user1", Role: RoleWriter},
			},
			expected: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "google", ProviderId: "user1", Role: RoleReader},
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "okta", ProviderId: "user1", Role: RoleWriter},
			},
		},
		{
			name: "user and group with same provider_id are NOT duplicates",
			input: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "editors", Role: RoleReader},
				{PrincipalType: AuthorizationPrincipalTypeGroup, Provider: "tmi", ProviderId: "editors", Role: RoleWriter},
			},
			expected: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "editors", Role: RoleReader},
				{PrincipalType: AuthorizationPrincipalTypeGroup, Provider: "tmi", ProviderId: "editors", Role: RoleWriter},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DeduplicateAuthorizationList(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAccessCheckFlexibleMatching(t *testing.T) {
	// This tests the critical flexible matching in AccessCheckWithGroupsAndIdPLookup
	// where matchesProviderID and matchesUserIdentifier compare against
	// internal_uuid, provider_user_id, and email interchangeably.

	tests := []struct {
		name                  string
		principal             string // email
		principalProviderID   string
		principalInternalUUID string
		principalIdP          string
		principalGroups       []string
		requiredRole          Role
		authData              AuthorizationData
		expected              bool
		description           string
	}{
		// --- Owner matching via different identifiers ---
		{
			name:                  "owner matched by email",
			principal:             "alice@example.com",
			principalProviderID:   "google-uid-alice",
			principalInternalUUID: "uuid-alice-internal",
			principalIdP:          "google",
			requiredRole:          RoleOwner,
			authData: AuthorizationData{
				Type: AuthTypeTMI10,
				Owner: User{
					PrincipalType: UserPrincipalTypeUser,
					Provider:      "google",
					ProviderId:    "alice@example.com", // matches principal (email)
				},
			},
			expected:    true,
			description: "Owner.ProviderId matching user email should grant access",
		},
		{
			name:                  "owner matched by provider_user_id",
			principal:             "alice@example.com",
			principalProviderID:   "google-uid-alice",
			principalInternalUUID: "uuid-alice-internal",
			principalIdP:          "google",
			requiredRole:          RoleOwner,
			authData: AuthorizationData{
				Type: AuthTypeTMI10,
				Owner: User{
					PrincipalType: UserPrincipalTypeUser,
					Provider:      "google",
					ProviderId:    "google-uid-alice", // matches principalProviderID
				},
			},
			expected:    true,
			description: "Owner.ProviderId matching provider_user_id should grant access",
		},
		{
			name:                  "owner matched by internal_uuid",
			principal:             "alice@example.com",
			principalProviderID:   "google-uid-alice",
			principalInternalUUID: "uuid-alice-internal",
			principalIdP:          "google",
			requiredRole:          RoleOwner,
			authData: AuthorizationData{
				Type: AuthTypeTMI10,
				Owner: User{
					PrincipalType: UserPrincipalTypeUser,
					Provider:      "google",
					ProviderId:    "uuid-alice-internal", // matches principalInternalUUID
				},
			},
			expected:    true,
			description: "Owner.ProviderId matching internal_uuid should grant access",
		},
		{
			name:                  "owner NOT matched - no identifier matches",
			principal:             "alice@example.com",
			principalProviderID:   "google-uid-alice",
			principalInternalUUID: "uuid-alice-internal",
			principalIdP:          "google",
			requiredRole:          RoleReader,
			authData: AuthorizationData{
				Type: AuthTypeTMI10,
				Owner: User{
					PrincipalType: UserPrincipalTypeUser,
					Provider:      "google",
					ProviderId:    "completely-different-id",
				},
				Authorization: []Authorization{},
			},
			expected:    false,
			description: "When owner.ProviderId matches none of the user's identifiers, no access",
		},

		// --- Authorization list matching via different identifiers ---
		{
			name:                  "auth list user matched by provider_user_id (not email)",
			principal:             "alice@example.com",
			principalProviderID:   "google-uid-alice",
			principalInternalUUID: "uuid-alice-internal",
			principalIdP:          "google",
			requiredRole:          RoleReader,
			authData: AuthorizationData{
				Type: AuthTypeTMI10,
				Owner: User{
					PrincipalType: UserPrincipalTypeUser,
					Provider:      "google",
					ProviderId:    "some-other-owner",
				},
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeUser,
						Provider:      "google",
						ProviderId:    "google-uid-alice", // matches principalProviderID
						Role:          RoleReader,
					},
				},
			},
			expected:    true,
			description: "Auth entry ProviderId matching provider_user_id grants access",
		},
		{
			name:                  "auth list user matched by internal_uuid",
			principal:             "alice@example.com",
			principalProviderID:   "google-uid-alice",
			principalInternalUUID: "uuid-alice-internal",
			principalIdP:          "google",
			requiredRole:          RoleWriter,
			authData: AuthorizationData{
				Type: AuthTypeTMI10,
				Owner: User{
					PrincipalType: UserPrincipalTypeUser,
					Provider:      "google",
					ProviderId:    "some-other-owner",
				},
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeUser,
						Provider:      "google",
						ProviderId:    "uuid-alice-internal", // matches principalInternalUUID
						Role:          RoleWriter,
					},
				},
			},
			expected:    true,
			description: "Auth entry ProviderId matching internal_uuid grants access",
		},
		{
			name:                  "auth list user matched by email",
			principal:             "alice@example.com",
			principalProviderID:   "google-uid-alice",
			principalInternalUUID: "uuid-alice-internal",
			principalIdP:          "google",
			requiredRole:          RoleReader,
			authData: AuthorizationData{
				Type: AuthTypeTMI10,
				Owner: User{
					PrincipalType: UserPrincipalTypeUser,
					Provider:      "google",
					ProviderId:    "some-other-owner",
				},
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeUser,
						Provider:      "google",
						ProviderId:    "alice@example.com", // matches principal (email)
						Role:          RoleReader,
					},
				},
			},
			expected:    true,
			description: "Auth entry ProviderId matching email grants access",
		},

		// --- Cross-field collision: a DIFFERENT user's provider_id matches this user's email ---
		// This tests the potential false-positive in matchesProviderID
		{
			name:                  "cross-field collision - auth entry provider_id equals different user email is still a match",
			principal:             "alice@example.com",
			principalProviderID:   "google-uid-alice",
			principalInternalUUID: "uuid-alice-internal",
			principalIdP:          "google",
			requiredRole:          RoleReader,
			authData: AuthorizationData{
				Type: AuthTypeTMI10,
				Owner: User{
					PrincipalType: UserPrincipalTypeUser,
					Provider:      "google",
					ProviderId:    "some-other-owner",
				},
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeUser,
						Provider:      "google",
						// This ProviderId is "alice@example.com" -- it's intended for a different
						// user whose provider_id happens to be that email string. However,
						// matchesProviderID will match against our user's email.
						// This documents the current behavior: flexible matching means this IS a match.
						ProviderId: "alice@example.com",
						Role:       RoleReader,
					},
				},
			},
			expected:    true,
			description: "Current behavior: flexible matching means ProviderId matching any user identifier is a match",
		},

		// --- Highest role wins across multiple matches ---
		{
			name:                  "multiple matching entries - highest role wins",
			principal:             "alice@example.com",
			principalProviderID:   "google-uid-alice",
			principalInternalUUID: "uuid-alice-internal",
			principalIdP:          "google",
			requiredRole:          RoleOwner,
			authData: AuthorizationData{
				Type: AuthTypeTMI10,
				Owner: User{
					PrincipalType: UserPrincipalTypeUser,
					Provider:      "google",
					ProviderId:    "some-other-owner",
				},
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeUser,
						Provider:      "google",
						ProviderId:    "alice@example.com", // matched by email
						Role:          RoleReader,
					},
					{
						PrincipalType: AuthorizationPrincipalTypeUser,
						Provider:      "google",
						ProviderId:    "google-uid-alice", // matched by provider_id
						Role:          RoleWriter,
					},
					{
						PrincipalType: AuthorizationPrincipalTypeUser,
						Provider:      "google",
						ProviderId:    "uuid-alice-internal", // matched by internal_uuid
						Role:          RoleOwner,
					},
				},
			},
			expected:    true,
			description: "When user matches via different identifiers, highest role should win",
		},

		// --- Empty identifiers ---
		{
			name:                  "empty principalProviderID and principalInternalUUID - only email match works",
			principal:             "alice@example.com",
			principalProviderID:   "",
			principalInternalUUID: "",
			principalIdP:          "tmi",
			requiredRole:          RoleReader,
			authData: AuthorizationData{
				Type: AuthTypeTMI10,
				Owner: User{
					PrincipalType: UserPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "some-other-owner",
				},
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeUser,
						Provider:      "tmi",
						ProviderId:    "alice@example.com",
						Role:          RoleReader,
					},
				},
			},
			expected:    true,
			description: "With empty provider_id and internal_uuid, email match should still work",
		},
		{
			name:                  "empty auth entry ProviderId matches empty principalProviderID",
			principal:             "alice@example.com",
			principalProviderID:   "",
			principalInternalUUID: "",
			principalIdP:          "tmi",
			requiredRole:          RoleReader,
			authData: AuthorizationData{
				Type: AuthTypeTMI10,
				Owner: User{
					PrincipalType: UserPrincipalTypeUser,
					Provider:      "tmi",
					ProviderId:    "some-other-owner",
				},
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeUser,
						Provider:      "tmi",
						ProviderId:    "", // empty matches empty principalProviderID and empty principalInternalUUID
						Role:          RoleReader,
					},
				},
			},
			// matchesProviderID("", "alice@example.com", "", "") =>
			// "" == "" (internalUUID) => true
			expected:    true,
			description: "Empty ProviderId matches empty principalInternalUUID due to string equality",
		},

		// --- Invalid authorization type ---
		{
			name:                  "invalid authorization type denies access",
			principal:             "alice@example.com",
			principalProviderID:   "google-uid-alice",
			principalInternalUUID: "uuid-alice-internal",
			principalIdP:          "google",
			requiredRole:          RoleReader,
			authData: AuthorizationData{
				Type: "invalid-type",
				Owner: User{
					PrincipalType: UserPrincipalTypeUser,
					Provider:      "google",
					ProviderId:    "alice@example.com",
				},
			},
			expected:    false,
			description: "Non-tmi-1.0 auth type should always deny",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AccessCheckWithGroupsAndIdPLookup(
				tt.principal,
				tt.principalProviderID,
				tt.principalInternalUUID,
				tt.principalIdP,
				tt.principalGroups,
				tt.requiredRole,
				tt.authData,
			)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

func TestCheckGroupMatchProviderScoping(t *testing.T) {
	// Tests the checkGroupMatch function indirectly through AccessCheckWithGroupsAndIdPLookup
	// Focus: Provider scoping for normal groups (not the "everyone" pseudo-group)

	ownerUser := User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      "tmi",
		ProviderId:    "owner@example.com",
	}

	tests := []struct {
		name         string
		principal    string
		principalIdP string
		groups       []string
		requiredRole Role
		authData     AuthorizationData
		expected     bool
		description  string
	}{
		{
			name:         "wildcard provider group matches any IdP user",
			principal:    "alice@example.com",
			principalIdP: "google",
			groups:       []string{"editors"},
			requiredRole: RoleWriter,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: ownerUser,
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeGroup,
						Provider:      "*",
						ProviderId:    "editors",
						Role:          RoleWriter,
					},
				},
			},
			expected:    true,
			description: "Provider '*' should match users from any IdP",
		},
		{
			name:         "specific provider group matches same IdP user",
			principal:    "alice@example.com",
			principalIdP: "google",
			groups:       []string{"editors"},
			requiredRole: RoleWriter,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: ownerUser,
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeGroup,
						Provider:      "google",
						ProviderId:    "editors",
						Role:          RoleWriter,
					},
				},
			},
			expected:    true,
			description: "Provider 'google' should match google IdP user",
		},
		{
			name:         "specific provider group does NOT match different IdP user",
			principal:    "alice@example.com",
			principalIdP: "okta",
			groups:       []string{"editors"},
			requiredRole: RoleWriter,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: ownerUser,
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeGroup,
						Provider:      "google",
						ProviderId:    "editors",
						Role:          RoleWriter,
					},
				},
			},
			expected:    false,
			description: "Provider 'google' should NOT match okta IdP user",
		},
		{
			name:         "user not in the group - no match even with correct provider",
			principal:    "alice@example.com",
			principalIdP: "google",
			groups:       []string{"viewers"},
			requiredRole: RoleWriter,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: ownerUser,
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeGroup,
						Provider:      "google",
						ProviderId:    "editors",
						Role:          RoleWriter,
					},
				},
			},
			expected:    false,
			description: "User not in 'editors' group should not get access even with matching provider",
		},
		{
			name:         "empty IdP on user does not match specific provider group",
			principal:    "alice@example.com",
			principalIdP: "",
			groups:       []string{"editors"},
			requiredRole: RoleWriter,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: ownerUser,
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeGroup,
						Provider:      "google",
						ProviderId:    "editors",
						Role:          RoleWriter,
					},
				},
			},
			expected:    false,
			description: "Empty IdP should not match 'google' provider group",
		},
		{
			name:         "empty IdP on user matches wildcard provider group",
			principal:    "alice@example.com",
			principalIdP: "",
			groups:       []string{"editors"},
			requiredRole: RoleWriter,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: ownerUser,
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeGroup,
						Provider:      "*",
						ProviderId:    "editors",
						Role:          RoleWriter,
					},
				},
			},
			expected:    true,
			description: "Empty IdP should match '*' provider group",
		},
		{
			name:         "multiple groups - first matching group grants access",
			principal:    "alice@example.com",
			principalIdP: "google",
			groups:       []string{"viewers", "editors", "admins"},
			requiredRole: RoleWriter,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: ownerUser,
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeGroup,
						Provider:      "google",
						ProviderId:    "editors",
						Role:          RoleWriter,
					},
				},
			},
			expected:    true,
			description: "User with multiple groups should match if any group matches",
		},
		{
			name:         "group and user entries both match - highest role wins",
			principal:    "alice@example.com",
			principalIdP: "google",
			groups:       []string{"editors"},
			requiredRole: RoleOwner,
			authData: AuthorizationData{
				Type:  AuthTypeTMI10,
				Owner: ownerUser,
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeGroup,
						Provider:      "*",
						ProviderId:    "editors",
						Role:          RoleWriter,
					},
					{
						PrincipalType: AuthorizationPrincipalTypeUser,
						Provider:      "google",
						ProviderId:    "alice@example.com",
						Role:          RoleOwner,
					},
				},
			},
			expected:    true,
			description: "User with owner role via user entry should get owner even though group gives only writer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AccessCheckWithGroupsAndIdPLookup(
				tt.principal,
				"",
				"",
				tt.principalIdP,
				tt.groups,
				tt.requiredRole,
				tt.authData,
			)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

func TestSecurityReviewersHelpers(t *testing.T) {
	t.Run("SecurityReviewersAuthorization returns correct entry", func(t *testing.T) {
		auth := SecurityReviewersAuthorization()
		assert.Equal(t, AuthorizationPrincipalTypeGroup, auth.PrincipalType)
		assert.Equal(t, "*", auth.Provider)
		assert.Equal(t, SecurityReviewersGroup, auth.ProviderId)
		assert.Equal(t, AuthorizationRoleOwner, auth.Role)
	})

	t.Run("IsSecurityReviewersGroup identifies security reviewers", func(t *testing.T) {
		tests := []struct {
			name     string
			auth     Authorization
			expected bool
		}{
			{
				name: "exact security-reviewers group",
				auth: Authorization{
					PrincipalType: AuthorizationPrincipalTypeGroup,
					Provider:      "*",
					ProviderId:    SecurityReviewersGroup,
					Role:          RoleOwner,
				},
				expected: true,
			},
			{
				name: "security-reviewers with different provider still matches",
				auth: Authorization{
					PrincipalType: AuthorizationPrincipalTypeGroup,
					Provider:      "google",
					ProviderId:    SecurityReviewersGroup,
					Role:          RoleReader,
				},
				expected: true,
			},
			{
				name: "user principal type is not security-reviewers",
				auth: Authorization{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "*",
					ProviderId:    SecurityReviewersGroup,
					Role:          RoleOwner,
				},
				expected: false,
			},
			{
				name: "different group name is not security-reviewers",
				auth: Authorization{
					PrincipalType: AuthorizationPrincipalTypeGroup,
					Provider:      "*",
					ProviderId:    "editors",
					Role:          RoleOwner,
				},
				expected: false,
			},
			{
				name: "administrators group is not security-reviewers",
				auth: Authorization{
					PrincipalType: AuthorizationPrincipalTypeGroup,
					Provider:      "*",
					ProviderId:    AdministratorsGroup,
					Role:          RoleOwner,
				},
				expected: false,
			},
			{
				name: "everyone pseudo-group is not security-reviewers",
				auth: Authorization{
					PrincipalType: AuthorizationPrincipalTypeGroup,
					Provider:      "*",
					ProviderId:    EveryonePseudoGroup,
					Role:          RoleOwner,
				},
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := IsSecurityReviewersGroup(tt.auth)
				assert.Equal(t, tt.expected, result)
			})
		}
	})
}

func TestCheckResourceAccessFromContext(t *testing.T) {
	// This tests the convenience function that extracts user auth fields from Gin context
	// and delegates to CheckResourceAccessWithGroups

	ownerUser := User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      "tmi",
		ProviderId:    "owner@example.com",
		DisplayName:   "Owner",
		Email:         openapi_types.Email("owner@example.com"),
	}

	tests := []struct {
		name           string
		subject        string
		setupContext   func(c *gin.Context)
		resource       any
		requiredRole   Role
		expectedAccess bool
		expectError    bool
		description    string
	}{
		{
			name:    "owner via context gets access",
			subject: "owner@example.com",
			setupContext: func(c *gin.Context) {
				SetFullUserContext(c, "owner@example.com", "provider-id-owner", "uuid-owner", "tmi", []string{})
			},
			resource: ThreatModel{
				Owner:         ownerUser,
				Authorization: []Authorization{},
			},
			requiredRole:   RoleOwner,
			expectedAccess: true,
			description:    "Owner identified via context should get access",
		},
		{
			name:    "user with group access via context",
			subject: "alice@example.com",
			setupContext: func(c *gin.Context) {
				SetFullUserContext(c, "alice@example.com", "google-uid-alice", "uuid-alice", "google", []string{"editors"})
			},
			resource: ThreatModel{
				Owner: ownerUser,
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeGroup,
						Provider:      "*",
						ProviderId:    "editors",
						Role:          RoleWriter,
					},
				},
			},
			requiredRole:   RoleWriter,
			expectedAccess: true,
			description:    "User in editors group via context should get writer access",
		},
		{
			name:    "user without access",
			subject: "bob@example.com",
			setupContext: func(c *gin.Context) {
				SetFullUserContext(c, "bob@example.com", "google-uid-bob", "uuid-bob", "google", []string{})
			},
			resource: ThreatModel{
				Owner:         ownerUser,
				Authorization: []Authorization{},
			},
			requiredRole:   RoleReader,
			expectedAccess: false,
			description:    "User with no matching access should be denied",
		},
		{
			name:    "empty context fields - only email match works",
			subject: "alice@example.com",
			setupContext: func(c *gin.Context) {
				// Set minimal context - no groups, no provider_id, no internal_uuid
			},
			resource: ThreatModel{
				Owner: ownerUser,
				Authorization: []Authorization{
					{
						PrincipalType: AuthorizationPrincipalTypeUser,
						Provider:      "tmi",
						ProviderId:    "alice@example.com",
						Role:          RoleReader,
					},
				},
			},
			requiredRole:   RoleReader,
			expectedAccess: true,
			description:    "With empty context, email-based match should still work",
		},
		{
			name:    "invalid resource returns error",
			subject: "alice@example.com",
			setupContext: func(c *gin.Context) {
				SetFullUserContext(c, "alice@example.com", "uid", "uuid", "tmi", []string{})
			},
			resource:     "not-a-resource",
			requiredRole: RoleReader,
			expectError:  true,
			description:  "Invalid resource type should return error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

			tt.setupContext(c)

			hasAccess, err := CheckResourceAccessFromContext(c, tt.subject, tt.resource, tt.requiredRole)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedAccess, hasAccess, tt.description)
			}
		})
	}
}

func TestUpdateHighestRole(t *testing.T) {
	tests := []struct {
		name           string
		currentHighest Role
		newRole        Role
		found          bool
		expectedRole   Role
		expectedFound  bool
	}{
		{
			name:           "first role found",
			currentHighest: "",
			newRole:        RoleReader,
			found:          false,
			expectedRole:   RoleReader,
			expectedFound:  true,
		},
		{
			name:           "higher role replaces lower",
			currentHighest: RoleReader,
			newRole:        RoleWriter,
			found:          true,
			expectedRole:   RoleWriter,
			expectedFound:  true,
		},
		{
			name:           "lower role does not replace higher",
			currentHighest: RoleOwner,
			newRole:        RoleReader,
			found:          true,
			expectedRole:   RoleOwner,
			expectedFound:  true,
		},
		{
			name:           "same role does not replace",
			currentHighest: RoleWriter,
			newRole:        RoleWriter,
			found:          true,
			expectedRole:   RoleWriter,
			expectedFound:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultRole, resultFound := updateHighestRole(tt.currentHighest, tt.newRole, tt.found)
			assert.Equal(t, tt.expectedRole, resultRole)
			assert.Equal(t, tt.expectedFound, resultFound)
		})
	}
}

// strPtr is already defined in ptr_helpers.go - reuse it
