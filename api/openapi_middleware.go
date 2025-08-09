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

	return middleware.OapiRequestValidatorWithOptions(swagger,
		&middleware.Options{
			ErrorHandler:          OpenAPIErrorHandler,
			SilenceServersWarning: true, // Silence the servers warning for tests
		}), nil
}
