package api

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ExtractUUID extracts and validates a UUID from a path parameter
// Returns the parsed UUID or an error with HTTP response already sent
func ExtractUUID(c *gin.Context, paramName string) (uuid.UUID, error) {
	id := c.Param(paramName)
	if id == "" {
		err := InvalidIDError("Missing " + paramName)
		HandleRequestError(c, err)
		return uuid.Nil, err
	}

	parsedID, parseErr := uuid.Parse(id)
	if parseErr != nil {
		err := InvalidIDError("Invalid " + paramName + " format, must be a valid UUID")
		HandleRequestError(c, err)
		return uuid.Nil, err
	}

	return parsedID, nil
}

// ExtractRequiredUUIDs extracts and validates multiple required UUID parameters
// Returns a map of parameter names to UUIDs, or an error with HTTP response already sent
func ExtractRequiredUUIDs(c *gin.Context, paramNames ...string) (map[string]uuid.UUID, error) {
	result := make(map[string]uuid.UUID, len(paramNames))

	for _, paramName := range paramNames {
		parsedID, err := ExtractUUID(c, paramName)
		if err != nil {
			// Error response already sent by ExtractUUID
			return nil, err
		}
		result[paramName] = parsedID
	}

	return result, nil
}

// ExtractOptionalUUID extracts and validates an optional UUID from a path parameter
// Returns the parsed UUID (or uuid.Nil if not present), and an error if parsing fails
func ExtractOptionalUUID(c *gin.Context, paramName string) (uuid.UUID, error) {
	id := c.Param(paramName)
	if id == "" {
		return uuid.Nil, nil
	}

	parsedID, parseErr := uuid.Parse(id)
	if parseErr != nil {
		err := InvalidIDError("Invalid " + paramName + " format, must be a valid UUID")
		HandleRequestError(c, err)
		return uuid.Nil, err
	}

	return parsedID, nil
}
