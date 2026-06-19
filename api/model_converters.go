package api

import (
	"github.com/ericfitz/tmi/api/models"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// userModelToAPI converts an auth/models.User to the OpenAPI User type.
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: convert a DB user model to the OpenAPI User DTO (pure)
func userModelToAPI(model *models.User) *User {
	email := openapi_types.Email(model.Email)
	return &User{
		PrincipalType: UserPrincipalType(AuthorizationPrincipalTypeUser),
		Provider:      string(model.Provider),
		ProviderId:    string(model.Email),
		DisplayName:   string(model.Name),
		Email:         email,
	}
}
