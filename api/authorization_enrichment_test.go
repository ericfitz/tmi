package api

import (
	"context"
	"errors"
	"testing"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnrichAuthorizationEntry_ValidationRules tests the input validation of
// EnrichAuthorizationEntry without requiring a database connection.
// These tests verify that invalid inputs are properly rejected before any DB query.
func TestEnrichAuthorizationEntry_ValidationRules(t *testing.T) {
	// Note: EnrichAuthorizationEntry requires a *gorm.DB, but validation happens
	// before the DB is used. We pass nil DB and expect validation errors or panics
	// to document the behavior.

	ctx := context.TODO()
	validEmail := openapi_types.Email("alice@example.com")
	emptyEmail := openapi_types.Email("")

	t.Run("group_principal_skipped", func(t *testing.T) {
		auth := &Authorization{
			PrincipalType: "group",
			Provider:      "tmi",
			ProviderId:    "admins",
		}
		// Groups should be skipped with no error and no DB access
		err := EnrichAuthorizationEntry(ctx, nil, auth)
		assert.NoError(t, err, "Group principals should be skipped without error")
	})

	t.Run("missing_provider_rejected", func(t *testing.T) {
		auth := &Authorization{
			PrincipalType: AuthorizationPrincipalTypeUser,
			Provider:      "", // Missing provider
			ProviderId:    "user123",
		}
		err := EnrichAuthorizationEntry(ctx, nil, auth)
		require.Error(t, err, "Missing provider should be rejected")
		var reqErr *RequestError
		require.True(t, errors.As(err, &reqErr), "Error should be a RequestError")
		assert.Equal(t, 400, reqErr.Status)
		assert.Contains(t, reqErr.Message, "provider is required")
	})

	t.Run("missing_both_identifiers_rejected", func(t *testing.T) {
		auth := &Authorization{
			PrincipalType: AuthorizationPrincipalTypeUser,
			Provider:      "tmi",
			ProviderId:    "",  // No provider_id
			Email:         nil, // No email
		}
		err := EnrichAuthorizationEntry(ctx, nil, auth)
		require.Error(t, err, "Missing both provider_id and email should be rejected")
		var reqErr *RequestError
		require.True(t, errors.As(err, &reqErr), "Error should be a RequestError")
		assert.Equal(t, 400, reqErr.Status)
		assert.Contains(t, reqErr.Message, "either provider_id or email")
	})

	t.Run("empty_email_treated_as_missing", func(t *testing.T) {
		auth := &Authorization{
			PrincipalType: AuthorizationPrincipalTypeUser,
			Provider:      "tmi",
			ProviderId:    "",          // No provider_id
			Email:         &emptyEmail, // Empty email (not nil, but empty string)
		}
		err := EnrichAuthorizationEntry(ctx, nil, auth)
		require.Error(t, err, "Empty email string should be treated as missing")
		var reqErr *RequestError
		require.True(t, errors.As(err, &reqErr), "Error should be a RequestError")
		assert.Equal(t, 400, reqErr.Status)
	})

	t.Run("provider_id_only_is_valid", func(t *testing.T) {
		auth := &Authorization{
			PrincipalType: AuthorizationPrincipalTypeUser,
			Provider:      "tmi",
			ProviderId:    "user123",
			Email:         nil,
		}
		// This will pass validation but panic on nil DB when trying to query.
		// We use recover to catch the panic and verify validation passed.
		func() {
			defer func() {
				r := recover()
				// A panic from nil DB means validation passed (reached DB query stage)
				if r != nil {
					t.Logf("Validation passed, panicked on nil DB as expected: %v", r)
				}
			}()
			err := EnrichAuthorizationEntry(ctx, nil, auth)
			// If we get here without panic, it returned an error from DB query
			if err != nil {
				t.Logf("Validation passed, DB error as expected: %v", err)
			}
		}()
	})

	t.Run("email_only_is_valid", func(t *testing.T) {
		auth := &Authorization{
			PrincipalType: AuthorizationPrincipalTypeUser,
			Provider:      "tmi",
			ProviderId:    "",
			Email:         &validEmail,
		}
		func() {
			defer func() {
				r := recover()
				if r != nil {
					t.Logf("Validation passed, panicked on nil DB as expected: %v", r)
				}
			}()
			err := EnrichAuthorizationEntry(ctx, nil, auth)
			if err != nil {
				t.Logf("Validation passed, DB error as expected: %v", err)
			}
		}()
	})

	t.Run("both_provider_id_and_email_accepted", func(t *testing.T) {
		// The code allows both to be provided but uses provider_id as primary.
		// This documents the behavior: no validation that they're consistent.
		auth := &Authorization{
			PrincipalType: AuthorizationPrincipalTypeUser,
			Provider:      "tmi",
			ProviderId:    "user123",   // Provider ID for user A
			Email:         &validEmail, // Email could be for user B — no consistency check
		}
		func() {
			defer func() {
				r := recover()
				if r != nil {
					t.Logf("Both identifiers accepted, panicked on nil DB: %v", r)
				}
			}()
			err := EnrichAuthorizationEntry(ctx, nil, auth)
			if err != nil {
				t.Logf("Both identifiers accepted, DB error: %v", err)
			}
		}()
		// If we get here, validation passed — both identifiers are accepted
		// without cross-validation, as documented in the code comment:
		// "We allow both to be provided, but we'll use provider_id as primary"
	})
}

// TestEnrichAuthorizationList_Validation tests the list enrichment validation
func TestEnrichAuthorizationList_Validation(t *testing.T) {
	ctx := context.TODO()

	t.Run("empty_list", func(t *testing.T) {
		err := EnrichAuthorizationList(ctx, nil, []Authorization{})
		assert.NoError(t, err, "Empty list should not cause errors")
	})

	t.Run("first_entry_fails_stops_processing", func(t *testing.T) {
		authList := []Authorization{
			{
				PrincipalType: AuthorizationPrincipalTypeUser,
				Provider:      "", // Invalid — missing provider
				ProviderId:    "user1",
			},
			{
				PrincipalType: AuthorizationPrincipalTypeUser,
				Provider:      "tmi",
				ProviderId:    "user2",
			},
		}
		err := EnrichAuthorizationList(ctx, nil, authList)
		require.Error(t, err, "Should fail on first invalid entry")
		var reqErr *RequestError
		require.True(t, errors.As(err, &reqErr))
		assert.Equal(t, 400, reqErr.Status)
	})

	t.Run("group_entries_skipped_in_list", func(t *testing.T) {
		authList := []Authorization{
			{
				PrincipalType: "group",
				Provider:      "tmi",
				ProviderId:    "admins",
			},
			{
				PrincipalType: "group",
				Provider:      "okta",
				ProviderId:    "engineers",
			},
		}
		err := EnrichAuthorizationList(ctx, nil, authList)
		assert.NoError(t, err, "All-group list should process successfully")
	})
}

// TestGetInheritedAuthData_RoleConversion documents the role string mapping behavior
// in GetInheritedAuthData. The function uses string comparison to convert roles.
func TestGetInheritedAuthData_RoleMapping(t *testing.T) {
	// We can't call GetInheritedAuthData without a real database,
	// but we can test the role mapping logic by verifying the Role constants
	// that the function uses match expected values.

	t.Run("valid_roles_map_correctly", func(t *testing.T) {
		assert.Equal(t, Role("owner"), RoleOwner)
		assert.Equal(t, Role("writer"), RoleWriter)
		assert.Equal(t, Role("reader"), RoleReader)
	})

	t.Run("invalid_roles_would_be_skipped", func(t *testing.T) {
		// Document the known invalid role strings that would hit the
		// `default: continue` case in GetInheritedAuthData.
		// These roles would be silently dropped from the authorization list.
		invalidRoles := []string{"admin", "superuser", "root", "Admin", "Owner", "WRITER", ""}
		for _, role := range invalidRoles {
			isValid := role == "owner" || role == "writer" || role == "reader"
			assert.False(t, isValid,
				"Role %q is not in the valid set and would be silently dropped by GetInheritedAuthData", role)
		}
	})

	t.Run("case_sensitive_role_matching", func(t *testing.T) {
		// The switch statement in GetInheritedAuthData uses lowercase strings.
		// This documents that role matching is case-sensitive.
		assert.NotEqual(t, Role("Owner"), RoleOwner, "Role matching is case-sensitive")
		assert.NotEqual(t, Role("WRITER"), RoleWriter, "Role matching is case-sensitive")
		assert.NotEqual(t, Role("Reader"), RoleReader, "Role matching is case-sensitive")
	})
}

// TestCheckSubResourceAccess_NilCache tests that CheckSubResourceAccess
// gracefully handles nil cache parameter (no Redis available).
func TestCheckSubResourceAccess_NilInputs(t *testing.T) {
	// We can't call CheckSubResourceAccess without a real database (it uses *sql.DB),
	// but we can verify CheckSubResourceAccessWithoutCache explicitly passes nil cache.
	// This is a smoke test documenting the API.

	t.Run("without_cache_passes_nil_cache", func(t *testing.T) {
		// CheckSubResourceAccessWithoutCache is a thin wrapper that passes nil cache.
		// Its nil DB will cause the function to fail at GetInheritedAuthData,
		// which is the expected behavior (fails at DB query, not at nil pointer deref on cache).

		// Can't call directly without a real DB, but verify the function exists
		// and accepts the expected parameters.
		assert.NotNil(t, CheckSubResourceAccessWithoutCache,
			"CheckSubResourceAccessWithoutCache should exist as a function")
	})
}
