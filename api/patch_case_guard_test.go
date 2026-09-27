package api

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyPatchOperations_RejectsCaseAliasedPaths pins that a path differing
// from a field name only by case cannot reach that field: encoding/json
// decodes "Alias"/"Owner" into alias/owner, which would bypass every
// exact-path gate (read-only fields, owner/authorization checks).
func TestApplyPatchOperations_RejectsCaseAliasedPaths(t *testing.T) {
	original := PatchTestEntity{
		Name: "entity",
		Owner: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "tmi",
			ProviderId:    "alice",
			Email:         "alice@example.com",
		},
	} // id and description are absent under omitempty
	for _, op := range []PatchOperation{
		{Op: "add", Path: "/ID", Value: "11111111-1111-1111-1111-111111111111"},
		{Op: "add", Path: "/Description", Value: "x"},
		{Op: "add", Path: "/Owner", Value: map[string]any{"provider": "tmi", "provider_id": "mallory"}},
		{Op: "add", Path: "/owner/Provider_Id", Value: "mallory"},
	} {
		t.Run(op.Path, func(t *testing.T) {
			_, err := ApplyPatchOperations(original, []PatchOperation{op})
			var reqErr *RequestError
			require.True(t, errors.As(err, &reqErr), "want RequestError, got %v", err)
			assert.Equal(t, http.StatusBadRequest, reqErr.Status)
			assert.Contains(t, reqErr.Message, op.Path)
		})
	}

	patched, err := ApplyPatchOperations(original, []PatchOperation{
		{Op: "replace", Path: "/name", Value: "renamed"},
		{Op: "add", Path: "/description", Value: "ok"},
	})
	require.NoError(t, err)
	assert.Equal(t, "renamed", patched.Name)
	assert.Equal(t, "ok", *patched.Description)
}
