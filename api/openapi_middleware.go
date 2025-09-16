package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	middleware "github.com/oapi-codegen/gin-middleware"
)

// OpenAPIErrorHandler converts OpenAPI validation errors to TMI's error format
func OpenAPIErrorHandler(c *gin.Context, message string, statusCode int) {
	var tmiError error

	// Enhanced debug logging for OpenAPI validation failures with request tracing
	requestID := getRequestID(c)
	logger := slogging.GetContextLogger(c)
	logger.Error("OPENAPI_VALIDATION_FAILED [%s] %s %s -> %d: %s",
		requestID, c.Request.Method, c.Request.URL.Path, statusCode, message)

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

	// Log the final error being returned to client for debugging
	requestError := tmiError.(*RequestError)
	logger.Error("OPENAPI_ERROR_CONVERTED [%s] Code: %s, Message: %s",
		requestID, requestError.Code, requestError.Message)

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

	// Clear servers to avoid host validation issues in tests
	swagger.Servers = nil

	// Create OpenAPI validator with custom logic to skip WebSocket routes
	validator := middleware.OapiRequestValidatorWithOptions(swagger,
		&middleware.Options{
			ErrorHandler:          OpenAPIErrorHandler,
			SilenceServersWarning: true, // Silence the servers warning for tests
		})

	// Return a wrapper that skips validation for WebSocket routes
	return func(c *gin.Context) {
		requestID := getRequestID(c)
		logger := slogging.GetContextLogger(c)

		// Skip OpenAPI validation for WebSocket endpoints
		// WebSocket endpoints are not REST APIs and shouldn't be validated against OpenAPI spec
		if strings.HasSuffix(c.Request.URL.Path, "/ws") {
			logger.Debug("OPENAPI_VALIDATION_SKIPPED [%s] WebSocket endpoint: %s %s",
				requestID, c.Request.Method, c.Request.URL.Path)
			c.Next()
			return
		}

		// Log that OpenAPI validation is being applied
		logger.Debug("OPENAPI_VALIDATION_STARTING [%s] %s %s",
			requestID, c.Request.Method, c.Request.URL.Path)

		// Apply OpenAPI validation for all routes
		validator(c)

		// Log if validation passed (if we get here, validation succeeded)
		logger.Debug("OPENAPI_VALIDATION_PASSED [%s] %s %s",
			requestID, c.Request.Method, c.Request.URL.Path)
	}, nil
}
