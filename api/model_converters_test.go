package api

import (
	"testing"

	"github.com/ericfitz/tmi/api/models"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
)

func TestUserModelToAPI(t *testing.T) {
	t.Run("converts all fields", func(t *testing.T) {
		model := &models.User{
			Email:    "alice@example.com",
			Name:     "Alice",
			Provider: "google",
		}

		result := userModelToAPI(model)

		assert.Equal(t, UserPrincipalType(AuthorizationPrincipalTypeUser), result.PrincipalType)
		assert.Equal(t, "google", result.Provider)
		assert.Equal(t, "alice@example.com", result.ProviderId)
		assert.Equal(t, "Alice", result.DisplayName)
		assert.Equal(t, openapi_types.Email("alice@example.com"), result.Email)
	})

	t.Run("handles empty fields", func(t *testing.T) {
		model := &models.User{
			Email:    "",
			Name:     "",
			Provider: "",
		}

		result := userModelToAPI(model)

		assert.Equal(t, "", result.Provider)
		assert.Equal(t, "", result.ProviderId)
		assert.Equal(t, "", result.DisplayName)
		assert.Equal(t, openapi_types.Email(""), result.Email)
	})
}
