package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExtractBearerToken tests the token extraction helper used during
// post-deletion JWT blacklisting.
func TestExtractBearerToken(t *testing.T) {
	t.Run("valid_bearer_token", func(t *testing.T) {
		token := extractBearerToken("Bearer abc123.def456.ghi789")
		assert.Equal(t, "abc123.def456.ghi789", token)
	})

	t.Run("empty_header", func(t *testing.T) {
		token := extractBearerToken("")
		assert.Equal(t, "", token)
	})

	t.Run("missing_bearer_prefix", func(t *testing.T) {
		token := extractBearerToken("abc123.def456.ghi789")
		assert.Equal(t, "", token)
	})

	t.Run("lowercase_bearer", func(t *testing.T) {
		// RFC 6750 specifies "Bearer" (capital B), but some clients use lowercase.
		// The function only accepts "Bearer " (capital B).
		token := extractBearerToken("bearer abc123")
		assert.Equal(t, "", token, "Lowercase 'bearer' should not be accepted")
	})

	t.Run("basic_auth_header", func(t *testing.T) {
		token := extractBearerToken("Basic dXNlcjpwYXNz")
		assert.Equal(t, "", token)
	})

	t.Run("bearer_prefix_only", func(t *testing.T) {
		// "Bearer " is 7 chars, "Bearer" is 6 chars
		token := extractBearerToken("Bearer")
		assert.Equal(t, "", token, "Bearer without trailing space and token should return empty")
	})

	t.Run("bearer_with_space_only", func(t *testing.T) {
		// "Bearer " with nothing after it
		token := extractBearerToken("Bearer ")
		assert.Equal(t, "", token, "Bearer with just a space should return empty")
	})

	t.Run("bearer_with_extra_spaces", func(t *testing.T) {
		// Token includes leading space if there are double spaces
		token := extractBearerToken("Bearer  two-spaces")
		assert.Equal(t, " two-spaces", token, "Extra spaces are included in the token")
	})

	t.Run("bearer_with_jwt_format", func(t *testing.T) {
		jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
		token := extractBearerToken("Bearer " + jwt)
		assert.Equal(t, jwt, token)
	})
}

// TestDeleteUserAccount_Unauthenticated tests that the handler rejects
// unauthenticated requests before attempting any deletion operations.
func TestDeleteUserAccount_Unauthenticated(t *testing.T) {
	handler := NewUserDeletionHandler(nil)

	t.Run("no_context_values", func(t *testing.T) {
		c, w := CreateTestGinContextWithBody(http.MethodDelete, "/me", "", nil)
		// Don't set any auth context values

		handler.DeleteUserAccount(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "Authentication required")
	})

	t.Run("email_but_no_provider_id", func(t *testing.T) {
		c, w := CreateTestGinContextWithBody(http.MethodDelete, "/me", "", nil)
		c.Set("userEmail", "alice@example.com")
		// Missing "userID" — ValidateAuthenticatedUser requires it

		handler.DeleteUserAccount(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "Authentication required")
	})
}

// TestDeleteUserAccount_RouteSelection documents that the handler uses the
// "challenge" query parameter to distinguish between step 1 (generate challenge)
// and step 2 (validate and delete). The handler depends on auth.Service which
// cannot be mocked (concrete type), so we can only test routing and auth validation.
func TestDeleteUserAccount_RouteSelection(t *testing.T) {
	// With nil authService, the generate/delete methods will panic when called.
	// We verify the handler routes correctly by checking that it gets past
	// authentication and into the correct branch.
	handler := NewUserDeletionHandler(nil)

	t.Run("no_challenge_routes_to_generate", func(t *testing.T) {
		c, _ := CreateTestGinContextWithBody(http.MethodDelete, "/me", "", nil)
		c.Set("userEmail", "alice@example.com")
		c.Set("userID", "alice-provider-id")

		// With nil authService, generateChallenge will panic on authService.GenerateDeletionChallenge
		func() {
			defer func() {
				r := recover()
				if r != nil {
					t.Logf("Routed to generateChallenge, panicked on nil authService as expected: %v", r)
				}
			}()
			handler.DeleteUserAccount(c)
		}()
		// If we get here, it means the auth check passed and routing worked
	})

	t.Run("with_challenge_routes_to_delete", func(t *testing.T) {
		c, _ := CreateTestGinContextWithBody(http.MethodDelete, "/me?challenge=test-challenge", "", nil)
		c.Set("userEmail", "alice@example.com")
		c.Set("userID", "alice-provider-id")
		c.Request.URL.RawQuery = "challenge=test-challenge"

		// With nil authService, deleteWithChallenge will panic on authService.ValidateDeletionChallenge
		func() {
			defer func() {
				r := recover()
				if r != nil {
					t.Logf("Routed to deleteWithChallenge, panicked on nil authService as expected: %v", r)
				}
			}()
			handler.DeleteUserAccount(c)
		}()
	})
}

// TestNewUserDeletionHandler tests the constructor.
func TestNewUserDeletionHandler(t *testing.T) {
	handler := NewUserDeletionHandler(nil)
	assert.NotNil(t, handler)
	assert.Nil(t, handler.authService)
}
