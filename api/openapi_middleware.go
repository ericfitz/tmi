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

	// Check for "no matching operation" errors (route/method not found in OpenAPI spec)
	messageLower := strings.ToLower(message)
	if strings.Contains(messageLower, "no matching operation") {
		// Provide detailed error message explaining what went wrong
		detailedMessage := fmt.Sprintf(
			"The endpoint '%s %s' is not defined in the API specification. "+
				"Check the request method and path (including trailing slashes).",
			c.Request.Method, c.Request.URL.Path)
		tmiError = &RequestError{
			Code:    "not_found",
			Message: detailedMessage,
			Status:  http.StatusNotFound,
		}
	} else {
		// Handle other validation errors
		switch statusCode {
		case http.StatusBadRequest:
			if strings.Contains(messageLower, "required") {
				tmiError = InvalidInputError(message)
			} else if strings.Contains(messageLower, "format") ||
				strings.Contains(messageLower, "pattern") {
				tmiError = InvalidIDError(message)
			} else {
				tmiError = InvalidInputError(message)
			}
		case http.StatusUnprocessableEntity:
			tmiError = InvalidInputError(message)
		default:
			tmiError = ServerError(message)
		}
	}

	// Log the final error being returned to client for debugging
	requestError := tmiError.(*RequestError)
	logger.Error("OPENAPI_ERROR_CONVERTED [%s] Code: %s, Message: %s",
		requestID, requestError.Code, requestError.Message)

	HandleRequestError(c, tmiError)
}

// GinServerErrorHandler converts parameter binding errors to TMI's error format
// This is used by the oapi-codegen generated server wrapper to handle parameter binding errors
func GinServerErrorHandler(c *gin.Context, err error, statusCode int) {
	requestID := getRequestID(c)
	logger := slogging.GetContextLogger(c)

	// Log the parameter binding error
	logger.Error("PARAMETER_BINDING_FAILED [%s] %s %s -> %d: %s",
		requestID, c.Request.Method, c.Request.URL.Path, statusCode, err.Error())

	// Convert to TMI error format
	var tmiError error
	errorMessage := err.Error()
	messageLower := strings.ToLower(errorMessage)

	switch statusCode {
	case http.StatusBadRequest:
		// Check if it's an enum validation error
		if strings.Contains(messageLower, "enum") ||
			strings.Contains(messageLower, "invalid value") {
			tmiError = InvalidInputError(fmt.Sprintf("Invalid parameter value: %s", errorMessage))
		} else if strings.Contains(messageLower, "required") {
			tmiError = InvalidInputError(fmt.Sprintf("Missing required parameter: %s", errorMessage))
		} else if strings.Contains(messageLower, "format") ||
			strings.Contains(messageLower, "pattern") {
			tmiError = InvalidIDError(errorMessage)
		} else {
			tmiError = InvalidInputError(errorMessage)
		}
	case http.StatusUnprocessableEntity:
		tmiError = InvalidInputError(errorMessage)
	default:
		tmiError = ServerError(errorMessage)
	}

	// Log the final error being returned to client
	requestError := tmiError.(*RequestError)
	logger.Error("PARAMETER_ERROR_CONVERTED [%s] Code: %s, Message: %s",
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
