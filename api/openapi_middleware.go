package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	middleware "github.com/oapi-codegen/gin-middleware"
)

// OpenAPIErrorHandler converts OpenAPI validation errors to TMI's error format
func OpenAPIErrorHandler(c *gin.Context, message string, statusCode int) {
	var tmiError error

	switch statusCode {
	case http.StatusBadRequest:
		if strings.Contains(strings.ToLower(message), "required") {
			tmiError = InvalidInputError(message)
		} else if strings.Contains(strings.ToLower(message), "format") ||
			strings.Contains(strings.ToLower(message), "pattern") {
			tmiError = InvalidIDError(message)
		} else {
			tmiError = InvalidInputError(message)
		}
	case http.StatusUnprocessableEntity:
		tmiError = InvalidInputError(message)
	default:
		tmiError = ServerError(message)
	}

	HandleRequestError(c, tmiError)
}

// SetupOpenAPIValidation creates and returns OpenAPI validation middleware
func SetupOpenAPIValidation() (gin.HandlerFunc, error) {
	swagger, err := GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("failed to load OpenAPI spec: %w", err)
	}

	// Clear security requirements for all paths to avoid authentication validation errors
	// Authentication is handled separately by our JWT middleware
	swagger.Components.SecuritySchemes = nil

	// Remove security requirements from all paths since we handle auth with our own JWT middleware
	for _, pathItem := range swagger.Paths.Map() {
		if pathItem.Get != nil {
			pathItem.Get.Security = nil
		}
		if pathItem.Post != nil {
			pathItem.Post.Security = nil
		}
		if pathItem.Put != nil {
			pathItem.Put.Security = nil
		}
		if pathItem.Patch != nil {
			pathItem.Patch.Security = nil
		}
		if pathItem.Delete != nil {
			pathItem.Delete.Security = nil
		}
	}
	return middleware.OapiRequestValidatorWithOptions(swagger,
		&middleware.Options{
			ErrorHandler:          OpenAPIErrorHandler,
			SilenceServersWarning: true, // Silence the servers warning for tests
		}), nil
}
