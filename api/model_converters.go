package api

import (
	"github.com/ericfitz/tmi/api/models"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// userModelToAPI converts an auth/models.User to the OpenAPI User type.
func userModelToAPI(model *models.User) *User {
	email := openapi_types.Email(model.Email)
	return &User{
		PrincipalType: UserPrincipalType(AuthorizationPrincipalTypeUser),
		Provider:      model.Provider,
		ProviderId:    model.Email,
		DisplayName:   model.Name,
		Email:         email,
	}
}
