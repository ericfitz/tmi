package api

import (
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// TypesUUID is an alias for openapi_types.UUID to make it easier to use
type TypesUUID = openapi_types.UUID

// ParseUUID converts a string to a TypesUUID
func ParseUUID(s string) (TypesUUID, error) {
	// Parse the UUID from string
	parsedUUID, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, err
	}
	return parsedUUID, nil
}